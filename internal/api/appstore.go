package api

// 应用商店远程更新：从 GitHub 仓库拉取最新的 manifest.json 与应用脚本，
// 原地合并（保留 config.env / installed_version 等运行时状态）。
// 仓库与分支可通过环境变量 NP_APPS_REPO / NP_APPS_BRANCH 覆盖，
// GITHUB_MIRROR 为完整前缀（如 https://ghfast.top/https://github.com）。

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// repoAppsURL 返回清单目录的下载基址（以 / 结尾）。
// 走 raw.githubusercontent 通道（比 github.com/raw 重定向兼容性好），
// GITHUB_MIRROR 为完整前缀（如 https://ghfast.top/https://github.com）时，
// 把前缀中的 github.com 换成 raw.githubusercontent.com。
func (s *Server) repoAppsURL() string {
	if s.appsRepoURL != "" {
		return s.appsRepoURL // 测试注入
	}
	repo := os.Getenv("NP_APPS_REPO")
	if repo == "" {
		repo = "LACS-Official/NanoPanel"
	}
	branch := os.Getenv("NP_APPS_BRANCH")
	if branch == "" {
		branch = "main"
	}
	raw := "https://raw.githubusercontent.com/" + repo + "/" + branch + "/deploy/apps/"
	if mirror := os.Getenv("GITHUB_MIRROR"); mirror != "" {
		// https://ghfast.top/https://github.com → https://ghfast.top/https://raw.githubusercontent.com
		raw = strings.Replace(mirror, "://github.com", "://raw.githubusercontent.com", 1) + "/" + repo + "/" + branch + "/deploy/apps/"
	}
	return raw
}

// appUpdateHTTPClient 远程拉取专用客户端。
var appUpdateHTTPClient = &http.Client{Timeout: 90 * time.Second}

// fetchRepoFile 拉取仓库文件，404 视为不存在返回 (nil, nil)。
func fetchRepoFile(base, name string) ([]byte, error) {
	resp, err := appUpdateHTTPClient.Get(base + name)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, name)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// UpdateManifest 从远程仓库更新应用清单与脚本。
func (s *Server) UpdateManifest(w http.ResponseWriter, r *http.Request) {
	base := s.repoAppsURL()

	manifest, err := fetchRepoFile(base, "manifest.json")
	if err != nil {
		fail(w, http.StatusBadGateway, "拉取清单失败: "+err.Error())
		return
	}
	if manifest == nil {
		fail(w, http.StatusBadGateway, "远程仓库没有 manifest.json（仓库尚未发布，或 NP_APPS_REPO 配置的路径不正确）")
		return
	}
	var probe appManifestFile
	if err := json.Unmarshal(manifest, &probe); err != nil {
		fail(w, http.StatusBadGateway, "远程清单损坏: "+err.Error())
		return
	}
	if len(probe.Apps) == 0 {
		fail(w, http.StatusBadGateway, "远程清单没有任何应用")
	}
	for _, a := range probe.Apps {
		if !appIDRe.MatchString(a.ID) {
			fail(w, http.StatusBadGateway, "远程清单含非法应用 ID: "+a.ID)
			return
		}
	}

	type updated struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	var results []updated
	s.mu.RLock()
	appsDir := s.cfg.AppsDir
	manifestPath := s.cfg.ManifestPath
	s.mu.RUnlock()

	for _, a := range probe.Apps {
		scripts := []string{"install.sh", "remove.sh", "status.sh"}
		// config.sh 可选：没有 config 字段的应用无需该脚本。
		dir := filepath.Join(appsDir, a.ID)
		if err := os.MkdirAll(dir, 0750); err != nil {
			fail(w, http.StatusInternalServerError, "创建应用目录失败: "+err.Error())
			return
		}
		// 原子性：先全部拉到临时目录再落盘
		staged := make(map[string][]byte)
		for _, sc := range scripts {
			data, err := fetchRepoFile(base, a.ID+"/"+sc)
			if err != nil {
				fail(w, http.StatusBadGateway, "拉取 "+a.ID+"/"+sc+" 失败: "+err.Error())
				return
			}
			if data == nil {
				fail(w, http.StatusBadGateway, "远程缺少 "+a.ID+"/"+sc)
				return
			}
			staged[sc] = data
		}
		if data, err := fetchRepoFile(base, a.ID+"/config.sh"); err != nil {
			fail(w, http.StatusBadGateway, "拉取 "+a.ID+"/config.sh 失败: "+err.Error())
			return
		} else if data != nil {
			staged["config.sh"] = data
		}
		for name, data := range staged {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0750); err != nil {
				fail(w, http.StatusInternalServerError, "写入脚本失败: "+err.Error())
				return
			}
		}
		results = append(results, updated{ID: a.ID, Version: a.Version})
	}

	// 清单最后落盘：全部脚本就位后生效。
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0750); err == nil {
		_ = os.WriteFile(manifestPath, manifest, 0640)
	}

	s.audit(r, "app", "更新应用商店清单", fmt.Sprintf("%d 个应用", len(results)))
	ok(w, map[string]interface{}{
		"msg":    "清单已更新",
		"apps":   results,
		"count":  len(results),
		"source": regexp.MustCompile(`\?.*`).ReplaceAllString(base, ""),
	})
}
