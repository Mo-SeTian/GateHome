package gateway

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

const homeStateFile = "wireguard.json"

var homeServiceIP = netip.MustParseAddr("10.254.254.254")
var homePrivateRanges = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("192.168.0.0/16"),
}

type homeSettings struct {
	Endpoint string       `json:"endpoint"`
	Port     int          `json:"port"`
	LAN      netip.Prefix `json:"lan"`
	Tunnel   netip.Prefix `json:"tunnel"`
}
type homePeer struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Address netip.Addr `json:"address"`
	Mode    string     `json:"mode"`
	Target  netip.Addr `json:"target,omitempty"`
	Port    int        `json:"port,omitempty"`
	Route   string     `json:"route,omitempty"`
	Host    string     `json:"host,omitempty"`
	Admin   bool       `json:"admin"`
}
type homeStoredPeer struct {
	homePeer
	Key string `json:"key"`
}
type homeState struct {
	Settings *homeSettings    `json:"settings"`
	Private  string           `json:"private"`
	Devices  []homeStoredPeer `json:"devices"`
}
type homeInput struct {
	Endpoint  string `json:"endpoint"`
	Port      int    `json:"port"`
	LAN       string `json:"lan"`
	Tunnel    string `json:"tunnel"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Mode      string `json:"mode"`
	Target    string `json:"target"`
	Route     string `json:"route"`
	PublicKey string `json:"public_key"`
	Admin     bool   `json:"admin"`
}
type homeRoute struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Host string `json:"host"`
	Port int    `json:"port"`
}
type homeStatusPeer struct {
	homePeer
	Handshake int64  `json:"handshake"`
	Rx        uint64 `json:"rx"`
	Tx        uint64 `json:"tx"`
}
type homeStatus struct {
	Settings *homeSettings    `json:"settings"`
	Running  bool             `json:"running"`
	Port     int              `json:"listen_port,omitempty"`
	Fault    string           `json:"fault,omitempty"`
	Devices  []homeStatusPeer `json:"devices"`
}
type remoteHome struct {
	mu          sync.Mutex
	state       homeState
	runtime     *homeRuntime
	fault       string
	loadBlocked bool
}

func (h *remoteHome) listenPort() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.runtime == nil {
		return 0
	}
	return h.state.Settings.Port
}

func (h *remoteHome) configuredPort() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state.Settings == nil {
		return 0
	}
	return h.state.Settings.Port
}

func homePrefix(value string) (netip.Prefix, bool) {
	p, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil || p != p.Masked() || !p.Addr().Is4() || p.Bits() > 30 {
		return p, false
	}
	return p, slices.ContainsFunc(homePrivateRanges, func(n netip.Prefix) bool {
		return p.Bits() >= n.Bits() && n.Contains(p.Addr())
	})
}

func homeUsableHost(p netip.Prefix, address netip.Addr) bool {
	if !p.Contains(address) || address == p.Addr() {
		return false
	}
	network, ip := p.Addr().As4(), address.As4()
	mask := uint32(1)<<(32-p.Bits()) - 1
	offset := binary.BigEndian.Uint32(ip[:]) ^ binary.BigEndian.Uint32(network[:])
	return offset != mask
}

func localHomeLANs() []string {
	nets := []string{}
	addresses, _ := net.InterfaceAddrs()
	for _, addr := range addresses {
		if p, err := netip.ParsePrefix(addr.String()); err == nil {
			if _, ok := homePrefix(p.Masked().String()); ok {
				nets = append(nets, p.Masked().String())
			}
		}
	}
	slices.Sort(nets)
	return slices.Compact(nets)
}

func normalizeHomeSettingsStatic(input homeInput) (homeSettings, error) {
	endpoint := strings.TrimSuffix(strings.TrimSpace(input.Endpoint), ".")
	endpoint = strings.TrimSuffix(strings.TrimPrefix(endpoint, "["), "]")
	endpoint = strings.ToLower(endpoint)
	addr, ipErr := netip.ParseAddr(endpoint)
	if (ipErr != nil && !validDomain(endpoint)) || (ipErr == nil && (addr.Zone() != "" || addr.IsUnspecified() || addr.IsMulticast())) {
		return homeSettings{}, errors.New("请输入公网域名或 IP，UDP 端口请单独填写")
	}
	lan, ok := homePrefix(input.LAN)
	if !ok || lan.Contains(homeServiceIP) {
		return homeSettings{}, errors.New("请输入有效的家庭私有 IPv4 网段，例如 192.168.2.0/24")
	}
	tunnel, ok := homePrefix(input.Tunnel)
	if !ok || tunnel.Bits() < 16 || tunnel.Bits() > 29 || tunnel.Overlaps(lan) || tunnel.Contains(homeServiceIP) {
		return homeSettings{}, errors.New("隧道应使用独立的 /16 至 /29 私有网段")
	}
	if input.Port < 1024 || input.Port > 65535 {
		return homeSettings{}, errors.New("内置 WireGuard 的 UDP 端口范围为 1024–65535")
	}
	return homeSettings{Endpoint: endpoint, Port: input.Port, LAN: lan, Tunnel: tunnel}, nil
}

func checkHomeTunnelOverlap(tunnel netip.Prefix) error {
	for _, cidr := range localHomeLANs() {
		network, _ := netip.ParsePrefix(cidr)
		if tunnel.Overlaps(network) {
			return errors.New("隧道网段与本机网段重叠")
		}
	}
	return nil
}

func normalizeHomeSettings(input homeInput) (homeSettings, error) {
	settings, err := normalizeHomeSettingsStatic(input)
	if err != nil {
		return homeSettings{}, err
	}
	if err := checkHomeTunnelOverlap(settings.Tunnel); err != nil {
		return homeSettings{}, err
	}
	return settings, nil
}

func homeKey(value string) ([]byte, error) {
	b, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || len(b) != 32 || base64.StdEncoding.EncodeToString(b) != value || slices.Equal(b, make([]byte, 32)) {
		return nil, errors.New("WireGuard 密钥格式无效")
	}
	return b, nil
}

func (s homeState) serverKey() (*ecdh.PrivateKey, error) {
	key, err := homeKey(s.Private)
	if err != nil {
		return nil, err
	}
	return ecdh.X25519().NewPrivateKey(key)
}

func (s homeState) policy() homePolicy {
	p := homePolicy{lan: s.Settings.LAN, peers: make(map[netip.Addr]homePeer, len(s.Devices))}
	for _, peer := range s.Devices {
		p.peers[peer.Address] = peer.homePeer
	}
	return p
}

func homePeerConfig(peer homeStoredPeer) string {
	key, _ := homeKey(peer.Key)
	return fmt.Sprintf("public_key=%x\nreplace_allowed_ips=true\nallowed_ip=%s/32\n", key, peer.Address)
}
func (s homeState) deviceConfig() string {
	var b strings.Builder
	key, _ := homeKey(s.Private)
	fmt.Fprintf(&b, "private_key=%x\nlisten_port=%d\nreplace_peers=true\n", key, s.Settings.Port)
	for _, peer := range s.Devices {
		b.WriteString(homePeerConfig(peer))
	}
	return b.String()
}
func (s homeState) delta(next homeState) string {
	var b strings.Builder
	for _, peer := range s.Devices {
		if !slices.ContainsFunc(next.Devices, func(p homeStoredPeer) bool { return p.Key == peer.Key }) {
			key, _ := homeKey(peer.Key)
			fmt.Fprintf(&b, "public_key=%x\nremove=true\n", key)
		}
	}
	for _, peer := range next.Devices {
		if !slices.ContainsFunc(s.Devices, func(p homeStoredPeer) bool { return p.Key == peer.Key }) {
			b.WriteString(homePeerConfig(peer))
		}
	}
	return b.String()
}

func (a *Admin) StartRemoteHome() {
	h := &a.remoteHome
	h.mu.Lock()
	defer h.mu.Unlock()
	h.loadBlocked = false
	h.fault = ""
	data, err := os.ReadFile(filepath.Join(a.store.paths.Data, homeStateFile))
	if errors.Is(err, os.ErrNotExist) {
		h.state = homeState{}
		return
	}
	var parsed homeState
	if err != nil || json.Unmarshal(data, &parsed) != nil || !validHomeState(parsed) {
		h.state = homeState{}
		h.loadBlocked = true
		h.fault = "无法读取内置 WireGuard 配置，已阻止覆盖以保护原密钥与设备，请检查数据目录"
		return
	}
	h.state = parsed
	if overlapErr := checkHomeTunnelOverlap(h.state.Settings.Tunnel); overlapErr != nil {
		h.fault = overlapErr.Error()
		return
	}
	h.runtime, err = newHomeRuntime(a, h.state)
	if err != nil {
		h.fault = err.Error()
	}
}

func (a *Admin) CloseRemoteHome() {
	h := &a.remoteHome
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.runtime != nil {
		h.runtime.close()
		h.runtime = nil
	}
}

func validHomeState(state homeState) bool {
	if state.Settings == nil || len(state.Devices) > 256 {
		return false
	}
	s := state.Settings
	normalized, err := normalizeHomeSettingsStatic(homeInput{Endpoint: s.Endpoint, Port: s.Port, LAN: s.LAN.String(), Tunnel: s.Tunnel.String()})
	if err != nil || normalized != *s {
		return false
	}
	key, err := state.serverKey()
	if err != nil {
		return false
	}
	pubKeys := map[string]bool{}
	addresses := map[netip.Addr]bool{}
	ids := map[string]bool{}
	for _, p := range state.Devices {
		if addresses[p.Address] || ids[p.ID] || p.ID == "" || !homeUsableHost(s.Tunnel, p.Address) || p.Address == s.Tunnel.Addr().Next() || pubKeys[p.Key] {
			return false
		}
		pub, err := homeKey(p.Key)
		if err != nil || slices.Equal(pub, key.PublicKey().Bytes()) {
			return false
		}
		if p.Mode != "lan" && p.Mode != "host" && p.Mode != "route" ||
			p.Mode == "host" && (!homeUsableHost(s.LAN, p.Target) || p.Port < 0 || p.Port > 65535) ||
			p.Mode == "route" && (!validDomain(p.Host) || p.Route == "" || p.Port <= 53 || p.Port > 65535 || p.Admin) {
			return false
		}
		addresses[p.Address], ids[p.ID], pubKeys[p.Key] = true, true, true
	}
	return true
}

// ImportRemoteHome copies credentials from the former helper via stdin, before
// GateHome starts. The destination is created exclusively and remains private.
func ImportRemoteHome(dir string, input io.Reader) error {
	var state homeState
	decoder := json.NewDecoder(io.LimitReader(input, 1<<20))
	if decoder.Decode(&state) != nil || decoder.Decode(new(any)) != io.EOF || state.Settings == nil {
		return errors.New("无法导入旧 WireGuard 配置，请检查原配置和端口")
	}
	old := state.Settings
	settings, err := normalizeHomeSettings(homeInput{
		Endpoint: old.Endpoint, Port: old.Port, LAN: old.LAN.String(), Tunnel: old.Tunnel.String(),
	})
	if err != nil {
		return err
	}
	state.Settings = &settings
	if !validHomeState(state) {
		return errors.New("无法导入旧 WireGuard 配置，请检查设备数据")
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".wireguard-import-*")
	if err != nil {
		return errors.New("无法创建内置 WireGuard 配置，请检查数据目录权限")
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("无法保存内置 WireGuard 配置")
	}
	if err := os.Link(file.Name(), filepath.Join(dir, homeStateFile)); err != nil {
		return errors.New("无法写入内置 WireGuard 配置，请检查权限及是否已配置")
	}
	return nil
}

func (a *Admin) homeRoutes() []homeRoute {
	cfg := a.store.Snapshot().Config
	routes := []homeRoute{}
	addrs := localAdminAddresses()
	for _, r := range cfg.Routes {
		g, ok := cfg.Group(r.GroupID)
		upstream, err := url.Parse(r.Upstream)
		if !ok || !cfg.RouteActive(r) || r.ID == "" || err != nil || isAdminAccessBlockedTarget(upstream, a.adminPort, addrs) {
			continue
		}
		p := g.HTTPPort
		if r.TLS || p == 0 {
			p = g.HTTPSPort
		}
		if p > 53 {
			routes = append(routes, homeRoute{r.ID, r.Name, r.Host, p})
		}
	}
	return routes
}

func (a *Admin) editHomePeer(next *homeState, op string, input homeInput) (map[string]string, error) {
	if next.Settings == nil {
		return nil, errors.New("请先配置 WireGuard")
	}
	index := slices.IndexFunc(next.Devices, func(p homeStoredPeer) bool { return p.ID == input.ID })
	if op != "add" && index < 0 {
		return nil, errors.New("设备已撤销或不存在")
	}
	if op == "remove" {
		next.Devices = slices.Delete(next.Devices, index, index+1)
		return nil, nil
	}
	p := homeStoredPeer{}
	if op == "edit" {
		p = next.Devices[index]
	}
	p.Name = strings.TrimSpace(input.Name)
	if p.Name == "" || len([]rune(p.Name)) > 80 || strings.ContainsAny(p.Name, "\x00\r\n") {
		return nil, errors.New("设备名称应为 1 至 80 个可见字符")
	}
	p.Mode, p.Admin = input.Mode, input.Admin
	p.Target, p.Port, p.Host, p.Route = netip.Addr{}, 0, "", ""
	switch input.Mode {
	case "lan":
	case "host":
		addr, err := netip.ParseAddr(strings.TrimSpace(input.Target))
		if err != nil || !homeUsableHost(next.Settings.LAN, addr) || input.Port < 0 || input.Port > 65535 {
			return nil, errors.New("请输入家庭网段内的设备地址和有效端口")
		}
		p.Target, p.Port = addr, input.Port
	case "route":
		for _, route := range a.homeRoutes() {
			if route.ID == input.Route {
				p.Route, p.Host, p.Port = route.ID, route.Host, route.Port
				break
			}
		}
		if p.Route == "" {
			return nil, errors.New("请选择当前可用的反代规则")
		}
		p.Admin = false
	default:
		return nil, errors.New("请选择访问范围")
	}
	if op == "edit" {
		next.Devices[index] = p
		return map[string]string{"id": p.ID}, nil
	}
	serverKey, err := next.serverKey()
	if err != nil {
		return nil, err
	}
	var secret *ecdh.PrivateKey
	p.Key = strings.TrimSpace(input.PublicKey)
	if p.Key == "" {
		secret, err = ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			return nil, errors.New("无法生成设备密钥")
		}
		p.Key = base64.StdEncoding.EncodeToString(secret.PublicKey().Bytes())
	}
	pub, err := homeKey(p.Key)
	if err != nil {
		return nil, err
	}
	public, _ := ecdh.X25519().NewPublicKey(pub)
	if _, err := serverKey.ECDH(public); err != nil || slices.Equal(pub, serverKey.PublicKey().Bytes()) {
		return nil, errors.New("客户端公钥无效")
	}
	if slices.ContainsFunc(next.Devices, func(d homeStoredPeer) bool { return d.Key == p.Key }) {
		return nil, errors.New("此公钥已授权")
	}
	tunnel := next.Settings.Tunnel
	p.Address = tunnel.Addr().Next().Next()
	for slices.ContainsFunc(next.Devices, func(d homeStoredPeer) bool { return d.Address == p.Address }) {
		p.Address = p.Address.Next()
	}
	if !homeUsableHost(tunnel, p.Address) || len(next.Devices) >= 256 {
		return nil, errors.New("设备数量已达到上限")
	}
	p.ID = rand.Text()
	next.Devices = append(next.Devices, p)
	result := map[string]string{"id": p.ID}
	if secret != nil {
		allowed, dns := next.Settings.LAN.String(), ""
		if p.Mode == "host" {
			allowed = p.Target.String() + "/32"
		}
		if p.Mode == "route" {
			allowed = homeServiceIP.String() + "/32"
			dns = "DNS = " + homeServiceIP.String() + "\n"
		}
		result["profile"] = fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = %s/32\n%s\n[Peer]\nPublicKey = %s\nEndpoint = %s\nAllowedIPs = %s\nPersistentKeepalive = 25\n",
			base64.StdEncoding.EncodeToString(secret.Bytes()), p.Address, dns, base64.StdEncoding.EncodeToString(serverKey.PublicKey().Bytes()),
			net.JoinHostPort(next.Settings.Endpoint, strconv.Itoa(next.Settings.Port)), allowed)
	}
	return result, nil
}

func (a *Admin) changeRemoteHome(op string, input homeInput) (map[string]string, error) {
	h := &a.remoteHome
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.loadBlocked {
		return nil, errors.New("内置 WireGuard 配置文件读取失败，已阻止覆盖以保护原密钥与设备，请先排查数据目录")
	}
	next := h.state
	next.Devices = slices.Clone(next.Devices)
	var result map[string]string
	var err error
	if op == "settings" {
		settings, err := normalizeHomeSettings(input)
		if err != nil {
			return nil, err
		}
		for _, f := range a.store.Snapshot().Config.Forwards {
			if f.Protocol != "tcp" && f.ListenPort == settings.Port {
				return nil, errors.New("内置 WireGuard 端口不能与 UDP 端口转发相同")
			}
		}
		if len(next.Devices) > 0 && (settings.LAN != next.Settings.LAN || settings.Tunnel != next.Settings.Tunnel) {
			return nil, errors.New("已有设备时不能修改网段")
		}
		next.Settings = &settings
		if next.Private == "" {
			key, err := ecdh.X25519().GenerateKey(rand.Reader)
			if err != nil {
				return nil, errors.New("无法生成 WireGuard 服务器密钥")
			}
			next.Private = base64.StdEncoding.EncodeToString(key.Bytes())
		}
	} else {
		result, err = a.editHomePeer(&next, op, input)
		if err != nil {
			return nil, err
		}
	}
	if result == nil {
		result = map[string]string{}
	}
	replacement := h.runtime == nil || next.Settings.Port != h.state.Settings.Port
	runtime := h.runtime
	if replacement {
		runtime, err = newHomeRuntime(a, next)
		if err != nil {
			return nil, err
		}
	} else if delta := h.state.delta(next); delta != "" {
		if err := runtime.device.IpcSet(delta); err != nil {
			return nil, errors.New("无法应用 WireGuard 设备变更")
		}
	}
	data, _ := json.Marshal(next)
	if err := atomicWrite(filepath.Join(a.store.paths.Data, homeStateFile), data); err != nil {
		if replacement {
			runtime.close()
		} else if delta := next.delta(h.state); delta != "" {
			_ = runtime.device.IpcSet(delta)
		}
		return nil, errors.New("无法保存内置 WireGuard 配置")
	}
	if replacement && h.runtime != nil {
		h.runtime.close()
	}
	runtime.setPolicy(next.policy())
	h.runtime, h.state, h.fault = runtime, next, ""
	return result, nil
}

func (a *Admin) remoteHomeStatus() (homeStatus, string) {
	h := &a.remoteHome
	h.mu.Lock()
	defer h.mu.Unlock()
	s := homeStatus{Settings: h.state.Settings, Fault: h.fault, Running: h.runtime != nil, Devices: []homeStatusPeer{}}
	peers := make(map[string]homeStatusPeer, len(h.state.Devices))
	for _, p := range h.state.Devices {
		key, _ := homeKey(p.Key)
		peers[hex.EncodeToString(key)] = homeStatusPeer{homePeer: p.homePeer}
	}
	if h.runtime != nil {
		s.Port = s.Settings.Port
		snapshot, _ := h.runtime.device.IpcGet()
		var key string
		for line := range strings.SplitSeq(snapshot, "\n") {
			field, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			if field == "public_key" {
				key = value
				continue
			}
			p, found := peers[key]
			if !found {
				continue
			}
			switch field {
			case "last_handshake_time_sec":
				p.Handshake, _ = strconv.ParseInt(value, 10, 64)
			case "rx_bytes":
				p.Rx, _ = strconv.ParseUint(value, 10, 64)
			case "tx_bytes":
				p.Tx, _ = strconv.ParseUint(value, 10, 64)
			default:
				continue
			}
			peers[key] = p
		}
	}
	for _, p := range h.state.Devices {
		key, _ := homeKey(p.Key)
		s.Devices = append(s.Devices, peers[hex.EncodeToString(key)])
	}
	if key, err := h.state.serverKey(); err == nil {
		return s, base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
	}
	return s, ""
}

func (a *Admin) remoteHomeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/remote-home", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		status, key := a.remoteHomeStatus()
		hosts := []string{}
		for _, g := range a.store.Snapshot().Config.DDNS.Groups {
			if g.Enabled {
				hosts = append(hosts, g.Hosts...)
			}
		}
		jsonResponse(w, 200, struct {
			homeStatus
			DDNS      []string    `json:"ddns"`
			LANs      []string    `json:"lans"`
			Routes    []homeRoute `json:"routes"`
			ServerKey string      `json:"server_public_key"`
		}{status, hosts, localHomeLANs(), a.homeRoutes(), key})
	}))
	for _, op := range []string{"settings", "add", "edit", "remove"} {
		mux.HandleFunc("POST /api/remote-home/"+op, a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
			var input homeInput
			if !decodeBody(w, r, &input) {
				return
			}
			a.updateMu.Lock()
			defer a.updateMu.Unlock()
			if a.maintenance != nil && a.maintenance.Busy() {
				apiError(w, 409, "维护操作执行中，请稍后重试")
				return
			}
			result, err := a.changeRemoteHome(op, input)
			if err != nil {
				apiError(w, 400, err.Error())
				return
			}
			jsonResponse(w, 200, result)
		}))
	}
}
