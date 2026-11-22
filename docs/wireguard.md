# 远程回家 · WireGuard

在控制台选择「服务接入 → 远程回家 → 配置 WireGuard」。首版使用 Linux 内核 WireGuard 和宿主机网络。GateHome 通过本地 Unix 套接字管理专用接口 `gh-wg0`。IPv4 或 IPv6 均可作为公网入口，家庭目标和隧道地址使用 IPv4。

## 宿主机准备

以下以使用 systemd 的 Debian / Ubuntu 为例。在解压的 GateHome 安装包或源码根目录执行。一键安装用户可先进入 `/etc/gatehome/wireguard`。Docker 部署也在 Linux 宿主机执行这些命令。

```sh
sudo apt-get install python3 wireguard-tools iproute2 nftables
getent group gatehouse >/dev/null || sudo groupadd --system gatehouse
sudo install -D -m 0644 deploy/wireguard_host.py /usr/local/lib/gatehome/wireguard_host.py
sudo install -m 0644 deploy/gatehome-wireguard.service /etc/systemd/system/
echo 'net.ipv4.ip_forward=1' | sudo tee /etc/sysctl.d/90-gatehome-wireguard.conf >/dev/null
sudo sysctl -p /etc/sysctl.d/90-gatehome-wireguard.conf
sudo systemctl daemon-reload
sudo systemctl enable --now gatehome-wireguard.service
```

原生 GateHome 服务使用 gatehouse 组，刷新「远程回家」即可。Docker 用户另外执行：

```sh
export GATEHOME_WG_GID="$(getent group gatehouse | cut -d: -f3)"
docker compose -f compose.yaml -f compose.wireguard.yaml up -d
```

使用 bridge 部署时把第一份 compose 文件换成 `compose.bridge.yaml`。局域网访问由宿主机提供，按反代规则授权需要 host 网络模式。重复部署时保留组 ID 和这两份 compose 文件。接口名 `gh-wg0` 和 nftables 表 `inet gatehome_wg` 由本服务专用。

Docker 主机可能默认拒绝 FORWARD 流量。有握手但打不开内网服务时，先按下面的「诊断与维护」配置所用防火墙。

## 手机访问家庭局域网

1. 填写公网地址，例如 DDNS 域名。使用普通 DNS 解析。公网 IPv4 位于路由器时，将选定的 UDP 端口映射至 Linux 主机。公网 IPv6 需要两端 IPv6 连通并放行入站 UDP。仅有运营商 NAT 且无可达公网 IPv6 的环境，需要先准备可达入口。
2. 选择实际的家庭网段，例如 `192.168.2.0/24`，另选独立隧道网段，例如 `10.77.0.0/24`。地址转换默认开启；关闭时，在家庭路由器添加通往隧道网段、下一跳为 Linux 主机的静态路由。
3. 添加手机，访问范围选择「整个家庭网段」。用 WireGuard 扫描一次性展示的二维码，或将下载的配置导入客户端。已经持有密钥的设备可提交公钥，使用设备详情中的服务器公钥、地址和 AllowedIPs 手动配置。
4. 手机关闭 Wi-Fi，打开隧道并访问实际地址，例如 `http://192.168.2.60:7777`。确认页面有最近握手和收发流量。其他网站使用手机原来的网络。当外部 Wi-Fi 与家庭使用同一子网时可切换到移动网络。

授权「指定内网设备或端口」时，仅该目标可达。管理端口需要另行勾选且目标位于授权范围内，GateHome 仍要求管理员登录。撤销授权立即移除该设备的 WireGuard 公钥。修改访问范围或公网入口后，请按详情同步客户端路由、DNS 或 Endpoint。

## 按反代规则授权

可选功能需要 Linux 主机安装 `dnsmasq-base`（提供 dnsmasq 可执行文件）。GateHome 在专用隧道地址 `10.254.254.254` 提供 DNS，将选中的域名解析到隧道入口，其他 DNS 查询使用宿主机配置的解析器。此地址和它的 53 端口必须可用。客户端仍使用原有反代域名、协议和监听端口。服务端按设备公钥对应的隧道 IP 和反代规则再次校验。规则变更或端口迁移后，重新编辑设备以更新授权。

## 诊断与维护

- 没有宿主机连接时，先检查 `systemctl status gatehome-wireguard`、套接字目录挂载和 gatehouse 用户组。
- 有握手但无法访问内网时，检查 Linux 转发、防火墙的 FORWARD 链、设备自身的服务监听和回程路由。Docker、UFW 或 firewalld 可能另有默认拒绝转发的规则，请在所用防火墙中允许从 gh-wg0 到指定 LAN 接口的转发及对应的已建立回程流量。以使用 iptables 后端的 Docker 为例，在 DOCKER-USER 链的最终 RETURN 之前放行两条方向明确的规则。去程匹配 `-i gh-wg0 -o 家庭网卡 -d 192.168.2.0/24`，回程匹配 `-i 家庭网卡 -o gh-wg0 -s 192.168.2.0/24 -m conntrack --ctstate ESTABLISHED,RELATED`。使用实际 LAN 接口和网段。
- 加载宿主机防火墙配置时保留 `inet gatehome_wg` 表。如果配置使用 `flush ruleset`，先停止 WireGuard 配套服务，加载防火墙后再启动该服务。
- 隧道接收和发送从网关视角统计。计数器随内核接口或设备重建而重置；仅有握手不能证明家庭服务可用。
- 服务器密钥和授权设备保存在宿主机 `/var/lib/gatehome-wireguard/state.json`，权限为 0600。普通 GateHome 备份不含此文件。客户端私钥仅在创建设备时返回一次。建议通过 HTTPS 或受信任的本地管理网络分发配置。
- 升级独立服务时重新安装脚本和 unit，然后执行 `sudo systemctl daemon-reload` 和 `sudo systemctl restart gatehome-wireguard`。安装脚本卸载 GateHome 时也会停止并禁用配套服务。使用 Docker 或单独停用时可运行 `sudo systemctl disable --now gatehome-wireguard`，停用后授权数据仍保留在宿主机。
