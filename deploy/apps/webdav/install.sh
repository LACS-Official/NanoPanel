#!/usr/bin/env bash
# WebDAV 文件服务安装脚本（hacdias/webdav 官方二进制）
set -euo pipefail

say() { printf "[webdav] %s\n" "$*"; }
die() { printf "[webdav] 错误: %s\n" "$*" >&2; exit 1; }
have_systemd() {
  command -v systemctl >/dev/null 2>&1 && \
    [ "$(ps -p 1 -o comm= 2>/dev/null | tr -d '[:space:]')" = "systemd" ]
}
arch_map() {
  case "$(uname -m)" in
    x86_64)         echo amd64 ;;
    aarch64|arm64)  echo arm64 ;;
    armv7l)         echo armv7 ;;
    armv6l)         echo armv6 ;;
    armv5tel|armv5tejl) echo armv5 ;;
    i[3-6]86|x86)   echo 386 ;;
    mips64)         echo mips64 ;;
    mips64le)       echo mips64le ;;
    mips|mipsocteon) echo mips ;;
    mipsle)         echo mipsle ;;
    *) return 1 ;;
  esac
}

GH="${GITHUB_MIRROR:-https://github.com}"
ARCH="$(arch_map)" || die "不支持的架构: $(uname -m)"
command -v curl >/dev/null 2>&1 || die "缺少 curl"
command -v tar >/dev/null 2>&1 || die "缺少 tar"

ASSET="linux-${ARCH}-webdav.tar.gz"
SUBPATH="/hacdias/webdav/releases/latest/download/${ASSET}"
say "下载 webdav ($ARCH)…"
downloaded=""
for base in "$GH" "https://github.com" \
            "https://ghfast.top/https://github.com" \
            "https://ghproxy.net/https://github.com"; do
  if curl -fsL --connect-timeout 8 --max-time 300 -o /tmp/webdav.tar.gz "${base}${SUBPATH}"; then
    downloaded="$base"
    break
  fi
  say "源不可用（${base#https://}），尝试下一个…"
done
[ -n "$downloaded" ] || die "所有下载源均失败，请检查网络或稍后重试"

rm -rf /tmp/webdav-extract && mkdir -p /tmp/webdav-extract
tar -xzf /tmp/webdav.tar.gz -C /tmp/webdav-extract
BIN="$(find /tmp/webdav-extract -type f -name webdav | head -1)"
[ -n "$BIN" ] || die "压缩包里未找到 webdav 可执行文件"

mkdir -p /opt/webdav/data /etc/webdav
install -m 0755 "$BIN" /opt/webdav/webdav
rm -rf /tmp/webdav-extract /tmp/webdav.tar.gz

# 默认配置：仅监听本机（未设置账号密码前不开放局域网，避免无鉴权暴露）
cat > /etc/webdav/config.yml <<'EOF'
address: 127.0.0.1
port: 6065
prefix: /
directory: /opt/webdav/data
permissions: CRUD
EOF
chmod 0640 /etc/webdav/config.yml

if have_systemd; then
  cat > /etc/systemd/system/webdav.service <<'EOF'
[Unit]
Description=WebDAV server (NanoPanel)
After=network.target

[Service]
Type=simple
ExecStart=/opt/webdav/webdav --config /etc/webdav/config.yml
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable --now webdav >/dev/null 2>&1 || true
  say "服务已启动: systemctl status webdav"
else
  say "未检测到 systemd，请手动运行: /opt/webdav/webdav --config /etc/webdav/config.yml"
fi

say "WebDAV 安装完成。请在面板「配置」里设置访问密码后再开放局域网使用。"
