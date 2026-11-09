//go:build !linux

package sysinfo

// Temperatures 非 Linux 平台无热区信息，返回空。
func Temperatures() []TempZone { return nil }

// CPUFreqs 非 Linux 平台无 /proc/cpuinfo，返回空。
func CPUFreqs() []float64 { return nil }
