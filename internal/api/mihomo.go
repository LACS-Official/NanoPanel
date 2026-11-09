package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// mihomoPathRe 限制可转发到 mihomo 控制接口的路径，避免面板被当成任意 HTTP 代理。
// 覆盖：版本、配置、代理组/节点、测速、连接、规则、代理提供者。
// 节点名允许中文等非 ASCII 字符，但整条路径不得含查询分隔符、反斜杠或控制字符；
// 另外在 handler 里拒绝含 ".." 的路径，防止穿越到内核的其他接口。
var mihomoPathRe = regexp.MustCompile(`^/(version|configs|proxies|connections|rules|providers/proxies)(/[^/?#\\\x00-\x1f]+)*$`)

// mihomoClient 独立客户端：短超时、不跟随重定向（防止被引导到内网其他地址）。
var mihomoClient = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// mihomoController 从应用配置读取控制端口与密钥。
func (s *Server) mihomoController() (int, string) {
	port := 9090
	secret := ""
	if v, err := s.readAppValues("mihomo"); err == nil {
		if p, err := strconv.Atoi(strings.TrimSpace(v["external_controller_port"])); err == nil && p > 0 && p <= 65535 {
			port = p
		}
		secret = strings.TrimSpace(v["controller_secret"])
	}
	return port, secret
}

// mihomoEndpoint 返回控制接口地址；mihomoBase 仅用于单元测试注入。
func (s *Server) mihomoEndpoint() string {
	if s.mihomoBase != "" {
		return strings.TrimRight(s.mihomoBase, "/")
	}
	port, _ := s.mihomoController()
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func (s *Server) mihomoAuthHeader(req *http.Request) {
	_, secret := s.mihomoController()
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
}

// MihomoStatus 返回 mihomo 是否安装、控制端口与内核版本，供前端管理页判断可用性。
func (s *Server) MihomoStatus(w http.ResponseWriter, r *http.Request) {
	port, _ := s.mihomoController()
	installed := s.appState(r.Context(), "mihomo") == "installed"
	uiEnabled := false
	if installed {
		if v, err := s.readAppValues("mihomo"); err == nil {
			uiEnabled = strings.TrimSpace(v["external_ui"]) == "yes"
		}
	}
	reachable, version := false, ""
	if installed {
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.mihomoEndpoint()+"/version", nil)
		if err == nil {
			s.mihomoAuthHeader(req)
			if resp, err := mihomoClient.Do(req); err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					reachable = true
					body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
					version = extractJSONString(body, "version")
				}
			}
		}
	}
	ok(w, map[string]interface{}{
		"installed":  installed,
		"reachable":  reachable,
		"port":       port,
		"version":    version,
		"controller": "127.0.0.1:" + strconv.Itoa(port),
		"ui_enabled": uiEnabled,
	})
}

// extractJSONString 从简单 JSON 对象中提取字符串字段，避免为 /version 引入完整解析结构。
func extractJSONString(body []byte, key string) string {
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// MihomoProxy 把面板请求安全转发到 mihomo 的 external-controller。
// 用法：/api/v1/mihomo?path=/proxies&<透传给内核的查询参数>
func (s *Server) MihomoProxy(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" || !mihomoPathRe.MatchString(path) || strings.Contains(path, "..") {
		fail(w, http.StatusBadRequest, "不允许的控制接口路径")
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		fail(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	if s.appState(r.Context(), "mihomo") != "installed" {
		fail(w, http.StatusServiceUnavailable, "mihomo 尚未安装")
		return
	}

	q := r.URL.Query()
	q.Del("path")
	// 代理组/节点名可能带空格，显式编码后拼进目标 URL。
	target := s.mihomoEndpoint() + strings.ReplaceAll(path, " ", "%20")
	if encoded := q.Encode(); encoded != "" {
		target += "?" + encoded
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var body io.Reader
	if r.Method != http.MethodGet {
		body = io.LimitReader(r.Body, 1<<20)
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, target, body)
	if err != nil {
		fail(w, http.StatusBadRequest, "构造内核请求失败")
		return
	}
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json; charset=utf-8"
	}
	req.Header.Set("Content-Type", contentType)
	// 刻意不转发面板的 Authorization，避免登录 token 泄漏给内核。
	s.mihomoAuthHeader(req)

	resp, err := mihomoClient.Do(req)
	if err != nil {
		fail(w, http.StatusBadGateway, "无法连接 mihomo 控制接口: "+err.Error())
		return
	}
	defer resp.Body.Close()

	// 写操作（切换节点、改模式、断开连接）记入面板日志并按 app 事件通知；GET 不记录，避免刷屏。
	if r.Method != http.MethodGet && resp.StatusCode < 400 {
		s.audit(r, "app", "Mihomo 控制操作", r.Method+" "+path)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 4<<20))
}
