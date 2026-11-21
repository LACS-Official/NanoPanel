package sysinfo

// LED sysfs 读写的单元测试：用临时目录模拟 /sys/class/leds，
// 覆盖枚举、trigger 解析、关闭时解除触发器、名称校验与权限边界。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeLED(t *testing.T, root, name, brightness, max, trigger string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for fn, val := range map[string]string{
		"brightness":     brightness,
		"max_brightness": max,
	} {
		if err := os.WriteFile(filepath.Join(dir, fn), []byte(val), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if trigger != "" {
		if err := os.WriteFile(filepath.Join(dir, "trigger"), []byte(trigger), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func readLEDAttr(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func TestListLEDs(t *testing.T) {
	root := t.TempDir()
	powerDir := makeLED(t, root, "power", "1", "1", "none rc-feedback [heartbeat] mmc0")
	_ = powerDir
	makeLED(t, root, "led0", "255", "255", "[none] rc-feedback kbd-scrolllock")
	makeLED(t, root, "led1", "0", "255", "")
	// 无 brightness 文件的条目应被忽略
	if err := os.MkdirAll(filepath.Join(root, "noise"), 0755); err != nil {
		t.Fatal(err)
	}

	leds, err := ListLEDs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(leds) != 3 {
		t.Fatalf("应枚举到 3 个 LED，实际 %d: %+v", len(leds), leds)
	}
	if leds[0].Name != "power" || !leds[0].IsPower {
		t.Fatalf("电源灯应排在最前并标记 is_power: %+v", leds[0])
	}
	byName := map[string]LED{}
	for _, l := range leds {
		byName[l.Name] = l
	}
	if byName["led0"].Trigger != "none" {
		t.Fatalf("trigger 应解析方括号中的当前值，实际 %q", byName["led0"].Trigger)
	}
	if byName["power"].Trigger != "heartbeat" {
		t.Fatalf("power 的 trigger 应为 heartbeat，实际 %q", byName["power"].Trigger)
	}
	if byName["led0"].Brightness != 255 || byName["led0"].MaxBrightness != 255 {
		t.Fatalf("亮度读取不正确: %+v", byName["led0"])
	}
	if byName["led1"].Trigger != "" || byName["led1"].Brightness != 0 {
		t.Fatalf("无 trigger 文件时应为空且亮度为 0: %+v", byName["led1"])
	}
	if !byName["led0"].Writable {
		t.Fatal("普通可写文件应标记为 writable")
	}
}

func TestListLEDsUnsupported(t *testing.T) {
	_, err := ListLEDs(filepath.Join(t.TempDir(), "missing"))
	if !errors.Is(err, ErrLEDUnsupported) {
		t.Fatalf("目录不存在应返回 ErrLEDUnsupported，实际 %v", err)
	}
}

func TestSetLEDTurnOffDetachesTrigger(t *testing.T) {
	root := t.TempDir()
	dir := makeLED(t, root, "blue-led", "255", "255", "none [heartbeat] cpu0 mmc0")
	if err := SetLED(root, "blue-led", 0); err != nil {
		t.Fatal(err)
	}
	if got := readLEDAttr(t, dir, "brightness"); got != "0" {
		t.Fatalf("关闭后 brightness 应为 0，实际 %q", got)
	}
	if got := readLEDAttr(t, dir, "trigger"); got != "none" {
		t.Fatalf("关闭时未解除 trigger，heartbeat 会重新点亮，实际 %q", got)
	}
}

func TestSetLEDTurnOnUsesMaxBrightness(t *testing.T) {
	root := t.TempDir()
	dir := makeLED(t, root, "status", "0", "255", "none [none] heartbeat")
	if err := SetLED(root, "status", 255); err != nil {
		t.Fatal(err)
	}
	if got := readLEDAttr(t, dir, "brightness"); got != "255" {
		t.Fatalf("开启应写到 max_brightness，实际 %q", got)
	}
	if got := readLEDAttr(t, dir, "trigger"); got != "none [none] heartbeat" {
		t.Fatalf("开启不应改动 trigger，实际 %q", got)
	}
}

func TestSetLEDValidation(t *testing.T) {
	root := t.TempDir()
	makeLED(t, root, "led0", "1", "1", "")

	for _, bad := range []string{"", "../etc", "a/b", "a\\b", "..", "foo/../led0"} {
		if err := SetLED(root, bad, 0); !errors.Is(err, ErrLEDNotFound) {
			t.Fatalf("非法名称 %q 应被拒绝，实际 %v", bad, err)
		}
	}
	if err := SetLED(root, "led0", 2); err == nil {
		t.Fatal("超过 max_brightness 的值应被拒绝")
	}
	if err := SetLED(root, "absent", 0); !errors.Is(err, ErrLEDNotFound) {
		t.Fatalf("不存在的灯应返回 ErrLEDNotFound，实际 %v", err)
	}
}

func TestValidLEDNameRealWorldNames(t *testing.T) {
	for _, n := range []string{"blue:heartbeat", "input3::capslock", "led0", "green:power", "ACPI::power"} {
		if !validLEDName(n) {
			t.Fatalf("真实 LED 名称应通过校验: %q", n)
		}
	}
	for _, n := range []string{"", "a/b", "a\\b", "..", "../x", "a..b", "-bad", "with space"} {
		if validLEDName(n) {
			t.Fatalf("非法名称不应通过校验: %q", n)
		}
	}
}

func TestIsPowerLED(t *testing.T) {
	for _, n := range []string{"power", "green:power", "POWER", "power:blue"} {
		if !isPowerLED(n) {
			t.Fatalf("应识别为电源灯: %q", n)
		}
	}
	for _, n := range []string{"led0", "blue:heartbeat", "wlan"} {
		if isPowerLED(n) {
			t.Fatalf("不应识别为电源灯: %q", n)
		}
	}
}
