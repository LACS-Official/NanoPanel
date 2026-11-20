package api

// 升级日志流与下载节点择优的回归测试。

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpdateNodesDedup(t *testing.T) {
	t.Setenv("GITHUB_MIRROR", "https://github.com/")
	t.Setenv("GITHUB_MIRROR_NAME", "自定义官方源")
	nodes := updateNodes()
	count := 0
	for _, n := range nodes {
		if n.Base == "https://github.com" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("同一个源 URL 应只出现一次，实际 %d 次: %+v", count, nodes)
	}
	if len(nodes) < len(githubMirrorCandidates) {
		t.Fatalf("候选节点数量不足: %d", len(nodes))
	}
}

func TestProbeUpdateNodesOrdersByLatency(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(90 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer slow.Close()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer fast.Close()

	nodes := []updateNode{
		{ID: "slow", Name: "慢节点", Base: slow.URL},
		{ID: "fast", Name: "快节点", Base: fast.URL},
	}
	logs := []string{}
	got := probeUpdateNodes("0.2.1", "nanopanel-amd64", nodes,
		func(f string, a ...interface{}) { logs = append(logs, fmt.Sprintf(f, a...)) })

	if len(got) != 2 || got[0].ID != "fast" {
		t.Fatalf("应按延迟升序排列，快节点在前，实际: %+v", got)
	}
	if !got[0].OK || !got[1].OK {
		t.Fatalf("两个节点都应标记为可用: %+v", got)
	}
	if len(logs) == 0 || !strings.Contains(strings.Join(logs, "\n"), "探测结果") {
		t.Fatalf("探测过程应输出日志，实际: %v", logs)
	}
}

func TestDownloadReleaseAssetFallback(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("binary-content"))
	}))
	defer good.Close()

	nodes := []updateNode{
		{ID: "bad", Name: "坏节点", Base: bad.URL},
		{ID: "good", Name: "好节点", Base: good.URL},
	}
	dest := filepath.Join(t.TempDir(), "asset")
	logs := []string{}
	s := &Server{}
	used, err := s.downloadReleaseAsset("0.2.1", "nanopanel-amd64", dest, nodes,
		func(f string, a ...interface{}) { logs = append(logs, fmt.Sprintf(f, a...)) })
	if err != nil {
		t.Fatalf("应在第二个节点下载成功: %v", err)
	}
	if used != "好节点" {
		t.Fatalf("应返回实际使用的节点名，实际 %q", used)
	}
	data, err := os.ReadFile(dest)
	if err != nil || string(data) != "binary-content" {
		t.Fatalf("下载内容不正确: %q, err=%v", string(data), err)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "切换下一个") || !strings.Contains(joined, "好节点") {
		t.Fatalf("日志应记录节点切换与实际使用节点，实际: %s", joined)
	}
}

func TestFmtBytes(t *testing.T) {
	cases := map[int64]string{
		0:        "0 B",
		1023:     "1023 B",
		1024:     "1.0 KB",
		10485760: "10.0 MB",
		-1:       "?",
	}
	for in, want := range cases {
		if got := fmtBytes(in); got != want {
			t.Fatalf("fmtBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

// 回归：latestReleaseTag 返回的是去掉 v 的纯版本号，早期代码直接拼进下载路径，
// 导致所有节点返回 404（GitHub 真实 tag 为 v0.2.2）。以下测试锁定 URL 中的 v 前缀。
func TestReleaseTag(t *testing.T) {
	cases := map[string]string{
		"0.2.2":   "v0.2.2",
		"v0.2.2":  "v0.2.2",
		"V0.2.3":  "v0.2.3",
		" 0.2.2 ": "v0.2.2",
		"":        "",
	}
	for in, want := range cases {
		if got := releaseTag(in); got != want {
			t.Fatalf("releaseTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProbeUpdateNodesUsesVersionedTag(t *testing.T) {
	paths := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths <- r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	probeUpdateNodes("0.2.2", "nanopanel-linux-arm64.tar.gz",
		[]updateNode{{ID: "x", Name: "测试节点", Base: srv.URL}}, nil)

	want := "/" + updateRepo + "/releases/download/v0.2.2/nanopanel-linux-arm64.tar.gz"
	select {
	case got := <-paths:
		if got != want {
			t.Fatalf("探测路径应为 %s，实际 %s", want, got)
		}
	default:
		t.Fatal("未发起任何探测请求")
	}
}

func TestDownloadReleaseAssetUsesVersionedTag(t *testing.T) {
	paths := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths <- r.URL.Path
		_, _ = w.Write([]byte("binary"))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "asset")
	s := &Server{}
	if _, err := s.downloadReleaseAsset("0.2.2", "nanopanel-arm64", dest,
		[]updateNode{{ID: "x", Name: "测试节点", Base: srv.URL}}, nil); err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	want := "/" + updateRepo + "/releases/download/v0.2.2/nanopanel-arm64"
	select {
	case got := <-paths:
		if got != want {
			t.Fatalf("下载路径应为 %s，实际 %s", want, got)
		}
	default:
		t.Fatal("未发起任何下载请求")
	}
}
