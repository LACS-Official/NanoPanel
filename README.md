# NanoPanel

![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8)
![Dependencies](https://img.shields.io/badge/dependencies-0-brightgreen)
![License](https://img.shields.io/badge/license-MIT-green)
![Platform](https://img.shields.io/badge/platform-linux%20%7C%20wsl2-lightgrey)

面向 OpenStick（UFI003 / MSM8916 / 512MB）这类小内存设备的**轻量运维面板**。单个 Go 二进制、前端内嵌、零第三方依赖，一条命令完成安装，但不用 Docker —— 全部走 `apt` + `systemd` 原生方案。

> 项目以 **root** 运行并能管理系统服务、进程与应用脚本，**仅限可信内网使用**。详见 [SECURITY.md](SECURITY.md)。

## 特性

- **单文件交付**：Go 标准库编译为静态二进制，前端用 `go:embed` 内嵌，部署只发一个约 6 MB 的文件
- **一键安装**：自动探测最优镜像源、自动生成随机初始密码、安装后直接打印访问地址与账号
- **`npctl` 管理工具**：查看账号、重置密码、启停服务、查看镜像源、卸载
- **WSL / systemd 自适应**：WSL2 Ubuntu 可直接安装；无 systemd 时自动回退为进程守护
- 系统概览、服务启停、进程管理、日志中心、Ping、应用商店、系统重启、修改密码
- **应用商店**：内置 CUPS、AdGuard Home、Tailscale、Mihomo、PicoClaw 五个真实应用，面板内可视化配置常用参数（端口、订阅链接、AuthKey、模型密钥等）
- **Mihomo 管理面板**：面板内切换代理节点、测速、切换运行模式、查看与断开活跃连接，无需单独部署前端
- **Webhook 通知**：支持企业微信、钉钉、飞书、Server酱与通用 JSON 五种格式，按事件类别订阅推送
- **应用健康检测**：定时检查已安装应用的 systemd 单元，掉线或恢复时自动推送 Webhook 并写入面板日志
- **日志中心**：系统日志可按服务与已安装应用筛选，面板操作全程留痕可审计
- 用户名 + 密码登录、PBKDF2 加盐哈希、登录限速、输入白名单、安全响应头

## 快速开始

### 离线安装（推荐，也适用于 WSL2）

从 [Releases](../../releases) 下载对应架构的压缩包，或从源码构建：

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
============================================================
```

Windows 浏览器打开 **http://localhost:8080** 即可（WSL2 自带 localhost 转发）。

> **WSL2 必读：发行版空闲会自动停机。** WSL2 在没有活动会话时可能停止整个发行版，systemd 与面板服务会一起停掉，此时 Windows 侧访问 `localhost:8080` 会超时（WSL 内 `systemctl is-active nanopanel` 也会因为发行版关机而不可用）。这是 WSL 的机制，不是面板故障。三种处理方式：
>
> 1. 打开一个 WSL 终端窗口并保持：`wsl -d Ubuntu2404`，服务会由 systemd 自动拉起；
> 2. 需要开机即常驻：在 PowerShell 执行 `powershell -ExecutionPolicy Bypass -File .\wsl-keepalive.ps1`（注册登录时计划任务，可用 `-Remove` 移除）；
> 3. 在 `%USERPROFILE%\.wslconfig` 中设置 `[wsl2] vmIdleTimeout=-1`，影响本机所有 WSL 发行版。

### 在线安装

发布前把仓库地址通过环境变量传入（脚本不硬编码任何仓库）：

```bash
export NP_REPO="yourname/NanoPanel"
export NP_VERSION="v0.1.0"
curl -fsSL https://raw.githubusercontent.com/${NP_REPO}/main/deploy/install.sh | sudo -E bash
```

在线安装会按测速结果依次尝试各镜像，首个失败自动回退到下一个。

### 安装参数

全部通过环境变量传入，无需交互式问答：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `NP_REPO` | 空 | GitHub `owner/repo`；留空则只允许同目录离线安装 |
| `NP_VERSION` | `v0.1.0` | 在线安装的 release 标签 |
| `NP_PORT` | `8080` | 面板监听端口 |
| `NP_USERNAME` | `admin` | 登录用户名 |
| `NP_PASSWORD` | 随机 16 位 | 登录密码；留空自动生成 |
| `NP_RANDOM_USER` | `0` | `1` 时生成随机用户名（`admin_xxxxxx`） |
| `NP_SOURCE` | `auto` | `auto` / `local`（仅本地）/ `online`（仅下载） |
| `NP_MIRROR` | 自动探测 | 手动指定 GitHub 下载源前缀 |
| `NP_SKIP_MIRROR_TEST` | `0` | `1` 时跳过测速，用首个候选 |
| `NP_PURGE` | 空 | 卸载时设为 `1`/`y` 可一并删除配置 |

示例：

```bash
sudo NP_PORT=9090 NP_RANDOM_USER=1 ./install.sh
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
| POST | `/app/action?id=&action=` | 安装 / 卸载应用 |
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
| ALL | `/mihomo?path=/proxies` | 转发到 mihomo 控制接口（路径白名单，GET/PUT/PATCH/DELETE） |

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

### 应用健康检测

面板启动后按 `monitor_interval_sec`（默认 60 秒）检查清单中已安装应用的 systemd 单元：

- 状态由「运行中」变为「已停止」→ 推送 `health` 事件通知「应用掉线」，恢复时推送「应用恢复」
- 首次检测只建立基线，不会因为启动瞬间的中间状态刷屏；未声明 `unit` 的应用不参与检测
- 告警同时写入面板日志（类别 `健康检测`），可在「设置 → 应用健康检测」调整间隔或关闭，也可点「立即检测并告警」手动体检
- 需要在 Webhook 设置里勾选「**应用掉线 / 恢复**」事件并填写地址，通知才会发出

### Mihomo 管理面板

左侧「代理管理」或应用卡片上的「管理」按钮进入。浏览器不直接连接内核，所有请求经面板转发到 `127.0.0.1:<控制端口>`：

- 转发路径限定在 `/version`、`/configs`、`/proxies`、`/connections`、`/rules`、`/providers/proxies` 白名单内，仅允许 GET/PUT/PATCH/DELETE，面板的登录 token 不会泄漏给内核
- 支持中文代理组与节点名；如果配置了「控制接口密钥」，面板会自动携带 `Bearer` 头
- 提供内核版本、运行模式、流量统计、代理组节点切换与测速、活跃连接查看/断开/清空，可开启 10 秒自动刷新

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

- 贡献指南：[CONTRIBUTING.md](CONTRIBUTING.md)
- 变更记录：[CHANGELOG.md](CHANGELOG.md)
- 许可证：[MIT](LICENSE)
