#!/usr/bin/env bash
set -euo pipefail

SOURCE_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR=/opt/gatehouse
DATA_DIR=/var/lib/gatehouse
SERVICE_FILE=/etc/systemd/system/gatehouse.service

fail() { printf '%s\n' "$1" >&2; exit 1; }
require_linux() {
  [[ "$(uname -s)" == Linux ]] || fail '此脚本仅支持 Linux。'
  [[ ${EUID} -eq 0 ]] || fail '请使用 sudo bash install.sh 运行。'
  for tool in systemctl install getent useradd runuser sha256sum; do
    command -v "$tool" >/dev/null || fail "缺少所需命令：$tool"
  done
  [[ -d /run/systemd/system ]] || fail '此安装方式需要 systemd。容器请使用 Docker 部署文件。'
}

install_gatehouse() {
  require_linux
  local arch binary version
  case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) fail '仅提供 amd64 和 arm64 程序。' ;; esac
  binary="$SOURCE_DIR/dist/gatehouse-linux-$arch"
  [[ -f "$binary" && -f "$SOURCE_DIR/VERSION" && -f "$SOURCE_DIR/SHA256SUMS" ]] || fail '安装包不完整，请使用“版本/版本号”目录中的完整包。'
  (cd -- "$SOURCE_DIR" && sha256sum --check SHA256SUMS) || fail '程序校验失败。'
  version="$(cat -- "$SOURCE_DIR/VERSION")"
  printf '即将安装 Gatehouse %s（%s）。\n' "$version" "$arch"
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
  if ! getent passwd gatehouse >/dev/null; then
    useradd --system --user-group --no-create-home --shell /usr/sbin/nologin gatehouse
  fi
  getent group gatehouse >/dev/null || fail '已有 gatehouse 用户但没有同名用户组，请先创建 gatehouse 系统用户组。'
  systemctl stop gatehouse.service 2>/dev/null || true
  install -d -m 0755 -o root -g root "$INSTALL_DIR"
  install -d -m 0750 -o gatehouse -g gatehouse "$INSTALL_DIR/app"
  install -d -m 0700 -o gatehouse -g gatehouse "$DATA_DIR"
  install -m 0755 -o root -g root "$binary" "$INSTALL_DIR/launcher"
  install -m 0750 -o gatehouse -g gatehouse "$binary" "$INSTALL_DIR/app/gatehouse"
  if [[ ! -f "$DATA_DIR/state.json" ]]; then
    printf '首次安装，请设置管理员密码（输入不会回显）。\n'
    runuser -u gatehouse -- "$INSTALL_DIR/app/gatehouse" -data "$DATA_DIR" init
  else
    printf '保留已有配置和管理员密码。\n'
  fi
  install -m 0644 -o root -g root "$SOURCE_DIR/deploy/gatehouse.service" "$SERVICE_FILE"
  # Replace the management-address override as well when reinstalling an old version.
  install -d -m 0755 -o root -g root "${SERVICE_FILE}.d"
  cat > "${SERVICE_FILE}.d/admin.conf" <<EOF
[Service]
ExecStart=
ExecStart=$INSTALL_DIR/launcher -supervise -managed-root $INSTALL_DIR/app -data $DATA_DIR -admin 0.0.0.0:16666
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
  printf '\n安装完成。管理监听：0.0.0.0:16666\n'
  printf '请使用浏览器访问：http://服务器局域网IP:16666\n'
  local address addresses
  addresses="$(hostname -I 2>/dev/null || true)"
  for address in $addresses; do
    if [[ "$address" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      printf '  http://%s:16666\n' "$address"
    fi
  done
}

uninstall_gatehouse() {
  require_linux
  local answer
  printf '卸载会停止服务并移除程序；默认保留所有配置、Token、证书和日志。\n'
  read -r -p '确认卸载？输入 UNINSTALL：' answer
  [[ "$answer" == UNINSTALL ]] || { printf '已取消。\n'; return; }
  systemctl disable --now gatehouse.service 2>/dev/null || true
  rm -f -- "$SERVICE_FILE"
  rm -f -- "${SERVICE_FILE}.d/admin.conf"
  rmdir -- "${SERVICE_FILE}.d" 2>/dev/null || true
  rm -rf -- "$INSTALL_DIR"
  systemctl daemon-reload
  printf '程序已卸载，数据保留在 %s。\n' "$DATA_DIR"
  read -r -p '如果还要永久删除数据，请输入 DELETE DATA；直接回车保留：' answer
  if [[ "$answer" == 'DELETE DATA' ]]; then
    rm -rf -- "$DATA_DIR"
    printf '数据已删除。\n'
  fi
}

printf '\nGatehouse Linux 安装管理\n1. 安装 / 重新安装（保留配置）\n2. 卸载\n0. 退出\n'
read -r -p '请选择 [0/1/2]：' choice
case "$choice" in 1) install_gatehouse ;; 2) uninstall_gatehouse ;; 0) exit 0 ;; *) fail '无效选项。' ;; esac
