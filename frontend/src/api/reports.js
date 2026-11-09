export function logQuery(scope, filters, page, size, through) {
  const keys = ['result', 'from', 'to', 'search']
  if (scope === 'project') keys.push('category')
  else keys.push('rule', 'ip', 'method', 'status')
  if (scope === 'firewall' || scope === 'security') keys.push('firewall', 'engine', 'decision', 'rule_id')
  const query = { scope, page, size, through }
  for (const key of keys) {
    if (!filters[key]) continue
    if (key === 'from' || key === 'to') {
      const time = new Date(filters[key])
      if (!Number.isFinite(time.getTime())) throw new Error('请输入有效的开始与结束时间')
      query[key] = time.toISOString()
    } else query[key] = filters[key]
  }
  if (query.from && query.to && Date.parse(query.from) > Date.parse(query.to))
    throw new Error('开始时间不能晚于结束时间')
  return query
}

export function trafficQuery(form) {
  if (form.range !== 'custom') return { rule: form.rule, hours: Number(form.range) }
  const from = new Date(form.from), to = new Date(form.to)
  if (!form.from || !form.to || !Number.isFinite(from.getTime()) || !Number.isFinite(to.getTime()) || from >= to)
    throw new Error('请选择有效的起止时间，结束时间须晚于开始时间')
  return { rule: form.rule, from: from.toISOString(), to: to.toISOString() }
}

export function subscriptionState(subscription, status) {
  if (!subscription.enabled) return { label: '已停用', tone: 'badge-neutral' }
  if (!status) return { label: '正在读取状态', tone: 'badge-neutral' }
  if (status.running) return { label: '正在拉取', tone: 'badge-brand' }
  const lastRun = Date.parse(status.last_run), lastSuccess = Date.parse(status.last_success)
  if (status.last_run && !status.last_run.startsWith('0001') && (!status.ready || lastRun > lastSuccess))
    return { label: status.ready ? '更新失败，使用缓存' : '拉取失败，暂无缓存', tone: 'badge-warn' }
  return status.ready ? { label: '规则可用', tone: 'badge-ok' } : { label: '等待首次更新', tone: 'badge-neutral' }
}

export function reportDate(value) {
  return !value || value.startsWith('0001') ? '—' : new Date(value).toLocaleString()
}

export function formatBytes(value) {
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let amount = value, unit = 0
  while (amount >= 1024 && unit < units.length - 1) { amount /= 1024; unit++ }
  return `${amount.toLocaleString(undefined, { maximumFractionDigits: unit ? 2 : 0 })} ${units[unit]}`
}

export function engineLabel(engine) {
  return { ip: 'IP 规则', waf: 'CRS', custom: 'HTTP 规则', rate: '访问限速', system: '系统保护', auth: '账号验证', freeze: 'IP 冻结' }[engine] || engine
}
