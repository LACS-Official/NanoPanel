#!/usr/bin/env bash
# AdGuard Home 卸载脚本（连同配置一起删除）
set -euo pipefail
say() { printf "[adguardhome] %s\n" "$*"; }

if command -v systemctl >/dev/null 2>&1; then
  systemctl disable --now AdGuardHome >/dev/null 2>&1 || true
  rm -f /etc/systemd/system/AdGuardHome.service
  systemctl daemon-reload >/dev/null 2>&1 || true
fi
rm -rf /opt/AdGuardHome
say "AdGuard Home 已卸载（含配置数据）"
