package sysinfo

import (
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// TempZone 一个热区的温度读数。
type TempZone struct {
	Label   string  `json:"label"` // 如 cpu-thermal / gpu-thermal
	Celsius float64 `json:"celsius"`
	Status  string  `json:"status"` // ok | warm | hot | critical | suspicious
}

// Overview 系统概览数据
type Overview struct {
	Hostname    string  `json:"hostname"`
	OS          string  `json:"os"`
	OSID        string  `json:"os_id"`
	Kernel      string  `json:"kernel"`
	UptimeSec   int64   `json:"uptime_sec"`
	CPUModel    string  `json:"cpu_model"`
	CPUCores    int     `json:"cpu_cores"`    // 逻辑核心数（含超线程），等价于线程数
	CPUPhysical int     `json:"cpu_physical"` // 物理核心数；无法识别时为 0
	CPUSockets  int     `json:"cpu_sockets"`  // 物理插槽数；无法识别时为 0
	CPUArch     string  `json:"cpu_arch"`     // 架构，如 x86_64 / aarch64 / armv7l
	Load1       float64 `json:"load1"`
	Load5       float64 `json:"load5"`
	Load15      float64 `json:"load15"`
	MemTotalMB  int     `json:"mem_total_mb"`
	MemAvailMB  int     `json:"mem_avail_mb"`
	MemUsedPct  float64 `json:"mem_used_pct"`
	DiskTotalMB int     `json:"disk_total_mb"`
	DiskAvailMB int     `json:"disk_avail_mb"`
	DiskUsedPct float64 `json:"disk_used_pct"`
	ServerTime  string  `json:"server_time"`

	// 温度（热区）与 CPU 当前频率；平台不支持时为空。
	Temps    []TempZone `json:"temps,omitempty"`
	CPUFreqs []float64  `json:"cpu_freqs,omitempty"`

	// 实时 CPU 使用率（两次采样差值）与进程总数；平台不支持时为零值。
	CPUUsagePct float64 `json:"cpu_usage_pct"`
	ProcCount   int     `json:"proc_count"`
}

// GetOverview 采集当前系统概览
func GetOverview() Overview {
	o := Overview{ServerTime: time.Now().Format("2006-01-02 15:04:05")}
	o.Hostname, _ = os.Hostname()

	o.OS, o.OSID = readOSRelease()

	if data, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		o.Kernel = strings.TrimSpace(string(data))
	}

	o.UptimeSec = readUptime()
	o.CPUModel = readCPUModel()
	o.CPUCores = runtime.NumCPU()
	o.CPUArch = runtime.GOARCH
	o.CPUPhysical, o.CPUSockets = readCPUTopology()
	o.Load1, o.Load5, o.Load15 = readLoadAvg()

	mt, ma := readMemInfo()
	o.MemTotalMB = mt / 1024
	o.MemAvailMB = ma / 1024
	if mt > 0 {
		o.MemUsedPct = round1(float64(mt-ma) / float64(mt) * 100)
	}

	total, avail := diskUsage("/")
	o.DiskTotalMB = int(total / 1024 / 1024)
	o.DiskAvailMB = int(avail / 1024 / 1024)
	if total > 0 {
		o.DiskUsedPct = round1(float64(total-avail) / float64(total) * 100)
	}
	o.Temps = Temperatures()
	o.CPUFreqs = CPUFreqs()
	o.CPUUsagePct = readCPUUsage()
	o.ProcCount = countProcesses()
	return o
}

// readCPUUsage 通过两次读取 /proc/stat 的差值估算 CPU 使用率（百分比）。
// 非 Linux 或无法读取时返回 0；采样间隔 250ms，仅当首次采样有效才睡眠。
func readCPUUsage() float64 {
	a := readProcStat()
	if a.total == 0 {
		return 0
	}
	time.Sleep(250 * time.Millisecond)
	b := readProcStat()
	if b.total == 0 {
		return 0
	}
	totalDelta := b.total - a.total
	idleDelta := (b.idle + b.iowait) - (a.idle + a.iowait)
	if totalDelta <= 0 {
		return 0
	}
	pct := (1 - float64(idleDelta)/float64(totalDelta)) * 100
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return round1(pct)
}

// readProcStat 解析 /proc/stat 首行 cpu 的累计时间，返回总量与空闲（含 iowait）。
func readProcStat() struct {
	total, idle, iowait int64
} {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return struct {
			total, idle, iowait int64
		}{}
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		f := strings.Fields(line)[1:]
		var v [10]int64
		for i := 0; i < len(f) && i < len(v); i++ {
			v[i], _ = strconv.ParseInt(f[i], 10, 64)
		}
		// 顺序：user nice system idle iowait irq softirq steal guest guest_nice
		total := v[0] + v[1] + v[2] + v[3] + v[4] + v[5] + v[6] + v[7] + v[8] + v[9]
		return struct {
			total, idle, iowait int64
		}{total: total, idle: v[3], iowait: v[4]}
	}
	return struct {
		total, idle, iowait int64
	}{}
}

// countProcesses 统计 /proc 下数字命名目录的数量，即当前进程总数。
func countProcesses() int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() && isDigits(e.Name()) {
			n++
		}
	}
	return n
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func readOSRelease() (pretty, id string) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			pretty = unquote(strings.TrimPrefix(line, "PRETTY_NAME="))
		}
		if strings.HasPrefix(line, "ID=") {
			id = unquote(strings.TrimPrefix(line, "ID="))
		}
	}
	return
}

func readUptime() int64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	f, _ := strconv.ParseFloat(fields[0], 64)
	return int64(f)
}

// readCPUModel 读取 CPU 型号。按优先级兼容不同架构 /proc/cpuinfo 的字段命名：
//   - x86：model name
//   - ARM（树莓派 / 全志 / 瑞芯微等）：Hardware、Processor、model name、cpu model
//   - 部分 ARM64 板卡：CPU model、processor
//
// 取第一个非空且看起来像型号的值；全部缺失时返回一个基于架构的占位说明，
// 保证前端不会出现空白 "-" 或把负载误当成 CPU 信息。
func readCPUModel() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err == nil {
		if model := parseCPUModel(string(data)); model != "" {
			return model
		}
	}
	// cpuinfo 缺失或字段不全时的回退：用架构描述，至少让用户知道是什么平台。
	if a := archLabel(runtime.GOARCH); a != "" {
		return a + "（型号未上报）"
	}
	return "未知 CPU 型号"
}

// parseCPUModel 从 cpuinfo 文本中按字段优先级提取型号，供单测直接验证。
// 注意：x86 的 "processor : 0" 是逻辑 CPU 序号（小写 p），ARM 的 "Processor : xxx"
// 才是型号（大写 P），必须区分大小写；先按字段收集全部值，再按优先级取，避免文件行序影响结果。
func parseCPUModel(data string) string {
	keys := []string{"model name", "Hardware", "Processor", "cpu model", "CPU model", "chip type", "cpu type"}
	values := map[string]string{}
	for _, line := range strings.Split(data, "\n") {
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		val := strings.TrimSpace(line[colon+1:])
		if val == "" {
			continue
		}
		for _, want := range keys {
			if cpuKeyMatches(key, want) {
				if _, exists := values[want]; !exists {
					values[want] = val
				}
				break
			}
		}
	}
	for _, want := range keys {
		if v := values[want]; v != "" {
			return v
		}
	}
	return ""
}

// cpuKeyMatches 判断 cpuinfo 字段名是否匹配目标键（大小写不敏感）。
// 特例：x86 的 "processor : 0" 是逻辑 CPU 序号（全小写），不能当作型号；
// ARM 的 "Processor : xxx" 首字母大写，才是真正的型号字段。
func cpuKeyMatches(key, want string) bool {
	if want == "Processor" && key == "processor" {
		return false
	}
	return strings.EqualFold(key, want)
}

func archLabel(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "386":
		return "x86"
	case "arm64":
		return "ARM64"
	case "arm":
		return "ARM"
	default:
		return goarch
	}
}

// readCPUTopology 从 /proc/cpuinfo 统计物理核心数与插槽数（按 physical id + core id 去重）。
// 字段缺失（很多 ARM 单核 / 小核心板卡不上报）时返回 0，由前端回退到逻辑核心数。
func readCPUTopology() (physical, sockets int) {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0, 0
	}
	type key struct{ phys, core string }
	seen := map[key]bool{}
	sockSet := map[string]bool{}
	haveFields := false
	var curPhys, curCore string
	flush := func() {
		if haveFields && curPhys != "" && curCore != "" {
			seen[key{curPhys, curCore}] = true
			sockSet[curPhys] = true
		}
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			flush()
			curPhys, curCore = "", ""
			continue
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		k := strings.TrimSpace(line[:colon])
		v := strings.TrimSpace(line[colon+1:])
		switch {
		case strings.EqualFold(k, "physical id"):
			curPhys, haveFields = v, true
		case strings.EqualFold(k, "core id"):
			curCore, haveFields = v, true
		}
	}
	flush()
	return len(seen), len(sockSet)
}

func readLoadAvg() (float64, float64, float64) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	f := strings.Fields(string(data))
	if len(f) < 3 {
		return 0, 0, 0
	}
	a, _ := strconv.ParseFloat(f[0], 64)
	b, _ := strconv.ParseFloat(f[1], 64)
	c, _ := strconv.ParseFloat(f[2], 64)
	return a, b, c
}

func readMemInfo() (total, avail int) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	memFree := 0
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.Atoi(fields[1])
		switch fields[0] {
		case "MemTotal:":
			total = val
		case "MemAvailable:":
			avail = val
		case "MemFree:":
			memFree = val
		}
	}
	if avail == 0 {
		avail = memFree
	}
	return
}

func unquote(s string) string {
	return strings.Trim(strings.TrimSpace(s), "\"")
}

// tempStatus 按摄氏度给出展示状态，供前端着色与告警提示。
// 0℃ 以下或 125℃ 以上视为传感器异常值（而非真实高温），单独标记 suspicious。
func tempStatus(c float64) string {
	switch {
	case c < 0 || c > 125:
		return "suspicious"
	case c >= 95:
		return "critical"
	case c >= 80:
		return "hot"
	case c >= 65:
		return "warm"
	default:
		return "ok"
	}
}

// round1 保留一位小数。
func round1(f float64) float64 {
	return math.Round(f*10) / 10
}
