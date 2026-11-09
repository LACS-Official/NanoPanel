package api

import (
	"fmt"
	"net/http"

	"nanopanel/internal/config"
	"nanopanel/internal/sysinfo"
)

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
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

	s.limiter.Reset(ip)
	s.audit(r, "login", "登录成功", "来源 IP "+ip)
	ok(w, map[string]string{
		"token":     s.auth.Issue(),
		"username":  req.Username,
		"ttl_hours": fmt.Sprintf("%d", s.cfg.TokenTTLHrs),
	})
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
