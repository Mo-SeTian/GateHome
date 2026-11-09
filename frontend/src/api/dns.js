export function domainList(value) {
  return value.split(/[\n,]+/).map(item => item.trim().toLowerCase()).filter(Boolean)
}

export function ddnsHosts(zone, value) {
  zone = zone.trim().toLowerCase()
  if (!zone) throw new Error('请先填写根域名')
  const hosts = domainList(value).map(prefix => prefix === '@' || prefix === zone ? zone : prefix.endsWith('.' + zone) ? prefix : `${prefix}.${zone}`)
  if (!hosts.length || hosts.length > 100) throw new Error('每组须填写 1–100 个域名')
  if (new Set(hosts).size !== hosts.length) throw new Error('域名列表中存在重复域名')
  return hosts
}

export function ddnsPrefixes(group) {
  return group.hosts.map(host => host === group.zone ? '@' : host.endsWith('.' + group.zone) ? host.slice(0, -group.zone.length - 1) : host).join('\n')
}

export function replaceDDNS(snapshot, group, originalId) {
  const config = structuredClone(snapshot.config)
  const index = config.ddns.groups.findIndex(item => item.id === originalId)
  if (originalId != null && index < 0) throw new Error('此 DDNS 组已不存在，请刷新后重新编辑')
  if (originalId == null) config.ddns.groups.push(group)
  else config.ddns.groups[index] = group
  return config
}

export function replaceCertificate(snapshot, request, originalId) {
  const config = structuredClone(snapshot.config)
  const index = config.acme.requests.findIndex(item => item.id === originalId)
  if (originalId != null && index < 0) throw new Error('此证书任务已不存在，请刷新后重新编辑')
  if (originalId == null) config.acme.requests.push(request)
  else config.acme.requests[index] = request
  return config
}

export function tokenChange(field, id, value, clear) {
  if (clear && value.trim()) throw new Error('填写新 Token 和清除 Token 不能同时选择')
  return clear || value.trim() ? { [field]: { [id]: clear ? '' : value.trim() } } : {}
}

export function jobState(enabled, job) {
  if (!enabled) return { label: '已停用', tone: 'badge-neutral' }
  if (!job || !job.last_run || job.last_run.startsWith('0001')) return { label: '等待运行', tone: 'badge-neutral' }
  if (job.running) return { label: '执行中', tone: 'badge-brand' }
  return Date.parse(job.last_success) >= Date.parse(job.last_run) ? { label: '执行成功', tone: 'badge-ok' } : { label: '执行失败', tone: 'badge-warn' }
}
