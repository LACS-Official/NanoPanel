package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMirrorCandidates 返回白名单候选与当前源 id。
func TestMirrorCandidates(t *testing.T) {
	srv, _ := newTestServer(t)
	t.Setenv("GITHUB_MIRROR", "https://ghfast.top/https://github.com")
	rec := httptest.NewRecorder()
	srv.MirrorCandidates(rec, httptest.NewRequest(http.MethodGet, "/api/v1/settings/mirror/candidates", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态 %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			Candidates []mirrorCandidate `json:"candidates"`
			CurrentID  string            `json:"current_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || len(resp.Data.Candidates) < 4 {
		t.Fatalf("候选源数量不足: %+v", resp.Data)
	}
	if resp.Data.CurrentID != "ghfast" {
		t.Fatalf("current_id = %s, want ghfast", resp.Data.CurrentID)
	}
}

// TestSelectMirrorSource 校验白名单、写配置、更新内存环境变量。
func TestSelectMirrorSource(t *testing.T) {
	srv, _ := newTestServer(t)
	conf := filepath.Join(t.TempDir(), "mirror.conf")
	t.Setenv("NP_MIRROR_CONF", conf)
	t.Setenv("GITHUB_MIRROR", "https://github.com")

	// 未知 id 应拒绝
	rec := httptest.NewRecorder()
	srv.SelectMirrorSource(rec, httptest.NewRequest(http.MethodPost, "/api/v1/settings/mirror/select",
		strings.NewReader(`{"id":"evil"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 id 应 400, got %d", rec.Code)
	}

	// 合法切换
	rec = httptest.NewRecorder()
	srv.SelectMirrorSource(rec, httptest.NewRequest(http.MethodPost, "/api/v1/settings/mirror/select",
		strings.NewReader(`{"id":"ghproxy"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("切换失败 %d: %s", rec.Code, rec.Body.String())
	}
	if got := os.Getenv("GITHUB_MIRROR"); got != "https://ghproxy.net/https://github.com" {
		t.Fatalf("内存环境未更新: %s", got)
	}
	data, err := os.ReadFile(conf)
	if err != nil {
		t.Fatal("配置文件未写入:", err)
	}
	if !strings.Contains(string(data), "GITHUB_MIRROR=https://ghproxy.net/https://github.com") {
		t.Fatalf("配置内容异常: %s", data)
	}
	// 不应残留 .tmp
	if _, err := os.Stat(conf + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("临时文件未清理")
	}
}

// TestWriteMirrorConfPreservesOtherKeys 切换 GitHub 源时保留 APT 源配置。
func TestWriteMirrorConfPreservesOtherKeys(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "mirror.conf")
	t.Setenv("NP_MIRROR_CONF", conf)
	orig := "# 原注释\nGITHUB_MIRROR=https://old\nAPT_MIRROR=https://apt.mirror\nSELECTED_AT=old\n"
	if err := os.WriteFile(conf, []byte(orig), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeMirrorConf(mirrorCandidate{ID: "github", Name: "GitHub 官方", Prefix: "https://github.com"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(conf)
	if !strings.Contains(string(data), "APT_MIRROR=https://apt.mirror") {
		t.Fatalf("APT 源配置丢失: %s", data)
	}
	if strings.Contains(string(data), "SELECTED_AT=old") {
		t.Fatalf("旧 SELECTED_AT 未更新: %s", data)
	}
}
