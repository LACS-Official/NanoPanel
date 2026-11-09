#!/usr/bin/env bash
# CUPS 状态脚本
command -v cupsd >/dev/null 2>&1 && echo installed || echo not_installed
