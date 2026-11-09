import { ref, reactive, computed } from 'vue'

// Global UI State
const theme = ref(localStorage.getItem('gh_theme') || 'dark')
const isSidebarOpen = ref(false)
const isCmdkOpen = ref(false)
const isAccountModalOpen = ref(false)

// Auth State (初次未登录时为 false，触发登录页拦截展示)
const isAuthenticated = ref(localStorage.getItem('gh_auth') === 'true')
const adminUser = reactive({
  username: localStorage.getItem('gh_user') || 'admin',
  role: '超级管理员 (Root)'
})

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

  const openAccountModal = () => {
    isAccountModalOpen.value = true
  }

  const closeAccountModal = () => {
    isAccountModalOpen.value = false
  }

  const login = async (username, password) => {
    if (!username || !username.trim()) {
      throw new Error('请输入管理员账号')
    }
    if (!password) {
      throw new Error('请输入管理密码')
    }
    // 模拟身份验证延迟
    await new Promise(r => setTimeout(r, 450))

    if (password.length < 6) {
      throw new Error('管理密码长度不足，请输入正确的密码')
    }

    isAuthenticated.value = true
    adminUser.username = username.trim()
    localStorage.setItem('gh_auth', 'true')
    localStorage.setItem('gh_user', username.trim())
    return { ok: true, username: adminUser.username }
  }

  const logout = () => {
    isAuthenticated.value = false
    localStorage.removeItem('gh_auth')
    isAccountModalOpen.value = false
  }

  const updateAdminPassword = async ({ currentPassword, newPassword, confirmPassword, username }) => {
    if (!currentPassword) {
      throw new Error('请输入当前管理密码以验证身份')
    }
    if (newPassword && newPassword !== confirmPassword) {
      throw new Error('两次输入的新密码不一致')
    }
    if (newPassword && (newPassword.length < 12 || newPassword.length > 72)) {
      throw new Error('新管理密码须为 12–72 字节')
    }
    await new Promise(r => setTimeout(r, 500))
    if (username) {
      adminUser.username = username
      localStorage.setItem('gh_user', username)
    }
    // 保存后所有管理员会话退出
    logout()
    return { ok: true, message: '管理账户已修改，请使用新凭据重新登录' }
  }

  // ================= 1. PROXY GROUPS (反代组) =================
  const proxyGroups = ref([
    {
      id: 'grp_default',
      name: '默认业务组 (公网对外)',
      enabled: true,
      httpPort: 80,
      httpsPort: 443,
      domainSuffix: '.nas.tech',
      ddnsGroupId: 'ddns_cf_main'
    },
    {
      id: 'grp_internal',
      name: '内部管理组 (局域网专用)',
      enabled: true,
      httpPort: 8080,
      httpsPort: 8443,
      domainSuffix: '.internal.lan',
      ddnsGroupId: ''
    }
  ])

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

  // ================= 3. IP SUBSCRIPTIONS (真实 IP/CIDR 规则订阅) =================
  const subscriptions = ref([
    {
      id: 'sub_cn_v4',
      name: '中国大陆 IPv4 地址段 (chnroute)',
      url: 'https://raw.githubusercontent.com/mayaxcn/china-ip-list/master/chnroute.txt',
      enabled: true,
      interval: 86400,
      count: 8624,
      usedCount: 2,
      ready: true,
      running: false,
      lastSuccess: '2026-11-09 03:00',
      message: '解析成功，共 8,624 条 CIDR'
    },
    {
      id: 'sub_cn_v6',
      name: '中国大陆 IPv6 地址段 (chnroute-v6)',
      url: 'https://raw.githubusercontent.com/mayaxcn/china-ip-list/master/chnroute-v6.txt',
      enabled: true,
      interval: 86400,
      count: 2140,
      usedCount: 1,
      ready: true,
      running: false,
      lastSuccess: '2026-11-09 03:01',
      message: '解析成功，共 2,140 条 CIDR'
    },
    {
      id: 'sub_threat_intel',
      name: '全网高危自动化恶意扫描器黑名单 (Blocklist.de)',
      url: 'https://lists.blocklist.de/lists/all.txt',
      enabled: true,
      interval: 43200,
      count: 14500,
      usedCount: 2,
      ready: true,
      running: false,
      lastSuccess: '2026-11-09 06:12',
      message: '解析成功，共 14,500 条高危来源'
    },
    {
      id: 'sub_cf_nodes',
      name: 'Cloudflare 边缘节点 IP 网段 (白名单直通)',
      url: 'https://www.cloudflare.com/ips-v4/',
      enabled: false,
      interval: 604800,
      count: 14,
      usedCount: 0,
      ready: true,
      running: false,
      lastSuccess: '2026-11-02 12:00',
      message: '已停用 · 缓存可用 (14 条)'
    }
  ])

  // ================= 4. FIREWALLS & WAF (防火墙与 Web 攻击防护) =================
  const firewalls = ref([
    {
      id: 'fw_china_only',
      name: '仅允许国内访问 + WAF 拦截',
      defaultAction: 'deny',
      groups: [
        {
          name: '中国大陆受信任访问组',
          match: 'include',
          action: 'allow',
          cidrs: ['192.168.0.0/16', '10.0.0.0/8'],
          subscriptions: ['sub_cn_v4', 'sub_cn_v6']
        },
        {
          name: '高危恶意扫描器阻断组',
          match: 'include',
          action: 'deny',
          cidrs: [],
          subscriptions: ['sub_threat_intel']
        }
      ],
      protection: {
        wafMode: 'block', // 'off' | 'detect' | 'block'
        wafLevel: 1,     // PL1 - PL4
        wafBodyKib: 1024,
        exceptions: [
          { ruleId: 942100, host: 'cloud.nas.tech', path: '/remote.php/dav' }
        ]
      }
    },
    {
      id: 'fw_internal_only',
      name: '纯内网访问控制策略',
      defaultAction: 'deny',
      groups: [
        {
          name: '本地内网私有网段',
          match: 'include',
          action: 'allow',
          cidrs: ['192.168.2.0/24', '127.0.0.1/32'],
          subscriptions: []
        }
      ],
      protection: {
        wafMode: 'detect',
        wafLevel: 1,
        wafBodyKib: 512,
        exceptions: []
      }
    }
  ])

  // ================= 5. IP BLOCKS (IP 冻结名单) =================
  const ipBlocks = ref([
    {
      id: 'blk_1',
      ip: '198.51.100.45',
      rule: 'grp_default/cloud.nas.tech',
      ruleName: 'Nextcloud 云盘服务',
      reason: '达到账号验证失败上限 (5次) · 自动冻结',
      until: '2026-11-09 15:28:10',
      remaining: '24 分钟',
      hits: 14,
      manual: false
    },
    {
      id: 'blk_2',
      ip: '203.0.113.88',
      rule: 'grp_default/vault.nas.tech',
      ruleName: 'Vaultwarden 密码库',
      reason: '触发 WAF 规则: SQL 注入 (CRS #942100)',
      until: '2026-11-16 14:26:00',
      remaining: '7 天',
      hits: 56,
      manual: false
    },
    {
      id: 'blk_3',
      ip: '185.220.101.5',
      rule: '全局阻断',
      ruleName: '全部反代规则',
      reason: '管理员手动永久冻结 · 恶意自动化扫描',
      until: '永久',
      remaining: '永久冻结',
      hits: 182,
      manual: true
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

  // ================= 7. PORT FORWARDS (端口转发) =================
  const forwards = ref([
    { id: 'fwd_1', name: 'MC 游戏专用服务器', protocol: 'tcp', listenPort: 25565, target: '192.168.2.105:25565', firewallId: 'fw_china_only', traffic: '48.2 GB', enabled: true },
    { id: 'fwd_2', name: 'WireGuard VPN 穿透', protocol: 'udp', listenPort: 51820, target: '192.168.2.1:51820', firewallId: '', traffic: '128.5 GB', enabled: true },
    { id: 'fwd_3', name: 'NAS SSH 调试端口', protocol: 'tcp', listenPort: 2222, target: '192.168.2.100:22', firewallId: 'fw_internal_only', traffic: '320 MB', enabled: true },
    { id: 'fwd_4', name: 'Git SSH 提交端口', protocol: 'tcp', listenPort: 2223, target: '192.168.2.200:22', firewallId: 'fw_internal_only', traffic: '1.8 GB', enabled: false }
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

  // ================= 9. LOGS (项目日志与反代访问日志双引擎) =================
  const accessLogs = ref([
    { id: 'log_1', time: '14:31:02', method: 'GET', status: 200, duration: 1.2, path: '/api/v1/telemetry', ip: '192.168.2.55', geo: '局域网 · 本地私网', route: 'GateHome 核心控制台', target: 'http://127.0.0.1:16666', ok: true },
    { id: 'log_2', time: '14:30:58', method: 'POST', status: 200, duration: 4.8, path: '/remote.php/dav/files', ip: '114.88.23.109', geo: '中国上海 · 电信', route: 'Nextcloud 云盘服务', target: 'http://192.168.2.110:80', ok: true },
    { id: 'log_3', time: '14:30:42', method: 'GET', status: 101, duration: 0.4, path: '/api/websocket', ip: '192.168.2.150', geo: '局域网 · IoT', route: 'HomeAssistant 智能家居', target: 'http://192.168.2.150:8123', ok: true },
    { id: 'log_4', time: '14:30:15', method: 'GET', status: 403, duration: 0.9, path: '/.env', ip: '45.154.255.8', geo: '德国 · 法兰克福', route: '默认拦截网关', target: 'WAF 阻断', ok: false },
    { id: 'log_5', time: '14:29:50', method: 'POST', status: 200, duration: 3.1, path: '/api/sync', ip: '192.168.2.120', geo: '局域网 · NAS', route: 'Vaultwarden 密码库', target: 'http://192.168.2.120:8001', ok: true },
    { id: 'log_6', time: '14:28:12', method: 'GET', status: 502, duration: 124.0, path: '/stream/live/ch1', ip: '192.168.2.185', geo: '局域网', route: 'Plex 多媒体服务', target: 'http://192.168.2.185:32400', ok: false }
  ])

  const projectLogs = ref([
    { id: 'pj_1', time: '14:30:00', category: 'ddns', action: 'DDNS 域名同步', target: 'Cloudflare nas.tech', ok: true, duration: 180, message: 'IPv4 与 IPv6 记录已匹配，无需变更' },
    { id: 'pj_2', time: '06:12:00', category: 'subscriptions', action: '拉取 IP 规则订阅', target: 'lists.blocklist.de', ok: true, duration: 920, message: '成功拉取 14,500 条高危 CIDR，已应用到防火墙组' },
    { id: 'pj_3', time: '03:00:00', category: 'subscriptions', action: '拉取 IP 规则订阅', target: 'mayaxcn/china-ip-list', ok: true, duration: 450, message: '成功拉取 8,624 条中国 IPv4 CIDR' },
    { id: 'pj_4', time: '02:00:15', category: 'certificates', action: 'ACME 证书检查', target: '*.nas.tech', ok: true, duration: 42, message: '证书有效期剩余 49 天，尚未达到 30 天续签阈值' },
    { id: 'pj_5', time: '01:00:00', category: 'admin', action: '自动日志轮换清理', target: 'log_storage', ok: true, duration: 12, message: '清理超过 30 天的历史调用日志 420 条' }
  ])

  // ================= 10. FIREWALL AUDIT LOGS (安全拦截事件) =================
  const firewallLogs = ref([
    { id: 'flog_1', time: '14:28:10', ip: '198.51.100.45', geo: '美国 · 俄勒冈', engine: 'CRS', rule: '敏感文件访问 (.env)', uri: '/.env', action: 'BLOCK', severity: 'HIGH' },
    { id: 'flog_2', time: '14:26:02', ip: '203.0.113.88', geo: '俄罗斯 · 莫斯科', engine: 'CRS', rule: 'SQL 注入攻击 (UNION SELECT)', uri: '/api/v1/user?id=1%20UNION%20SELECT', action: 'BLOCK', severity: 'CRITICAL' },
    { id: 'flog_3', time: '14:21:49', ip: '192.0.2.12', geo: '荷兰 · 阿姆斯特丹', engine: 'RATE', rule: '高频 CC 速率超限', uri: '/login', action: 'RATE_LIMIT', severity: 'MEDIUM' },
    { id: 'flog_4', time: '14:15:33', ip: '45.154.255.8', geo: '德国 · 法兰克福', engine: 'CRS', rule: '探测 wp-admin 路径', uri: '/wp-login.php', action: 'BLOCK', severity: 'LOW' }
  ])

  // ================= 11. TELEMETRY STATS =================
  const telemetry = reactive({
    cpu: 18.4,
    memory: 42.1,
    memoryUsed: '3.37 GB',
    memoryTotal: '8.00 GB',
    disk: 38.6,
    diskUsed: '189 GB',
    diskTotal: '490 GB',
    qps: 184,
    activeConns: 1420,
    rxRate: '6.84 MB/s',
    txRate: '14.20 MB/s',
    rxTotal: '1.42 TB',
    txTotal: '4.89 TB',
    uptime: '34d 18h 42m',
    routesActive: computed(() => routes.value.filter(r => r.status === 'active').length),
    routesTotal: computed(() => routes.value.length),
    wafBlockedToday: 1384,
    certExpiringCount: 1
  })

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
    isAuthenticated,
    adminUser,
    isAccountModalOpen,
    openAccountModal,
    closeAccountModal,
    login,
    logout,
    updateAdminPassword,
    proxyGroups,
    routes,
    subscriptions,
    firewalls,
    ipBlocks,
    ddnsTasks,
    forwards,
    certificates,
    accessLogs,
    projectLogs,
    firewallLogs,
    telemetry,
    mcp,
    backups,
    settings
  }
}
