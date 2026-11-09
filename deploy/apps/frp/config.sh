#!/usr/bin/env bash
# frp 配置应用脚本：由面板传入环境变量，生成 frpc.toml 并重启服务
set -euo pipefail

say() { printf "[frp] %s\n" "$*"; }
die() { printf "[frp] 错误: %s\n" "$*" >&2; exit 1; }

server_addr="${server_addr:-127.0.0.1}"
server_port="${server_port:-7000}"
auth_token="${auth_token:-}"
proxy_name="${proxy_name:-panel}"
proxy_type="${proxy_type:-tcp}"
local_ip="${local_ip:-127.0.0.1}"
local_port="${local_port:-8080}"
remote_port="${remote_port:-18080}"

case "$server_addr" in (*" "*|*"'"*|*"\""*) die "服务器地址不能包含空格或引号";; esac
case "$server_port" in (*[!0-9]*|"") die "服务器端口必须是数字";; esac
[ "$server_port" -ge 1 ] && [ "$server_port" -le 65535 ] || die "服务器端口超出范围"
case "$proxy_name" in (*[!A-Za-z0-9_-]*) die "穿透名称只允许字母、数字与 _ -";; esac
[ "${#proxy_name}" -ge 1 ] && [ "${#proxy_name}" -le 64 ] || die "穿透名称长度需为 1-64"
case "$proxy_type" in tcp|udp) ;; *) die "穿透类型必须是 tcp 或 udp";; esac
case "$local_ip" in (*" "*|*"'"*|*"\""*) die "本地地址不能包含空格或引号";; esac
for v in local_port remote_port; do
  eval "val=\${$v}"
  case "$val" in (*[!0-9]*|"") die "$v 必须是数字";; esac
  [ "$val" -ge 1 ] && [ "$val" -le 65535 ] || die "$v 超出范围"
done
[ "${#auth_token}" -le 128 ] || die "令牌过长"
case "$auth_token" in (*"'"*|*"\""*) die "令牌不能包含单引号或双引号";; esac

cat > /etc/frp/frpc.toml <<EOF
serverAddr = "${server_addr}"
serverPort = ${server_port}
auth.token = "${auth_token}"

[[proxies]]
name = "${proxy_name}"
type = "${proxy_type}"
localIP = "${local_ip}"
localPort = ${local_port}
remotePort = ${remote_port}
EOF
chmod 0640 /etc/frp/frpc.toml

if command -v systemctl >/dev/null 2>&1 && systemctl is-enabled frpc >/dev/null 2>&1; then
  systemctl restart frpc
  say "配置已应用并重启：${proxy_type} ${local_ip}:${local_port} → ${server_addr}:${remote_port}"
else
  say "配置已写入 /etc/frp/frpc.toml（服务未启用，启动后生效）"
fi
say "检查连通性: journalctl -u frpc -n 20"
