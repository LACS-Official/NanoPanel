package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nanopanel/internal/auth"
	"nanopanel/internal/config"
)

type apiResp struct {
	OK   bool              `json:"ok"`
	Data map[string]string `json:"data"`
	Err  string            `json:"err"`
}

func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	cfg := config.Default()
	cfg.PasswordHash = config.HashPassword("secret123")
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveCredentials(cfgPath, cfg.Username, "secret123"); err != nil {
		t.Fatal(err)
	}
	return NewServer(cfg, auth.NewManager(time.Hour, "test-secret"), cfgPath, "test"), cfgPath
}

func decodeResp(t *testing.T, rec *httptest.ResponseRecorder) apiResp {
	t.Helper()
	var r apiResp
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("响应不是合法 JSON: %v (%s)", err, rec.Body.String())
	}
	return r
}

func loginRequest(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Login(rec, req)
	return rec
}

func TestHealth(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Health(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("健康检查状态码 %d", rec.Code)
	}
	r := decodeResp(t, rec)
	if r.Data["status"] != "ok" || r.Data["version"] != "test" || r.Data["name"] != "NanoPanel" {
		t.Fatalf("健康检查内容异常: %+v", r.Data)
	}
}

func TestLogin(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := loginRequest(t, srv, `{"username":"admin","password":"secret123"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("正确凭据登录失败: %d %s", rec.Code, rec.Body.String())
	}
	r := decodeResp(t, rec)
	if !strings.HasPrefix(r.Data["token"], "v1.") || r.Data["username"] != "admin" {
		t.Fatalf("登录响应异常: %+v", r.Data)
	}
}

func TestLoginRejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"错误密码", `{"username":"admin","password":"nope"}`},
		{"错误用户名", `{"username":"root","password":"secret123"}`},
		{"缺少字段", `{"password":"secret123"}`},
		{"未知字段", `{"username":"admin","password":"secret123","extra":1}`},
		{"非法 JSON", `not json`},
		{"超大请求体", `{"username":"admin","password":"` + strings.Repeat("x", 9000) + `"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newTestServer(t)
			rec := loginRequest(t, srv, tc.body)
			if rec.Code == http.StatusOK {
				t.Fatal("异常输入不应登录成功")
			}
			if r := decodeResp(t, rec); r.OK {
				t.Fatal("失败响应 ok 应为 false")
			}
		})
	}
}

func TestLoginRateLimit(t *testing.T) {
	srv, _ := newTestServer(t)
	for i := 0; i < 5; i++ {
		rec := loginRequest(t, srv, `{"username":"admin","password":"bad"}`)
		if i < 4 && rec.Code != http.StatusUnauthorized {
			t.Fatalf("第 %d 次失败应为 401，实际 %d", i+1, rec.Code)
		}
		if i == 4 && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("达到阈值应返回 429，实际 %d", rec.Code)
		}
	}
	if rec := loginRequest(t, srv, `{"username":"admin","password":"secret123"}`); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("锁定期内正确密码也应被拒绝，实际 %d", rec.Code)
	}
}

func TestAuthMiddleware(t *testing.T) {
	srv, _ := newTestServer(t)
	h := srv.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("无 token 应返回 401，实际 %d", rec.Code)
	}

	tok := decodeResp(t, loginRequest(t, srv, `{"username":"admin","password":"secret123"}`)).Data["token"]
	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("有效 token 应放行，实际 %d", rec.Code)
	}
}

func TestChangePassword(t *testing.T) {
	srv, cfgPath := newTestServer(t)
	oldToken := decodeResp(t, loginRequest(t, srv, `{"username":"admin","password":"secret123"}`)).Data["token"]

	// 弱新密码应被拒绝
	req := httptest.NewRequest(http.MethodPost, "/api/v1/change-password",
		strings.NewReader(`{"old_password":"secret123","new_password":"short"}`))
	rec := httptest.NewRecorder()
	srv.ChangePassword(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("短密码应返回 400，实际 %d", rec.Code)
	}

	// 原密码错误
	req = httptest.NewRequest(http.MethodPost, "/api/v1/change-password",
		strings.NewReader(`{"old_password":"wrong","new_password":"newsecret123"}`))
	rec = httptest.NewRecorder()
	srv.ChangePassword(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("原密码错误应返回 401，实际 %d", rec.Code)
	}

	// 正常修改
	req = httptest.NewRequest(http.MethodPost, "/api/v1/change-password",
		strings.NewReader(`{"old_password":"secret123","new_password":"newsecret123"}`))
	rec = httptest.NewRecorder()
	srv.ChangePassword(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("改密失败: %d %s", rec.Code, rec.Body.String())
	}
	newToken := decodeResp(t, rec).Data["token"]
	if newToken == "" || newToken == oldToken {
		t.Fatal("改密后应返回新的 token")
	}

	// 旧 token 失效、旧密码失效、新密码可用、凭据文件已同步
	h := srv.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	req = httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	req.Header.Set("Authorization", "Bearer "+oldToken)
	rec = httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatal("改密后旧登录态未失效")
	}
	if loginRequest(t, srv, `{"username":"admin","password":"secret123"}`).Code != http.StatusUnauthorized {
		t.Fatal("旧密码仍可登录")
	}
	if loginRequest(t, srv, `{"username":"admin","password":"newsecret123"}`).Code != http.StatusOK {
		t.Fatal("新密码无法登录")
	}
	cred, err := config.LoadCredentials(cfgPath)
	if err != nil || cred.Password != "newsecret123" {
		t.Fatalf("凭据文件未同步: %+v, err=%v", cred, err)
	}
}

func TestSecurityHeaders(t *testing.T) {
	srv, _ := newTestServer(t)
	h := srv.SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
		"Cache-Control":          "no-store",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("响应头 %s = %q，期望 %q", k, got, v)
		}
	}
}
