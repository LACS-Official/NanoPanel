# Changelog

本项目遵循 [Semantic Versioning](https://semver.org/)。

## [Unreleased]

### 新增

- 暂无

## [0.1.3] - 2026-11-09

一条命令装完面板：官网、仓库、GitHub Release 与面板自动更新现在共用同一份版本数据。

### 新增

- **一键安装真正可用**：`install.sh` 默认仓库改为 `LACS-Official/NanoPanel`、默认版本 `latest`，可直接 `curl -fsSL https://npanel.lacs.cc/install.sh | sudo bash`；在线安装改为下载 Release 完整包（含 `apps/`、`npctl`、卸载脚本），修复此前 `curl | bash` 场景因脚本目录没有配套文件而无法注册服务的问题
- **在线 SHA256 校验**：下载后自动取同源 `.sha256` 校验，失败即中止；离线安装同样校验
- **安装参数扩展**：新增 `NP_INSTALL_DIR` / `NP_CONF_DIR` / `NP_ARCH` / `-h|--help`；systemd 单元按实际路径动态生成，支持自定义目录
- **官网与 GitHub 联动**：官网构建时自动解析最新 Release（API → `releases/latest` 重定向 → 兜底配置，fetch 失败时回退系统 curl），版本号、下载文件名、资产大小全部来自真实 Release，不再手写
- **官网 Umami 统计**：接入 Umami（`websiteId 952a5878-d4bd-4e17-9aea-9edb286d2089`），仅生产构建注入
- **官网自动部署**：新增 `.github/workflows/pages.yml`，`website/` 变更或 Release 完成后自动构建并部署到 GitHub Pages（`npanel.lacs.cc`）
- 官网新增 `sitemap.xml`、`robots.txt`、`CNAME`；下载页增加复制按钮、国内镜像与指定版本用法、手动安装校验步骤

### 修复

- **官网下载链接 404**：下载页资产名写作 `nanopanel-linux-*`，与 Release 实际资产 `nanopanel-*` 不一致，现已对齐并改为 `latest/download` 稳定路径
- **官网版本号失真**：此前硬编码 `0.9.0`，现随 Release 自动更新

### 工程

- Release 工作流额外发布稳定名安装包 `nanopanel-linux-<arch>.tar.gz` 与 `install.sh` / `uninstall.sh` / `npctl` 资产，并在发布说明中写入一键安装命令
- 官网 `public/install.sh` 由 `scripts/sync-deploy.mjs` 在 `predev` / `prebuild` 时从 `deploy/` 同步，避免两份脚本漂移
- `.gitattributes` 补齐 `*.mjs` / `*.ts` / `*.astro` / `*.txt` 的 `eol=lf`

## [0.1.2] - 2026-11-09

### 修复

- **检查更新始终失败**：`releases/latest` 的 302 重定向此前被 HTTP 客户端自动跟随，取不到 `Location` 无法解析版本号，导致 `/update/check` 恒返回「无法解析最新版本」；现改为不跟随重定向，正确解析最新 tag（影响关于页检查更新与自动更新）

## [0.1.1] - 2026-11-09

### 修复

- **文件管理器路径判定**：根目录归属比较改为规范化后进行（`Abs` + `EvalSymlinks`，Windows 统一小写），修复根目录写法为 8.3 短名（如 `RUNNER~1`）或大小写不一致时被误判「路径超出允许范围」的问题
- **根目录删除保护**：删除前的根目录拦截由字符串相等改为规范化路径比较，修复写法不一致时未拦住、可能误删白名单根目录的问题

### 工程

- 修复 `gofmt` 未格式化的文件与 `golangci-lint` 配置缺失（新增 `.golangci.yml`，lint 结果 0 issues），CI 恢复全绿
- CI 测试失败时把失败测试名输出为 annotation 并写入 job summary，便于直接定位
- `.gitattributes` 补齐 `*.js` / `*.css` 的 `eol=lf`

## [0.1.0] - 2026-11-09

首个公开发布版本。

### 新增

- **进程内存占用可视化**：进程列表的内存列改为人类可读格式（KB/MB/GB），并保留精确 KB 数值
- **应用页面直达**：manifest 新增 `web_port` / `web_path` 声明（指向配置字段，自动解析用户保存的实际端口），已安装且带网页界面的应用卡片显示「打开」按钮，一键在新标签页跳转到 `http://<设备IP>:<端口>` 的应用页面（CUPS、AdGuard Home、PicoClaw 已声明；Mihomo 继续走「管理面板」按钮）
- **概览页丰富**：新增 CPU 实时使用率（两次 `/proc/stat` 采样差，250ms）与进程总数（`/proc` 数字目录计数）；资源占用卡片由内存/磁盘扩展为 CPU / 内存 / 磁盘三条带阈值变色的进度条（≥70% 橙、≥90% 红）；新增「运行概览」卡片展示进程数、已安装应用数、运行中服务数、最高温度
- **应用品牌图标**：应用商店卡片图标由首字母占位改为各应用品牌色 + 白色矢量图标（CUPS / AdGuard Home / Tailscale / Mihomo / PicoClaw / WebDAV / frp / Samba / 示例），未登记应用回退首字母方块
- **关于页独立为主 Tab**：从「设置 → 关于」子页签提升为左侧主导航主页面；新增项目简介、核心特性网格、技术栈、版本与更新（检查更新按钮）、捐赠支持卡片、官方链接与开源协议
- **检查更新接口**：新增 `GET /api/v1/update/check`，复用 npctl 的 GitHub 镜像回退逻辑解析最新 Release 并与当前版本比较；网络不可达时优雅返回（不报错），为自动更新做准备
- **自动更新（通知 + 自动安装）**：新增后台更新检查器，每日在配置小时（默认 04:00）检查 GitHub Release；发现新版本始终推送 `update` 事件 Webhook 通知，开启「自动更新」后自动下载当前架构资产、校验可执行性、备份旧二进制（`.bak`）并替换重启，失败时回滚；新增 `GET/POST /api/v1/update/settings` 与 `POST /api/v1/update/apply`（手动立即升级），关于页新增自动更新开关 / 检查时间设置与「立即升级」按钮；更新源优先走 `GITHUB_MIRROR` 环境变量，回退 GitHub 官方与 ghfast 镜像
- **Logo 改版**：采用参考图定稿设计——蓝色渐变圆形承载白色 N，绿色四分之一圆弧贴在圆外左下；favicon 同步更新
- **登录页重新设计**：全新背景（多层彩色光晕 + 网格遮罩 + 浮动动画），登录卡片改为毛玻璃质感；卡片底部新增「版本号 · 作者 · 官网」信息栏，官网可点击跳转
- **项目官网**：新增 `website/` 目录，基于 Astro + Tailwind CSS 的纯静态官网（首页 / 功能特性 / 下载 / 文档 / 404），含面板界面 SVG 示意图，`npm run build` 产物可直接静态托管

### 修复

- **前端 JS 语法错误**：修复 `web/app.js` 中应用卡片「配置」按钮的引号断裂（自「应用页面直达」提交起引入），该错误导致整份 `app.js` 无法解析、面板交互与 Logo 注入失效；CI 新增 `node --check web/app.js` 语法检查防止回归

### 变更

- **移除系统服务卡片**：应用商店「全部」页签不再展示系统服务管理卡片（白名单默认项大多未安装，实用性低）；`/services` 与 `/service/action` API 保留，日志中心单元下拉只列出当前运行中的系统服务
- **安全加固（P0）**：HTTPS 一键开启（面板内生成 ECDSA 自签证书、10 年有效、SAN 覆盖主机名与全部网卡地址，保存后自动重启生效，也可 `npctl`/`-gen-cert` 独立生成）；fstab 写入保护（先备份、原子重写、`findmnt --verify` 校验失败自动回滚）；panel.log 按 5MB 滚动轮转保留 3 份；连续登录失败触发锁定时推送 `security` 事件告警；Webhook 失败指数退避重试 3 次
- **温度与频率监控**：概览页展示热区温度（分色）与各核 CPU 频率；可配置超温阈值（40-100℃），超阈值推送 `temp` 事件、回落滞回带后通知恢复
- **网卡实时流量**：网络页每 2 秒采样，SVG 双折线展示最近 60 个采样点的上下行速率与累计流量
- **文件管理器**：白名单根目录（/mnt /media /opt /srv /home /data 等，`file_roots` 可配置）内的浏览/上传/下载/新建/重命名/删除/在线编辑小文本；软链越界防护、根目录删除保护、目录删除须显式递归；全部变更操作审计 + `file` 事件 Webhook
- **备份与恢复**：一键导出面板配置与应用配置为 tar.gz；上传恢复（应用配置立即生效，面板配置校验后还原并自动重启）
- **定时任务**：面板内管理 every:Nm / hourly:MM / daily:HH:MM 三种周期任务，持久化保存、立即执行、启停删除，执行结果推送 `task` 事件
- **远程清单更新**：从 GitHub 仓库拉取最新应用清单与脚本（保留运行时状态），商店脱离面板发版独立迭代
- **frp 内网穿透应用**：frpc 客户端，8 项常用配置生成 frpc.toml，版本动态解析 + 多镜像回退
- **安装预检**：manifest 支持 `arch` / `min_mem_mb` 约束，安装前校验架构、可用内存与磁盘余量
- **工程与体验（P3）**：前端拆分为 style.css / app.js（仍 go:embed 单二进制）；深浅色主题三态切换（跟随系统/深色/浅色，持久化）；`npctl upgrade` 自升级（Release 下载、版本校验、自动重启）；CI 新增 golangci-lint

### 变更

- **页面重构**：删除独立「服务」与「代理管理」页面；应用商店改为二级页签「全部 / 已安装 / 可升级 / 设置」——系统服务管理并入「全部」页签，代理管理功能由 metacubexd 面板完全接管（Mihomo 应用卡片新增「管理面板」入口按钮），后端 `/api/v1/mihomo` 转发代理与路径白名单一并移除（缩小攻击面），仅保留 `/mihomo/status`
- **应用可升级**：安装成功时记录脚本版本到 `apps/<id>/installed_version`，与清单版本不一致的应用出现在「可升级」页签，卡片显示新旧版本对比，「升级」按钮重新执行安装脚本（幂等、保留配置与数据），完成后记录自动更新
- **应用商店设置页签**：展示安装时自动测速选择的 GitHub 下载源 / apt 软件源 / 测速延迟（新端点 `GET /settings/mirror`，读取 systemd 注入的 mirror.conf 环境变量）；「应用健康检测」设置从「设置 → 通知」迁移至此

### 新增

- **持久化登录**：登录 token 改为无状态 HMAC 签名（`v1.nonce.expiry.hmac`，密钥为配置文件持久化的 `secret`），**面板重启后已登录的浏览器无需重新登录**；修改密码时轮换签名密钥并随配置落盘，历史 token 全部失效且无法复活；单端登出走内存撤销列表
- **metacubexd 管理面板**：Mihomo 应用配置新增「metacubexd 管理面板」开关，开启后自动下载 metacubexd（官方 release dist）到内核 ui 目录，控制接口切换为 `0.0.0.0` 监听并强制鉴权（未设密钥时自动生成并回写应用配置），局域网设备访问 `http://<设备IP>:9090/ui` 即可使用完整管理面板（流量图表等 WebSocket 能力由内核原生提供）；代理管理页新增「打开 metacubexd 面板」入口按钮
- mihomo 安装脚本在 GitHub API 匿名限流时自动改用 `releases/latest` 302 重定向解析版本号，不再因限流安装失败
- `appScriptEnv` 向应用脚本注入 `NP_APPS_DIR`，`config.sh` 可定位并回写自身 `config.env`
- **品牌 Logo**：全新 SVG Logo（蓝色渐变圆角方块 + N 字标 + 在线状态绿点），用于登录页、侧边栏、设置「关于」页与站点 favicon；`docs/logo.svg`、`docs/banner.svg` 随仓库分发
- **设置「关于」页**：展示面板版本、项目简介，以及领创工作室官网（lacs.cc）、项目官网（npanel.lacs.cc）、GitHub 仓库（LACS-Official/NanoPanel）直达链接
- **二级页签上移**：设置页改为「通知 / 安全 / 系统 / 关于」、日志中心改为「系统日志 / 面板日志」，统一渲染在主内容区顶部的页面头（标题 + 副标题 + 分段式页签）中，与左侧导航形成两级导航结构
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

- 前端 Logo 渐变定义移入常驻 SVG defs：登录容器隐藏时主界面 Logo 不再因 `url(#npg)` 引用失效而丢失填充
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
