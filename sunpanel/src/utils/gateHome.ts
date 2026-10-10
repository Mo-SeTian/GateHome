export interface GateHomeRoute { id: string; title: string; group: string; url: string; lanUrl: string }
export interface GateHomeLink { routeId: string; title: string; url: string; lanUrl: string }

export function routeLink(route: GateHomeRoute): GateHomeLink {
  return { routeId: route.id, title: Array.from(route.title).slice(0, 20).join(''), url: route.url, lanUrl: route.lanUrl }
}

export function canonicalSite(raw: string) {
  try {
    const url = new URL(raw.trim())
    url.hash = ''
    return `${url.protocol}//${url.host}${url.pathname.replace(/\/+$/, '')}${url.search}`
  }
  catch { return raw }
}

export function isImported(route: GateHomeRoute, items: { url: string; gateHome?: GateHomeLink | null }[]) {
  return items.some(item => item.gateHome?.routeId === route.id || canonicalSite(item.url) === canonicalSite(route.url))
}

export function routeChanges(item: { title: string; url: string; lanUrl?: string; gateHome?: GateHomeLink | null }, route: GateHomeRoute) {
  const previous = item.gateHome
  if (!previous || previous.routeId !== route.id)
    return []
  const next = routeLink(route)
  return (['title', 'url', 'lanUrl'] as const).filter(field => previous[field] !== next[field]).map(field => ({
    field,
    label: { title: '名称', url: '默认网址', lanUrl: '内网地址' }[field],
    current: item[field] || '',
    next: next[field],
    preserved: (item[field] || '') !== previous[field],
  }))
}

export async function loadGateHomeRoutes(): Promise<GateHomeRoute[]> {
  const response = await fetch('/api/sunpanel/routes', { credentials: 'same-origin' })
  if (!response.ok)
    throw new Error(response.status === 401 ? '请先在当前浏览器登录 GateHome 管理页，再点击刷新。' : '无法读取反代配置，请稍后重试。')
  return response.json()
}
