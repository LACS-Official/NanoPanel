package api

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"nanopanel/internal/config"
)

// AppHealth 是单个应用的运行状态快照。
type AppHealth struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Unit      string `json:"unit,omitempty"`
	Installed bool   `json:"installed"`
	// Status: running / down / unknown / not_installed
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// healthChange 是一次检测中发生的状态翻转。
type healthChange struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	From  string `json:"from"`
	To    string `json:"to"`
	Event string `json:"event"` // down / recovered
}

// appRuntimeStatus 检测单个应用：安装状态 + systemd 单元存活状态。
// 没有声明 unit 的应用不参与存活检测，返回 unknown，避免误报告警。
func (s *Server) appRuntimeStatus(ctx context.Context, app *AppMeta) AppHealth {
	h := AppHealth{ID: app.ID, Name: app.Name, Unit: app.Unit}
	installed := s.appState(ctx, app.ID) == "installed"
	h.Installed = installed
	switch {
	case !installed:
		h.Status = "not_installed"
	case app.Unit == "":
		h.Status = "unknown"
		h.Detail = "未声明 systemd 单元，跳过存活检测"
	default:
		raw := s.unitState(ctx, app.Unit)
		h.Detail = "systemctl: " + raw
		switch raw {
		case "active":
			h.Status = "running"
		case "inactive", "failed", "deactivating":
			h.Status = "down"
		default:
			// activating / unknown / 非 systemd 环境：状态不明确，不触发告警。
			h.Status = "unknown"
		}
	}
	return h
}

// unitState 查询 systemd 单元状态；测试可注入 unitProbe 模拟掉线/恢复。
func (s *Server) unitState(ctx context.Context, unit string) string {
	if s.unitProbe != nil {
		return s.unitProbe(ctx, unit)
	}
	return activeState(ctx, unit)
}

// healthSnapshot 计算清单中所有应用的当前状态。
func (s *Server) healthSnapshot(ctx context.Context) []AppHealth {
	m, err := s.loadManifest()
	if err != nil {
		return []AppHealth{}
	}
	out := make([]AppHealth, 0, len(m.Apps))
	for i := range m.Apps {
		out = append(out, s.appRuntimeStatus(ctx, &m.Apps[i]))
	}
	return out
}

// checkHealth 检测应用状态变化。
//   - emit=false：只读取快照，不更新基线、不推送（供界面查询）。
//   - emit=true 且尚无基线：仅建立基线，不推送，避免服务刚启动时刷屏。
//   - force=true：忽略前态，对所有已掉线的应用立即推送（供手动体检）。
func (s *Server) checkHealth(ctx context.Context, emit, force bool) ([]AppHealth, []healthChange) {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()

	states := s.healthSnapshot(ctx)
	current := make(map[string]string, len(states))
	for _, h := range states {
		current[h.ID] = h.Status
	}
	if !emit {
		return states, nil
	}
	if s.lastHealth == nil && !force {
		s.lastHealth = current
		return states, nil
	}

	changes := make([]healthChange, 0)
	for _, h := range states {
		prev := s.lastHealth[h.ID]
		var event string
		switch {
		case force && h.Status == "down":
			event = "down"
		case prev == "running" && h.Status == "down":
			event = "down"
		case prev == "down" && h.Status == "running":
			event = "recovered"
		}
		if event == "" {
			continue
		}
		changes = append(changes, healthChange{
			ID: h.ID, Name: h.Name, From: prev, To: h.Status, Event: event,
		})
		s.healthAlert(h, event)
	}
	s.lastHealth = current
	return states, changes
}

// healthAlert 推送掉线/恢复通知，并把事件写入面板日志。
func (s *Server) healthAlert(h AppHealth, event string) {
	title := "应用掉线：" + h.Name
	action := "应用掉线告警"
	content := "应用 " + h.ID + " 已停止运行，请检查服务状态。"
	if event == "recovered" {
		title = "应用恢复：" + h.Name
		action = "应用恢复通知"
		content = "应用 " + h.ID + " 已恢复运行。"
	}
	if h.Detail != "" {
		content += "\n状态: " + h.Detail
	}
	if h.Unit != "" {
		content += "\n单元: " + h.Unit
	}
	s.notifyAsync("health", title, content)
	go s.appendAudit(auditEntry{
		Time:     time.Now().Format("2006-01-02 15:04:05"),
		IP:       "monitor",
		User:     s.username(),
		Category: "health",
		Action:   action,
		Detail:   h.ID + " (" + h.Status + ")",
	})
}

// monitorInterval 返回当前检测间隔（秒），负数表示已关闭。
func (s *Server) monitorInterval() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.MonitorIntervalSec
}

// StartMonitor 启动后台健康检测循环，随 ctx 退出。
// 检测间隔每次循环重新读取，设置页修改后下一轮即生效。
func (s *Server) StartMonitor(ctx context.Context) {
	go func() {
		// 启动即建立基线，避免把启动瞬间的中间状态当成掉线。
		base, cancel := context.WithTimeout(ctx, 30*time.Second)
		s.checkHealth(base, true, false)
		cancel()

		for {
			interval := s.monitorInterval()
			wait := 5 * time.Second
			if interval > 0 {
				wait = time.Duration(interval) * time.Second
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			if s.monitorInterval() <= 0 {
				continue // 自动检测已关闭
			}
			checkCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			_, changes := s.checkHealth(checkCtx, true, false)
			cancel()
			if len(changes) > 0 {
				log.Printf("应用健康检测发现 %d 处状态变化", len(changes))
			}
		}
	}()
}

// AppHealthStatus 返回所有应用的当前运行状态与监控配置。
func (s *Server) AppHealthStatus(w http.ResponseWriter, r *http.Request) {
	states, _ := s.checkHealth(r.Context(), false, false)
	interval := s.monitorInterval()
	ok(w, map[string]interface{}{
		"apps":            states,
		"interval_sec":    interval,
		"monitor_enabled": interval > 0,
	})
}

// CheckAppHealth 立即执行一次检测并推送状态变化通知。
// ?force=1 时忽略前态，对所有当前已掉线的应用直接告警。
func (s *Server) CheckAppHealth(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("force") == "1"
	states, changes := s.checkHealth(r.Context(), true, force)
	ok(w, map[string]interface{}{
		"apps":    states,
		"changes": changes,
	})
}

// GetMonitorSettings 返回监控设置与可选项。
func (s *Server) GetMonitorSettings(w http.ResponseWriter, r *http.Request) {
	interval := s.monitorInterval()
	ok(w, map[string]interface{}{
		"interval_sec":    interval,
		"monitor_enabled": interval > 0,
		"default_sec":     config.DefaultMonitorIntervalSec,
		"options": []map[string]interface{}{
			{"value": -1, "label": "关闭自动检测"},
			{"value": 30, "label": "每 30 秒"},
			{"value": 60, "label": "每 1 分钟"},
			{"value": 300, "label": "每 5 分钟"},
			{"value": 900, "label": "每 15 分钟"},
		},
	})
}

type monitorReq struct {
	IntervalSec int `json:"interval_sec"`
}

// SaveMonitorSettings 保存检测间隔并持久化。
func (s *Server) SaveMonitorSettings(w http.ResponseWriter, r *http.Request) {
	var req monitorReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	v := req.IntervalSec
	if v != -1 && (v < 10 || v > 3600) {
		fail(w, http.StatusBadRequest, "检测间隔需为 10-3600 秒，或 -1 关闭")
		return
	}

	s.mu.Lock()
	old := s.cfg.MonitorIntervalSec
	s.cfg.MonitorIntervalSec = v
	if err := s.cfg.Save(s.configPath); err != nil {
		s.cfg.MonitorIntervalSec = old
		s.mu.Unlock()
		fail(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	s.mu.Unlock()

	label := "已关闭自动检测"
	if v > 0 {
		label = "检测间隔 " + strconv.Itoa(v) + " 秒"
	}
	s.audit(r, "settings", "修改应用健康检测设置", label)
	ok(w, map[string]interface{}{"msg": "已保存", "interval_sec": v})
}
