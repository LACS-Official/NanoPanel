package config

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHashPasswordAndVerify(t *testing.T) {
	h := HashPassword("correct horse battery")
	if !strings.HasPrefix(h, "pbkdf2-sha256$") {
		t.Fatalf("哈希格式不正确: %s", h)
	}
	if !VerifyPassword(h, "correct horse battery") {
		t.Fatal("正确密码校验失败")
	}
	if VerifyPassword(h, "wrong password") {
		t.Fatal("错误密码竟然通过校验")
	}
	if h == HashPassword("correct horse battery") {
		t.Fatal("相同密码两次哈希结果一致，盐未生效")
	}
}

func TestVerifyLegacyHash(t *testing.T) {
	sum := sha256.Sum256([]byte("legacy"))
	legacy := hex.EncodeToString(sum[:])
	if !VerifyPassword(legacy, "legacy") {
		t.Fatal("旧版 sha256 哈希应向后兼容")
	}
	if VerifyPassword(legacy, "other") {
		t.Fatal("旧版哈希错误密码不应通过")
	}
}

func TestVerifyMalformedHash(t *testing.T) {
	bad := []string{
		"", "abc", strings.Repeat("a", 64),
		"pbkdf2-sha256$notanumber$00$aabb",
		"pbkdf2-sha256$120000$zz$abcd",
		"pbkdf2-sha256$120000$00$zz",
		"pbkdf2-sha256$999999999$00$abcd",
	}
	for _, b := range bad {
		if VerifyPassword(b, "anything") {
			t.Errorf("畸形哈希不应通过: %q", b)
		}
	}
}

func TestRandomPassword(t *testing.T) {
	p, err := RandomPassword(16)
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 16 {
		t.Fatalf("期望长度 16，实际 %d", len(p))
	}
	for _, c := range p {
		if !strings.ContainsRune(string(passwordAlphabet), c) {
			t.Fatalf("密码含非法字符 %q", c)
		}
	}
	if strings.ContainsAny(p, "0O1lI") {
		t.Fatalf("密码含易混淆字符: %s", p)
	}
	p2, _ := RandomPassword(16)
	if p == p2 {
		t.Fatal("两次生成的密码相同")
	}
	short, _ := RandomPassword(3)
	if len(short) < MinPasswordLength {
		t.Fatal("短长度请求应被提升到最小长度")
	}
}

func TestDefaultHasNoUsablePassword(t *testing.T) {
	if Default().PasswordHash != "" {
		t.Fatal("默认配置不得包含可用密码，否则会出现固定弱口令")
	}
	if Default().Username != DefaultUsername {
		t.Fatal("默认用户名不正确")
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"默认值", func(*Config) {}, false},
		{"非法端口", func(c *Config) { c.Listen = ":0" }, true},
		{"越界端口", func(c *Config) { c.Listen = ":99999" }, true},
		{"缺少端口", func(c *Config) { c.Listen = "localhost" }, true},
		{"非法用户名", func(c *Config) { c.Username = "bad user!" }, true},
		{"空用户名", func(c *Config) { c.Username = "" }, true},
		{"TTL 为 0", func(c *Config) { c.TokenTTLHrs = 0 }, true},
		{"TTL 过大", func(c *Config) { c.TokenTTLHrs = 99999 }, true},
		{"非法服务名", func(c *Config) { c.Services = []ServiceDef{{Name: "bad;rm"}} }, true},
		{"空应用目录", func(c *Config) { c.AppsDir = "" }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Default()
			tc.mutate(c)
			err := c.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() = %v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	c := Default()
	c.Username = "tester"
	c.PasswordHash = HashPassword("secret123")
	c.Listen = ":9090"
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "tester" || got.Listen != ":9090" || got.Port() != 9090 {
		t.Fatalf("字段未正确持久化: %+v", got)
	}
	if !VerifyPassword(got.PasswordHash, "secret123") {
		t.Fatal("持久化的密码哈希无法校验")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Fatalf("配置文件权限应为 0600，实际 %o", perm)
		}
	}
}

func TestLoadFillsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	// 模拟早期版本配置：没有 username、secret 等字段。
	if err := os.WriteFile(path, []byte(`{"listen":":8080","password_hash":"`+
		HashPassword("pw")+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != DefaultUsername || got.TokenTTLHrs != 24 || got.Secret == "" || got.AppsDir == "" {
		t.Fatalf("缺省字段未补齐: %+v", got)
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err != nil {
		t.Fatalf("配置不存在应返回默认配置: %v", err)
	}
}

func TestCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := SaveCredentials(path, "admin", "first-pass"); err != nil {
		t.Fatal(err)
	}
	c, err := LoadCredentials(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Username != "admin" || c.Password != "first-pass" || c.CreatedAt == "" {
		t.Fatalf("凭据读取异常: %+v", c)
	}
	if err := UpdateCredentialsPassword(path, "second-pass"); err != nil {
		t.Fatal(err)
	}
	c, _ = LoadCredentials(path)
	if c.Password != "second-pass" || c.UpdatedAt == "" {
		t.Fatalf("凭据未更新: %+v", c)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(CredentialsPath(path))
		if info.Mode().Perm() != 0600 {
			t.Fatalf("凭据文件权限应为 0600，实际 %o", info.Mode().Perm())
		}
	}
	// 凭据文件被删除时应自动重建，用户名取配置文件中的值。
	cfg := Default()
	cfg.Username = "customuser"
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(CredentialsPath(path)); err != nil {
		t.Fatal(err)
	}
	if err := UpdateCredentialsPassword(path, "third-pass"); err != nil {
		t.Fatal(err)
	}
	c, _ = LoadCredentials(path)
	if c.Username != "customuser" || c.Password != "third-pass" {
		t.Fatalf("重建凭据异常: %+v", c)
	}
}
