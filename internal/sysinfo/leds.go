package sysinfo

// LED 指示灯的枚举与开关（Linux sysfs: /sys/class/leds）。
//
// 与 Temperatures / Disks 等采集逻辑不同，这里既要读也要写，因此对名称做了
// 严格白名单校验（禁止路径穿越），且写入前必须确认该灯确实由 ListLEDs 枚举到，
// 避免通过构造名称去写任意 sysfs 文件。
//
// root 由调用方传入而非写死全局常量：既便于在 Windows / 容器等没有 sysfs 的
// 环境下返回明确的不支持提示，也便于单元测试用临时目录模拟真实 sysfs。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// DefaultLEDRoot 是 Linux 内核暴露 LED 设备的标准目录。
const DefaultLEDRoot = "/sys/class/leds"

var (
	// ErrLEDUnsupported 表示当前系统没有 /sys/class/leds（Windows、容器或设备无 LED）。
	ErrLEDUnsupported = errors.New("当前系统未提供 /sys/class/leds 接口")
	// ErrLEDNotFound 表示请求的指示灯不存在或名称非法。
	ErrLEDNotFound = errors.New("指示灯不存在")

	// ledNameRe 只允许 LED 名称中真实出现的字符，天然排除 / 与 \ 等路径分隔符。
	ledNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)
)

// LED 描述一个可由 sysfs 控制的指示灯。
type LED struct {
	Name          string `json:"name"`
	Brightness    int    `json:"brightness"`
	MaxBrightness int    `json:"max_brightness"`
	Trigger       string `json:"trigger"` // 当前内核触发器；空串表示无 trigger 文件
	Writable      bool   `json:"writable"`
	IsPower       bool   `json:"is_power"` // 电源指示灯，批量关闭时默认保留
}

// validLEDName 拒绝空名、路径分隔符、.. 穿越与奇异字符。
func validLEDName(name string) bool {
	if !ledNameRe.MatchString(name) || strings.Contains(name, "..") {
		return false
	}
	return !strings.ContainsAny(name, "/\\")
}

// isPowerLED 判定是否为电源灯，与常见运维脚本的 *power* 规则一致。
func isPowerLED(name string) bool {
	return strings.Contains(strings.ToLower(name), "power")
}

// readInt 读取 sysfs 中的整数属性，缺失或解析失败时返回 0。
func readLEDInt(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// readTrigger 解析 trigger 文件里被方括号标记的当前触发器。
// 典型内容："none rc-feedback kbd-scrolllock [heartbeat] mmc0 ..."。
func readTrigger(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(raw))
	if i := strings.IndexByte(s, '['); i >= 0 {
		if j := strings.IndexByte(s[i+1:], ']'); j >= 0 {
			return s[i+1 : i+1+j]
		}
	}
	if s == "" {
		return ""
	}
	// 少数驱动不带方括号，直接给出首项。
	return strings.Fields(s)[0]
}

// writableFile 通过只打开不写入来探测文件是否可写（sysfs 上安全，不改变亮度）。
func writableFile(path string) bool {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// ListLEDs 枚举 root 下所有可控指示灯，按「电源灯优先 + 名称」排序。
// root 不存在时返回 ErrLEDUnsupported；目录存在但为空时返回空切片（非 nil）。
func ListLEDs(root string) ([]LED, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrLEDUnsupported
		}
		return nil, fmt.Errorf("读取 LED 目录失败: %w", err)
	}
	out := make([]LED, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if !validLEDName(name) {
			continue
		}
		dir := filepath.Join(root, name)
		info, serr := os.Stat(dir) // /sys/class/leds/* 多为符号链接，需跟随后再判断
		if serr != nil || !info.IsDir() {
			continue
		}
		bpath := filepath.Join(dir, "brightness")
		if _, berr := os.Stat(bpath); berr != nil {
			continue
		}
		max := readLEDInt(filepath.Join(dir, "max_brightness"))
		if max <= 0 {
			max = 1
		}
		brightness := readLEDInt(bpath)
		if brightness > max {
			brightness = max
		}
		out = append(out, LED{
			Name:          name,
			Brightness:    brightness,
			MaxBrightness: max,
			Trigger:       readTrigger(filepath.Join(dir, "trigger")),
			Writable:      writableFile(bpath),
			IsPower:       isPowerLED(name),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsPower != out[j].IsPower {
			return out[i].IsPower
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// SetLED 设置指定指示灯的亮度。
// 关闭（value=0）时先把 trigger 置为 none：heartbeat / cpu / mmc 等内核触发器
// 会在写入 brightness 后立刻重新点亮，仅写 0 无法真正关灯。
func SetLED(root, name string, value int) error {
	if !validLEDName(name) {
		return ErrLEDNotFound
	}
	leds, err := ListLEDs(root)
	if err != nil {
		return err
	}
	max := 0
	found := false
	for _, l := range leds {
		if l.Name == name {
			found = true
			max = l.MaxBrightness
			break
		}
	}
	if !found {
		return ErrLEDNotFound
	}
	if value < 0 || value > max {
		return fmt.Errorf("亮度需在 0-%d 之间", max)
	}
	dir := filepath.Join(root, name)
	if value == 0 {
		tpath := filepath.Join(dir, "trigger")
		if _, serr := os.Stat(tpath); serr == nil {
			// 部分 LED 没有可写的 trigger，忽略错误后继续写 brightness。
			_ = os.WriteFile(tpath, []byte("none"), 0)
		}
	}
	bpath := filepath.Join(dir, "brightness")
	if err := os.WriteFile(bpath, []byte(strconv.Itoa(value)), 0); err != nil {
		return fmt.Errorf("写入 brightness 失败（需要 root 权限或该灯只读）: %w", err)
	}
	return nil
}
