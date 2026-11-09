package api

import (
	"context"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"nanopanel/internal/config"
)

type serviceStatus struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	State string `json:"state"`
}

func (s *Server) findService(name string) *config.ServiceDef {
	for i := range s.cfg.Services {
		if s.cfg.Services[i].Name == name {
			return &s.cfg.Services[i]
		}
	}
	return nil
}

// activeState 查询服务状态，带超时避免 systemctl 卡死拖住请求。
func activeState(ctx context.Context, name string) string {
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "systemctl", "is-active", name).Output()
	if err != nil && len(out) == 0 {
		return "unknown"
	}
	st := strings.TrimSpace(string(out))
	if st == "" {
		return "unknown"
	}
	return st
}

// ListServices 列出托管服务及当前状态。
func (s *Server) ListServices(w http.ResponseWriter, r *http.Request) {
	list := make([]serviceStatus, 0, len(s.cfg.Services))
	for _, svc := range s.cfg.Services {
		list = append(list, serviceStatus{
			Name: svc.Name, Label: svc.Label, State: activeState(r.Context(), svc.Name),
		})
	}
	ok(w, list)
}

// ServiceAction 对指定服务执行 start/stop/restart。
func (s *Server) ServiceAction(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("svc")
	action := r.URL.Query().Get("action")

	if s.findService(name) == nil {
		fail(w, http.StatusBadRequest, "未知服务")
		return
	}
	if action != "start" && action != "stop" && action != "restart" {
		fail(w, http.StatusBadRequest, "不支持的操作")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "systemctl", action, name).Run(); err != nil {
		fail(w, http.StatusInternalServerError, "操作失败: "+err.Error())
		return
	}
	ok(w, map[string]string{
		"svc": name, "action": action, "state": activeState(ctx, name),
	})
}
