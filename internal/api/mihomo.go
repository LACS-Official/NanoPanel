package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

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
