// Package auth 管理登录态 token 与登录失败限速。
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// token 采用无状态 HMAC 签名方案：
//
//	v1.<rand hex>.<expiry unix hex>.<hmac hex>           读写 token（兼容旧版）
//	v2.<rand hex>.<expiry unix hex>.<scope>.<hmac hex>   携带权限声明的 token
//
// scope 取值：rw（读写）/ ro（只读）。v1 恒定视为 rw。
// 签名密钥来自配置文件持久化的 secret，因此面板重启后已签发的 token 依然有效
// （持久化登录）；修改密码时轮换 secret 即可吊销全部历史 token；单个登出写入
// 内存撤销列表，条目在过期后惰性清理。
const (
	tokenPrefix  = "v1"
	tokenPrefix2 = "v2"
	ScopeRW      = "rw"
	ScopeRO      = "ro"
)

// Manager 管理无状态 HMAC token 的签发、校验与撤销。
type Manager struct {
	mu      sync.Mutex
	secret  []byte
	ttl     time.Duration
	revoked map[string]int64 // 已撤销 token -> 过期时间戳（秒）
}

// NewManager 创建 token 管理器。secret 为签名密钥，来自持久化配置；
// 传入空串时自动生成（仅应出现在测试中，生产路径必须传配置 secret）。
func NewManager(ttl time.Duration, secret string) *Manager {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	if secret == "" {
		secret = hex.EncodeToString(randomBytes(32))
	}
	return &Manager{secret: []byte(secret), ttl: ttl, revoked: make(map[string]int64)}
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand 不可用: " + err.Error())
	}
	return b
}

// sign 对 v1（无 scope）与 v2（带 scope）分别签名。scope 纳入 HMAC 输入，
// 防止把只读 token 篡改为读写 token。
func (m *Manager) sign(nonce []byte, expiry int64, scope string) []byte {
	mac := hmac.New(sha256.New, m.secret)
	if scope == "" {
		mac.Write([]byte(tokenPrefix))
	} else {
		mac.Write([]byte(tokenPrefix2))
		mac.Write([]byte(scope))
	}
	mac.Write(nonce)
	var exp [8]byte
	binary.BigEndian.PutUint64(exp[:], uint64(expiry))
	mac.Write(exp[:])
	return mac.Sum(nil)
}

// Issue 签发读写 token，同时惰性清理过期的撤销记录。
func (m *Manager) Issue() string { return m.issueWithScope("") }

// IssueReadonly 签发只读 token：仅可用于 GET/HEAD 类接口。
func (m *Manager) IssueReadonly() string { return m.issueWithScope(ScopeRO) }

func (m *Manager) issueWithScope(scope string) string {
	nonce := randomBytes(16)
	expiry := time.Now().Add(m.ttl).UnixMilli()
	sig := m.sign(nonce, expiry, scope)
	expHex := hex.EncodeToString(binary.BigEndian.AppendUint64(nil, uint64(expiry)))
	var tok string
	if scope == "" {
		tok = tokenPrefix + "." + hex.EncodeToString(nonce) + "." + expHex + "." + hex.EncodeToString(sig)
	} else {
		tok = tokenPrefix2 + "." + hex.EncodeToString(nonce) + "." + expHex + "." +
			scope + "." + hex.EncodeToString(sig)
	}

	now := time.Now().UnixMilli()
	m.mu.Lock()
	for k, exp := range m.revoked {
		if now > exp {
			delete(m.revoked, k)
		}
	}
	m.mu.Unlock()
	return tok
}

// parseToken 拆解并验签，返回过期时间戳、nonce 与 scope（v1 为 rw）。
// 任何不合法输入返回 ok=false。
func (m *Manager) parseToken(tok string) (int64, []byte, string, bool) {
	parts := strings.Split(tok, ".")
	if len(parts) == 4 && parts[0] == tokenPrefix {
		nonce, err := hex.DecodeString(parts[1])
		if err != nil || len(nonce) != 16 {
			return 0, nil, "", false
		}
		expRaw, err := hex.DecodeString(parts[2])
		if err != nil || len(expRaw) != 8 {
			return 0, nil, "", false
		}
		expiry := int64(binary.BigEndian.Uint64(expRaw))
		sig, err := hex.DecodeString(parts[3])
		if err != nil {
			return 0, nil, "", false
		}
		if !hmac.Equal(sig, m.sign(nonce, expiry, "")) {
			return 0, nil, "", false
		}
		return expiry, nonce, ScopeRW, true
	}
	if len(parts) == 5 && parts[0] == tokenPrefix2 {
		nonce, err := hex.DecodeString(parts[1])
		if err != nil || len(nonce) != 16 {
			return 0, nil, "", false
		}
		expRaw, err := hex.DecodeString(parts[2])
		if err != nil || len(expRaw) != 8 {
			return 0, nil, "", false
		}
		expiry := int64(binary.BigEndian.Uint64(expRaw))
		scope := parts[3]
		if scope != ScopeRW && scope != ScopeRO {
			return 0, nil, "", false
		}
		sig, err := hex.DecodeString(parts[4])
		if err != nil {
			return 0, nil, "", false
		}
		if !hmac.Equal(sig, m.sign(nonce, expiry, scope)) {
			return 0, nil, "", false
		}
		return expiry, nonce, scope, true
	}
	return 0, nil, "", false
}

// Valid 校验 token 是否有效。
func (m *Manager) Valid(t string) bool {
	if t == "" {
		return false
	}
	expiry, _, _, parsed := m.parseToken(t)
	if !parsed || time.Now().UnixMilli() > expiry {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, revoked := m.revoked[t]; revoked {
		return false
	}
	return true
}

// Scope 返回 token 的权限声明（rw / ro）；无效 token 返回空串。
func (m *Manager) Scope(t string) string {
	_, _, scope, parsed := m.parseToken(t)
	if !parsed {
		return ""
	}
	return scope
}

// Revoke 注销单个 token（登出）。
func (m *Manager) Revoke(t string) {
	m.mu.Lock()
	if expiry, _, _, parsed := m.parseToken(t); parsed {
		m.revoked[t] = expiry
	}
	m.mu.Unlock()
}

// RevokeAll 轮换签名密钥并清空撤销列表，历史 token 全部失效（改密后调用）。
func (m *Manager) RevokeAll() {
	m.RotateSecret(hex.EncodeToString(randomBytes(32)))
}

// RotateSecret 轮换签名密钥：所有按旧密钥签发的 token 立即失效。
// 调用方负责把新 secret 持久化到配置，否则重启后会退回旧密钥。
func (m *Manager) RotateSecret(newSecret string) {
	if newSecret == "" {
		newSecret = hex.EncodeToString(randomBytes(32))
	}
	m.mu.Lock()
	m.secret = []byte(newSecret)
	m.revoked = make(map[string]int64)
	m.mu.Unlock()
}

// Secret 返回当前签名密钥，供调用方在改密流程中持久化到配置文件。
func (m *Manager) Secret() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return string(m.secret)
}

// Count 返回撤销列表条目数，主要用于测试。
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.revoked)
}

// LoginLimiter 按客户端 IP 限制连续登录失败次数，缓解暴力破解。
type LoginLimiter struct {
	mu          sync.Mutex
	entries     map[string]*attempt
	maxFailures int
	window      time.Duration
	lockFor     time.Duration
}

type attempt struct {
	failures     int
	first        time.Time
	blockedUntil time.Time
}

// NewLoginLimiter 创建限速器。maxFailures 次失败后锁定 lockFor 时长。
func NewLoginLimiter(maxFailures int, window, lockFor time.Duration) *LoginLimiter {
	if maxFailures < 1 {
		maxFailures = 5
	}
	return &LoginLimiter{
		entries:     make(map[string]*attempt),
		maxFailures: maxFailures,
		window:      window,
		lockFor:     lockFor,
	}
}

// Blocked 返回该 key 是否处于锁定期，以及剩余锁定时间。
func (l *LoginLimiter) Blocked(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(time.Now())
	a := l.entries[key]
	if a == nil {
		return false, 0
	}
	if remain := time.Until(a.blockedUntil); remain > 0 {
		return true, remain
	}
	return false, 0
}

// Fail 记录一次失败，返回是否触发锁定及剩余时间。
func (l *LoginLimiter) Fail(key string) (bool, time.Duration) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)
	a := l.entries[key]
	if a == nil || now.Sub(a.first) > l.window {
		a = &attempt{first: now}
		l.entries[key] = a
	}
	a.failures++
	if a.failures >= l.maxFailures {
		a.blockedUntil = now.Add(l.lockFor)
		return true, l.lockFor
	}
	return false, 0
}

// Reset 登录成功后清除计数。
func (l *LoginLimiter) Reset(key string) {
	l.mu.Lock()
	delete(l.entries, key)
	l.mu.Unlock()
}

func (l *LoginLimiter) sweep(now time.Time) {
	for k, a := range l.entries {
		if now.After(a.blockedUntil) && now.Sub(a.first) > l.window {
			delete(l.entries, k)
		}
	}
}
