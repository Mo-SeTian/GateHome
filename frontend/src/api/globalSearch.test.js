import assert from 'node:assert/strict'
import test from 'node:test'
import { buildSearchItems, searchGlobalItems } from './globalSearch.js'

const sampleConfig = {
  groups: [
    { id: 'main', name: '主力反代组', enabled: true, http_port: 18080, https_port: 18443, domain_suffix: 'home.arpa' }
  ],
  routes: [
    {
      id: 'r-jellyfin',
      group_id: 'main',
      name: 'Jellyfin 影音',
      host: 'jellyfin.home.arpa',
      upstream: 'http://192.168.1.10:8096',
      enabled: true,
      tls: true
    },
    {
      id: 'r-nas',
      group_id: 'main',
      name: '群晖 DSM',
      host: 'nas.home.arpa',
      upstream: 'http://192.168.1.10:5000',
      enabled: false,
      tls: false
    }
  ],
  forwards: [
    {
      id: 'f-ssh',
      name: '内网 SSH 转发',
      protocol: 'tcp',
      listen_port: 22022,
      upstream: '192.168.1.10:22',
      firewall_id: 'fw-lan',
      enabled: true
    },
    {
      id: 'f-dns',
      name: 'AdGuard UDP',
      protocol: 'udp',
      listen_port: 5353,
      upstream: '192.168.1.2:53',
      enabled: true
    }
  ],
  ddns: {
    groups: [
      { id: 'd-cf', name: 'Cloudflare 主域名', provider: 'cloudflare', zone: 'example.com', hosts: ['@', 'nas'], mode: 'dual', enabled: true }
    ]
  },
  acme: {
    requests: [
      { id: 'c-wildcard', provider: 'cloudflare', domains: ['*.example.com', 'example.com'], enabled: true }
    ]
  },
  firewalls: [
    {
      id: 'fw-lan',
      name: '局域网与可信订阅',
      default_action: 'deny',
      groups: [{ name: '家庭局域网', cidrs: ['192.168.1.0/24'] }],
      protection: { waf: { mode: 'block' } }
    }
  ],
  subscriptions: [
    { id: 'sub-cf', name: 'Cloudflare 回源 IP', url: 'https://www.cloudflare.com/ips-v4', interval: 12, enabled: true }
  ]
}

const sampleNotifications = [
  {
    id: 'evt-1',
    resource: 'jellyfin.home.arpa',
    message: '上游连接超时 (dial tcp 192.168.1.10:8096: i/o timeout)',
    type: 'error',
    category: 'upstream',
    unread: true
  }
]

const samplePages = [
  { title: '概览 (Overview)', path: '/overview', keywords: ['dashboard', '首页'] },
  { title: '反向代理 (Routes)', path: '/routes', keywords: ['proxy', '反代'] },
  { title: '端口转发 (Forwards)', path: '/forwards', keywords: ['tcp', 'udp'] }
]

const sampleActions = [
  { id: 'act_new_route', title: '新建反向代理路由', shortcut: '⌘N' }
]

test('global search indexes pages, routes, forwards, network policies, notifications and actions', () => {
  const items = buildSearchItems({
    config: sampleConfig,
    notifications: sampleNotifications,
    pages: samplePages,
    actions: sampleActions
  })

  // Page search keeps Overview first when searching '概览'
  const overviewRes = searchGlobalItems(items, { query: '概览' })
  assert.equal(overviewRes.flatItems[0].to, '/overview')

  // Multi-token search matches reverse proxy route by name + port
  const routeRes = searchGlobalItems(items, { query: 'jellyfin 8096' })
  assert.ok(routeRes.flatItems.some((item) => item.kind === 'route' && item.title.includes('Jellyfin')))
  const matchedRoute = routeRes.flatItems.find((item) => item.kind === 'route')
  assert.deepEqual(matchedRoute.to, {
    path: '/routes',
    query: { q: 'jellyfin.home.arpa' }
  })

  // Port forward search matches listen port and protocol
  const forwardRes = searchGlobalItems(items, { query: 'udp 5353' })
  assert.equal(forwardRes.flatItems.length, 1)
  assert.equal(forwardRes.flatItems[0].kind, 'forward')
  assert.deepEqual(forwardRes.flatItems[0].to, {
    path: '/forwards',
    query: { q: '5353' }
  })

  // Network & security search finds firewall by CIDR or WAF keyword
  const fwRes = searchGlobalItems(items, { query: '192.168.1.0/24' })
  assert.equal(fwRes.flatItems.length, 1)
  assert.equal(fwRes.flatItems[0].kind, 'firewall')

  // Category filter scopes results to notifications only and derives category title
  const noticeRes = searchGlobalItems(items, { query: 'jellyfin', category: 'notifications' })
  assert.equal(noticeRes.flatItems.length, 1)
  assert.equal(noticeRes.flatItems[0].kind, 'notification')
  assert.equal(noticeRes.flatItems[0].title, '反向代理告警 · jellyfin.home.arpa')
  assert.equal(noticeRes.counts.services, 1)
  assert.equal(noticeRes.counts.notifications, 1)
})
