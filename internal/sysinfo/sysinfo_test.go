package sysinfo

import (
	"runtime"
	"testing"
)

func TestGetOverviewNeverPanics(t *testing.T) {
	// 非 Linux 开发机上大部分字段为零值，但采集过程必须安全完成。
	o := GetOverview()
	if o.ServerTime == "" {
		t.Fatal("ServerTime 不应为空")
	}
	if o.CPUCores != runtime.NumCPU() {
		t.Fatalf("CPUCores = %d, 期望 %d", o.CPUCores, runtime.NumCPU())
	}
	if o.MemUsedPct < 0 || o.MemUsedPct > 100 || o.DiskUsedPct < 0 || o.DiskUsedPct > 100 {
		t.Fatalf("百分比越界: mem=%v disk=%v", o.MemUsedPct, o.DiskUsedPct)
	}
	if o.CPUUsagePct < 0 || o.CPUUsagePct > 100 {
		t.Fatalf("CPU 使用率越界: %v", o.CPUUsagePct)
	}
}

func TestRound1(t *testing.T) {
	cases := map[float64]float64{1.24: 1.2, 1.25: 1.3, 0: 0, 99.99: 100}
	for in, want := range cases {
		if got := round1(in); got != want {
			t.Errorf("round1(%v) = %v, 期望 %v", in, got, want)
		}
	}
}

func TestUnquote(t *testing.T) {
	if got := unquote(`  "Ubuntu 24.04 LTS"  `); got != "Ubuntu 24.04 LTS" {
		t.Fatalf("unquote 结果异常: %q", got)
	}
	if got := unquote(`ubuntu`); got != "ubuntu" {
		t.Fatalf("unquote 结果异常: %q", got)
	}
}
