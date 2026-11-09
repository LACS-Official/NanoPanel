package api

// 受限文件管理器：在白名单根目录内浏览、上传、下载、删除、重命名，
// 以及小文本文件的在线编辑。所有变更操作写审计日志并按 file 事件推送。

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// defaultFileRoots 文件管理器允许访问的默认根目录。
var defaultFileRoots = []string{
	"/mnt", "/media", "/opt", "/srv", "/home", "/data",
	"/etc/nanopanel", "/var/log/nanopanel", "/var/lib",
}

// fileEntry 目录列表中的一项。
type fileEntry struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	Mode    string `json:"mode"`
	ModTime string `json:"mod_time"`
	// Text 可在线编辑的候选：小于 1MB 的常规文件（下载/编辑按钮依此显示）。
	Text bool `json:"text"`
}

// fileRoots 返回当前文件管理根目录（去重、按平台语义清洗）。
func (s *Server) fileRoots() []string {
	s.mu.RLock()
	roots := append([]string(nil), s.cfg.FileRoots...)
	s.mu.RUnlock()
	if len(roots) == 0 {
		roots = defaultFileRoots
	}
	out := make([]string, 0, len(roots))
	seen := make(map[string]bool)
	for _, r := range roots {
		c := cleanManagedPath(r)
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// cleanManagedPath 按平台语义清洗路径：绝对路径原样清洗，
// 相对路径按根相对处理（"/data" 在 Linux 即根下 data）。
func cleanManagedPath(p string) string {
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") && !filepath.IsAbs(filepath.FromSlash(p)) {
		p = "/" + p
	}
	return filepath.Clean(filepath.FromSlash(p))
}

// withinRoots 判断已解析路径是否落在根目录集合内（含根本身）。
// 两侧都做短名/软链规范化后再比较，避免 Windows 8.3 短路径
// （如 RUNNER~1）与实际长路径写法不一致导致误判越界。
func withinRoots(resolved string, roots []string) bool {
	resolved = canonicalPath(resolved)
	for _, root := range roots {
		rel, err := filepath.Rel(canonicalPath(root), resolved)
		if err != nil {
			continue
		}
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return true
		}
	}
	return false
}

// canonicalPath 把路径规范为绝对路径并解析软链与平台短名；
// Windows 下另做小写化，使路径归属比较不受大小写影响。
// 路径不存在（如待创建文件）时保留 Abs 结果，不视为失败。
func canonicalPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

// samePath 判断两个路径是否指向同一位置（解析软链与平台短名，Windows 忽略大小写）。
func samePath(a, b string) bool {
	return canonicalPath(a) == canonicalPath(b)
}

// resolveFilePath 校验面板操作的文件路径：必须落在某个根目录内，
// 并解析软链防止借道越界。非存在路径按其最近存在祖先解析。
func (s *Server) resolveFilePath(p string) (string, error) {
	if len(p) > 512 || strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("路径不合法")
	}
	clean := cleanManagedPath(p)
	if clean == string(filepath.Separator) {
		return "", fmt.Errorf("不允许操作根目录")
	}

	// 从目标路径逐级向上找到最近的存在路径，解析软链后回拼下级。
	// 例如 /mnt/disk/link/newfile：解析 /mnt/disk/link，回拼 newfile。
	probe, suffix := clean, ""
	for {
		if _, err := os.Lstat(probe); err == nil {
			break
		}
		if suffix == "" {
			suffix = filepath.Base(probe)
		} else {
			suffix = filepath.Join(filepath.Base(probe), suffix)
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", fmt.Errorf("路径不可用")
		}
		probe = parent
	}
	resolved, err := filepath.EvalSymlinks(probe)
	if err != nil {
		return "", fmt.Errorf("路径不可用")
	}
	if suffix != "" {
		resolved = filepath.Join(resolved, suffix)
	}

	if !withinRoots(resolved, s.fileRoots()) {
		return "", fmt.Errorf("路径超出允许范围")
	}
	return resolved, nil
}

// ListFiles 列出目录内容。
func (s *Server) ListFiles(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("path")
	if raw == "" {
		ok(w, map[string]interface{}{"roots": s.fileRoots(), "entries": []fileEntry{}})
		return
	}
	p, err := s.resolveFilePath(raw)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	st, err := os.Stat(p)
	if err != nil {
		fail(w, http.StatusNotFound, "路径不存在或不可访问")
		return
	}
	if !st.IsDir() {
		fail(w, http.StatusBadRequest, "不是目录")
		return
	}
	items, err := os.ReadDir(p)
	if err != nil {
		fail(w, http.StatusInternalServerError, "读取目录失败: "+err.Error())
		return
	}
	entries := make([]fileEntry, 0, len(items))
	for _, it := range items {
		info, err := it.Info()
		if err != nil {
			continue // 竞态中被删除的条目跳过
		}
		e := fileEntry{
			Name:    it.Name(),
			IsDir:   it.IsDir(),
			Size:    info.Size(),
			Mode:    info.Mode().String(),
			ModTime: info.ModTime().Format("2006-01-02 15:04"),
			Text:    isSmallText(p+"/"+it.Name(), info),
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return entries[i].Name < entries[j].Name
	})
	ok(w, map[string]interface{}{"path": p, "entries": entries})
}

// isSmallText 判断是否为可在线编辑的小文件（≤1MB 的常规文件）。
func isSmallText(p string, info os.FileInfo) bool {
	if info.IsDir() || info.Size() > 1<<20 || !info.Mode().IsRegular() {
		return false
	}
	// 设备文件、socket 等一律不算
	if info.Mode().Type() != 0 {
		return false
	}
	_ = p
	return true
}

// DownloadFile 以附件形式下发文件。
func (s *Server) DownloadFile(w http.ResponseWriter, r *http.Request) {
	p, err := s.resolveFilePath(r.URL.Query().Get("path"))
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		fail(w, http.StatusNotFound, "文件不存在")
		return
	}
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", path.Base(p)))
	http.ServeFile(w, r, p)
}

// archiveLimit 打包下载的目录总大小上限（1GB）。
const archiveLimit = int64(1) << 30

// DownloadArchive 把目录打包为 tar.gz 流式下载。
// 跳过软链与非常规文件；打包前预扫描总大小，超过 1GB 时拒绝。
func (s *Server) DownloadArchive(w http.ResponseWriter, r *http.Request) {
	p, err := s.resolveFilePath(r.URL.Query().Get("path"))
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	st, err := os.Stat(p)
	if err != nil || !st.IsDir() {
		fail(w, http.StatusNotFound, "目录不存在")
		return
	}
	// 预扫描：统计常规文件总大小与数量，超限直接拒绝（避免边打包边发现超限）。
	var total int64
	var files int
	var tooBig bool
	_ = filepath.WalkDir(p, func(fp string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 不可读的条目直接跳过
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		total += info.Size()
		files++
		if total > archiveLimit {
			tooBig = true
			return filepath.SkipAll
		}
		return nil
	})
	if tooBig {
		fail(w, http.StatusBadRequest, "目录总大小超过 1GB，拒绝打包下载")
		return
	}
	if files == 0 {
		fail(w, http.StatusBadRequest, "目录内没有可打包的常规文件")
		return
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", path.Base(p)+".tar.gz"))
	gzw := gzip.NewWriter(w)
	tw := tar.NewWriter(gzw)
	_ = filepath.WalkDir(p, func(fp string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, rerr := filepath.Rel(p, fp)
		if rerr != nil || rel == "." {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil || !info.Mode().IsRegular() && !d.IsDir() {
			return nil
		}
		hdr, herr := tar.FileInfoHeader(info, "")
		if herr != nil {
			return nil
		}
		hdr.Name = filepath.ToSlash(rel)
		if d.IsDir() {
			hdr.Name += "/"
		} else {
			hdr.Mode = int64(info.Mode().Perm())
		}
		if tw.WriteHeader(hdr) != nil {
			return nil
		}
		if !d.IsDir() {
			f, ferr := os.Open(fp)
			if ferr != nil {
				return nil
			}
			_, _ = io.Copy(tw, f)
			f.Close()
		}
		return nil
	})
	tw.Close()
	gzw.Close()
}

// ReadTextFile 返回小文本文件内容（≤1MB）。
func (s *Server) ReadTextFile(w http.ResponseWriter, r *http.Request) {
	p, err := s.resolveFilePath(r.URL.Query().Get("path"))
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	st, err := os.Stat(p)
	if err != nil || st.IsDir() || st.Size() > 1<<20 || !st.Mode().IsRegular() {
		fail(w, http.StatusNotFound, "文件不存在或超过 1MB")
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		fail(w, http.StatusInternalServerError, "读取失败: "+err.Error())
		return
	}
	ok(w, map[string]interface{}{"path": p, "content": string(data)})
}

type fileWriteReq struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// WriteTextFile 保存小文本文件内容。
func (s *Server) WriteTextFile(w http.ResponseWriter, r *http.Request) {
	var req fileWriteReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Content) > 1<<20 {
		fail(w, http.StatusBadRequest, "内容超过 1MB")
		return
	}
	p, err := s.resolveFilePath(req.Path)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		fail(w, http.StatusBadRequest, "目标是目录")
		return
	}
	if err := os.WriteFile(p, []byte(req.Content), 0644); err != nil {
		fail(w, http.StatusInternalServerError, "写入失败: "+err.Error())
		return
	}
	s.audit(r, "file", "编辑文件", p)
	ok(w, map[string]string{"msg": "已保存"})
}

// UploadFiles 接收 multipart 上传，写入指定目录。
func (s *Server) UploadFiles(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	p, err := s.resolveFilePath(dir)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	st, err := os.Stat(p)
	if err != nil || !st.IsDir() {
		fail(w, http.StatusBadRequest, "目标目录不存在")
		return
	}
	// 单文件 1GB 上限（小内存设备实际上传远小于此，由磁盘为准）。
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		fail(w, http.StatusBadRequest, "解析上传失败: "+err.Error())
		return
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		fail(w, http.StatusBadRequest, "没有接收到文件")
		return
	}
	saved := make([]string, 0, len(files))
	for _, fh := range files {
		name := path.Base(filepath.ToSlash(fh.Filename))
		if name == "" || name == "." || name == "/" || strings.ContainsAny(name, "\x00") {
			continue
		}
		src, err := fh.Open()
		if err != nil {
			continue
		}
		dst, err := os.OpenFile(p+"/"+name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			src.Close()
			continue
		}
		_, copyErr := io.Copy(dst, io.LimitReader(src, 1<<30))
		dst.Close()
		src.Close()
		if copyErr != nil {
			continue
		}
		saved = append(saved, name)
	}
	if len(saved) == 0 {
		fail(w, http.StatusInternalServerError, "全部文件保存失败")
		return
	}
	s.audit(r, "file", "上传文件", p+"（"+strings.Join(saved, ", ")+"）")
	ok(w, map[string]interface{}{"msg": "已保存 " + fmt.Sprint(len(saved)) + " 个文件", "saved": saved})
}

type fileActionReq struct {
	Path      string `json:"path"`
	To        string `json:"to,omitempty"`
	Recursive bool   `json:"recursive,omitempty"`
}

// MkdirFile 创建目录。
func (s *Server) MkdirFile(w http.ResponseWriter, r *http.Request) {
	var req fileActionReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := s.resolveFilePath(req.Path)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.MkdirAll(p, 0755); err != nil {
		fail(w, http.StatusInternalServerError, "创建失败: "+err.Error())
		return
	}
	s.audit(r, "file", "新建目录", p)
	ok(w, map[string]string{"msg": "已创建"})
}

// DeleteFile 删除文件或目录（目录必须显式 recursive）。
func (s *Server) DeleteFile(w http.ResponseWriter, r *http.Request) {
	var req fileActionReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := s.resolveFilePath(req.Path)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	// 根目录本身不可删。
	for _, root := range s.fileRoots() {
		if samePath(p, root) {
			fail(w, http.StatusBadRequest, "不允许删除根目录")
			return
		}
	}
	st, err := os.Stat(p)
	if err != nil {
		fail(w, http.StatusNotFound, "路径不存在")
		return
	}
	if st.IsDir() {
		if !req.Recursive {
			fail(w, http.StatusBadRequest, "删除目录需勾选「递归删除」")
			return
		}
		if err := os.RemoveAll(p); err != nil {
			fail(w, http.StatusInternalServerError, "删除失败: "+err.Error())
			return
		}
	} else if err := os.Remove(p); err != nil {
		fail(w, http.StatusInternalServerError, "删除失败: "+err.Error())
		return
	}
	detail := p
	if st.IsDir() {
		detail += "（递归）"
	}
	s.audit(r, "file", "删除文件", detail)
	ok(w, map[string]string{"msg": "已删除"})
}

// RenameFile 重命名/移动（目标路径同样受根目录约束）。
func (s *Server) RenameFile(w http.ResponseWriter, r *http.Request) {
	var req fileActionReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	from, err := s.resolveFilePath(req.Path)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	to, err := s.resolveFilePath(req.To)
	if err != nil {
		fail(w, http.StatusBadRequest, "目标路径: "+err.Error())
		return
	}
	if _, err := os.Stat(from); err != nil {
		fail(w, http.StatusNotFound, "源路径不存在")
		return
	}
	if _, err := os.Stat(to); err == nil {
		fail(w, http.StatusConflict, "目标已存在")
		return
	}
	if err := os.Rename(from, to); err != nil {
		fail(w, http.StatusInternalServerError, "重命名失败: "+err.Error())
		return
	}
	s.audit(r, "file", "重命名", from+" -> "+to)
	ok(w, map[string]string{"msg": "已重命名"})
}
