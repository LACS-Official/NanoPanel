package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"nanopanel/internal/config"
)

// webhookHTTPClient 通知推送专用客户端，严格超时避免阻塞。
var webhookHTTPClient = &http.Client{Timeout: 6 * time.Second}

// webhookSnapshot 取一份 webhook 配置的只读快照，避免与改配置并发竞争。
func (s *Server) webhookSnapshot() (url, format string, events []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.WebhookURL, s.cfg.WebhookFormat, append([]string(nil), s.cfg.WebhookEvents...)
}

// eventEnabled 判断该事件类别是否已订阅。
func eventEnabled(events []string, event string) bool {
	for _, e := range events {
		if e == event {
			return true
		}
	}
	return false
}

// notifyAsync 按当前配置异步推送 webhook 通知。
// 面板操作不应该被慢速的 webhook 端点拖住，因此始终异步执行。
func (s *Server) notifyAsync(event, title, content string) {
	url, format, events := s.webhookSnapshot()
	if url == "" || !eventEnabled(events, event) {
		return
	}
	go func() {
		if err := postWebhook(url, format, event, title, content); err != nil {
			log.Printf("webhook 通知发送失败: %v", err)
		}
	}()
}

// postWebhook 按指定格式构造并推送通知。
func postWebhook(url, format, event, title, content string) error {
	body, err := buildWebhookPayload(format, event, title, content)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "NanoPanel-Webhook/1.0")
	resp, err := webhookHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook 端点返回 HTTP %d", resp.StatusCode)
	}
	return nil
}

// buildWebhookPayload 针对不同平台构造消息体。
func buildWebhookPayload(format, event, title, content string) ([]byte, error) {
	host, _ := os.Hostname()
	stamp := time.Now().Format("2006-01-02 15:04:05")
	full := title + "\n" + content
	switch format {
	case "wecom":
		md := fmt.Sprintf("**%s**\n%s\n\n> %s · NanoPanel @ %s", title, content, stamp, host)
		return json.Marshal(map[string]interface{}{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"content": md,
			},
		})
	case "dingtalk":
		md := fmt.Sprintf("#### %s\n\n%s\n\n%s · NanoPanel @ %s", title, content, stamp, host)
		return json.Marshal(map[string]interface{}{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"title": title,
				"text":  md,
			},
		})
	case "feishu":
		return json.Marshal(map[string]interface{}{
			"msg_type": "text",
			"content":  map[string]string{"text": full + "\n" + stamp + " · NanoPanel @ " + host},
		})
	case "serverchan":
		return json.Marshal(map[string]string{
			"title": title,
			"desp":  content + "\n\n" + stamp + " · " + event,
		})
	default: // generic
		return json.Marshal(map[string]string{
			"title":   title,
			"content": content,
			"event":   event,
			"host":    host,
			"time":    stamp,
		})
	}
}

// webhookReq 设置页提交的 webhook 配置。
type webhookReq struct {
	URL    string   `json:"url"`
	Format string   `json:"format"`
	Events []string `json:"events"`
}

// GetWebhook 返回当前 webhook 配置与可选项。
func (s *Server) GetWebhook(w http.ResponseWriter, r *http.Request) {
	url, format, events := s.webhookSnapshot()
	if format == "" {
		format = "generic"
	}
	ok(w, map[string]interface{}{
		"url":           url,
		"format":        format,
		"events":        events,
		"formats":       config.WebhookFormats,
		"event_options": notifyEventOptions(),
		"configured":    url != "",
	})
}

// SaveWebhook 保存 webhook 配置并持久化。
func (s *Server) SaveWebhook(w http.ResponseWriter, r *http.Request) {
	var req webhookReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL != "" {
		if len(req.URL) > 2048 ||
			(!strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://")) {
			fail(w, http.StatusBadRequest, "Webhook 地址不合法，需以 http(s):// 开头")
			return
		}
	}
	if req.Format == "" {
		req.Format = "generic"
	}
	if !config.ValidWebhookFormat(req.Format) {
		fail(w, http.StatusBadRequest, "不支持的通知格式")
		return
	}
	for _, e := range req.Events {
		if !config.ValidNotifyEvent(e) {
			fail(w, http.StatusBadRequest, "未知的事件类别: "+e)
			return
		}
	}

	s.mu.Lock()
	oldURL, oldFmt, oldEvents := s.cfg.WebhookURL, s.cfg.WebhookFormat, s.cfg.WebhookEvents
	s.cfg.WebhookURL = req.URL
	s.cfg.WebhookFormat = req.Format
	s.cfg.WebhookEvents = append([]string(nil), req.Events...)
	if err := s.cfg.Save(s.configPath); err != nil {
		s.cfg.WebhookURL, s.cfg.WebhookFormat, s.cfg.WebhookEvents = oldURL, oldFmt, oldEvents
		s.mu.Unlock()
		fail(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	s.mu.Unlock()

	s.audit(r, "settings", "保存 Webhook 配置", req.Format)
	s.notifyAsync("settings", "Webhook 配置已更新", "通知格式: "+req.Format)
	ok(w, map[string]string{"msg": "已保存"})
}

// TestWebhook 立即推送一条测试通知，同步返回结果。
func (s *Server) TestWebhook(w http.ResponseWriter, r *http.Request) {
	url, format, _ := s.webhookSnapshot()
	if url == "" {
		fail(w, http.StatusBadRequest, "尚未配置 Webhook 地址")
		return
	}
	host, _ := os.Hostname()
	err := postWebhook(url, format, "settings", "NanoPanel 测试通知",
		"这是一条来自 NanoPanel 的测试通知。\n主机: "+host)
	if err != nil {
		s.audit(r, "settings", "Webhook 测试失败", err.Error())
		fail(w, http.StatusBadGateway, "发送失败: "+err.Error())
		return
	}
	s.audit(r, "settings", "Webhook 测试通知", "格式 "+format)
	ok(w, map[string]string{"msg": "测试通知已发送"})
}
