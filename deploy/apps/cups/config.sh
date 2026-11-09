#!/usr/bin/env bash
# CUPS 配置脚本：环境变量 port / remote_admin
set -euo pipefail
say() { printf "[cups] %s\n" "$*"; }

port="${port:-631}"
remote_admin="${remote_admin:-no}"
conf=/etc/cups/cupsd.conf

[ -f "$conf" ] || { say "未找到 $conf，请确认 CUPS 已安装"; exit 1; }

# 先用 cupsctl 处理远程管理相关指令，再统一接管监听配置
if command -v cupsctl >/dev/null 2>&1; then
  if [ "$remote_admin" = "yes" ]; then
    cupsctl --remote-admin --remote-any >/dev/null 2>&1 || true
  else
    cupsctl --no-remote-admin --no-remote-any >/dev/null 2>&1 || true
  fi
fi

# 移除全部 Listen/Port 行，追加我们管理的监听
sed -i -E '/^[[:space:]]*(Listen|Port)[[:space:]]/Id' "$conf"
if [ "$remote_admin" = "yes" ]; then
  printf '\n# NanoPanel 管理的监听配置\nListen 0.0.0.0:%s\n' "$port" >> "$conf"
else
  printf '\n# NanoPanel 管理的监听配置\nListen localhost:%s\n' "$port" >> "$conf"
fi

if command -v systemctl >/dev/null 2>&1; then
  systemctl restart cups
fi
say "CUPS 配置已应用: 端口 $port，局域网管理 $remote_admin"
