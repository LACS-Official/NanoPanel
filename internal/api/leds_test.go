package api

// LED 控制接口的回归测试：用临时目录模拟 sysfs，验证列表、单灯开关、
// 批量开关（保留电源灯）以及参数校验与鉴权。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type ledResp struct {
	OK   bool                   `json:"ok"`
	Data map[string]interface{} `json:"data"`
	Err  string                 `json:"err"`
}

func ledTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	srv, _ := newTestServer(t)
	root := t.TempDir()
	srv.ledsRoot = root
	srv.logDir = t.TempDir()
	write := func(name, brightness, max, trigger string) {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		for fn, val := range map[string]string{
			"brightness": brightness, "max_brightness": max,
		} {
			if err := os.WriteFile(filepath.Join(dir, fn), []byte(val), 0644); err != nil {
				t.Fatal(err)
			}
		}
		if trigger != "" {
			if err := os.WriteFile(filepath.Join(dir, "trigger"), []byte(trigger), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	write("power", "1", "1", "none [none] heartbeat")
	write("blue-led", "255", "255", "none [heartbeat] cpu0")
	write("green-led", "0", "1", "")
	return srv, root
}

func ledDo(t *testing.T, srv *Server, method, path, body string, auth bool) (*httptest.ResponseRecorder, ledResp) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case path == "/api/v1/leds" && method == http.MethodGet:
			srv.ListLEDs(w, r)
		case path == "/api/v1/leds/set":
			srv.SetLED(w, r)
		case path == "/api/v1/leds/all":
			srv.SetAllLEDs(w, r)
		default:
			t.Fatalf("未映射的请求 %s %s", method, path)
		}
	})
	if auth {
		srv.AuthMiddleware(h).ServeHTTP(rec, req)
	} else {
		h.ServeHTTP(rec, req)
	}
	var resp ledResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v (%s)", err, rec.Body.String())
	}
	return rec, resp
}

func ledAttr(t *testing.T, root, name, attr string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, name, attr))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func TestLEDListAPI(t *testing.T) {
	srv, root := ledTestServer(t)
	rec, resp := ledDo(t, srv, http.MethodGet, "/api/v1/leds", "", false)
	if rec.Code != http.StatusOK || !resp.OK {
		t.Fatalf("列表接口失败: %d %+v", rec.Code, resp)
	}
	if resp.Data["supported"] != true {
		t.Fatalf("supported 应为 true: %+v", resp.Data)
	}
	leds, ok := resp.Data["leds"].([]interface{})
	if !ok || len(leds) != 3 {
		t.Fatalf("应返回 3 个 LED，实际 %v", resp.Data["leds"])
	}
	first := leds[0].(map[string]interface{})
	if first["name"] != "power" || first["is_power"] != true {
		t.Fatalf("电源灯应排首位并标记 is_power: %+v", first)
	}
	if resp.Data["root"] != root {
		t.Fatalf("root 应回传实际使用的目录: %+v", resp.Data)
	}
}

func TestLEDSetAPITurnsOffAndDetachesTrigger(t *testing.T) {
	srv, root := ledTestServer(t)
	rec, resp := ledDo(t, srv, http.MethodPost, "/api/v1/leds/set",
		`{"name":"blue-led","on":false}`, false)
	if rec.Code != http.StatusOK || !resp.OK {
		t.Fatalf("关闭单灯失败: %d %+v", rec.Code, resp)
	}
	if got := ledAttr(t, root, "blue-led", "brightness"); got != "0" {
		t.Fatalf("brightness 应写入 0，实际 %q", got)
	}
	if got := ledAttr(t, root, "blue-led", "trigger"); got != "none" {
		t.Fatalf("关闭时应解除 heartbeat trigger，实际 %q", got)
	}
	if resp.Data["brightness"] != float64(0) || resp.Data["on"] != false {
		t.Fatalf("响应状态不正确: %+v", resp.Data)
	}
}

func TestLEDSetAPITurnsOnToMax(t *testing.T) {
	srv, root := ledTestServer(t)
	_, resp := ledDo(t, srv, http.MethodPost, "/api/v1/leds/set",
		`{"name":"green-led","on":true}`, false)
	if !resp.OK {
		t.Fatalf("开启单灯失败: %+v", resp)
	}
	if got := ledAttr(t, root, "green-led", "brightness"); got != "1" {
		t.Fatalf("开启应写到 max_brightness=1，实际 %q", got)
	}
}

func TestLEDSetAPIValidation(t *testing.T) {
	srv, _ := ledTestServer(t)
	cases := []struct {
		body string
		code int
	}{
		{`{}`, http.StatusBadRequest},
		{`{"name":"blue-led"}`, http.StatusBadRequest},
		{`{"on":true}`, http.StatusBadRequest},
		{`{"name":"nope","on":true}`, http.StatusNotFound},
		{`{"name":"blue-led","on":true,"extra":1}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		rec, _ := ledDo(t, srv, http.MethodPost, "/api/v1/leds/set", c.body, false)
		if rec.Code != c.code {
			t.Fatalf("body=%s 期望 %d，实际 %d", c.body, c.code, rec.Code)
		}
	}
}

func TestLEDSetAllAPIKeepPower(t *testing.T) {
	srv, root := ledTestServer(t)
	rec, resp := ledDo(t, srv, http.MethodPost, "/api/v1/leds/all",
		`{"on":false,"keep_power":true}`, false)
	if rec.Code != http.StatusOK || !resp.OK {
		t.Fatalf("批量关闭失败: %d %+v", rec.Code, resp)
	}
	if resp.Data["changed"] != float64(2) || resp.Data["skipped"] != float64(1) {
		t.Fatalf("应关闭 2 个并跳过 1 个电源灯: %+v", resp.Data)
	}
	if got := ledAttr(t, root, "power", "brightness"); got != "1" {
		t.Fatalf("电源灯应保留点亮，实际 %q", got)
	}
	for _, n := range []string{"blue-led", "green-led"} {
		if got := ledAttr(t, root, n, "brightness"); got != "0" {
			t.Fatalf("%s 应被关闭，实际 %q", n, got)
		}
	}
}

func TestLEDSetAllAPIAllOn(t *testing.T) {
	srv, root := ledTestServer(t)
	_, resp := ledDo(t, srv, http.MethodPost, "/api/v1/leds/all",
		`{"on":true,"keep_power":false}`, false)
	if !resp.OK || resp.Data["changed"] != float64(3) {
		t.Fatalf("全部开启应成功 3 个: %+v", resp)
	}
	for n, want := range map[string]string{"power": "1", "blue-led": "255", "green-led": "1"} {
		if got := ledAttr(t, root, n, "brightness"); got != want {
			t.Fatalf("%s 开启后应为 %s，实际 %q", n, want, got)
		}
	}
}

func TestLEDListAPIUnsupported(t *testing.T) {
	srv, _ := ledTestServer(t)
	srv.ledsRoot = filepath.Join(t.TempDir(), "no-sysfs")
	rec, resp := ledDo(t, srv, http.MethodGet, "/api/v1/leds", "", false)
	if rec.Code != http.StatusOK || resp.Data["supported"] != false {
		t.Fatalf("无 sysfs 时应返回 supported=false: %d %+v", rec.Code, resp.Data)
	}
	if msg, _ := resp.Data["message"].(string); msg == "" {
		t.Fatal("应给出不支持的提示文案")
	}
}

func TestLEDWriteRequiresLogin(t *testing.T) {
	srv, _ := ledTestServer(t)
	rec, _ := ledDo(t, srv, http.MethodPost, "/api/v1/leds/set",
		`{"name":"blue-led","on":false}`, true)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录写操作应返回 401，实际 %d", rec.Code)
	}
}
