#!/usr/bin/env bash
# PicoClaw 安装脚本：下载官方预编译包并以 systemd 托管 Gateway
set -euo pipefail
say() { printf "[picoclaw] %s\n" "$*"; }
die() { printf "[picoclaw] 失败: %s\n" "$*" >&2; exit 1; }
warn() { printf "[picoclaw] 警告: %s\n" "$*" >&2; }
have_systemd() {
  command -v systemctl >/dev/null 2>&1 && \
    [ "$(ps -p 1 -o comm= 2>/dev/null | tr -d '[:space:]')" = "systemd" ]
}
arch_map() {
  case "$(uname -m)" in
    x86_64|amd64)      echo x86_64 ;;
    aarch64|arm64)     echo arm64 ;;
    armv7l)            echo armv7 ;;
    armv6l)            echo armv6 ;;
    riscv64)           echo riscv64 ;;
    loongarch64)       echo loong64 ;;
    mipsel)            echo mipsle ;;
    *) return 1 ;;
  esac
}

GH="${GITHUB_MIRROR:-https://github.com}"
ARCH="$(arch_map)" || die "不支持的架构: $(uname -m)"
command -v curl >/dev/null 2>&1 || die "缺少 curl"
command -v tar  >/dev/null 2>&1 || die "缺少 tar"

ASSET="picoclaw_Linux_${ARCH}.tar.gz"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
URL="${GH}/sipeed/picoclaw/releases/latest/download/${ASSET}"
say "下载 ${ASSET}…"
if ! curl -fL --connect-timeout 8 --max-time 300 -o "${TMP}/pkg.tar.gz" "$URL"; then
  say "镜像源下载失败，回退 GitHub 直连…"
  curl -fL --connect-timeout 8 --max-time 300 -o "${TMP}/pkg.tar.gz" \
    "https://github.com/sipeed/picoclaw/releases/latest/download/${ASSET}" \
    || die "下载失败，请检查网络或稍后重试"
fi
tar -xzf "${TMP}/pkg.tar.gz" -C "$TMP" || die "解压失败"

BIN="$(find "$TMP" -maxdepth 3 -type f -name 'picoclaw' | head -1)"
if [ -z "$BIN" ]; then
  BIN="$(find "$TMP" -maxdepth 3 -type f -name 'picoclaw*' | head -1)"
fi
[ -n "$BIN" ] || die "发布包中未找到 picoclaw 可执行文件"

mkdir -p /opt/picoclaw /opt/picoclaw/home /etc/picoclaw
install -m 0755 "$BIN" /opt/picoclaw/picoclaw

# 首次安装写一份最小配置；升级时保留用户已填的密钥
if [ ! -f /etc/picoclaw/config.json ]; then
  cat > /etc/picoclaw/config.json <<'EOF'
{
  "version": 2,
  "agents": { "defaults": { "restrict_to_workspace": true } },
  "model_list": [],
  "channels": {},
  "gateway": { "host": "127.0.0.1", "port": 18790, "log_level": "warn", "hot_reload": true }
}
EOF
  chmod 0600 /etc/picoclaw/config.json
fi

if have_systemd; then
  cat > /etc/systemd/system/picoclaw.service <<'EOF'
[Unit]
Description=PicoClaw Gateway
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=PICOCLAW_CONFIG=/etc/picoclaw/config.json
Environment=PICOCLAW_HOME=/opt/picoclaw/home
ExecStart=/opt/picoclaw/picoclaw gateway
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable picoclaw >/dev/null 2>&1 || true
  if ! systemctl restart picoclaw; then
    warn "服务未能立即启动，通常是因为尚未配置模型 API Key；可在面板「应用商店 → PicoClaw → 配置」里填写后重试"
  fi
  say "服务单元: picoclaw (systemctl status picoclaw)"
else
  say "未检测到 systemd，请手动运行: PICOCLAW_CONFIG=/etc/picoclaw/config.json /opt/picoclaw/picoclaw gateway"
fi
say "PicoClaw 安装完成；配置文件 /etc/picoclaw/config.json（0600）"
