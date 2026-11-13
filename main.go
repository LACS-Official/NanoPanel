package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"nanopanel/internal/api"
	"nanopanel/internal/auth"
	"nanopanel/internal/config"
	"nanopanel/internal/version"
)

//go:embed web
var webFS embed.FS

func main() {
	cfgPath := flag.String("c", "/etc/nanopanel/config.json", "配置文件路径")
	hashPw := flag.String("hashpw", "", "打印指定密码的哈希后退出")
	initCfg := flag.Bool("init", false, "生成配置文件与凭据后退出")
	pw := flag.String("password", "", "配合 -init 设置初始密码（留空则随机生成）")
	username := flag.String("username", config.DefaultUsername, "配合 -init 设置初始用户名")
	port := flag.String("port", "", "配合 -init 设置监听端口")
	resetPw := flag.String("reset-password", "", "重置现有配置的登录密码后退出")
	genPw := flag.Int("genpw", 0, "生成指定长度的随机密码后退出")
	genCert := flag.String("gen-cert", "", "生成自签 HTTPS 证书到指定目录（默认 /etc/nanopanel/tls）后退出")
	showVersion := flag.Bool("version", false, "打印版本信息后退出")
	dev := flag.Bool("dev", false, "开发模式：未指定 -c 时使用 ./dev/config.json")
	flag.Parse()

	if *showVersion {
		fmt.Println("NanoPanel", version.Info())
		return
	}
	if *hashPw != "" {
		fmt.Println(config.HashPassword(*hashPw))
		return
	}
	if *genPw > 0 {
		p, err := config.RandomPassword(*genPw)
		if err != nil {
			log.Fatalf("生成随机密码失败: %v", err)
		}
		fmt.Println(p)
		return
	}
	if flagWasSet("gen-cert") {
		dir := *genCert
		if dir == "" {
			dir = api.DefaultTLSDir
		}
		certPath := filepath.Join(dir, "nanopanel.crt")
		keyPath := filepath.Join(dir, "nanopanel.key")
		if err := api.GenerateSelfSignedCert(certPath, keyPath, api.LocalHostnames()); err != nil {
			log.Fatalf("生成证书失败: %v", err)
		}
		fmt.Printf("证书已生成:\n  %s\n  %s\n在配置文件中设置 tls_cert / tls_key 后以 HTTPS 启动。\n", certPath, keyPath)
		return
	}

	if *dev && !flagWasSet("c") {
		*cfgPath = filepath.Join("dev", "config.json")
	}

	if *resetPw != "" {
		resetPassword(*cfgPath, *resetPw)
		return
	}
	if *initCfg {
		initConfig(*cfgPath, *username, *pw, *port)
		return
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 配置缺失密码时，绝不回退到固定弱密码：生成一次性随机密码。
	var temporary string
	if cfg.PasswordHash == "" {
		temporary, err = config.RandomPassword(16)
		if err != nil {
			log.Fatalf("生成临时密码失败: %v", err)
		}
		cfg.PasswordHash = config.HashPassword(temporary)
	}

	am := auth.NewManager(time.Duration(cfg.TokenTTLHrs)*time.Hour, cfg.Secret)
	srv := api.NewServer(cfg, am, *cfgPath, version.Version)

	mux := http.NewServeMux()

	// 公开接口
	mux.HandleFunc("POST /api/v1/login", srv.Login)
	mux.HandleFunc("GET /api/v1/health", srv.Health)

	// 需要登录的接口
	mux.HandleFunc("POST /api/v1/logout", srv.AuthMiddleware(srv.Logout))
	mux.HandleFunc("GET /api/v1/overview", srv.AuthMiddleware(srv.Overview))
	mux.HandleFunc("POST /api/v1/change-password", srv.AuthMiddleware(srv.ChangePassword))
	mux.HandleFunc("GET /api/v1/services", srv.AuthMiddleware(srv.ListServices))
	mux.HandleFunc("POST /api/v1/service/action", srv.AuthMiddleware(srv.ServiceAction))
	mux.HandleFunc("GET /api/v1/processes", srv.AuthMiddleware(srv.ListProcesses))
	mux.HandleFunc("POST /api/v1/process/kill", srv.AuthMiddleware(srv.KillProcess))
	mux.HandleFunc("POST /api/v1/reboot", srv.AuthMiddleware(srv.Reboot))
	mux.HandleFunc("GET /api/v1/ping", srv.AuthMiddleware(srv.Ping))
	mux.HandleFunc("GET /api/v1/net/traffic", srv.AuthMiddleware(srv.TrafficStats))
	mux.HandleFunc("GET /api/v1/logs", srv.AuthMiddleware(srv.Logs))
	mux.HandleFunc("GET /api/v1/panel-logs", srv.AuthMiddleware(srv.PanelLogs))
	mux.HandleFunc("GET /api/v1/apps", srv.AuthMiddleware(srv.ListApps))
	mux.HandleFunc("POST /api/v1/app/action", srv.AuthMiddleware(srv.AppAction))
	mux.HandleFunc("POST /api/v1/app/action/stream", srv.AuthMiddleware(srv.AppActionStream))
	mux.HandleFunc("GET /api/v1/disks", srv.AuthMiddleware(srv.ListDisks))
	mux.HandleFunc("POST /api/v1/disks/mount", srv.AuthMiddleware(srv.DiskMount))
	mux.HandleFunc("POST /api/v1/disks/umount", srv.AuthMiddleware(srv.DiskUmount))
	mux.HandleFunc("GET /api/v1/apps/config", srv.AuthMiddleware(srv.GetAppConfig))
	mux.HandleFunc("POST /api/v1/apps/config", srv.AuthMiddleware(srv.SaveAppConfig))
	mux.HandleFunc("GET /api/v1/apps/health", srv.AuthMiddleware(srv.AppHealthStatus))
	mux.HandleFunc("POST /api/v1/apps/check", srv.AuthMiddleware(srv.CheckAppHealth))
	mux.HandleFunc("GET /api/v1/settings/monitor", srv.AuthMiddleware(srv.GetMonitorSettings))
	mux.HandleFunc("POST /api/v1/settings/monitor", srv.AuthMiddleware(srv.SaveMonitorSettings))
	mux.HandleFunc("GET /api/v1/metrics", srv.AuthMiddleware(srv.Metrics))
	mux.HandleFunc("GET /api/v1/settings/alerts", srv.AuthMiddleware(srv.GetAlertSettings))
	mux.HandleFunc("POST /api/v1/settings/alerts", srv.AuthMiddleware(srv.SaveAlertSettings))
	mux.HandleFunc("GET /api/v1/settings/mirror", srv.AuthMiddleware(srv.MirrorSettings))
	mux.HandleFunc("GET /api/v1/settings/webhook", srv.AuthMiddleware(srv.GetWebhook))
	mux.HandleFunc("POST /api/v1/settings/webhook", srv.AuthMiddleware(srv.SaveWebhook))
	mux.HandleFunc("POST /api/v1/settings/webhook/test", srv.AuthMiddleware(srv.TestWebhook))
	mux.HandleFunc("GET /api/v1/mihomo/status", srv.AuthMiddleware(srv.MihomoStatus))
	mux.HandleFunc("GET /api/v1/settings/tls", srv.AuthMiddleware(srv.TLSSettings))
	mux.HandleFunc("POST /api/v1/settings/tls", srv.AuthMiddleware(srv.SaveTLSSettings))

	// 文件管理器
	mux.HandleFunc("GET /api/v1/files", srv.AuthMiddleware(srv.ListFiles))
	mux.HandleFunc("GET /api/v1/files/download", srv.AuthMiddleware(srv.DownloadFile))
	mux.HandleFunc("GET /api/v1/files/archive", srv.AuthMiddleware(srv.DownloadArchive))
	mux.HandleFunc("GET /api/v1/files/read", srv.AuthMiddleware(srv.ReadTextFile))
	mux.HandleFunc("POST /api/v1/files/write", srv.AuthMiddleware(srv.WriteTextFile))
	mux.HandleFunc("POST /api/v1/files/upload", srv.AuthMiddleware(srv.UploadFiles))
	mux.HandleFunc("POST /api/v1/files/mkdir", srv.AuthMiddleware(srv.MkdirFile))
	mux.HandleFunc("POST /api/v1/files/delete", srv.AuthMiddleware(srv.DeleteFile))
	mux.HandleFunc("POST /api/v1/files/rename", srv.AuthMiddleware(srv.RenameFile))

	// 备份与恢复
	mux.HandleFunc("GET /api/v1/backup", srv.AuthMiddleware(srv.Backup))
	mux.HandleFunc("POST /api/v1/restore", srv.AuthMiddleware(srv.Restore))

	// 定时任务
	mux.HandleFunc("GET /api/v1/cron", srv.AuthMiddleware(srv.ListCron))
	mux.HandleFunc("POST /api/v1/cron", srv.AuthMiddleware(srv.SaveCron))
	mux.HandleFunc("POST /api/v1/cron/delete", srv.AuthMiddleware(srv.DeleteCron))
	mux.HandleFunc("POST /api/v1/cron/run", srv.AuthMiddleware(srv.RunCronNow))

	// 应用商店远程更新
	mux.HandleFunc("POST /api/v1/apps/update-manifest", srv.AuthMiddleware(srv.UpdateManifest))
	mux.HandleFunc("GET /api/v1/update/check", srv.AuthMiddleware(srv.CheckUpdate))
	mux.HandleFunc("GET /api/v1/update/settings", srv.AuthMiddleware(srv.GetUpdateSettings))
	mux.HandleFunc("POST /api/v1/update/settings", srv.AuthMiddleware(srv.SaveUpdateSettings))
	mux.HandleFunc("POST /api/v1/update/apply", srv.AuthMiddleware(srv.ApplyUpdate))

	// 内置终端：设置与票据走登录鉴权，WebSocket 用一次性票据自鉴权。
	mux.HandleFunc("GET /api/v1/terminal/settings", srv.AuthMiddleware(srv.TerminalSettings))
	mux.HandleFunc("POST /api/v1/terminal/settings", srv.AuthMiddleware(srv.SaveTerminalSettings))
	mux.HandleFunc("POST /api/v1/terminal/session", srv.AuthMiddleware(srv.CreateTerminalSession))
	mux.HandleFunc("GET /api/v1/terminal/ws", srv.TerminalWS)

	// 前端静态文件（SPA）
	staticSub, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}
	mux.HandleFunc("/", spaHandler(staticSub))

	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.SecurityHeaders(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// 应用商店脚本最长执行 5 分钟，写超时需覆盖该窗口。
		WriteTimeout:   6 * time.Minute,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// 历史指标采样随服务生命周期启停；独立于健康检测开关，保证趋势图始终有数据。
	srv.StartMetrics(ctx)
	// 应用健康检测随服务生命周期启停，掉线/恢复会推送 health 事件通知。
	srv.StartMonitor(ctx)
	// 定时任务调度器随服务生命周期启停。
	srv.StartCron(ctx)
	// 后台自动更新检查随服务生命周期启停（每日在配置小时检查、可自动安装）。
	srv.StartAutoUpdate(ctx)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		log.Println("收到退出信号，正在优雅关闭…")
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("NanoPanel %s 启动，监听 %s，用户 %s，配置 %s",
		version.Info(), listenDesc(cfg), cfg.Username, *cfgPath)
	if temporary != "" {
		log.Printf("警告: 配置中没有密码，本次临时密码为 %s（重启后失效，请用 npctl reset-password 固化）", temporary)
	}
	if config.IsLegacyHash(cfg.PasswordHash) {
		log.Printf("警告: 检测到旧版 sha256 密码哈希，建议执行 npctl reset-password 升级为 PBKDF2")
	}

	// 配置了证书则以 HTTPS 启动，否则保持 HTTP。
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		if err := httpServer.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("服务启动失败: %v", err)
		}
	} else if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("服务启动失败: %v", err)
	}
	log.Println("NanoPanel 已停止")
}

// listenDesc 描述监听地址（HTTPS 时标注协议）。
func listenDesc(cfg *config.Config) string {
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		return "https://" + cfg.Listen
	}
	return "http://" + cfg.Listen
}

func flagWasSet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func initConfig(path, username, password, port string) {
	c := config.Default()
	c.Username = username
	if port != "" {
		c.Listen = ":" + strings.TrimPrefix(port, ":")
	}
	if password == "" {
		generated, err := config.RandomPassword(16)
		if err != nil {
			log.Fatalf("生成随机密码失败: %v", err)
		}
		password = generated
	}
	c.PasswordHash = config.HashPassword(password)
	if err := c.Save(path); err != nil {
		log.Fatalf("生成配置失败: %v", err)
	}
	if err := config.SaveCredentials(path, c.Username, password); err != nil {
		log.Fatalf("写入凭据失败: %v", err)
	}
	fmt.Printf("配置已生成: %s\n监听端口: %s\n用户名: %s\n密码: %s\n", path, c.Listen, c.Username, password)
}

func resetPassword(path, password string) {
	if len(password) < config.MinPasswordLength {
		log.Fatalf("密码至少 %d 位", config.MinPasswordLength)
	}
	c, err := config.Load(path)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	c.PasswordHash = config.HashPassword(password)
	if err := c.Save(path); err != nil {
		log.Fatalf("保存配置失败: %v", err)
	}
	if err := config.SaveCredentials(path, c.Username, password); err != nil {
		log.Fatalf("写入凭据失败: %v", err)
	}
	fmt.Printf("密码已重置\n用户名: %s\n密码: %s\n配置文件: %s\n", c.Username, password, path)
}

// spaHandler 提供静态文件，找不到时回退到 index.html。
func spaHandler(staticFS fs.FS) http.HandlerFunc {
	fileServer := http.FileServer(http.FS(staticFS))
	return func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/")
		if rel == "" {
			rel = "index.html"
		}
		if _, err := fs.Stat(staticFS, rel); err != nil {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	}
}
