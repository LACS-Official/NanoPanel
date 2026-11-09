#!/usr/bin/env bash
# Mihomo (Clash.Meta) 安装脚本：从 GitHub 下载官方二进制
set -euo pipefail
say() { printf "[mihomo] %s\n" "$*"; }
die() { printf "[mihomo] 失败: %s\n" "$*" >&2; exit 1; }
have_systemd() {
  command -v systemctl >/dev/null 2>&1 && \
    [ "$(ps -p 1 -o comm= 2>/dev/null | tr -d '[:space:]')" = "systemd" ]
}
arch_map() {
  case "$(uname -m)" in
    x86_64|amd64)  echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    armv7l|armv6l) echo armv7 ;;
    *) return 1 ;;
  esac
}

GH="${GITHUB_MIRROR:-https://github.com}"
ARCH="$(arch_map)" || die "不支持的架构: $(uname -m)"
command -v curl >/dev/null 2>&1 || die "缺少 curl"
command -v gunzip >/dev/null 2>&1 || die "缺少 gzip"

# mihomo 的发布资产名带版本号，须动态解析。优先走 GitHub API；
# API 匿名限流（或不可达）时，改用 releases/latest 的 302 重定向提取 tag，
# 资产名规则为 mihomo-linux-<arch>[-compatible]-<tag>.gz，可直接构造。
resolve_mihomo_url() {
  local pattern asset
  if [ "$ARCH" = "amd64" ]; then
    pattern="mihomo-linux-amd64-compatible-v[0-9.]+\\.gz$"   # compatible 兼容老 CPU
  else
    pattern="mihomo-linux-${ARCH}-v[0-9.]+\\.gz$"
  fi
  asset=$(curl -fsSL --connect-timeout 8 --max-time 20 \
    "https://api.github.com/repos/MetaCubeX/mihomo/releases/latest" 2>/dev/null \
    | grep -oE '"browser_download_url": *"[^"]+"' \
    | cut -d'"' -f4 | grep -E "$pattern" | grep -v -- "-go1" | head -1)
  if [ -n "$asset" ]; then
    echo "$asset"
    return 0
  fi
  local tag
  tag=$(curl -fsS --connect-timeout 8 --max-time 20 -o /dev/null -w '%{redirect_url}' \
    "$GH/MetaCubeX/mihomo/releases/latest" 2>/dev/null \
    | grep -oE 'tag/v[0-9.]+$' | cut -d/ -f2)
  if [ -n "$tag" ]; then
    local compat=""
    [ "$ARCH" = "amd64" ] && compat="-compatible"   # 老版本资产无 compatible 后缀，失败时再退
    echo "https://github.com/MetaCubeX/mihomo/releases/download/${tag}/mihomo-linux-${ARCH}${compat}-${tag}.gz"
    return 0
  fi
  return 1
}

DL="$(resolve_mihomo_url)"
[ -n "$DL" ] || die "无法解析 mihomo 下载地址（GitHub API 不可达，请检查网络）"
say "下载 ${DL##*/}…"
url="${GH}${DL#https://github.com}"
if ! curl -fL --connect-timeout 8 --max-time 300 -o /tmp/mihomo.gz "$url"; then
  say "镜像源下载失败，回退 GitHub 直连…"
  curl -fL --connect-timeout 8 --max-time 300 -o /tmp/mihomo.gz "$DL" \
    || die "下载失败，请检查网络或稍后重试"
fi
gunzip -f /tmp/mihomo.gz

mkdir -p /opt/mihomo /etc/mihomo
install -m 0755 /tmp/mihomo /opt/mihomo/mihomo
rm -f /tmp/mihomo

conf=/etc/mihomo/config.yaml
if [ ! -f "$conf" ]; then
  cat > "$conf" <<'EOF'
mixed-port: 7890
allow-lan: false
external-controller: 127.0.0.1:9090
log-level: info
EOF
fi

if have_systemd; then
  cat > /etc/systemd/system/mihomo.service <<'EOF'
[Unit]
Description=Mihomo (Clash.Meta)
After=network.target

[Service]
ExecStart=/opt/mihomo/mihomo -d /etc/mihomo
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable --now mihomo
  say "服务已启动 (systemctl status mihomo)"
else
  say "未检测到 systemd，请手动运行: /opt/mihomo/mihomo -d /etc/mihomo"
fi
say "mihomo 安装完成；代理端口 7890，配置文件 /etc/mihomo/config.yaml"
