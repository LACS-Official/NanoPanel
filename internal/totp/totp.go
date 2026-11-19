// Package totp 实现 RFC 6238 基于时间的一次性密码（TOTP），仅用 Go 标准库。
// 用于面板登录的第二步验证：HMAC-SHA1 / 6 位 / 30 秒步长 / ±1 时间窗口容错。
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	// Period 是每个验证码的有效期（秒）。
	Period = 30
	// Digits 是验证码位数。
	Digits = 6
	// Skew 是允许的时间步容错（前后各 1 步，即 ±30 秒）。
	Skew = 1
	// SecretBytes 是生成密钥的随机字节数（160 位，兼容主流验证器 App）。
	SecretBytes = 20
)

var b32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateSecret 生成一个新的 Base32（无填充）密钥。
func GenerateSecret() (string, error) {
	b := make([]byte, SecretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成 TOTP 密钥: %w", err)
	}
	return b32NoPad.EncodeToString(b), nil
}

// NormalizeSecret 清洗用户/配置中的密钥：去空白、转大写、去掉填充符。
func NormalizeSecret(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "")
	return strings.TrimRight(s, "=")
}

// codeAt 计算给定时间步的 6 位验证码。
func codeAt(secret []byte, counter uint64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0F
	bin := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7FFFFFFF
	return fmt.Sprintf("%06d", bin%1000000)
}

// codeAtTime 计算 now 对应时间步的验证码。
func codeAtTime(secret []byte, now time.Time) string {
	counter := uint64(now.Unix() / Period)
	return codeAt(secret, counter)
}

// Code 返回 secret 在 now 时刻的 6 位验证码（主时间步，无容错）。
// 供测试与需要自行生成验证码的场景使用。
func Code(secret string, now time.Time) (string, error) {
	key, err := b32NoPad.DecodeString(NormalizeSecret(secret))
	if err != nil || len(key) == 0 {
		return "", fmt.Errorf("无效的 Base32 密钥")
	}
	return codeAtTime(key, now), nil
}

// Validate 校验 6 位验证码，允许 ±1 时间步容错。code 中的空格与连字符会被忽略。
func Validate(secret, code string, now time.Time) bool {
	code = strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(code))
	if len(code) != Digits {
		return false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return false
		}
	}
	key, err := b32NoPad.DecodeString(NormalizeSecret(secret))
	if err != nil || len(key) == 0 {
		return false
	}
	for skew := -Skew; skew <= Skew; skew++ {
		t := now.Add(time.Duration(skew*Period) * time.Second)
		if subtle.ConstantTimeCompare([]byte(codeAtTime(key, t)), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// ProvisioningURI 生成可扫码添加验证器的 otpauth URI。
// issuer / account 会被正确转义。
func ProvisioningURI(secret, issuer, account string) string {
	q := url.Values{}
	q.Set("secret", NormalizeSecret(secret))
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprintf("%d", Digits))
	q.Set("period", fmt.Sprintf("%d", Period))
	label := issuer + ":" + account
	return "otpauth://totp/" + url.PathEscape(label) + "?" + q.Encode()
}

// GenerateRecoveryCodes 生成 n 个一次性恢复码，每个为 10 位大写十六进制。
// 返回的明文用于展示给用户；调用方应存储其 SHA256 哈希。
func GenerateRecoveryCodes(n int) ([]string, error) {
	if n < 1 {
		n = 10
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		b := make([]byte, 5)
		if _, err := rand.Read(b); err != nil {
			return nil, fmt.Errorf("生成恢复码: %w", err)
		}
		out = append(out, strings.ToUpper(fmt.Sprintf("%X", b)))
	}
	return out, nil
}
