# 贡献指南

感谢你愿意为 NanoPanel 做点什么。项目保持「零第三方 Go 依赖」这条底线，提交前请确认没有引入新的 `require`。

## 开发准备

- Go 1.25 或更高版本
- 一个 Linux 环境（WSL2 也可以，需启用 systemd 以获得完整功能）
- 无需 Node、无需数据库、无需 Docker

```bash
git clone <你的仓库地址>
cd NanoPanel
make check          # gofmt 检查 + go vet + go test
make dev            # 生成 dev/ 配置并在 :18080 启动开发版
```

`make dev` 会生成 `dev/config.json`（已被 `.gitignore` 排除），默认用户名 `admin`，密码打印在终端。

## 代码约定

1. 只使用 Go 标准库。
2. 提交前执行 `gofmt -w .` 与 `make check`。
3. 新增 HTTP 接口必须：在 `main.go` 注册路由、明确是否需要鉴权、更新 README 的 API 表。
4. 所有外部命令调用必须带 `context` 超时。
5. 所有拼接进 HTML 的数据必须经过 `esc()`；优先用事件委托而不是内联 `onclick` 字符串拼接。
6. 用户输入（服务名、应用 ID、单元名、主机名）必须有白名单或正则校验。

## 测试

```bash
go test ./...
go test -race ./...     # Linux / macOS
```

新增纯函数请补单元测试；涉及系统调用的函数请拆出可测的解析层。

## 提交与 PR

- 提交信息使用简体中文或英文均可，建议格式：`fix: 修复 xxx` / `feat: 新增 xxx`
- 一个 PR 只做一件事，附上验证方式（命令 + 结果）
- 涉及安装脚本改动时，请至少在一个干净的 Linux/WSL 环境里实际跑一次 `install.sh`

## 应用商店脚本约定

参见 README「应用商店」章节。新增应用请确保 `status.sh` 只输出 `installed` 或 `not_installed`，且脚本在超时被杀掉时不会留下破坏性半成品。

## 报告问题

请使用仓库的 Issue 模板。安全问题请走 [SECURITY.md](SECURITY.md) 中说明的私下报告渠道，不要开公开 Issue。
