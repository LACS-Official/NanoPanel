//go:build linux

package sysinfo

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Temperatures 读取全部热区温度（/sys/class/thermal/thermal_zone*）。
// 读不到或值异常（≤0 或 ≥150）的分区被跳过；无热区时返回空。
func Temperatures() []TempZone {
	matches, err := filepath.Glob("/sys/class/thermal/thermal_zone*")
	if err != nil {
		return nil
	}
	out := make([]TempZone, 0, len(matches))
	for _, dir := range matches {
		raw, err := os.ReadFile(filepath.Join(dir, "temp"))
		if err != nil {
			continue
		}
		milli, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
		if err != nil {
			continue
		}
		c := milli / 1000
		if c <= 0 || c >= 150 {
			continue
		}
		label := "thermal"
		if t, err := os.ReadFile(filepath.Join(dir, "type")); err == nil {
			if v := strings.TrimSpace(string(t)); v != "" {
				label = v
			}
		}
		out = append(out, TempZone{Label: label, Celsius: round1(c)})
	}
	return out
}

// CPUFreqs 读取每个逻辑核心的当前频率（MHz），来自 /proc/cpuinfo。
// 返回空表示该平台不可用。
func CPUFreqs() []float64 {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return nil
	}
	var out []float64
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "cpu MHz") && !strings.HasPrefix(line, "clock") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		mhz, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err != nil || mhz <= 0 {
			continue
		}
		out = append(out, round1(mhz))
	}
	return out
}
