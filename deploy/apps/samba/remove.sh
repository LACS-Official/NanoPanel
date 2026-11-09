#!/usr/bin/env bash
# Samba 卸载脚本（保留共享目录里的用户数据）
set -uo pipefail
say() { printf "[samba] %s\n" "$*"; }

if command -v systemctl >/dev/null 2>&1; then
  systemctl disable --now smbd >/dev/null 2>&1 || true
fi
if command -v apt-get >/dev/null 2>&1; then
  export DEBIAN_FRONTEND=noninteractive
  # 只精确清理 Samba 自身；不跑 autoremove，避免带走其他应用依赖的包
  apt-get purge -y -qq samba samba-common-bin >/dev/null 2>&1 || true
fi
rm -rf /etc/samba
command -v systemctl >/dev/null 2>&1 && systemctl daemon-reload >/dev/null 2>&1 || true
say "Samba 已卸载（共享目录 /opt/samba/share 保留，如不需要请手动删除）"
