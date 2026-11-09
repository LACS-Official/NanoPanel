#!/usr/bin/env bash
# CUPS 安装脚本（Debian/Ubuntu）
set -euo pipefail

say() { printf "[cups] %s\n" "$*"; }
have_systemd() {
  command -v systemctl >/dev/null 2>&1 && \
    [ "$(ps -p 1 -o comm= 2>/dev/null | tr -d '[:space:]')" = "systemd" ]
}

command -v apt-get >/dev/null 2>&1 || { say "仅支持 Debian/Ubuntu（缺少 apt-get）"; exit 1; }

export DEBIAN_FRONTEND=noninteractive
say "安装 CUPS…"
apt-get update -qq
apt-get install -y -qq cups

if have_systemd; then
  systemctl enable --now cups >/dev/null 2>&1 || true
  say "服务已启动 (systemctl status cups)"
else
  say "未检测到 systemd，请手动启动 cups 服务"
fi
say "CUPS 安装完成，管理界面: http://<主机IP>:631"
