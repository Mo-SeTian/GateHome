#!/usr/bin/env bash
set -euo pipefail

SCRIPT_PATH="${BASH_SOURCE[0]:-}"
SOURCE_DIR=""
[[ -z "$SCRIPT_PATH" ]] || SOURCE_DIR="$(cd -- "$(dirname -- "$SCRIPT_PATH")" && pwd)"
INSTALL_DIR=/etc/gatehome
CONFIG_DIR="$INSTALL_DIR/config"
LOG_DIR="$INSTALL_DIR/log"
DATA_DIR="$INSTALL_DIR/data"
LEGACY_DATA_DIR=/var/lib/gatehouse
SERVICE_FILE=/etc/systemd/system/gatehouse.service
DOWNLOAD_DIR=""
INSTALL_PROXY=""

fail() { printf '%s\n' "$1" >&2; exit 1; }
require_linux() {
  [[ "$(uname -s)" == Linux ]] || fail '此脚本仅支持 Linux。'
  [[ ${EUID} -eq 0 ]] || fail '请使用 sudo bash install.sh 或 sudo bash -s -- install 运行。'
  for tool in systemctl install getent useradd runuser sha256sum tar; do
    command -v "$tool" >/dev/null || fail "缺少所需命令：$tool"
  done
  [[ -d /run/systemd/system ]] || fail '此安装方式需要 systemd。容器请使用 Docker 部署文件。'
}

ensure_ca_certificates() {
  if [[ ! -f /etc/ssl/certs/ca-certificates.crt && ! -f /etc/pki/tls/certs/ca-bundle.crt ]]; then
    if command -v apt-get >/dev/null; then
      apt-get update
      apt-get install -y ca-certificates
    elif command -v dnf >/dev/null; then
      dnf install -y ca-certificates
    elif command -v yum >/dev/null; then
      yum install -y ca-certificates
    else
      fail '请先安装 ca-certificates，再运行本脚本。'
    fi
  fi
}

download_file() {
  local url="$1" target="$2" attempt
  local proxy_args=(--show-error)
  if [[ -n "$INSTALL_PROXY" ]]; then
    proxy_args=(--proxy "$INSTALL_PROXY" --noproxy "" --stderr /dev/null)
  fi
  for ((attempt=1; attempt<=4; attempt++)); do
    if curl "${proxy_args[@]}" --proto '=https' --proto-redir '=https' --tlsv1.2 --http1.1 -fLs --connect-timeout 10 --max-time 300 "$url" -o "$target"; then
      return
    fi
    rm -f -- "$target"
    if [[ "$attempt" -lt 4 ]]; then
      printf '下载连接失败，2 秒后重试（%s/4）……\n' "$((attempt+1))" >&2
      sleep 2
    fi
  done
  fail 'GitHub 下载失败。请检查 github.com 与 release-assets.githubusercontent.com 的网络或代理规则后重试；尚未修改已有安装。'
}

load_package() {
  local arch="$1" name="gatehome-linux-$1.tar.gz"
  if [[ -n "$SOURCE_DIR" && -f "$SOURCE_DIR/dist/gatehouse-linux-$arch" && -f "$SOURCE_DIR/VERSION" && -f "$SOURCE_DIR/SHA256SUMS" ]]; then
    return
  fi
  command -v curl >/dev/null || fail '请先安装 curl。'
  DOWNLOAD_DIR="$(mktemp -d)"
  local base='https://github.com/Mo-SeTian/GateHome/releases/latest/download'
  printf '下载最新 Linux %s 安装包……\n' "$arch"
  download_file "$base/$name" "$DOWNLOAD_DIR/$name"
  download_file "$base/gatehome-SHA256SUMS" "$DOWNLOAD_DIR/checksums"
  awk -v name="$name" '$2 == name { print }' "$DOWNLOAD_DIR/checksums" > "$DOWNLOAD_DIR/expected"
  [[ "$(wc -l < "$DOWNLOAD_DIR/expected")" -eq 1 ]] || fail '安装包校验清单不完整。'
  (cd -- "$DOWNLOAD_DIR" && sha256sum -c expected >/dev/null) || fail '安装包 SHA-256 校验失败。'
  SOURCE_DIR="$DOWNLOAD_DIR/package"
  mkdir -- "$SOURCE_DIR"
  tar -xzf "$DOWNLOAD_DIR/$name" --no-same-owner -C "$SOURCE_DIR"
}

install_gatehouse() {
  require_linux
  local arch binary version
  case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) fail '仅提供 amd64 和 arm64 程序。' ;; esac
  ensure_ca_certificates
  load_package "$arch"
  binary="$SOURCE_DIR/dist/gatehouse-linux-$arch"
  [[ -f "$binary" && -f "$SOURCE_DIR/VERSION" && -f "$SOURCE_DIR/SHA256SUMS" && -f "$SOURCE_DIR/deploy/gatehouse.service" ]] || fail '安装包不完整。'
  (cd -- "$SOURCE_DIR" && sha256sum -c SHA256SUMS >/dev/null) || fail '程序校验失败。'
  version="$(cat -- "$SOURCE_DIR/VERSION")"
  printf '即将安装 Gatehouse %s（%s）。\n' "$version" "$arch"
  if ! getent passwd gatehouse >/dev/null; then
    useradd --system --user-group --no-create-home --shell /usr/sbin/nologin gatehouse
  fi
  getent group gatehouse >/dev/null || fail '已有 gatehouse 用户但没有同名用户组，请先创建 gatehouse 系统用户组。'
  systemctl stop gatehouse.service 2>/dev/null || true
  install -d -m 0755 -o root -g root "$INSTALL_DIR"
  install -d -m 0750 -o gatehouse -g gatehouse "$INSTALL_DIR/app"
  install -d -m 0700 -o gatehouse -g gatehouse "$CONFIG_DIR" "$LOG_DIR" "$DATA_DIR"
  if [[ ! -f "$CONFIG_DIR/state.json" && -f "$LEGACY_DATA_DIR/state.json" ]]; then
    printf '迁移旧版数据；原目录保留在 %s。\n' "$LEGACY_DATA_DIR"
    cp -a -n -- "$LEGACY_DATA_DIR/." "$DATA_DIR/"
  fi
  chown -R gatehouse:gatehouse "$CONFIG_DIR" "$LOG_DIR" "$DATA_DIR" "$INSTALL_DIR/app"
  install -m 0755 -o root -g root "$binary" "$INSTALL_DIR/launcher"
  install -m 0750 -o gatehouse -g gatehouse "$binary" "$INSTALL_DIR/app/gatehouse"
  runuser -u gatehouse -- "$INSTALL_DIR/app/gatehouse" -config "$CONFIG_DIR" -log "$LOG_DIR" -data "$DATA_DIR" migrate
  if [[ ! -f "$CONFIG_DIR/state.json" ]]; then
    printf '首次安装，请设置管理员账号和管理密码（密码输入不会回显）。\n'
    runuser -u gatehouse -- "$INSTALL_DIR/app/gatehouse" -config "$CONFIG_DIR" -log "$LOG_DIR" -data "$DATA_DIR" init < /dev/tty
  else
    printf '保留已有配置和管理员账户。\n'
  fi
  install -m 0644 -o root -g root "$SOURCE_DIR/deploy/gatehouse.service" "$SERVICE_FILE"
  install -d -m 0755 -o root -g root "${SERVICE_FILE}.d"
  cat > "${SERVICE_FILE}.d/admin.conf" <<EOF
[Service]
WorkingDirectory=$INSTALL_DIR
ExecStart=
ExecStart=$INSTALL_DIR/launcher -supervise -managed-root $INSTALL_DIR/app -config $CONFIG_DIR -log $LOG_DIR -data $DATA_DIR -admin 0.0.0.0:16666
ReadWritePaths=
ReadWritePaths=$INSTALL_DIR/app $CONFIG_DIR $LOG_DIR $DATA_DIR
EOF
  chmod 0644 "${SERVICE_FILE}.d/admin.conf"
  systemctl daemon-reload
  rm -f -- "$DATA_DIR/maintenance/ready.json"
  systemctl enable --now gatehouse.service
  local ready=0
  for ((attempt=0; attempt<80; attempt++)); do
    if [[ -s "$DATA_DIR/maintenance/ready.json" ]]; then ready=1; break; fi
    sleep 0.25
  done
  [[ "$ready" -eq 1 ]] || fail '程序未通过启动检查，请用 journalctl -u gatehouse.service 检查。'
  systemctl is-active --quiet gatehouse.service || fail '服务未启动，请用 journalctl -u gatehouse.service 检查。'
  printf '\n安装完成。目录：%s\n管理界面：http://服务器局域网IP:16666\n' "$INSTALL_DIR"
  printf '配置：%s\n日志：%s\n数据：%s\n' "$CONFIG_DIR" "$LOG_DIR" "$DATA_DIR"
  local address addresses
  addresses="$(hostname -I 2>/dev/null || true)"
  for address in $addresses; do
    if [[ "$address" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then printf '  http://%s:16666\n' "$address"; fi
  done
}

uninstall_gatehouse() {
  require_linux
  local answer
  printf '卸载会停止服务并移除程序；默认保留 config、log、data 三个目录。\n'
  read -r -p '确认卸载？输入 UNINSTALL：' answer < /dev/tty
  [[ "$answer" == UNINSTALL ]] || { printf '已取消。\n'; return; }
  systemctl disable --now gatehouse.service 2>/dev/null || true
  rm -f -- "$SERVICE_FILE" "${SERVICE_FILE}.d/admin.conf" "$INSTALL_DIR/launcher"
  rmdir -- "${SERVICE_FILE}.d" 2>/dev/null || true
  rm -rf -- "$INSTALL_DIR/app"
  systemctl daemon-reload
  printf '程序已卸载，配置、日志和数据保留在 %s。\n' "$INSTALL_DIR"
  read -r -p '永久删除这三个目录请输入 DELETE DATA；回车保留：' answer < /dev/tty
  if [[ "$answer" == 'DELETE DATA' ]]; then
    rm -rf -- "$CONFIG_DIR" "$LOG_DIR" "$DATA_DIR"
    rmdir -- "$INSTALL_DIR" 2>/dev/null || true
    printf '数据已删除。\n'
  fi
}

main() {
  trap '[[ -z "$DOWNLOAD_DIR" ]] || rm -rf -- "$DOWNLOAD_DIR"' EXIT
  local choice=menu
  while [[ "$#" -gt 0 ]]; do
    case "$1" in
      --proxy|-x)
        [[ "$#" -ge 2 && -n "$2" && "$2" != -* ]] || fail '请在 --proxy 后填写代理地址，例如 http://YOUR_PROXY_HOST:7890。'
        INSTALL_PROXY="$2"
        shift 2
        ;;
      install|uninstall|menu|0|1|2)
        [[ "$choice" == menu ]] || fail '请只指定一个安装或卸载操作。'
        choice="$1"
        shift
        ;;
      *) fail '支持 install（安装）、uninstall（卸载），可添加 --proxy <代理地址> 或 -x <代理地址>。' ;;
    esac
  done
  if [[ "$choice" == menu ]]; then
    printf '\nGatehouse Linux 安装管理\n1. 安装 / 重新安装（保留配置）\n2. 卸载\n0. 退出\n'
    read -r -p '请选择 [0/1/2]：' choice < /dev/tty
  fi
  case "$choice" in install|1) install_gatehouse ;; uninstall|2) uninstall_gatehouse ;; 0) return ;; *) fail '支持 install（安装）或 uninstall（卸载）。' ;; esac
}
if [[ -z "$SCRIPT_PATH" || "$SCRIPT_PATH" == "$0" ]]; then main "$@"; fi
