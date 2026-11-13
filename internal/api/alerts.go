package api

import (
	"fmt"
	"net/http"
	"time"
)

// 资源阈值告警：CPU / 内存 / 磁盘持续超阈值才通知，回落需低于阈值 5 个百分点
// （滞回带）才判定恢复，避免临界值反复推送。温度沿用原有 checkTemp 机制，
// 此处不重复处理，防止同一事件收到两份通知。
const (
	defaultAlertSustainSec = 300
	alertCooldownSec       = 1800
	alertHysteresis        = 5.0
)

// alertTrack 记录单个指标的告警状态。
type alertTrack struct {
	since    time.Time // 首次超过阈值的时间
	alerted  bool
	lastSent time.Time // 上次推送时间，用于冷却
}

type alertConfig struct {
	cpu, mem, disk, sustain int
}

// alertConfigSnapshot 读取当前阈值配置。sustain 为 0 时回落到默认值。
func (s *Server) alertConfigSnapshot() alertConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := alertConfig{
		cpu:     s.cfg.AlertCPUPct,
		mem:     s.cfg.AlertMemPct,
		disk:    s.cfg.AlertDiskPct,
		sustain: s.cfg.AlertSustainSec,
	}
	if c.sustain == 0 {
		c.sustain = defaultAlertSustainSec
	}
	return c
}

// checkResourceAlerts 在每次采样后评估三项资源阈值。
func (s *Server) checkResourceAlerts(p MetricPoint) {
	c := s.alertConfigSnapshot()
	now := time.Now()
	s.evaluateAlert("cpu", c.cpu, p.CPU, c.sustain, now, "CPU 使用率")
	s.evaluateAlert("mem", c.mem, p.Mem, c.sustain, now, "内存使用率")
	s.evaluateAlert("disk", c.disk, p.Disk, c.sustain, now, "磁盘使用率")
}

// evaluateAlert 更新单个指标的状态机，并在锁外发送通知（推送可能耗时）。
func (s *Server) evaluateAlert(key string, threshold int, value float64, sustain int, now time.Time, label string) {
	s.metricsMu.Lock()
	if s.alertTracks == nil {
		s.alertTracks = make(map[string]*alertTrack)
	}
	st := s.alertTracks[key]
	if st == nil {
		st = &alertTrack{}
		s.alertTracks[key] = st
	}

	fire := ""
	if threshold <= 0 {
		*st = alertTrack{} // 阈值关闭时清空状态，避免下次开启沿用旧计时
	} else if value >= float64(threshold) {
		if st.since.IsZero() {
			st.since = now
		}
		if !st.alerted && now.Sub(st.since) >= time.Duration(sustain)*time.Second &&
			(st.lastSent.IsZero() || now.Sub(st.lastSent) >= alertCooldownSec*time.Second) {
			st.alerted = true
			st.lastSent = now
			fire = "alert"
		}
	} else if value < float64(threshold)-alertHysteresis {
		if st.alerted {
			st.alerted = false
			st.since = time.Time{}
			fire = "recovered"
		} else {
			st.since = time.Time{} // 未告警就回落，重新计算持续时间
		}
	}
	s.metricsMu.Unlock()

	if fire != "" {
		s.sendResourceAlert(label, value, threshold, sustain, fire == "recovered")
	}
}

// sendResourceAlert 复用现有 Webhook 通道与面板审计日志。
func (s *Server) sendResourceAlert(label string, value float64, threshold, sustain int, recovered bool) {
	title, action := "资源告警："+label, "资源阈值告警"
	content := fmt.Sprintf("%s 当前 %.1f%%，已持续超过阈值 %d%% 达 %d 秒，请检查设备负载。",
		label, value, threshold, sustain)
	if recovered {
		title, action = "资源恢复："+label, "资源告警恢复"
		content = fmt.Sprintf("%s 已回落至 %.1f%%（阈值 %d%%），告警解除。", label, value, threshold)
	}
	s.notifyAsync("resource", title, content)
	go s.appendAudit(auditEntry{
		Time:     time.Now().Format("2006-01-02 15:04:05"),
		IP:       "monitor",
		User:     s.username(),
		Category: "resource",
		Action:   action,
		Detail:   fmt.Sprintf("%s %.1f%% / 阈值 %d%%", label, value, threshold),
	})
}

// GetAlertSettings 返回当前阈值配置与默认值，供设置页渲染。
func (s *Server) GetAlertSettings(w http.ResponseWriter, r *http.Request) {
	c := s.alertConfigSnapshot()
	ok(w, map[string]interface{}{
		"alert_cpu_pct":     c.cpu,
		"alert_mem_pct":     c.mem,
		"alert_disk_pct":    c.disk,
		"alert_sustain_sec": c.sustain,
		"temp_threshold_c":  s.tempThreshold(),
		"default_sustain":   defaultAlertSustainSec,
		"cooldown_sec":      alertCooldownSec,
		"hysteresis":        alertHysteresis,
		"sample_interval":   metricsIntervalSec,
	})
}

// alertSettingsReq 使用指针字段区分「未提交」与「值为 0（关闭）」，
// 这样旧的 saveMonitor 请求（只带 interval_sec / temp_threshold_c）不会
// 意外清空这里配置的资源阈值。
type alertSettingsReq struct {
	CPU     *int `json:"alert_cpu_pct"`
	Mem     *int `json:"alert_mem_pct"`
	Disk    *int `json:"alert_disk_pct"`
	Sustain *int `json:"alert_sustain_sec"`
	Temp    *int `json:"temp_threshold_c"`
}

func validPct(v int) bool  { return v == 0 || (v >= 1 && v <= 100) }
func validTemp(v int) bool { return v == 0 || (v >= 40 && v <= 100) }

// SaveAlertSettings 保存资源阈值；校验失败时不改动内存配置，保证原子性。
func (s *Server) SaveAlertSettings(w http.ResponseWriter, r *http.Request) {
	var req alertSettingsReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	for _, f := range []struct {
		name string
		val  *int
		ok   func(int) bool
		desc string
	}{
		{"CPU 阈值", req.CPU, validPct, "1-100 或 0（关闭）"},
		{"内存阈值", req.Mem, validPct, "1-100 或 0（关闭）"},
		{"磁盘阈值", req.Disk, validPct, "1-100 或 0（关闭）"},
		{"温度阈值", req.Temp, validTemp, "40-100 或 0（关闭）"},
	} {
		if f.val != nil && !f.ok(*f.val) {
			fail(w, http.StatusBadRequest, f.name+" 需为 "+f.desc)
			return
		}
	}
	if req.Sustain != nil && *req.Sustain != 0 && (*req.Sustain < 60 || *req.Sustain > 3600) {
		fail(w, http.StatusBadRequest, "持续时间需为 60-3600 秒或 0（默认 300 秒）")
		return
	}

	s.mu.Lock()
	old := struct{ cpu, mem, disk, sustain, temp int }{
		s.cfg.AlertCPUPct, s.cfg.AlertMemPct, s.cfg.AlertDiskPct,
		s.cfg.AlertSustainSec, s.cfg.TempThresholdC,
	}
	if req.CPU != nil {
		s.cfg.AlertCPUPct = *req.CPU
	}
	if req.Mem != nil {
		s.cfg.AlertMemPct = *req.Mem
	}
	if req.Disk != nil {
		s.cfg.AlertDiskPct = *req.Disk
	}
	if req.Sustain != nil {
		s.cfg.AlertSustainSec = *req.Sustain
	}
	if req.Temp != nil {
		s.cfg.TempThresholdC = *req.Temp
	}
	err := s.cfg.Save(s.configPath)
	if err != nil {
		s.cfg.AlertCPUPct, s.cfg.AlertMemPct, s.cfg.AlertDiskPct = old.cpu, old.mem, old.disk
		s.cfg.AlertSustainSec, s.cfg.TempThresholdC = old.sustain, old.temp
	}
	s.mu.Unlock()
	if err != nil {
		fail(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}

	// 阈值变化后清空状态机，让新阈值从零开始累计持续时间。
	s.metricsMu.Lock()
	s.alertTracks = make(map[string]*alertTrack)
	s.metricsMu.Unlock()

	s.audit(r, "settings", "修改资源阈值告警设置",
		fmt.Sprintf("CPU=%d%% 内存=%d%% 磁盘=%d%% 温度=%d℃ 持续=%ds",
			derefInt(req.CPU, old.cpu), derefInt(req.Mem, old.mem),
			derefInt(req.Disk, old.disk), derefInt(req.Temp, old.temp),
			derefInt(req.Sustain, old.sustain)))
	ok(w, map[string]interface{}{"msg": "已保存"})
}

func derefInt(p *int, fallback int) int {
	if p == nil {
		return fallback
	}
	return *p
}
