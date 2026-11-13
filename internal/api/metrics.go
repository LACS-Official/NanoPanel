package api

import (
	"bufio"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"nanopanel/internal/sysinfo"
)

// 历史指标：后台定时采样，JSONL 追加落盘 + 内存缓存，供概览页绘制趋势图。
// 不引入任何第三方存储：数据量小（7 天 × 每分钟 1 点 ≈ 1 万行、约 1 MB），
// 单文件顺序追加足够，且保持「单二进制、零依赖」的部署特性。
const (
	metricsIntervalSec   = 60
	metricsRetentionDays = 7
	metricsMaxPoints     = 240 // 单次 API 返回的最多点数，前端渲染压力可控
	metricsFile          = "metrics.jsonl"
	metricsTrimEvery     = 60 // 每写 60 个点裁剪一次过期数据
)

// MetricPoint 是一个采样点。NetRx/NetTx 是网卡累计字节数（排除 lo），
// 前端用相邻两点差值除以时间差换算成速率。
type MetricPoint struct {
	TS    int64   `json:"ts"`
	CPU   float64 `json:"cpu"`
	Mem   float64 `json:"mem"`
	Disk  float64 `json:"disk"`
	Temp  float64 `json:"temp"`
	NetRx uint64  `json:"net_rx"`
	NetTx uint64  `json:"net_tx"`
}

// defaultMetricsDir 是历史指标的数据目录。非 Linux（开发/测试）落到临时目录。
func defaultMetricsDir() string {
	if runtime.GOOS == "linux" {
		return "/var/lib/nanopanel"
	}
	return filepath.Join(os.TempDir(), "nanopanel-metrics")
}

// metricsDir 返回生效目录；测试可通过 metricsDirOverride 注入临时路径。
func (s *Server) metricsDir() string {
	if s.metricsDirOverride != "" {
		return s.metricsDirOverride
	}
	return defaultMetricsDir()
}

func (s *Server) metricsPath() string {
	return filepath.Join(s.metricsDir(), metricsFile)
}

// initMetrics 从磁盘恢复最近 7 天的采样点。文件不存在、损坏或目录不可写时
// 只影响持久化，内存采样仍然可用，面板不会因此启动失败。
func (s *Server) initMetrics() {
	path := s.metricsPath()
	cutoff := time.Now().Add(-metricsRetentionDays * 24 * time.Hour).Unix()

	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	s.metricsData = nil
	s.metricsDiskOK = true

	f, err := os.Open(path)
	if err != nil {
		if err := os.MkdirAll(s.metricsDir(), 0750); err != nil {
			s.metricsDiskOK = false
			log.Printf("历史指标目录不可用，改为仅内存保存: %v", err)
		}
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var p MetricPoint
		if json.Unmarshal([]byte(line), &p) != nil || p.TS < cutoff {
			continue // 跳过损坏行与过期点
		}
		s.metricsData = append(s.metricsData, p)
	}
	if err := sc.Err(); err != nil {
		log.Printf("读取历史指标出错，已保留可读部分: %v", err)
	}
}

// collectMetric 采集一次当前系统状态。CPU 使用率由 sysinfo.GetOverview 内部
// 通过两次 /proc/stat 差值得到（约 250ms），在后台 goroutine 中执行不阻塞请求。
func collectMetric() MetricPoint {
	o := sysinfo.GetOverview()
	maxTemp := 0.0
	for _, z := range o.Temps {
		if z.Celsius > maxTemp {
			maxTemp = z.Celsius
		}
	}
	var rx, tx uint64
	for _, st := range sysinfo.NetDevStats() {
		if st.Name == "lo" {
			continue
		}
		rx += st.RxBytes
		tx += st.TxBytes
	}
	return MetricPoint{
		TS:    time.Now().Unix(),
		CPU:   o.CPUUsagePct,
		Mem:   o.MemUsedPct,
		Disk:  o.DiskUsedPct,
		Temp:  round1Metric(maxTemp),
		NetRx: rx,
		NetTx: tx,
	}
}

func round1Metric(f float64) float64 {
	return float64(int64(f*10+0.5)) / 10
}

// appendMetric 把一个采样点写入内存缓存与磁盘。磁盘失败时降级为纯内存，
// 只打印一次日志，避免每 60 秒刷屏。
func (s *Server) appendMetric(p MetricPoint) {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()

	s.metricsData = append(s.metricsData, p)
	if !s.metricsDiskOK {
		return
	}
	if err := os.MkdirAll(s.metricsDir(), 0750); err != nil {
		s.disableMetricDisk(err)
		return
	}
	f, err := os.OpenFile(s.metricsPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		s.disableMetricDisk(err)
		return
	}
	line, err := json.Marshal(p)
	if err == nil {
		_, err = f.Write(append(line, '\n'))
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		s.disableMetricDisk(err)
		return
	}
	s.metricsWrites++
	if s.metricsWrites%metricsTrimEvery == 0 {
		s.trimMetricsLocked()
	}
}

// disableMetricDisk 必须在持有 metricsMu 时调用。
func (s *Server) disableMetricDisk(err error) {
	if s.metricsDiskOK {
		log.Printf("历史指标无法写入磁盘，改为仅内存保存: %v", err)
	}
	s.metricsDiskOK = false
}

// trimMetricsLocked 重写文件，丢弃超过保留期的采样点。调用方须持有 metricsMu。
func (s *Server) trimMetricsLocked() {
	cutoff := time.Now().Add(-metricsRetentionDays * 24 * time.Hour).Unix()
	kept := s.metricsData[:0]
	for _, p := range s.metricsData {
		if p.TS >= cutoff {
			kept = append(kept, p)
		}
	}
	s.metricsData = append([]MetricPoint(nil), kept...)

	tmp := s.metricsPath() + ".tmp"
	f, err := os.OpenFile(tmp, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	w := bufio.NewWriter(f)
	for i := range s.metricsData {
		line, merr := json.Marshal(s.metricsData[i])
		if merr != nil {
			continue
		}
		if _, werr := w.Write(append(line, '\n')); werr != nil {
			_ = f.Close()
			return
		}
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return
	}
	if err := f.Close(); err != nil {
		return
	}
	if err := os.Rename(tmp, s.metricsPath()); err != nil {
		_ = os.Remove(tmp)
	}
}

// collectAndStore 采样、持久化并评估阈值告警。
func (s *Server) collectAndStore() {
	p := collectMetric()
	s.appendMetric(p)
	s.checkResourceAlerts(p)
}

// StartMetrics 启动历史指标采样循环，随 ctx 退出。
// 采样独立于「应用健康检测」开关：即使关掉自动检测，历史趋势仍应持续记录。
func (s *Server) StartMetrics(ctx context.Context) {
	s.initMetrics()
	go func() {
		s.collectAndStore()
		ticker := time.NewTicker(metricsIntervalSec * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.collectAndStore()
			}
		}
	}()
}

// Metrics 返回指定时间窗内的历史采样点（降采样至最多 metricsMaxPoints 个点）。
func (s *Server) Metrics(w http.ResponseWriter, r *http.Request) {
	win := parseMetricsRange(r.URL.Query().Get("range"))
	since := time.Now().Add(-win).Unix()

	s.metricsMu.Lock()
	all := append([]MetricPoint(nil), s.metricsData...)
	diskOK := s.metricsDiskOK
	s.metricsMu.Unlock()

	points := make([]MetricPoint, 0, len(all))
	for _, p := range all {
		if p.TS >= since {
			points = append(points, p)
		}
	}
	out := downsampleMetrics(points, metricsMaxPoints)
	ok(w, map[string]interface{}{
		"points":         out,
		"range":          metricsRangeLabel(win),
		"interval_sec":   metricsIntervalSec,
		"retention_days": metricsRetentionDays,
		"total":          len(points),
		"disk_ok":        diskOK,
	})
}

func parseMetricsRange(v string) time.Duration {
	switch v {
	case "6h":
		return 6 * time.Hour
	case "24h", "1d":
		return 24 * time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	default:
		return time.Hour
	}
}

func metricsRangeLabel(win time.Duration) string {
	switch win {
	case 6 * time.Hour:
		return "6h"
	case 24 * time.Hour:
		return "24h"
	case 7 * 24 * time.Hour:
		return "7d"
	default:
		return "1h"
	}
}

// downsampleMetrics 按等分桶压缩点数：数值型指标取桶内均值，累计流量取桶末值，
// 时间戳取桶末点，保证前端换算速率时时间轴与数值对齐。
func downsampleMetrics(points []MetricPoint, max int) []MetricPoint {
	if len(points) == 0 {
		return []MetricPoint{}
	}
	if len(points) <= max {
		return points
	}
	step := float64(len(points)) / float64(max)
	out := make([]MetricPoint, 0, max)
	for i := 0; i < max; i++ {
		start := int(float64(i) * step)
		end := int(float64(i+1) * step)
		if end <= start {
			end = start + 1
		}
		if end > len(points) {
			end = len(points)
		}
		bucket := points[start:end]
		var sumCPU, sumMem, sumDisk, sumTemp float64
		for _, p := range bucket {
			sumCPU += p.CPU
			sumMem += p.Mem
			sumDisk += p.Disk
			sumTemp += p.Temp
		}
		n := float64(len(bucket))
		last := bucket[len(bucket)-1]
		out = append(out, MetricPoint{
			TS:    last.TS,
			CPU:   round1Metric(sumCPU / n),
			Mem:   round1Metric(sumMem / n),
			Disk:  round1Metric(sumDisk / n),
			Temp:  round1Metric(sumTemp / n),
			NetRx: last.NetRx,
			NetTx: last.NetTx,
		})
		if end == len(points) {
			break
		}
	}
	return out
}
