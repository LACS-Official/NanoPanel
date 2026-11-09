package api

// 备份与定时任务测试。

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// makeBackupTar 手工构造一个备份流。
func makeBackupTar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

// TestBackupAndRestore 导出 → 修改 → 恢复闭环。
func TestBackupAndRestore(t *testing.T) {
	srv, _ := newTestServer(t)
	dir := filepath.Dir(srv.configPath)
	appsDir := t.TempDir()
	srv.mu.Lock()
	srv.cfg.AppsDir = appsDir
	srv.mu.Unlock()

	// 准备应用配置
	if err := os.MkdirAll(filepath.Join(appsDir, "mihomo"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appsDir, "mihomo", "config.env"),
		[]byte("mixed_port=7890\n"), 0600); err != nil {
		t.Fatal(err)
	}

	// 1) 导出
	rec := httptest.NewRecorder()
	srv.Backup(rec, httptest.NewRequest(http.MethodGet, "/api/v1/backup", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("备份失败: %d", rec.Code)
	}
	backup := rec.Body.Bytes()

	// 2) 清空应用配置
	if err := os.RemoveAll(filepath.Join(appsDir, "mihomo")); err != nil {
		t.Fatal(err)
	}

	// 3) 恢复（真实 multipart 请求体）
	req := newRestoreRequest(t, "/api/v1/restore?apply_config=0", backup)
	rec = httptest.NewRecorder()
	srv.Restore(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("恢复失败: %d %s", rec.Code, rec.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(appsDir, "mihomo", "config.env"))
	if err != nil || string(data) != "mixed_port=7890\n" {
		t.Fatalf("应用配置未恢复: %q %v", data, err)
	}
	// apply_config=0：面板配置只落盘
	if _, err := os.Stat(filepath.Join(dir, "config.json.restored")); err != nil {
		t.Fatalf("面板配置应落盘为 .restored: %v", err)
	}
}

// newRestoreRequest 构造带备份文件的 multipart POST 请求。
func newRestoreRequest(t *testing.T, url string, body []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "nanopanel-backup.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, url, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// TestRestoreRejectsInvalid 损坏的文件必须被拒绝。
func TestRestoreRejectsInvalid(t *testing.T) {
	srv, _ := newTestServer(t)
	req := newRestoreRequest(t, "/api/v1/restore", []byte("this is not gzip"))
	rec := httptest.NewRecorder()
	srv.Restore(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("损坏备份应 400: %d", rec.Code)
	}
}

// TestCronScheduleValidate 调度表达式校验与到期判断。
func TestCronScheduleValidate(t *testing.T) {
	for _, good := range []string{"every:30m", "every:5m", "hourly:0", "hourly:59", "daily:03:30", "daily:23:59"} {
		if !validSchedule(good) {
			t.Errorf("%q 应合法", good)
		}
	}
	for _, bad := range []string{"every:3m", "every:30", "hourly:60", "daily:24:00", "daily:3", "weekly"} {
		if validSchedule(bad) {
			t.Errorf("%q 应不合法", bad)
		}
	}

	// every:5m 到期判断
	state := &cronState{lastTrigger: map[string]time.Time{}, lastDaily: map[string]string{}}
	task := CronTask{ID: "t1", Schedule: "every:5m"}
	now := time.Now()
	if cronDue(state, task, now) {
		t.Fatal("首次检查只建立基准，不应触发")
	}
	if cronDue(state, task, now.Add(2*time.Minute)) {
		t.Fatal("未满周期不应触发")
	}
	if !cronDue(state, task, now.Add(6*time.Minute)) {
		t.Fatal("满周期应触发")
	}

	// daily:03:30 当天只触发一次
	d := &cronState{lastTrigger: map[string]time.Time{}, lastDaily: map[string]string{}}
	daily := CronTask{ID: "d1", Schedule: "daily:03:30"}
	at := time.Date(2026, 11, 9, 4, 0, 0, 0, time.Local)
	if !cronDue(d, daily, at) {
		t.Fatal("已过 03:30 应触发")
	}
	if cronDue(d, daily, at.Add(time.Hour)) {
		t.Fatal("同一天不应重复触发")
	}
}

// TestCronSaveAndRun 保存任务 → 立即执行 → 结果记录。
func TestCronSaveAndRun(t *testing.T) {
	srv, _ := newTestServer(t)

	// 保存
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cron", strings.NewReader(
		`{"name":"测试任务","schedule":"daily:03:30","command":"echo hello-cron","enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.SaveCron(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("保存任务失败: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Tasks []CronTask `json:"tasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data.Tasks) != 1 {
		t.Fatalf("应有 1 个任务: %s", rec.Body.String())
	}
	id := resp.Data.Tasks[0].ID

	// 立即执行（echo 一定成功）
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cron/run", strings.NewReader(`{"id":"`+id+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.RunCronNow(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ok") {
		t.Fatalf("立即执行失败: %d %s", rec.Code, rec.Body.String())
	}

	// 结果持久化
	tasks := srv.loadCronTasks()
	if tasks[0].LastStatus != "ok" || !strings.Contains(tasks[0].LastOutput, "hello-cron") {
		t.Fatalf("执行结果未记录: %+v", tasks[0])
	}

	// 删除
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cron/delete", strings.NewReader(`{"id":"`+id+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.DeleteCron(rec, req)
	if rec.Code != http.StatusOK || len(srv.loadCronTasks()) != 0 {
		t.Fatalf("删除失败: %d %s", rec.Code, rec.Body.String())
	}
}
