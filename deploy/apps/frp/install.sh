#!/usr/bin/env bash
# frp 内网穿透客户端（frpc）安装脚本（fatedier/frp 官方二进制）
set -euo pipefail

say() { printf "[frp] %s\n" "$*"; }
die() { printf "[frp] 错误: %s\n" "$*" >&2; exit 1; }
have_systemd() {
  command -v systemctl >/dev/null 2>&1 && \
    [ "$(ps -p 1 -o comm= 2>/dev/null | tr -d '[:space:]')" = "systemd" ]
}
arch_map() {
  case "$(uname -m)" in
    x86_64)         echo amd64 ;;
    aarch64|arm64)  echo arm64 ;;
    armv7l)         echo armv7 ;;
    armv6l)         echo armv6 ;;
    i[3-6]86|x86)   echo 386 ;;
    *) return 1 ;;
  esac
}

GH="${GITHUB_MIRROR:-https://github.com}"
ARCH="$(arch_map)" || die "不支持的架构: $(uname -m)"
command -v curl >/dev/null 2>&1 || die "缺少 curl"
command -v tar >/dev/null 2>&1 || die "缺少 tar"

# frp 的发布资产名带版本号（frp_vX.Y.Z_linux_amd64.tar.gz）。
# 优先用 GitHub API 解析最新 tag；被限流时退回 releases/latest 的 302 重定向。
resolve_tag() {
  curl -fsSL --connect-timeout 8 --max-time 20 \
    "https://api.github.com/repos/fatedier/frp/releases/latest" 2>/dev/null \
    | grep -oE '"tag_name": *"[^"]+"' | cut -d'"' -f4 | head -1
  return 0
}
tag="$(resolve_tag)"
if [ -z "$tag" ]; then
  for base in "$GH" "https://github.com" "https://ghfast.top/https://github.com"; do
    loc="$(curl -s -o /dev/null -w '%{redirect_url}' --connect-timeout 8 --max-time 20 "${base}/fatedier/frp/releases/latest")"
    tag="$(printf '%s' "$loc" | grep -oE 'tag/v[0-9.]+$' | sed 's/^tag\///')"
    [ -n "$tag" ] && break
  done
fi
[ -n "$tag" ] || die "无法解析 frp 最新版本（GitHub API 限流且重定向失败）"

# frp 的资产名形如 frp_0.72.0_linux_amd64.tar.gz（版本部分不带 v 前缀）
ASSET="frp_${tag#v}_linux_${ARCH}.tar.gz"
SUBPATH="/fatedier/frp/releases/download/${tag}/${ASSET}"
say "下载 frp ${tag} (${ARCH})…"
downloaded=""
for base in "$GH" "https://github.com" \
            "https://ghfast.top/https://github.com" \
            "https://ghproxy.net/https://github.com"; do
  if curl -fsL --connect-timeout 8 --max-time 300 -o /tmp/frp.tar.gz "${base}${SUBPATH}"; then
    downloaded="$base"
    break
  fi
  say "源不可用（${base#https://}），尝试下一个…"
done
[ -n "$downloaded" ] || die "所有下载源均失败，请检查网络或稍后重试"

rm -rf /tmp/frp-extract && mkdir -p /tmp/frp-extract
tar -xzf /tmp/frp.tar.gz -C /tmp/frp-extract
BIN="$(find /tmp/frp-extract -type f -name frpc | head -1)"
[ -n "$BIN" ] || die "压缩包里未找到 frpc 可执行文件"

mkdir -p /opt/frp /etc/frp
install -m 0755 "$BIN" /opt/frp/frpc
rm -rf /tmp/frp-extract /tmp/frp.tar.gz

# 最小可用配置：占位值，请在面板「配置」里填写真实服务器
cat > /etc/frp/frpc.toml <<'EOF'
serverAddr = "127.0.0.1"
serverPort = 7000

[[proxies]]
name = "panel"
type = "tcp"
localIP = "127.0.0.1"
localPort = 8080
remotePort = 18080
EOF
chmod 0640 /etc/frp/frpc.toml

if have_systemd; then
  cat > /etc/systemd/system/frpc.service <<'EOF'
[Unit]
Description=frp client (NanoPanel)
After=network.target

[Service]
Type=simple
ExecStart=/opt/frp/frpc -c /etc/frp/frpc.toml
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable --now frpc >/dev/null 2>&1 || true
  say "服务已启动: systemctl status frpc"
else
  say "未检测到 systemd，请手动运行: /opt/frp/frpc -c /etc/frp/frpc.toml"
fi

say "frp ${tag} 安装完成。请在面板「配置」里填写服务器地址与端口后重启服务。"
