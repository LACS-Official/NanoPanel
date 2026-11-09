package sysinfo

import (
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

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
	return o
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
