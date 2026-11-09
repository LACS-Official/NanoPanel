#!/usr/bin/env bash
set -euo pipefail
say() { printf "[picoclaw] %s\n" "$*"; }
if command -v systemctl >/dev/null 2>&1; then
  systemctl disable --now picoclaw >/dev/null 2>&1 || true
  rm -f /etc/systemd/system/picoclaw.service
  systemctl daemon-reload >/dev/null 2>&1 || true
fi
rm -rf /opt/picoclaw /etc/picoclaw
say "PicoClaw 已卸载"
