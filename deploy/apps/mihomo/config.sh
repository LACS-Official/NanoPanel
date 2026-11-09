#!/usr/bin/env bash
# Mihomo 配置脚本：环境变量 mixed_port / allow_lan / external_controller_port /
# external_ui(metacubexd) / controller_secret / subscribe_url
set -euo pipefail
say() { printf "[mihomo] %s\n" "$*"; }
die() { printf "[mihomo] 失败: %s\n" "$*" >&2; exit 1; }

conf=/etc/mihomo/config.yaml
ui_dir=/etc/mihomo/ui
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
ui_input="${external_ui:-no}"

# metacubexd 面板开启时控制口必须对外开放，否则远程设备访问不到 /ui
controller_bind="127.0.0.1"
[ "$ui_input" = "yes" ] && controller_bind="0.0.0.0"

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
  sed -i -E "s|^external-controller:.*|external-controller: ${controller_bind}:${external_controller_port}|" "$conf"
else
  printf 'external-controller: %s:%s\n' "$controller_bind" "$external_controller_port" >> "$conf"
fi

# 控制接口密钥：先删除旧值再按需写入，避免残留或 YAML 注入。
# metacubexd 开启时密钥必填；用户留空则自动生成并回写 config.env，面板下次就能自动携带。
secret_value="${controller_secret:-}"
if [ -z "$secret_value" ] && [ "$ui_input" = "yes" ]; then
  secret_value="$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')"
  env_file="${NP_APPS_DIR:-/opt/nanopanel/apps}/mihomo/config.env"
  if [ -f "$env_file" ]; then
    sed -i -E '/^controller_secret=/d' "$env_file"
    printf 'controller_secret=%s\n' "$secret_value" >> "$env_file"
    chmod 600 "$env_file"
    say "已自动生成控制接口密钥并写回应用配置"
  fi
fi
sed -i -E '/^secret:/d' "$conf"
if [ -n "$secret_value" ]; then
  [[ "$secret_value" =~ ^[A-Za-z0-9._-]{1,64}$ ]] || die "控制接口密钥只允许字母、数字、点、下划线和短横"
  printf 'secret: "%s"\n' "$secret_value" >> "$conf"
fi

# metacubexd（external-ui）：下载官方 dist 到内核 ui 目录
sed -i -E '/^external-ui(-name|-url)?:/d' "$conf"
if [ "$ui_input" = "yes" ]; then
  printf 'external-ui: %s\n' "$ui_dir" >> "$conf"
  if [ -f "$ui_dir/index.html" ]; then
    say "metacubexd 已存在（$ui_dir），跳过下载"
  else
    GH="${GITHUB_MIRROR:-https://github.com}"
    UI_URL="$GH/MetaCubeX/metacubexd/releases/latest/download/compressed-dist.tgz"
    say "下载 metacubexd 管理面板…"
    tmp_tgz="$(mktemp)"
    if ! curl -fL --connect-timeout 8 --max-time 120 -o "$tmp_tgz" "$UI_URL"; then
      say "镜像源下载失败，回退 GitHub 直连…"
      curl -fL --connect-timeout 8 --max-time 120 -o "$tmp_tgz" \
        "https://github.com/MetaCubeX/metacubexd/releases/latest/download/compressed-dist.tgz" \
        || { rm -f "$tmp_tgz"; say "警告: metacubexd 下载失败，控制口与代理不受影响，可稍后重新保存配置重试"; }
    fi
    if [ -s "$tmp_tgz" ]; then
      mkdir -p "$ui_dir.new"
      if tar -xzf "$tmp_tgz" -C "$ui_dir.new" 2>/dev/null && [ -f "$ui_dir.new/index.html" ]; then
        rm -rf "$ui_dir"; mv "$ui_dir.new" "$ui_dir"
        say "metacubexd 已安装到 $ui_dir"
      else
        rm -rf "$ui_dir.new"
        say "警告: metacubexd 压缩包解析失败，未启用"
      fi
      rm -f "$tmp_tgz"
    fi
  fi
fi

if command -v systemctl >/dev/null 2>&1; then
  systemctl restart mihomo
fi
if [ "$ui_input" = "yes" ]; then
  say "配置已应用: 混合端口 ${mixed_port}，局域网 ${allow_lan}，控制端口 ${external_controller_port}，metacubexd http://<设备IP>:${external_controller_port}/ui"
else
  say "配置已应用: 混合端口 ${mixed_port}，局域网 ${allow_lan}，控制端口 ${external_controller_port}"
fi
