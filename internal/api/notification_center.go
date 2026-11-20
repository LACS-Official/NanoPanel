package api

// 应用内通知中心：把 notifyAsync 推送的事件同时记入内存环形缓冲，
// 供面板底部状态栏展示。webhook 是"推到外部"，这里是"留在面板内"。
// 全部保存在内存，面板重启即清空（与外部通知的事件语义一致）。

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// 通知级别（前端据此着色与分档）。
const (
	notifInfo    = "info"
	notifSuccess = "success"
	notifWarn    = "warn"
	notifDanger  = "danger"
)

// notifyMaxItems 环形缓冲容量，足够覆盖日常运维通知而不占多少内存。
const notifyMaxItems = 200

type notifyItem struct {
	ID        int64  `json:"id"`
	Time      string `json:"time"`
	Level     string `json:"level"`
	Event     string `json:"event"`
	Title     string `json:"title"`
	Content   string `json:"content,omitempty"`
	Read      bool   `json:"read"`
	LevelText string `json:"level_text"`
}

var levelText = map[string]string{
	notifInfo:    "提示",
	notifSuccess: "成功",
	notifWarn:    "注意",
	notifDanger:  "告警",
}

// notifyLevel 根据事件类别与文案判定通知档位。
// 安全/失败/超温/资源告警 → 告警；恢复/完成/成功 → 成功；更新发现/回落 → 注意；其余 → 提示。
func notifyLevel(event, title, content string) string {
	text := strings.ToLower(title + " " + content)
	switch event {
	case "security":
		return notifDanger
	case "resource":
		// 资源阈值告警本身就是告警级别，只有明确回落/恢复才降级为成功。
		if strings.Contains(text, "回落") || strings.Contains(text, "恢复") || strings.Contains(text, "正常") {
			return notifSuccess
		}
		return notifDanger
	case "health", "temp":
		if strings.Contains(text, "失败") || strings.Contains(text, "过高") ||
			strings.Contains(text, "超温") || strings.Contains(text, "掉线") ||
			strings.Contains(text, "异常") {
			return notifDanger
		}
		if strings.Contains(text, "回落") || strings.Contains(text, "恢复") {
			return notifSuccess
		}
		return notifWarn
	case "update":
		if strings.Contains(text, "失败") {
			return notifDanger
		}
		if strings.Contains(text, "发现新版本") {
			return notifWarn
		}
		return notifSuccess
	case "app", "task":
		if strings.Contains(text, "失败") || strings.Contains(text, "错误") {
			return notifDanger
		}
		if strings.Contains(text, "完成") || strings.Contains(text, "成功") {
			return notifSuccess
		}
	}
	if strings.Contains(text, "失败") || strings.Contains(text, "错误") {
		return notifDanger
	}
	if strings.Contains(text, "成功") || strings.Contains(text, "完成") {
		return notifSuccess
	}
	return notifInfo
}

// pushNotify 追加一条应用内通知，超出容量时丢弃最早的。
func (s *Server) pushNotify(event, title, content string) {
	level := notifyLevel(event, title, content)
	item := notifyItem{
		ID:        time.Now().UnixNano(),
		Time:      time.Now().Format("01-02 15:04:05"),
		Level:     level,
		LevelText: levelText[level],
		Event:     event,
		Title:     title,
		Content:   content,
	}
	s.notifyMu.Lock()
	s.notifies = append(s.notifies, item)
	if len(s.notifies) > notifyMaxItems {
		s.notifies = append([]notifyItem(nil), s.notifies[len(s.notifies)-notifyMaxItems:]...)
	}
	s.notifyMu.Unlock()
}

// Notifications 返回通知列表（最新在前）与未读计数。?limit= 限制条数，默认全部。
func (s *Server) Notifications(w http.ResponseWriter, r *http.Request) {
	limit := notifyMaxItems
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n < limit {
			limit = n
		}
	}
	s.notifyMu.Lock()
	unread := 0
	for _, it := range s.notifies {
		if !it.Read {
			unread++
		}
	}
	items := make([]notifyItem, 0, limit)
	for i := len(s.notifies) - 1; i >= 0 && len(items) < limit; i-- {
		items = append(items, s.notifies[i])
	}
	s.notifyMu.Unlock()
	ok(w, map[string]interface{}{
		"items":  items,
		"unread": unread,
		"total":  len(items),
	})
}

// MarkNotificationsRead 标记通知为已读。body 为空或 all=true 时全部已读，
// 否则按 ids 数组标记指定项。
func (s *Server) MarkNotificationsRead(w http.ResponseWriter, r *http.Request) {
	var req struct {
		All bool    `json:"all"`
		IDs []int64 `json:"ids"`
	}
	_ = decodeJSON(w, r, &req)
	s.notifyMu.Lock()
	if req.All {
		for i := range s.notifies {
			s.notifies[i].Read = true
		}
	} else {
		set := map[int64]bool{}
		for _, id := range req.IDs {
			set[id] = true
		}
		for i := range s.notifies {
			if set[s.notifies[i].ID] {
				s.notifies[i].Read = true
			}
		}
	}
	s.notifyMu.Unlock()
	ok(w, map[string]string{"msg": "已更新"})
}

// ClearNotifications 清空所有通知。
func (s *Server) ClearNotifications(w http.ResponseWriter, r *http.Request) {
	s.notifyMu.Lock()
	s.notifies = nil
	s.notifyMu.Unlock()
	ok(w, map[string]string{"msg": "已清空"})
}
