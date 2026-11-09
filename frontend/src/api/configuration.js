export function routeKey(route) {
  return `${route.group_id}/${route.host}`
}

export function routeHost(value, group) {
  const host = value.trim().toLowerCase().replace(/\.$/, '')
  const suffix = group?.domain_suffix
  if (!suffix || host.includes('.')) return host
  return host === '@' ? suffix : `${host}.${suffix}`
}

export function replaceConfigItem(snapshot, collection, item, originalId) {
  const config = structuredClone(snapshot.config)
  const items = config[collection] ?? []
  if (originalId == null) {
    config[collection] = [...items, item]
    return config
  }
  const index = items.findIndex((existing) => (existing.id || routeKey(existing)) === originalId)
  if (index < 0) throw new Error('原配置项已变化，请重新载入后编辑')
  items[index] = item
  return config
}

export function forwardState(rule, listeners) {
  if (listeners === undefined) return { label: '正在读取监听状态', tone: 'neutral' }
  const running = (listeners ?? []).find((item) => item.id === rule.id && item.enabled)
  if (!rule.enabled)
    return running ? { label: '停用待重启', tone: 'warn' } : { label: '已停用', tone: 'neutral' }
  if (!running) return { label: '启用待重启', tone: 'warn' }
  const changed = ['protocol', 'listen_port', 'upstream', 'firewall_id'].some(
    (key) => rule[key] !== running[key]
  )
  const timeoutChanged =
    rule.protocol !== 'tcp' && (rule.udp_idle_seconds || 300) !== (running.udp_idle_seconds || 300)
  return changed || timeoutChanged ? { label: '修改待重启', tone: 'warn' } : { label: '正在监听', tone: 'ok' }
}
