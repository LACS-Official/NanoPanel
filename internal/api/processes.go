package api

import (
	"net/http"
	"os/exec"
	"strconv"
	"strings"
)

type procInfo struct {
	PID    int     `json:"pid"`
	User   string  `json:"user"`
	CPUPct float64 `json:"cpu_pct"` // ps 的 %CPU：进程启动以来的平均占用
	RSSKB  int     `json:"rss_kb"`
	Comm   string  `json:"comm"`
	Args   string  `json:"args,omitempty"` // 完整命令行，用于前端悬停查看与用途推断
	Desc   string  `json:"desc"`           // 该进程是干什么的（本地字典推断）
}

// ListProcesses 列出进程，按内存占用从高到低排序，最多返回 100 条
func (s *Server) ListProcesses(w http.ResponseWriter, r *http.Request) {
	// 额外取 args：comm 会被内核截断到 15 字符且不含参数，
	// 完整命令行能让「用途说明」判断得更准，也方便前端悬停查看。
	out, err := exec.Command("ps", "-eo", "pid,user,pcpu,rss,comm,args",
		"--no-headers", "--sort=-rss").Output()
	if err != nil {
		fail(w, http.StatusInternalServerError, "读取进程失败: "+err.Error())
		return
	}

	ok(w, parseProcessList(out))
}

// parseProcessList 解析 `ps -eo pid,user,pcpu,rss,comm,args --no-headers` 的输出。
// 抽成纯函数便于在没有 procps 的环境（如 Windows CI）里用真实样本做回归。
func parseProcessList(out []byte) []procInfo {
	list := []procInfo{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		cpu, _ := strconv.ParseFloat(f[2], 64)
		rss, _ := strconv.Atoi(f[3])
		// comm 固定为第 5 列（ps -eo comm 只给可执行名，通常不含空格），
		// 第 6 列起为完整命令行；内核线程没有参数时退化为 comm 本身。
		comm := f[4]
		args := comm
		if len(f) >= 6 {
			args = strings.Join(f[5:], " ")
		}
		list = append(list, procInfo{
			PID:    pid,
			User:   f[1],
			CPUPct: cpu,
			RSSKB:  rss,
			Comm:   comm,
			Args:   args,
			Desc:   describeProcess(comm, args),
		})
		if len(list) >= 100 {
			break
		}
	}
	return list
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
