#!/usr/bin/env bash
# CUPS 卸载脚本
set -euo pipefail
say() { printf "[cups] %s\n" "$*"; }

export DEBIAN_FRONTEND=noninteractive
if command -v systemctl >/dev/null 2>&1; then
  systemctl stop cups >/dev/null 2>&1 || true
  systemctl disable cups >/dev/null 2>&1 || true
fi
apt-get purge -y -qq cups >/dev/null 2>&1 || true
apt-get autoremove -y -qq >/dev/null 2>&1 || true
say "CUPS 已卸载"
