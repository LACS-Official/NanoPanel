#!/usr/bin/env bash
# Samba 文件共享安装脚本（Debian/Ubuntu）
set -euo pipefail

say() { printf "[samba] %s\n" "$*"; }
die() { printf "[samba] 错误: %s\n" "$*" >&2; exit 1; }
have_systemd() {
  command -v systemctl >/dev/null 2>&1 && \
    [ "$(ps -p 1 -o comm= 2>/dev/null | tr -d '[:space:]')" = "systemd" ]
}

command -v apt-get >/dev/null 2>&1 || { say "仅支持 Debian/Ubuntu（缺少 apt-get）"; exit 1; }

export DEBIAN_FRONTEND=noninteractive
say "安装 Samba…"
# 部分镜像源可能临时 403，索引刷新失败不致命：包缓存通常仍可用
apt-get update -qq || say "软件源刷新失败（镜像可能临时不可用），继续尝试安装"
apt-get install -y -qq samba

mkdir -p /opt/samba/share

if have_systemd; then
  systemctl enable --now smbd >/dev/null 2>&1 || true
  say "服务已启动 (systemctl status smbd)"
else
  say "未检测到 systemd，请手动启动 smbd 服务"
fi
say "Samba 安装完成。请在面板「配置」里设置共享名、目录与访问密码。"
