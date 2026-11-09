#!/usr/bin/env bash
# Tailscale 卸载脚本
set -euo pipefail
say() { printf "[tailscale] %s\n" "$*"; }

if command -v systemctl >/dev/null 2>&1; then
  systemctl disable --now tailscaled >/dev/null 2>&1 || true
fi
export DEBIAN_FRONTEND=noninteractive
apt-get purge -y -qq tailscale >/dev/null 2>&1 || true
rm -f /etc/apt/sources.list.d/tailscale.list /usr/share/keyrings/tailscale-archive-keyring.gpg
apt-get update -qq >/dev/null 2>&1 || true
say "Tailscale 已卸载"
