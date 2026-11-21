import { routeAccessURL, routeKey } from './configuration.js'

export const SEARCH_CATEGORIES = [
  { id: 'all', label: '全部' },
  { id: 'pages', label: '页面' },
  { id: 'services', label: '反代与转发' },
  { id: 'network', label: '网络与安全' },
  { id: 'notifications', label: '通知' },
  { id: 'actions', label: '操作' }
]

export const GROUP_TITLES = {
  pages: '快捷页面导航',
  services: '反代与端口转发',
  network: '网络、证书与安全策略',
  notifications: '通知与告警记录',
  actions: '常用操作指令'
}

const NOTICE_CATEGORY_LABELS = {
  upstream: '反向代理',
  backup: '备份与恢复',
  cert: 'SSL 证书',
  certificate: 'SSL 证书',
  waf: '安全防护',
  ddns: '动态域名',
  mcp: 'AI 管理',
  system: '系统通知'
}

function formatNoticeTitle(notice) {
  if (notice.title) return notice.title
  const catLabel = NOTICE_CATEGORY_LABELS[notice.category] || '系统通知'
  const type = notice.type === 'warn' ? 'warning' : notice.type
  const statusLabel =
    type === 'error' ? '告警' : type === 'warning' ? '预警' : type === 'success' ? '已恢复' : '提醒'
  return `${catLabel}${statusLabel}`
}

const GROUP_ORDER = ['pages', 'services', 'network', 'notifications', 'actions']

export function buildSearchItems({ config = null, notifications = [], pages = [], actions = [] } = {}) {
  const items = []

  for (const page of pages) {
    items.push({
      id: `page:${page.id || page.path}`,
      group: 'pages',
      kind: 'page',
      title: page.title,
      subtitle: page.subtitle || '',
      meta: typeof page.path === 'string' ? page.path : '',
      badge: page.badge || '',
      tone: page.tone || 'neutral',
      icon: page.icon,
      to: page.to || page.path,
      keywords: page.keywords || []
    })
  }

  const groups = config?.groups ?? []
  const firewalls = config?.firewalls ?? []

  for (const route of config?.routes ?? []) {
    const group = groups.find((g) => g.id === route.group_id)
    const accessURL = routeAccessURL(route, group)
    const id = route.id || routeKey(route)
    const displayName = route.name ? `${route.name} (${route.host})` : route.host
    items.push({
      id: `route:${id}`,
      group: 'services',
      kind: 'route',
      title: displayName || '未命名反代服务',
      subtitle: `${accessURL || route.host} → ${route.upstream}${group?.name ? ` · ${group.name}` : ''}`,
      meta: '',
      badge: !route.enabled ? '已停用' : route.tls ? 'HTTPS' : 'HTTP',
      tone: !route.enabled ? 'neutral' : 'ok',
      to: { path: '/routes', query: { q: route.host || route.name } },
      keywords: [
        route.name,
        route.host,
        route.upstream,
        accessURL,
        group?.name,
        route.tls ? 'https ssl tls' : 'http',
        route.enabled ? '已启用 运行中' : '已停用 禁用',
        '反向代理 反代 路由 服务 route proxy'
      ]
    })
  }

  for (const group of groups) {
    const ports = [
      group.http_port ? `HTTP :${group.http_port}` : '',
      group.https_port ? `HTTPS :${group.https_port}` : ''
    ]
      .filter(Boolean)
      .join(' / ')
    items.push({
      id: `group:${group.id}`,
      group: 'services',
      kind: 'group',
      title: `反代组 · ${group.name}`,
      subtitle: [group.domain_suffix ? `*.${group.domain_suffix}` : '自定义域名', ports]
        .filter(Boolean)
        .join(' · '),
      meta: '',
      badge: group.enabled ? '反代组' : '已停用',
      tone: group.enabled ? 'brand' : 'neutral',
      to: { path: '/routes', query: { group: group.id } },
      keywords: [
        group.name,
        group.domain_suffix,
        String(group.http_port || ''),
        String(group.https_port || ''),
        '反代组 代理组 监听端口 proxy group'
      ]
    })
  }

  for (const forward of config?.forwards ?? []) {
    const protocol = (forward.protocol || 'tcp').toUpperCase()
    const fw = firewalls.find((f) => f.id === forward.firewall_id)
    items.push({
      id: `forward:${forward.id || forward.listen_port}`,
      group: 'services',
      kind: 'forward',
      title: forward.name || `端口转发 :${forward.listen_port}`,
      subtitle: `${protocol} :${forward.listen_port} → ${forward.upstream}${fw?.name ? ` · ${fw.name}` : ''}`,
      meta: '',
      badge: forward.enabled ? protocol : '已停用',
      tone: forward.enabled ? 'ok' : 'neutral',
      to: { path: '/forwards', query: { q: String(forward.listen_port || forward.name) } },
      keywords: [
        forward.name,
        String(forward.listen_port || ''),
        `:${forward.listen_port}`,
        forward.upstream,
        protocol,
        fw?.name,
        forward.enabled ? '已启用 监听' : '已停用',
        '端口转发 转发 映射 tcp udp forward'
      ]
    })
  }

  for (const ddns of config?.ddns?.groups ?? []) {
    const hosts = (ddns.hosts ?? []).join(', ')
    items.push({
      id: `ddns:${ddns.id || ddns.name || ddns.zone}`,
      group: 'network',
      kind: 'ddns',
      title: `DDNS · ${ddns.name || ddns.zone || '动态域名'}`,
      subtitle: [ddns.provider, ddns.zone, hosts].filter(Boolean).join(' · '),
      meta: '/ddns',
      badge: ddns.enabled ? (ddns.mode || 'DDNS').toUpperCase() : '已停用',
      tone: ddns.enabled ? 'ok' : 'neutral',
      to: '/ddns',
      keywords: [
        ddns.name,
        ddns.provider,
        ddns.zone,
        ...(ddns.hosts ?? []),
        ddns.mode,
        '动态域名 ddns dns 域名解析'
      ]
    })
  }

  for (const cert of config?.acme?.requests ?? []) {
    const domains = (cert.domains ?? []).join(', ')
    items.push({
      id: `cert:${cert.id || domains}`,
      group: 'network',
      kind: 'certificate',
      title: `SSL 证书 · ${domains || cert.id}`,
      subtitle: `ACME 证书管理 · 服务商: ${cert.provider || '默认'}`,
      meta: '/certificates',
      badge: cert.enabled ? '自动续期' : '已停用',
      tone: cert.enabled ? 'ok' : 'neutral',
      to: '/certificates',
      keywords: [
        ...(cert.domains ?? []),
        cert.provider,
        cert.id,
        'ssl 证书 https tls acme 续期 certificate'
      ]
    })
  }

  for (const fw of firewalls) {
    const ruleGroups = (fw.groups ?? []).map((g) => g.name).filter(Boolean)
    const cidrs = (fw.groups ?? []).flatMap((g) => g.cidrs ?? [])
    const wafMode = fw.protection?.waf?.mode
    items.push({
      id: `firewall:${fw.id}`,
      group: 'network',
      kind: 'firewall',
      title: `防火墙策略 · ${fw.name}`,
      subtitle: `默认${fw.default_action === 'deny' ? '拦截' : '放行'} · ${(fw.groups ?? []).length} 个来源规则组${
        wafMode && wafMode !== 'off' ? ` · WAF (${wafMode})` : ''
      }`,
      meta: '/access',
      badge: wafMode && wafMode !== 'off' ? 'WAF 开启' : '防火墙',
      tone: 'brand',
      to: '/access',
      keywords: [
        fw.name,
        ...ruleGroups,
        ...cidrs,
        wafMode,
        '防火墙 安全策略 waf 访问控制 白名单 黑名单 firewall'
      ]
    })
  }

  for (const sub of config?.subscriptions ?? []) {
    items.push({
      id: `sub:${sub.id || sub.name}`,
      group: 'network',
      kind: 'subscription',
      title: `远程订阅 · ${sub.name}`,
      subtitle: sub.url || '',
      meta: '/subscriptions',
      badge: sub.enabled ? `每 ${sub.interval || 24}h` : '已停用',
      tone: sub.enabled ? 'ok' : 'neutral',
      to: '/subscriptions',
      keywords: [sub.name, sub.url, '订阅 远程订阅 cidr ip列表 subscription']
    })
  }

  for (const notice of (notifications ?? []).slice(0, 30)) {
    const type = notice.type === 'warn' ? 'warning' : notice.type
    const tone =
      type === 'error' ? 'bad' : type === 'warning' ? 'warn' : type === 'success' ? 'ok' : 'neutral'
    const baseTitle = formatNoticeTitle(notice)
    const catLabel = NOTICE_CATEGORY_LABELS[notice.category] || '系统通知'
    const searchTarget = notice.resource || String(notice.message || '').slice(0, 24).trim()
    items.push({
      id: `notice:${notice.id}`,
      group: 'notifications',
      kind: 'notification',
      title: notice.resource ? `${baseTitle} · ${notice.resource}` : baseTitle,
      subtitle: notice.message || '',
      meta: '',
      badge: notice.unread ? '未读' : '记录',
      tone,
      to: searchTarget ? { path: '/notifications', query: { q: searchTarget } } : '/notifications',
      keywords: [
        baseTitle,
        catLabel,
        notice.resource,
        notice.message,
        notice.category,
        notice.unread ? '未读' : '已读',
        '通知 告警 故障 异常 恢复 notification alert'
      ]
    })
  }

  for (const action of actions) {
    items.push({
      id: `action:${action.id}`,
      group: 'actions',
      kind: 'action',
      title: action.title,
      subtitle: action.subtitle || '',
      meta: '',
      badge: action.shortcut || 'Action',
      tone: 'action',
      icon: action.icon,
      run: action.run,
      keywords: action.keywords || []
    })
  }

  return items
}

function itemSearchText(item) {
  return [
    item.title,
    item.subtitle,
    item.meta,
    item.badge,
    ...(item.keywords ?? [])
  ]
    .filter(Boolean)
    .join(' ')
    .toLowerCase()
}

function scoreItem(item, tokens, rawQuery) {
  const title = (item.title || '').toLowerCase()
  const subtitle = (item.subtitle || '').toLowerCase()
  const meta = (item.meta || '').toLowerCase()
  const text = itemSearchText(item)

  for (const token of tokens) {
    if (!text.includes(token)) return -1
  }

  let score = 10
  if (title === rawQuery) score += 100
  else if (title.startsWith(rawQuery)) score += 70
  else if (title.includes(rawQuery)) score += 45
  if (subtitle.includes(rawQuery)) score += 25
  if (meta.includes(rawQuery)) score += 15
  for (const token of tokens) {
    if (title.includes(token)) score += 10
    if (subtitle.includes(token)) score += 5
  }
  return score
}

export function searchGlobalItems(items, { query = '', category = 'all' } = {}) {
  const rawQuery = String(query || '').trim().toLowerCase()
  const tokens = rawQuery ? rawQuery.split(/\s+/).filter(Boolean) : []

  const counts = {
    all: 0,
    pages: 0,
    services: 0,
    network: 0,
    notifications: 0,
    actions: 0
  }

  const matchedWithScore = []

  for (const item of items) {
    if (!tokens.length) {
      counts[item.group] = (counts[item.group] || 0) + 1
      counts.all++
      matchedWithScore.push({ item, score: 0 })
      continue
    }
    const score = scoreItem(item, tokens, rawQuery)
    if (score >= 0) {
      counts[item.group] = (counts[item.group] || 0) + 1
      counts.all++
      matchedWithScore.push({ item, score })
    }
  }

  const filtered = matchedWithScore.filter(({ item }) => {
    if (category && category !== 'all') return item.group === category
    // When query is empty in 'all' view, keep the default list clean: Pages + top 6 Services + Actions
    if (!tokens.length) {
      return item.group === 'pages' || item.group === 'services' || item.group === 'actions'
    }
    return true
  })

  const groups = []
  const flatItems = []

  for (const groupKey of GROUP_ORDER) {
    let groupEntries = filtered.filter(({ item }) => item.group === groupKey)
    if (!groupEntries.length) continue
    if (tokens.length) {
      groupEntries.sort((a, b) => b.score - a.score)
    } else if (category === 'all' && groupKey === 'services') {
      groupEntries = groupEntries.slice(0, 6)
    }
    const groupItems = groupEntries.map(({ item }) => {
      const flatIndex = flatItems.length
      const enriched = { ...item, flatIndex }
      flatItems.push(enriched)
      return enriched
    })
    groups.push({
      key: groupKey,
      title: GROUP_TITLES[groupKey] || groupKey,
      items: groupItems
    })
  }

  return {
    groups,
    flatItems,
    counts
  }
}
