// Package version 保存构建期注入的版本信息。
package version

import "fmt"

// 这些变量由 Makefile / CI 通过 -ldflags "-X ..." 注入。
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// Info 返回用于展示和日志的版本字符串。
func Info() string {
	if Version == "dev" {
		return "dev"
	}
	return fmt.Sprintf("%s (%s, %s)", Version, Commit, Date)
}
