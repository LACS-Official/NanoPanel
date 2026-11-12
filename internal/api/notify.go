package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
// 面板操作不应该被慢速的 webhook 端点拖住，因此始终异步执行；
// 发送失败时按指数退避自动重试，缓解端点抖动导致的通知丢失。
func (s *Server) notifyAsync(event, title, content string) {
	url, format, events := s.webhookSnapshot()
	if url == "" || !eventEnabled(events, event) {
		return
	}
	go func() {
		var lastErr error
		for i := 0; i < webhookMaxAttempts; i++ {
			if i > 0 {
				time.Sleep(webhookRetryBackoff << (i - 1))
			}
			if lastErr = postWebhook(url, format, event, title, content); lastErr == nil {
				return
			}
		}
		log.Printf("webhook 通知发送失败（已重试 %d 次）: %v", webhookMaxAttempts, lastErr)
	}()
}

const (
	webhookMaxAttempts  = 3
	webhookRetryBackoff = 2 * time.Second // 2s → 4s
)

// resolveWebhookFormat 在尊重用户显式选择的前提下，对未指定（generic）且 URL 命中
// 已知平台域名的场景自动纠正，避免企业微信 / 钉钉 / 飞书 / Server酱 机器人因格式错配
// 而静默失败。用户已显式选择具体格式时一律尊重其选择。
func resolveWebhookFormat(url, userFormat string) string {
	if userFormat != "" && userFormat != "generic" {
		return userFormat
	}
	u := strings.ToLower(url)
	switch {
	case strings.Contains(u, "qyapi.weixin.qq.com"):
		return "wecom"
	case strings.Contains(u, "oapi.dingtalk.com"):
		return "dingtalk"
	case strings.Contains(u, "open.feishu.cn") || strings.Contains(u, "larksuite.com"):
		return "feishu"
	case strings.Contains(u, "sc.ftqq.com") || strings.Contains(u, "sctapi.ftqq.com"):
		return "serverchan"
	default:
		if userFormat == "" {
			return "generic"
		}
		return userFormat
	}
}

// postWebhook 按解析后的格式构造并推送通知。
// format 解析遵循 resolveWebhookFormat：用户已选具体格式时尊重之，否则按 URL 域名识别。
func postWebhook(url, userFormat, event, title, content string) error {
	format := resolveWebhookFormat(url, userFormat)
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
	// 企业微信 / 钉钉 / 飞书 / Server酱 等平台对非法载荷返回 HTTP 200 但业务码非 0，
	// 必须解析响应体，否则会误判为成功而静默丢失通知。
	if berr := checkWebhookBizError(format, resp); berr != nil {
		return berr
	}
	return nil
}

// checkWebhookBizError 读取响应体，对已知平台校验业务层状态码。
// 非 JSON 或不含已知错误码的响应一律视作成功（兼容自定义端点）。
func checkWebhookBizError(format string, resp *http.Response) error {
	var m struct {
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
		Code    int    `json:"code"`
		Msg     string `json:"msg"`
		Errno   int    `json:"errno"`
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil || len(body) == 0 {
		return nil
	}
	if jsonErr := json.Unmarshal(body, &m); jsonErr != nil {
		return nil
	}
	switch format {
	case "wecom", "dingtalk":
		if m.Errcode != 0 {
			return fmt.Errorf("webhook 端点业务错误: errcode=%d %s", m.Errcode, m.Errmsg)
		}
	case "feishu", "serverchan":
		if m.Code != 0 {
			return fmt.Errorf("webhook 端点业务错误: code=%d %s", m.Code, m.Msg)
		}
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
	// URL 命中已知平台但用户未显式选格式时，自动纠正，避免静默失配。
	resolved := resolveWebhookFormat(req.URL, req.Format)
	autoFixed := resolved != req.Format
	s.cfg.WebhookURL = req.URL
	s.cfg.WebhookFormat = resolved
	s.cfg.WebhookEvents = append([]string(nil), req.Events...)
	if err := s.cfg.Save(s.configPath); err != nil {
		s.cfg.WebhookURL, s.cfg.WebhookFormat, s.cfg.WebhookEvents = oldURL, oldFmt, oldEvents
		s.mu.Unlock()
		fail(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	s.mu.Unlock()

	s.audit(r, "settings", "保存 Webhook 配置", resolved)
	s.notifyAsync("settings", "Webhook 配置已更新", "通知格式: "+resolved)
	msg := "已保存"
	if autoFixed {
		msg = "已保存，并自动识别为「" + resolved + "」格式"
	}
	ok(w, map[string]interface{}{"msg": msg, "format": resolved})
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
