#!/usr/bin/env bash
# 示例应用状态脚本：约定输出 installed 或 not_installed
if [ -f /opt/micropanel/apps/.hello_installed ]; then
  echo "installed"
else
  echo "not_installed"
fi
