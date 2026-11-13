package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"nanopanel/internal/version"
)

// updateRepo 是发布版本所在的 GitHub 仓库（与 npctl upgrade 保持一致）。
const updateRepo = "LACS-Official/NanoPanel"

// UpdateInfo 描述一次版本检查结果。
type UpdateInfo struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	HasUpdate      bool   `json:"has_update"`
	ReleaseURL     string `json:"release_url,omitempty"`
	Status         string `json:"status"` // ok | unreachable | unknown
	Message        string `json:"message,omitempty"`
	CheckedAt      string `json:"checked_at"`
}

// CheckUpdate 检查 GitHub 是否有比当前版本更新的 Release。
// 即使网络不可达也返回 200，仅 Status 标记为 unreachable，便于前端友好提示。
func (s *Server) CheckUpdate(w http.ResponseWriter, r *http.Request) {
	info := UpdateInfo{
		CurrentVersion: version.Version,
		CheckedAt:      time.Now().UTC().Format(time.RFC3339),
		Status:         "unknown",
	}
	tag, url, err := latestReleaseTag()
	if err != nil {
		info.Status = "unreachable"
		info.Message = err.Error()
		ok(w, info)
		return
	}
	info.LatestVersion = tag
	info.ReleaseURL = url
	info.Status = "ok"
	info.HasUpdate = VersionGreater(tag, version.Version)
	if info.HasUpdate {
		info.Message = "发现新版本 v" + tag
	} else {
		info.Message = "已是最新版本"
	}
	ok(w, info)
}

// updateBases 返回更新源基址列表：优先环境变量指定的镜像，其次 GitHub 官方，
// 最后 ghfast 镜像。与 deploy/npctl 的 cmd_upgrade 保持一致。
func updateBases() []string {
	bases := []string{}
	if m := strings.TrimRight(os.Getenv("GITHUB_MIRROR"), "/"); m != "" {
		bases = append(bases, m)
	}
	bases = append(bases, "https://github.com", "https://ghfast.top/https://github.com")
	return bases
}

// latestReleaseTag 从 GitHub（带镜像回退）解析最新 Release 的 tag 与发布页地址。
// 逻辑与 deploy/npctl 的 cmd_upgrade 保持一致。
//
// 关键修正：不再"第一个能解析出 tag 的源就采用"，而是并行探测所有源、收集到
// 的全部 tag 后取最高版本。原因：安装脚本会把探测到的最优镜像写入
// /etc/nanopanel/mirror.conf 并经 EnvironmentFile 注入 GITHUB_MIRROR，若该镜像的
// releases/latest 缓存陈旧（例如仍停在旧版本），旧逻辑会直接采信它而永远报
// "已是最新"。改为取全部源的最大值即可绕过个别镜像的过期缓存。
func latestReleaseTag() (tag, releaseURL string, err error) {
	bases := updateBases()

	type cand struct{ tag, url string }
	ch := make(chan cand, len(bases))
	var wg sync.WaitGroup
	for _, base := range bases {
		wg.Add(1)
		go func(base string) {
			defer wg.Done()
			if t, u := fetchLatestTag(base); t != "" {
				ch <- cand{t, u}
			}
		}(base)
	}
	go func() { wg.Wait(); close(ch) }()

	found := map[string]string{} // tag -> releaseURL，主协程独占写入，无竞态
	for c := range ch {
		found[c.tag] = c.url
	}
	if len(found) == 0 {
		return "", "", fmt.Errorf("无法解析最新版本（所有更新源均不可达或返回异常）")
	}
	best := ""
	for t := range found {
		if best == "" || VersionGreater(t, best) {
			best = t
		}
	}
	return best, found[best], nil
}

// fetchLatestTag 向单个源查询最新 Release 的 tag。不跟随重定向（releases/latest
// 的 302 Location 即为最新 tag）。失败或无 tag 时返回空字符串。
func fetchLatestTag(base string) (tag, releaseURL string) {
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, rerr := http.NewRequest(http.MethodGet, base+"/"+updateRepo+"/releases/latest", nil)
	if rerr != nil {
		return "", ""
	}
	req.Header.Set("User-Agent", "NanoPanel/"+version.Version)
	resp, derr := client.Do(req)
	if derr != nil {
		return "", ""
	}
	loc := ""
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		loc = resp.Header.Get("Location")
	}
	resp.Body.Close()
	if loc == "" {
		return "", ""
	}
	if t := parseTagFromURL(loc); t != "" {
		return t, "https://github.com/" + updateRepo + "/releases/tag/v" + t
	}
	return "", ""
}

// parseTagFromURL 从 GitHub 跳转地址中提取版本号（去掉前缀 v）。
func parseTagFromURL(loc string) string {
	if i := strings.LastIndex(loc, "/tag/"); i >= 0 {
		return strings.TrimPrefix(loc[i+5:], "v")
	}
	if i := strings.LastIndex(loc, "/releases/"); i >= 0 {
		return strings.TrimPrefix(loc[i+10:], "v")
	}
	return ""
}

// VersionGreater 判断 a 是否严格大于 b（仅比较数字段，忽略前缀 v 与预发布后缀）。
func VersionGreater(a, b string) bool {
	pa, pb := parseSemver(a), parseSemver(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

// parseSemver 将 "v1.2.3-rc1" 之类解析为 [major, minor, patch] 三个整数。
func parseSemver(v string) [3]int {
	var r [3]int
	v = strings.TrimLeft(strings.TrimSpace(v), "vV")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	for i := 0; i < len(parts) && i < 3; i++ {
		n := 0
		for _, c := range parts[i] {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		r[i] = n
	}
	return r
}

// downloadReleaseAsset 从更新源（带镜像回退）下载指定 Release 资产到 dest。
// 任一源成功即返回；全部失败返回错误。
func (s *Server) downloadReleaseAsset(tag, asset, dest string) error {
	client := &http.Client{Timeout: 300 * time.Second}
	for _, base := range updateBases() {
		url := base + "/" + updateRepo + "/releases/download/" + tag + "/" + asset
		req, rerr := http.NewRequest(http.MethodGet, url, nil)
		if rerr != nil {
			continue
		}
		req.Header.Set("User-Agent", "NanoPanel/"+version.Version)
		resp, derr := client.Do(req)
		if derr != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}
		f, ferr := os.Create(dest)
		if ferr != nil {
			resp.Body.Close()
			continue
		}
		_, cerr := io.Copy(f, resp.Body)
		resp.Body.Close()
		f.Close()
		if cerr != nil {
			os.Remove(dest)
			continue
		}
		return nil
	}
	return fmt.Errorf("下载失败，请检查网络或稍后重试")
}
