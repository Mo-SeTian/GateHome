# 远程回家 · WireGuard

GateHome 内置 WireGuard 和用户态网络栈。原生 Linux 和 Docker 部署都可以直接在「服务接入 → 远程回家 → 配置 WireGuard」中开启。使用现有 GateHome 用户权限即可运行。

## 手机访问家庭局域网

1. 填写公网域名或 IP 和 UDP 端口。默认端口为 `51820`。公网 IPv4 位于路由器时，将 UDP 端口映射到 GateHome 主机。公网 IPv6 需要连通并放行入站 UDP。DDNS 域名需要使用普通 DNS 解析。
2. 家庭局域网段填实际地址，例如 `192.168.2.0/24`。另选独立的隧道地址段，例如 `10.77.0.0/24`。
3. 选择「添加设备 → 整个家庭网段」。用手机的 WireGuard 扫描二维码。电脑可以下载并导入配置文件。已有客户端可以提交公钥，按设备详情填写连接参数。
4. 手机关闭 Wi-Fi，在移动网络开启 WireGuard，然后打开家庭服务，例如 `http://192.168.2.60:7777`。页面可查看最近握手和收发流量。

家庭服务支持 IPv4 TCP 和 UDP，服务端看到的连接来源是 GateHome。普通互联网流量继续走手机原来的网络。如果外部 Wi-Fi 与家庭网段相同，可切换到移动网络验证。按 IP 访问不会自动转发局域网广播或 mDNS，连通性请用服务端口验证。

## Docker UDP 端口

使用 GateHome 的 host 网络模式时，WireGuard 直接监听主机上的 UDP 端口。使用 bridge 时，确保容器发布相同 UDP 端口。仓库的 `compose.bridge.yaml` 默认发布 `51820/udp`，自定义端口时调整 `GATEHOME_WG_PORT` 并重建容器。已有 Docker 部署通过在线更新得到新版程序后，仍需确认 Compose 已发布该 UDP 端口。

## 设备权限和维护

- 可以授权整个家庭网段、单个内网设备、单个端口或指定反代服务。GateHome 管理端口默认拒绝，启用该权限后仍需要管理账户登录。
- 反代授权使用内置隧道 DNS，并按设备公钥对应的隧道地址及反代规则校验。规则的域名或监听端口变化后，重新编辑设备以更新授权。
- 撤销设备会移除公钥并关闭它的转发连接。更改访问范围或公网入口后，按设备详情更新客户端配置。
- 服务端私钥和授权列表保存在 GateHome 数据目录的 `wireguard.json`，权限为 0600，迁移或备份时请单独保存该文件。二维码和下载配置含客户端私钥，仅在创建时提供。

### 从旧宿主机服务迁移

如果旧版已经添加过设备，可在部署内置版时导入原有密钥及授权，客户端可继续使用现有配置。先停止 GateHome 和旧的 `gatehome-wireguard.service`，再运行新版本的 `gatehouse`，以 GateHome 平时使用的用户、数据目录和配置目录执行 `import-wireguard`，从标准输入传入原文件 `/var/lib/gatehome-wireguard/state.json`。原生默认路径示例：

```sh
sudo cat /var/lib/gatehome-wireguard/state.json | sudo -u gatehouse /etc/gatehome/app/gatehouse -config /etc/gatehome/config -data /etc/gatehome/data import-wireguard
```

Docker 可用新镜像下的 `docker compose run --rm -T gatehouse import-wireguard` 接收同一份输入。导入仅在内置配置尚未创建时可用。导入完成后，禁用旧服务并启动 GateHome。旧 Compose 套接字覆盖文件可以移出启动命令。已有配置使用 1024 以下 UDP 端口时，先修改旧配置和客户端的端口，再进行导入。
