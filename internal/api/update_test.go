package api

// 更新模块测试：语义化版本比较（含 v 前缀 / 预发布后缀 / 畸形输入）。

import "testing"

func TestVersionGreater(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.1.1", "v0.1.0", true},
		{"v0.2.0", "v0.1.9", true},
		{"v1.0.0", "v0.9.9", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.1.1", false},
		{"0.1.1", "v0.1.0", true},       // 无 v 前缀
		{"v0.1.0", "0.1.0", false},      // 混用前缀
		{"v1.0.0-rc1", "v0.9.9", true},  // 预发布后缀被忽略
		{"v1.0.0-rc1", "v1.0.0", false}, // rc 不大于正式版（同号）
		{"v1.0.1+build5", "v1.0.0", true},
		{"v10.0.0", "v9.99.99", true}, // 多位数
		{"", "v0.1.0", false},
		{"v0.1.0", "", true},
		{"garbage", "v0.0.1", false}, // 畸形输入解析为 0
		{"v0.0.2", "garbage", true},
	}
	for _, c := range cases {
		if got := VersionGreater(c.a, c.b); got != c.want {
			t.Errorf("VersionGreater(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestParseSemver(t *testing.T) {
	cases := []struct {
		in   string
		want [3]int
	}{
		{"v1.2.3", [3]int{1, 2, 3}},
		{"1.2", [3]int{1, 2, 0}},
		{"v2", [3]int{2, 0, 0}},
		{" v1.2.3 ", [3]int{1, 2, 3}},
		{"v1.2.3-beta.2", [3]int{1, 2, 3}},
		{"v1.2.3+meta", [3]int{1, 2, 3}},
		{"vv1.2.3", [3]int{1, 2, 3}}, // 前缀多余 v 也能剥掉
		{"abc", [3]int{0, 0, 0}},
		{"", [3]int{0, 0, 0}},
	}
	for _, c := range cases {
		if got := parseSemver(c.in); got != c.want {
			t.Errorf("parseSemver(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestUpdateArch(t *testing.T) {
	// 该函数依赖 runtime.GOARCH，只需确认常见架构下非空且为已知值。
	a := updateArch()
	switch a {
	case "amd64", "arm64", "armv7", "":
		// GOARCH=arm 映射 armv7；其余未知架构返回空
	default:
		t.Fatalf("updateArch() 返回未知值 %q", a)
	}
}
