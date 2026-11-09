#!/usr/bin/env bash
# MicroPanel 安装脚本
# 支持离线安装（同目录带二进制）与在线安装（自动选择最快镜像源）。
set -euo pipefail

# ============ 可通过环境变量覆盖的配置 ============
REPO="${MP_REPO:-}"                       # GitHub 仓库 owner/repo，留空则只允许离线安装
VERSION="${MP_VERSION:-v0.1.0}"           # 在线安装时的 release 版本
PORT="${MP_PORT:-8080}"                   # 面板端口
MP_USERNAME="${MP_USERNAME:-admin}"       # 登录用户名
MP_PASSWORD="${MP_PASSWORD:-}"            # 留空则自动生成随机密码
MP_RANDOM_USER="${MP_RANDOM_USER:-0}"     # 1 = 生成随机用户名
SOURCE="${MP_SOURCE:-auto}"               # auto | local | online
SKIP_MIRROR_TEST="${MP_SKIP_MIRROR_TEST:-0}"
MP_MIRROR="${MP_MIRROR:-}"                # 手动指定 GitHub 镜像前缀
# ==================================================

INSTALL_DIR="/opt/micropanel"
CONF_DIR="/etc/micropanel"
MIRROR_CONF="${CONF_DIR}/mirror.conf"
SERVICE_NAME="micropanel"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ -t 1 ]; then
  C_RESET=$'\033[0m'; C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'; C_RED=$'\033[31m'; C_BLUE=$'\033[36m'
else
  C_RESET=''; C_GREEN=''; C_YELLOW=''; C_RED=''; C_BLUE=''
fi

log()   { printf "%s[%s]%s %s\n" "$C_BLUE" "$SERVICE_NAME" "$C_RESET" "$*"; }
ok()    { printf "%s[ OK ]%s %s\n" "$C_GREEN" "$C_RESET" "$*"; }
warn()  { printf "%s[WARN]%s %s\n" "$C_YELLOW" "$C_RESET" "$*"; }
die()   { printf "%s[FAIL]%s %s\n" "$C_RED" "$C_RESET" "$*" >&2; exit 1; }
step()  { printf "\n%s==> %s%s\n" "$C_GREEN" "$*" "$C_RESET"; }

# 格式: 名称|下载前缀|探测地址
GITHUB_CANDIDATES=(
  "github|https://github.com|https://api.github.com"
  "ghfast|https://ghfast.top/https://github.com|https://ghfast.top"
  "ghproxy|https://ghproxy.net/https://github.com|https://ghproxy.net"
  "gh-proxy|https://gh-proxy.com/https://github.com|https://gh-proxy.com"
)
APT_CANDIDATES=(
  "aliyun|https://mirrors.aliyun.com|https://mirrors.aliyun.com/ubuntu/"
  "huawei|https://repo.huaweicloud.com|https://repo.huaweicloud.com/ubuntu/"
  "ustc|https://mirrors.ustc.edu.cn|https://mirrors.ustc.edu.cn/ubuntu/"
  "tsinghua|https://mirrors.tuna.tsinghua.edu.cn|https://mirrors.tuna.tsinghua.edu.cn/ubuntu/"
)

# 探测 URL，输出 "HTTP状态 耗时秒"
probe() {
  curl -s -o /dev/null -m 5 --connect-timeout 2 \
    -w '%{http_code} %{time_total}' "$1" 2>/dev/null || echo "000 99"
}

# 从候选列表中选出延迟最低的可用节点，结果写入 BEST_NAME / BEST_PREFIX / BEST_LATENCY
select_fastest() {
  local label="$1"; shift
  local results=() entry name prefix probe_url res code latency best
  for entry in "$@"; do
    IFS='|' read -r name prefix probe_url <<< "$entry"
    res="$(probe "$probe_url")"
    read -r code latency <<< "$res"
    if [[ "$code" =~ ^[23][0-9][0-9]$ ]]; then
      printf "  %-12s %ss  %s可用%s\n" "$name" "$latency" "$C_GREEN" "$C_RESET"
      results+=("$latency $name $prefix")
    else
      printf "  %-12s   --   %s不可用 (%s)%s\n" "$name" "$C_YELLOW" "$code" "$C_RESET"
    fi
  done
  if [ "${#results[@]}" -eq 0 ]; then
    warn "$label 无可用节点，使用首个候选作为记录值"
    IFS='|' read -r name prefix probe_url <<< "$1"
    BEST_NAME="$name"; BEST_PREFIX="$prefix"; BEST_LATENCY="unknown"
    return 1
  fi
  best="$(printf '%s\n' "${results[@]}" | awk 'NR==1 || $1+0 < min { min=$1; line=$0 } END { print line }')"
  read -r BEST_LATENCY BEST_NAME BEST_PREFIX _ <<< "$best"
  ok "$label 选用: $BEST_NAME ($BEST_PREFIX, ${BEST_LATENCY}s)"
  return 0
}

json_field() {
  [ -f "$2" ] || return 0
  grep -o "\"$1\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" "$2" 2>/dev/null | head -n1 | cut -d'"' -f4
}

step "1/8 环境检查"
[ "$(id -u)" -eq 0 ] || die "请用 root 运行（sudo ./install.sh）"
case "$(uname -m)" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  armv7l|armv6l) ARCH="armv7" ;;
  *) die "暂不支持的架构: $(uname -m)" ;;
esac
command -v curl >/dev/null 2>&1 || die "缺少 curl，请先执行: apt-get update && apt-get install -y curl"
command -v install >/dev/null 2>&1 || die "缺少 coreutils 的 install 命令"

IS_WSL=0
if grep -qiE 'microsoft|wsl' /proc/version 2>/dev/null; then IS_WSL=1; fi
HAS_SYSTEMD=0
if command -v systemctl >/dev/null 2>&1 && \
   [ "$(ps -p 1 -o comm= 2>/dev/null | tr -d '[:space:]')" = "systemd" ]; then
  HAS_SYSTEMD=1
fi
OS_NAME="$(. /etc/os-release 2>/dev/null && echo "${PRETTY_NAME:-unknown}")"
ok "系统: ${OS_NAME} / 架构: ${ARCH}"
if [ "$IS_WSL" -eq 1 ]; then ok "检测到 WSL 环境"; fi
if [ "$HAS_SYSTEMD" -eq 1 ]; then
  ok "systemd 可用，将以系统服务方式运行"
else
  warn "systemd 不可用，将以进程守护方式运行（WSL 可在 /etc/wsl.conf 启用 [boot] systemd=true）"
fi

step "2/8 探测最优镜像源"
if [ -n "$MP_MIRROR" ]; then
  GITHUB_NAME="custom"; GITHUB_PREFIX="$MP_MIRROR"; GITHUB_LATENCY="manual"
  ok "使用手动指定的下载源: $MP_MIRROR"
elif [ "$SKIP_MIRROR_TEST" = "1" ]; then
  IFS='|' read -r GITHUB_NAME GITHUB_PREFIX _ <<< "${GITHUB_CANDIDATES[0]}"
  GITHUB_LATENCY="skipped"
  ok "跳过镜像测速，使用: $GITHUB_NAME"
else
  log "正在测试 GitHub 下载源…"
  select_fastest "GitHub 源" "${GITHUB_CANDIDATES[@]}" || true
  GITHUB_NAME="$BEST_NAME"; GITHUB_PREFIX="$BEST_PREFIX"; GITHUB_LATENCY="$BEST_LATENCY"
fi

if [ -n "$MP_MIRROR" ] || [ "$SKIP_MIRROR_TEST" = "1" ]; then
  IFS='|' read -r APT_NAME APT_PREFIX _ <<< "${APT_CANDIDATES[0]}"
  APT_LATENCY="skipped"
else
  log "正在测试软件包镜像源…"
  select_fastest "APT 源" "${APT_CANDIDATES[@]}" || true
  APT_NAME="$BEST_NAME"; APT_PREFIX="$BEST_PREFIX"; APT_LATENCY="$BEST_LATENCY"
fi

step "3/8 创建目录与镜像配置"
mkdir -p "$INSTALL_DIR" "$CONF_DIR" "${INSTALL_DIR}/apps"
chmod 0750 "$CONF_DIR"
cat > "$MIRROR_CONF" <<EOF
# MicroPanel 自动探测出的最优镜像源，由安装脚本生成，可手工修改。
GITHUB_MIRROR=${GITHUB_PREFIX}
GITHUB_MIRROR_NAME=${GITHUB_NAME}
GITHUB_MIRROR_LATENCY=${GITHUB_LATENCY}
APT_MIRROR=${APT_PREFIX}
APT_MIRROR_NAME=${APT_NAME}
APT_MIRROR_LATENCY=${APT_LATENCY}
SELECTED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)
EOF
chmod 0644 "$MIRROR_CONF"
ok "镜像配置已写入 $MIRROR_CONF"

step "4/8 获取二进制"
LOCAL_BIN="${SCRIPT_DIR}/micropanel-${ARCH}"
BIN_TMP="$(mktemp)"
trap 'rm -f "$BIN_TMP"' EXIT
fetch_ok=0
if [ "$SOURCE" != "online" ] && [ -f "$LOCAL_BIN" ]; then
  cp "$LOCAL_BIN" "$BIN_TMP"
  fetch_ok=1
  ok "使用本地二进制 micropanel-${ARCH}（离线安装）"
elif [ -n "$REPO" ]; then
  ASSET="micropanel-${ARCH}"
  log "从镜像源下载 ${ASSET} (${VERSION})…"
  ORDERED=("${GITHUB_PREFIX}")
  for entry in "${GITHUB_CANDIDATES[@]}"; do
    IFS='|' read -r _ p _ <<< "$entry"
    if [ "$p" != "${GITHUB_PREFIX}" ]; then ORDERED+=("$p"); fi
  done
  for prefix in "${ORDERED[@]}"; do
    url="${prefix}/${REPO}/releases/download/${VERSION}/${ASSET}"
    log "尝试: $url"
    if curl -fL --connect-timeout 5 --max-time 180 -o "$BIN_TMP" "$url" 2>/dev/null; then
      fetch_ok=1
      ok "下载成功（源: $prefix）"
      break
    fi
  done
fi
[ "$fetch_ok" -eq 1 ] || die "未找到本地 micropanel-${ARCH}，且在线下载失败。可设置 MP_REPO=owner/repo，或把二进制放到脚本同目录。"

if [ "$SOURCE" != "online" ] && [ -f "${LOCAL_BIN}.sha256" ]; then
  expected="$(cut -d' ' -f1 "${LOCAL_BIN}.sha256")"
  actual="$(sha256sum "$BIN_TMP" | cut -d' ' -f1)"
  [ "$expected" = "$actual" ] || die "二进制 SHA256 校验失败，已中止安装"
  ok "SHA256 校验通过"
fi
install -m 0755 "$BIN_TMP" "${INSTALL_DIR}/micropanel"
BIN="${INSTALL_DIR}/micropanel"
ok "二进制已安装到 $BIN"

step "5/8 生成登录凭据"
FIRST_INSTALL=0
if [ ! -f "${CONF_DIR}/config.json" ]; then
  FIRST_INSTALL=1
  if [ "$MP_RANDOM_USER" = "1" ]; then
    MP_USERNAME="admin_$("$BIN" -genpw 6 | tr '[:upper:]' '[:lower:]')"
  fi
  if [ -z "$MP_PASSWORD" ]; then MP_PASSWORD="$("$BIN" -genpw 16)"; fi
  "$BIN" -init -c "${CONF_DIR}/config.json" \
    -username "$MP_USERNAME" -password "$MP_PASSWORD" -port "$PORT" >/dev/null
  ok "已生成随机密码并写入凭据文件（0600）"
else
  warn "配置已存在，保留原有账号与密码（升级安装）"
fi
chmod 0600 "${CONF_DIR}/config.json" "${CONF_DIR}/credentials.json" 2>/dev/null || true
FINAL_USER="$(json_field username "${CONF_DIR}/credentials.json")"
FINAL_PASS="$(json_field password "${CONF_DIR}/credentials.json")"
if [ -z "$FINAL_USER" ]; then FINAL_USER="$MP_USERNAME"; fi
if [ -z "$FINAL_PASS" ]; then FINAL_PASS="$MP_PASSWORD"; fi

step "6/8 安装应用商店与管理工具"
if [ -d "${SCRIPT_DIR}/apps" ]; then
  cp -a "${SCRIPT_DIR}/apps/." "${INSTALL_DIR}/apps/"
  find "${INSTALL_DIR}/apps" -name '*.sh' -exec chmod 0755 {} +
fi
if [ ! -f "${INSTALL_DIR}/apps/manifest.json" ]; then
  echo '{"version":1,"apps":[]}' > "${INSTALL_DIR}/apps/manifest.json"
fi
if [ -f "${SCRIPT_DIR}/mpctl" ]; then
  install -m 0755 "${SCRIPT_DIR}/mpctl" /usr/local/bin/mpctl
fi
if [ -f "${SCRIPT_DIR}/uninstall.sh" ]; then
  install -m 0755 "${SCRIPT_DIR}/uninstall.sh" "${INSTALL_DIR}/uninstall.sh"
fi
ok "应用目录 / mpctl / 卸载脚本 已就绪"

step "7/8 注册并启动服务"
if [ "$HAS_SYSTEMD" -eq 1 ]; then
  install -m 0644 "${SCRIPT_DIR}/micropanel.service" "/etc/systemd/system/${SERVICE_NAME}.service"
  systemctl daemon-reload
  systemctl enable "${SERVICE_NAME}" >/dev/null 2>&1 || true
  systemctl restart "${SERVICE_NAME}"
  sleep 1
  if ! systemctl is-active --quiet "${SERVICE_NAME}"; then
    journalctl -u "${SERVICE_NAME}" -n 20 --no-pager >&2 || true
    die "服务启动失败"
  fi
  ok "systemd 服务已启动 (systemctl status ${SERVICE_NAME})"
else
  if [ -f "${INSTALL_DIR}/micropanel.pid" ]; then
    old_pid="$(cat "${INSTALL_DIR}/micropanel.pid")"
    if kill -0 "$old_pid" 2>/dev/null; then kill "$old_pid" || true; fi
  fi
  nohup "$BIN" -c "${CONF_DIR}/config.json" >> /var/log/micropanel.log 2>&1 &
  echo $! > "${INSTALL_DIR}/micropanel.pid"
  sleep 1
  kill -0 "$(cat "${INSTALL_DIR}/micropanel.pid")" 2>/dev/null || die "进程启动失败，请查看 /var/log/micropanel.log"
  ok "后台进程已启动 (PID $(cat "${INSTALL_DIR}/micropanel.pid"))"
fi

step "8/8 健康检查"
HEALTH=""
for _i in 1 2 3 4 5; do
  if HEALTH="$(curl -fsS -m 3 "http://127.0.0.1:${PORT}/api/v1/health" 2>/dev/null)"; then break; fi
  sleep 1
done
[ -n "$HEALTH" ] || die "健康检查失败，请查看日志: journalctl -u ${SERVICE_NAME} -n 50"
RUN_VERSION="$(echo "$HEALTH" | grep -o '"version":"[^"]*"' | cut -d'"' -f4 || true)"
RUN_VERSION="${RUN_VERSION:-unknown}"
ok "面板响应正常 (版本 ${RUN_VERSION})"

IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
printf "\n%s" "$C_GREEN"
cat <<EOF
============================================================
  MicroPanel 安装完成
------------------------------------------------------------
  访问地址 : http://${IP:-127.0.0.1}:${PORT}
  本机访问 : http://127.0.0.1:${PORT}
EOF
if [ "$IS_WSL" -eq 1 ]; then
  echo "  Windows  : http://localhost:${PORT}  (WSL2 端口转发)"
fi
cat <<EOF
  用户名   : ${FINAL_USER}
  密码     : ${FINAL_PASS}
  版本     : ${RUN_VERSION}
  下载源   : ${GITHUB_NAME} (${GITHUB_LATENCY}s)
  软件源   : ${APT_NAME} (${APT_LATENCY}s)
------------------------------------------------------------
  管理命令 : mpctl
    mpctl user-info        查看账号密码
    mpctl reset-password   重置为随机密码
    mpctl status|start|stop|restart
    mpctl mirror-info      查看当前镜像源
    mpctl uninstall        卸载面板
============================================================
EOF
printf "%s" "$C_RESET"
if [ "$FIRST_INSTALL" -eq 1 ]; then
  warn "初始密码仅此一次明文展示；随时可用 mpctl user-info 查看，登录后建议尽快修改。"
fi
if [ "$IS_WSL" -eq 1 ]; then
  warn "WSL2 发行版空闲会自动停机；需要长期访问请运行 deploy/wsl-keepalive.ps1（详见 README）。"
fi
