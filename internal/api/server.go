// Package api 实现面板的全部 HTTP 接口。
package api

import (
	"context"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"nanopanel/internal/auth"
	"nanopanel/internal/config"
	"nanopanel/internal/sysinfo"
)

// Server 持有面板运行所需的依赖。
type Server struct {
	cfg        *config.Config
	auth       *auth.Manager
	configPath string
	version    string
	limiter    *auth.LoginLimiter
	// logDir 面板操作日志目录，空值时按平台默认。
	logDir string

	// mu 保护运行期可变的配置字段（PasswordHash、Webhook、监控间隔等）。
	mu sync.RWMutex

	// healthMu 保护应用健康状态快照，避免定时检测与手动检测并发。
	healthMu    sync.Mutex
	lastHealth  map[string]string // appID -> running / down / unknown / not_installed
	tempAlerted bool              // 温度告警去重标记（滞回带内只告警一次）

	// mihomoBase 仅用于测试注入，为空时按配置的控制端口拼装。
	mihomoBase string

	// unitProbe 仅用于测试注入，为空时通过 systemctl 查询单元状态。
	unitProbe func(context.Context, string) string

	// tempProbe 仅用于测试注入，为空时读取 sysinfo 热区温度。
	tempProbe func() []sysinfo.TempZone

	// selfRestart 仅用于测试注入，为空时通过 systemctl 重启面板服务。
	selfRestart func() error

	// tlsDir 仅用于测试注入，为空时使用默认证书目录。
	tlsDir string

	// actionMu 与 runningActions 防止同一应用并发执行安装/卸载脚本。
	actionMu       sync.Mutex
	runningActions map[string]bool
}

// NewServer 创建 API 服务容器。
func NewServer(cfg *config.Config, am *auth.Manager, configPath, version string) *Server {
	return &Server{
		cfg:        cfg,
		auth:       am,
		configPath: configPath,
		version:    version,
		limiter:    auth.NewLoginLimiter(5, 10*time.Minute, time.Minute),
	}
}

// bearer 从请求头提取登录 token。
func (s *Server) bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return r.Header.Get("X-Token")
}

// AuthMiddleware 保护需要登录的接口。
func (s *Server) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.auth.Valid(s.bearer(r)) {
			fail(w, http.StatusUnauthorized, "未登录或登录已过期")
			return
		}
		next(w, r)
	}
}

// SecurityHeaders 为所有响应补充基础安全头。
func (s *Server) SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-XSS-Protection", "0")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// Services 对外暴露托管服务清单（供路由注册与前端使用）。
func (s *Server) Services() []config.ServiceDef {
	return s.cfg.Services
}

// Health 是公开的健康检查端点，供安装脚本与监控系统使用。
func (s *Server) Health(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]string{
		"status":   "ok",
		"name":     "NanoPanel",
		"version":  s.version,
		"platform": runtime.GOOS,
	})
}

// clientIP 返回对端 IP。刻意不信任 X-Forwarded-For，避免绕过登录限速。
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) username() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Username
}

func (s *Server) passwordHash() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.PasswordHash
}
