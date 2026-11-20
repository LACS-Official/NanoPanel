//go:build linux

package sysinfo

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Temperatures 读取全部热区温度（/sys/class/thermal/thermal_zone*）。
// 跳过完全无效（解析失败）的分区；数值异常（<0 或 >150）的分区保留并标记
// suspicious，让前端能明确提示"读数异常"而不是静默隐藏。
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
		if err != nil || math.IsNaN(milli) || math.IsInf(milli, 0) {
			continue
		}
		c := round1(milli / 1000)
		label := "thermal"
		if t, err := os.ReadFile(filepath.Join(dir, "type")); err == nil {
			if v := strings.TrimSpace(string(t)); v != "" {
				label = v
			}
		}
		out = append(out, TempZone{Label: label, Celsius: c, Status: tempStatus(c)})
	}
	// 温度高的排前面，异常值（suspicious）放最后，便于用户一眼看到最需要关注的项。
	sort.SliceStable(out, func(i, j int) bool {
		si := out[i].Status == "suspicious"
		sj := out[j].Status == "suspicious"
		if si != sj {
			return !si
		}
		return out[i].Celsius > out[j].Celsius
	})
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
