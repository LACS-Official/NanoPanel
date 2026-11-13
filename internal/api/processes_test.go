package api

import (
	"strings"
	"testing"
)

// 样本取自 WSL 上真实的 `ps -eo pid,user,pcpu,rss,comm,args --no-headers --sort=-rss`：
// 含带参数的完整命令行、被截断到 15 字符的 comm、内核线程与仅 comm 无参数的行。
const psSample = `    208 root      0.0 36992 snapd           /usr/lib/snapd/snapd
    424 root      1.2 22272 unattended-upgr /usr/bin/python3 /usr/share/unattended-upgrades/unattended-upgrade-shutdown --wait-for-signal
    253 root      3.4 17504 NetworkManager  /usr/sbin/NetworkManager --no-daemon
      1 root      0.1 13048 systemd         /sbin/init
    173 systemd+  0.0 12800 systemd-resolve /usr/lib/systemd/systemd-resolved
    213 root      0.2 12780 udisksd         /usr/libexec/udisks2/udisksd
     42 root      0.0     0 kworker/0:1     [kworker/0:1-events]
    500 admin     8.6  2048 nanopanel      /opt/nanopanel/nanopanel -c /etc/nanopanel/config.json
`

func TestParseProcessList(t *testing.T) {
	list := parseProcessList([]byte(psSample))
	if len(list) != 8 {
		t.Fatalf("应解析出 8 行，实际 %d", len(list))
	}

	// 第一行：六列字段拆分正确（新增的 CPU% 不能串位）
	if list[0].PID != 208 || list[0].User != "root" || list[0].CPUPct != 0 ||
		list[0].RSSKB != 36992 || list[0].Comm != "snapd" {
		t.Errorf("首行字段错误: %+v", list[0])
	}
	if list[7].CPUPct != 8.6 {
		t.Errorf("CPU%% 解析错误: %+v", list[7])
	}
	if !strings.Contains(list[0].Desc, "Snap") {
		t.Errorf("snapd 说明错误: %q", list[0].Desc)
	}

	// 带空格的完整命令行应完整保留在 args，且不被误当成 comm
	if !strings.Contains(list[1].Args, "--wait-for-signal") {
		t.Errorf("args 丢失参数: %q", list[1].Args)
	}
	if list[1].Comm != "unattended-upgr" {
		t.Errorf("comm 应为 ps 给出的截断名，实际 %q", list[1].Comm)
	}
	if !strings.Contains(list[1].Desc, "自动更新") {
		t.Errorf("截断名应补全为自动更新服务，实际 %q", list[1].Desc)
	}

	// 内核线程
	if !strings.Contains(list[6].Desc, "内核线程") {
		t.Errorf("内核线程说明错误: %q", list[6].Desc)
	}

	// 面板自身 + 完整命令行
	if !strings.Contains(list[7].Desc, "NanoPanel") {
		t.Errorf("面板进程说明错误: %q", list[7].Desc)
	}
	if !strings.Contains(list[7].Args, "/etc/nanopanel/config.json") {
		t.Errorf("面板命令行未保留: %q", list[7].Args)
	}

	// 每一行都必须给出非空的说明
	for _, p := range list {
		if p.Desc == "" {
			t.Errorf("PID %d 缺少说明", p.PID)
		}
	}
}

func TestParseProcessListMax100(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 130; i++ {
		b.WriteString("  123 root 0.5 1024 bash /bin/bash -c sleep 100\n")
	}
	list := parseProcessList([]byte(b.String()))
	if len(list) != 100 {
		t.Fatalf("应最多返回 100 条，实际 %d", len(list))
	}
}

func TestParseProcessListEmpty(t *testing.T) {
	list := parseProcessList([]byte("\n  \n"))
	if len(list) != 0 {
		t.Fatalf("空输出应返回空列表，实际 %d", len(list))
	}
}
