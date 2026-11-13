package api

import (
	"strings"
	"testing"
)

func TestDescribeProcessExact(t *testing.T) {
	cases := []struct {
		name, want string
	}{
		{"systemd", "系统与服务管理器"},
		{"sshd", "SSH 远程登录服务"},
		{"nginx", "Nginx Web 服务器"},
		{"nanopanel", "NanoPanel 面板本体"},
		{"dockerd", "Docker 容器引擎"},
		{"python3", "Python 3 解释器"},
	}
	for _, c := range cases {
		got := describeProcess(c.name, c.name)
		if !strings.Contains(got, c.want) {
			t.Errorf("describeProcess(%q) = %q，期望包含 %q", c.name, got, c.want)
		}
	}
}

func TestDescribeProcessKernelThread(t *testing.T) {
	got := describeProcess("kworker/0:1-events", "[kworker/0:1-events]")
	if !strings.Contains(got, "内核线程") {
		t.Errorf("内核线程应被识别，实际 %q", got)
	}
	got = describeProcess("ksoftirqd/0", "ksoftirqd/0")
	if !strings.Contains(got, "软中断") {
		t.Errorf("ksoftirqd 应命中前缀规则，实际 %q", got)
	}
}

func TestDescribeProcessPrefixAndTruncated(t *testing.T) {
	// comm 被内核截断到 15 字符时，应补回字典里更长的名字
	got := describeProcess("systemd-journal", "systemd-journald")
	if !strings.Contains(got, "日志收集") {
		t.Errorf("截断名称应补全匹配，实际 %q", got)
	}
	got = describeProcess("containerd-shim-runc-v2", "containerd-shim-runc-v2")
	if !strings.Contains(got, "容器运行时垫片") {
		t.Errorf("应命中字典精确项，实际 %q", got)
	}
}

func TestDescribeProcessTruncatedComm(t *testing.T) {
	// ps 的 comm 会被内核截断到 15 字符，以下取自 WSL 真实输出
	cases := []struct {
		comm, args, want string
	}{
		{"systemd-resolve", "/usr/lib/systemd/systemd-resolved", "DNS"},
		{"unattended-upgr", "/usr/bin/python3 /usr/share/unattended-upgrades/unattended-upgrade-shutdown", "自动更新"},
		{"journalctl", "/bin/journalctl --boot 0 --dmesg", "日志"},
		{"gnome-remote-de", "/usr/libexec/gnome-remote-desktop-daemon --system", "GNOME"},
		{"NetworkManager", "/usr/sbin/NetworkManager --no-daemon", "网络连接管理"},
	}
	for _, c := range cases {
		got := describeProcess(c.comm, c.args)
		if !strings.Contains(got, c.want) {
			t.Errorf("describeProcess(%q) = %q，期望包含 %q", c.comm, got, c.want)
		}
	}
}

func TestDescribeProcessArgsRefine(t *testing.T) {
	// 通用解释器应依据完整命令行给出更具体的说明
	got := describeProcess("python3", "/usr/bin/python3 -m uvicorn main:app --host 0.0.0.0")
	if !strings.Contains(got, "Uvicorn") {
		t.Errorf("应由 args 推断为 Uvicorn，实际 %q", got)
	}
	got = describeProcess("node", "/usr/bin/node /opt/nanopanel/apps/hello/server.js")
	if !strings.Contains(got, "NanoPanel") {
		t.Errorf("应由 args 推断为面板应用，实际 %q", got)
	}
	comm := describeProcess("python3", "/usr/bin/python3 /srv/backup.py")
	if !strings.Contains(comm, "Python 3 解释器") {
		t.Errorf("无特征时应回落到字典说明，实际 %q", comm)
	}
}

func TestDescribeProcessNormalize(t *testing.T) {
	// 路径、大小写、尾部参数都应规范化
	got := describeProcess("NGINX", "/usr/sbin/nginx -g daemon off;")
	if !strings.Contains(got, "Nginx") {
		t.Errorf("大小写/路径规范化失败，实际 %q", got)
	}
	got = describeProcess("/usr/sbin/sshd", "/usr/sbin/sshd -D")
	if !strings.Contains(got, "SSH") {
		t.Errorf("带路径的 comm 应取 basename，实际 %q", got)
	}
}

func TestDescribeProcessUnknown(t *testing.T) {
	got := describeProcess("my-custom-daemon-xyz", "/opt/mine/my-custom-daemon-xyz --serve")
	if !strings.Contains(got, "未收录") || !strings.Contains(got, "ps -p") {
		t.Errorf("未知进程应给出可自助排查的兜底说明，实际 %q", got)
	}
	// 兜底也要能利用命令行关键字
	got = describeProcess("my-wrapper", "/opt/mine/wrapper kubectl apply -f x.yaml")
	if !strings.Contains(got, "Kubernetes") {
		t.Errorf("未知进程应尝试 args 关键字，实际 %q", got)
	}
}
