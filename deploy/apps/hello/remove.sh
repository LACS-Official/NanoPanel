#!/usr/bin/env bash
# 示例应用卸载脚本
set -euo pipefail
rm -f /opt/nanopanel/apps/.hello_installed
echo "示例应用已卸载"
