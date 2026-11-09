package api

import (
	"net/http"
	"os/exec"
	"strconv"
	"strings"
)

type procInfo struct {
	PID   int    `json:"pid"`
	User  string `json:"user"`
	RSSKB int    `json:"rss_kb"`
	Comm  string `json:"comm"`
}

// ListProcesses 列出进程，按内存占用从高到低排序，最多返回 100 条
func (s *Server) ListProcesses(w http.ResponseWriter, r *http.Request) {
	out, err := exec.Command("ps", "-eo", "pid,user,rss,comm",
		"--no-headers", "--sort=-rss").Output()
	if err != nil {
		fail(w, http.StatusInternalServerError, "读取进程失败: "+err.Error())
		return
	}

	list := []procInfo{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		rss, _ := strconv.Atoi(f[2])
		list = append(list, procInfo{
			PID:   pid,
			User:  f[1],
			RSSKB: rss,
			Comm:  strings.Join(f[3:], " "),
		})
		if len(list) >= 100 {
			break
		}
	}
	ok(w, list)
}

// KillProcess 结束指定进程，默认 SIGTERM，force=1 时 SIGKILL
func (s *Server) KillProcess(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.Atoi(r.URL.Query().Get("pid"))
	if err != nil || pid <= 1 {
		fail(w, http.StatusBadRequest, "无效的进程号")
		return
	}

	sig := "-TERM"
	if r.URL.Query().Get("force") == "1" {
		sig = "-KILL"
	}
	if err := exec.Command("kill", sig, strconv.Itoa(pid)).Run(); err != nil {
		fail(w, http.StatusInternalServerError, "结束失败: "+err.Error())
		return
	}
	s.audit(r, "process", "结束进程", "PID "+strconv.Itoa(pid)+" 信号 "+strings.TrimPrefix(sig, "-"))
	ok(w, map[string]int{"pid": pid})
}
