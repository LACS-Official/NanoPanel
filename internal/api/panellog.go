package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// auditEntry 面板操作日志的一条记录。
type auditEntry struct {
	Time     string `json:"time"`
	IP       string `json:"ip"`
	User     string `json:"user"`
	Category string `json:"category"`
	Action   string `json:"action"`
	Detail   string `json:"detail,omitempty"`
}

// defaultPanelLogDir 面板日志目录。非 Linux 环境（开发/测试）落到临时目录。
func defaultPanelLogDir() string {
	if runtime.GOOS == "linux" {
		return "/var/log/nanopanel"
	}
	return filepath.Join(os.TempDir(), "nanopanel-logs")
}

// audit 追加一条面板操作日志，并按事件类别触发 webhook 通知。
// 日志写失败只影响日志本身，不影响业务操作。
func (s *Server) audit(r *http.Request, category, action, detail string) {
	if s.logDir == "" {
		s.logDir = defaultPanelLogDir()
	}
	entry := auditEntry{
		Time:     time.Now().Format("2006-01-02 15:04:05"),
		IP:       clientIP(r),
		User:     s.username(),
		Category: category,
		Action:   action,
		Detail:   detail,
	}
	go s.appendAudit(entry)
	s.notifyAsync(category, action, detail)
}

// auditMutex 保护日志文件的追加与轮转。appendAudit 在独立 goroutine 中执行，
// 无锁并发追加同一文件可能产生交错行。
var auditMutex sync.Mutex

// 面板日志按大小轮转：panel.log → panel.log.1 → panel.log.2，最多保留 3 份。
const (
	auditLogBackups  = 2
	auditLogFilename = "panel.log"
)

// auditLogMaxSize 轮转阈值，测试可调小以触发轮转。
var auditLogMaxSize int64 = 5 << 20 // 5MB

// rotateAuditLog 在文件超过上限时滚动轮转，保留最近 auditLogBackups 份历史。
func rotateAuditLog(path string) {
	st, err := os.Stat(path)
	if err != nil || st.Size() < auditLogMaxSize {
		return
	}
	for i := auditLogBackups - 1; i >= 1; i-- {
		older := fmt.Sprintf("%s.%d", path, i)
		newer := fmt.Sprintf("%s.%d", path, i+1)
		if i == auditLogBackups-1 {
			_ = os.Remove(newer) // 最老的一份直接删除
		}
		if _, err := os.Stat(older); err == nil {
			_ = os.Rename(older, newer)
		}
	}
	_ = os.Rename(path, path+".1")
}

func (s *Server) appendAudit(entry auditEntry) {
	auditMutex.Lock()
	defer auditMutex.Unlock()
	if err := os.MkdirAll(s.logDir, 0750); err != nil {
		log.Printf("创建面板日志目录失败: %v", err)
		return
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	path := filepath.Join(s.logDir, auditLogFilename)
	rotateAuditLog(path)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		log.Printf("写入面板日志失败: %v", err)
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}

// PanelLogs 返回最近的面板操作日志，最新在前。
func (s *Server) PanelLogs(w http.ResponseWriter, r *http.Request) {
	if s.logDir == "" {
		s.logDir = defaultPanelLogDir()
	}
	path := filepath.Join(s.logDir, "panel.log")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			ok(w, []auditEntry{})
			return
		}
		fail(w, http.StatusInternalServerError, "读取面板日志失败: "+err.Error())
		return
	}

	const maxEntries = 300
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	entries := make([]auditEntry, 0, 64)
	for i := len(lines) - 1; i >= 0 && len(entries) < maxEntries; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var e auditEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // 跳过被破坏的行
		}
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []auditEntry{}
	}
	w.Header().Set("X-Total", fmt.Sprintf("%d", len(lines)))
	ok(w, entries)
}
