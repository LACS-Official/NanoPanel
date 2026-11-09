#!/usr/bin/env bash
# Tailscale 配置脚本：环境变量 auth_key / exit_node / accept_dns
set -euo pipefail
say() { printf "[tailscale] %s\n" "$*"; }
die() { printf "[tailscale] 失败: %s\n" "$*" >&2; exit 1; }

command -v tailscale >/dev/null 2>&1 || die "tailscale 尚未安装"

args=""
[ "${accept_dns:-yes}" = "no" ] && args="$args --accept-dns=false"
[ "${exit_node:-no}" = "yes" ] && args="$args --advertise-exit-node"

if [ -n "${auth_key:-}" ]; then
  say "使用 AuthKey 接入网络…"
  # shellcheck disable=SC2086
  if timeout 60 tailscale up --authkey="$auth_key" $args; then
    say "已接入 Tailscale 网络"
    tailscale ip -4 2>/dev/null | head -1 || true
  else
    die "tailscale up 未成功，请检查 AuthKey 是否有效或已过期"
  fi
else
  say "未提供 AuthKey，输出登录链接（15 秒内复制到浏览器打开）："
  # shellcheck disable=SC2086
  timeout 15 tailscale up $args 2>&1 || true
fi
