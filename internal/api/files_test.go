package api

// 文件管理器测试：白名单约束、越界拒绝、软链借道、增删改查链路。

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestWithinRootsNormalization 根目录与目标路径写法不一致时仍应正确判定归属。
// 回归：GitHub Windows runner 的临时目录是 8.3 短名（RUNNER~1），
// 解析软链后变成长名，旧实现按字符串前缀比较会误判「超出允许范围」。
func TestWithinRootsNormalization(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "docs")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if !withinRoots(sub, []string{root}) {
		t.Fatalf("子目录应判定在根内: root=%s sub=%s", root, sub)
	}
	if withinRoots(t.TempDir(), []string{root}) {
		t.Fatal("无关目录不应判定在根内")
	}
	if runtime.GOOS == "windows" && !withinRoots(strings.ToUpper(sub), []string{root}) {
		t.Fatal("Windows 下大小写差异不应影响归属判定")
	}
}

func newFileTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	srv, _ := newTestServer(t)
	srv.logDir = t.TempDir()
	root := t.TempDir()
	srv.mu.Lock()
	srv.cfg.FileRoots = []string{root}
	srv.mu.Unlock()
	return srv, root
}

func fileBody(t *testing.T, srv *Server, method, url, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, url, nil)
	} else {
		req = httptest.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	srv.mu.RLock()
	defer srv.mu.RUnlock()
	switch strings.SplitN(strings.TrimPrefix(url, "/api/v1/files"), "?", 2)[0] {
	case "":
		srv.ListFiles(rec, req)
	case "/download":
		srv.DownloadFile(rec, req)
	case "/archive":
		srv.DownloadArchive(rec, req)
	case "/read":
		srv.ReadTextFile(rec, req)
	case "/write":
		srv.WriteTextFile(rec, req)
	case "/mkdir":
		srv.MkdirFile(rec, req)
	case "/delete":
		srv.DeleteFile(rec, req)
	case "/rename":
		srv.RenameFile(rec, req)
	default:
		t.Fatalf("未知的文件端点: %s", url)
	}
	return rec
}

// TestFilePathWhitelist 白名单外的路径必须被拒绝。
func TestFilePathWhitelist(t *testing.T) {
	srv, root := newFileTestServer(t)
	_ = root

	// 系统敏感路径拒绝（Windows 上等价于系统根下的路径）
	for _, p := range []string{"/etc/passwd", "C:/Windows/System32/config", "/etc"} {
		if _, err := srv.resolveFilePath(p); err == nil {
			t.Errorf("敏感路径 %q 不应通过校验", p)
		}
	}
	// 根目录本身拒绝操作
	if _, err := srv.resolveFilePath("/"); err == nil {
		t.Error("根目录不应通过校验")
	}
}

// TestFileCRUD 全链路：建目录 → 上传文件内容 → 读 → 编辑 → 列表 → 重命名 → 删除。
func TestFileCRUD(t *testing.T) {
	srv, root := newFileTestServer(t)
	rootSlash := filepath.ToSlash(root)

	// mkdir
	rec := fileBody(t, srv, http.MethodPost, "/api/v1/files/mkdir",
		"{\"path\":\""+rootSlash+"/docs\"}")
	if rec.Code != http.StatusOK {
		t.Fatalf("mkdir 失败: %d %s", rec.Code, rec.Body.String())
	}

	// write
	rec = fileBody(t, srv, http.MethodPost, "/api/v1/files/write",
		"{\"path\":\""+rootSlash+"/docs/note.txt\",\"content\":\"hello nanopanel\"}")
	if rec.Code != http.StatusOK {
		t.Fatalf("write 失败: %d %s", rec.Code, rec.Body.String())
	}

	// read
	rec = fileBody(t, srv, http.MethodGet, "/api/v1/files/read?path="+rootSlash+"/docs/note.txt", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "hello nanopanel") {
		t.Fatalf("read 失败: %d %s", rec.Code, rec.Body.String())
	}

	// list
	rec = fileBody(t, srv, http.MethodGet, "/api/v1/files?path="+rootSlash+"/docs", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "note.txt") {
		t.Fatalf("list 失败: %d %s", rec.Code, rec.Body.String())
	}

	// rename
	rec = fileBody(t, srv, http.MethodPost, "/api/v1/files/rename",
		"{\"path\":\""+rootSlash+"/docs/note.txt\",\"to\":\""+rootSlash+"/docs/note2.txt\"}")
	if rec.Code != http.StatusOK {
		t.Fatalf("rename 失败: %d %s", rec.Code, rec.Body.String())
	}

	// 非递归删目录必须拒绝
	rec = fileBody(t, srv, http.MethodPost, "/api/v1/files/delete",
		"{\"path\":\""+rootSlash+"/docs\"}")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非递归删目录应 400: %d", rec.Code)
	}

	// 递归删除
	rec = fileBody(t, srv, http.MethodPost, "/api/v1/files/delete",
		"{\"path\":\""+rootSlash+"/docs\",\"recursive\":true}")
	if rec.Code != http.StatusOK {
		t.Fatalf("递归删除失败: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); !os.IsNotExist(err) {
		t.Fatal("目录应已被删除")
	}
}

// TestFileRootDeletionRefused 根目录本身不可删除；根下新建内容可删。
func TestFileRootDeletionRefused(t *testing.T) {
	srv, root := newFileTestServer(t)

	rec := fileBody(t, srv, http.MethodPost, "/api/v1/files/delete",
		`{"path":"`+filepath.ToSlash(root)+`","recursive":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("删除根目录应被拒绝: %d %s", rec.Code, rec.Body.String())
	}
}

// TestFileSymlinkEscape 软链借道出根目录必须被拒绝。
func TestFileSymlinkEscape(t *testing.T) {
	if !symlinksSupported() {
		t.Skip("当前平台/环境不支持软链")
	}
	srv, root := newFileTestServer(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s"), 0644); err != nil {
		t.Fatal(err)
	}
	// 在根内建一个指向根外的软链
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	// 通过软链访问根外文件必须失败
	rec := fileBody(t, srv, http.MethodGet, "/api/v1/files/read?path="+filepath.ToSlash(root)+"/escape/secret.txt", "")
	if rec.Code == http.StatusOK {
		t.Fatal("软链借道访问根外文件不应成功")
	}
	// 直接访问根外也必须失败
	rec = fileBody(t, srv, http.MethodGet, "/api/v1/files/read?path="+filepath.ToSlash(outside)+"/secret.txt", "")
	if rec.Code == http.StatusOK {
		t.Fatal("根外路径不应可访问")
	}
}

func symlinksSupported() bool {
	tmp, err := os.MkdirTemp("", "np-sym")
	if err != nil {
		return false
	}
	defer os.RemoveAll(tmp)
	return os.Symlink(tmp, tmp+"/link") == nil
}

// TestFileArchive 目录打包下载：内容完整、跳过软链、白名单外的目录被拒绝。
func TestFileArchive(t *testing.T) {
	srv, root := newFileTestServer(t)
	sub := filepath.Join(root, "docs")
	if err := os.MkdirAll(filepath.Join(sub, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "a.txt"), []byte("hello archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "nested", "b.txt"), []byte("inner"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 软链不应被打包
	if symlinksSupported() {
		outside := t.TempDir()
		_ = os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o644)
		_ = os.Symlink(outside, filepath.Join(sub, "link"))
	}

	rec := fileBody(t, srv, http.MethodGet, "/api/v1/files/archive?path="+filepath.ToSlash(sub), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("打包下载应成功，got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/gzip" {
		t.Fatalf("Content-Type 应为 application/gzip，got %q", ct)
	}

	// 解包校验
	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip 头解析失败: %v", err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	found := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar 解析失败: %v", err)
		}
		if hdr.Typeflag == tar.TypeReg {
			data, _ := io.ReadAll(tr)
			found[hdr.Name] = string(data)
		}
	}
	if found["a.txt"] != "hello archive" {
		t.Fatalf("a.txt 内容不符: %q", found["a.txt"])
	}
	if found[filepath.ToSlash(filepath.Join("nested", "b.txt"))] != "inner" {
		t.Fatalf("nested/b.txt 内容不符: %v", found)
	}
	for name := range found {
		if strings.Contains(name, "link") {
			t.Fatalf("软链不应被打包: %s", name)
		}
	}

	// 空目录应被拒绝
	empty := filepath.Join(root, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	rec = fileBody(t, srv, http.MethodGet, "/api/v1/files/archive?path="+filepath.ToSlash(empty), "")
	if rec.Code == http.StatusOK {
		t.Fatal("空目录打包应被拒绝")
	}

	// 根外目录应被拒绝
	rec = fileBody(t, srv, http.MethodGet, "/api/v1/files/archive?path="+filepath.ToSlash(t.TempDir()), "")
	if rec.Code == http.StatusOK {
		t.Fatal("白名单外目录打包应被拒绝")
	}
}
