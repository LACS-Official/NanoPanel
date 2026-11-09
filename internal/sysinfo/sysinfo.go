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
}

// Overview 系统概览数据
type Overview struct {
	Hostname    string  `json:"hostname"`
	OS          string  `json:"os"`
	OSID        string  `json:"os_id"`
	Kernel      string  `json:"kernel"`
	UptimeSec   int64   `json:"uptime_sec"`
	CPUModel    string  `json:"cpu_model"`
	CPUCores    int     `json:"cpu_cores"`
	Load1       float64 `json:"load1"`
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
	o.Load1, _, _ = readLoadAvg()

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

func readCPUModel() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Hardware") || strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
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

func round1(f float64) float64 {
	return math.Round(f*10) / 10
}
