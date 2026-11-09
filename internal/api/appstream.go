package api

// 应用安装/卸载的实时输出流（SSE）。
// 与同步版 /app/action 的差异：
//   - 脚本 stdout/stderr 逐行推送给前端，弹窗里实时滚动；
//   - 使用 context.WithoutCancel，浏览器关闭弹窗不会中断安装；
//   - 同一应用同一时间只允许一个操作，防止并发脚本互相踩踏。

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// tryBeginAction 抢占某应用的执行权；已有操作在跑时返回 false。
func (s *Server) tryBeginAction(id string) bool {
	s.actionMu.Lock()
	defer s.actionMu.Unlock()
	if s.runningActions == nil {
		s.runningActions = map[string]bool{}
	}
	if s.runningActions[id] {
		return false
	}
	s.runningActions[id] = true
	return true
}

func (s *Server) endAction(id string) {
	s.actionMu.Lock()
	delete(s.runningActions, id)
	s.actionMu.Unlock()
}

// AppActionStream 以 SSE 流式执行 install/remove，实时回传脚本输出。
func (s *Server) AppActionStream(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	action := r.URL.Query().Get("action")

	if !appIDRe.MatchString(id) {
		fail(w, http.StatusBadRequest, "应用 ID 不合法")
		return
	}
	var scriptName string
	switch action {
	case "install":
		scriptName = "install.sh"
	case "remove":
		scriptName = "remove.sh"
	default:
		fail(w, http.StatusBadRequest, "不支持的操作")
		return
	}
	script := filepath.Join(s.cfg.AppsDir, id, scriptName)
	if _, err := os.Stat(script); err != nil {
		fail(w, http.StatusNotFound, "应用脚本不存在")
		return
	}
	if !s.tryBeginAction(id) {
		fail(w, http.StatusConflict, "该应用正在执行其他操作，请稍候")
		return
	}
	defer s.endAction(id)

	flusher, canFlush := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(payload map[string]interface{}) {
		b, err := json.Marshal(payload)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", b)
		if canFlush {
			flusher.Flush()
		}
	}

	send(map[string]interface{}{"event": "start", "app": id, "action": action})

	// 客户端断开不中断脚本：安装可能长达数分钟，中途关掉弹窗应继续执行。
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", script)
	if extra, err := s.appScriptEnv(id); err == nil {
		cmd.Env = append(os.Environ(), extra...)
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		send(map[string]interface{}{"event": "done", "ok": false, "err": err.Error()})
		return
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		send(map[string]interface{}{"event": "done", "ok": false, "err": err.Error()})
		return
	}
	waitErr := make(chan error, 1)
	go func() {
		waitErr <- cmd.Wait()
		pw.Close() // 子进程退出后关闭写端，令读端收到 EOF
	}()

	reader := bufio.NewReaderSize(pr, 32*1024)
	for {
		line, rerr := reader.ReadString('\n')
		if text := strings.TrimRight(line, "\r\n"); text != "" {
			send(map[string]interface{}{"event": "out", "line": text})
		}
		if rerr != nil {
			if rerr != io.EOF {
				send(map[string]interface{}{"event": "out", "line": "读取输出失败: " + rerr.Error()})
			}
			break
		}
	}
	pr.Close()

	// 读取通道会阻塞到子进程退出；ctx 超时会导致 Wait 返回 err（超时杀进程），符合预期。
	exitErr := <-waitErr
	state := s.appState(context.WithoutCancel(r.Context()), id)
	actionLabel := map[string]string{"install": "安装", "remove": "卸载"}[action]

	done := map[string]interface{}{
		"event": "done", "ok": exitErr == nil, "state": state,
	}
	if exitErr != nil {
		done["err"] = exitErr.Error()
		s.audit(r, "app", "应用"+actionLabel+"失败", id)
		s.notifyAsync("app", "应用"+actionLabel+"失败: "+id, "exit: "+exitErr.Error())
	} else {
		s.audit(r, "app", "应用"+actionLabel, id)
		s.notifyAsync("app", "应用"+actionLabel+"完成", "应用: "+id)
	}
	send(done)
}
