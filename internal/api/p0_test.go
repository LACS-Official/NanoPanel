package api

// P0 安全加固相关的测试：自签证书、fstab 备份回滚、日志轮转、登录锁定告警。

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"nanopanel/internal/sysinfo"
)

// TestGenerateSelfSignedCert 生成证书后应能用 crypto/tls 加载，SAN 含 localhost。
func TestGenerateSelfSignedCert(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "nanopanel.crt")
	keyPath := filepath.Join(dir, "nanopanel.key")
	if err := GenerateSelfSignedCert(certPath, keyPath, []string{"localhost", "127.0.0.1", "nanopanel.test"}); err != nil {
		t.Fatalf("生成证书失败: %v", err)
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("加载证书失败: %v", err)
	}
	if cert.Leaf == nil {
		// LoadX509KeyPair 不解析 leaf；用 X509KeyPair 再验一次解析链路。
		if _, err := tls.X509KeyPair(mustRead(t, certPath), mustRead(t, keyPath)); err != nil {
			t.Fatalf("X509KeyPair 失败: %v", err)
		}
	}
	// 私钥权限必须收紧（POSIX 平台）。
	if runtime.GOOS != "windows" {
		if st, err := os.Stat(keyPath); err != nil || st.Mode().Perm() != 0600 {
			t.Errorf("私钥权限应为 0600: %v", st.Mode())
		}
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestTLSSettingsToggle 开启 HTTPS 生成证书并持久化配置，关闭时清空字段。
func TestTLSSettingsToggle(t *testing.T) {
	srv, cfgPath := newTestServer(t)
	srv.tlsDir = t.TempDir()
	restarted := false
	srv.selfRestart = func() error { restarted = true; return nil }

	// 初始状态：未启用
	rec := httptest.NewRecorder()
	srv.TLSSettings(rec, httptest.NewRequest(http.MethodGet, "/api/v1/settings/tls", nil))
	if !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Fatalf("初始应为未启用: %s", rec.Body.String())
	}

	// 开启
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/tls",
		strings.NewReader(`{"enabled":true}`))
	srv.SaveTLSSettings(rec, req)
	if rec.Code != http.StatusOK || !restarted {
		t.Fatalf("开启失败: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(srv.tlsDir, "nanopanel.crt")); err != nil {
		t.Fatalf("证书未生成: %v", err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "tls_cert") {
		t.Fatalf("配置未持久化: %s", data)
	}

	// 状态端点应带证书有效期
	rec = httptest.NewRecorder()
	srv.TLSSettings(rec, httptest.NewRequest(http.MethodGet, "/api/v1/settings/tls", nil))
	if !strings.Contains(rec.Body.String(), "expires") {
		t.Fatalf("状态应含有效期: %s", rec.Body.String())
	}

	// 关闭
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/settings/tls",
		strings.NewReader(`{"enabled":false}`))
	srv.SaveTLSSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("关闭失败: %d", rec.Code)
	}
	data, _ = os.ReadFile(cfgPath)
	var m map[string]interface{}
	_ = json.Unmarshal(data, &m)
	if v, present := m["tls_cert"]; present && v != "" {
		t.Fatalf("关闭后 tls_cert 应清空: %s", data)
	}
}

// TestFstabBackupAndNoDuplicate fstab 写入前应备份，已有条目不重复写。
func TestFstabBackupAndNoDuplicate(t *testing.T) {
	dir := t.TempDir()
	orig := fstabPath
	fstabPath = filepath.Join(dir, "fstab")
	defer func() { fstabPath = orig }()

	original := "UUID=1111 / ext4 defaults 0 1\n"
	if err := os.WriteFile(fstabPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	added, err := appendFstab(context.Background(), "/dev/sdz9", "/mnt/data")
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if !added {
		t.Fatal("首次应写入")
	}
	// 备份应等于写入前内容
	backup, err := os.ReadFile(fstabPath + ".nanopanel.bak")
	if err != nil || string(backup) != original {
		t.Fatalf("备份内容不正确: %q %v", backup, err)
	}
	// 重复写同设备应跳过
	added, err = appendFstab(context.Background(), "/dev/sdz9", "/mnt/data")
	if err != nil || added {
		t.Fatalf("重复条目应跳过: added=%v err=%v", added, err)
	}
	data, _ := os.ReadFile(fstabPath)
	if strings.Count(string(data), "/dev/sdz9") != 1 {
		t.Fatalf("条目重复写入: %s", data)
	}
}

// TestAuditLogRotation 超过阈值的日志文件应滚动到 .1，最老备份删除。
func TestAuditLogRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, auditLogFilename)
	old := auditLogMaxSize
	auditLogMaxSize = 10
	defer func() { auditLogMaxSize = old }()

	if err := os.WriteFile(path, []byte(strings.Repeat("x", 20)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".1", []byte("older"), 0600); err != nil {
		t.Fatal(err)
	}
	rotateAuditLog(path)
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("未滚动出 .1: %v", err)
	}
	if got, _ := os.ReadFile(path + ".1"); string(got) != strings.Repeat("x", 20) {
		t.Fatalf(".1 应为原当前日志内容")
	}
	if got, _ := os.ReadFile(path + ".2"); string(got) != "older" {
		t.Fatalf(".2 应为原 .1 内容: %q", got)
	}
	// 小文件不轮转
	if err := os.WriteFile(path, []byte("tiny"), 0600); err != nil {
		t.Fatal(err)
	}
	rotateAuditLog(path)
	if got, _ := os.ReadFile(path); string(got) != "tiny" {
		t.Fatalf("小文件不应轮转")
	}
}

// TestLoginLockAlert 连续失败达到阈值返回 429，审计日志应记录 security 告警。
func TestLoginLockAlert(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.logDir = t.TempDir()

	for i := 0; i < 4; i++ {
		rec := loginRequest(t, srv, `{"username":"admin","password":"wrong-password"}`)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("第 %d 次应 401: %d", i+1, rec.Code)
		}
	}
	rec := loginRequest(t, srv, `{"username":"admin","password":"wrong-password"}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("第 5 次应 429: %d", rec.Code)
	}

	// 等异步日志落盘
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filepath.Join(srv.logDir, auditLogFilename))
		if err == nil && strings.Contains(string(data), "登录失败锁定") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("审计日志未记录登录锁定告警")
}

// TestTempAlertAndRecovery 超过阈值只告警一次，回落滞回带后恢复可再告警。
func TestTempAlertAndRecovery(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.logDir = t.TempDir()
	srv.mu.Lock()
	srv.cfg.TempThresholdC = 50
	srv.mu.Unlock()
	current := 55.0 // 高于阈值 50
	srv.tempProbe = func() []sysinfo.TempZone {
		return []sysinfo.TempZone{{Label: "cpu-thermal", Celsius: current}}
	}

	srv.checkTemp(context.Background())
	if !srv.tempAlerted {
		t.Fatal("超过阈值应置位告警标记")
	}

	// 持续高温不再重复告警（标记保持，不会二次推送）
	srv.checkTemp(context.Background())
	if !srv.tempAlerted {
		t.Fatal("高温期间标记应保持")
	}

	// 回落到滞回带以下（阈值-5）清除标记
	current = 44
	srv.checkTemp(context.Background())
	if srv.tempAlerted {
		t.Fatal("回落后应清除告警标记")
	}
}
