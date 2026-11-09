package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"nanopanel/internal/version"
)

// updateArch 返回当前运行架构对应的发布资产后缀。
func updateArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	case "arm":
		return "armv7"
	}
	return ""
}

// StartAutoUpdate 启动后台自动更新检查：启动后延迟 5 分钟做首次检查，
// 之后每小时轮询一次；当开启自动更新且当前小时等于配置的检查小时时，
// 发现新版本会推送 "update" 通知并在开启自动更新时下载安装。
func (s *Server) StartAutoUpdate(ctx context.Context) {
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Minute):
		}
		s.autoUpdateTick()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.autoUpdateTick()
			}
		}
	}()
}

// autoUpdateTick 执行一次后台检查：仅在开启自动更新且当前小时匹配时动作。
func (s *Server) autoUpdateTick() {
	s.mu.RLock()
	auto := s.cfg.AutoUpdate
	hour := s.cfg.UpdateCheckHour
	s.mu.RUnlock()
	if !auto {
		return
	}
	if time.Now().Hour() != hour {
		return
	}
	if _, err := s.checkAndApplyUpdate(true); err != nil {
		log.Printf("自动更新失败: %v", err)
	}
}

// checkAndApplyUpdate 检查最新版本；apply=true 时在发现新版本后执行安装。
// 发现新版本时始终推送 "update" 通知（自动更新提示自动安装，手动检查提示手动升级）。
func (s *Server) checkAndApplyUpdate(apply bool) (UpdateInfo, error) {
	info := UpdateInfo{
		CurrentVersion: version.Version,
		CheckedAt:      time.Now().UTC().Format(time.RFC3339),
		Status:         "unknown",
	}
	tag, url, err := latestReleaseTag()
	if err != nil {
		info.Status = "unreachable"
		info.Message = err.Error()
		return info, err
	}
	info.LatestVersion = tag
	info.ReleaseURL = url
	info.Status = "ok"
	if !VersionGreater(tag, version.Version) {
		info.HasUpdate = false
		info.Message = "已是最新版本"
		return info, nil
	}
	info.HasUpdate = true
	info.Message = "发现新版本 v" + tag
	s.notifyAsync("update", "NanoPanel 发现新版本 v"+tag,
		fmt.Sprintf("当前版本 %s，最新版本 v%s。%s", version.Version, tag,
			map[bool]string{true: "将自动下载并安装，请留意后续升级结果通知。",
				false: "可前往「关于」页手动升级。"}[apply]))
	if !apply {
		return info, nil
	}
	if aerr := s.applyUpdate(tag); aerr != nil {
		s.notifyAsync("update", "NanoPanel 自动升级失败", aerr.Error())
		return info, aerr
	}
	s.notifyAsync("update", "NanoPanel 已升级到 v"+tag, "面板正在重启，请稍后刷新页面。")
	return info, nil
}

// applyUpdate 下载新版本、校验、备份当前二进制、替换并重启面板；失败回滚。
func (s *Server) applyUpdate(tag string) error {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()

	arch := updateArch()
	if arch == "" {
		return fmt.Errorf("当前架构 %s 暂不支持自动更新", runtime.GOARCH)
	}
	asset := "nanopanel-" + arch

	tmp, err := os.CreateTemp("", "nanopanel-update-")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpName)

	if err := s.downloadReleaseAsset(tag, asset, tmpName); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0755); err != nil {
		return fmt.Errorf("设置可执行权限失败: %w", err)
	}
	if !verifyBinary(tmpName) {
		return fmt.Errorf("下载的文件无法执行，已取消升级（可能为损坏或不兼容的版本）")
	}

	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前路径失败: %w", err)
	}
	if bin, err = filepath.EvalSymlinks(bin); err != nil {
		return fmt.Errorf("解析可执行路径失败: %w", err)
	}
	backup := bin + ".bak"
	if err := copyFile(bin, backup); err != nil {
		return fmt.Errorf("备份当前版本失败: %w", err)
	}
	if err := replaceBinary(tmpName, bin); err != nil {
		_ = copyFile(backup, bin) // 替换失败回滚到备份
		return fmt.Errorf("替换二进制失败，已回滚: %w", err)
	}
	// 让响应与通知先发出，再延迟重启面板服务。
	if rerr := s.restartSelf(); rerr != nil {
		return fmt.Errorf("升级文件已就位，但重启面板失败（请手动重启）: %w", rerr)
	}
	return nil
}

// verifyBinary 运行 <path> -version 校验其可执行且为本项目二进制（早期退出，不绑定端口）。
func verifyBinary(path string) bool {
	out, err := exec.Command(path, "-version").CombinedOutput()
	if err != nil {
		return false
	}
	return len(out) > 0
}

// copyFile 复制文件内容并保留权限位。
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	mode := os.FileMode(0755)
	if info, serr := os.Stat(src); serr == nil {
		mode = info.Mode().Perm()
	}
	return os.WriteFile(dst, data, mode)
}

// replaceBinary 用 src 替换 dst（优先 install，失败回退拷贝）。
func replaceBinary(src, dst string) error {
	if err := exec.Command("install", "-m", "0755", src, dst).Run(); err == nil {
		return nil
	}
	return copyFile(src, dst)
}

// GetUpdateSettings 返回自动更新相关设置。
func (s *Server) GetUpdateSettings(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ok(w, map[string]interface{}{
		"auto_update":       s.cfg.AutoUpdate,
		"update_check_hour": s.cfg.UpdateCheckHour,
		"current_version":   version.Version,
	})
}

type updateSettingsReq struct {
	AutoUpdate      *bool `json:"auto_update"`
	UpdateCheckHour *int  `json:"update_check_hour"`
}

// SaveUpdateSettings 保存自动更新设置并持久化。
func (s *Server) SaveUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req updateSettingsReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	if req.AutoUpdate != nil {
		s.cfg.AutoUpdate = *req.AutoUpdate
	}
	if req.UpdateCheckHour != nil {
		if *req.UpdateCheckHour < 0 || *req.UpdateCheckHour > 23 {
			s.mu.Unlock()
			fail(w, http.StatusBadRequest, "检查小时需在 0-23 之间")
			return
		}
		s.cfg.UpdateCheckHour = *req.UpdateCheckHour
	}
	if err := s.cfg.Save(s.configPath); err != nil {
		s.mu.Unlock()
		fail(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	s.mu.Unlock()
	s.audit(r, "settings", "保存自动更新设置",
		fmt.Sprintf("auto=%v hour=%d", s.cfg.AutoUpdate, s.cfg.UpdateCheckHour))
	ok(w, map[string]string{"msg": "已保存"})
}

// ApplyUpdate 手动立即升级：检查并安装最新版本（若有），返回状态。
func (s *Server) ApplyUpdate(w http.ResponseWriter, r *http.Request) {
	info, err := s.checkAndApplyUpdate(true)
	if err != nil {
		if info.Status == "unreachable" {
			ok(w, map[string]string{"msg": "无法连接更新源：" + info.Message, "status": "unreachable"})
			return
		}
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	if !info.HasUpdate {
		ok(w, map[string]string{"msg": "已是最新版本", "status": "uptodate"})
		return
	}
	ok(w, map[string]string{
		"msg":           "已下载新版本，面板将在数秒后自动重启。",
		"status":        "updating",
		"latest_version": info.LatestVersion,
	})
}
