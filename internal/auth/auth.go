// Package auth 管理登录态 token 与登录失败限速。
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Manager 管理内存中的登录 token。
// 过期项在 Issue 时惰性清理，不启动常驻 goroutine，便于测试与优雅退出。
type Manager struct {
	mu     sync.Mutex
	tokens map[string]time.Time
	ttl    time.Duration
}

// NewManager 创建 token 管理器。
func NewManager(ttl time.Duration) *Manager {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Manager{tokens: make(map[string]time.Time), ttl: ttl}
}

// Issue 签发新 token。
func (m *Manager) Issue() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand 不可用: " + err.Error())
	}
	t := hex.EncodeToString(b)
	now := time.Now()
	m.mu.Lock()
	for k, exp := range m.tokens {
		if now.After(exp) {
			delete(m.tokens, k)
		}
	}
	m.tokens[t] = now.Add(m.ttl)
	m.mu.Unlock()
	return t
}

// Valid 校验 token 是否有效。
func (m *Manager) Valid(t string) bool {
	if t == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.tokens[t]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(m.tokens, t)
		return false
	}
	return true
}

// Revoke 注销单个 token。
func (m *Manager) Revoke(t string) {
	m.mu.Lock()
	delete(m.tokens, t)
	m.mu.Unlock()
}

// RevokeAll 注销全部 token（改密后调用）。
func (m *Manager) RevokeAll() {
	m.mu.Lock()
	m.tokens = make(map[string]time.Time)
	m.mu.Unlock()
}

// Count 返回当前有效 token 数量，主要用于测试。
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tokens)
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
