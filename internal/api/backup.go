package api

// 面板备份与恢复：导出配置（config.json、credentials、mirror.conf）与
// 全部应用的 config.env / installed_version 为一个 tar.gz 流；
// 恢复时还原应用配置，可选恢复面板配置（校验通过后自动重启生效）。

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"nanopanel/internal/config"
)

// backupTar 覆盖的固定文件（相对路径 → 绝对路径）。
func (s *Server) backupFixedFiles() []struct{ rel, abs string } {
	dir := filepath.Dir(s.configPath)
	return []struct{ rel, abs string }{
		{"panel/config.json", s.configPath},
		{"panel/credentials", filepath.Join(dir, "credentials")},
		{"panel/mirror.conf", "/etc/nanopanel/mirror.conf"},
	}
}

// Backup 导出面板与应用配置为 tar.gz 附件。
func (s *Server) Backup(w http.ResponseWriter, r *http.Request) {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	// 注意关闭顺序：必须先关 tar（写出收尾块）再关 gzip（写出压缩尾）。
	defer gz.Close()
	defer tw.Close()

	add := func(rel, abs string) {
		data, err := os.ReadFile(abs)
		if err != nil {
			return // 不存在的固定文件（如未装 credentials）直接跳过
		}
		_ = tw.WriteHeader(&tar.Header{
			Name: rel, Mode: 0600, Size: int64(len(data)),
			ModTime: time.Now(), Format: tar.FormatPAX,
		})
		_, _ = tw.Write(data)
	}

	for _, f := range s.backupFixedFiles() {
		add(f.rel, f.abs)
	}

	s.mu.RLock()
	appsDir := s.cfg.AppsDir
	s.mu.RUnlock()
	items, _ := os.ReadDir(appsDir)
	for _, it := range items {
		if !it.IsDir() {
			continue
		}
		for _, name := range []string{"config.env", "installed_version"} {
			add("apps/"+it.Name()+"/"+name, filepath.Join(appsDir, it.Name(), name))
		}
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=nanopanel-backup-%s.tar.gz",
			time.Now().Format("20060102-150405")))
}

// restoreReport 汇总恢复结果给前端。
type restoreReport struct {
	AppsRestored []string `json:"apps_restored"`
	PanelApplied bool     `json:"panel_applied"`
	PanelStaged  string   `json:"panel_staged,omitempty"`
	Restarted    bool     `json:"restarted"`
}

// Restore 上传备份 tar.gz 并恢复。?apply_config=0 时面板配置只落盘不生效。
func (s *Server) Restore(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		fail(w, http.StatusBadRequest, "解析上传失败: "+err.Error())
		return
	}
	fh := r.MultipartForm.File["file"]
	if len(fh) != 1 {
		fail(w, http.StatusBadRequest, "需要恰好一个备份文件")
		return
	}
	src, err := fh[0].Open()
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	defer src.Close()

	gz, err := gzip.NewReader(io.LimitReader(src, 256<<20))
	if err != nil {
		fail(w, http.StatusBadRequest, "不是有效的 gzip 备份: "+err.Error())
		return
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	report := restoreReport{}
	var panelCfg []byte
	var credentials, mirror []byte
	appsCfg := map[string][]byte{} // appID/config.env -> data
	appsVer := map[string][]byte{}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			fail(w, http.StatusBadRequest, "读取备份失败: "+err.Error())
			return
		}
		name := path.Clean(hdr.Name)
		if strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
			continue // 拒绝异常条目
		}
		data, err := io.ReadAll(io.LimitReader(tr, 16<<20))
		if err != nil || len(data) > 16<<20 {
			continue
		}
		switch {
		case name == "panel/config.json":
			panelCfg = data
		case name == "panel/credentials":
			credentials = data
		case name == "panel/mirror.conf":
			mirror = data
		case strings.HasPrefix(name, "apps/") && strings.HasSuffix(name, "/config.env"):
			appsCfg[strings.TrimSuffix(strings.TrimPrefix(name, "apps/"), "/config.env")] = data
		case strings.HasPrefix(name, "apps/") && strings.HasSuffix(name, "/installed_version"):
			appsVer[strings.TrimSuffix(strings.TrimPrefix(name, "apps/"), "/installed_version")] = data
		}
	}

	if len(panelCfg) == 0 && len(appsCfg) == 0 {
		fail(w, http.StatusBadRequest, "备份中没有可恢复的内容")
		return
	}

	// 应用配置直接落盘（不存在的应用目录创建）
	s.mu.RLock()
	appsDir := s.cfg.AppsDir
	s.mu.RUnlock()
	for id, data := range appsCfg {
		if id == "" || strings.ContainsRune(id, '/') {
			continue
		}
		dir := filepath.Join(appsDir, id)
		if err := os.MkdirAll(dir, 0750); err == nil {
			if err := os.WriteFile(filepath.Join(dir, "config.env"), data, 0600); err == nil {
				report.AppsRestored = append(report.AppsRestored, id)
			}
		}
	}
	for id, data := range appsVer {
		if id == "" || strings.ContainsRune(id, '/') {
			continue
		}
		_ = os.WriteFile(filepath.Join(appsDir, id, "installed_version"), data, 0640)
	}

	applyConfig := r.URL.Query().Get("apply_config") != "0"
	if len(panelCfg) > 0 {
		var probe config.Config
		if err := json.Unmarshal(panelCfg, &probe); err != nil {
			fail(w, http.StatusBadRequest, "备份中的面板配置损坏: "+err.Error())
			return
		}
		dir := filepath.Dir(s.configPath)
		if applyConfig {
			// 校验通过才覆盖当前配置
			if err := os.WriteFile(s.configPath, panelCfg, 0600); err != nil {
				fail(w, http.StatusInternalServerError, "写入配置失败: "+err.Error())
				return
			}
			if len(credentials) > 0 {
				_ = os.WriteFile(filepath.Join(dir, "credentials"), credentials, 0600)
			}
			report.PanelApplied = true
		} else {
			staged := filepath.Join(dir, "config.json.restored")
			_ = os.WriteFile(staged, panelCfg, 0600)
			report.PanelStaged = staged
		}
		if len(mirror) > 0 {
			_ = os.WriteFile("/etc/nanopanel/mirror.conf", mirror, 0644)
		}
	}

	s.audit(r, "settings", "恢复备份",
		fmt.Sprintf("应用 %d 个，面板配置 %v", len(report.AppsRestored), report.PanelApplied))
	if report.PanelApplied {
		report.Restarted = true
		_ = s.restartSelf() // 生效需要重启
	}
	ok(w, report)
}
