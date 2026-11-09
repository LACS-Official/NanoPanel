#!/usr/bin/env bash
# PicoClaw 配置脚本：由面板以环境变量传入 manifest 声明的字段
# gateway_port / gateway_host / model / api_base / api_key /
# telegram_token / telegram_allow_from / log_level
set -euo pipefail
say() { printf "[picoclaw] %s\n" "$*"; }
die() { printf "[picoclaw] 失败: %s\n" "$*" >&2; exit 1; }

[ -x /opt/picoclaw/picoclaw ] || die "PicoClaw 尚未安装"
conf=/etc/picoclaw/config.json
mkdir -p /etc/picoclaw /opt/picoclaw/home

# 纯 bash 的 JSON 字符串转义，避免依赖 jq / python3
json_escape() {
  local s="$1"
  s="${s//$'\r'/}"; s="${s//$'\n'/}"; s="${s//$'\t'/}"
  s="${s//\\/\\\\}"
  s="${s//\"/\\\"}"
  printf '%s' "$s"
}

port="${gateway_port:-18790}"
[[ "$port" =~ ^[0-9]{1,5}$ ]] || port=18790
host="${gateway_host:-127.0.0.1}"
level="${log_level:-warn}"
model="${model:-openai/gpt-4o-mini}"
api_base="${api_base:-https://api.openai.com/v1}"
api_key="${api_key:-}"
tg_token="${telegram_token:-}"
tg_allow="${telegram_allow_from:-}"

agents='  "agents": { "defaults": { "restrict_to_workspace": true } },'
models='  "model_list": [],'
if [ -n "$api_key" ]; then
  agents='  "agents": { "defaults": { "restrict_to_workspace": true, "model_name": "main" } },'
  models="$(printf '  "model_list": [\n    { "model_name": "main", "model": "%s", "api_keys": ["%s"], "api_base": "%s" }\n  ],' \
    "$(json_escape "$model")" "$(json_escape "$api_key")" "$(json_escape "$api_base")")"
fi

channels='  "channels": {},'
if [ -n "$tg_token" ]; then
  if [ -n "$tg_allow" ]; then
    allow='["'"$(json_escape "$tg_allow")"'"]'
  else
    allow='[]'
  fi
  channels="$(printf '  "channels": { "telegram": { "enabled": true, "token": "%s", "allow_from": %s } },' \
    "$(json_escape "$tg_token")" "$allow")"
fi

tmp="${conf}.tmp"
cat > "$tmp" <<EOF
{
  "version": 2,
${agents}
${models}
${channels}
  "gateway": { "host": "$(json_escape "$host")", "port": ${port}, "log_level": "$(json_escape "$level")", "hot_reload": true }
}
EOF
chmod 0600 "$tmp"
mv "$tmp" "$conf"

if command -v systemctl >/dev/null 2>&1; then
  systemctl restart picoclaw || say "服务重启失败，请检查: journalctl -u picoclaw -n 30"
fi
say "配置已写入 $conf（0600）: Gateway ${host}:${port}，日志级别 ${level}"
