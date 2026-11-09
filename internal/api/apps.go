package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var appIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// AppMeta 应用商店里的应用信息。
type AppMeta struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Icon        string `json:"icon"`
	Category    string `json:"category"`
	State       string `json:"state"`
}

type appManifestFile struct {
	Version int       `json:"version"`
	Apps    []AppMeta `json:"apps"`
}

func (s *Server) loadManifest() (*appManifestFile, error) {
	data, err := os.ReadFile(s.cfg.ManifestPath)
	if err != nil {
		return nil, err
	}
	var m appManifestFile
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	for _, a := range m.Apps {
		if !appIDRe.MatchString(a.ID) {
			return nil, fmt.Errorf("清单中包含非法应用 ID: %q", a.ID)
		}
	}
	return &m, nil
}

// appState 执行 status.sh，输出 installed / not_installed，超时或异常返回 unknown。
func (s *Server) appState(ctx context.Context, id string) string {
	if !appIDRe.MatchString(id) {
		return "unknown"
	}
	script := filepath.Join(s.cfg.AppsDir, id, "status.sh")
	if _, err := os.Stat(script); err != nil {
		return "unknown"
	}
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "bash", script).Output()
	if err != nil {
		return "unknown"
	}
	state := strings.TrimSpace(string(out))
	if state != "installed" && state != "not_installed" {
		return "unknown"
	}
	return state
}

// ListApps 列出应用商店中的应用及安装状态。
func (s *Server) ListApps(w http.ResponseWriter, r *http.Request) {
	m, err := s.loadManifest()
	if err != nil {
		fail(w, http.StatusInternalServerError, "读取应用清单失败: "+err.Error())
		return
	}
	for i := range m.Apps {
		m.Apps[i].State = s.appState(r.Context(), m.Apps[i].ID)
	}
	ok(w, m.Apps)
}

// AppAction 对指定应用执行 install/remove。
func (s *Server) AppAction(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	action := r.URL.Query().Get("action")

	if !appIDRe.MatchString(id) {
		fail(w, http.StatusBadRequest, "应用 ID 不合法")
		return
	}

	var scriptName string
	switch action {
	case "install":
		scriptName = "install.sh"
	case "remove":
		scriptName = "remove.sh"
	default:
		fail(w, http.StatusBadRequest, "不支持的操作")
		return
	}

	script := filepath.Join(s.cfg.AppsDir, id, scriptName)
	if _, err := os.Stat(script); err != nil {
		fail(w, http.StatusNotFound, "应用脚本不存在")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	out, err := exec.CommandContext(ctx, "bash", script).CombinedOutput()
	if err != nil {
		fail(w, http.StatusInternalServerError,
			fmt.Sprintf("操作失败: %v\n%s", err, strings.TrimSpace(string(out))))
		return
	}
	ok(w, map[string]string{
		"id": id, "action": action, "output": string(out),
		"state": s.appState(ctx, id),
	})
}
