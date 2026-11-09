#!/usr/bin/env bash
# Mihomo 卸载脚本
set -euo pipefail
say() { printf "[mihomo] %s\n" "$*"; }

if command -v systemctl >/dev/null 2>&1; then
  systemctl disable --now mihomo >/dev/null 2>&1 || true
  rm -f /etc/systemd/system/mihomo.service
  systemctl daemon-reload >/dev/null 2>&1 || true
fi
rm -rf /opt/mihomo /etc/mihomo
say "mihomo 已卸载（含配置文件）"
