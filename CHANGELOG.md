# Changelog

本项目遵循 [Semantic Versioning](https://semver.org/)。

## [Unreleased]

### 新增

- **磁盘管理**：左侧「磁盘」页展示块设备与挂载点（容量/文件系统/用量），支持面板内挂载、卸载，可选写入 `/etc/fstab`（UUID + nofail）开机自动挂载；挂载点限制在 `/mnt` `/media` `/opt` `/srv` `/home` `/data` 白名单下；挂载/卸载写入面板日志并按需推送 `disk` 事件 Webhook
- **实时安装输出**：应用安装/卸载改为 SSE 流式端点 `POST /app/action/stream`，前端弹窗实时滚动脚本输出；关闭弹窗不中断任务（`context.WithoutCancel`）；同一应用并发操作返回 409
- **WebDAV 应用**：hacdias/webdav 官方二进制（多源回退下载），面板内配置端口、共享目录、账号密码、只读；未设置密码前仅监听 127.0.0.1，Windows「映射网络驱动器」即可管理文件
- **Samba 应用**：apt 安装，面板内配置共享名、目录、账号密码、访客模式、只读；`smb.conf` 受管段幂等生成并经 `testparm` 校验；卸载保留共享数据
- **应用掉线通知**：后台定时检测已安装应用的 systemd 单元，掉线/恢复时推送 `health` 事件 Webhook 并写入面板日志；检测间隔可在设置页调整或关闭，支持手动体检（`POST /apps/check`）
- **Mihomo 管理面板**：左侧「代理管理」页，支持内核版本/模式/流量、代理组节点切换与测速、活跃连接管理；后端提供路径白名单的安全转发代理，自动携带控制密钥，不泄漏面板 token
- **PicoClaw 应用**：从官方 release 下载预编译包并以 systemd 托管 Gateway，面板内配置模型/API Key、Gateway 端口与地址、Telegram Bot、日志级别；生成的 `config.json` 权限 0600
- 新增端点：`GET /apps/health`、`POST /apps/check`、`GET|POST /settings/monitor`、`GET /mihomo/status`、`/mihomo` 转发代理、`GET /disks`、`POST /disks/mount`、`POST /disks/umount`
- 应用列表与应用卡片显示运行状态（运行中 / 已掉线 / 未监控），Mihomo 卡片增加「管理」入口
- Mihomo 应用配置新增「控制接口密钥」；`config.sh` 会同步 `secret` 到内核配置
- 新增 9 个单元测试（磁盘列表/挂载校验/卸载校验、挂载点规范化、SSE 流成功/失败/参数拒绝/并发锁、disk 事件），共 60 项

### 修复

- samba/cups 安装脚本对 `apt-get update` 失败不再致命（部分镜像源临时 403 时仍可从缓存安装）
- samba 卸载不再执行 `apt-get autoremove`，避免误删其他应用依赖的软件包
- 磁盘列表过滤 lsblk 对 swap 的 `[SWAP]` 伪挂载点；还原 `/proc/self/mounts` 的 `\040` `\134` 等八进制转义
- 放宽 Mihomo 转发路径白名单以兼容中文代理组与节点名，同时保留路径穿越与任意路径访问防护

## [0.1.0] - 2026-11-09

首个可安装版本。

### 新增

- 单文件 Go 二进制 + `go:embed` 内嵌前端，零第三方依赖
- 系统概览、服务管理、进程管理、日志、Ping、应用商店、重启、改密
- 前端采用左侧导航栏布局，窄屏自动降级为顶部横滑条
- 一键式安装体验：自动探测最优镜像源、随机初始密码、安装后信息面板
- 应用商店接入真实应用：CUPS、AdGuard Home、Tailscale、Mihomo，面板内可视化配置常用参数
- Webhook 通知：企业微信 / 钉钉 / 飞书 / Server酱 / 通用 JSON 五种格式，按事件类别订阅
- 日志中心：系统日志按服务与已安装应用单元筛选；面板操作全程审计留痕（`/var/log/nanopanel/panel.log`）
- `npctl` 命令行工具：状态管理、账号查看、密码重置、镜像信息、卸载
- WSL / systemd 自动适配，无 systemd 时回退为进程守护
- `deploy/wsl-keepalive.ps1`：解决 WSL2 发行版空闲停机后面板不可达的可选常驻方案
- GitHub Actions CI（Linux + Windows）与 tag 触发的多架构发布流程

### 安全

- 登录改为「用户名 + 密码」，密码使用 PBKDF2-HMAC-SHA256 加盐存储（兼容旧 sha256 哈希）
- 登录失败限速（5 次 / 10 分钟，锁定 1 分钟）
- 请求体大小限制、外部输入白名单校验、应用 ID 路径穿越防护
- HTTP 超时、优雅关闭、安全响应头、前端输出转义与事件委托
- 所有脚本执行带超时；凭据与配置文件权限 0600
- 配置缺失密码时生成一次性随机密码，不再使用固定弱口令

### 变更

- 新增 `GET /api/v1/health` 公开健康检查端点
- 新增 `GET /api/v1/login` 返回 `username` 与 `ttl_hours`
- 修改密码后吊销全部登录态并返回新 token
- 安装脚本不再硬编码仓库地址，改用 `NP_REPO` 等环境变量
