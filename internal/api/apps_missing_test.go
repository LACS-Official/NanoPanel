package api

// 应用清单缺失时应优雅返回空列表，而不是 500（新安装/Windows 开发环境常见）。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestListAppsMissingManifestReturnsEmpty(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.cfg.ManifestPath = filepath.Join(t.TempDir(), "nonexistent", "manifest.json")

	rec := httptest.NewRecorder()
	srv.ListApps(rec, httptest.NewRequest(http.MethodGet, "/api/v1/apps", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("清单缺失应返回 200，实际 %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK   bool      `json:"ok"`
		Data []AppMeta `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.Data == nil || len(resp.Data) != 0 {
		t.Fatalf("应返回空列表，实际: %+v", resp)
	}
}

func TestListAppsInvalidManifestReturns500(t *testing.T) {
	srv, _ := newTestServer(t)
	dir := t.TempDir()
	bad := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(bad, []byte("not-json"), 0644); err != nil {
		t.Fatal(err)
	}
	srv.cfg.ManifestPath = bad

	rec := httptest.NewRecorder()
	srv.ListApps(rec, httptest.NewRequest(http.MethodGet, "/api/v1/apps", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("清单格式错误应返回 500，实际 %d", rec.Code)
	}
}
