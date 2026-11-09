package api

import "testing"

func TestParseSemver(t *testing.T) {
	cases := []struct {
		in   string
		want [3]int
	}{
		{"v1.2.3", [3]int{1, 2, 3}},
		{"1.2.3-rc1", [3]int{1, 2, 3}},
		{"v0.0.1", [3]int{0, 0, 1}},
		{"2.10", [3]int{2, 10, 0}},
		{"dev", [3]int{0, 0, 0}},
		{"v10.0.0+build5", [3]int{10, 0, 0}},
	}
	for _, c := range cases {
		if got := parseSemver(c.in); got != c.want {
			t.Fatalf("parseSemver(%q)=%v, want %v", c.in, got, c.want)
		}
	}
}

func TestVersionGreater(t *testing.T) {
	cases := []struct{ a, b string; want bool }{
		{"v1.2.4", "v1.2.3", true},
		{"v1.3.0", "v1.2.9", true},
		{"v2.0.0", "v1.9.9", true},
		{"v1.2.3", "v1.2.3", false},
		{"v1.2.3", "v1.2.4", false},
		{"v1.2.3", "v1.10.0", false},
		{"dev", "v1.0.0", false},
		{"v1.0.0", "dev", true},
	}
	for _, c := range cases {
		if got := VersionGreater(c.a, c.b); got != c.want {
			t.Fatalf("VersionGreater(%q,%q)=%v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestParseTagFromURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://github.com/LACS-Official/NanoPanel/releases/tag/v1.2.3", "1.2.3"},
		{"https://github.com/owner/repo/releases/v0.9.0", "0.9.0"},
		{"https://example.com/whatever", ""},
	}
	for _, c := range cases {
		if got := parseTagFromURL(c.in); got != c.want {
			t.Fatalf("parseTagFromURL(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

func TestUpdateArch(t *testing.T) {
	// updateArch 依赖运行时 GOARCH，仅校验它返回非空且仅接受已知架构。
	got := updateArch()
	switch got {
	case "amd64", "arm64", "armv7", "":
		// 允许空（如 386/riscv64 等暂不支持架构）。
	default:
		t.Fatalf("updateArch() 返回意外值: %q", got)
	}
}

