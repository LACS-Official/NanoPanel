//go:build !linux

package sysinfo

// NetIface 一块网卡的流量统计。
type NetIface struct {
	Name    string `json:"name"`
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
}

// NetDevStats 非 Linux 平台无 /proc/net/dev，返回空。
func NetDevStats() []NetIface { return nil }
