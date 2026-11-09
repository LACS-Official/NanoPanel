#!/usr/bin/env bash
# 示例应用安装脚本：退出码 0 表示成功，stdout 会回传到面板。
set -euo pipefail

MARK="/opt/nanopanel/apps/.hello_installed"
{
  echo "安装时间: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "下载源:   ${GITHUB_MIRROR:-未配置}"
  echo "软件源:   ${APT_MIRROR:-未配置}"
} > "$MARK"

echo "示例应用安装完成（已写入 $MARK）"
echo "提示: 应用脚本可直接读取安装阶段探测出的 GITHUB_MIRROR / APT_MIRROR。"
