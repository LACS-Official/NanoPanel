package api

import (
	"context"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"time"
)

var (
	hostRe = regexp.MustCompile(`^[A-Za-z0-9.\-:]+$`)
	unitRe = regexp.MustCompile(`^[A-Za-z0-9@._\-]+$`)
)

// Reboot 重启系统，延迟 1 秒让响应先返回
func (s *Server) Reboot(w http.ResponseWriter, r *http.Request) {
	s.audit(r, "system", "重启系统", "操作来自面板")
	go func() {
		time.Sleep(time.Second)
		_ = exec.Command("systemctl", "reboot").Run()
	}()
	ok(w, map[string]string{"msg": "系统即将重启"})
}

// Ping 对目标主机执行 4 次连通性测试
func (s *Server) Ping(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Query().Get("host")
	if host == "" || !hostRe.MatchString(host) {
		fail(w, http.StatusBadRequest, "目标地址不合法")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "ping", "-c", "4", "-W", "2", host).CombinedOutput()
	reachable := err == nil
	ok(w, map[string]string{
		"host": host, "output": string(out),
		"reachable": strconv.FormatBool(reachable),
	})
}

// Logs 查看系统日志，可指定 unit 和行数 n
func (s *Server) Logs(w http.ResponseWriter, r *http.Request) {
	n := 100
	if v, err := strconv.Atoi(r.URL.Query().Get("n")); err == nil && v > 0 && v <= 500 {
		n = v
	}

	args := []string{"-n", strconv.Itoa(n), "--no-pager"}
	if unit := r.URL.Query().Get("unit"); unit != "" {
		if !unitRe.MatchString(unit) {
			fail(w, http.StatusBadRequest, "单元名不合法")
			return
		}
		args = append(args, "-u", unit)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "journalctl", args...).Output()
	if err != nil {
		fail(w, http.StatusInternalServerError, "读取日志失败: "+err.Error())
		return
	}
	ok(w, map[string]string{"logs": string(out)})
}
