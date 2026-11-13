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
)

func metricsTestServer(t *testing.T) *Server {
	t.Helper()
	srv, _ := newTestServer(t)
	srv.metricsDirOverride = t.TempDir()
	srv.logDir = t.TempDir() // 告警审计日志落到临时目录，避免污染真实日志
	srv.initMetrics()
	return srv
}

// 采集函数必须在非 Linux（CI 的 Windows runner 读不到 /proc）也不 panic，
// 否则后台 goroutine 会让服务在开发环境直接崩溃。
func TestCollectMetricPortable(t *testing.T) {
	p := collectMetric()
	if p.TS <= 0 {
		t.Fatalf("采样时间戳无效: %+v", p)
	}
	if p.CPU < 0 || p.CPU > 100 || p.Mem < 0 || p.Mem > 100 || p.Disk < 0 || p.Disk > 100 {
		t.Fatalf("百分比超出合理范围: %+v", p)
	}
}

func TestDownsampleMetrics(t *testing.T) {
	if got := downsampleMetrics(nil, 240); len(got) != 0 {
		t.Fatalf("空输入应返回空切片，实际 %d", len(got))
	}
	short := []MetricPoint{{TS: 1, CPU: 10}, {TS: 2, CPU: 20}}
	got := downsampleMetrics(short, 240)
	if len(got) != 2 || got[1].CPU != 20 {
		t.Fatalf("少于上限时应原样返回，实际 %+v", got)
	}

	points := make([]MetricPoint, 0, 1000)
	for i := 0; i < 1000; i++ {
		points = append(points, MetricPoint{
			TS: int64(1000 + i), CPU: 50, Mem: 60, Disk: 70, Temp: 40,
			NetRx: uint64(i * 10), NetTx: uint64(i * 5),
		})
	}
	out := downsampleMetrics(points, 240)
	if len(out) > 240 {
		t.Fatalf("降采样后点数 %d 超过上限 240", len(out))
	}
	if len(out) < 100 {
		t.Fatalf("降采样后点数偏少: %d", len(out))
	}
	for _, p := range out {
		if p.CPU != 50 || p.Mem != 60 || p.Disk != 70 || p.Temp != 40 {
			t.Fatalf("均值计算错误: %+v", p)
		}
	}
	// 累计流量与时间轴都取桶内最后一个点，二者必须对应
	last := out[len(out)-1]
	if last.TS != points[len(points)-1].TS || last.NetRx != 9990 {
		t.Fatalf("累计流量/时间戳未取桶末值: %+v", last)
	}
}

func TestMetricsPersistence(t *testing.T) {
	srv := metricsTestServer(t)
	now := time.Now().Unix()
	srv.appendMetric(MetricPoint{TS: now - 120, CPU: 11.1, NetRx: 100})
	srv.appendMetric(MetricPoint{TS: now, CPU: 22.2, NetRx: 200})

	path := srv.metricsPath()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("指标文件未生成: %v", err)
	}

	// 追加一行损坏数据与一行过期数据，恢复时应跳过
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("{not-json}\n")
	_, _ = f.WriteString(`{"ts":1,"cpu":99}` + "\n")
	_ = f.Close()

	srv.metricsData = nil
	srv.initMetrics()
	if len(srv.metricsData) != 2 {
		t.Fatalf("应恢复 2 个有效点，实际 %d", len(srv.metricsData))
	}
	if srv.metricsData[0].CPU != 11.1 || srv.metricsData[1].CPU != 22.2 {
		t.Fatalf("恢复的数据不正确: %+v", srv.metricsData)
	}
	srv.metricsMu.Lock()
	diskOK := srv.metricsDiskOK
	srv.metricsMu.Unlock()
	if !diskOK {
		t.Fatal("正常目录下不应降级为纯内存")
	}
}

func TestMetricsDiskFallback(t *testing.T) {
	srv := metricsTestServer(t)
	// 用一个已存在的普通文件冒充目录，令 MkdirAll 失败
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	srv.metricsDirOverride = filepath.Join(blocker, "sub")
	srv.initMetrics()
	srv.appendMetric(MetricPoint{TS: time.Now().Unix(), CPU: 33.3})

	srv.metricsMu.Lock()
	data, diskOK := append([]MetricPoint(nil), srv.metricsData...), srv.metricsDiskOK
	srv.metricsMu.Unlock()
	if diskOK {
		t.Fatal("目录不可写时应降级")
	}
	if len(data) != 1 || data[0].CPU != 33.3 {
		t.Fatalf("降级后内存采样仍应可用: %+v", data)
	}
}

func TestMetricsHandler(t *testing.T) {
	srv := metricsTestServer(t)
	now := time.Now()
	for i := 0; i < 12; i++ {
		srv.appendMetric(MetricPoint{
			TS: now.Add(-time.Duration(i) * time.Minute).Unix(), CPU: float64(i),
		})
	}
	// 7 天窗口之外的数据不应出现在 1h 结果中
	srv.appendMetric(MetricPoint{TS: now.Add(-48 * time.Hour).Unix(), CPU: 999})

	rec := httptest.NewRecorder()
	srv.Metrics(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metrics?range=1h", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d", rec.Code)
	}
	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			Points      []MetricPoint `json:"points"`
			Range       string        `json:"range"`
			Total       int           `json:"total"`
			IntervalSec int           `json:"interval_sec"`
			Retention   int           `json:"retention_days"`
			DiskOK      bool          `json:"disk_ok"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.Data.Range != "1h" || resp.Data.Total != 12 {
		t.Fatalf("1h 窗口应返回 12 个点，实际 %+v", resp.Data)
	}
	if resp.Data.IntervalSec != metricsIntervalSec || resp.Data.Retention != metricsRetentionDays {
		t.Fatalf("元信息错误: %+v", resp.Data)
	}
	for _, p := range resp.Data.Points {
		if p.CPU == 999 {
			t.Fatal("48 小时前的数据混入了 1h 窗口")
		}
	}

	rec = httptest.NewRecorder()
	srv.Metrics(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metrics?range=7d", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Range != "7d" || resp.Data.Total != 13 {
		t.Fatalf("7d 窗口应返回 13 个点，实际 %+v", resp.Data)
	}
}

func TestParseMetricsRange(t *testing.T) {
	cases := map[string]time.Duration{
		"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour,
		"7d": 7 * 24 * time.Hour, "": time.Hour, "bogus": time.Hour,
	}
	for in, want := range cases {
		if got := parseMetricsRange(in); got != want {
			t.Errorf("parseMetricsRange(%q)=%v，期望 %v", in, got, want)
		}
	}
}

func TestResourceAlertStateMachine(t *testing.T) {
	srv := metricsTestServer(t)
	now := time.Now()
	const sustain = 300

	// 1) 刚超阈值：只开始计时，不告警
	srv.evaluateAlert("cpu", 80, 90, sustain, now, "CPU 使用率")
	if alertStateOf(srv, "cpu").alerted {
		t.Fatal("刚开始超阈值不应立即告警")
	}
	// 2) 未达持续时长：仍不告警
	srv.evaluateAlert("cpu", 80, 90, sustain, now.Add(299*time.Second), "CPU 使用率")
	if alertStateOf(srv, "cpu").alerted {
		t.Fatal("未达持续时长不应告警")
	}
	// 3) 达到持续时长：触发一次
	srv.evaluateAlert("cpu", 80, 90, sustain, now.Add(301*time.Second), "CPU 使用率")
	st := alertStateOf(srv, "cpu")
	if !st.alerted || st.lastSent.IsZero() {
		t.Fatal("达到持续时长后应触发告警")
	}
	sentAt := st.lastSent
	// 4) 冷却期内持续超阈值，不重复推送
	srv.evaluateAlert("cpu", 80, 95, sustain, now.Add(900*time.Second), "CPU 使用率")
	if alertStateOf(srv, "cpu").lastSent != sentAt {
		t.Fatal("冷却期内重复推送")
	}
	// 5) 阈值下方但仍在滞回带内，不判定恢复
	srv.evaluateAlert("cpu", 80, 78, sustain, now.Add(901*time.Second), "CPU 使用率")
	if !alertStateOf(srv, "cpu").alerted {
		t.Fatal("滞回带内不应解除告警")
	}
	// 6) 低于阈值 5 个百分点，判定恢复
	srv.evaluateAlert("cpu", 80, 74, sustain, now.Add(902*time.Second), "CPU 使用率")
	if alertStateOf(srv, "cpu").alerted {
		t.Fatal("回落后应解除告警")
	}
	// 7) 关闭阈值时清空状态机
	srv.evaluateAlert("cpu", 0, 99, sustain, now.Add(903*time.Second), "CPU 使用率")
	st = alertStateOf(srv, "cpu")
	if st.alerted || !st.since.IsZero() {
		t.Fatal("阈值关闭应清空状态机")
	}
}

func alertStateOf(s *Server, key string) alertTrack {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	if st, ok := s.alertTracks[key]; ok {
		return *st
	}
	return alertTrack{}
}

func TestAlertSettingsAPI(t *testing.T) {
	srv := metricsTestServer(t)

	// 首次保存三项阈值
	body := `{"alert_cpu_pct":85,"alert_mem_pct":90,"alert_sustain_sec":120}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/alerts", strings.NewReader(body))
	srv.SaveAlertSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("保存失败: %d %s", rec.Code, rec.Body.String())
	}
	c := srv.alertConfigSnapshot()
	if c.cpu != 85 || c.mem != 90 || c.disk != 0 || c.sustain != 120 {
		t.Fatalf("阈值未正确保存: %+v", c)
	}

	// 部分更新：只提交磁盘阈值，其余保持不变（指针字段保证不被清零）
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/settings/alerts", strings.NewReader(`{"alert_disk_pct":95}`))
	srv.SaveAlertSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("部分更新失败: %d %s", rec.Code, rec.Body.String())
	}
	c = srv.alertConfigSnapshot()
	if c.cpu != 85 || c.mem != 90 || c.disk != 95 || c.sustain != 120 {
		t.Fatalf("部分更新误清零了其它字段: %+v", c)
	}

	// 非法值应被拒绝且不改动内存配置
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/settings/alerts", strings.NewReader(`{"alert_cpu_pct":150}`))
	srv.SaveAlertSettings(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法阈值应返回 400，实际 %d", rec.Code)
	}
	if srv.alertConfigSnapshot().cpu != 85 {
		t.Fatal("校验失败后不应改动已有配置")
	}

	// GET 返回生效值
	rec = httptest.NewRecorder()
	srv.GetAlertSettings(rec, httptest.NewRequest(http.MethodGet, "/api/v1/settings/alerts", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("读取设置失败: %d", rec.Code)
	}
	var resp struct {
		Data struct {
			CPU     int `json:"alert_cpu_pct"`
			Disk    int `json:"alert_disk_pct"`
			Sustain int `json:"alert_sustain_sec"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.CPU != 85 || resp.Data.Disk != 95 || resp.Data.Sustain != 120 {
		t.Fatalf("GET 返回的生效值不对: %+v", resp.Data)
	}
}

func TestAlertConfigDefaults(t *testing.T) {
	srv := metricsTestServer(t)
	c := srv.alertConfigSnapshot()
	if c.cpu != 0 || c.mem != 0 || c.disk != 0 || c.sustain != defaultAlertSustainSec {
		t.Fatalf("默认应为关闭且持续时长 300 秒，实际 %+v", c)
	}
}
