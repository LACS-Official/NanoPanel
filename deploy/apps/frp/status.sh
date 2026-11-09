#!/usr/bin/env bash
# frp 状态探测：只输出 installed / not_installed
set -euo pipefail
if [ -x /opt/frp/frpc ] && [ -f /etc/frp/frpc.toml ]; then
  echo installed
else
  echo not_installed
fi
