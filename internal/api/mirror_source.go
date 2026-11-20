package api

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// mirrorCandidate 一个可选的 GitHub 下载源。prefix 用于拼 release 下载链接，
// probe 为连通性测试地址（通常是镜像首页，比 release API 更轻量）。
type mirrorCandidate struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Prefix string `json:"prefix"`
	Probe  string `json:"probe"`
}

// githubMirrorCandidates 与 deploy/install.sh 的 GITHUB_CANDIDATES 保持一致。
// 使用固定白名单而非接受任意 URL：既避免 SSRF，也保证写入配置的前缀可信。
var githubMirrorCandidates = []mirrorCandidate{
	{ID: "github", Name: "GitHub 官方", Prefix: "https://github.com", Probe: "https://github.com"},
	{ID: "ghfast", Name: "ghfast", Prefix: "https://ghfast.top/https://github.com", Probe: "https://ghfast.top"},
	{ID: "ghproxy", Name: "ghproxy", Prefix: "https://ghproxy.net/https://github.com", Probe: "https://ghproxy.net"},
	{ID: "gh-proxy", Name: "gh-proxy", Prefix: "https://gh-proxy.com/https://github.com", Probe: "https://gh-proxy.com"},
}

func findMirrorCandidate(id string) (mirrorCandidate, bool) {
	for _, c := range githubMirrorCandidates {
		if c.ID == id {
			return c, true
		}
	}
	return mirrorCandidate{}, false
}

// mirrorConfPath 允许测试通过环境变量覆盖配置路径，避免写真实 /etc。
func mirrorConfPath() string {
	if p := os.Getenv("NP_MIRROR_CONF"); p != "" {
		return p
	}
	return "/etc/nanopanel/mirror.conf"
}

// MirrorCandidates 返回可选镜像源列表、当前生效源与配置来源。
func (s *Server) MirrorCandidates(w http.ResponseWriter, r *http.Request) {
	current := os.Getenv("GITHUB_MIRROR")
	currentID := ""
	for _, c := range githubMirrorCandidates {
		if strings.TrimRight(current, "/") == strings.TrimRight(c.Prefix, "/") {
			currentID = c.ID
			break
		}
	}
	if currentID == "" && current != "" {
		currentID = "custom"
	}
	if currentID == "" {
		currentID = "github"
	}
	ok(w, map[string]interface{}{
		"candidates":     githubMirrorCandidates,
		"current_id":     currentID,
		"current_prefix": current,
		"current_name":   os.Getenv("GITHUB_MIRROR_NAME"),
		"selected_at":    os.Getenv("SELECTED_AT"),
		"conf_path":      mirrorConfPath(),
	})
}

type mirrorTestResult struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Latency float64 `json:"latency_ms"` // 毫秒（保留 1 位小数）；测速失败时为 0
	OK      bool    `json:"ok"`
	Status  string  `json:"status"` // HTTP 状态码或错误简述
}

// TestMirrorSources 并发测试全部候选源的连通性与延迟。指定 ?id= 时只测单个。
func (s *Server) TestMirrorSources(w http.ResponseWriter, r *http.Request) {
	only := r.URL.Query().Get("id")
	list := githubMirrorCandidates
	if only != "" {
		if c, found := findMirrorCandidate(only); found {
			list = []mirrorCandidate{c}
		} else {
			fail(w, http.StatusBadRequest, "未知镜像源: "+only)
			return
		}
	}

	// 关键：绕过系统代理直连镜像源。Go 默认 Transport 会读取 HTTP(S)_PROXY，
	// 若本机代理对探测页做缓存，测出的只是"到代理的延迟"（接近 0），并非真实链路。
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{Timeout: 6 * time.Second, Transport: transport}
	results := make([]mirrorTestResult, len(list))
	var wg sync.WaitGroup
	for i, c := range list {
		wg.Add(1)
		go func(i int, c mirrorCandidate) {
			defer wg.Done()
			results[i] = probeMirror(client, c)
		}(i, c)
	}
	wg.Wait()

	// 可用的按延迟升序排前面，不可用的置底，便于前端直观展示最优源。
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].OK != results[j].OK {
			return results[i].OK
		}
		return results[i].Latency < results[j].Latency
	})
	ok(w, map[string]interface{}{"results": results})
}

func probeMirror(client *http.Client, c mirrorCandidate) mirrorTestResult {
	res := mirrorTestResult{ID: c.ID, Name: c.Name}
	req, err := http.NewRequest(http.MethodGet, c.Probe, nil)
	if err != nil {
		res.Status = err.Error()
		return res
	}
	req.Header.Set("User-Agent", "NanoPanel-MirrorTest/1.0")
	req.Header.Set("Cache-Control", "no-cache")
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		res.Status = "连接失败"
		return res
	}
	defer resp.Body.Close()
	// 读取一小段真实响应体再结算：只等响应头时，本机代理缓存命中会测出接近 0 的假延迟。
	// 读前 8KB 既反映真实往返/传输耗时，又不会因首页体积过大拖慢测速。
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
	res.Latency = math.Round(float64(time.Since(start).Microseconds())/10) / 100
	if res.Latency < 0.1 {
		res.Latency = 0.1
	}
	res.Status = fmt.Sprintf("HTTP %d", resp.StatusCode)
	res.OK = resp.StatusCode >= 200 && resp.StatusCode < 400
	return res
}

type selectMirrorReq struct {
	ID string `json:"id"`
}

// SelectMirrorSource 切换 GitHub 下载源：校验白名单 → 写入 mirror.conf → 同步更新进程内环境变量。
// 写配置失败时不改变内存环境，避免"界面显示已切换、重启后又回退"的不一致。
func (s *Server) SelectMirrorSource(w http.ResponseWriter, r *http.Request) {
	var req selectMirrorReq
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	c, found := findMirrorCandidate(req.ID)
	if !found {
		fail(w, http.StatusBadRequest, "未知镜像源: "+req.ID)
		return
	}
	if err := writeMirrorConf(c); err != nil {
		s.audit(r, "system", "切换下载源失败", c.Name+": "+err.Error())
		fail(w, http.StatusInternalServerError, "写入配置失败: "+err.Error())
		return
	}
	os.Setenv("GITHUB_MIRROR", c.Prefix)
	os.Setenv("GITHUB_MIRROR_NAME", c.Name)
	os.Setenv("GITHUB_MIRROR_LATENCY", "manual")
	os.Setenv("SELECTED_AT", time.Now().UTC().Format(time.RFC3339))
	s.audit(r, "system", "切换下载源", c.Name+" ("+c.Prefix+")")
	ok(w, map[string]interface{}{
		"id":      c.ID,
		"name":    c.Name,
		"prefix":  c.Prefix,
		"message": "已切换到 " + c.Name + "，下次更新检查即生效",
	})
}

// writeMirrorConf 更新 mirror.conf 中的 GitHub 源相关键，保留 APT_* 与注释等其他行。
// 采用临时文件 + rename 保证写入原子性，避免中途失败留下半截配置。
func writeMirrorConf(c mirrorCandidate) error {
	path := mirrorConfPath()
	existing, _ := os.ReadFile(path)
	lines := []string{}
	if len(existing) > 0 {
		for _, line := range strings.Split(strings.TrimRight(string(existing), "\n"), "\n") {
			key := strings.TrimSpace(strings.SplitN(line, "=", 2)[0])
			switch key {
			case "GITHUB_MIRROR", "GITHUB_MIRROR_NAME", "GITHUB_MIRROR_LATENCY", "SELECTED_AT", "# NanoPanel 自动探测出的最优镜像源，由安装脚本生成，可手工修改。":
				continue // 下面统一重写
			}
			lines = append(lines, line)
		}
	}
	header := "# NanoPanel 下载源配置，由面板「应用 → 下载源」管理，可手工修改。"
	lines = append([]string{header,
		"GITHUB_MIRROR=" + c.Prefix,
		"GITHUB_MIRROR_NAME=" + c.Name,
		"GITHUB_MIRROR_LATENCY=manual",
		"SELECTED_AT=" + time.Now().UTC().Format(time.RFC3339),
	}, lines...)

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
