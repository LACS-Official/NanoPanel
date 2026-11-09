package auth

import (
	"strings"
	"testing"
	"time"
)

func TestTokenLifecycle(t *testing.T) {
	m := NewManager(time.Hour, "test-secret")
	tok := m.Issue()
	if !strings.HasPrefix(tok, "v1.") {
		t.Fatalf("token 应带 v1 前缀: %s", tok)
	}
	if n := strings.Count(tok, "."); n != 3 {
		t.Fatalf("token 应为 4 段点分隔，实际 %d 段", n+1)
	}
	if !m.Valid(tok) {
		t.Fatal("新签发的 token 应有效")
	}
	m.Revoke(tok)
	if m.Valid(tok) {
		t.Fatal("注销后的 token 仍有效")
	}
	// 撤销后重新登录签发的 token 不受影响
	if tok2 := m.Issue(); !m.Valid(tok2) {
		t.Fatal("注销单个 token 不应影响新签发的 token")
	}
}

func TestTokenExpiry(t *testing.T) {
	m := NewManager(10*time.Millisecond, "s")
	tok := m.Issue()
	time.Sleep(30 * time.Millisecond)
	if m.Valid(tok) {
		t.Fatal("过期 token 应失效")
	}
}

// 持久化登录的核心语义：同一 secret 重建的 Manager 能验证旧 token。
func TestTokenSurvivesRestart(t *testing.T) {
	m1 := NewManager(time.Hour, "persisted-secret")
	tok := m1.Issue()
	m2 := NewManager(time.Hour, "persisted-secret")
	if !m2.Valid(tok) {
		t.Fatal("面板重启后（同 secret）token 应保持有效")
	}
	m3 := NewManager(time.Hour, "different-secret")
	if m3.Valid(tok) {
		t.Fatal("不同 secret 不应验证旧 token")
	}
}

func TestRotateSecretRevokesAll(t *testing.T) {
	m := NewManager(time.Hour, "s1")
	a, b := m.Issue(), m.Issue()
	m.RevokeAll()
	if m.Valid(a) || m.Valid(b) {
		t.Fatal("轮换密钥后历史 token 应全部失效")
	}
	fresh := m.Issue()
	if !m.Valid(fresh) {
		t.Fatal("轮换后新签发的 token 应有效")
	}
}

func TestMalformedTokens(t *testing.T) {
	m := NewManager(time.Hour, "s")
	good := m.Issue()
	bad := []string{
		"",
		"v1.deadbeef.0000017ffffff.0011",
		"v2." + strings.TrimPrefix(good, "v1."),
		"v1.00000000000000000000000000000000.0000000000000000." +
			strings.Split(good, ".")[3], // 借用签名但 nonce 不匹配
		"v1.zzzz.0000000000000000." + strings.Split(good, ".")[3],
		good[:len(good)-2] + "ff", // 篡改签名
	}
	for _, tok := range bad {
		if m.Valid(tok) {
			t.Fatalf("畸形 token 不应通过校验: %q", tok)
		}
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
