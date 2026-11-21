package api

// 指示灯（LED）控制接口。
// 三项能力对应常见的运维脚本：枚举 /sys/class/leds、单灯开关、
// 以及「一键关闭除电源灯外的所有灯」。所有写操作均经过登录鉴权与审计。

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"nanopanel/internal/sysinfo"
)

// ledRoot 返回 sysfs LED 目录，测试可通过 s.ledsRoot 注入临时目录。
func (s *Server) ledRoot() string {
	if s.ledsRoot != "" {
		return s.ledsRoot
	}
	return sysinfo.DefaultLEDRoot
}

// ledUnavailable 把「平台不支持」统一包装成 200 响应，便于前端友好提示。
func ledUnavailable(w http.ResponseWriter, root string) {
	ok(w, map[string]interface{}{
		"leds":      []sysinfo.LED{},
		"supported": false,
		"root":      root,
		"message":   "当前系统未提供 /sys/class/leds（需 Linux 且存在板载指示灯）",
	})
}

// ListLEDs 返回全部指示灯及其当前亮度、最大亮度、触发器与可写状态。
func (s *Server) ListLEDs(w http.ResponseWriter, r *http.Request) {
	root := s.ledRoot()
	leds, err := sysinfo.ListLEDs(root)
	if err != nil {
		if errors.Is(err, sysinfo.ErrLEDUnsupported) {
			ledUnavailable(w, root)
			return
		}
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]interface{}{
		"leds":      leds,
		"supported": true,
		"root":      root,
	})
}

type ledSetReq struct {
	Name string `json:"name"`
	On   *bool  `json:"on"`
}

// SetLED 开启或关闭单个指示灯。
// on=true 写 max_brightness，on=false 写 0（并解除内核触发器）。
func (s *Server) SetLED(w http.ResponseWriter, r *http.Request) {
	var req ledSetReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" || req.On == nil {
		fail(w, http.StatusBadRequest, "缺少指示灯名称或开关状态")
		return
	}
	root := s.ledRoot()
	leds, err := sysinfo.ListLEDs(root)
	if err != nil {
		if errors.Is(err, sysinfo.ErrLEDUnsupported) {
			fail(w, http.StatusBadRequest, "当前系统不支持指示灯控制")
			return
		}
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	value, max := 0, 0
	found := false
	for _, l := range leds {
		if l.Name == req.Name {
			found = true
			max = l.MaxBrightness
			break
		}
	}
	if !found {
		fail(w, http.StatusNotFound, "指示灯不存在: "+req.Name)
		return
	}
	if *req.On {
		value = max
		if value <= 0 {
			value = 1
		}
	}
	if err := sysinfo.SetLED(root, req.Name, value); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	state := "关闭"
	if *req.On {
		state = "开启"
	}
	s.audit(r, "system", "指示灯"+state,
		fmt.Sprintf("%s (brightness=%d)", req.Name, value))
	ok(w, map[string]interface{}{
		"name": req.Name, "on": *req.On, "brightness": value,
	})
}

type ledAllReq struct {
	On        bool `json:"on"`
	KeepPower bool `json:"keep_power"`
}

// SetAllLEDs 批量开关。keep_power=true 时跳过电源指示灯，
// 对应「关掉所有状态灯但别把电源灯也熄了」的常见需求。
func (s *Server) SetAllLEDs(w http.ResponseWriter, r *http.Request) {
	var req ledAllReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	root := s.ledRoot()
	leds, err := sysinfo.ListLEDs(root)
	if err != nil {
		if errors.Is(err, sysinfo.ErrLEDUnsupported) {
			fail(w, http.StatusBadRequest, "当前系统不支持指示灯控制")
			return
		}
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	changed, skipped := 0, 0
	failed := make([]string, 0)
	for _, l := range leds {
		if req.KeepPower && l.IsPower {
			skipped++
			continue
		}
		value := 0
		if req.On {
			value = l.MaxBrightness
			if value <= 0 {
				value = 1
			}
		}
		if err := sysinfo.SetLED(root, l.Name, value); err != nil {
			failed = append(failed, l.Name+": "+err.Error())
			continue
		}
		changed++
	}
	state := "全部关闭"
	if req.On {
		state = "全部开启"
	}
	if req.KeepPower {
		state += "（保留电源灯）"
	}
	s.audit(r, "system", "指示灯"+state,
		fmt.Sprintf("成功 %d 个，跳过 %d 个，失败 %d 个", changed, skipped, len(failed)))
	ok(w, map[string]interface{}{
		"changed": changed, "skipped": skipped, "failed": failed,
	})
}
