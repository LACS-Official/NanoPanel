package api

import (
	"path"
	"strings"
)

// processDescriptions 是「进程名 -> 用途说明」的内置字典。
// 面板面向轻量运维场景，说明全部在本地生成，不依赖网络、LLM 或外部服务。
// key 统一使用小写；匹配时会先对 ps 的 comm 做去路径、小写化。
var processDescriptions = map[string]string{
	// —— 内核与初始化 ——
	"systemd":    "系统与服务管理器（1 号进程），负责开机启动并托管所有服务，请勿结束",
	"init":       "系统初始化进程（1 号进程），所有用户进程的祖先",
	"kthreadd":   "内核线程管理器（2 号进程），负责派生其它内核线程",
	"ksoftirqd":  "内核软中断处理线程，分担网络/磁盘中断的后续处理",
	"migration":  "内核 CPU 负载均衡线程，负责在核心间迁移任务",
	"watchdog":   "内核看门狗线程，用于检测系统是否卡死",
	"irqbalance": "多核中断负载均衡服务，把硬件中断分散到不同 CPU",
	"rcu_sched":  "内核 RCU 同步辅助线程",
	"rcu_bh":     "内核 RCU 软中断辅助线程",

	// —— 系统基础服务 ——
	"sshd":                    "SSH 远程登录服务，处理远程终端与文件传输连接",
	"ssh":                     "SSH 客户端，本机的远程登录会话",
	"dropbear":                "轻量 SSH 服务（常见于嵌入式/软路由设备）",
	"cron":                    "定时任务调度器，按 crontab 周期执行脚本",
	"crond":                   "定时任务调度器，按 crontab 周期执行脚本",
	"atd":                     "一次性定时任务服务",
	"systemd-journald":        "systemd 日志收集服务，journalctl 的数据来源",
	"systemd-logind":          "用户登录会话与电源事件管理服务",
	"systemd-resolved":        "systemd DNS 域名解析服务",
	"systemd-networkd":        "systemd 网络配置服务",
	"systemd-timesyncd":       "systemd 网络时间同步服务",
	"systemd-udevd":           "设备事件管理服务，处理硬件热插拔",
	"udevd":                   "设备事件管理服务，处理硬件热插拔",
	"systemd-hostnamed":       "主机名管理服务",
	"dbus-daemon":             "D-Bus 进程间通信总线，桌面与服务程序靠它互相调用",
	"dbus":                    "D-Bus 进程间通信总线",
	"networkmanager":          "网络连接管理服务，负责网卡、Wi-Fi 与拨号",
	"rsyslogd":                "系统日志服务，写入 /var/log 下的日志文件",
	"syslog-ng":               "系统日志服务",
	"polkitd":                 "权限策略服务，处理普通用户的提权授权",
	"udisksd":                 "磁盘与存储设备管理守护进程",
	"agetty":                  "物理/串口终端的登录提示（TTY）",
	"getty":                   "物理/串口终端的登录提示（TTY）",
	"login":                   "终端登录会话进程",
	"dhclient":                "DHCP 客户端，自动获取 IP 地址",
	"dhcpcd":                  "DHCP 客户端，自动获取 IP 地址",
	"dnsmasq":                 "轻量 DNS/DHCP 服务，常用于本地域名解析与软路由",
	"wpa_supplicant":          "Wi-Fi 连接与认证服务",
	"chronyd":                 "NTP 网络时间同步服务",
	"ntpd":                    "NTP 网络时间同步服务",
	"rngd":                    "随机数熵源服务，补充系统随机池",
	"haveged":                 "随机数熵源服务，补充系统随机池",
	"snapd":                   "Snap 软件包管理服务",
	"acpid":                   "电源与 ACPI 事件服务",
	"supervisord":             "进程守护与管理工具，负责拉起并看护子进程",
	"pm2":                     "Node.js 进程管理器，负责守护与重启 Node 服务",
	"containerd-shim-runc-v2": "容器运行时垫片，维持单个容器的生命周期",

	// —— Shell 与常用命令 ——
	"bash":                "Bash Shell，交互式终端或脚本解释器",
	"sh":                  "Shell 命令解释器，交互式终端或脚本",
	"dash":                "Dash Shell，常用于系统脚本",
	"zsh":                 "Zsh Shell，交互式终端",
	"fish":                "Fish Shell，交互式终端",
	"sudo":                "提权执行命令（临时以其它用户身份运行）",
	"su":                  "切换用户身份的命令",
	"sleep":               "延时等待，通常是脚本里的计时/轮询",
	"curl":                "网络请求/下载工具",
	"wget":                "网络下载工具",
	"git":                 "Git 版本控制命令",
	"tar":                 "打包/解包工具",
	"rsync":               "文件同步与增量备份工具",
	"ffmpeg":              "音视频转码与处理工具",
	"vim":                 "终端文本编辑器",
	"nano":                "终端文本编辑器",
	"tmux":                "终端复用器，断开连接后仍保持会话",
	"screen":              "终端复用器，断开连接后仍保持会话",
	"top":                 "实时系统资源监控工具",
	"htop":                "实时系统资源监控工具（增强版）",
	"ps":                  "查看当前进程列表的命令",
	"apt":                 "Debian/Ubuntu 软件包管理命令",
	"dpkg":                "Debian/Ubuntu 底层包管理命令",
	"make":                "项目构建工具（按 Makefile 执行编译）",
	"gcc":                 "C/C++ 编译器",
	"openssl":             "TLS 证书与加密命令行工具",
	"logger":              "向系统日志写入一条消息的命令行工具",
	"timeout":             "给命令设置最长运行时间的工具",
	"journalctl":          "查看 systemd 系统日志的命令行工具",
	"unattended-upgrades": "Ubuntu 无人值守自动更新服务，自动安装安全补丁",
	"packagekitd":         "PackageKit 软件包更新与安装服务",
	"apt-daily":           "系统每日自动检查/安装更新的定时任务",

	// —— Web / 数据库 / 中间件 ——
	"nginx":        "Nginx Web 服务器 / 反向代理，对外提供网站与转发",
	"apache2":      "Apache HTTP Web 服务器",
	"httpd":        "Apache HTTP Web 服务器",
	"caddy":        "Caddy Web 服务器，自带自动 HTTPS 证书",
	"php-fpm":      "PHP FastCGI 进程管理器，为 Web 服务器执行 PHP",
	"mysqld":       "MySQL 数据库服务进程",
	"mariadbd":     "MariaDB 数据库服务进程",
	"postgres":     "PostgreSQL 数据库服务进程",
	"postmaster":   "PostgreSQL 数据库主进程",
	"redis-server": "Redis 内存数据库 / 缓存服务",
	"mongod":       "MongoDB 文档数据库服务",
	"memcached":    "Memcached 内存缓存服务",
	"haproxy":      "HAProxy 负载均衡 / 反向代理",
	"traefik":      "Traefik 反向代理与负载均衡",
	"vsftpd":       "FTP 文件传输服务",
	"smbd":         "Samba 文件共享服务（Windows 网上邻居）",
	"nmbd":         "Samba 名称解析与浏览服务",
	"coredns":      "CoreDNS 域名解析服务",
	"etcd":         "etcd 分布式键值存储，常用作 K8s 配置中心",

	// —— 容器与编排 ——
	"docker":       "Docker 命令行客户端，用于管理容器与镜像",
	"dockerd":      "Docker 容器引擎守护进程，管理镜像与容器",
	"docker-proxy": "Docker 端口映射代理，把宿主机端口转发到容器",
	"containerd":   "容器运行时，Docker/K8s 的底层容器管理组件",
	"runc":         "OCI 容器运行工具，负责真正启动容器",
	"podman":       "Podman 容器引擎",
	"kubelet":      "Kubernetes 节点代理，负责在本机拉起 Pod",
	"kube-proxy":   "Kubernetes 网络代理，维护 Service 转发规则",

	// —— 语言运行时 ——
	"node":     "Node.js 运行时，通常是一个 JavaScript 服务或构建脚本",
	"python":   "Python 解释器，运行脚本或后端服务",
	"python3":  "Python 3 解释器，运行脚本或后端服务",
	"java":     "Java 虚拟机，运行 Java / Spring 等应用",
	"php":      "PHP 解释器，运行 PHP 脚本或应用",
	"ruby":     "Ruby 解释器，运行 Ruby 脚本或应用",
	"go":       "Go 工具链或 Go 编写的程序",
	"dotnet":   ".NET 运行时，运行 C# 应用",
	"bun":      "Bun JavaScript 运行时 / 包管理器",
	"deno":     "Deno JavaScript / TypeScript 运行时",
	"npm":      "Node.js 包管理器，安装依赖或执行脚本",
	"yarn":     "Node.js 包管理器",
	"pnpm":     "Node.js 包管理器",
	"pip":      "Python 包管理器",
	"uvicorn":  "Python Uvicorn ASGI 服务器，托管 FastAPI / Starlette 应用",
	"gunicorn": "Python Gunicorn WSGI 服务器，托管 Django / Flask 应用",

	// —— 代理 / 组网 / 面板生态 ——
	"frps":         "frp 内网穿透服务端，接收外部流量并转发",
	"frpc":         "frp 内网穿透客户端，把本机服务暴露到公网",
	"mihomo":       "Mihomo（Clash.Meta）代理核心，负责分流与翻墙规则",
	"clash":        "Clash 代理核心，负责分流与规则匹配",
	"sing-box":     "sing-box 代理核心",
	"xray":         "Xray 代理核心",
	"v2ray":        "V2Ray 代理核心",
	"adguardhome":  "AdGuard Home，全网广告与跟踪域名过滤 DNS 服务",
	"tailscaled":   "Tailscale 组网客户端，组建异地虚拟局域网",
	"zerotier-one": "ZeroTier 组网客户端，组建异地虚拟局域网",
	"openvpn":      "OpenVPN VPN 服务",
	"wg-quick":     "WireGuard VPN 快速配置工具",
	"picoclaw":     "PicoClaw 应用（可通过本面板安装）",
	"webdav":       "WebDAV 文件共享服务",
	"nanopanel":    "NanoPanel 面板本体，就是当前这个管理系统，请勿结束",
	"npctl":        "NanoPanel 命令行管理工具",
}

// processPrefixRules 处理带编号/后缀的进程名，例如 kworker/0:1-events、
// containerd-shim-<id>、systemd-journal 等。按顺序匹配，命中即返回。
var processPrefixRules = []struct{ prefix, desc string }{
	{"kworker", "内核工作线程，处理中断、定时器等异步内核任务"},
	{"ksoftirqd", "内核软中断处理线程，分担网络/磁盘中断的后续处理"},
	{"migration/", "内核 CPU 负载均衡线程，负责在核心间迁移任务"},
	{"rcu", "内核 RCU 同步辅助线程"},
	{"containerd-shim", "容器运行时垫片，维持单个容器的生命周期"},
	{"docker-containerd", "Docker 内置容器运行时"},
	{"systemd-journal", "systemd 日志收集服务，journalctl 的数据来源"},
}

// processFallbackPrefixRules 是泛化前缀，放在「截断名补全」之后才生效，
// 否则会挡住 systemd-resolve → systemd-resolved 这类更具体的推断。
var processFallbackPrefixRules = []struct{ prefix, desc string }{
	{"systemd-", "systemd 系统服务组件"},
	{"gnome-", "GNOME 桌面环境组件"},
	{"dbus-", "D-Bus 相关服务进程"},
	{"docker-", "Docker 相关组件进程"},
}

// argRefineNames 中的进程名过于笼统（只知道是解释器，或需要区分服务与会话），
// 会优先用完整命令行推断出更具体的用途。
var argRefineNames = map[string]bool{
	"node": true, "python": true, "python3": true, "java": true,
	"php": true, "ruby": true, "sh": true, "bash": true, "sshd": true,
}

// processArgRules 用完整命令行（args）里的关键字细化说明。
// 顺序即优先级：越具体的规则放在越前面。
var processArgRules = []struct{ keyword, desc string }{
	{"/opt/nanopanel/apps/", "通过 NanoPanel 面板安装的第三方应用进程"},
	{"uvicorn", "Python Uvicorn ASGI 服务（FastAPI / Starlette 应用）"},
	{"gunicorn", "Python Gunicorn WSGI 服务（Django / Flask 应用）"},
	{"manage.py", "Django 项目的管理脚本 / 开发服务器"},
	{"celery", "Celery 分布式任务队列 worker"},
	{"jupyter", "Jupyter Notebook / Lab 交互式计算服务"},
	{"next dev", "Next.js 前端开发服务器"},
	{"vite", "Vite 前端开发服务器 / 构建工具"},
	{"webpack", "Webpack 前端打包构建进程"},
	{"npm run", "npm 脚本启动的 Node.js 任务"},
	{"pm2", "由 PM2 守护的 Node.js 服务"},
	{"spring", "Java Spring / Spring Boot 应用服务"},
	{"tomcat", "Java Tomcat Web 应用服务器"},
	{"docker-compose", "Docker Compose 容器编排命令"},
	{"kubectl", "Kubernetes 集群管理命令"},
	{"apt-get", "系统软件包安装 / 升级任务"},
	{"rsync", "文件同步 / 备份任务"},
	{"unattended-upgrad", "系统无人值守自动更新任务（自动安装安全补丁）"},
	{"sshd:", "SSH 远程登录会话子进程，对应一个在线终端连接"},
}

const unknownProcessDesc = "未收录的进程（自定义或第三方程序）；可在终端执行 ps -p <PID> -o args= 查看完整命令行"

// describeProcess 依据 ps 的 comm（进程名）与 args（完整命令行）返回一句用途说明。
// 纯本地字典推断，不发起任何网络请求，因此对离线/内网设备同样可用。
func describeProcess(comm, args string) string {
	name := normalizeProcName(comm)
	argLine := strings.ToLower(strings.TrimSpace(args))

	// 内核线程在 ps 里通常带方括号；也可能只在 args 里出现。
	if strings.HasPrefix(name, "[") || strings.HasPrefix(argLine, "[") {
		return "Linux 内核线程（内核内部任务，非用户程序，通常不应结束）"
	}
	if name == "" {
		name = normalizeProcName(firstArg(args))
	}

	// 1) 通用解释器 / 需区分会话的进程，优先用命令行细化，
	// 避免只显示「Python 解释器」这种没有信息量的说明。
	if argRefineNames[name] {
		if d, ok := matchArgRule(argLine); ok {
			return d
		}
		if d, ok := processDescriptions[name]; ok {
			return d
		}
	}

	// 2) 精确匹配
	if d, ok := processDescriptions[name]; ok {
		return d
	}
	// 3) 前缀规则（带编号、带实例后缀的进程）
	for _, r := range processPrefixRules {
		if strings.HasPrefix(name, r.prefix) {
			return r.desc
		}
	}
	// 4) comm 被内核截断（最长 15 字符）时，尝试补回字典里更长的名字。
	// 多个候选时取最短 key，保证结果确定、不受 map 遍历顺序影响。
	if len(name) >= 6 {
		best, desc := "", ""
		for k, d := range processDescriptions {
			if strings.HasPrefix(k, name) && len(k)-len(name) <= 6 && (best == "" || len(k) < len(best)) {
				best, desc = k, d
			}
		}
		if best != "" {
			return desc
		}
	}
	// 5) 泛化前缀兜底（systemd-*、gnome-* 等未收录组件）
	for _, r := range processFallbackPrefixRules {
		if strings.HasPrefix(name, r.prefix) {
			return r.desc
		}
	}
	// 6) 未识别进程：最后再从命令行关键字推断一次
	if d, ok := matchArgRule(argLine); ok {
		return d
	}
	return unknownProcessDesc
}

func matchArgRule(argLine string) (string, bool) {
	for _, r := range processArgRules {
		if strings.Contains(argLine, r.keyword) {
			return r.desc, true
		}
	}
	return "", false
}

// normalizeProcName 去掉路径、参数与大小写差异，得到可用于查字典的进程名。
func normalizeProcName(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if i := strings.IndexAny(v, " \t"); i > 0 {
		v = v[:i]
	}
	// 只对绝对路径取 basename：内核线程名如 kworker/0:1-events、
	// ksoftirqd/0 本身含斜杠，直接 path.Base 会被错误截成 "0"。
	if strings.HasPrefix(v, "/") {
		v = path.Base(v)
	}
	return strings.ToLower(v)
}

// firstArg 取完整命令行的第一个字段（可执行文件路径）。
func firstArg(args string) string {
	f := strings.Fields(strings.TrimSpace(args))
	if len(f) == 0 {
		return ""
	}
	return f[0]
}
