package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveWebhookFormat(t *testing.T) {
	cases := []struct {
		url        string
		userFormat string
		want       string
	}{
		// 用户显式选择具体格式时一律尊重。
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc", "dingtalk", "dingtalk"},
		{"https://example.com/hook", "feishu", "feishu"},
		// 未指定（generic）且命中已知平台域名时自动纠正。
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc", "generic", "wecom"},
		{"https://oapi.dingtalk.com/robot/send?access_token=x", "generic", "dingtalk"},
		{"https://open.feishu.cn/open-apis/bot/v2/hook/x", "generic", "feishu"},
		{"https://sctapi.ftqq.com/SCTxxx.send", "generic", "serverchan"},
		// 自定义域名保持 generic。
		{"https://example.com/my-hook", "generic", "generic"},
		{"https://example.com/my-hook", "", "generic"},
	}
	for _, c := range cases {
		got := resolveWebhookFormat(c.url, c.userFormat)
		if got != c.want {
			t.Errorf("resolveWebhookFormat(%q,%q) = %q, want %q", c.url, c.userFormat, got, c.want)
		}
	}
}

func TestCheckWebhookBizError(t *testing.T) {
	mk := func(status int, body string) *http.Response {
		rec := httptest.NewRecorder()
		rec.Code = status
		rec.Body = bytes.NewBufferString(body)
		return rec.Result()
	}
	// 企业微信业务错误（HTTP 200 但 errcode 非 0）必须被识别为失败。
	resp := mk(200, `{"errcode":93000,"errmsg":"invalid webhook url"}`)
	if err := checkWebhookBizError("wecom", resp); err == nil {
		t.Error("期望 wecom 业务错误被识别，但返回 nil")
	}
	// 企业微信成功（errcode=0）不报错。
	resp = mk(200, `{"errcode":0,"errmsg":"ok"}`)
	if err := checkWebhookBizError("wecom", resp); err != nil {
		t.Errorf("期望 wecom 成功，但报错: %v", err)
	}
	// 飞书成功 / 失败。
	resp = mk(200, `{"code":0,"msg":"success"}`)
	if err := checkWebhookBizError("feishu", resp); err != nil {
		t.Errorf("期望 feishu 成功，但报错: %v", err)
	}
	resp = mk(200, `{"code":19001,"msg":"params error"}`)
	if err := checkWebhookBizError("feishu", resp); err == nil {
		t.Error("期望 feishu 业务错误被识别，但返回 nil")
	}
	// 非 JSON 响应（自定义端点）视作成功。
	resp = mk(200, `OK`)
	if err := checkWebhookBizError("generic", resp); err != nil {
		t.Errorf("期望非 JSON 响应视作成功，但报错: %v", err)
	}
	// HTTP 错误仍由上层处理，body 校验不应误报。
	resp = mk(500, `{"errcode":0}`)
	if err := checkWebhookBizError("wecom", resp); err != nil {
		t.Errorf("期望 HTTP 错误不在此处处理，但报错: %v", err)
	}
}

func TestPostWebhookGenericKeepsFormat(t *testing.T) {
	// 自定义域名的 generic 选择应保持不变（不误纠），载荷为通用结构。
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(200)
		w.Write([]byte(`ok`))
	}))
	defer srv.Close()

	if err := postWebhook(srv.URL, "generic", "settings", "t", "c"); err != nil {
		t.Fatalf("postWebhook 失败: %v", err)
	}
	if gotBody["msgtype"] != nil {
		t.Errorf("自定义域名不应被纠正为平台载荷，实际: %v", gotBody)
	}
	if gotBody["title"] != "t" {
		t.Errorf("期望通用载荷含 title，实际: %v", gotBody)
	}
}
