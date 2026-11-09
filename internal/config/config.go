// Package config 负责面板配置的读写、校验以及密码哈希。
package config

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	// DefaultUsername 是兼容旧配置时使用的默认登录用户名。
	DefaultUsername = "admin"
	// MinPasswordLength 是面板允许的最短密码长度。
	MinPasswordLength = 8

	hashPrefix       = "pbkdf2-sha256"
	pbkdf2Iterations = 120000
	keyLength        = 32
	saltLength       = 16
)

var (
	usernameRe       = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,32}$`)
	serviceRe        = regexp.MustCompile(`^[A-Za-z0-9@._-]+$`)
	passwordAlphabet = []byte("ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789")
)

// ServiceDef 是面板允许操作的 systemd 服务白名单条目。
type ServiceDef struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

// Config 是 /etc/nanopanel/config.json 的结构。
type Config struct {
	Listen       string       `json:"listen"`
	Username     string       `json:"username"`
	PasswordHash string       `json:"password_hash"`
	Secret       string       `json:"secret"`
	TokenTTLHrs  int          `json:"token_ttl_hours"`
	Services     []ServiceDef `json:"services"`
	AppsDir      string       `json:"apps_dir"`
	ManifestPath string       `json:"manifest_path"`

	// Webhook 通知配置。
	WebhookURL    string   `json:"webhook_url,omitempty"`
	WebhookFormat string   `json:"webhook_format,omitempty"`
	WebhookEvents []string `json:"webhook_events,omitempty"`

	// MonitorIntervalSec 是应用健康检测间隔（秒）。
	// 0 表示使用默认值，小于 0 表示关闭自动检测。
	MonitorIntervalSec int `json:"monitor_interval_sec,omitempty"`
}

// DefaultMonitorIntervalSec 是应用掉线检测的默认间隔。
const DefaultMonitorIntervalSec = 60

// WebhookFormats 支持的通知推送格式。
var WebhookFormats = []string{"generic", "wecom", "dingtalk", "feishu", "serverchan"}

// NotifyEvents 可订阅的通知事件类别。
var NotifyEvents = []string{"login", "service", "process", "app", "settings", "system", "health", "disk"}

// ValidWebhookFormat 判断格式是否受支持。
func ValidWebhookFormat(f string) bool {
	for _, v := range WebhookFormats {
		if v == f {
			return true
		}
	}
	return false
}

// Default 返回一份不含可用密码的默认配置。
// 密码必须显式设置；若启动时仍为空，main 会生成随机密码并打印。
func Default() *Config {
	return &Config{
		Listen:       ":8080",
		Username:     DefaultUsername,
		PasswordHash: "",
		Secret:       randHex(32),
		TokenTTLHrs:  24,
		Services: []ServiceDef{
			{Name: "cups", Label: "CUPS 打印"},
			{Name: "AdGuardHome", Label: "AdGuard Home"},
			{Name: "tailscaled", Label: "Tailscale"},
			{Name: "nginx", Label: "Nginx"},
			{Name: "ssh", Label: "SSH"},
		},
		AppsDir:            "/opt/nanopanel/apps",
		ManifestPath:       "/opt/nanopanel/apps/manifest.json",
		MonitorIntervalSec: DefaultMonitorIntervalSec,
	}
}

// Load 读取配置并补齐缺省字段。文件不存在时返回默认配置。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return nil, fmt.Errorf("读取配置: %w", err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("解析配置: %w", err)
	}
	d := Default()
	if c.Listen == "" {
		c.Listen = d.Listen
	}
	if c.Username == "" {
		c.Username = d.Username
	}
	if c.Secret == "" {
		c.Secret = d.Secret
	}
	if c.TokenTTLHrs <= 0 {
		c.TokenTTLHrs = d.TokenTTLHrs
	}
	if c.AppsDir == "" {
		c.AppsDir = d.AppsDir
	}
	if c.ManifestPath == "" {
		c.ManifestPath = d.ManifestPath
	}
	if len(c.Services) == 0 {
		c.Services = d.Services
	}
	if c.WebhookFormat == "" {
		c.WebhookFormat = "generic"
	}
	if c.MonitorIntervalSec == 0 {
		c.MonitorIntervalSec = d.MonitorIntervalSec
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate 检查配置字段的合法性。
func (c *Config) Validate() error {
	if c.Listen == "" {
		return fmt.Errorf("listen 不能为空")
	}
	if _, _, err := splitPort(c.Listen); err != nil {
		return fmt.Errorf("listen 无效: %w", err)
	}
	if !usernameRe.MatchString(c.Username) {
		return fmt.Errorf("用户名不合法: %q", c.Username)
	}
	if c.TokenTTLHrs < 1 || c.TokenTTLHrs > 24*30 {
		return fmt.Errorf("token_ttl_hours 超出范围: %d", c.TokenTTLHrs)
	}
	for _, s := range c.Services {
		if !serviceRe.MatchString(s.Name) {
			return fmt.Errorf("服务名不合法: %q", s.Name)
		}
	}
	if c.AppsDir == "" || c.ManifestPath == "" {
		return fmt.Errorf("apps_dir / manifest_path 不能为空")
	}
	if c.WebhookURL != "" {
		if len(c.WebhookURL) > 2048 ||
			(!strings.HasPrefix(c.WebhookURL, "http://") && !strings.HasPrefix(c.WebhookURL, "https://")) {
			return fmt.Errorf("webhook_url 不合法")
		}
	}
	if c.WebhookFormat != "" && !ValidWebhookFormat(c.WebhookFormat) {
		return fmt.Errorf("webhook_format 不支持: %q", c.WebhookFormat)
	}
	for _, e := range c.WebhookEvents {
		if !ValidNotifyEvent(e) {
			return fmt.Errorf("webhook_events 包含未知事件: %q", e)
		}
	}
	v := c.MonitorIntervalSec
	if v != 0 && v != -1 && (v < 10 || v > 3600) {
		return fmt.Errorf("monitor_interval_sec 超出范围（10-3600 秒，或 -1 关闭）: %d", v)
	}
	return nil
}

// ValidNotifyEvent 判断事件类别是否受支持。
func ValidNotifyEvent(e string) bool {
	for _, v := range NotifyEvents {
		if v == e {
			return true
		}
	}
	return false
}

func splitPort(listen string) (string, int, error) {
	i := strings.LastIndex(listen, ":")
	if i < 0 {
		return "", 0, fmt.Errorf("缺少端口")
	}
	p, err := strconv.Atoi(listen[i+1:])
	if err != nil || p < 1 || p > 65535 {
		return "", 0, fmt.Errorf("端口必须是 1-65535")
	}
	return listen[:i], p, nil
}

// Port 返回监听端口。
func (c *Config) Port() int {
	_, p, _ := splitPort(c.Listen)
	return p
}

// Save 以 0600 权限写入配置。
func (c *Config) Save(path string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return fmt.Errorf("创建配置目录: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0600)
}

// HashPassword 生成带随机盐的 PBKDF2-HMAC-SHA256 哈希。
// 格式: pbkdf2-sha256$<iterations>$<salt-hex>$<key-hex>
func HashPassword(pw string) string {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		// crypto/rand 失败属于不可恢复的系统级故障。
		panic("crypto/rand 不可用: " + err.Error())
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, pbkdf2Iterations, keyLength)
	if err != nil {
		panic("pbkdf2 计算失败: " + err.Error())
	}
	return fmt.Sprintf("%s$%d$%s$%s", hashPrefix, pbkdf2Iterations,
		hex.EncodeToString(salt), hex.EncodeToString(key))
}

// VerifyPassword 校验密码，同时兼容早期版本的纯 sha256 哈希。
func VerifyPassword(stored, password string) bool {
	if stored == "" {
		return false
	}
	if !strings.HasPrefix(stored, hashPrefix+"$") {
		// 旧格式: 64 位十六进制 sha256。仅用于平滑升级，建议尽快重置密码。
		if len(stored) != sha256.Size*2 {
			return false
		}
		sum := sha256.Sum256([]byte(password))
		return subtle.ConstantTimeCompare([]byte(stored), []byte(hex.EncodeToString(sum[:]))) == 1
	}
	parts := strings.Split(stored, "$")
	if len(parts) != 4 {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 || iter > 10_000_000 {
		return false
	}
	salt, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(want, got) == 1
}

// IsLegacyHash 判断是否为需要升级的旧格式哈希。
func IsLegacyHash(stored string) bool {
	return stored != "" && !strings.HasPrefix(stored, hashPrefix+"$")
}

// RandomPassword 生成不含易混淆字符（0/O/1/l/I）的随机密码。
func RandomPassword(n int) (string, error) {
	if n < MinPasswordLength {
		n = MinPasswordLength
	}
	out := make([]byte, 0, n)
	buf := make([]byte, 64)
	limit := 256 - (256 % len(passwordAlphabet))
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("生成随机密码: %w", err)
		}
		for _, b := range buf {
			if int(b) >= limit {
				continue
			}
			out = append(out, passwordAlphabet[int(b)%len(passwordAlphabet)])
			if len(out) == n {
				break
			}
		}
	}
	return string(out), nil
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand 不可用: " + err.Error())
	}
	return hex.EncodeToString(b)
}
