#!/usr/bin/env bash
# Mihomo 配置脚本：环境变量 mixed_port / allow_lan / external_controller_port / subscribe_url
set -euo pipefail
say() { printf "[mihomo] %s\n" "$*"; }
die() { printf "[mihomo] 失败: %s\n" "$*" >&2; exit 1; }

conf=/etc/mihomo/config.yaml
[ -x /opt/mihomo/mihomo ] || die "mihomo 尚未安装"

# 订阅：填写了就拉取并替换配置
if [ -n "${subscribe_url:-}" ]; then
  say "拉取订阅配置…"
  if curl -fL --connect-timeout 8 --max-time 60 -o "${conf}.new" "$subscribe_url"; then
    if [ -s "${conf}.new" ]; then
      mv "${conf}.new" "$conf"
      say "订阅已更新"
    else
      rm -f "${conf}.new"; say "订阅内容为空，保留原配置"
    fi
  else
    rm -f "${conf}.new"; say "订阅拉取失败，保留原配置"
  fi
fi

# 端口与局域网开关：覆写为面板管理的值
mixed_port="${mixed_port:-7890}"
external_controller_port="${external_controller_port:-9090}"
lan_input="${allow_lan:-no}"
lan_value="false"; [ "$lan_input" = "yes" ] && lan_value="true"

touch "$conf"
if grep -Eq '^mixed-port:' "$conf"; then
  sed -i -E "s|^mixed-port:.*|mixed-port: ${mixed_port}|" "$conf"
else
  sed -i "1i mixed-port: ${mixed_port}" "$conf"
fi
if grep -Eq '^allow-lan:' "$conf"; then
  sed -i -E "s|^allow-lan:.*|allow-lan: ${lan_value}|" "$conf"
else
  sed -i "2i allow-lan: ${lan_value}" "$conf"
fi
if grep -Eq '^external-controller:' "$conf"; then
  sed -i -E "s|^external-controller:.*|external-controller: 127.0.0.1:${external_controller_port}|" "$conf"
else
  printf 'external-controller: 127.0.0.1:%s\n' "$external_controller_port" >> "$conf"
fi

if command -v systemctl >/dev/null 2>&1; then
  systemctl restart mihomo
fi
say "配置已应用: 混合端口 ${mixed_port}，局域网 ${allow_lan}，控制端口 ${external_controller_port}"
