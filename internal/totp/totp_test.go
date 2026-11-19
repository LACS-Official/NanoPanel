package totp

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"
)

// RFC 6238 附录 B 的 SHA1 测试向量（取后 6 位）。
func TestRFC6238Vectors(t *testing.T) {
	secret := []byte("12345678901234567890") // ASCII，直接作为 HMAC 密钥
	cases := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	}
	for _, c := range cases {
		got := codeAt(secret, uint64(c.unix/Period))
		if got != c.want {
			t.Errorf("unix=%d: got %s want %s", c.unix, got, c.want)
		}
	}
}

func TestValidateCurrentAndSkew(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	now := time.Unix(1700000000, 0)
	code := codeAtTime(mustDecode(secret), now)
	if !Validate(secret, code, now) {
		t.Fatal("当前时间步验证码应通过")
	}
	if !Validate(secret, code, now.Add(25*time.Second)) {
		t.Fatal("+25 秒内验证码应通过（±1 步容错）")
	}
	if !Validate(strings.ToLower(secret), code, now) {
		t.Fatal("小写密钥应被规范化后通过")
	}
	if Validate(secret, "000000", now) && code != "000000" {
		t.Fatal("错误验证码不应通过")
	}
	if Validate(secret, "12345", now) {
		t.Fatal("位数不足不应通过")
	}
}

func TestGenerateSecretAndCodes(t *testing.T) {
	s1, err := GenerateSecret()
	if err != nil || len(s1) < 32 {
		t.Fatalf("密钥生成失败: %v (len=%d)", err, len(s1))
	}
	s2, _ := GenerateSecret()
	if s1 == s2 {
		t.Fatal("两次生成的密钥不应相同")
	}
	codes, err := GenerateRecoveryCodes(10)
	if err != nil || len(codes) != 10 {
		t.Fatalf("恢复码生成失败: %v", err)
	}
	for _, c := range codes {
		if len(c) != 10 {
			t.Errorf("恢复码应为 10 位，实际 %q", c)
		}
	}
}

func TestProvisioningURI(t *testing.T) {
	uri := ProvisioningURI("JBSWY3DPEHPK3PXP", "NanoPanel", "admin@host")
	if !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Fatalf("URI 前缀错误: %s", uri)
	}
	for _, part := range []string{"secret=JBSWY3DPEHPK3PXP", "issuer=NanoPanel", "digits=6", "period=30"} {
		if !strings.Contains(uri, part) {
			t.Errorf("URI 缺少 %q: %s", part, uri)
		}
	}
}

func mustDecode(s string) []byte {
	b, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
	return b
}
