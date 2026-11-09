package api

// 网卡实时流量：读取 /proc/net/dev 的字节计数，与上一次请求的快照
// 求差计算速率。速率随前端轮询推进（面板场景下无人观看时无需采样），
// 第一次请求返回 0 速率与累计字节数。

import (
	"net/http"
	"time"

	"nanopanel/internal/sysinfo"
)

// netSample 单块网卡的采样点。
type netSample struct {
	rx, tx uint64
	at     time.Time
}

// netRateStat 返回给前端的速率数据。
type netRateStat struct {
	Name    string  `json:"name"`
	RxBytes uint64  `json:"rx_bytes"`
	TxBytes uint64  `json:"tx_bytes"`
	RxRate  float64 `json:"rx_rate"` // 字节/秒
	TxRate  float64 `json:"tx_rate"`
}

// TrafficStats 返回各网卡累计流量与相对上次请求的实时速率。
func (s *Server) TrafficStats(w http.ResponseWriter, r *http.Request) {
	stats := sysinfo.NetDevStats()
	if s.netProbe != nil {
		stats = s.netProbe()
	}
	now := time.Now()

	s.netMu.Lock()
	if s.netPrev == nil {
		s.netPrev = make(map[string]netSample)
	}
	out := make([]netRateStat, 0, len(stats))
	for _, st := range stats {
		cur := netRateStat{Name: st.Name, RxBytes: st.RxBytes, TxBytes: st.TxBytes}
		if prev, ok := s.netPrev[st.Name]; ok {
			dt := now.Sub(prev.at).Seconds()
			if dt > 0.2 && dt < 300 { // 过久的间隔视为新会话，不作差
				cur.RxRate = rate(st.RxBytes, prev.rx, dt)
				cur.TxRate = rate(st.TxBytes, prev.tx, dt)
			}
		}
		s.netPrev[st.Name] = netSample{rx: st.RxBytes, tx: st.TxBytes, at: now}
		out = append(out, cur)
	}
	s.netMu.Unlock()
	ok(w, out)
}

// rate 计算字节速率，处理计数器回绕（uint64 溢出极难发生，回绕则归零）。
func rate(cur, prev uint64, dt float64) float64 {
	if cur < prev {
		return 0
	}
	return float64(cur-prev) / dt
}
