//go:build linux

package sysinfo

import (
	"os"
	"strconv"
	"strings"
)

// NetIface 一块网卡的流量统计。
type NetIface struct {
	Name     string `json:"name"`
	RxBytes  uint64 `json:"rx_bytes"`
	TxBytes  uint64 `json:"tx_bytes"`
}

// NetDevStats 读取 /proc/net/dev 的各网卡收发字节计数。
// 物理回环 lo 不包含（对图表无意义）。
func NetDevStats() []NetIface {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return nil
	}
	out := make([]NetIface, 0, 8)
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if i < 2 { // 头两行是表头
			continue
		}
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "lo" {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(parts[1]))
		if len(fields) < 10 {
			continue
		}
		rx, err1 := strconv.ParseUint(fields[0], 10, 64)
		tx, err2 := strconv.ParseUint(fields[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, NetIface{Name: name, RxBytes: rx, TxBytes: tx})
	}
	return out
}
