#!/usr/bin/env bash
# Tailscale 安装脚本：官方一键安装
set -euo pipefail
say() { printf "[tailscale] %s\n" "$*"; }
die() { printf "[tailscale] 失败: %s\n" "$*" >&2; exit 1; }
have_systemd() {
  command -v systemctl >/dev/null 2>&1 && \
    [ "$(ps -p 1 -o comm= 2>/dev/null | tr -d '[:space:]')" = "systemd" ]
}

if command -v tailscale >/dev/null 2>&1; then
  say "tailscale 已存在，跳过安装"
  exit 0
fi

command -v curl >/dev/null 2>&1 || die "缺少 curl"
say "执行官方安装脚本…"
curl -fsSL https://tailscale.com/install.sh | sh || die "安装脚本执行失败"

if have_systemd; then
  systemctl enable --now tailscaled
  say "tailscaled 服务已启动"
else
  say "未检测到 systemd，请手动启动 tailscaled"
fi
say "Tailscale 安装完成；在「应用配置」中填入 AuthKey 即可自动接入网络"
