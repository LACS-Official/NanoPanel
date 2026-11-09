#!/usr/bin/env bash
# WebDAV 配置应用脚本：由面板传入环境变量，生成 config.yml 并重启服务
set -euo pipefail

say() { printf "[webdav] %s\n" "$*"; }
die() { printf "[webdav] 错误: %s\n" "$*" >&2; exit 1; }

port="${port:-6065}"
share_dir="${share_dir:-/opt/webdav/data}"
username="${username:-admin}"
password="${password:-}"
read_only="${read_only:-no}"

case "$port" in (*[!0-9]*|"") die "端口必须是数字";; esac
[ "$port" -ge 1 ] && [ "$port" -le 65535 ] || die "端口超出范围"
[ "${#username}" -ge 1 ] && [ "${#username}" -le 32 ] || die "用户名长度需为 1-32"
case "$username" in (*[!A-Za-z0-9_.-]*) die "用户名只允许字母、数字与 _ . -";; esac
case "$password" in (*"'"*|*"\""*) die "密码不能包含单引号或双引号";; esac
[ "${#password}" -le 128 ] || die "密码过长"
case "$read_only" in yes|no) ;; *) die "read_only 必须是 yes/no";; esac
case "$share_dir" in /*) ;; *) die "共享目录必须是绝对路径";; esac
case "$share_dir" in (*" "*|*"'"*|*"\""*) die "共享目录不能包含空格或引号";; esac

mkdir -p "$share_dir"

perms="CRUD"
[ "$read_only" = "yes" ] && perms="R"

address="127.0.0.1"
users=""
if [ -n "$password" ]; then
  address="0.0.0.0"
  users="users:
  - username: '${username}'
    password: '${password}'"
else
  say "未设置密码：保持仅本机 (127.0.0.1) 监听，设置密码后自动开放局域网"
fi

cat > /etc/webdav/config.yml <<EOF
address: ${address}
port: ${port}
prefix: /
directory: ${share_dir}
permissions: ${perms}
${users}
EOF
chmod 0640 /etc/webdav/config.yml

if command -v systemctl >/dev/null 2>&1 && systemctl is-active webdav >/dev/null 2>&1; then
  systemctl restart webdav
  say "配置已应用: 端口 ${port}，目录 ${share_dir}，监听 ${address}，权限 ${perms}"
else
  say "配置已写入 /etc/webdav/config.yml（服务未运行，启动后生效）"
fi
say "Windows 使用：资源管理器右键「此电脑」→ 映射网络驱动器 → http://<主机IP>:${port}/"
