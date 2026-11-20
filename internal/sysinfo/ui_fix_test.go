package sysinfo

import (
	"testing"
)

// TestParseCPUModelFromFixture 用不同架构的 cpuinfo 片段验证型号解析兼容性。
func TestParseCPUModelFromFixture(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"x86 model name", "processor\t: 0\nmodel name\t: Intel(R) N100\n", "Intel(R) N100"},
		{"ARM Hardware 优先于 Processor", "Processor\t: ARMv7 Processor rev 4 (v7l)\nHardware\t: sun8i\n", "sun8i"},
		{"ARM only Processor", "Processor\t: ARMv8 Processor rev 4 (v8l)\nBogoMIPS\t: 24.0\n", "ARMv8 Processor rev 4 (v8l)"},
		{"ARM64 cpu model", "cpu model\t: Neoverse-N1\nFeatures\t: fp asimd\n", "Neoverse-N1"},
		{"大小写不敏感", "MODEL NAME\t: AMD Ryzen 7\n", "AMD Ryzen 7"},
		{"空字段跳过", "model name\t:   \nHardware\t: rockchip\n", "rockchip"},
		{"无匹配字段", "processor : 0\n", ""},
	}
	for _, c := range cases {
		if got := parseCPUModel(c.data); got != c.want {
			t.Errorf("%s: parseCPUModel = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestTempStatus 验证温度状态分级（含异常值识别）。
func TestTempStatus(t *testing.T) {
	cases := map[float64]string{
		-5:   "suspicious",
		0:    "ok",
		40:   "ok",
		64.9: "ok",
		65:   "warm",
		79.9: "warm",
		80:   "hot",
		94.9: "hot",
		95:   "critical",
		126:  "suspicious",
	}
	for c, want := range cases {
		if got := tempStatus(c); got != want {
			t.Errorf("tempStatus(%v) = %s, want %s", c, got, want)
		}
	}
}

// TestArchLabel 验证架构回退标签。
func TestArchLabel(t *testing.T) {
	cases := map[string]string{
		"amd64": "x86_64", "arm64": "ARM64", "arm": "ARM", "386": "x86", "riscv64": "riscv64",
	}
	for in, want := range cases {
		if got := archLabel(in); got != want {
			t.Errorf("archLabel(%s) = %s, want %s", in, got, want)
		}
	}
}
