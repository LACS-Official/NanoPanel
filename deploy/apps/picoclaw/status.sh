#!/usr/bin/env bash
# 面板约定：只输出 installed / not_installed
if [ -x /opt/picoclaw/picoclaw ] && [ -f /etc/picoclaw/config.json ]; then
  echo installed
else
  echo not_installed
fi
