package api

// 内置终端：会话票据、并发控制、WebSocket ↔ PTY 双向转发。
//
// 安全设计：
//   - 长期登录 token 不出现在 WebSocket URL 中。前端先携带 Bearer 调用
//     /terminal/session 换取一次性、30 秒有效的随机票据，WS 只用票据升级。
//   - 校验 Origin，阻止跨站 WebSocket 劫持。
//   - 终端以面板服务进程身份运行（通常为 root），因此提供配置开关，
//     并把会话开启/结束写入面板审计日志。
//   - 消息与帧长均设上限，空闲会话自动回收。

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	terminalMaxSessions = 8
	terminalTicketTTL   = 30 * time.Second
	terminalIdleTimeout = 2 * time.Hour
	terminalHeartbeat   = 25 * time.Second
	terminalMaxCols     = 500
	terminalMaxRows     = 200
	terminalExitGrace   = 3 * time.Second
)

var terminalSessionIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

type terminalTicket struct {
	expires time.Time
}

type terminalSession struct {
	id       string
	started  time.Time
	remote   string
	shell    string
	lastSeen atomic.Int64
}

// consumeTerminalTicket 校验并立即销毁票据（一次性）。
func (s *Server) consumeTerminalTicket(key string) bool {
	if key == "" {
		return false
	}
	s.termMu.Lock()
	defer s.termMu.Unlock()
	t, exists := s.termTickets[key]
	if !exists {
		return false
	}
	delete(s.termTickets, key)
	return time.Now().Before(t.expires)
}

func (s *Server) addTerminalSession(sess *terminalSession) bool {
	s.termMu.Lock()
	defer s.termMu.Unlock()
	if len(s.termSessions) >= terminalMaxSessions {
		return false
	}
	if s.termSessions == nil {
		s.termSessions = map[string]*terminalSession{}
	}
	s.termSessions[sess.id] = sess
	return true
}

func (s *Server) removeTerminalSession(id string) {
	s.termMu.Lock()
	defer s.termMu.Unlock()
	delete(s.termSessions, id)
}

func (s *Server) terminalSessionCount() int {
	s.termMu.Lock()
	defer s.termMu.Unlock()
	return len(s.termSessions)
}

func (s *Server) terminalEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.TerminalEnabled == nil || *s.cfg.TerminalEnabled
}

func (s *Server) terminalShell() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.TerminalShell
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func currentUserName() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	if v := os.Getenv("USER"); v != "" {
		return v
	}
	return "unknown"
}

func terminalEnv() []string {
	env := []string{
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"NANOPANEL_TERMINAL=1",
	}
	if os.Getenv("HOME") == "" {
		env = append(env, "HOME=/root")
	}
	if os.Getenv("LANG") == "" {
		env = append(env, "LANG=C.UTF-8")
	}
	if os.Getenv("PATH") == "" {
		env = append(env, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	}
	return env
}

func terminalCwd() string {
	home := os.Getenv("HOME")
	if home == "" {
		home = "/root"
	}
	if st, err := os.Stat(home); err == nil && st.IsDir() {
		return home
	}
	return "/"
}

// TerminalSettings 返回终端能力与当前配置。
func (s *Server) TerminalSettings(w http.ResponseWriter, r *http.Request) {
	shell := s.terminalShell()
	resolved, _ := detectShell(shell)
	ok(w, map[string]interface{}{
		"enabled":        s.terminalEnabled(),
		"shell":          shell,
		"shell_resolved": resolved,
		"platform":       runtime.GOOS,
		"pty":            runtime.GOOS == "linux",
		"user":           currentUserName(),
		"max_sessions":   terminalMaxSessions,
		"active":         s.terminalSessionCount(),
	})
}

type terminalSettingsReq struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Shell   *string `json:"shell,omitempty"`
}

// SaveTerminalSettings 修改终端开关与默认 shell，失败时回滚内存配置。
func (s *Server) SaveTerminalSettings(w http.ResponseWriter, r *http.Request) {
	var req terminalSettingsReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Shell != nil && *req.Shell != "" {
		v := *req.Shell
		if len(v) > 256 || v[0] != '/' {
			fail(w, http.StatusBadRequest, "shell 需为绝对路径")
			return
		}
		st, err := os.Stat(v)
		if err != nil || st.IsDir() || st.Mode()&0111 == 0 {
			fail(w, http.StatusBadRequest, "shell 不存在或不可执行")
			return
		}
	}
	s.mu.Lock()
	oldEnabled, oldShell := s.cfg.TerminalEnabled, s.cfg.TerminalShell
	if req.Enabled != nil {
		v := *req.Enabled
		s.cfg.TerminalEnabled = &v
	}
	if req.Shell != nil {
		s.cfg.TerminalShell = *req.Shell
	}
	if err := s.cfg.Save(s.configPath); err != nil {
		s.cfg.TerminalEnabled, s.cfg.TerminalShell = oldEnabled, oldShell
		s.mu.Unlock()
		fail(w, http.StatusInternalServerError, "保存配置失败: "+err.Error())
		return
	}
	s.mu.Unlock()
	s.audit(r, "settings", "修改终端设置", fmt.Sprintf("enabled=%v shell=%s", s.terminalEnabled(), s.terminalShell()))
	ok(w, map[string]interface{}{"enabled": s.terminalEnabled(), "shell": s.terminalShell()})
}

type terminalSessionReq struct {
	Cols int `json:"cols,omitempty"`
	Rows int `json:"rows,omitempty"`
}

// CreateTerminalSession 签发一次性 WebSocket 连接票据。
func (s *Server) CreateTerminalSession(w http.ResponseWriter, r *http.Request) {
	if !s.terminalEnabled() {
		fail(w, http.StatusForbidden, "内置终端已在设置中禁用")
		return
	}
	var req terminalSessionReq
	// 允许空请求体：前端不传尺寸时使用默认值。
	if r.ContentLength > 0 {
		if err := decodeJSON(w, r, &req); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	ticket, err := randomToken(24)
	if err != nil {
		fail(w, http.StatusInternalServerError, "生成会话票据失败")
		return
	}
	id, err := randomToken(9)
	if err != nil {
		fail(w, http.StatusInternalServerError, "生成会话编号失败")
		return
	}
	s.termMu.Lock()
	now := time.Now()
	for k, t := range s.termTickets {
		if now.After(t.expires) {
			delete(s.termTickets, k)
		}
	}
	if len(s.termSessions)+len(s.termTickets) >= terminalMaxSessions {
		s.termMu.Unlock()
		fail(w, http.StatusTooManyRequests, fmt.Sprintf("终端会话已达上限（%d）", terminalMaxSessions))
		return
	}
	if s.termTickets == nil {
		s.termTickets = map[string]terminalTicket{}
	}
	s.termTickets[ticket] = terminalTicket{expires: now.Add(terminalTicketTTL)}
	s.termMu.Unlock()

	ok(w, map[string]interface{}{
		"ticket":       ticket,
		"session":      id,
		"expires":      terminalTicketTTL / time.Second,
		"max_sessions": terminalMaxSessions,
	})
}

func clampTermSize(v, max, fallback int) uint16 {
	if v <= 0 || v > max {
		return uint16(fallback)
	}
	return uint16(v)
}

func wsJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

// TerminalWS 建立 WebSocket，并在 PTY 与浏览器之间双向转发字节流。
func (s *Server) TerminalWS(w http.ResponseWriter, r *http.Request) {
	if !s.terminalEnabled() {
		fail(w, http.StatusForbidden, "内置终端已在设置中禁用")
		return
	}
	if r.Header.Get("Upgrade") == "" {
		fail(w, http.StatusBadRequest, "需要 WebSocket 升级请求")
		return
	}
	if !s.consumeTerminalTicket(r.URL.Query().Get("ticket")) {
		fail(w, http.StatusUnauthorized, "终端票据无效或已过期，请重新打开终端")
		return
	}

	q := r.URL.Query()
	cols := clampTermSize(parseInt(q.Get("cols")), terminalMaxCols, 80)
	rows := clampTermSize(parseInt(q.Get("rows")), terminalMaxRows, 24)
	sessionID := q.Get("sid")
	if !terminalSessionIDRe.MatchString(sessionID) {
		sessionID, _ = randomToken(9)
	}

	ws, err := wsUpgrade(w, r)
	if err != nil {
		return
	}

	sess := &terminalSession{
		id:      sessionID,
		started: time.Now(),
		remote:  clientIP(r),
		shell:   s.terminalShell(),
	}
	sess.lastSeen.Store(time.Now().UnixNano())
	if !s.addTerminalSession(sess) {
		_ = ws.writeMessage(opText, wsJSON(map[string]interface{}{
			"type": "error", "message": "终端会话已达上限，请关闭其他会话后重试",
		}))
		_ = ws.writeControl(opClose, nil)
		ws.Close()
		return
	}
	defer s.removeTerminalSession(sess.id)

	proc, err := startTerminal(sess.shell, terminalCwd(), cols, rows, terminalEnv())
	if err != nil {
		_ = ws.writeMessage(opText, wsJSON(map[string]interface{}{
			"type": "error", "message": "启动终端失败: " + err.Error(),
		}))
		_ = ws.writeControl(opClose, nil)
		ws.Close()
		return
	}
	sh, _ := detectShell(sess.shell)
	_ = ws.writeMessage(opText, wsJSON(map[string]interface{}{
		"type": "ready", "session": sess.id, "shell": sh,
		"user": currentUserName(), "platform": runtime.GOOS,
		"pty": proc.IsPTY(), "cols": cols, "rows": rows,
	}))
	s.audit(r, "terminal", "终端会话开启", "shell="+sh)

	done := make(chan struct{})
	var once sync.Once
	closeAll := func() {
		once.Do(func() {
			close(done)
			ws.Close()
			proc.Close()
		})
	}
	defer closeAll()
	defer s.audit(r, "terminal", "终端会话结束", "shell="+sh)

	// 输出泵：PTY → WebSocket 二进制帧。
	// 注意：shell 退出时这里不能抢先关闭连接，否则退出码消息来不及送达；
	// 统一由下方的进程监视协程在排空输出后收尾。
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		buf := make([]byte, 32*1024)
		for {
			n, err := proc.Read(buf)
			if n > 0 {
				sess.lastSeen.Store(time.Now().UnixNano())
				if e := ws.writeMessage(opBinary, buf[:n]); e != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
	}()

	// 进程退出监视：等待 shell 结束、输出排空后推送退出码并收尾。
	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		code := 0
		if err := proc.Wait(); err != nil {
			code = 1
			var ee *exec.ExitError
			if errors.As(err, &ee) && ee.ExitCode() >= 0 {
				code = ee.ExitCode()
			}
		}
		select {
		case <-outputDone:
		case <-time.After(500 * time.Millisecond):
		}
		_ = ws.writeMessage(opText, wsJSON(map[string]interface{}{"type": "exit", "code": code}))
		_ = ws.writeControl(opClose, nil)
		// 给浏览器留出读取退出消息与 close 帧的窗口，再关闭底层连接。
		time.Sleep(80 * time.Millisecond)
		closeAll()
	}()

	// 心跳：维持 NAT 映射、回收僵死与空闲会话。
	go func() {
		ticker := time.NewTicker(terminalHeartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				last := time.Unix(0, sess.lastSeen.Load())
				if time.Since(last) > terminalIdleTimeout {
					_ = ws.writeMessage(opText, wsJSON(map[string]interface{}{
						"type": "error", "message": "会话空闲超时，已自动断开",
					}))
					closeAll()
					return
				}
				if err := ws.writeControl(opPing, []byte("np")); err != nil {
					closeAll()
					return
				}
			}
		}
	}()

	// 输入循环：WebSocket → PTY / 控制消息。
	for {
		op, data, err := ws.readMessage()
		if err != nil {
			break
		}
		sess.lastSeen.Store(time.Now().UnixNano())
		switch op {
		case opBinary:
			if len(data) > 0 {
				if _, err := proc.Write(data); err != nil {
					closeAll()
				}
			}
		case opText:
			var msg struct {
				Type string `json:"type"`
				Data string `json:"data,omitempty"`
				Cols int    `json:"cols,omitempty"`
				Rows int    `json:"rows,omitempty"`
			}
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			switch msg.Type {
			case "input":
				if msg.Data != "" {
					_, _ = proc.Write([]byte(msg.Data))
				}
			case "resize":
				c := clampTermSize(msg.Cols, terminalMaxCols, int(cols))
				rw := clampTermSize(msg.Rows, terminalMaxRows, int(rows))
				_ = proc.Resize(c, rw)
			case "ping":
				_ = ws.writeMessage(opText, wsJSON(map[string]interface{}{"type": "pong"}))
			case "close":
				closeAll()
			}
		}
		select {
		case <-done:
			goto finished
		default:
		}
	}

finished:
	closeAll()
	// 给 shell 一点时间响应 SIGHUP；超时则强杀整个进程组，避免遗留孤儿进程。
	select {
	case <-waitDone:
	case <-time.After(terminalExitGrace):
		proc.Kill()
	}
}

func parseInt(s string) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v
}
