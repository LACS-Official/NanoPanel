#!/usr/bin/env bash
# AdGuard Home 配置脚本：环境变量 web_port / bind
set -euo pipefail
say() { printf "[adguardhome] %s\n" "$*"; }
die() { printf "[adguardhome] 失败: %s\n" "$*" >&2; exit 1; }

web_port="${web_port:-3000}"
bind="${bind:-0.0.0.0}"
yaml=/opt/AdGuardHome/AdGuardHome.yaml

[ -d /opt/AdGuardHome ] || die "AdGuard Home 尚未安装"

if [ -f "$yaml" ]; then
  # 仅替换 http: 小节内的 address 行
  awk -v addr="${bind}:${web_port}" '
    /^[A-Za-z_]+:/ { inhttp = ($0 ~ /^http:/) }
    inhttp && $0 ~ /address:/ { print "  address: " addr; next }
    { print }
  ' "$yaml" > "${yaml}.tmp" && mv "${yaml}.tmp" "$yaml"
  if ! grep -q '^http:' "$yaml"; then
    printf '\nhttp:\n  address: %s:%s\n' "$bind" "$web_port" >> "$yaml"
  fi
else
  # 首次配置：写入最小配置，首次访问时走 /install.html 向导
  cat > "$yaml" <<EOF
http:
  address: ${bind}:${web_port}
users: []
schema_version: 27
EOF
fi

if command -v systemctl >/dev/null 2>&1; then
  systemctl restart AdGuardHome
fi
say "配置已应用: 控制台 ${bind}:${web_port}"
