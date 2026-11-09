package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nanopanel/internal/auth"
	"nanopanel/internal/config"
)

// newMihomoTestServer 构造一个已安装 mihomo、内核由 httptest 模拟的面板。
func newMihomoTestServer(t *testing.T) (*Server, *httptest.Server, *[]capturedReq) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.PasswordHash = config.HashPassword("secret123")
	cfg.AppsDir = filepath.Join(dir, "apps")
	cfg.ManifestPath = filepath.Join(cfg.AppsDir, "manifest.json")
	cfgPath := filepath.Join(dir, "config.json")
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	manifest := `{"version":1,"apps":[{"id":"mihomo","name":"Mihomo","description":"d",
"version":"1.0.0","icon":"","category":"网络","unit":"mihomo"}]}`
	if err := os.MkdirAll(filepath.Join(cfg.AppsDir, "mihomo"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.ManifestPath, []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.AppsDir, "mihomo", "status.sh"),
		[]byte("#!/bin/sh\necho installed\n"), 0755); err != nil {
		t.Fatal(err)
	}

	caps := make([]capturedReq, 0)
	var mu sync.Mutex
	kernel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		caps = append(caps, capturedReq{
			Method: r.Method, Path: r.URL.Path,
			Auth: r.Header.Get("Authorization"), Body: string(body),
		})
		mu.Unlock()
		switch r.URL.Path {
		case "/version":
			_, _ = io.WriteString(w, `{"version":"v1.19.0"}`)
		case "/proxies":
			_, _ = io.WriteString(w, `{"proxies":{}}`)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	srv := NewServer(cfg, auth.NewManager(time.Hour, "test-secret"), cfgPath, "test")
	srv.mihomoBase = kernel.URL
	t.Cleanup(kernel.Close)
	return srv, kernel, &caps
}

type capturedReq struct {
	Method string
	Path   string
	Auth   string
	Body   string
}

func TestAppHealthWebhookAlerts(t *testing.T) {
	srv, _ := newAppTestServer(t)

	got := make(chan string, 8)
	wh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer wh.Close()

	srv.mu.Lock()
	srv.cfg.WebhookURL = wh.URL
	srv.cfg.WebhookFormat = "generic"
	srv.cfg.WebhookEvents = []string{"health"}
	srv.mu.Unlock()

	state := "active"
	srv.unitProbe = func(context.Context, string) string { return state }
	ctx := context.Background()

	// 首次检测只建立基线，不推送
	states, changes := srv.checkHealth(ctx, true, false)
	if len(states) != 1 || states[0].Status != "running" || len(changes) != 0 {
		t.Fatalf("基线检测异常: states=%+v changes=%+v", states, changes)
	}
	select {
	case <-got:
		t.Fatal("建立基线时不应推送通知")
	case <-time.After(150 * time.Millisecond):
	}

	// 掉线：应产生 down 事件并推送
	state = "inactive"
	_, changes = srv.checkHealth(ctx, true, false)
	if len(changes) != 1 || changes[0].Event != "down" {
		t.Fatalf("掉线未被检出: %+v", changes)
	}
	select {
	case body := <-got:
		if !strings.Contains(body, "应用掉线") || !strings.Contains(body, `"event":"health"`) {
			t.Fatalf("掉线通知内容异常: %s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("掉线未推送 webhook")
	}

	// 恢复：应产生 recovered 事件并推送
	state = "active"
	_, changes = srv.checkHealth(ctx, true, false)
	if len(changes) != 1 || changes[0].Event != "recovered" {
		t.Fatalf("恢复未被检出: %+v", changes)
	}
	select {
	case body := <-got:
		if !strings.Contains(body, "应用恢复") {
			t.Fatalf("恢复通知内容异常: %s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("恢复未推送 webhook")
	}

	// 未订阅 health 时不再推送
	srv.mu.Lock()
	srv.cfg.WebhookEvents = []string{"app"}
	srv.mu.Unlock()
	state = "inactive"
	_, _ = srv.checkHealth(ctx, true, false)
	select {
	case <-got:
		t.Fatal("未订阅事件不应推送")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestAppHealthForceCheckAndList(t *testing.T) {
	srv, _ := newAppTestServer(t)
	srv.unitProbe = func(context.Context, string) string { return "failed" }

	// 无基线时 force=1 也要立即报掉线
	_, changes := srv.checkHealth(context.Background(), true, true)
	if len(changes) != 1 || changes[0].Event != "down" {
		t.Fatalf("强制体检未报掉线: %+v", changes)
	}

	// 应用列表应带运行状态字段
	srv.unitProbe = func(context.Context, string) string { return "active" }
	rec := httptest.NewRecorder()
	srv.ListApps(rec, httptest.NewRequest(http.MethodGet, "/api/v1/apps", nil))
	var got struct {
		Data []AppMeta `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Data) != 1 || got.Data[0].Status != "running" {
		t.Fatalf("ListApps 未返回运行状态: %+v", got.Data)
	}
}

func TestMonitorSettings(t *testing.T) {
	srv, cfgPath := newAppTestServer(t)

	// 非法间隔
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/monitor",
		strings.NewReader(`{"interval_sec":5}`))
	rec := httptest.NewRecorder()
	srv.SaveMonitorSettings(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法间隔应返回 400，实际 %d", rec.Code)
	}

	// 关闭自动检测
	req = httptest.NewRequest(http.MethodPost, "/api/v1/settings/monitor",
		strings.NewReader(`{"interval_sec":-1}`))
	rec = httptest.NewRecorder()
	srv.SaveMonitorSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("关闭失败: %d %s", rec.Code, rec.Body.String())
	}
	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.MonitorIntervalSec != -1 {
		t.Fatalf("关闭状态未持久化: %d", reloaded.MonitorIntervalSec)
	}

	// 重新开启并读取
	req = httptest.NewRequest(http.MethodPost, "/api/v1/settings/monitor",
		strings.NewReader(`{"interval_sec":30}`))
	rec = httptest.NewRecorder()
	srv.SaveMonitorSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("保存失败: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/settings/monitor", nil)
	rec = httptest.NewRecorder()
	srv.GetMonitorSettings(rec, req)
	var got struct {
		Data struct {
			IntervalSec int           `json:"interval_sec"`
			Enabled     bool          `json:"monitor_enabled"`
			Options     []interface{} `json:"options"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Data.IntervalSec != 30 || !got.Data.Enabled || len(got.Data.Options) != 5 {
		t.Fatalf("监控设置读取异常: %+v", got.Data)
	}
}

func TestMihomoStatus(t *testing.T) {
	srv, _, _ := newMihomoTestServer(t)
	rec := httptest.NewRecorder()
	srv.MihomoStatus(rec, httptest.NewRequest(http.MethodGet, "/api/v1/mihomo/status", nil))
	var got struct {
		Data struct {
			Installed bool   `json:"installed"`
			Reachable bool   `json:"reachable"`
			Version   string `json:"version"`
			Port      int    `json:"port"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Data.Installed || !got.Data.Reachable || got.Data.Version != "v1.19.0" || got.Data.Port != 9090 {
		t.Fatalf("内核状态异常: %+v", got.Data)
	}
}
