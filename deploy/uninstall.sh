#!/usr/bin/env bash
# MicroPanel 卸载脚本（root 运行）
# 默认保留 /etc/micropanel 配置与凭据，便于重装时恢复。
set -euo pipefail

INSTALL_DIR="/opt/micropanel"
CONF_DIR="/etc/micropanel"
SERVICE="micropanel"
PIDFILE="${INSTALL_DIR}/micropanel.pid"

log(){ echo "[MicroPanel] $*"; }
die(){ echo "[MicroPanel] 错误: $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "请用 root 运行"
[ "$INSTALL_DIR" = "/opt/micropanel" ] || die "安装目录异常，已中止"
[ "$CONF_DIR" = "/etc/micropanel" ] || die "配置目录异常，已中止"

if command -v systemctl >/dev/null 2>&1 && \
   [ "$(ps -p 1 -o comm= 2>/dev/null | tr -d '[:space:]')" = "systemd" ]; then
  systemctl stop "$SERVICE" 2>/dev/null || true
  systemctl disable "$SERVICE" 2>/dev/null || true
  rm -f "/etc/systemd/system/${SERVICE}.service"
  systemctl daemon-reload
elif [ -f "$PIDFILE" ]; then
  pid="$(cat "$PIDFILE" 2>/dev/null || true)"
  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then kill "$pid" || true; fi
fi

rm -f /usr/local/bin/mpctl
rm -rf "$INSTALL_DIR"
log "程序文件已清理"

purge="${MP_PURGE:-}"
if [ -z "$purge" ] && [ -t 0 ] && [ -d "$CONF_DIR" ]; then
  read -r -p "是否同时删除配置与凭据 ${CONF_DIR}？(y/N) " purge
fi
if [ "$purge" = "y" ] || [ "$purge" = "1" ]; then
  rm -rf "$CONF_DIR"
  log "配置已删除"
else
  log "配置已保留在 ${CONF_DIR}"
fi

log "卸载完成"
