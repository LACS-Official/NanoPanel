//go:build linux

package sysinfo

import "syscall"

// diskUsage 返回路径所在文件系统的总字节数和可用字节数
func diskUsage(path string) (total, avail uint64) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return 0, 0
	}
	total = fs.Blocks * uint64(fs.Bsize)
	avail = fs.Bavail * uint64(fs.Bsize)
	return
}

// DiskUsage 是 diskUsage 的对外导出，供磁盘管理页使用。
func DiskUsage(path string) (total, avail uint64) {
	return diskUsage(path)
}
