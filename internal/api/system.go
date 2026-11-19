package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"nanopanel/internal/config"
	"nanopanel/internal/sysinfo"
	"nanopanel/internal/totp"
)

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// loginVerifyReq 是 TOTP 第二步验证请求。
type loginVerifyReq struct {
	Ticket string `json:"ticket"`
	Code   string `json:"code"`
}

// mfaTicketTTL 是 TOTP 预验证票据的有效期。
const mfaTicketTTL = 5 * time.Minute

// totpEnabled 读取 TOTP 是否启用（持锁）。
func (s *Server) totpEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.TOTPEnabled && s.cfg.TOTPSecret != ""
}

// issueMfaTicket 签发一次性预验证票据。
func (s *Server) issueMfaTicket(ip string) string {
	ticket := config.RandHex(24)
	s.mfaMu.Lock()
	if s.mfaPending == nil {
		s.mfaPending = make(map[string]mfaPending)
	}
	// 惰性清理过期票据
	now := time.Now()
	for k, v := range s.mfaPending {
		if now.After(v.expires) {
			delete(s.mfaPending, k)
		}
	}
	s.mfaPending[ticket] = mfaPending{ip: ip, expires: now.Add(mfaTicketTTL)}
	s.mfaMu.Unlock()
	return ticket
}

// consumeMfaTicket 原子地取出并删除票据，返回是否为该 IP 签发的有效票据。
func (s *Server) consumeMfaTicket(ticket, ip string) bool {
	s.mfaMu.Lock()
	defer s.mfaMu.Unlock()
	p, ok := s.mfaPending[ticket]
	if !ok || time.Now().After(p.expires) || p.ip != ip {
		delete(s.mfaPending, ticket)
		return false
	}
	delete(s.mfaPending, ticket)
	return true
}

func subtleEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// recoveryHash 计算恢复码的存储哈希（SHA256）。
func recoveryHash(code string) string {
	sum := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(code))))
	return hex.EncodeToString(sum[:])
}

// consumeRecoveryCode 校验并消耗一个恢复码（一次性）。命中时返回 true。
func (s *Server) consumeRecoveryCode(code string) bool {
	h := recoveryHash(code)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, stored := range s.cfg.TOTPRecovery {
		if subtleEqual(stored, h) {
			// 删除已使用的恢复码并持久化。
			s.cfg.TOTPRecovery = append(s.cfg.TOTPRecovery[:i], s.cfg.TOTPRecovery[i+1:]...)
			_ = s.cfg.Save(s.configPath)
			return true
		}
	}
	return false
}

type changePwdReq struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// Login 校验用户名与密码并签发 token。
func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if blocked, remain := s.limiter.Blocked(ip); blocked {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(remain.Seconds())+1))
		fail(w, http.StatusTooManyRequests,
			fmt.Sprintf("登录失败次数过多，请 %d 秒后重试", int(remain.Seconds())+1))
		return
	}

	var req loginReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Username != s.username() || !config.VerifyPassword(s.passwordHash(), req.Password) {
		if locked, _ := s.limiter.Fail(ip); locked {
			// 连续失败达到阈值触发锁定，推送安全告警（security 事件类别）。
			s.audit(r, "security", "登录失败锁定",
				fmt.Sprintf("来源 IP %s 连续登录失败，已临时锁定", ip))
			fail(w, http.StatusTooManyRequests, "登录失败次数过多，请稍后重试")
			return
		}
		fail(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}

	// 密码正确后，若启用了 TOTP 则进入第二步验证：签发一次性票据，
	// 不直接发放登录 token。票据 5 分钟有效、一次性使用。
	if s.totpEnabled() {
		ticket := s.issueMfaTicket(ip)
		ok(w, map[string]interface{}{
			"mfa_required": true,
			"ticket":       ticket,
		})
		return
	}

	s.limiter.Reset(ip)
	s.audit(r, "login", "登录成功", "来源 IP "+ip)
	ok(w, map[string]string{
		"token":     s.auth.Issue(),
		"username":  req.Username,
		"ttl_hours": fmt.Sprintf("%d", s.cfg.TokenTTLHrs),
	})
}

// VerifyLogin 完成 TOTP 第二步验证：校验票据 + 验证码/恢复码，签发正式登录 token。
func (s *Server) VerifyLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if blocked, remain := s.limiter.Blocked(ip); blocked {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(remain.Seconds())+1))
		fail(w, http.StatusTooManyRequests, "验证失败次数过多，请稍后重试")
		return
	}
	var req loginVerifyReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Ticket == "" || req.Code == "" {
		fail(w, http.StatusBadRequest, "票据与验证码不能为空")
		return
	}
	// 票据一次性消费：无论验证成功与否，用过的票据立即作废，防止爆破。
	if !s.consumeMfaTicket(req.Ticket, ip) {
		if locked, _ := s.limiter.Fail(ip); locked {
			s.audit(r, "security", "TOTP 验证失败锁定",
				fmt.Sprintf("来源 IP %s 连续验证失败，已临时锁定", ip))
		}
		fail(w, http.StatusUnauthorized, "验证票据无效或已过期，请重新登录")
		return
	}

	secret := ""
	s.mu.RLock()
	secret = s.cfg.TOTPSecret
	username := s.cfg.Username
	ttl := s.cfg.TokenTTLHrs
	s.mu.RUnlock()

	verified := totp.Validate(secret, req.Code, time.Now())
	usingRecovery := false
	if !verified {
		verified = s.consumeRecoveryCode(req.Code)
		usingRecovery = verified
	}
	if !verified {
		if locked, _ := s.limiter.Fail(ip); locked {
			s.audit(r, "security", "TOTP 验证失败锁定",
				fmt.Sprintf("来源 IP %s 连续验证失败，已临时锁定", ip))
		}
		fail(w, http.StatusUnauthorized, "验证码错误或已使用")
		return
	}

	s.limiter.Reset(ip)
	detail := "验证器验证码"
	if usingRecovery {
		detail = "一次性恢复码"
	}
	s.audit(r, "login", "两步验证登录成功", "来源 IP "+ip+" · "+detail)
	ok(w, map[string]string{
		"token":     s.auth.Issue(),
		"username":  username,
		"ttl_hours": fmt.Sprintf("%d", ttl),
	})
}

// CreateReadonlyToken 签发一个只读 API token，仅可用于 GET/HEAD 接口。
// token 无状态、不落盘，仅在响应中返回一次，调用方需自行保存。
func (s *Server) CreateReadonlyToken(w http.ResponseWriter, r *http.Request) {
	tok := s.auth.IssueReadonly()
	s.audit(r, "security", "生成只读 API token", "仅可用于 GET 类接口")
	ok(w, map[string]string{
		"token":     tok,
		"ttl_hours": fmt.Sprintf("%d", s.cfg.TokenTTLHrs),
		"scope":     "ro",
	})
}

// RevokeToken 注销指定 token（用于吊销只读 token）。
func (s *Server) RevokeToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Token == "" {
		fail(w, http.StatusBadRequest, "token 不能为空")
		return
	}
	// 不允许吊销自己当前正在使用的 token（应走 logout）。
	if req.Token == s.bearer(r) {
		fail(w, http.StatusBadRequest, "不能吊销当前登录 token，请使用退出登录")
		return
	}
	s.auth.Revoke(req.Token)
	s.audit(r, "security", "吊销 API token", "token 已加入撤销列表")
	ok(w, map[string]string{"msg": "已吊销"})
}

// Logout 注销当前 token。
func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	s.auth.Revoke(s.bearer(r))
	ok(w, map[string]string{"msg": "已退出"})
}

// Overview 返回系统概览。
func (s *Server) Overview(w http.ResponseWriter, r *http.Request) {
	ok(w, sysinfo.GetOverview())
}

// ChangePassword 修改登录密码并持久化，同时吊销已有登录态。
func (s *Server) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var req changePwdReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.NewPassword) < config.MinPasswordLength {
		fail(w, http.StatusBadRequest,
			fmt.Sprintf("新密码至少 %d 位", config.MinPasswordLength))
		return
	}

	s.mu.Lock()
	if !config.VerifyPassword(s.cfg.PasswordHash, req.OldPassword) {
		s.mu.Unlock()
		fail(w, http.StatusUnauthorized, "原密码错误")
		return
	}
	old := s.cfg.PasswordHash
	oldSecret := s.cfg.Secret
	s.cfg.PasswordHash = config.HashPassword(req.NewPassword)
	// 轮换签名密钥并随配置一起持久化：既吊销全部历史 token，又保证重启后旧 token 无法复活。
	newSecret := config.RandHex(32)
	s.cfg.Secret = newSecret
	if err := s.cfg.Save(s.configPath); err != nil {
		s.cfg.PasswordHash = old // 回滚内存状态，避免与磁盘不一致
		s.cfg.Secret = oldSecret
		s.mu.Unlock()
		fail(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	s.mu.Unlock()
	s.auth.RotateSecret(newSecret)

	// 凭据文件不存在时静默跳过，不影响改密本身。
	if err := config.UpdateCredentialsPassword(s.configPath, req.NewPassword); err != nil {
		fail(w, http.StatusInternalServerError, "凭据同步失败: "+err.Error())
		return
	}

	s.audit(r, "settings", "修改登录密码", "签名密钥已轮换，全部历史登录态失效")
	ok(w, map[string]string{
		"msg":   "密码已修改，其他登录态已失效",
		"token": s.auth.Issue(),
	})
}
