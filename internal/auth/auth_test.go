package auth

import (
	"testing"
	"time"
)

func TestTokenLifecycle(t *testing.T) {
	m := NewManager(time.Hour)
	tok := m.Issue()
	if len(tok) != 64 {
		t.Fatalf("token 长度异常: %d", len(tok))
	}
	if !m.Valid(tok) {
		t.Fatal("新签发的 token 应有效")
	}
	if m.Count() != 1 {
		t.Fatalf("期望 1 个 token，实际 %d", m.Count())
	}
	m.Revoke(tok)
	if m.Valid(tok) {
		t.Fatal("注销后的 token 仍有效")
	}
}

func TestTokenExpiry(t *testing.T) {
	m := NewManager(10 * time.Millisecond)
	tok := m.Issue()
	time.Sleep(30 * time.Millisecond)
	if m.Valid(tok) {
		t.Fatal("过期 token 应失效")
	}
}

func TestIssueSweepsExpiredTokens(t *testing.T) {
	m := NewManager(10 * time.Millisecond)
	_ = m.Issue()
	_ = m.Issue()
	time.Sleep(30 * time.Millisecond)
	_ = m.Issue() // Issue 时惰性清理
	if m.Count() != 1 {
		t.Fatalf("过期 token 未被清理，剩余 %d", m.Count())
	}
}

func TestRevokeAll(t *testing.T) {
	m := NewManager(time.Hour)
	a, b := m.Issue(), m.Issue()
	m.RevokeAll()
	if m.Valid(a) || m.Valid(b) || m.Count() != 0 {
		t.Fatal("RevokeAll 未清空所有登录态")
	}
}

func TestLoginLimiter(t *testing.T) {
	l := NewLoginLimiter(3, time.Minute, time.Minute)
	if blocked, _ := l.Blocked("1.2.3.4"); blocked {
		t.Fatal("新 IP 不应被锁定")
	}
	for i := 0; i < 2; i++ {
		locked, _ := l.Fail("1.2.3.4")
		if locked {
			t.Fatalf("第 %d 次失败不应触发锁定", i+1)
		}
	}
	locked, remain := l.Fail("1.2.3.4")
	if !locked || remain <= 0 {
		t.Fatal("达到阈值后应触发锁定")
	}
	if blocked, _ := l.Blocked("1.2.3.4"); !blocked {
		t.Fatal("锁定应能被 Blocked 检测到")
	}
	if blocked, _ := l.Blocked("5.6.7.8"); blocked {
		t.Fatal("不同 IP 不应互相影响")
	}
	l.Reset("1.2.3.4")
	if blocked, _ := l.Blocked("1.2.3.4"); blocked {
		t.Fatal("Reset 后应解除锁定")
	}
}

func TestLoginLimiterWindowExpiry(t *testing.T) {
	l := NewLoginLimiter(2, 20*time.Millisecond, 20*time.Millisecond)
	_, _ = l.Fail("ip")
	time.Sleep(40 * time.Millisecond)
	locked, _ := l.Fail("ip")
	if locked {
		t.Fatal("窗口过期后计数应重置，不应立即锁定")
	}
}
