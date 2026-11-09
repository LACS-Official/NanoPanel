# Changelog

本项目遵循 [Semantic Versioning](https://semver.org/)。

## [0.1.0] - 2026-11-09

首个公开发布版本。

### 新增

- **单文件交付**：Go 标准库编译为静态二进制，前端 `go:embed` 内嵌，零第三方依赖，部署只发一个约 6 MB 的文件
- **一键安装**：`curl -fsSL https://npanel.lacs.cc/install.sh | sudo bash` 即可完成安装——自动识别架构、测速挑选最快 GitHub 镜像、下载 Release 完整包（含 `apps/`、`npctl`、卸载脚本）、SHA256 校验、安装 systemd 服务、生成随机初始密码并打印访问地址与账号；systemd 单元按实际路径动态生成，支持自定义安装/配置目录（`NP_INSTALL_DIR` / `NP_CONF_DIR` / `NP_ARCH` / `NP_SOURCE` / `NP_MIRROR` / `NP_DRY_RUN` 等参数，`--help` 可查）
- **检查更新与自动升级**：`GET /api/v1/update/check` 解析 GitHub 最新 Release（不跟随 302，正确取 `Location`），关于页一键检查；后台更新检查器每日在配置小时（默认 04:00）检查，发现新版本推送 `update` 事件通知，开启「自动更新」后自动下载当前架构资产、备份旧二进制（`.bak`）并替换重启，失败自动回滚；`POST /api/v1/update/apply` 支持手动立即升级；更新源优先 `GITHUB_MIRROR`，回退 GitHub 官方与 ghfast 镜像
- **系统概览**：CPU 实时使用率、内存/磁盘用量进度条（阈值变色）、进程总数、热区温度与各核频率（超温告警）、网卡实时流量双折线图、运行概览卡片
- **进程管理**：按内存排序的进程列表，内存占用人类可读（KB/MB/GB），一键结束进程
- **应用商店**：二级页签「全部 / 已安装 / 可升级 / 设置」；内置 CUPS、AdGuard Home、Tailscale、Mihomo、PicoClaw、WebDAV、Samba、frp 八个真实应用，品牌色矢量图标，安装/卸载 SSE 流式实时输出，安装成功记录版本支持一键升级；应用卡片「打开」按钮直达应用页面；远程清单更新让商店脱离面板发版独立迭代；安装预检（架构 / 内存 / 磁盘余量）
- **metacubexd 管理面板**：Mihomo 应用配置开启后自动下载 metacubexd 官方静态资源，局域网访问 `http://<设备IP>:9090/ui` 使用完整代理管理（流量图表、规则、连接监控），WebSocket 与内核同源直连
- **磁盘管理**：块设备与挂载点总览，面板内一键挂载/卸载，可选写入 `/etc/fstab`（UUID + nofail）开机自动挂载，挂载点白名单防护
- **文件管理器**：白名单根目录（`file_roots` 可配置）内浏览、上传下载、在线编辑、重命名/删除；软链越界防护、根目录删除保护（规范化路径比较），全部变更审计 + Webhook
- **备份与恢复**：面板与应用配置一键导出 tar.gz，上传恢复（校验后自动重启生效）
- **定时任务**：every:Nm / hourly:MM / daily:HH:MM 三种周期任务，持久化保存、立即执行、执行结果推送通知
- **Webhook 通知**：企业微信 / 钉钉 / 飞书 / Server酱 / 通用 JSON 五种格式，按事件类别订阅（登录 / 服务 / 进程 / 应用 / 设置 / 系统 / 掉线 / 磁盘 / 温度 / 更新 / 安全等），失败指数退避重试
- **应用健康检测**：定时检查已安装应用的 systemd 单元，掉线/恢复自动推送 `health` 事件并写入面板日志，间隔可调可关
- **持久化登录**：登录 token 为无状态 HMAC 签名（密钥随配置落盘），面板重启不掉登录态；修改密码轮换密钥，历史 token 全部失效
- **品牌 Logo**：蓝色渐变圆形承载白色 N，绿色四分之一圆弧贴于圆外左下；登录页、侧边栏、关于页与 favicon 统一使用
- **登录页重新设计**：多层彩色光晕 + 网格遮罩背景，毛玻璃登录卡片，底部「版本号 · 作者 · 官网」信息栏
- **关于页**：独立主 Tab，含版本与更新（检查更新 / 自动更新设置 / 立即升级）、微信赞赏码与支付宝收款码赞助卡片、官方链接
- **项目官网**：`website/` 目录为 Astro + Tailwind 纯静态官网（首页 / 功能 / 下载 / 文档），构建时自动解析 GitHub 最新 Release，版本号与下载资产全部真实；接入 Umami 统计；托管于 Vercel（`npanel.lacs.cc`）
- **日志中心**：系统日志按服务与应用筛选，面板操作全程审计留痕
- `npctl` 命令行工具：状态 / 启停 / 日志 / 账号查看 / 密码重置 / `upgrade` 自升级 / 镜像信息 / 卸载
- WSL / systemd 自适应，无 systemd 回退进程守护；`deploy/wsl-keepalive.ps1` 解决 WSL2 空闲停机
- 深浅色主题三态切换（跟随系统 / 深色 / 浅色，持久化）
- 镜像源自动选择：安装时并发探测 GitHub 下载源与 apt 软件源，结果写入 `mirror.conf` 供面板与应用脚本复用
- GitHub Actions：CI（Ubuntu/Windows 矩阵 + golangci-lint + 前端语法检查 + 官网构建校验）与 tag 触发的多架构 Release（amd64 / arm64 / armv7，共 15 个资产含一键安装脚本）

### 修复

- **文件管理器路径判定**：根目录归属与删除保护均改为规范化路径比较（`Abs` + `EvalSymlinks`，Windows 统一小写），修复根目录写法为 8.3 短名（如 `RUNNER~1`）或大小写不一致时误判越界、根目录删除保护被绕过的问题
- **检查更新解析**：`releases/latest` 的 302 重定向不再被自动跟随，正确解析 `Location` 中的最新 tag，修复检查更新恒返回「无法解析最新版本」的问题
- **前端 JS 语法错误**：修复应用卡片「配置」按钮的引号断裂（曾导致整份 `app.js` 无法解析、面板交互与 Logo 注入失效）；CI 新增 `node --check` 防回归
- 前端 Logo 渐变定义移入常驻 SVG defs：登录容器隐藏时主界面 Logo 不再因 `url(#npg)` 引用失效而丢失填充
- samba/cups 安装脚本对 `apt-get update` 失败不再致命；samba 卸载不再执行 `apt-get autoremove`，避免误删其他应用依赖
- 磁盘列表过滤 lsblk 对 swap 的 `[SWAP]` 伪挂载点；还原 `/proc/self/mounts` 的八进制转义
- 放宽 Mihomo 转发路径白名单以兼容中文代理组与节点名，同时保留路径穿越防护

### 安全

- 密码使用 PBKDF2-HMAC-SHA256 加盐存储（120000 轮）；登录失败限速（5 次 / 10 分钟，锁定 1 分钟），触发锁定推送 `security` 告警
- 请求体大小限制、外部输入白名单校验、应用 ID 路径穿越防护
- HTTPS 一键开启（面板内生成 ECDSA 自签证书、10 年有效、SAN 覆盖主机名与全部网卡地址）
- fstab 写入保护（先备份、原子重写、`findmnt --verify` 校验失败自动回滚）
- HTTP 超时、优雅关闭、安全响应头、前端输出转义与事件委托
- 所有脚本执行带超时；凭据与配置文件权限 0600；配置缺失密码时生成一次性随机密码

### 变更

- 移除内置代理管理页面，代理能力由 metacubexd 完全接管；后端 `/api/v1/mihomo` 转发代理移除（缩小攻击面），仅保留 `/mihomo/status`
- 应用商店不再展示系统服务管理卡片；`/services` API 保留，日志中心只列运行中的系统服务
- 设置页改为「通知 / 安全 / 系统 / 关于」二级页签，渲染在主内容区顶部页面头中
- 前端拆分为 style.css / app.js（仍 `go:embed` 单二进制）
- Release 工作流发布稳定名安装包 `nanopanel-linux-<arch>.tar.gz` 与 `install.sh` / `uninstall.sh` / `npctl` 资产，发布说明含一键安装命令
