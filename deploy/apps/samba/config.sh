#!/usr/bin/env bash
# Samba 配置应用脚本：由面板传入环境变量，生成受管共享段并重启 smbd
set -euo pipefail

say() { printf "[samba] %s\n" "$*"; }
die() { printf "[samba] 错误: %s\n" "$*" >&2; exit 1; }

share_name="${share_name:-nanoshare}"
share_dir="${share_dir:-/opt/samba/share}"
username="${username:-nanofile}"
password="${password:-}"
guest="${guest:-no}"
read_only="${read_only:-no}"

[ "${#share_name}" -ge 1 ] && [ "${#share_name}" -le 32 ] || die "共享名长度需为 1-32"
case "$share_name" in *[!A-Za-z0-9_-]*) die "共享名只允许字母、数字、下划线与短横线";; esac
[ "${#username}" -le 32 ] || die "用户名过长"
[ "${#password}" -le 128 ] || die "密码过长"
case "$guest" in yes|no) ;; *) die "guest 必须是 yes/no";; esac
case "$read_only" in yes|no) ;; *) die "read_only 必须是 yes/no";; esac
case "$share_dir" in /*) ;; *) die "共享目录必须是绝对路径";; esac
case "$share_dir" in (*" "*|*"'"*|*"\""*) die "共享目录不能包含空格或引号";; esac

command -v smbd >/dev/null 2>&1 || [ -x /usr/sbin/smbd ] || die "Samba 尚未安装"
CONF=/etc/samba/smb.conf
[ -f "$CONF" ] || die "缺少 /etc/samba/smb.conf"

mkdir -p "$share_dir"

guest_ok="no"
valid_users_line="valid users = ${username}"
if [ "$guest" = "yes" ]; then
  guest_ok="yes"
  valid_users_line="guest ok = yes"
  say "访客模式：局域网内无需账号即可访问共享"
else
  [ -n "$password" ] || die "非访客模式必须设置访问密码"
fi

if [ "$guest" != "yes" ]; then
  # 建立 Linux 账号（仅用于 Samba 认证，禁止登录）并设置 SMB 密码
  if ! id -u "$username" >/dev/null 2>&1; then
    useradd -M -s /usr/sbin/nologin "$username"
    say "已创建系统账号 $username（禁止交互登录）"
  fi
  printf '%s\n%s\n' "$password" "$password" | smbpasswd -s -a "$username" >/dev/null \
    || die "设置 Samba 密码失败（密码可能包含 smbpasswd 不接受的字符）"
  chown "$username" "$share_dir" 2>/dev/null || true
fi

ro="yes"
[ "$read_only" = "no" ] && ro="no"

# 替换面板受管的共享段（幂等）
if [ -f "$CONF" ] && grep -q "BEGIN NanoPanel" "$CONF"; then
  sed -i '/BEGIN NanoPanel/,/END NanoPanel/d' "$CONF"
fi
cat >> "$CONF" <<EOF
# BEGIN NanoPanel managed share
[${share_name}]
   path = ${share_dir}
   browseable = yes
   read only = ${ro}
   ${valid_users_line}
   create mask = 0664
   directory mask = 0775
# END NanoPanel managed share
EOF

testparm -s "$CONF" >/dev/null 2>&1 || die "生成的 smb.conf 校验未通过，请检查共享名与目录"

if command -v systemctl >/dev/null 2>&1 && systemctl is-active smbd >/dev/null 2>&1; then
  systemctl restart smbd
  say "配置已应用: 共享名 ${share_name}，目录 ${share_dir}，访客 ${guest}，只读 ${ro}"
else
  systemctl restart smbd >/dev/null 2>&1 || true
  say "配置已写入 smb.conf（服务可能未运行）"
fi
say "Windows 使用：资源管理器打开 \\\\主机IP\\${share_name}"
