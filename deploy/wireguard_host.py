#!/usr/bin/env python3
"""GateHome's Linux-only WireGuard control service. Accessible through a group-owned Unix socket."""
import base64
import copy
import ipaddress
import json
import os
import re
import shutil
import socketserver
import subprocess
import time
import tempfile
import uuid
from http.server import BaseHTTPRequestHandler
from pathlib import Path

STATE = Path("/var/lib/gatehome-wireguard/state.json")
SOCKET = Path("/run/gatehome-wireguard/control.sock")
IFACE = "gh-wg0"
OWNER = "gatehome-wireguard-v1"
TABLE = "gatehome_wg"
PROXY_IP = "10.254.254.254"
NETWORKS = [ipaddress.ip_network(x) for x in ("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16")]
state = {"settings": None, "devices": []}
fault = ""


class Invalid(ValueError):
    pass


def command(*args, data=None):
    try:
        result = subprocess.run(args, input=data, capture_output=True, text=True, timeout=5, check=True)
    except (OSError, subprocess.SubprocessError):
        # Tool output (especially wg config) may contain key material.
        raise Invalid(f"{args[0]} 执行失败，请检查依赖、权限、端口占用和网络配置") from None
    return result.stdout.strip()


def dependencies():
    return [name for name in ("ip", "wg", "nft") if not shutil.which(name)]


def links():
    return json.loads(command("ip", "-j", "-4", "address", "show"))


def networks():
    return sorted({str(ipaddress.ip_interface(f"{a['local']}/{a['prefixlen']}").network)
                   for link in links() if link["ifname"] not in (IFACE, "lo")
                   for a in link.get("addr_info", []) if a["scope"] == "global"
                   and any(ipaddress.ip_address(a["local"]) in n for n in NETWORKS)})


def private_net(value):
    try:
        net = ipaddress.IPv4Network(value)
    except ValueError:
        raise Invalid("请填写有效的 IPv4 网段，例如 192.168.2.0/24") from None
    if not any(net.subnet_of(n) for n in NETWORKS):
        raise Invalid("请选择家庭私有 IPv4 网段")
    return net


def port(value):
    if type(value) is not int or not 1 <= value <= 65535:
        raise Invalid("端口范围为 1–65535")
    return value


def settings(body, old, devices):
    endpoint = str(body.get("endpoint", "")).strip()
    if "%" in endpoint or any(character.isspace() for character in endpoint):
        raise Invalid("公网入口不能包含作用域或空白字符")
    if endpoint.startswith("[") and endpoint.endswith("]"):
        endpoint = endpoint[1:-1]
    if endpoint.endswith("."):
        endpoint = endpoint[:-1]
    try:
        endpoint = str(ipaddress.ip_address(endpoint))
    except ValueError:
        if len(endpoint) > 253 or not all(re.fullmatch(r"[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?", part)
                                          for part in endpoint.split(".")):
            raise Invalid("请输入公网 IP 或域名，端口请单独填写") from None
    lan, tunnel = private_net(body.get("lan", "")), private_net(body.get("tunnel", ""))
    if not 16 <= tunnel.prefixlen <= 29 or tunnel.overlaps(lan):
        raise Invalid("隧道应使用独立的 /16 至 /29 私有网段")
    proxy = ipaddress.ip_address(PROXY_IP)
    if proxy in tunnel or proxy in lan or any(tunnel.overlaps(ipaddress.ip_network(n)) for n in networks()):
        raise Invalid("隧道或服务地址与现有网段冲突")
    routes = json.loads(command("ip", "-j", "-4", "route", "show", "table", "main"))
    for route in routes:
        dest = route.get("dst", "default")
        if route.get("dev") != IFACE and dest != "default" and tunnel.overlaps(ipaddress.ip_network(dest, strict=False)):
            raise Invalid("隧道网段与宿主机现有路由冲突")
    if devices and (old["tunnel"] != str(tunnel) or old["lan"] != str(lan)):
        raise Invalid("请先撤销已有设备，再修改网段并重新分发配置")
    return dict(endpoint=endpoint, port=port(body.get("port")), tunnel=str(tunnel), lan=str(lan),
                admin_port=port(body.get("admin_port")), snat=body.get("snat", True) is True)


def device(body, current, config, devices):
    name = str(body.get("name", "")).strip()
    if not 1 <= len(name) <= 80 or any(ord(c) < 32 for c in name):
        raise Invalid("设备名称需为 1 至 80 个可见字符")
    mode = body.get("mode")
    lan = private_net(config["lan"])
    rule = dict(mode=mode, target="", port=0, route="", host="", admin=body.get("admin") is True)
    if mode == "host":
        try:
            address = ipaddress.IPv4Address(body.get("target", ""))
        except ValueError:
            raise Invalid("请输入有效的家庭设备 IPv4 地址") from None
        if address not in lan or address in (lan.network_address, lan.broadcast_address):
            raise Invalid("设备地址必须在所选家庭网段中")
        rule.update(target=str(address), port=port(body["port"]) if body.get("port") else 0)
    elif mode == "route":
        route, host = body.get("route", ""), body.get("host", "")
        if not route or not re.fullmatch(r"[a-zA-Z0-9.-]{1,253}", host):
            raise Invalid("请选择可用的反代规则")
        rule.update(route=route, host=host, port=port(body.get("port")), admin=False)
    elif mode != "lan":
        raise Invalid("请选择访问范围")
    if current:
        return dict(current, name=name, **rule), None
    key = str(body.get("public_key", "")).strip()
    secret = None
    if key:
        try:
            raw = base64.b64decode(key, validate=True)
            if len(raw) != 32 or not any(raw) or base64.b64encode(raw).decode() != key:
                raise ValueError()
        except ValueError:
            raise Invalid("WireGuard 公钥格式无效") from None
    else:
        secret = command("wg", "genkey")
        key = command("wg", "pubkey", data=secret)
    if any(p["key"] == key for p in devices):
        raise Invalid("此公钥已授权")
    occupied = {p["address"] for p in devices}
    addresses = private_net(config["tunnel"]).hosts()
    next(addresses)  # server
    address = next((str(a) for a in addresses if str(a) not in occupied), None)
    if not address or len(devices) >= 256:
        raise Invalid("设备数量已达到上限")
    return dict(id=uuid.uuid4().hex, address=address, key=key, name=name, **rule), secret


def allowed(peer, config):
    if peer["mode"] == "lan":
        return config["lan"]
    return (PROXY_IP if peer["mode"] == "route" else peer["target"]) + "/32"


def firewall(config, peers):
    # Check original destinations before Docker/other DNAT; only touch our own table.
    lines = [f"add table inet {TABLE}", f"delete table inet {TABLE}", f"table inet {TABLE} {{", f' comment "{OWNER}"',
             " chain access { type filter hook prerouting priority -110; policy accept;",
             f'  iifname != "{IFACE}" return']
    for peer in peers:
        source = "  ip saddr " + peer["address"]
        if not peer["admin"]:
            lines.append(f'{source} fib daddr type local tcp dport {config["admin_port"]} drop')
        dest = source + " ip daddr " + allowed(peer, config)
        if peer["port"]:
            if peer["mode"] == "route":
                lines.extend([f"{dest} udp dport 53 accept", f"{dest} tcp dport 53 accept"])
            lines.append(f'{dest} tcp dport {peer["port"]} accept')
            if peer["mode"] == "host":
                lines.append(f'{dest} udp dport {peer["port"]} accept')
        else:
            lines.append(dest + " accept")
    lines.extend(["  drop", " }"])
    if any(p["mode"] == "route" for p in peers):
        # The synthetic address belongs exclusively to this host, even with Docker DNAT.
        lines.extend([" chain localdns { type filter hook input priority -5; policy accept;",
                      f'  iifname != "{IFACE}" ip daddr {PROXY_IP} drop', " }",
                      " chain routed { type filter hook forward priority -5; policy accept;",
                      f'  iifname "{IFACE}" ct original ip daddr {PROXY_IP} drop', " }"])
    if config["snat"]:
        lines.extend([" chain outgoing { type nat hook postrouting priority srcnat; policy accept;",
                      f'  iifname "{IFACE}" ip saddr {config["tunnel"]} ip daddr {config["lan"]} masquerade', " }"])
    return "\n".join(lines + ["}", ""])


def owned():
    path = Path("/sys/class/net") / IFACE
    if not path.exists():
        return False
    if (path / "ifalias").read_text().strip() != OWNER:
        raise Invalid("专用接口名称已被其他程序占用")
    return True


dns_process = None
dns_hosts = ()


def configure_dns(peers):
    # A dedicated resolver supplies the tunnel IP for authorized proxy hostnames.
    global dns_process, dns_hosts
    hosts = tuple(sorted({p["host"] for p in peers if p["mode"] == "route"}))
    if hosts == dns_hosts and (not hosts or dns_process and dns_process.poll() is None):
        return
    if dns_process and dns_process.poll() is None:
        dns_process.terminate()
        dns_process.wait(timeout=3)
    dns_process, dns_hosts = None, ()
    if not hosts:
        return
    args = ["dnsmasq", "--no-daemon", "--conf-file=/dev/null", "--no-hosts",
            "--bind-interfaces", "--listen-address=" + PROXY_IP, "--pid-file="]
    for host in hosts:
        args.extend(["--local=/" + host + "/", "--address=/" + host + "/" + PROXY_IP])
    try:
        proc = subprocess.Popen(args, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    except OSError:
        raise Invalid("指定反代服务需要安装 dnsmasq，并允许隧道地址监听 DNS") from None
    time.sleep(0.12)
    if proc.poll() is not None:
        raise Invalid("隧道 DNS 启动失败，请检查 dnsmasq 和 53 端口占用")
    dns_process, dns_hosts = proc, hosts


def table_owned():
    entries = json.loads(command("nft", "-j", "list", "tables"))["nftables"]
    if not any(t.get("table", {}).get("name") == TABLE and t["table"].get("family") == "inet" for t in entries):
        return False
    entries = json.loads(command("nft", "-j", "list", "table", "inet", TABLE))["nftables"]
    if not any(t.get("table", {}).get("comment") == OWNER for t in entries):
        raise Invalid("专用防火墙表名称已被占用，请检查 inet gatehome_wg")
    return True


def teardown():
    configure_dns([])
    if owned():
        command("ip", "link", "del", "dev", IFACE)
    if table_owned():
        command("nft", "delete", "table", "inet", TABLE)


def apply(desired):
    config = desired["settings"]
    if not config:
        return
    table_owned()
    existing = owned()
    if not existing:
        command("ip", "link", "add", "dev", IFACE, "type", "wireguard")
        try:
            command("ip", "link", "set", "dev", IFACE, "alias", OWNER)
        except Invalid:
            command("ip", "link", "del", "dev", IFACE)
            raise
    # Apply a complete ACL atomically before adding peers; syncconf preserves sessions.
    peers = desired["devices"]
    command("nft", "-f", "-", data=firewall(config, peers))
    wg = [f'[Interface]\nPrivateKey = {desired["private"]}\nListenPort = {config["port"]}']
    wg.extend(f'[Peer]\nPublicKey = {p["key"]}\nAllowedIPs = {p["address"]}/32' for p in peers)
    command("wg", "syncconf", IFACE, "/dev/stdin", data="\n".join(wg) + "\n")
    tunnel = private_net(config["tunnel"])
    wanted = {str(next(tunnel.hosts())) + "/" + str(tunnel.prefixlen)}
    if any(p["mode"] == "route" for p in peers):
        if any(ipaddress.ip_address(PROXY_IP) in ipaddress.ip_network(n) for n in networks()):
            raise Invalid("反代隧道地址与宿主机网络冲突")
        wanted.add(PROXY_IP + "/32")
    current = {f"{a['local']}/{a['prefixlen']}" for link in links() if link["ifname"] == IFACE for a in link.get("addr_info", [])}
    for address in current - wanted:
        command("ip", "address", "del", address, "dev", IFACE)
    for address in wanted - current:
        command("ip", "address", "add", address, "dev", IFACE)
    if Path("/proc/sys/net/ipv4/ip_forward").read_text().strip() != "1":
        raise Invalid("请先在宿主机启用 net.ipv4.ip_forward=1")
    command("ip", "link", "set", "dev", IFACE, "up")
    configure_dns(peers)


def save(desired):
    with tempfile.NamedTemporaryFile(mode="w", dir=STATE.parent, delete=False) as f:
        tmp = Path(f.name)
        try:
            json.dump(desired, f)
            f.flush()
            os.fsync(f.fileno())
            os.replace(tmp, STATE)
        finally:
            tmp.unlink(missing_ok=True)


def mutate(op, body):
    global state, fault
    desired = copy.deepcopy(state)
    peers, config = desired["devices"], desired["settings"]
    result = {}
    if op == "settings":
        desired["settings"] = settings(body, config, peers)
        if not config:
            desired["private"] = command("wg", "genkey")
    else:
        if not config:
            raise Invalid("请先配置 WireGuard")
        previous = next((p for p in peers if p["id"] == body.get("id")), None)
        if op != "add" and not previous:
            raise Invalid("设备已被撤销或不存在")
        if op == "remove":
            peers.remove(previous)
        else:
            peer, secret = device(body, previous, config, peers)
            if previous:
                peers[peers.index(previous)] = peer
            else:
                peers.append(peer)
            result["id"] = peer["id"]
            if not previous and peer["key"] == command("wg", "pubkey", data=desired["private"]):
                raise Invalid("客户端与服务器必须使用不同的密钥")
            if secret:
                host = config["endpoint"]
                endpoint = f'[{host}]' if ":" in host else host
                public = command("wg", "pubkey", data=desired["private"])
                dns_line = f"DNS = {PROXY_IP}\n" if peer["mode"] == "route" else ""
                result["profile"] = (f'[Interface]\nPrivateKey = {secret}\nAddress = {peer["address"]}/32\n{dns_line}\n'
                                     f'[Peer]\nPublicKey = {public}\nEndpoint = {endpoint}:{config["port"]}\n'
                                     f'AllowedIPs = {allowed(peer, config)}\nPersistentKeepalive = 25\n')
    try:
        apply(desired)
        save(desired)
    except (OSError, Invalid):
        if state["settings"]:
            try:
                apply(state)
            except (OSError, Invalid):
                teardown()
        elif owned():
            teardown()
        raise
    state = desired
    fault = ""
    return result


def status():
    missing = dependencies()
    config = state["settings"]
    response = dict(available=True, missing=missing, fault=fault, settings=config, devices=[], running=False,
                    lans=networks() if "ip" not in missing else [],
                    forwarding=Path("/proc/sys/net/ipv4/ip_forward").read_text().strip() == "1")
    activity, transfer = {}, {}
    if config and not missing and owned():
        response["listen_port"] = int(command("wg", "show", IFACE, "listen-port"))
        response["running"] = (response["listen_port"] == config["port"] and
                               bool(int((Path("/sys/class/net") / IFACE / "flags").read_text().strip(), 16) & 1))
        activity = dict(line.split() for line in command("wg", "show", IFACE, "latest-handshakes").splitlines())
        transfer = {key: (int(rx), int(tx)) for key, rx, tx in
                    (line.split() for line in command("wg", "show", IFACE, "transfer").splitlines())}
    if any(p["mode"] == "route" for p in state["devices"]):
        response["dns_ready"] = bool(dns_process and dns_process.poll() is None)
    for p in state["devices"]:
        rx, tx = transfer.get(p["key"], (None, None))
        response["devices"].append(dict({k: v for k, v in p.items() if k != "key"},
                                        handshake=int(activity.get(p["key"], 0)), rx=rx, tx=tx))
    if config and "wg" not in missing:
        response["server_public_key"] = command("wg", "pubkey", data=state["private"])
    return response


class Handler(BaseHTTPRequestHandler):
    timeout = 10

    def log_message(self, *args):
        pass

    def reply(self, code, data):
        payload = json.dumps(data).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def do_GET(self):
        self.handle_op("status" if self.path == "/status" else "")

    def do_POST(self):
        self.handle_op(self.path.lstrip("/"))

    def handle_op(self, op):
        if op not in ("status", "settings", "add", "edit", "remove", "authorize"):
            return self.reply(404, {})
        try:
            if op == "status":
                result = status()
            else:
                length = int(self.headers.get("Content-Length", 0))
                if not 1 <= length <= 16384:
                    raise Invalid("请求内容无效")
                body = json.loads(self.rfile.read(length))
                if not isinstance(body, dict):
                    raise Invalid("请求格式无效")
                if op == "authorize":
                    peer = next((p for p in state["devices"] if p["address"] == body.get("address")), None)
                    if body.get("management") is True:
                        result = {"allowed": bool(peer and peer["admin"])}
                    else:
                        result = {"allowed": bool(peer and peer["mode"] == "route" and peer["route"] == body.get("route"))}
                else:
                    result = mutate(op, body)
        except Invalid as exc:
            return self.reply(400, {"error": str(exc)})
        except (ValueError, TypeError, OSError, KeyError):
            return self.reply(500, {"error": "宿主机配置或运行状态异常，请检查服务"})
        self.reply(200, result)


if __name__ == "__main__":
    import signal
    import sys
    os.umask(0o077)
    if STATE.exists():
        state = json.loads(STATE.read_text())
        try:
            apply(state)
        except (OSError, Invalid) as exc:
            fault = str(exc) if isinstance(exc, Invalid) else "宿主机配置恢复失败"
            if owned():
                teardown()
            # Serve the control socket so the administrator can repair the settings.
    elif owned():
        teardown()
    SOCKET.unlink(missing_ok=True)
    with socketserver.UnixStreamServer(str(SOCKET), Handler) as server:
        os.chmod(SOCKET, 0o660)
        signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
        try:
            server.serve_forever()
        finally:
            if owned():
                teardown()
