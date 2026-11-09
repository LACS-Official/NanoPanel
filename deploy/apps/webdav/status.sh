#!/usr/bin/env bash
# WebDAV 状态探测：只输出 installed / not_installed
set -euo pipefail
if [ -x /opt/webdav/webdav ] && [ -f /etc/webdav/config.yml ]; then
  echo installed
else
  echo not_installed
fi
