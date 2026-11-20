package api

// 应用内通知中心的回归测试：分档、未读计数、已读/清空、环形缓冲。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNotifyLevel(t *testing.T) {
	cases := []struct {
		event, title, want string
	}{
		{"security", "登录失败次数过多，已锁定", notifDanger},
		{"temp", "设备温度过高", notifDanger},
		{"resource", "CPU 使用率超阈值", notifDanger},
		{"health", "应用掉线", notifDanger},
		{"health", "应用已恢复", notifSuccess},
		{"temp", "温度已回落", notifSuccess},
		{"update", "NanoPanel 发现新版本 v1.0", notifWarn},
		{"update", "NanoPanel 已升级到 v1.0", notifSuccess},
		{"update", "NanoPanel 自动升级失败", notifDanger},
		{"app", "应用安装完成", notifSuccess},
		{"app", "应用安装失败", notifDanger},
		{"settings", "Webhook 配置已更新", notifInfo},
	}
	for _, c := range cases {
		if got := notifyLevel(c.event, c.title, ""); got != c.want {
			t.Errorf("notifyLevel(%s, %s) = %s, want %s", c.event, c.title, got, c.want)
		}
	}
}

func TestNotificationCenterAPI(t *testing.T) {
	srv, _ := newTestServer(t)
	// 清空初始状态，避免其它测试的 webhook 调用污染
	srv.notifies = nil

	srv.pushNotify("update", "NanoPanel 发现新版本 v1.0", "可前往关于页升级")
	srv.pushNotify("app", "应用安装完成", "demo")
	srv.pushNotify("security", "登录失败", "IP 1.2.3.4")

	rec := httptest.NewRecorder()
	srv.Notifications(rec, httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态 %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			Items  []notifyItem `json:"items"`
			Unread int          `json:"unread"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.Data.Unread != 3 {
		t.Fatalf("未读数应为 3: %+v", resp.Data)
	}
	if len(resp.Data.Items) != 3 {
		t.Fatalf("应有 3 条通知: %d", len(resp.Data.Items))
	}
	// 最新在前，且档位正确
	if resp.Data.Items[0].Level != notifDanger {
		t.Fatalf("最新应为告警档: %+v", resp.Data.Items[0])
	}
	if resp.Data.Items[2].Level != notifWarn {
		t.Fatalf("最早应为注意档: %+v", resp.Data.Items[2])
	}

	// 全部已读
	rec = httptest.NewRecorder()
	srv.MarkNotificationsRead(rec, httptest.NewRequest(http.MethodPost, "/api/v1/notifications/read",
		strings.NewReader(`{"all":true}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("标记已读失败 %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	srv.Notifications(rec, httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil))
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Data.Unread != 0 {
		t.Fatalf("全部已读后未读数应为 0: %d", resp.Data.Unread)
	}

	// 清空
	rec = httptest.NewRecorder()
	srv.ClearNotifications(rec, httptest.NewRequest(http.MethodPost, "/api/v1/notifications/clear", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("清空失败 %d", rec.Code)
	}
	if len(srv.notifies) != 0 {
		t.Fatalf("清空后应无通知: %d", len(srv.notifies))
	}
}

func TestNotifyRingBuffer(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.notifies = nil
	for i := 0; i < notifyMaxItems+50; i++ {
		srv.pushNotify("settings", "测试通知", "")
	}
	if len(srv.notifies) != notifyMaxItems {
		t.Fatalf("环形缓冲应保留 %d 条，实际 %d", notifyMaxItems, len(srv.notifies))
	}
}
