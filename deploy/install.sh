#!/usr/bin/env bash
# NanoPanel 安装脚本
# 支持离线安装（同目录带二进制）与在线安装（自动选择最快镜像源）。
# 在线安装默认跟随 GitHub 最新 Release，也可通过 NP_VERSION 指定版本。
set -euo pipefail

# ============ 可通过环境变量覆盖的配置 ============
REPO="${NP_REPO:-LACS-Official/NanoPanel}" # GitHub 仓库 owner/repo，默认官方仓库
VERSION="${NP_VERSION:-latest}"             # latest = 跟随最新 Release；也可写 v0.1.2
PORT="${NP_PORT:-8080}"                     # 面板端口
NP_USERNAME="${NP_USERNAME:-admin}"         # 登录用户名
NP_PASSWORD="${NP_PASSWORD:-}"              # 留空则自动生成随机密码
NP_RANDOM_USER="${NP_RANDOM_USER:-0}"       # 1 = 生成随机用户名
SOURCE="${NP_SOURCE:-auto}"                 # auto | local | online
SKIP_MIRROR_TEST="${NP_SKIP_MIRROR_TEST:-0}"
NP_MIRROR="${NP_MIRROR:-}"                  # 手动指定 GitHub 镜像前缀
NP_ARCH="${NP_ARCH:-}"                      # 手动指定架构 amd64/arm64/armv7
INSTALL_DIR="${NP_INSTALL_DIR:-/opt/nanopanel}"
CONF_DIR="${NP_CONF_DIR:-/etc/nanopanel}"
# ==================================================

MIRROR_CONF="${CONF_DIR}/mirror.conf"
SERVICE_NAME="nanopanel"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEBSITE="https://npanel.lacs.cc"
REPO_URL="https://github.com/${REPO}"

usage() {
  cat <<EOF
NanoPanel 安装脚本

用法:
  sudo ./install.sh                 # 自动：优先本地二进制，否则在线安装
  curl -fsSL ${WEBSITE}/install.sh | sudo bash

环境变量:
  NP_VERSION          latest（默认）或 v0.1.2
  NP_PORT             面板端口，默认 8080
  NP_USERNAME         用户名，默认 admin
  NP_PASSWORD         密码，默认随机生成
  NP_INSTALL_DIR      安装目录，默认 /opt/nanopanel
  NP_CONF_DIR         配置目录，默认 /etc/nanopanel
  NP_ARCH             强制架构 amd64 / arm64 / armv7
  NP_MIRROR           GitHub 镜像前缀，跳过测速
  NP_REPO             仓库 owner/repo，默认 LACS-Official/NanoPanel
  NP_SOURCE           auto | local | online
  NP_SKIP_MIRROR_TEST 1 = 跳过测速
  NP_DRY_RUN          1 = 只下载与校验，不改动系统

示例:
  curl -fsSL ${WEBSITE}/install.sh | sudo NP_PORT=9090 bash
  curl -fsSL ${WEBSITE}/install.sh | sudo NP_VERSION=v0.1.2 bash
EOF
}

case "${1:-}" in
  -h|--help|help) usage; exit 0 ;;
esac


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
  "github|https://github.com|https://github.com"
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
if [ -n "$NP_ARCH" ]; then
  case "$NP_ARCH" in
    amd64|arm64|armv7) ARCH="$NP_ARCH" ;;
    *) die "NP_ARCH 仅支持 amd64 / arm64 / armv7，收到: $NP_ARCH" ;;
  esac
else
  case "$(uname -m)" in
    x86_64|amd64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    armv7l|armv6l) ARCH="armv7" ;;
    *) die "暂不支持的架构: $(uname -m)，可用 NP_ARCH 手动指定" ;;
  esac
fi
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
if [ -n "$NP_MIRROR" ]; then
  GITHUB_NAME="custom"; GITHUB_PREFIX="$NP_MIRROR"; GITHUB_LATENCY="manual"
  ok "使用手动指定的下载源: $NP_MIRROR"
elif [ "$SKIP_MIRROR_TEST" = "1" ]; then
  IFS='|' read -r GITHUB_NAME GITHUB_PREFIX _ <<< "${GITHUB_CANDIDATES[0]}"
  GITHUB_LATENCY="skipped"
  ok "跳过镜像测速，使用: $GITHUB_NAME"
else
  log "正在测试 GitHub 下载源…"
  select_fastest "GitHub 源" "${GITHUB_CANDIDATES[@]}" || true
  GITHUB_NAME="$BEST_NAME"; GITHUB_PREFIX="$BEST_PREFIX"; GITHUB_LATENCY="$BEST_LATENCY"
fi

if [ -n "$NP_MIRROR" ] || [ "$SKIP_MIRROR_TEST" = "1" ]; then
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
# NanoPanel 自动探测出的最优镜像源，由安装脚本生成，可手工修改。
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

step "4/8 获取二进制与配套文件"
ASSET="nanopanel-${ARCH}"
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT
PKG_DIR="$SCRIPT_DIR" # 离线：脚本同目录；在线：解压出的压缩包目录
AUX_DIR="$SCRIPT_DIR" # apps / npctl / uninstall 所在目录，可能来自源码归档
PKG_SOURCE="本地目录"
fetch_ok=0
RELEASE_LABEL="$VERSION"

# latest 时解析真实 tag：既用于展示，也用于拼压缩包文件名
resolve_latest_tag() {
  curl -fsSI -m 10 "https://github.com/${REPO}/releases/latest" 2>/dev/null \
    | tr -d '\r' | awk 'tolower($1)=="location:"{print $2}' \
    | sed -n 's#.*/tag/##p' | head -n1
}

download_url() {
  if [ "$VERSION" = "latest" ]; then
    printf '%s/%s/releases/latest/download/%s' "$1" "$REPO" "$2"
  else
    printf '%s/%s/releases/download/%s/%s' "$1" "$REPO" "$VERSION" "$2"
  fi
}

mirror_order() {
  ORDERED=("${GITHUB_PREFIX}")
  local entry p
  for entry in "${GITHUB_CANDIDATES[@]}"; do
    IFS='|' read -r _ p _ <<< "$entry"
    if [ "$p" != "${GITHUB_PREFIX}" ]; then ORDERED+=("$p"); fi
  done
  printf '%s\n' "${ORDERED[@]}"
}

if [ "$SOURCE" != "online" ] && [ -f "${SCRIPT_DIR}/${ASSET}" ]; then
  ok "使用本地二进制 ${ASSET}（离线安装）"
  fetch_ok=1
else
  if [ "$VERSION" = "latest" ]; then
    RELEASE_LABEL="$(resolve_latest_tag || true)"
    RELEASE_LABEL="${RELEASE_LABEL:-latest}"
  fi
  log "目标版本: ${RELEASE_LABEL} / 架构: ${ARCH} / 仓库: ${REPO}"

  # 优先整包下载：压缩包里带 npctl、systemd 单元与 apps 应用脚本，
  # 这是 `curl | bash` 场景能装出完整面板的前提。
  # latest 用稳定文件名（release 工作流会额外发布一份），固定版本用带版本号的文件名。
  if [ "$VERSION" = "latest" ]; then
    BUNDLE="nanopanel-linux-${ARCH}.tar.gz"
  else
    BUNDLE="nanopanel-${RELEASE_LABEL}-linux-${ARCH}.tar.gz"
  fi
  if command -v tar >/dev/null 2>&1; then
    while read -r prefix; do
      [ -n "$prefix" ] || continue
      url="$(download_url "$prefix" "$BUNDLE")"
      log "尝试: $url"
      if curl -fL --retry 2 --retry-delay 1 --connect-timeout 6 --max-time 300 \
        -o "$WORK_DIR/bundle.tar.gz" "$url" 2>/dev/null; then
        mkdir -p "$WORK_DIR/pkg"
        if tar -xzf "$WORK_DIR/bundle.tar.gz" -C "$WORK_DIR/pkg" 2>/dev/null \
          && [ -f "$WORK_DIR/pkg/${ASSET}" ]; then
          PKG_DIR="$WORK_DIR/pkg"
          AUX_DIR="$WORK_DIR/pkg"
          PKG_SOURCE="$prefix"
          fetch_ok=1
          ok "完整安装包下载成功（源: $prefix）"
          break
        fi
        warn "压缩包解压失败，继续尝试其他源"
      fi
    done <<< "$(mirror_order)"
  fi

  # 二级兜底：从仓库源码归档取 apps / npctl / uninstall（二进制仍单独下载）
  if [ "$fetch_ok" -eq 0 ] && [ "$RELEASE_LABEL" != "latest" ]; then
    log "安装包不可用，尝试从源码归档获取配套脚本…"
    while read -r prefix; do
      [ -n "$prefix" ] || continue
      url="${prefix}/${REPO}/archive/refs/tags/${RELEASE_LABEL}.tar.gz"
      if curl -fL --retry 1 --connect-timeout 6 --max-time 300 -o "$WORK_DIR/src.tar.gz" "$url" 2>/dev/null; then
        mkdir -p "$WORK_DIR/src"
        if tar -xzf "$WORK_DIR/src.tar.gz" -C "$WORK_DIR/src" 2>/dev/null; then
          found="$(find "$WORK_DIR/src" -maxdepth 3 -type d -name deploy | head -n1)"
          if [ -n "$found" ] && [ -d "${found}/apps" ]; then
            AUX_DIR="$found"
            ok "已从源码归档取得应用脚本与 npctl（源: $prefix）"
            break
          fi
        fi
      fi
    done <<< "$(mirror_order)"
  fi

  # 三级兜底：只下载单个二进制
  if [ "$fetch_ok" -eq 0 ]; then
    warn "未能获取完整安装包，改为单文件下载"
    while read -r prefix; do
      [ -n "$prefix" ] || continue
      url="$(download_url "$prefix" "$ASSET")"
      log "尝试: $url"
      if curl -fL --retry 2 --retry-delay 1 --connect-timeout 6 --max-time 240 \
        -o "$WORK_DIR/${ASSET}" "$url" 2>/dev/null; then
        PKG_DIR="$WORK_DIR"
        PKG_SOURCE="$prefix"
        fetch_ok=1
        if [ "$AUX_DIR" = "$SCRIPT_DIR" ]; then AUX_DIR="$WORK_DIR"; fi
        ok "二进制下载成功（源: $prefix）"
        break
      fi
    done <<< "$(mirror_order)"
  fi
fi
[ "$fetch_ok" -eq 1 ] || die "下载失败。可设置 NP_REPO=owner/repo、NP_MIRROR=https://ghfast.top/https://github.com，或把二进制放到脚本同目录做离线安装。"

# 校验：优先用包内 .sha256，其次从同源补下
SUM_FILE="${PKG_DIR}/${ASSET}.sha256"
if [ ! -s "$SUM_FILE" ] && [ "$PKG_DIR" = "$WORK_DIR" ]; then
  for prefix in $(mirror_order); do
    [ -n "$prefix" ] || continue
    if curl -fsL --connect-timeout 6 --max-time 30 -o "$SUM_FILE" \
      "$(download_url "$prefix" "${ASSET}.sha256")" 2>/dev/null && [ -s "$SUM_FILE" ]; then
      break
    fi
  done
fi

if [ -s "$SUM_FILE" ]; then
  expected="$(cut -d' ' -f1 "$SUM_FILE")"
  actual="$(sha256sum "${PKG_DIR}/${ASSET}" | cut -d' ' -f1)"
  if [ -z "$expected" ]; then
    warn "校验文件内容为空，跳过校验"
  elif [ "$expected" != "$actual" ]; then
    die "SHA256 校验失败（期望 ${expected:0:12}…，实际 ${actual:0:12}…），已中止安装"
  else
    ok "SHA256 校验通过"
  fi
else
  warn "未取到 .sha256 校验文件，跳过完整性校验"
fi

if [ "${NP_DRY_RUN:-0}" = "1" ]; then
  ok "预演完成：${PKG_DIR}/${ASSET} 已下载并校验，未改动系统"
  log "将安装到 ${INSTALL_DIR}，配置目录 ${CONF_DIR}，监听端口 ${PORT}"
  exit 0
fi

install -m 0755 "${PKG_DIR}/${ASSET}" "${INSTALL_DIR}/nanopanel"
BIN="${INSTALL_DIR}/nanopanel"
ok "二进制已安装到 $BIN（来源: ${PKG_SOURCE}）"

step "5/8 生成登录凭据"
FIRST_INSTALL=0
if [ ! -f "${CONF_DIR}/config.json" ]; then
  FIRST_INSTALL=1
  if [ "$NP_RANDOM_USER" = "1" ]; then
    NP_USERNAME="admin_$("$BIN" -genpw 6 | tr '[:upper:]' '[:lower:]')"
  fi
  if [ -z "$NP_PASSWORD" ]; then NP_PASSWORD="$("$BIN" -genpw 16)"; fi
  "$BIN" -init -c "${CONF_DIR}/config.json" \
    -username "$NP_USERNAME" -password "$NP_PASSWORD" -port "$PORT" >/dev/null
  ok "已生成随机密码并写入凭据文件（0600）"
else
  warn "配置已存在，保留原有账号与密码（升级安装）"
fi
chmod 0600 "${CONF_DIR}/config.json" "${CONF_DIR}/credentials.json" 2>/dev/null || true
FINAL_USER="$(json_field username "${CONF_DIR}/credentials.json")"
FINAL_PASS="$(json_field password "${CONF_DIR}/credentials.json")"
if [ -z "$FINAL_USER" ]; then FINAL_USER="$NP_USERNAME"; fi
if [ -z "$FINAL_PASS" ]; then FINAL_PASS="$NP_PASSWORD"; fi

step "6/8 安装应用与管理工具"
if [ -d "${AUX_DIR}/apps" ]; then
  cp -a "${AUX_DIR}/apps/." "${INSTALL_DIR}/apps/"
  find "${INSTALL_DIR}/apps" -name '*.sh' -exec chmod 0755 {} +
else
  warn "未获取到 apps 目录，应用清单将为空（可稍后从仓库 deploy/apps 复制到 ${INSTALL_DIR}/apps）"
fi
if [ ! -f "${INSTALL_DIR}/apps/manifest.json" ]; then
  echo '{"version":1,"apps":[]}' > "${INSTALL_DIR}/apps/manifest.json"
fi
if [ -f "${AUX_DIR}/npctl" ]; then
  install -m 0755 "${AUX_DIR}/npctl" /usr/local/bin/npctl
else
  warn "未获取到 npctl，可稍后从仓库 deploy/npctl 手动放置到 /usr/local/bin"
fi
if [ -f "${AUX_DIR}/uninstall.sh" ]; then
  install -m 0755 "${AUX_DIR}/uninstall.sh" "${INSTALL_DIR}/uninstall.sh"
fi
ok "应用目录 / npctl / 卸载脚本 已就绪"

step "7/8 注册并启动服务"
# 单元文件按实际安装路径生成，保证 NP_INSTALL_DIR / NP_CONF_DIR 自定义后仍可用
gen_unit() {
  cat > "$1" <<UNIT
[Unit]
Description=NanoPanel lightweight control panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
# 安装脚本探测出的最优镜像源会作为环境变量传给应用脚本。
EnvironmentFile=-${CONF_DIR}/mirror.conf
WorkingDirectory=${INSTALL_DIR}
ExecStart=${INSTALL_DIR}/nanopanel -c ${CONF_DIR}/config.json
Restart=on-failure
RestartSec=3
StandardOutput=journal
StandardError=journal
SyslogIdentifier=nanopanel

[Install]
WantedBy=multi-user.target
UNIT
}

if [ "$HAS_SYSTEMD" -eq 1 ]; then
  gen_unit "${WORK_DIR}/nanopanel.service"
  install -m 0644 "${WORK_DIR}/nanopanel.service" "/etc/systemd/system/${SERVICE_NAME}.service"
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
  if [ -f "${INSTALL_DIR}/nanopanel.pid" ]; then
    old_pid="$(cat "${INSTALL_DIR}/nanopanel.pid")"
    if kill -0 "$old_pid" 2>/dev/null; then kill "$old_pid" || true; fi
  fi
  nohup "$BIN" -c "${CONF_DIR}/config.json" >> /var/log/nanopanel.log 2>&1 &
  echo $! > "${INSTALL_DIR}/nanopanel.pid"
  sleep 1
  kill -0 "$(cat "${INSTALL_DIR}/nanopanel.pid")" 2>/dev/null || die "进程启动失败，请查看 /var/log/nanopanel.log"
  ok "后台进程已启动 (PID $(cat "${INSTALL_DIR}/nanopanel.pid"))"
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
# 延迟展示：数字带 s，非数字（skipped/unknown/manual）原样输出
fmt_latency() {
  case "$1" in
    ''|*[!0-9.]*) printf '%s' "$1" ;;
    *) printf '%ss' "$1" ;;
  esac
}
printf "\n%s" "$C_GREEN"
cat <<EOF
============================================================
  NanoPanel 安装完成
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
  下载源   : ${GITHUB_NAME} ($(fmt_latency "$GITHUB_LATENCY"))
  软件源   : ${APT_NAME} ($(fmt_latency "$APT_LATENCY"))
------------------------------------------------------------
  官网     : ${WEBSITE}
  文档     : ${WEBSITE}/docs
  仓库     : ${REPO_URL}
------------------------------------------------------------
  管理命令 : npctl
    npctl user-info        查看账号密码
    npctl reset-password   重置为随机密码
    npctl status|start|stop|restart
    npctl mirror-info      查看当前镜像源
    npctl uninstall        卸载面板
============================================================
EOF
printf "%s" "$C_RESET"
if [ "$FIRST_INSTALL" -eq 1 ]; then
  warn "初始密码仅此一次明文展示；随时可用 npctl user-info 查看，登录后建议尽快修改。"
fi
if [ "$IS_WSL" -eq 1 ]; then
  warn "WSL2 发行版空闲会自动停机；需要长期访问请运行 deploy/wsl-keepalive.ps1（详见 README）。"
fi
