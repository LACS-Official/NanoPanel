package api

// 定时任务：面板内管理小型例行任务（清理日志、定时脚本等）。
// 任务持久化在配置目录的 cron.json；调度器随面板启动，
// 支持三种周期：every:Nm（每 N 分钟）、hourly:MM（每小时第 MM 分）、daily:HH:MM。

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"nanopanel/internal/config"
)

// CronTask 一条定时任务。
type CronTask struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Schedule   string `json:"schedule"` // every:30m / hourly:15 / daily:03:30
	Command    string `json:"command"`
	Enabled    bool   `json:"enabled"`
	LastRun    string `json:"last_run,omitempty"`
	LastStatus string `json:"last_status,omitempty"` // ok / fail
	LastOutput string `json:"last_output,omitempty"`
}

// cronState 调度器的内存状态（不落盘）。
type cronState struct {
	// lastTrigger 记录上次触发时间；daily 记录上次触发的日期，避免重复执行。
	lastTrigger map[string]time.Time
	lastDaily   map[string]string
}

// cronPath 任务持久化文件。
func (s *Server) cronPath() string {
	return filepath.Join(filepath.Dir(s.configPath), "cron.json")
}

// loadCronTasks 读取持久化的任务列表。
func (s *Server) loadCronTasks() []CronTask {
	data, err := os.ReadFile(s.cronPath())
	if err != nil {
		return nil
	}
	var tasks []CronTask
	if json.Unmarshal(data, &tasks) != nil {
		return nil
	}
	return tasks
}

// saveCronTasks 原子化写回任务列表。
func (s *Server) saveCronTasks(tasks []CronTask) error {
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.cronPath() + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.cronPath())
}

// validSchedule 校验调度表达式。
func validSchedule(sc string) bool {
	switch {
	case strings.HasPrefix(sc, "every:") && strings.HasSuffix(sc, "m"):
		v := strings.TrimSuffix(strings.TrimPrefix(sc, "every:"), "m")
		n, err := strconv.Atoi(v)
		return err == nil && n >= 5 && n <= 1440
	case strings.HasPrefix(sc, "hourly:"):
		m, err := strconv.Atoi(strings.TrimPrefix(sc, "hourly:"))
		return err == nil && m >= 0 && m <= 59
	case strings.HasPrefix(sc, "daily:"):
		parts := strings.Split(strings.TrimPrefix(sc, "daily:"), ":")
		if len(parts) != 2 {
			return false
		}
		h, err1 := strconv.Atoi(parts[0])
		m, err2 := strconv.Atoi(parts[1])
		return err1 == nil && err2 == nil && h >= 0 && h <= 23 && m >= 0 && m <= 59
	}
	return false
}

func scheduleDesc(sc string) string {
	if strings.HasPrefix(sc, "every:") {
		return "每 " + strings.TrimPrefix(strings.TrimSuffix(sc, "m"), "every:") + " 分钟"
	}
	if strings.HasPrefix(sc, "hourly:") {
		return "每小时第 " + strings.TrimPrefix(sc, "hourly:") + " 分"
	}
	if strings.HasPrefix(sc, "daily:") {
		return "每天 " + strings.TrimPrefix(sc, "daily:")
	}
	return sc
}

// ListCron 返回任务列表。
func (s *Server) ListCron(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]interface{}{
		"tasks": s.loadCronTasks(),
		"schedules": []map[string]string{
			{"value": "every:30m", "label": "每 30 分钟"},
			{"value": "every:60m", "label": "每 1 小时"},
			{"value": "every:360m", "label": "每 6 小时"},
			{"value": "hourly:15", "label": "每小时第 15 分"},
			{"value": "daily:03:30", "label": "每天 03:30"},
		},
	})
}

type cronReq struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Enabled  *bool  `json:"enabled"`
}

// SaveCron 新建或更新任务（带 ID 为更新）。
func (s *Server) SaveCron(w http.ResponseWriter, r *http.Request) {
	var req cronReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Command = strings.TrimSpace(req.Command)
	if len(req.Name) < 1 || len(req.Name) > 64 {
		fail(w, http.StatusBadRequest, "任务名称长度 1-64")
		return
	}
	if len(req.Command) == 0 || len(req.Command) > 1024 {
		fail(w, http.StatusBadRequest, "命令不能为空且不超过 1024 字符")
		return
	}
	if !validSchedule(req.Schedule) {
		fail(w, http.StatusBadRequest, "调度表达式不合法（every:Nm / hourly:MM / daily:HH:MM）")
		return
	}

	tasks := s.loadCronTasks()
	if req.ID != "" {
		found := false
		for i := range tasks {
			if tasks[i].ID == req.ID {
				tasks[i].Name, tasks[i].Schedule, tasks[i].Command = req.Name, req.Schedule, req.Command
				if req.Enabled != nil {
					tasks[i].Enabled = *req.Enabled
				}
				found = true
				break
			}
		}
		if !found {
			fail(w, http.StatusNotFound, "任务不存在")
			return
		}
	} else {
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		tasks = append(tasks, CronTask{
			ID: config.RandHex(4), Name: req.Name, Schedule: req.Schedule,
			Command: req.Command, Enabled: enabled,
		})
	}
	if err := s.saveCronTasks(tasks); err != nil {
		fail(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	s.audit(r, "settings", "保存定时任务", req.Name+"（"+scheduleDesc(req.Schedule)+"）")
	ok(w, map[string]interface{}{"msg": "已保存", "tasks": s.loadCronTasks()})
}

// DeleteCron 删除任务。
func (s *Server) DeleteCron(w http.ResponseWriter, r *http.Request) {
	var req cronReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	tasks := s.loadCronTasks()
	out := tasks[:0]
	found := false
	for _, t := range tasks {
		if t.ID != req.ID {
			out = append(out, t)
		} else {
			found = true
		}
	}
	if !found {
		fail(w, http.StatusNotFound, "任务不存在")
		return
	}
	if err := s.saveCronTasks(out); err != nil {
		fail(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	s.audit(r, "settings", "删除定时任务", req.Name)
	ok(w, map[string]interface{}{"msg": "已删除", "tasks": s.loadCronTasks()})
}

// RunCronNow 立即执行一次任务（不受 Enabled 限制）。
func (s *Server) RunCronNow(w http.ResponseWriter, r *http.Request) {
	var req cronReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	var task *CronTask
	tasks := s.loadCronTasks()
	for i := range tasks {
		if tasks[i].ID == req.ID {
			task = &tasks[i]
			break
		}
	}
	if task == nil {
		fail(w, http.StatusNotFound, "任务不存在")
		return
	}
	s.audit(r, "settings", "手动执行定时任务", task.Name)
	// 同步执行（前端有等待反馈），10 分钟超时。
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Minute)
	defer cancel()
	status, output := runCronCommand(ctx, task.Command)
	s.recordCronResult(req.ID, status, output)
	ok(w, map[string]interface{}{"status": status, "output": output})
}

// runCronCommand 执行命令并截取输出（≤4KB）。
func runCronCommand(ctx context.Context, command string) (status, output string) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	out, err := cmd.CombinedOutput()
	if len(out) > 4096 {
		out = out[:4096]
	}
	if err != nil {
		return "fail", string(out) + "\n" + err.Error()
	}
	return "ok", string(out)
}

// recordCronResult 更新任务的执行结果并持久化。
func (s *Server) recordCronResult(id, status, output string) {
	tasks := s.loadCronTasks()
	for i := range tasks {
		if tasks[i].ID == id {
			tasks[i].LastRun = time.Now().Format("2006-01-02 15:04:05")
			tasks[i].LastStatus = status
			tasks[i].LastOutput = output
			break
		}
	}
	_ = s.saveCronTasks(tasks)
}

// StartCron 启动调度循环：每 30 秒检查到期任务。
func (s *Server) StartCron(ctx context.Context) {
	go func() {
		var mu sync.Mutex
		state := &cronState{
			lastTrigger: make(map[string]time.Time),
			lastDaily:   make(map[string]string),
		}
		// 启动时以当前时间作为基准，避免面板重启即触发全部任务。
		for _, t := range s.loadCronTasks() {
			state.lastTrigger[t.ID] = time.Now()
			state.lastDaily[t.ID] = time.Now().Format("2006-01-02")
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
			}
			mu.Lock()
			now := time.Now()
			for _, t := range s.loadCronTasks() {
				if !t.Enabled || !validSchedule(t.Schedule) {
					continue
				}
				if !cronDue(state, t, now) {
					continue
				}
				go func(task CronTask) {
					ctx2, cancel := context.WithTimeout(ctx, 10*time.Minute)
					defer cancel()
					status, output := runCronCommand(ctx2, task.Command)
					s.recordCronResult(task.ID, status, output)
					title := "定时任务完成：" + task.Name
					content := "命令: " + task.Command + "\n结果: " + status
					if status == "fail" {
						title = "定时任务失败：" + task.Name
					}
					if output != "" {
						content += "\n输出:\n" + output
					}
					s.notifyAsync("task", title, content)
					go s.appendAudit(auditEntry{
						Time: time.Now().Format("2006-01-02 15:04:05"), IP: "cron",
						User: s.username(), Category: "task",
						Action: "定时任务" + map[bool]string{true: "成功", false: "失败"}[status == "ok"],
						Detail: task.Name,
					})
				}(t)
			}
			mu.Unlock()
		}
	}()
}

// cronDue 判断任务是否到期（同时更新基准时间，保证每周期只触发一次）。
func cronDue(state *cronState, t CronTask, now time.Time) bool {
	switch {
	case strings.HasPrefix(t.Schedule, "every:"):
		v := strings.TrimSuffix(strings.TrimPrefix(t.Schedule, "every:"), "m")
		mins, _ := strconv.Atoi(v)
		last, ok := state.lastTrigger[t.ID]
		if !ok {
			state.lastTrigger[t.ID] = now
			return false
		}
		if now.Sub(last) >= time.Duration(mins)*time.Minute {
			state.lastTrigger[t.ID] = now
			return true
		}
	case strings.HasPrefix(t.Schedule, "hourly:"):
		m, _ := strconv.Atoi(strings.TrimPrefix(t.Schedule, "hourly:"))
		last, ok := state.lastTrigger[t.ID]
		if !ok {
			state.lastTrigger[t.ID] = now
			return false
		}
		if now.Minute() >= m && (now.Hour() != last.Hour() || now.Sub(last) >= time.Hour) {
			state.lastTrigger[t.ID] = now
			return true
		}
	case strings.HasPrefix(t.Schedule, "daily:"):
		parts := strings.Split(strings.TrimPrefix(t.Schedule, "daily:"), ":")
		h, _ := strconv.Atoi(parts[0])
		m, _ := strconv.Atoi(parts[1])
		today := now.Format("2006-01-02")
		if state.lastDaily[t.ID] == today {
			return false
		}
		if now.Hour() > h || (now.Hour() == h && now.Minute() >= m) {
			state.lastDaily[t.ID] = today
			state.lastTrigger[t.ID] = now
			return true
		}
	}
	return false
}
