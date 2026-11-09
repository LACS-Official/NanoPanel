#!/usr/bin/env bash
# WebDAV 卸载脚本（保留用户数据目录）
set -uo pipefail
say() { printf "[webdav] %s\n" "$*"; }

if command -v systemctl >/dev/null 2>&1; then
  systemctl disable --now webdav >/dev/null 2>&1 || true
fi
rm -f /etc/systemd/system/webdav.service
command -v systemctl >/dev/null 2>&1 && systemctl daemon-reload >/dev/null 2>&1 || true
rm -rf /opt/webdav /etc/webdav
say "WebDAV 已卸载（如需彻底清理数据请手动删除共享目录）"
