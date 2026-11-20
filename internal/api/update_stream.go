package api

// 升级过程的实时日志与下载节点择优。
// 设计目标：
//   - 前端点击升级后能看到每一步（检查、探测节点、下载进度、校验、替换、重启）；
//   - 下载不再按固定顺序试源，而是先并发探测真实 Release 资产的延迟，
//     按延迟从低到高下载，失败自动切换到下一个节点。

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"nanopanel/internal/version"
)

// updateLogf 升级过程日志回调，参数为 printf 格式化串。
type updateLogf func(format string, args ...interface{})

// updateNode 升级下载的一个候选节点。
type updateNode struct {
	ID      string
	Name    string
	Base    string
	Latency time.Duration
	OK      bool
	Detail  string
}

// updateNodes 汇总候选节点：白名单 + 环境变量中的自定义镜像，按 URL 去重。
// 白名单来自 mirror_source.go，避免任意 URL 造成 SSRF。
func updateNodes() []updateNode {
	nodes := []updateNode{}
	seen := map[string]bool{}
	add := func(id, name, prefix string) {
		prefix = strings.TrimRight(strings.TrimSpace(prefix), "/")
		if prefix == "" || seen[prefix] {
			return
		}
		seen[prefix] = true
		if name == "" {
			name = id
		}
		nodes = append(nodes, updateNode{ID: id, Name: name, Base: prefix})
	}
	for _, c := range githubMirrorCandidates {
		add(c.ID, c.Name, c.Prefix)
	}
	if m := os.Getenv("GITHUB_MIRROR"); m != "" {
		add("custom", os.Getenv("GITHUB_MIRROR_NAME"), m)
	}
	return nodes
}

// probeUpdateNodes 并发探测各节点对目标资产的实际响应速度，并按可用性/延迟排序。
// 探测的是真实 Release 下载路径（而非镜像首页），因此排序结果直接反映下载体验。
// 探测全部失败时保留原始顺序，由下载环节逐个尝试。
// releaseTag 规范化下载路径中的 tag：GitHub 上真实 tag 形如 v0.2.2，
// 而 latestReleaseTag 通过 parseTagFromURL 返回的是去掉 v 的纯版本号（如 0.2.2）。
// 若直接拼进 /releases/download/<tag>/ 会得到 .../download/0.2.2/... 并返回 404，
// 因此构造探测与下载 URL 前统一补齐小写 v 前缀（与 deploy/npctl 的行为一致）。
func releaseTag(tag string) string {
	t := strings.TrimLeft(strings.TrimSpace(tag), "vV")
	if t == "" {
		return strings.TrimSpace(tag)
	}
	return "v" + t
}

func probeUpdateNodes(tag, asset string, nodes []updateNode, logf updateLogf) []updateNode {
	path := "/" + updateRepo + "/releases/download/" + releaseTag(tag) + "/" + asset
	// 与镜像测速一致：绕过系统代理直连，避免代理缓存让节点延迟失真。
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	client := &http.Client{
		Timeout:   6 * time.Second,
		Transport: tr,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	var wg sync.WaitGroup
	for i := range nodes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			url := nodes[i].Base + path
			start := time.Now()
			resp, err := client.Head(url)
			if err != nil || resp == nil || resp.StatusCode >= http.StatusBadRequest {
				// 部分代理不支持 HEAD，改用小范围 GET 回退。
				if resp != nil {
					resp.Body.Close()
				}
				req, rerr := http.NewRequest(http.MethodGet, url, nil)
				if rerr != nil {
					nodes[i].Detail = "请求构造失败"
					return
				}
				req.Header.Set("Range", "bytes=0-0")
				resp, err = client.Do(req)
			}
			if err != nil || resp == nil {
				nodes[i].Detail = "连接失败"
				return
			}
			defer resp.Body.Close()
			nodes[i].Latency = time.Since(start)
			nodes[i].OK = resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusBadRequest
			nodes[i].Detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}(i)
	}
	wg.Wait()

	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].OK != nodes[j].OK {
			return nodes[i].OK
		}
		return nodes[i].Latency < nodes[j].Latency
	})

	if logf != nil {
		logf("下载节点探测结果（目标 %s）：", asset)
		for _, n := range nodes {
			mark := "✗"
			if n.OK {
				mark = "✓"
				logf("  %s %-14s %6d ms  %s", mark, n.Name, n.Latency.Milliseconds(), n.Base)
			} else {
				logf("  %s %-14s %-10s  %s", mark, n.Name, n.Detail, n.Base)
			}
		}
	}
	return nodes
}

// fmtVer 保证版本号带且只带一个 v 前缀（version.Version 可能已带 v）。
func fmtVer(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "未知版本"
	}
	if strings.HasPrefix(v, "v") || strings.HasPrefix(v, "V") {
		return v
	}
	return "v" + v
}

// fmtBytes 把字节数格式化为可读单位，用于下载进度日志。
func fmtBytes(n int64) string {
	if n < 0 {
		return "?"
	}
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	v := float64(n)
	for _, unit := range []string{"KB", "MB", "GB"} {
		v /= 1024
		if v < 1024 || unit == "GB" {
			return fmt.Sprintf("%.1f %s", v, unit)
		}
	}
	return "?"
}

// updateProgress 统计已下载字节并周期性输出进度。
type updateProgress struct {
	w     io.Writer
	total int64
	done  int64
	last  time.Time
	logf  updateLogf
}

func (p *updateProgress) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.done += int64(n)
	now := time.Now()
	if p.total > 0 && (p.done >= p.total || now.Sub(p.last) >= 400*time.Millisecond) {
		pct := p.done * 100 / p.total
		if pct > 100 {
			pct = 100
		}
		if p.logf != nil {
			p.logf("  进度 %s / %s (%d%%)", fmtBytes(p.done), fmtBytes(p.total), pct)
		}
		p.last = now
	}
	return n, err
}

// UpdateApplyStream 以 SSE 流式执行升级，实时回传每一步日志。
// 使用 POST 提交，客户端需带 Authorization: Bearer <token>（EventSource 不支持自定义头）。
func (s *Server) UpdateApplyStream(w http.ResponseWriter, r *http.Request) {
	flusher, canFlush := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(payload map[string]interface{}) {
		b, err := json.Marshal(payload)
		if err != nil {
			return
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		if canFlush {
			flusher.Flush()
		}
	}
	logf := func(format string, args ...interface{}) {
		send(map[string]interface{}{
			"event": "log",
			"line":  "[" + time.Now().Format("15:04:05") + "] " + fmt.Sprintf(format, args...),
		})
	}

	// 与自动更新互斥；appIDRe 不允许该 key，故不会与应用安装操作冲突。
	if !s.tryBeginAction("__nanopanel_update__") {
		logf("已有升级任务正在执行，请稍候")
		send(map[string]interface{}{"event": "done", "ok": false, "status": "busy", "msg": "已有升级任务正在执行"})
		return
	}
	defer s.endAction("__nanopanel_update__")

	send(map[string]interface{}{"event": "status", "phase": "checking"})
	logf("开始检查新版本（当前 %s）", fmtVer(version.Version))
	info, err := s.checkAndApplyUpdateWithLog(true, logf)
	if err != nil {
		msg := err.Error()
		if info.Status == "unreachable" {
			msg = "无法连接更新源：" + info.Message
		}
		logf("升级失败：%s", msg)
		send(map[string]interface{}{"event": "done", "ok": false, "status": "failed", "msg": msg})
		return
	}
	if !info.HasUpdate {
		logf("当前已是最新版本 v%s", info.LatestVersion)
		send(map[string]interface{}{"event": "done", "ok": true, "status": "uptodate"})
		return
	}
	send(map[string]interface{}{"event": "status", "phase": "rebooting", "latest": info.LatestVersion})
	logf("升级完成！面板将在数秒后自动重启，请稍后刷新页面。")
	send(map[string]interface{}{
		"event": "done", "ok": true, "status": "updating", "latest": info.LatestVersion,
	})
}
