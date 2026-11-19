package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nanopanel/internal/auth"
	"nanopanel/internal/config"
	"nanopanel/internal/totp"
)

// ---------- weekly / once 定时调度 ----------

func TestWeeklyScheduleDue(t *testing.T) {
	// 2026-11-17 是周二（Weekday=2）。
	state := freshCronState()
	task := CronTask{ID: "w1", Schedule: "weekly:2:10:30"} // 周二 10:30
	// 周二 10:00 启动，基准应落在上一周，到 10:31 触发。
	primeCronBaseline(state, task, at(10, 0))
	if cronDue(state, task, at(10, 31)) != true {
		t.Fatal("weekly 到点应触发")
	}
	// 同周不再触发
	if cronDue(state, task, at(11, 0)) != false {
		t.Fatal("weekly 同一周不应重复触发")
	}
	// 下一周同一时刻触发
	next := at(10, 31).AddDate(0, 0, 7)
	if cronDue(state, task, next) != true {
		t.Fatal("weekly 下一周应触发")
	}
}

func TestWeeklyScheduleAlreadyPassed(t *testing.T) {
	state := freshCronState()
	task := CronTask{ID: "w2", Schedule: "weekly:2:10:30"}
	// 周二 12:00 启动，本周计划时刻已过，不应补跑。
	primeCronBaseline(state, task, at(12, 0))
	if cronDue(state, task, at(12, 1)) != false {
		t.Fatal("weekly 启动时刻已过计划时刻，不应补跑")
	}
}

func TestOnceScheduleDue(t *testing.T) {
	state := freshCronState()
	task := CronTask{ID: "o1", Schedule: "once:2026-11-17T10:30"}
	primeCronBaseline(state, task, at(10, 0))
	if cronDue(state, task, at(10, 31)) != true {
		t.Fatal("once 到点应触发")
	}
	if cronDue(state, task, at(11, 0)) != false {
		t.Fatal("once 触发后不应重复触发")
	}
}

func TestOnceSchedulePastOnStartup(t *testing.T) {
	state := freshCronState()
	task := CronTask{ID: "o2", Schedule: "once:2026-11-17T08:00"}
	// 10:00 启动，计划时刻已过，重启不补跑历史一次性任务。
	primeCronBaseline(state, task, at(10, 0))
	if cronDue(state, task, at(10, 1)) != false {
		t.Fatal("once 计划时刻已过，启动不应补跑")
	}
}

func TestValidScheduleExtended(t *testing.T) {
	for _, s := range []string{
		"weekly:0:03:30", "weekly:6:23:59", "once:2026-12-31T23:59",
	} {
		if !validSchedule(s) {
			t.Errorf("合法表达式被拒绝: %s", s)
		}
	}
	for _, s := range []string{
		"weekly:7:03:30", "weekly:0:24:00", "once:2026-13-01T03:00", "once:garbage",
	} {
		if validSchedule(s) {
			t.Errorf("非法表达式被接受: %s", s)
		}
	}
}

// ---------- CSV 导出 ----------

func TestMetricsExportCSV(t *testing.T) {
	srv := metricsTestServer(t)
	base := time.Now().Unix()
	srv.appendMetric(MetricPoint{TS: base - 10, CPU: 12.3, Mem: 45.6, Disk: 70.1, Temp: 51.5, NetRx: 100, NetTx: 200})
	srv.appendMetric(MetricPoint{TS: base - 5, CPU: 88.8, Mem: 90.1, Disk: 71.0, Temp: 60.0, NetRx: 300, NetTx: 400})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/export?range=1h", nil)
	rec := httptest.NewRecorder()
	srv.MetricsExport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("Content-Type 错误: %s", ct)
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("缺少 attachment 下载头")
	}
	body := rec.Body.String()
	// BOM 后第一行应为表头
	first := strings.SplitN(body, "\n", 2)[0]
	if !strings.Contains(first, "cpu_pct,mem_pct,disk_pct,temp_c,net_rx_bytes,net_tx_bytes") {
		t.Fatalf("表头错误: %q", first)
	}
	if !strings.Contains(body, "12.3,45.6,70.1,51.5,100,200") {
		t.Fatalf("CSV 缺少采样数据行: %s", body)
	}
	if !strings.Contains(body, "88.8,90.1,71.0,60.0,300,400") {
		t.Fatalf("CSV 缺少第二条数据行: %s", body)
	}
}

// ---------- 只读 token ----------

func TestReadonlyTokenScope(t *testing.T) {
	am := auth.NewManager(time.Hour, "scope-secret")
	rw := am.Issue()
	ro := am.IssueReadonly()
	if am.Scope(rw) != auth.ScopeRW {
		t.Fatalf("普通 token scope 应为 rw，实际 %s", am.Scope(rw))
	}
	if am.Scope(ro) != auth.ScopeRO {
		t.Fatalf("只读 token scope 应为 ro，实际 %s", am.Scope(ro))
	}
	// 篡改 scope 段应使验签失败
	tampered := strings.Replace(ro, ".ro.", ".rw.", 1)
	if am.Valid(tampered) {
		t.Fatal("篡改 scope 的 token 不应有效")
	}
}

func TestReadonlyTokenMiddleware(t *testing.T) {
	srv, _ := newTestServer(t)
	ro := srv.auth.IssueReadonly()
	called := 0
	handler := srv.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called++
		ok(w, map[string]string{"ok": "yes"})
	})

	// GET 放行
	get := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	get.Header.Set("Authorization", "Bearer "+ro)
	rec := httptest.NewRecorder()
	handler(rec, get)
	if rec.Code != http.StatusOK || called != 1 {
		t.Fatalf("只读 token 的 GET 应放行: code=%d called=%d", rec.Code, called)
	}

	// POST 拦截
	post := httptest.NewRequest(http.MethodPost, "/api/v1/process/kill", nil)
	post.Header.Set("Authorization", "Bearer "+ro)
	rec2 := httptest.NewRecorder()
	handler(rec2, post)
	if rec2.Code != http.StatusForbidden || called != 1 {
		t.Fatalf("只读 token 的 POST 应被 403 拦截: code=%d called=%d", rec2.Code, called)
	}
}

// ---------- TOTP 登录两步验证 ----------

func TestTOTPLoginFlow(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.logDir = t.TempDir()
	secret, err := totp.GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	srv.cfg.TOTPEnabled = true
	srv.cfg.TOTPSecret = secret
	srv.mu.Unlock()

	// 第一步：密码正确应返回 mfa_required，不发 token
	loginBody := strings.NewReader(`{"username":"admin","password":"secret123"}`)
	r1 := httptest.NewRequest(http.MethodPost, "/api/v1/login", loginBody)
	w1 := httptest.NewRecorder()
	srv.Login(w1, r1)
	var d1 struct {
		OK   bool `json:"ok"`
		Data struct {
			MFARequired bool   `json:"mfa_required"`
			Ticket      string `json:"ticket"`
			Token       string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w1.Body.Bytes(), &d1); err != nil {
		t.Fatal(err)
	}
	if !d1.Data.MFARequired || d1.Data.Ticket == "" || d1.Data.Token != "" {
		t.Fatalf("第一步应要求 MFA 且不下发 token: %s", w1.Body.String())
	}

	// 第二步：正确验证码换 token
	code := totpCurrentCode(t, secret)
	body2, _ := json.Marshal(map[string]string{"ticket": d1.Data.Ticket, "code": code})
	r2 := httptest.NewRequest(http.MethodPost, "/api/v1/login/verify", strings.NewReader(string(body2)))
	w2 := httptest.NewRecorder()
	srv.VerifyLogin(w2, r2)
	var d2 struct {
		OK   bool `json:"ok"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &d2); err != nil {
		t.Fatal(err)
	}
	if !d2.OK || d2.Data.Token == "" || !srv.auth.Valid(d2.Data.Token) {
		t.Fatalf("第二步应签发有效 token: %s", w2.Body.String())
	}

	// 票据一次性：重复使用应失败
	r3 := httptest.NewRequest(http.MethodPost, "/api/v1/login/verify", strings.NewReader(string(body2)))
	w3 := httptest.NewRecorder()
	srv.VerifyLogin(w3, r3)
	if w3.Code != http.StatusUnauthorized {
		t.Fatalf("重复票据应 401，实际 %d", w3.Code)
	}
}

func totpCurrentCode(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.Code(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return code
}

// ---------- 免打扰时段 ----------

func TestQuietHours(t *testing.T) {
	c := config.Default()
	c.QuietEnabled = true
	c.QuietStart = "23:00"
	c.QuietEnd = "07:00"
	for _, tc := range []struct {
		h    int
		want bool
	}{
		{23, true}, {0, true}, {6, true}, {7, false}, {12, false}, {22, false},
	} {
		now := time.Date(2026, 11, 17, tc.h, 30, 0, 0, time.Local)
		if got := c.InQuietHours(now); got != tc.want {
			t.Errorf("%d:30 免打扰=%v，期望 %v", tc.h, got, tc.want)
		}
	}
	c.QuietEnabled = false
	if c.InQuietHours(time.Date(2026, 11, 17, 23, 30, 0, 0, time.Local)) {
		t.Fatal("关闭时不应命中免打扰")
	}
}

// ---------- 配置校验 ----------

func TestNewConfigValidation(t *testing.T) {
	c := config.Default()
	c.RemoteBackupType = "ftp"
	if c.Validate() == nil {
		t.Fatal("非法远程备份类型应被拒绝")
	}
	c.RemoteBackupType = "webdav"
	c.RemoteBackupURL = "not-a-url"
	if c.Validate() == nil {
		t.Fatal("非法远程备份 URL 应被拒绝")
	}
	c.RemoteBackupURL = "https://dav.example.com"
	c.QuietEnabled = true
	c.QuietStart = "25:00"
	if c.Validate() == nil {
		t.Fatal("非法免打扰时段应被拒绝")
	}
}
