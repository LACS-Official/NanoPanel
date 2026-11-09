//go:build !linux

package sysinfo

// diskUsage 在非 Linux 平台返回零值，仅用于保证可编译
func diskUsage(path string) (total, avail uint64) {
	return 0, 0
}
