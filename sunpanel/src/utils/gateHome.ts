import { withRouteImages } from '../../../shared/sunpanel-images'
export interface GateHomeRoute { id: string; title: string; group: string; url: string; lanUrl: string; image?: string; imageData?: string; imageUnavailable?: boolean }
export interface GateHomeLink { routeId: string; title: string; url: string; lanUrl: string; image?: string; icon?: Panel.ItemIcon }

export function routeLink(route: GateHomeRoute, icon?: Panel.ItemIcon): GateHomeLink {
  return { routeId: route.id, title: Array.from(route.title).slice(0, 20).join(''), url: route.url, lanUrl: route.lanUrl,
    ...(!route.imageUnavailable && route.image !== undefined ? { image: route.image, ...(icon ? { icon } : {}) } : {}) }
}

export function sameIcon(a?: Panel.ItemIcon | null, b?: Panel.ItemIcon | null) {
  return (a?.itemType || 0) === (b?.itemType || 0) && (a?.src || '') === (b?.src || '') && (a?.text || '') === (b?.text || '') && (a?.backgroundColor || '') === (b?.backgroundColor || '')
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

export function routeChanges(item: { title: string; url: string; lanUrl?: string; icon?: Panel.ItemIcon | null; gateHome?: GateHomeLink | null }, route: GateHomeRoute) {
  const previous = item.gateHome
  if (!previous || previous.routeId !== route.id)
    return []
  const next = routeLink(route)
  const changes: { field: 'title' | 'url' | 'lanUrl' | 'image'; label: string; current: string; next: string; preserved: boolean }[] = (['title', 'url', 'lanUrl'] as const).filter(field => previous[field] !== next[field]).map(field => ({
    field,
    label: { title: '名称', url: '默认网址', lanUrl: '内网地址' }[field],
    current: item[field] || '',
    next: next[field],
    preserved: (item[field] || '') !== previous[field],
  }))
  if (next.image !== undefined && next.image !== (previous.image || '')) changes.push({ field: 'image', label: '图标', current: item.icon?.src || '', next: route.imageData || '', preserved: !sameIcon(item.icon, previous.icon) })
  return changes
}

export class GateHomeLoginRequired extends Error {}

export async function loadGateHomeRoutes(signal?: AbortSignal): Promise<GateHomeRoute[]> {
  const response = await fetch('/api/sunpanel/routes', { credentials: 'same-origin', signal })
  if (response.status === 401)
    throw new GateHomeLoginRequired('请连接 GateHome 管理页后读取反代。')
  if (!response.ok)
    throw new Error('无法读取反代配置，请稍后重试。')
  return withRouteImages(await response.json(), signal)
}

export function openGateHomeConnection(origin: string) {
  const target = new URL(origin)
  if (!['http:', 'https:'].includes(target.protocol) || target.origin !== origin)
    throw new Error('GateHome 管理入口无效。')
  const state = Array.from(crypto.getRandomValues(new Uint8Array(16)), byte => byte.toString(16).padStart(2, '0')).join('')
  const query = new URLSearchParams({ origin: window.location.origin, state })
  const popup = window.open(`${origin}/console/#/sunpanel-connect?${query}`, '_blank', 'popup,width=540,height=680')
  if (!popup)
    throw new Error('浏览器拦截了连接窗口，请允许弹出窗口后重试。')
  let cancel = () => {}
  const promise = new Promise<GateHomeRoute[]>((resolve, reject) => {
    const started = Date.now()
    const finish = (error?: Error, routes?: GateHomeRoute[]) => {
      window.removeEventListener('message', message)
      clearInterval(timer)
      popup.close()
      if (error) reject(error)
      else resolve(routes!)
    }
    const message = (event: MessageEvent) => {
      if (event.origin !== origin || event.source !== popup || event.data?.type !== 'gatehome:sunpanel-routes' || event.data.state !== state || !Array.isArray(event.data.routes)) return
      const fields = ['id', 'title', 'group', 'url', 'lanUrl'] as const
      if (!event.data.routes.every((row: GateHomeRoute) => row && fields.every(field => typeof row[field] === 'string'))) return
      if (!event.data.routes.every((row: GateHomeRoute) => (!row.imageData || (/^data:image\/png;base64,[A-Za-z0-9+/]+=*$/.test(row.imageData) && row.imageData.length <= 128 * 1024)) && (row.image === undefined || typeof row.image === 'string'))) return
      finish(undefined, event.data.routes.map((row: GateHomeRoute) => ({ ...Object.fromEntries(fields.map(field => [field, row[field]])),
        ...(row.image !== undefined ? { image: row.image } : {}), ...(row.imageData ? { imageData: row.imageData } : {}), ...(row.imageUnavailable ? { imageUnavailable: true } : {}) }) as unknown as GateHomeRoute))
    }
    const timer = setInterval(() => {
      if (popup.closed) finish(new Error('连接窗口已关闭，请重新连接。'))
      else if (Date.now() - started > 120000) finish(new Error('连接超时，请检查 GateHome 登录状态后重试。'))
    }, 400)
    cancel = () => finish(new DOMException('连接已取消', 'AbortError'))
    window.addEventListener('message', message)
  })
  return { promise, cancel }
}
