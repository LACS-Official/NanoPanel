package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nanopanel/internal/auth"
	"nanopanel/internal/config"
)

// newAppTestServer 构造带应用商店目录与配置声明的测试服务器。
func newAppTestServer(t *testing.T) (*Server, string) {
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
	// 清单：带一个声明了配置字段的示例应用
	manifest := `{"version":1,"apps":[{"id":"demo","name":"演示","description":"d",
"version":"1.0.0","icon":"","category":"demo","unit":"demo-svc","state":"not_installed",
"config":[
 {"name":"port","label":"端口","type":"number","default":"8080"},
 {"name":"bind","label":"监听","type":"select","default":"0.0.0.0","options":["0.0.0.0","127.0.0.1"]},
 {"name":"token","label":"令牌","type":"password","default":""}
]}]}`
	if err := os.MkdirAll(filepath.Join(cfg.AppsDir, "demo"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.ManifestPath, []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	// status.sh 返回 installed，让配置流程可用
	statusScript := "#!/bin/sh\necho installed\n"
	if err := os.WriteFile(filepath.Join(cfg.AppsDir, "demo", "status.sh"), []byte(statusScript), 0755); err != nil {
		t.Fatal(err)
	}
	return NewServer(cfg, auth.NewManager(time.Hour, "test-secret"), cfgPath, "test"), cfgPath
}

func TestValidateAppValues(t *testing.T) {
	fields := []AppConfigField{
		{Name: "port", Label: "端口", Type: "number", Default: "8080"},
		{Name: "bind", Label: "监听", Type: "select", Default: "0.0.0.0", Options: []string{"0.0.0.0", "127.0.0.1"}},
	}
	if _, err := validateAppValues(fields, map[string]string{"port": "9090"}); err != nil {
		t.Fatalf("合法值被拒绝: %v", err)
	}
	if _, err := validateAppValues(fields, map[string]string{"port": "99999"}); err == nil {
		t.Fatal("超范围端口应被拒绝")
	}
	if _, err := validateAppValues(fields, map[string]string{"port": "abc"}); err == nil {
		t.Fatal("非数字端口应被拒绝")
	}
	if _, err := validateAppValues(fields, map[string]string{"bind": "evil"}); err == nil {
		t.Fatal("select 非法取值应被拒绝")
	}
	if _, err := validateAppValues(fields, map[string]string{"injected": "x"}); err == nil {
		t.Fatal("未声明的字段应被拒绝（防环境变量注入）")
	}
}

func TestAppConfigSaveAndRead(t *testing.T) {
	srv, _ := newAppTestServer(t)

	// 读取默认值
	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/config?id=demo", nil)
	rec := httptest.NewRecorder()
	srv.GetAppConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("读取配置失败: %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		OK   bool `json:"ok"`
		Data struct {
			Values map[string]string `json:"values"`
			State  string            `json:"state"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Data.Values["port"] != "8080" || got.Data.Values["bind"] != "0.0.0.0" {
		t.Fatalf("默认值异常: %+v", got.Data.Values)
	}

	// 保存合法配置
	req = httptest.NewRequest(http.MethodPost, "/api/v1/apps/config?id=demo",
		strings.NewReader(`{"values":{"port":"9090","bind":"127.0.0.1"}}`))
	rec = httptest.NewRecorder()
	srv.SaveAppConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("保存配置失败: %d %s", rec.Code, rec.Body.String())
	}

	// 保存后 config.env 应有值，且再次读取返回新值
	values, err := srv.readAppValues("demo")
	if err != nil {
		t.Fatal(err)
	}
	if values["port"] != "9090" || values["bind"] != "127.0.0.1" {
		t.Fatalf("config.env 内容异常: %+v", values)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/apps/config?id=demo", nil)
	rec = httptest.NewRecorder()
	srv.GetAppConfig(rec, req)
	var got2 struct {
		Data struct {
			Values map[string]string `json:"values"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got2)
	if got2.Data.Values["port"] != "9090" {
		t.Fatalf("读取应返回新值: %+v", got2.Data.Values)
	}

	// 非法值被拒绝
	req = httptest.NewRequest(http.MethodPost, "/api/v1/apps/config?id=demo",
		strings.NewReader(`{"values":{"port":"70000"}}`))
	rec = httptest.NewRecorder()
	srv.SaveAppConfig(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法端口应返回 400，实际 %d", rec.Code)
	}

	// 未安装的应用（status.sh 输出 not_installed）不可配置
	bad := "#!/bin/sh\necho not_installed\n"
	if err := os.WriteFile(filepath.Join(srv.cfg.AppsDir, "demo", "status.sh"), []byte(bad), 0755); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/apps/config?id=demo",
		strings.NewReader(`{"values":{"port":"9090"}}`))
	rec = httptest.NewRecorder()
	srv.SaveAppConfig(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未安装应用配置应返回 400，实际 %d", rec.Code)
	}
}

func TestBuildWebhookPayload(t *testing.T) {
	cases := map[string][]string{
		"generic":    {"title", "content"},
		"wecom":      {"msgtype", "markdown"},
		"dingtalk":   {"msgtype", "markdown"},
		"feishu":     {"msg_type", "content"},
		"serverchan": {"title", "desp"},
	}
	for format, keys := range cases {
		body, err := buildWebhookPayload(format, "app", "标题", "内容")
		if err != nil {
			t.Fatalf("%s 构造失败: %v", format, err)
		}
		var m map[string]interface{}
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("%s 不是合法 JSON: %v", format, err)
		}
		for _, k := range keys {
			if _, exists := m[k]; !exists {
				t.Errorf("%s 缺少字段 %s: %s", format, k, body)
			}
		}
	}
}

func TestWebhookSaveAndValidate(t *testing.T) {
	srv, cfgPath := newAppTestServer(t)

	// 非法 URL 被拒绝
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/webhook",
		strings.NewReader(`{"url":"ftp://bad","format":"generic","events":["login"]}`))
	rec := httptest.NewRecorder()
	srv.SaveWebhook(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 URL 应返回 400，实际 %d", rec.Code)
	}

	// 非法事件被拒绝
	req = httptest.NewRequest(http.MethodPost, "/api/v1/settings/webhook",
		strings.NewReader(`{"url":"https://example.com/hook","format":"generic","events":["nope"]}`))
	rec = httptest.NewRecorder()
	srv.SaveWebhook(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未知事件应返回 400，实际 %d", rec.Code)
	}

	// 合法保存并持久化
	req = httptest.NewRequest(http.MethodPost, "/api/v1/settings/webhook",
		strings.NewReader(`{"url":"https://example.com/hook","format":"wecom","events":["app","login"]}`))
	rec = httptest.NewRecorder()
	srv.SaveWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("保存失败: %d %s", rec.Code, rec.Body.String())
	}
	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.WebhookURL != "https://example.com/hook" || reloaded.WebhookFormat != "wecom" ||
		len(reloaded.WebhookEvents) != 2 {
		t.Fatalf("webhook 配置未持久化: %+v", reloaded)
	}

	// GetWebhook 返回配置与可选项
	req = httptest.NewRequest(http.MethodGet, "/api/v1/settings/webhook", nil)
	rec = httptest.NewRecorder()
	srv.GetWebhook(rec, req)
	var got struct {
		Data struct {
			URL    string   `json:"url"`
			Format string   `json:"format"`
			Events []string `json:"events"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Data.URL != "https://example.com/hook" || got.Data.Format != "wecom" ||
		len(got.Data.Events) != 2 {
		t.Fatalf("GetWebhook 内容异常: %+v", got.Data)
	}

	// 清空 URL 表示停用通知
	req = httptest.NewRequest(http.MethodPost, "/api/v1/settings/webhook",
		strings.NewReader(`{"url":"","format":"generic","events":[]}`))
	rec = httptest.NewRecorder()
	srv.SaveWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("清空失败: %d", rec.Code)
	}
}

func TestPanelLogs(t *testing.T) {
	srv, _ := newAppTestServer(t)
	srv.logDir = t.TempDir()

	// 未写日志时返回空数组
	req := httptest.NewRequest(http.MethodGet, "/api/v1/panel-logs", nil)
	rec := httptest.NewRecorder()
	srv.PanelLogs(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "[") {
		t.Fatalf("空日志响应异常: %d %s", rec.Code, rec.Body.String())
	}

	// 写入两条并读取（新在前）
	srv.appendAudit(auditEntry{Time: "t1", IP: "1.1.1.1", User: "admin", Category: "app", Action: "安装应用", Detail: "cups"})
	srv.appendAudit(auditEntry{Time: "t2", IP: "1.1.1.1", User: "admin", Category: "login", Action: "登录成功"})
	req = httptest.NewRequest(http.MethodGet, "/api/v1/panel-logs", nil)
	rec = httptest.NewRecorder()
	srv.PanelLogs(rec, req)
	var got struct {
		Data []auditEntry `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Data) != 2 {
		t.Fatalf("应有 2 条日志，实际 %d", len(got.Data))
	}
	if got.Data[0].Action != "登录成功" {
		t.Fatalf("最新记录应在前: %+v", got.Data)
	}

	// 损坏行应被跳过
	if err := os.WriteFile(filepath.Join(srv.logDir, "panel.log"),
		[]byte("{broken json\n"), 0600); err != nil {
		t.Fatal(err)
	}
	srv.appendAudit(auditEntry{Time: "t3", IP: "2.2.2.2", User: "admin", Category: "system", Action: "重启系统"})
	rec = httptest.NewRecorder()
	srv.PanelLogs(rec, req)
	var got2 struct {
		Data []auditEntry `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got2)
	if len(got2.Data) != 1 || got2.Data[0].Action != "重启系统" {
		t.Fatalf("损坏行应被跳过: %+v", got2.Data)
	}
}

// TestAppWebPortResolution 已安装应用的 web_port 应解析为实际端口：
// 优先 config.env 保存值，其次清单默认值；未安装或未声明时为空。
func TestAppWebPortResolution(t *testing.T) {
	srv, _ := newAppTestServer(t)

	// 清单声明 web_port 指向 port 字段（默认 8080）
	manifest := `{"version":1,"apps":[{"id":"demo","name":"演示","description":"d",
"version":"1.0.0","icon":"","category":"demo","unit":"demo-svc",
"web_port":"port","web_path":"/",
"config":[{"name":"port","label":"端口","type":"number","default":"8080"}]}]}`
	if err := os.WriteFile(srv.cfg.ManifestPath, []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	listApps := func() AppMeta {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/apps", nil)
		rec := httptest.NewRecorder()
		srv.ListApps(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data []AppMeta `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Data) != 1 {
			t.Fatalf("应有 1 个应用: %s", rec.Body.String())
		}
		return resp.Data[0]
	}

	// demo 的 status.sh 返回 installed；未保存配置时应回退默认端口
	a := listApps()
	if a.State != "installed" || a.WebPort != "8080" || a.WebPath != "/" {
		t.Fatalf("已安装应用应解析出默认端口 8080: %+v", a)
	}

	// 保存自定义端口后应优先取 config.env 的值
	if err := os.WriteFile(filepath.Join(srv.cfg.AppsDir, "demo", "config.env"),
		[]byte("port=9527\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if a = listApps(); a.WebPort != "9527" {
		t.Fatalf("应优先使用保存的端口 9527: %+v", a)
	}

	// web_port 指向未声明的字段应被清单校验拒绝
	bad := `{"version":1,"apps":[{"id":"demo","name":"演示","description":"d",
"version":"1.0.0","icon":"","category":"demo","web_port":"no_such_field"}]}`
	if err := os.WriteFile(srv.cfg.ManifestPath, []byte(bad), 0644); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps", nil)
	rec := httptest.NewRecorder()
	srv.ListApps(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("web_port 指向未声明字段应返回 500，得到 %d: %s", rec.Code, rec.Body.String())
	}
}
