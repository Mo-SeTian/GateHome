import { routeKey } from './configuration.js'

export const LOG_WHITELIST_GROUP_NAME = '日志快速加白'

const IPV4_RE = /^(?:(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\.){3}(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)$/
const IPV6_RE = /^[0-9a-fA-F:]+$/

export function normalizeIPAddress(raw) {
  const value = String(raw || '').trim()
  if (!value || value === '未知') return ''
  if (IPV4_RE.test(value)) return value
  if (value.includes(':') && value.length <= 45 && IPV6_RE.test(value)) {
    return value.toLowerCase()
  }
  return ''
}

export function ipToSingleCIDR(raw) {
  const ip = normalizeIPAddress(raw)
  if (!ip) return ''
  return ip.includes(':') ? `${ip}/128` : `${ip}/32`
}

export function canManageLogIP(entry, config) {
  if (!entry || !config?.routes?.length) return false
  if (!normalizeIPAddress(entry.remote)) return false
  return Boolean(entry.rule && config.routes.some((route) => routeKey(route) === entry.rule))
}

export function routeForLogEntry(entry, config) {
  if (!entry?.rule || !config?.routes?.length) return null
  return config.routes.find((route) => routeKey(route) === entry.rule) || null
}

export function routeFirewallForEntry(entry, config) {
  const route = routeForLogEntry(entry, config)
  if (!route?.firewall_id || !config?.firewalls?.length) return null
  return config.firewalls.find((fw) => fw.id === route.firewall_id) || null
}

export function isIPAllowlistedInFirewall(firewall, rawIP) {
  const ip = normalizeIPAddress(rawIP)
  const cidr = ipToSingleCIDR(ip)
  if (!firewall || !ip) return false
  for (const group of firewall.groups ?? []) {
    if (group.action !== 'allow') {
      // A deny rule precedes any later allow rule, so top-level allowlisting is not guaranteed.
      break
    }
    if (group.match !== 'include') continue
    if ((group.cidrs ?? []).some((item) => {
      const normalized = String(item || '').trim().toLowerCase()
      return normalized === ip || normalized === cidr
    })) {
      return true
    }
  }
  return false
}

export function applyFirewallWhitelist(snapshot, firewallId, rawIP) {
  const ip = normalizeIPAddress(rawIP)
  const cidr = ipToSingleCIDR(ip)
  if (!ip || !cidr) {
    throw new Error('来源 IP 格式无效，无法加入白名单')
  }
  if (!snapshot?.config) {
    throw new Error('当前网关配置尚未加载完成，请稍后重试')
  }
  const config = structuredClone(snapshot.config)
  const firewall = (config.firewalls ?? []).find((item) => item.id === firewallId)
  if (!firewall) {
    throw new Error('该反代规则绑定的防火墙策略不存在，请刷新后重试')
  }
  firewall.groups = Array.isArray(firewall.groups) ? firewall.groups : []
  if (isIPAllowlistedInFirewall(firewall, ip)) {
    return { changed: false, config, cidr, firewallName: firewall.name }
  }

  let targetGroup = firewall.groups[0]
  const canReuseFirst =
    targetGroup &&
    targetGroup.name === LOG_WHITELIST_GROUP_NAME &&
    targetGroup.match === 'include' &&
    targetGroup.action === 'allow'

  if (!canReuseFirst) {
    if (firewall.groups.length >= 100) {
      throw new Error(`防火墙「${firewall.name}」的规则组数量已达上限（100 组），无法自动创建白名单组`)
    }
    targetGroup = {
      name: LOG_WHITELIST_GROUP_NAME,
      match: 'include',
      action: 'allow',
      cidrs: [],
      subscriptions: []
    }
    firewall.groups.unshift(targetGroup)
  }

  targetGroup.cidrs = Array.isArray(targetGroup.cidrs) ? targetGroup.cidrs : []
  if (targetGroup.cidrs.length >= 200) {
    throw new Error(`「${LOG_WHITELIST_GROUP_NAME}」组的手动 IP 条目已达上限（200 个），请前往防火墙管理整理`)
  }
  targetGroup.cidrs.push(cidr)
  return { changed: true, config, cidr, firewallName: firewall.name }
}
