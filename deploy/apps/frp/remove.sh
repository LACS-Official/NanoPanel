#!/usr/bin/env bash
# frp 卸载脚本（保留 /etc/frp 配置供重装复用）
set -uo pipefail
say() { printf "[frp] %s\n" "$*"; }

if command -v systemctl >/dev/null 2>&1; then
  systemctl disable --now frpc >/dev/null 2>&1 || true
fi
rm -f /etc/systemd/system/frpc.service
command -v systemctl >/dev/null 2>&1 && systemctl daemon-reload >/dev/null 2>&1 || true
rm -rf /opt/frp
say "frp 已卸载（配置保留在 /etc/frp，如需彻底清理请手动删除）"
