#!/usr/bin/env bash
# Samba 状态探测：只输出 installed / not_installed
set -euo pipefail
if { command -v smbd >/dev/null 2>&1 || [ -x /usr/sbin/smbd ]; } && [ -f /etc/samba/smb.conf ]; then
  echo installed
else
  echo not_installed
fi
