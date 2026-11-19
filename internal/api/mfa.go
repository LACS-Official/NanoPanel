package api

// TOTP 双因素认证的设置接口：生成密钥、展示 otpauth URI、启用/关闭、
// 管理一次性恢复码。密钥以 Base32 明文存于配置文件（文件权限 0600），
// 恢复码仅存 SHA256 哈希，明文只在生成/重置时返回一次。

import (
	"fmt"
	"net/http"
	"time"

	"nanopanel/internal/totp"
)

// GetMFASettings 返回当前 TOTP 状态与（若已生成但未启用）待确认密钥的 otpauth URI。
func (s *Server) GetMFASettings(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	enabled := s.cfg.TOTPEnabled && s.cfg.TOTPSecret != ""
	secret := s.cfg.TOTPSecret
	recoveryLeft := len(s.cfg.TOTPRecovery)
	username := s.cfg.Username
	s.mu.RUnlock()

	resp := map[string]interface{}{
		"enabled":       enabled,
		"has_secret":    secret != "",
		"recovery_left": recoveryLeft,
	}
	if secret != "" {
		resp["provisioning_uri"] = totp.ProvisioningURI(secret, "NanoPanel", username)
	}
	ok(w, resp)
}

// SetupMFA 生成新的 Base32 密钥并暂存到配置（尚未启用）。返回密钥明文与 otpauth URI，
// 供前端展示二维码/手动输入。用户须用 GetMFASettings 展示，再用 EnableMFA 验证启用。
func (s *Server) SetupMFA(w http.ResponseWriter, r *http.Request) {
	secret, err := totp.GenerateSecret()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.mu.Lock()
	old := s.cfg.TOTPSecret
	s.cfg.TOTPSecret = secret
	if err := s.cfg.Save(s.configPath); err != nil {
		s.cfg.TOTPSecret = old
		s.mu.Unlock()
		fail(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	username := s.cfg.Username
	s.mu.Unlock()

	s.audit(r, "settings", "生成 TOTP 密钥", "等待验证码确认启用")
	ok(w, map[string]interface{}{
		"secret":           secret,
		"provisioning_uri": totp.ProvisioningURI(secret, "NanoPanel", username),
		"period":           totp.Period,
		"digits":           totp.Digits,
	})
}

// mfaEnableReq 携带启用/重置恢复码时用于确认身份的验证码。
type mfaEnableReq struct {
	Code string `json:"code"`
}

// EnableMFA 用当前密钥校验验证码，通过后启用 TOTP 并生成一次性恢复码。
func (s *Server) EnableMFA(w http.ResponseWriter, r *http.Request) {
	var req mfaEnableReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.RLock()
	secret := s.cfg.TOTPSecret
	s.mu.RUnlock()
	if secret == "" {
		fail(w, http.StatusBadRequest, "请先生成密钥")
		return
	}
	if !totp.Validate(secret, req.Code, time.Now()) {
		fail(w, http.StatusBadRequest, "验证码不正确")
		return
	}
	codes, err := totp.GenerateRecoveryCodes(10)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	hashes := make([]string, 0, len(codes))
	for _, c := range codes {
		hashes = append(hashes, recoveryHash(c))
	}
	s.mu.Lock()
	oldEnabled := s.cfg.TOTPEnabled
	oldRecovery := s.cfg.TOTPRecovery
	s.cfg.TOTPEnabled = true
	s.cfg.TOTPRecovery = hashes
	if err := s.cfg.Save(s.configPath); err != nil {
		s.cfg.TOTPEnabled, s.cfg.TOTPRecovery = oldEnabled, oldRecovery
		s.mu.Unlock()
		fail(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	s.mu.Unlock()

	s.audit(r, "settings", "启用 TOTP 两步验证", "已生成 10 个一次性恢复码")
	ok(w, map[string]interface{}{"enabled": true, "recovery_codes": codes})
}

// DisableMFA 关闭 TOTP 并清除密钥与恢复码。需提供当前验证码确认身份。
func (s *Server) DisableMFA(w http.ResponseWriter, r *http.Request) {
	var req mfaEnableReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.RLock()
	secret := s.cfg.TOTPSecret
	s.mu.RUnlock()
	if secret != "" && !totp.Validate(secret, req.Code, time.Now()) {
		// 允许用恢复码关闭，避免验证器丢失后彻底锁死。
		if !s.consumeRecoveryCode(req.Code) {
			fail(w, http.StatusBadRequest, "验证码不正确")
			return
		}
	}
	s.mu.Lock()
	old := struct {
		enabled  bool
		secret   string
		recovery []string
	}{s.cfg.TOTPEnabled, s.cfg.TOTPSecret, s.cfg.TOTPRecovery}
	s.cfg.TOTPEnabled = false
	s.cfg.TOTPSecret = ""
	s.cfg.TOTPRecovery = nil
	if err := s.cfg.Save(s.configPath); err != nil {
		s.cfg.TOTPEnabled, s.cfg.TOTPSecret, s.cfg.TOTPRecovery = old.enabled, old.secret, old.recovery
		s.mu.Unlock()
		fail(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	s.mu.Unlock()

	s.audit(r, "settings", "关闭 TOTP 两步验证", fmt.Sprintf("来源 IP %s", clientIP(r)))
	ok(w, map[string]interface{}{"enabled": false})
}

// ResetRecoveryCodes 重新生成恢复码（会使旧恢复码全部失效），需当前验证码确认。
func (s *Server) ResetRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	var req mfaEnableReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.RLock()
	secret := s.cfg.TOTPSecret
	s.mu.RUnlock()
	if secret == "" {
		fail(w, http.StatusBadRequest, "尚未配置 TOTP")
		return
	}
	if !totp.Validate(secret, req.Code, time.Now()) {
		fail(w, http.StatusBadRequest, "验证码不正确")
		return
	}
	codes, err := totp.GenerateRecoveryCodes(10)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	hashes := make([]string, 0, len(codes))
	for _, c := range codes {
		hashes = append(hashes, recoveryHash(c))
	}
	s.mu.Lock()
	old := s.cfg.TOTPRecovery
	s.cfg.TOTPRecovery = hashes
	if err := s.cfg.Save(s.configPath); err != nil {
		s.cfg.TOTPRecovery = old
		s.mu.Unlock()
		fail(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	s.mu.Unlock()

	s.audit(r, "settings", "重置 TOTP 恢复码", "旧恢复码已全部失效")
	ok(w, map[string]interface{}{"recovery_codes": codes})
}
