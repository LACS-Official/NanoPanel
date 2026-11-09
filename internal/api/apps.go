package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	appIDRe    = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	appFieldRe = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)
	appPortRe  = regexp.MustCompile(`^[0-9]{1,5}$`)
	appYesNo   = []string{"yes", "no"}
)

// AppConfigField 应用配置项的声明，由 manifest.json 提供。
type AppConfigField struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Type        string   `json:"type"` // text | password | select | textarea | number
	Default     string   `json:"default"`
	Placeholder string   `json:"placeholder,omitempty"`
	Description string   `json:"description,omitempty"`
	Options     []string `json:"options,omitempty"` // select 的候选值
}

// AppMeta 应用商店里的应用信息。
type AppMeta struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	Version      string           `json:"version"`
	Icon         string           `json:"icon"`
	Category     string           `json:"category"`
	Unit         string           `json:"unit,omitempty"`              // 对应的 systemd 单元，供日志筛选与存活检测
	State        string           `json:"state"`                       // installed / not_installed / unknown
	Status       string           `json:"status,omitempty"`            // running / down / unknown / not_installed
	InstalledVer string           `json:"installed_version,omitempty"` // 安装时记录的脚本版本，与 Version 不一致即可升级
	Config       []AppConfigField `json:"config,omitempty"`            // 常用配置项声明
}

type appManifestFile struct {
	Version int       `json:"version"`
	Apps    []AppMeta `json:"apps"`
}

func (s *Server) loadManifest() (*appManifestFile, error) {
	data, err := os.ReadFile(s.cfg.ManifestPath)
	if err != nil {
		return nil, err
	}
	var m appManifestFile
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	for _, a := range m.Apps {
		if !appIDRe.MatchString(a.ID) {
			return nil, fmt.Errorf("清单中包含非法应用 ID: %q", a.ID)
		}
		for _, f := range a.Config {
			if !appFieldRe.MatchString(f.Name) {
				return nil, fmt.Errorf("应用 %s 配置字段名不合法: %q", a.ID, f.Name)
			}
			switch f.Type {
			case "text", "password", "select", "textarea", "number":
			default:
				return nil, fmt.Errorf("应用 %s 字段 %s 类型不合法: %q", a.ID, f.Name, f.Type)
			}
			if f.Type == "select" && len(f.Options) == 0 {
				return nil, fmt.Errorf("应用 %s 字段 %s 缺少 options", a.ID, f.Name)
			}
		}
	}
	return &m, nil
}

func (s *Server) findApp(id string) *AppMeta {
	m, err := s.loadManifest()
	if err != nil {
		return nil
	}
	for i := range m.Apps {
		if m.Apps[i].ID == id {
			return &m.Apps[i]
		}
	}
	return nil
}

// appState 执行 status.sh，输出 installed / not_installed，超时或异常返回 unknown。
func (s *Server) appState(ctx context.Context, id string) string {
	if !appIDRe.MatchString(id) {
		return "unknown"
	}
	script := filepath.Join(s.cfg.AppsDir, id, "status.sh")
	if _, err := os.Stat(script); err != nil {
		return "unknown"
	}
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "bash", script).Output()
	if err != nil {
		return "unknown"
	}
	state := strings.TrimSpace(string(out))
	if state != "installed" && state != "not_installed" {
		return "unknown"
	}
	return state
}

// appVersionFile 记录应用安装时的脚本版本，用于「可升级」判断。
func (s *Server) appVersionFile(id string) string {
	return filepath.Join(s.cfg.AppsDir, id, "installed_version")
}

// readInstalledVersion 返回记录的安装版本；无记录返回空串（不视为可升级）。
func (s *Server) readInstalledVersion(id string) string {
	b, err := os.ReadFile(s.appVersionFile(id))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// ListApps 列出应用商店中的应用及安装状态。
func (s *Server) ListApps(w http.ResponseWriter, r *http.Request) {
	m, err := s.loadManifest()
	if err != nil {
		fail(w, http.StatusInternalServerError, "读取应用清单失败: "+err.Error())
		return
	}
	for i := range m.Apps {
		m.Apps[i].State = s.appState(r.Context(), m.Apps[i].ID)
		m.Apps[i].Status = s.appRuntimeStatus(r.Context(), &m.Apps[i]).Status
		if m.Apps[i].State == "installed" {
			m.Apps[i].InstalledVer = s.readInstalledVersion(m.Apps[i].ID)
		}
	}
	ok(w, m.Apps)
}

// appEnvFile 应用配置持久化文件路径。
func (s *Server) appEnvFile(id string) string {
	return filepath.Join(s.cfg.AppsDir, id, "config.env")
}

// readAppValues 读取已保存的配置值；文件不存在时返回空 map。
func (s *Server) readAppValues(id string) (map[string]string, error) {
	data, err := os.ReadFile(s.appEnvFile(id))
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		values[strings.TrimSpace(k)] = v
	}
	return values, nil
}

// writeAppValues 以 0600 权限保存配置值。
func (s *Server) writeAppValues(id string, values map[string]string) error {
	var b strings.Builder
	for k, v := range values {
		if strings.ContainsAny(k+v, "\n\x00") {
			return fmt.Errorf("配置值包含非法字符")
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v)
		b.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Join(s.cfg.AppsDir, id), 0750); err != nil {
		return err
	}
	return os.WriteFile(s.appEnvFile(id), []byte(b.String()), 0600)
}

// appScriptEnv 构造传给应用脚本的额外环境变量。
// NP_APPS_DIR 让 config.sh 能定位并回写自己的 config.env（如 mihomo 自动生成的控制密钥）。
func (s *Server) appScriptEnv(id string) ([]string, error) {
	values, err := s.readAppValues(id)
	if err != nil {
		return nil, err
	}
	env := make([]string, 0, len(values)+1)
	env = append(env, "NP_APPS_DIR="+s.cfg.AppsDir)
	for k, v := range values {
		env = append(env, k+"="+v)
	}
	return env, nil
}

// validateAppValues 依据字段声明校验用户提交的配置值。
func validateAppValues(fields []AppConfigField, input map[string]string) (map[string]string, error) {
	saved := map[string]string{}
	for _, f := range fields {
		v, exists := input[f.Name]
		if !exists {
			continue
		}
		v = strings.TrimSpace(v)
		switch f.Type {
		case "select":
			if !containsStr(f.Options, v) {
				return nil, fmt.Errorf("%s 的取值必须是 %s 之一", f.Label, strings.Join(f.Options, "/"))
			}
		case "number":
			if v != "" {
				if !appPortRe.MatchString(v) {
					return nil, fmt.Errorf("%s 必须是数字", f.Label)
				}
				n, _ := strconv.Atoi(v)
				if n < 1 || n > 65535 {
					return nil, fmt.Errorf("%s 超出端口范围 1-65535", f.Label)
				}
			}
		}
		if len(v) > 512 {
			return nil, fmt.Errorf("%s 过长", f.Label)
		}
		saved[f.Name] = v
	}
	// 拒绝未在清单声明的字段，防止注入任意环境变量。
	for k := range input {
		if !hasField(fields, k) {
			return nil, fmt.Errorf("未知配置项: %s", k)
		}
	}
	return saved, nil
}

func hasField(fields []AppConfigField, name string) bool {
	for _, f := range fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// AppAction 对指定应用执行 install/remove。
func (s *Server) AppAction(w http.ResponseWriter, r *http.Request) {
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

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", script)
	if extra, err := s.appScriptEnv(id); err == nil {
		cmd.Env = append(os.Environ(), extra...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		fail(w, http.StatusInternalServerError,
			fmt.Sprintf("操作失败: %v\n%s", err, strings.TrimSpace(string(out))))
		return
	}

	actionLabel := map[string]string{"install": "安装", "remove": "卸载"}[action]
	s.audit(r, "app", "应用"+actionLabel, id)
	ok(w, map[string]string{
		"id": id, "action": action, "output": string(out),
		"state": s.appState(ctx, id),
	})
}

// GetAppConfig 返回指定应用的配置项声明与当前值。
func (s *Server) GetAppConfig(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if !appIDRe.MatchString(id) {
		fail(w, http.StatusBadRequest, "应用 ID 不合法")
		return
	}
	app := s.findApp(id)
	if app == nil {
		fail(w, http.StatusNotFound, "应用不存在")
		return
	}
	saved, err := s.readAppValues(id)
	if err != nil {
		fail(w, http.StatusInternalServerError, "读取配置失败: "+err.Error())
		return
	}
	values := map[string]string{}
	for _, f := range app.Config {
		if v, exists := saved[f.Name]; exists {
			values[f.Name] = v
		} else {
			values[f.Name] = f.Default
		}
	}
	ok(w, map[string]interface{}{
		"id":     id,
		"name":   app.Name,
		"unit":   app.Unit,
		"state":  s.appState(r.Context(), id),
		"fields": app.Config,
		"values": values,
	})
}

type appConfigReq struct {
	Values map[string]string `json:"values"`
}

// SaveAppConfig 保存应用配置并调用 config.sh 使其生效。
func (s *Server) SaveAppConfig(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if !appIDRe.MatchString(id) {
		fail(w, http.StatusBadRequest, "应用 ID 不合法")
		return
	}
	app := s.findApp(id)
	if app == nil {
		fail(w, http.StatusNotFound, "应用不存在")
		return
	}
	if len(app.Config) == 0 {
		fail(w, http.StatusBadRequest, "该应用没有可配置项")
		return
	}
	if st := s.appState(r.Context(), id); st != "installed" {
		fail(w, http.StatusBadRequest, "应用尚未安装，无法配置")
		return
	}

	var req appConfigReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	values, err := validateAppValues(app.Config, req.Values)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}

	// 与已有值合并：未出现在本次提交里的字段保持原值。
	if old, err := s.readAppValues(id); err == nil {
		for k, v := range old {
			if hasField(app.Config, k) {
				if _, ok2 := values[k]; !ok2 {
					values[k] = v
				}
			}
		}
	}
	if err := s.writeAppValues(id, values); err != nil {
		fail(w, http.StatusInternalServerError, "保存配置失败: "+err.Error())
		return
	}

	// config.sh 存在时执行以应用配置；不存在则仅保存。
	script := filepath.Join(s.cfg.AppsDir, id, "config.sh")
	if _, err := os.Stat(script); err == nil {
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "bash", script)
		cmd.Env = append(os.Environ(), s.envPairs(values)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			fail(w, http.StatusInternalServerError,
				fmt.Sprintf("配置脚本执行失败: %v\n%s", err, strings.TrimSpace(string(out))))
			return
		}
		s.audit(r, "app", "修改应用配置", id)
		ok(w, map[string]string{"id": id, "msg": "配置已保存并应用", "output": string(out)})
		return
	}
	s.audit(r, "app", "修改应用配置", id)
	ok(w, map[string]string{"id": id, "msg": "配置已保存"})
}

func (s *Server) envPairs(values map[string]string) []string {
	env := make([]string, 0, len(values))
	for k, v := range values {
		env = append(env, k+"="+v)
	}
	return env
}

// notifyEventOptions 提供给前端的订阅事件说明。
func notifyEventOptions() []map[string]string {
	return []map[string]string{
		{"key": "login", "label": "登录"},
		{"key": "service", "label": "服务操作"},
		{"key": "process", "label": "进程操作"},
		{"key": "app", "label": "应用商店"},
		{"key": "settings", "label": "面板设置"},
		{"key": "system", "label": "系统操作"},
		{"key": "health", "label": "应用掉线 / 恢复"},
		{"key": "disk", "label": "磁盘挂载"},
		{"key": "security", "label": "安全告警（登录锁定）"},
		{"key": "temp", "label": "温度告警（超温 / 回落）"},
		{"key": "file", "label": "文件管理操作"},
		{"key": "task", "label": "定时任务执行结果"},
	}
}
