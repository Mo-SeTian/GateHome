import { ref, reactive } from 'vue'

// Global UI State
const theme = ref(localStorage.getItem('gh_theme') || 'dark')
const isSidebarOpen = ref(false)
const isCmdkOpen = ref(false)

export function useAppStore() {
  const toggleTheme = () => {
    theme.value = theme.value === 'dark' ? 'light' : 'dark'
    localStorage.setItem('gh_theme', theme.value)
    document.body.className = `theme-${theme.value}`
  }

  const setTheme = (val) => {
    theme.value = val
    localStorage.setItem('gh_theme', val)
    document.body.className = `theme-${val}`
  }

  const toggleSidebar = () => {
    isSidebarOpen.value = !isSidebarOpen.value
  }

  const closeSidebar = () => {
    isSidebarOpen.value = false
  }

  const toggleCmdk = () => {
    isCmdkOpen.value = !isCmdkOpen.value
  }

  const openCmdk = () => {
    isCmdkOpen.value = true
  }

  const closeCmdk = () => {
    isCmdkOpen.value = false
  }

  // ================= 2. ROUTES (反代域名规则) =================
  const routes = ref([
    {
      id: 'rt_1',
      groupId: 'grp_internal',
      name: 'GateHome 核心控制台',
      domain: 'gate.internal.lan',
      target: 'http://127.0.0.1:16666',
      ssl: true,
      firewallId: 'fw_internal_only',
      auth: { enabled: true, username: 'admin', failureLimit: 5, freezeSeconds: 1800 },
      websocket: true,
      status: 'active',
      hitCount: '2.4M',
      latency: '1.2ms',
      icon: 'shield',
      category: 'System'
    },
    {
      id: 'rt_2',
      groupId: 'grp_default',
      name: 'Nextcloud 云盘服务',
      domain: 'cloud.nas.tech',
      target: 'http://192.168.2.110:80',
      ssl: true,
      firewallId: 'fw_china_only',
      auth: { enabled: false, username: '', failureLimit: 5, freezeSeconds: 1800 },
      websocket: false,
      status: 'active',
      hitCount: '890K',
      latency: '4.8ms',
      icon: 'hard-drive',
      category: 'Storage'
    },
    {
      id: 'rt_3',
      groupId: 'grp_default',
      name: 'Vaultwarden 密码库',
      domain: 'vault.nas.tech',
      target: 'http://192.168.2.120:8001',
      ssl: true,
      firewallId: 'fw_china_only',
      auth: { enabled: true, username: 'vault_admin', failureLimit: 3, freezeSeconds: 3600 },
      websocket: true,
      status: 'active',
      hitCount: '340K',
      latency: '2.1ms',
      icon: 'key',
      category: 'Security'
    },
    {
      id: 'rt_4',
      groupId: 'grp_default',
      name: 'HomeAssistant 智能家居',
      domain: 'home.iot.tech',
      target: 'http://192.168.2.150:8123',
      ssl: true,
      firewallId: 'fw_internal_only',
      auth: { enabled: false, username: '', failureLimit: 5, freezeSeconds: 1800 },
      websocket: true,
      status: 'active',
      hitCount: '1.8M',
      latency: '3.6ms',
      icon: 'home',
      category: 'IoT'
    },
    {
      id: 'rt_5',
      groupId: 'grp_default',
      name: 'Jellyfin 影音媒体中心',
      domain: 'media.stream.org',
      target: 'http://192.168.2.180:8096',
      ssl: true,
      firewallId: '',
      auth: { enabled: false, username: '', failureLimit: 5, freezeSeconds: 1800 },
      websocket: false,
      status: 'active',
      hitCount: '520K',
      latency: '6.2ms',
      icon: 'play',
      category: 'Media'
    },
    {
      id: 'rt_6',
      groupId: 'grp_internal',
      name: 'Gitea 代码托管仓库',
      domain: 'git.dev.lan',
      target: 'http://192.168.2.200:3000',
      ssl: true,
      firewallId: 'fw_internal_only',
      auth: { enabled: false, username: '', failureLimit: 5, freezeSeconds: 1800 },
      websocket: false,
      status: 'active',
      hitCount: '185K',
      latency: '2.4ms',
      icon: 'code',
      category: 'Dev'
    },
    {
      id: 'rt_7',
      groupId: 'grp_default',
      name: 'Plex 多媒体服务',
      domain: 'plex.nas.tech',
      target: 'http://192.168.2.185:32400',
      ssl: false,
      firewallId: '',
      auth: { enabled: false, username: '', failureLimit: 5, freezeSeconds: 1800 },
      websocket: true,
      status: 'inactive',
      hitCount: '42K',
      latency: '--',
      icon: 'tv',
      category: 'Media'
    },
    {
      id: 'rt_8',
      groupId: 'grp_internal',
      name: 'Prometheus 遥测采集',
      domain: 'monitor.ops.lan',
      target: 'http://127.0.0.1:9090',
      ssl: true,
      firewallId: 'fw_internal_only',
      auth: { enabled: true, username: 'prom_reader', failureLimit: 5, freezeSeconds: 1800 },
      websocket: false,
      status: 'active',
      hitCount: '6.7M',
      latency: '0.8ms',
      icon: 'activity',
      category: 'Ops'
    }
  ])

  // ================= 6. DDNS GROUPS (动态域名任务组) =================
  const ddnsTasks = ref([
    {
      id: 'ddns_cf_main',
      name: 'Cloudflare 个人根域名组',
      provider: 'Cloudflare',
      zone: 'nas.tech',
      hosts: ['@', 'cloud', 'vault', 'plex'],
      mode: 'dual', // 'ipv4' | 'ipv6' | 'dual'
      enabled: true,
      interval: 300,
      lastSync: '2 分钟前',
      status: 'synced',
      currentIpv4: '114.88.23.109',
      currentIpv6: '2408:8207:1800::4a91',
      message: 'IPv4 与 IPv6 解析记录已全部同步正常'
    },
    {
      id: 'ddns_aliyun',
      name: '阿里云 DNS 国内备份',
      provider: 'Aliyun 阿里云 DNS',
      zone: 'example.cn',
      hosts: ['gate', 'vpn'],
      mode: 'ipv4',
      enabled: true,
      interval: 600,
      lastSync: '12 分钟前',
      status: 'synced',
      currentIpv4: '114.88.23.109',
      currentIpv6: '',
      message: 'IPv4 解析记录匹配成功'
    }
  ])

  // ================= 8. CERTIFICATES (ACME SSL 证书) =================
  const certificates = ref([
    {
      id: 'cert_1',
      domain: '*.nas.tech, nas.tech',
      issuer: "Let's Encrypt Authority X3",
      expiresAt: '2026-12-28',
      daysLeft: 49,
      status: 'valid',
      autoRenew: true,
      keyType: 'ECDSA P-256'
    },
    {
      id: 'cert_2',
      domain: 'gate.internal.lan',
      issuer: 'GateHome Internal CA',
      expiresAt: '2027-10-15',
      daysLeft: 340,
      status: 'valid',
      autoRenew: true,
      keyType: 'RSA 4096'
    },
    {
      id: 'cert_3',
      domain: 'home.iot.tech',
      issuer: 'ZeroSSL Global TLS CA',
      expiresAt: '2026-11-20',
      daysLeft: 11,
      status: 'expiring_soon',
      autoRenew: true,
      keyType: 'ECDSA P-256'
    }
  ])

  // ================= 12. MCP & BACKUPS & SETTINGS =================
  const mcp = reactive({
    enabled: true,
    transport: 'SSE / Stream (Port 8089)',
    clients: [
      { id: 'c_1', name: 'Claude Desktop', ip: '127.0.0.1', connectedAt: '3 小时前', status: 'online' },
      { id: 'c_2', name: 'Cursor IDE', ip: '127.0.0.1', connectedAt: '1 小时前', status: 'online' },
      { id: 'c_3', name: 'Antigravity Studio', ip: '127.0.0.1', connectedAt: '刚刚', status: 'online' }
    ],
    tools: [
      { name: 'list_proxy_routes', desc: '获取所有反代组规则状态、端口与流量指标', calls: 342 },
      { name: 'create_proxy_route', desc: '为指定内网服务快捷创建反代路由与证书绑定', calls: 18 },
      { name: 'block_ip_address', desc: '将高危 IP 添加到全局冻结名单并阻断访问', calls: 56 },
      { name: 'get_system_telemetry', desc: '获取 CPU/内存/带宽/QPS 实时指标快照', calls: 1205 }
    ]
  })

  const backups = ref([
    { id: 'bk_1', name: 'gatehouse_backup_20261109_0300.zip', size: '14.2 MB', type: 'AUTO', storage: 'Local + WebDAV', createdAt: '2026-11-09 03:00' },
    { id: 'bk_2', name: 'gatehouse_pre_upgrade_v0.0.67.zip', size: '13.9 MB', type: 'MANUAL', storage: 'Local', createdAt: '2026-11-08 19:40' },
    { id: 'bk_3', name: 'gatehouse_backup_20261102_0300.zip', size: '13.6 MB', type: 'AUTO', storage: 'Local + WebDAV', createdAt: '2026-11-02 03:00' }
  ])

  const settings = reactive({
    adminPort: 16666, // GateHome 真实默认管理端口 16666!
    adminUsername: 'admin',
    adminAccess: {
      enabled: false,
      origins: ['https://gate.nas.tech']
    },
    outboundProxy: {
      enabled: false,
      url: 'http://192.168.2.1:7890',
      username: ''
    },
    logRetention: {
      maxSizeMb: 64,
      keepDays: 30
    },
    autoBackupEnabled: true,
    webdavUrl: 'https://dav.jianguoyun.com/dav/gatehome/',
    currentVersion: `v${__GATEHOME_VERSION__}`,
    channel: 'stable' // 'stable' | 'beta'
  })

  return {
    theme,
    toggleTheme,
    setTheme,
    isSidebarOpen,
    toggleSidebar,
    closeSidebar,
    isCmdkOpen,
    toggleCmdk,
    openCmdk,
    closeCmdk,
    routes,
    ddnsTasks,
    certificates,
    mcp,
    backups,
    settings
  }
}
