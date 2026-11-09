#!/usr/bin/env bash
# AdGuard Home 安装脚本：从 GitHub 下载官方二进制
set -euo pipefail
say() { printf "[adguardhome] %s\n" "$*"; }
die() { printf "[adguardhome] 失败: %s\n" "$*" >&2; exit 1; }
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
command -v tar >/dev/null 2>&1 || die "缺少 tar"

ASSET="AdGuardHome_linux_${ARCH}.tar.gz"
say "下载 AdGuardHome ($ARCH)…"
if ! curl -fL --connect-timeout 8 --max-time 300 -o /tmp/agh.tar.gz \
     "${GH}/AdguardTeam/AdGuardHome/releases/latest/download/${ASSET}"; then
  say "镜像源下载失败，回退 GitHub 直连…"
  curl -fL --connect-timeout 8 --max-time 300 -o /tmp/agh.tar.gz \
    "https://github.com/AdguardTeam/AdGuardHome/releases/latest/download/${ASSET}" \
    || die "下载失败，请检查网络或稍后重试"
fi

rm -rf /opt/AdGuardHome
tar -xzf /tmp/agh.tar.gz -C /opt
rm -f /tmp/agh.tar.gz
[ -x /opt/AdGuardHome/AdGuardHome ] || die "解压后未找到可执行文件"

if have_systemd; then
  cat > /etc/systemd/system/AdGuardHome.service <<'EOF'
[Unit]
Description=AdGuard Home
Wants=network-online.target
After=network-online.target

[Service]
ExecStart=/opt/AdGuardHome/AdGuardHome -c /opt/AdGuardHome/AdGuardHome.yaml -w /opt/AdGuardHome
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable --now AdGuardHome
  say "服务已启动 (systemctl status AdGuardHome)"
else
  say "未检测到 systemd，请手动运行: /opt/AdGuardHome/AdGuardHome"
fi
say "AdGuard Home 安装完成，首次访问 http://<主机IP>:3000 完成初始化向导"
