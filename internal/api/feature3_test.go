package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nanopanel/internal/config"
)

// TestListDisksSmoke 磁盘列表端点在任何平台都应返回可用结构。
func TestListDisksSmoke(t *testing.T) {
	srv, _ := newAppTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/disks", nil)
	rec := httptest.NewRecorder()
	srv.ListDisks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		OK   bool `json:"ok"`
		Data struct {
			Devices []DiskEntry       `json:"devices"`
			Mounts  []StandaloneMount `json:"mounts"`
			Lsblk   bool              `json:"lsblk"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK {
		t.Fatal("ok=false")
	}
	if got.Data.Devices == nil || got.Data.Mounts == nil {
		t.Fatal("devices/mounts 必须是数组而非 null")
	}
}

// TestDiskMountValidation 挂载端点的输入校验。
func TestDiskMountValidation(t *testing.T) {
	srv, _ := newAppTestServer(t)

	cases := []struct {
		name string
		body string
		code int
	}{
		{"非法设备路径", `{"device":"/dev/../etc/passwd","mountpoint":"/mnt/x"}`, http.StatusBadRequest},
		{"相对路径挂载点", `{"device":"/dev/sdb1","mountpoint":"mnt/x"}`, http.StatusBadRequest},
		{"穿越挂载点", `{"device":"/dev/sdb1","mountpoint":"/mnt/../etc"}`, http.StatusBadRequest},
		{"不存在的设备", `{"device":"/dev/nanopanel-no-such-dev","mountpoint":"/mnt/x"}`, http.StatusBadRequest},
		{"坏 JSON", `{`, http.StatusBadRequest},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/disks/mount", strings.NewReader(c.body))
		rec := httptest.NewRecorder()
		srv.DiskMount(rec, req)
		if rec.Code != c.code {
			t.Errorf("%s: HTTP %d, 期望 %d, body=%s", c.name, rec.Code, c.code, rec.Body.String())
		}
	}
}

// TestDiskUmountValidation 卸载端点的输入校验。
func TestDiskUmountValidation(t *testing.T) {
	srv, _ := newAppTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/disks/umount", strings.NewReader(`{"target":"/mnt/not-mounted-anywhere"}`))
	rec := httptest.NewRecorder()
	srv.DiskUmount(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未挂载目标应返回 404，得到 %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/disks/umount", strings.NewReader(`{"target":"!!bad path!!"}`))
	rec = httptest.NewRecorder()
	srv.DiskUmount(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法路径应返回 400，得到 %d", rec.Code)
	}
}

// TestValidMountPoint 覆盖挂载点规范化。
func TestValidMountPoint(t *testing.T) {
	if p, err := validMountPoint("media/usb"); err != nil || p != "/media/usb" {
		t.Fatalf("相对路径应规范化为绝对: %q, %v", p, err)
	}
	if _, err := validMountPoint("/mnt/../../etc"); err == nil {
		t.Fatal("包含 .. 的路径应被拒绝")
	}
}

// newStreamTestServer 构造一个脚本会输出多行并成功退出的测试应用。
func newStreamTestServer(t *testing.T) *Server {
	t.Helper()
	srv, _ := newAppTestServer(t)
	appDir := filepath.Join(srv.cfg.AppsDir, "demo")
	script := "#!/bin/sh\necho step-1\necho step-2\n"
	if err := os.WriteFile(filepath.Join(appDir, "install.sh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	failScript := "#!/bin/sh\necho before-fail\nexit 3\n"
	if err := os.WriteFile(filepath.Join(appDir, "remove.sh"), []byte(failScript), 0755); err != nil {
		t.Fatal(err)
	}
	return srv
}

// TestAppActionStreamSuccess SSE 流式安装：逐行输出 + done ok。
func TestAppActionStreamSuccess(t *testing.T) {
	srv := newStreamTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/app/action/stream?id=demo&action=install", nil)
	rec := httptest.NewRecorder()
	srv.AppActionStream(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type 应为 text/event-stream: %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"event":"start"`, `"event":"out","line":"step-1"`, `"event":"out","line":"step-2"`,
		`"event":"done"`, `"ok":true`, `"state":"installed"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("响应缺少 %s\n完整响应: %s", want, body)
		}
	}
}

// TestAppActionStreamFailure 脚本失败时 done.ok=false 且带错误。
func TestAppActionStreamFailure(t *testing.T) {
	srv := newStreamTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/app/action/stream?id=demo&action=remove", nil)
	rec := httptest.NewRecorder()
	srv.AppActionStream(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("SSE 失败也应是 200 流，得到 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"ok":false`) {
		t.Errorf("脚本 exit 3 应产生 ok=false: %s", body)
	}
	if !strings.Contains(body, "before-fail") {
		t.Errorf("失败前的输出应已推送: %s", body)
	}
}

// TestAppActionStreamRejectsBadInput 参数校验走 JSON 错误响应而非 SSE。
func TestAppActionStreamRejectsBadInput(t *testing.T) {
	srv := newStreamTestServer(t)
	for _, q := range []string{"?id=bad%20id&action=install", "?id=demo&action=purge", "?id=missing&action=install"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/app/action/stream"+q, nil)
		rec := httptest.NewRecorder()
		srv.AppActionStream(rec, req)
		if rec.Code == http.StatusOK {
			t.Errorf("%s 不应进入 SSE 流", q)
		}
	}
}

// TestAppActionStreamConcurrentGuard 同一应用并发操作应被 409 拦截。
func TestAppActionStreamConcurrentGuard(t *testing.T) {
	srv := newStreamTestServer(t)
	if !srv.tryBeginAction("demo") {
		t.Fatal("首次抢占应成功")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/app/action/stream?id=demo&action=install", nil)
	rec := httptest.NewRecorder()
	srv.AppActionStream(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("并发操作应返回 409，得到 %d: %s", rec.Code, rec.Body.String())
	}
	srv.endAction("demo")
	// 释放后应可继续
	req = httptest.NewRequest(http.MethodPost, "/api/v1/app/action/stream?id=demo&action=install", nil)
	rec = httptest.NewRecorder()
	srv.AppActionStream(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("释放后应可执行，得到 %d", rec.Code)
	}
}

// TestDiskNotifyEvent disk 应是合法的订阅事件。
func TestDiskNotifyEvent(t *testing.T) {
	if !config.ValidNotifyEvent("disk") {
		t.Fatal("disk 应是合法通知事件")
	}
	found := false
	for _, o := range notifyEventOptions() {
		if o["key"] == "disk" {
			found = true
		}
	}
	if !found {
		t.Fatal("前端事件选项里缺少 disk")
	}
}
