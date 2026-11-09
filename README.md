<p align="center">
  <img src="docs/banner.svg" alt="NanoPanel" width="420">
</p>

<h1 align="center">NanoPanel</h1>

<p align="center">
<img src="docs/logo.svg" width="20" alt="logo"> 官网 <a href="https://npanel.lacs.cc">npanel.lacs.cc</a> ·
仓库 <a href="https://github.com/LACS-Official/NanoPanel">GitHub</a> ·
由 <a href="https://lacs.cc">领创工作室（LAC Studio）</a>开源维护
</p>

<p align="center">
<a href="https://github.com/LACS-Official/NanoPanel/releases/latest"><img src="https://img.shields.io/github/v/release/LACS-Official/NanoPanel?color=0071e3" alt="Release"></a>
<a href="https://github.com/LACS-Official/NanoPanel/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/LACS-Official/NanoPanel/ci.yml?branch=main" alt="CI"></a>
<img src="https://img.shields.io/badge/Go-1.25%2B-00ADD8" alt="Go">
<img src="https://img.shields.io/badge/dependencies-0-brightgreen" alt="Dependencies">
<img src="https://img.shields.io/badge/license-MIT-green" alt="License">
<img src="https://img.shields.io/badge/platform-linux%20%7C%20wsl2-lightgrey" alt="Platform">
</p>

<p align="center">
  <code>curl -fsSL https://npanel.lacs.cc/install.sh | sudo bash</code>
</p>

面向小内存 Linux 设备（树莓派、N1 盒子、迷你主机、低内存 VPS、WSL2）的**轻量运维面板**。单个 Go 二进制、前端内嵌、零第三方依赖，一条命令完成安装，但不用 Docker —— 全部走 `apt` + `systemd` 原生方案。

> 项目以 **root** 运行并能管理系统服务、进程与应用脚本，**仅限可信内网使用**。详见 [SECURITY.md](SECURITY.md)。

## 特性

- **单文件交付**：Go 标准库编译为静态二进制，前端用 `go:embed` 内嵌，部署只发一个约 6 MB 的文件
- **一键安装**：自动探测最优镜像源、自动生成随机初始密码、安装后直接打印访问地址与账号
- **检查更新与自动升级**：面板内一键检查 GitHub 最新 Release，可自动下载安装，失败自动回滚；支持国内镜像加速
- **`npctl` 管理工具**：查看账号、重置密码、启停服务、查看镜像源、卸载
- **WSL / systemd 自适应**：WSL2 Ubuntu 可直接安装；无 systemd 时自动回退为进程守护
- 系统概览、进程管理（含内存占用）、日志中心、Ping、磁盘管理、文件管理、应用商店（含可升级检测与应用页面直达）、系统重启、修改密码
- **磁盘管理**：块设备与挂载点总览（容量 / 用量 / 文件系统），面板内一键挂载、卸载，可选写入 `/etc/fstab` 开机自动挂载
- **应用商店**：内置 CUPS、AdGuard Home、Tailscale、Mihomo、PicoClaw、WebDAV、Samba 七个真实应用，面板内可视化配置常用参数（端口、订阅链接、AuthKey、模型密钥等）
- **文件共享**：WebDAV 与 Samba 应用让 Windows 直接管理设备文件（映射网络驱动器 / `\\IP\共享名`）
- **实时安装输出**：应用安装/卸载过程在弹窗中实时滚动显示脚本输出，可随时关闭弹窗不中断任务
- **metacubexd 管理面板**：Mihomo 应用卡片一键打开 metacubexd（自动下载、自动配置鉴权），完整代理管理能力由 metacubexd 提供
- **持久化登录**：登录 token 经 HMAC 签名、密钥随配置落盘，面板重启不掉登录态；改密自动轮换密钥，吊销全部历史 token
- **Webhook 通知**：支持企业微信、钉钉、飞书、Server酱与通用 JSON 五种格式，按事件类别订阅推送（登录/服务/进程/应用/设置/系统/掉线/磁盘）
- **应用健康检测**：定时检查已安装应用的 systemd 单元，掉线或恢复时自动推送 Webhook 并写入面板日志
- **日志中心**：系统日志可按服务与已安装应用筛选，面板操作全程留痕可审计
- 用户名 + 密码登录、PBKDF2 加盐哈希、登录限速、输入白名单、安全响应头

## 快速开始

### 一键安装（推荐）

```bash
curl -fsSL https://npanel.lacs.cc/install.sh | sudo bash
```

脚本会：识别架构 → 自动测速挑选最快的 GitHub 镜像 → 下载最新 Release 完整包 → 校验 SHA256 → 安装 systemd 服务 → 生成随机密码 → 健康检查，最后打印访问地址与账号。

常见变体：

```bash
# 指定版本 / 端口
curl -fsSL https://npanel.lacs.cc/install.sh | sudo NP_VERSION=v0.1.0 NP_PORT=9090 bash

# 强制走某个镜像（跳过测速）
curl -fsSL https://npanel.lacs.cc/install.sh | sudo NP_MIRROR=https://ghfast.top/https://github.com bash

# 不走官网，直接从仓库取脚本（官网 /install.sh 与本仓库 deploy/install.sh 内容完全一致）
curl -fsSL https://raw.githubusercontent.com/LACS-Official/NanoPanel/main/deploy/install.sh | sudo bash
```

卸载同理：

```bash
sudo npctl uninstall          # 或 curl -fsSL https://npanel.lacs.cc/uninstall.sh | sudo bash
```

### 离线安装（源码 / 内网）

从 [Releases](../../releases) 下载 `nanopanel-vX.Y.Z-linux-<arch>.tar.gz`，解压后执行 `sudo ./install.sh`；
脚本检测到同目录的 `nanopanel-<arch>` 会直接离线安装，不再联网。

从源码构建：

```bash
make build-all          # 生成 deploy/nanopanel-{amd64,arm64,armv7} 及 .sha256
cd deploy
sudo ./install.sh
```

WSL2 中可直接用挂载目录安装：

```bash
cd /path/to/NanoPanel/deploy   # 例如 WSL 挂载路径 /mnt/e/Go/MicroPanel/deploy
sudo ./install.sh          # WSL 默认 root 时可直接 ./install.sh
```

安装脚本会依次完成：环境检查 → 镜像源测速 → 生成随机凭据 → 安装二进制 → 注册 systemd 服务 → 健康检查，最后输出：

```
============================================================
  NanoPanel 安装完成
------------------------------------------------------------
  访问地址 : http://172.22.219.0:8080
  本机访问 : http://127.0.0.1:8080
  Windows  : http://localhost:8080  (WSL2 端口转发)
  用户名   : admin
  密码     : KK5fuGszsWE5Mktb
  版本     : 0.1.0
  下载源   : github (0.361059s)
  软件源   : ustc (0.220176s)
------------------------------------------------------------
  官网     : https://npanel.lacs.cc
  文档     : https://npanel.lacs.cc/docs
  仓库     : https://github.com/LACS-Official/NanoPanel
============================================================
```

Windows 浏览器打开 **http://localhost:8080** 即可（WSL2 自带 localhost 转发）。

> **WSL2 必读：发行版空闲会自动停机。** WSL2 在没有活动会话时可能停止整个发行版，systemd 与面板服务会一起停掉，此时 Windows 侧访问 `localhost:8080` 会超时（WSL 内 `systemctl is-active nanopanel` 也会因为发行版关机而不可用）。这是 WSL 的机制，不是面板故障。三种处理方式：
>
> 1. 打开一个 WSL 终端窗口并保持：`wsl -d Ubuntu2404`，服务会由 systemd 自动拉起；
> 2. 需要开机即常驻：在 PowerShell 执行 `powershell -ExecutionPolicy Bypass -File .\wsl-keepalive.ps1`（注册登录时计划任务，可用 `-Remove` 移除）；
> 3. 在 `%USERPROFILE%\.wslconfig` 中设置 `[wsl2] vmIdleTimeout=-1`，影响本机所有 WSL 发行版。

### 在线安装说明

脚本默认从官方仓库 `LACS-Official/NanoPanel` 的 **最新 Release** 取包，无需任何环境变量；
`NP_REPO` 仅在你想指向自己的 fork 时才需要设置。下载会按测速结果依次尝试各镜像，首个失败自动回退到下一个。

### 安装参数

全部通过环境变量传入，无需交互式问答：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `NP_REPO` | `LACS-Official/NanoPanel` | GitHub `owner/repo`，指向自己的 fork 即可自建发布 |
| `NP_VERSION` | `latest` | `latest` 跟随最新 Release，也可写 `v0.1.0` |
| `NP_PORT` | `8080` | 面板监听端口 |
| `NP_USERNAME` | `admin` | 登录用户名 |
| `NP_PASSWORD` | 随机 16 位 | 登录密码；留空自动生成 |
| `NP_RANDOM_USER` | `0` | `1` 时生成随机用户名（`admin_xxxxxx`） |
| `NP_INSTALL_DIR` | `/opt/nanopanel` | 安装目录，systemd 单元按此路径生成 |
| `NP_CONF_DIR` | `/etc/nanopanel` | 配置目录 |
| `NP_ARCH` | 自动识别 | 强制 `amd64` / `arm64` / `armv7` |
| `NP_SOURCE` | `auto` | `auto` / `local`（仅本地）/ `online`（仅下载） |
| `NP_MIRROR` | 自动探测 | 手动指定 GitHub 下载源前缀 |
| `NP_SKIP_MIRROR_TEST` | `0` | `1` 时跳过测速，用首个候选 |
| `NP_PURGE` | 空 | 卸载时设为 `1`/`y` 可一并删除配置 |

示例：

```bash
sudo NP_PORT=9090 NP_RANDOM_USER=1 ./install.sh
curl -fsSL https://npanel.lacs.cc/install.sh | sudo NP_INSTALL_DIR=/srv/nanopanel bash
```

## 镜像源自动选择

安装阶段会并发探测下列节点，按实测延迟选出最快的，并写入 `/etc/nanopanel/mirror.conf`：

- **GitHub 下载源**：GitHub 直连、`ghfast.top`、`ghproxy.net`、`gh-proxy.com`
- **软件包源**：阿里云、华为云、中科大、清华

测速结果通过 systemd 的 `EnvironmentFile` 传给面板进程，应用商店脚本可直接读取：

```bash
echo "${GITHUB_MIRROR}"   # 例：https://github.com
echo "${APT_MIRROR}"      # 例：https://mirrors.ustc.edu.cn
```

`npctl mirror-info` 可随时查看当前选择；手工编辑 `mirror.conf` 后重启面板即可覆盖。若本机直连 GitHub 就很快，脚本会老老实实选直连，不会强行套代理。

## npctl 管理命令

```bash
npctl status                 # 运行状态与访问地址
npctl start|stop|restart     # 启停面板
npctl logs                   # 最近 100 行日志
npctl user-info              # 查看地址 / 用户名 / 密码
npctl reset-password         # 重置为随机密码（也可 npctl reset-password '自定义密码'）
npctl version                # 版本信息
npctl mirror-info            # 当前镜像源
npctl uninstall              # 卸载（默认保留 /etc/nanopanel）
```

## 配置

主配置 `/etc/nanopanel/config.json`（权限 0600）：

| 字段 | 说明 |
|---|---|
| `listen` | 监听地址，默认 `:8080` |
| `username` | 登录用户名 |
| `password_hash` | `pbkdf2-sha256$<轮数>$<盐>$<哈希>` |
| `token_ttl_hours` | token 有效期，默认 24 小时 |
| `services` | 允许操作的 systemd 服务白名单 |
| `apps_dir` / `manifest_path` | 应用商店目录与清单路径 |
| `webhook_url` / `webhook_format` / `webhook_events` | 通知地址、格式与订阅事件（含 `health` 掉线告警） |
| `monitor_interval_sec` | 应用健康检测间隔（秒），默认 60；`-1` 关闭自动检测 |

凭据文件 `/etc/nanopanel/credentials.json`（0600）保存明文密码，供 `npctl user-info` 使用；不需要可删除，不影响改密功能。

二进制辅助命令：

```bash
nanopanel -version
nanopanel -genpw 16
nanopanel -hashpw '某密码'
nanopanel -init -c /etc/nanopanel/config.json -username admin -password 'xxx' -port 8080
nanopanel -reset-password '新密码' -c /etc/nanopanel/config.json
```

## 开发

```bash
make check        # gofmt 检查 + go vet + go test
make dev          # 生成 dev/ 配置，在 :18080 启动开发版（admin / devonly）
make build        # 编译当前平台
make build-all    # 交叉编译三个 Linux 架构
```

需要 Go 1.25+，不需要 Node、Docker 或任何第三方 Go 模块。更多约定见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## API

前缀 `/api/v1`。除 `login`、`health` 外，请求头需带 `Authorization: Bearer <token>`。统一响应格式为 `{"ok":true,"data":...}` 或 `{"ok":false,"err":"..."}`。

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/login` | 登录，body 为 `{"username":"...","password":"..."}` |
| GET | `/health` | 公开健康检查，返回版本与平台 |
| POST | `/logout` | 退出登录 |
| GET | `/overview` | 系统概览 |
| POST | `/change-password` | 修改密码，返回新 token |
| GET | `/services` | 服务列表及状态 |
| POST | `/service/action?svc=&action=` | 启动 / 停止 / 重启 |
| GET | `/processes` | 进程列表（按内存排序，前 100） |
| POST | `/process/kill?pid=&force=` | 结束进程 |
| POST | `/reboot` | 重启系统 |
| GET | `/ping?host=` | 连通性测试 |
| GET | `/logs?unit=&n=` | journalctl 日志，可按服务/应用单元筛选 |
| GET | `/panel-logs` | 面板操作审计日志（最近 300 条，新在前） |
| GET | `/apps` | 应用列表（含安装与运行状态） |
| POST | `/app/action?id=&action=` | 安装 / 卸载应用（同步，结束后返回全部输出） |
| POST | `/app/action/stream?id=&action=` | 安装 / 卸载应用（SSE 流，实时回传脚本输出；断连不中断任务；同一应用并发返回 409） |
| GET | `/disks` | 块设备与挂载点列表（lsblk + /proc/mounts 合并） |
| POST | `/disks/mount` | 挂载块设备 `{"device":"/dev/sdb1","mountpoint":"/mnt/data","fstab":true}` |
| POST | `/disks/umount` | 卸载 `{"target":"/mnt/data"}`（目标可以是挂载点或设备路径） |
| GET | `/apps/config?id=` | 读取应用配置项与当前值 |
| POST | `/apps/config?id=` | 保存应用配置并调用 `config.sh` 应用 |
| GET | `/apps/health` | 所有应用的运行状态快照与监控配置 |
| POST | `/apps/check?force=1` | 立即体检；`force=1` 时对当前已掉线的应用直接告警 |
| GET | `/settings/monitor` | 读取健康检测设置与可选间隔 |
| POST | `/settings/monitor` | 保存检测间隔（`{"interval_sec":60}`，`-1` 关闭） |
| GET | `/settings/webhook` | 读取 Webhook 通知配置与可选项 |
| POST | `/settings/webhook` | 保存通知地址 / 格式 / 订阅事件 |
| POST | `/settings/webhook/test` | 立即发送一条测试通知 |
| GET | `/mihomo/status` | mihomo 安装状态、控制端口与内核版本 |
| GET | `/settings/mirror` | 应用商店下载源（安装时自动测速结果） |

## 应用商店约定

- 清单：`/opt/nanopanel/apps/manifest.json`
- 每个应用一个目录 `/opt/nanopanel/apps/<id>/`，含 `install.sh`、`remove.sh`、`status.sh`、可选 `config.sh`
- `status.sh` 只输出 `installed` 或 `not_installed`
- 应用 ID 仅允许字母、数字、下划线、短横，用于防止路径穿越
- 安装脚本退出码 0 表示成功，stdout 会回传到面板；执行超时为 5 分钟
- 脚本可读取 `GITHUB_MIRROR` / `APT_MIRROR` 使用安装阶段探测出的最优镜像源

### 内置应用

| 应用 | 安装方式 | 常用配置 |
|---|---|---|
| CUPS | apt | 管理端口、局域网访问开关 |
| AdGuard Home | GitHub 官方二进制 | 控制台端口、监听地址 |
| Tailscale | 官方一键脚本 | AuthKey 自动入网、出口节点、DNS 设置 |
| Mihomo (Clash.Meta) | GitHub 官方二进制 | 混合代理端口、局域网开关、控制端口与密钥、订阅链接 |
| PicoClaw | GitHub 官方预编译包 | Gateway 端口/地址、模型与 API Key、Telegram Bot、日志级别 |
| WebDAV | GitHub 官方二进制（多源回退） | 端口、共享目录、账号密码、只读 |
| Samba | apt | 共享名、共享目录、账号密码、访客模式、只读 |

### 磁盘管理

左侧「磁盘」页展示块设备（整盘/分区，含容量、文件系统、挂载点、用量）与非块设备挂载（WSL 的 Windows 盘、NFS 等）：

- 未挂载的可挂载文件系统一键挂载，可选写入 `/etc/fstab`（按 UUID，`nofail`）开机自动挂载
- 已挂载设备可从面板卸载；挂载点仅允许位于 `/mnt`、`/media`、`/opt`、`/srv`、`/home`、`/data` 之下，防止误挂系统目录
- 挂载/卸载会写入面板日志并按需推送 `disk` 事件 Webhook

### 文件共享（Windows 管理设备文件）

- **WebDAV**：安装后在配置里设置访问密码（留空仅本机监听，不暴露局域网），Windows 资源管理器「此电脑 → 映射网络驱动器」填 `http://<主机IP>:<端口>/`
- **Samba**：配置共享名、目录与账号密码（或访客模式），Windows 直接访问 `\\<主机IP>\<共享名>`；卸载保留共享目录中的数据

> WSL2 环境注意：WebDAV 走 HTTP 端口，Windows 用 `http://localhost:<端口>` 即可；SMB 的 445 端口被 Windows 自身占用，请使用 `\\<WSL IP>\共享名` 访问。

### 应用健康检测

面板启动后按 `monitor_interval_sec`（默认 60 秒）检查清单中已安装应用的 systemd 单元：

- 状态由「运行中」变为「已停止」→ 推送 `health` 事件通知「应用掉线」，恢复时推送「应用恢复」
- 首次检测只建立基线，不会因为启动瞬间的中间状态刷屏；未声明 `unit` 的应用不参与检测
- 告警同时写入面板日志（类别 `健康检测`），可在「设置 → 应用健康检测」调整间隔或关闭，也可点「立即检测并告警」手动体检
- 需要在 Webhook 设置里勾选「**应用掉线 / 恢复**」事件并填写地址，通知才会发出

### Mihomo 管理入口

Mihomo 卡片上的「管理面板」按钮直达 metacubexd（需先在应用配置里开启，见下节）；未开启时会提示先配置。面板不再内置代理控制台——完整能力（流量图表、规则、连接监控）由 metacubexd 原生提供，WebSocket 与内核同源直连，面板无需参与转发。

### metacubexd 完整管理面板（可选）

需要流量图表、规则、连接监控等完整能力时，可在 Mihomo 应用配置里把「metacubexd 管理面板」设为 `yes`：

- 保存配置时自动下载 [metacubexd](https://github.com/MetaCubeX/metacubexd) 官方静态资源到内核 ui 目录，之后局域网任意设备访问 `http://<设备IP>:9090/ui` 即可使用
- 控制接口将切换为 `0.0.0.0` 监听并强制鉴权：未设置「控制接口密钥」时自动生成一个随机密钥并回写应用配置，面板代理功能不受影响
- Mihomo 应用卡片的「管理面板」按钮直达；在 metacubexd 的设置里填入控制接口地址与密钥即可连接

### 应用配置约定

- `manifest.json` 中每个应用可通过 `config` 数组声明配置字段（名称、类型、默认值、候选值），面板据此自动渲染配置表单
- 字段类型支持 `text` / `password` / `select` / `textarea` / `number`，值经过白名单校验后保存到 `apps/<id>/config.env`（0600）
- 后端拒绝一切未在清单中声明的字段，防止通过配置注入环境变量
- 保存后自动以环境变量形式传给 `config.sh` 应用；安装/卸载脚本同样能读取已保存的配置值

## 目录结构

```
NanoPanel/
├── main.go                  # 入口、路由、embed 前端、优雅关闭
├── internal/
│   ├── version/             # 构建期注入的版本信息
│   ├── config/              # 配置、校验、密码哈希、凭据文件
│   ├── auth/                # token 管理与登录限速
│   ├── sysinfo/             # /proc 采集与跨平台磁盘信息
│   └── api/                 # 全部 HTTP 接口（含健康检测、通知、内核代理）
├── web/index.html           # 内嵌单页前端
├── deploy/
│   ├── install.sh           # 一键安装脚本
│   ├── uninstall.sh
│   ├── npctl                # 命令行管理工具
│   ├── nanopanel.service   # systemd 单元
│   ├── wsl-keepalive.ps1    # WSL2 常驻（可选）
│   └── apps/                # 应用商店清单与示例应用
└── .github/                 # CI、发布流程、Issue/PR 模板
```

## 安全

面板按设计具备 root 级操作能力，请勿暴露到公网。密码使用 PBKDF2-HMAC-SHA256（120000 轮 + 随机盐），登录失败 5 次锁定 1 分钟，所有外部命令带超时，配置文件权限 0600。完整威胁模型、已知限制与漏洞报告方式见 [SECURITY.md](SECURITY.md)。

## 贡献与许可

- 项目官网：<https://npanel.lacs.cc>
- 领创工作室官网：<https://lacs.cc>
- 本仓库：<https://github.com/LACS-Official/NanoPanel>
- 贡献指南：[CONTRIBUTING.md](CONTRIBUTING.md)
- 变更记录：[CHANGELOG.md](CHANGELOG.md)
- 许可证：[MIT](LICENSE)
