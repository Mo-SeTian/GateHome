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

export function firewallDraft(firewall) {
  const draft = structuredClone(firewall)
  draft.groups = (draft.groups ?? []).map((group) => ({
    ...group, cidrsText: (group.cidrs ?? []).join('\n'), subscriptions: group.subscriptions ?? []
  }))
  const protection = draft.protection ?? {}
  const waf = protection.waf ?? {}
  const freeze = protection.freeze ?? {}
  draft.protection = {
    ...protection,
    waf: { ...waf, mode: waf.mode || 'off', level: waf.level || 1, exceptions: waf.exceptions ?? [] },
    rate: {
      ...protection.rate, enabled: !!protection.rate?.enabled, requests: protection.rate?.requests || 120,
      window_seconds: protection.rate?.window_seconds || 60, path: protection.rate?.path || '/'
    },
    freeze: { ...freeze, enabled: !!freeze.enabled, failures: freeze.failures || 5, window_seconds: freeze.window_seconds || 300 },
    rules: protection.rules ?? []
  }
  draft.bodyKiB = (waf.body_limit || 1048576) / 1024
  draft.freezeMinutes = (freeze.seconds || 3600) / 60
  return draft
}

export function firewallRecord(draft) {
  const { bodyKiB, freezeMinutes, ...record } = structuredClone(draft)
  record.name = record.name.trim()
  record.groups = record.groups.map(({ cidrsText, ...group }) => ({
    ...group, name: group.name.trim(), cidrs: cidrsText.split(/[\n,]+/).map((item) => item.trim()).filter(Boolean)
  }))
  record.protection.waf.body_limit = Number(bodyKiB) * 1024
  record.protection.waf.exceptions = record.protection.waf.exceptions.map((exception) => ({
    ...exception, host: exception.host.trim().toLowerCase(), path: exception.path.trim(),
    parameter: exception.parameter.trim()
  }))
  record.protection.freeze.seconds = Number(freezeMinutes) * 60
  record.protection.rate.path = record.protection.rate.path.trim()
  record.protection.rules = record.protection.rules.map((rule) => ({ ...rule, name: rule.name.trim() }))
  return record
}
