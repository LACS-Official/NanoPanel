//go:build !linux

package sysinfo

// diskUsage 在非 Linux 平台返回零值，仅用于保证可编译
func diskUsage(path string) (total, avail uint64) {
	return 0, 0
}

// DiskUsage 是 diskUsage 的对外导出，供磁盘管理页使用。
func DiskUsage(path string) (total, avail uint64) {
	return diskUsage(path)
}
