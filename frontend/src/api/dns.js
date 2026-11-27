export const DNS_PROVIDERS = [
  {
    id: 'cloudflare',
    name: 'Cloudflare',
    requiresKeyId: false,
    keyIdLabel: '',
    keyIdSecretName: '',
    tokenLabel: '独立 API Token',
    tokenSecretName: 'Token',
    ddnsHint: '需要此 Zone 的 Zone:Read / DNS:Edit 权限。已保存 Token 可点击小眼睛查看，不与证书任务共用。',
    certHint: '使用 ECDSA P-256。独立 Token 需要相应 Zone 的 Zone:Read / DNS:Edit 权限，与 DDNS 组的凭据分开。'
  },
  {
    id: 'alidns',
    name: '阿里云 DNS',
    requiresKeyId: true,
    keyIdLabel: 'AccessKey ID',
    keyIdSecretName: 'AccessKey ID',
    tokenLabel: 'AccessKey Secret',
    tokenSecretName: 'AccessKey Secret',
    ddnsHint: '建议使用仅授予 AliyunDNSFullAccess 或对应域名解析权限的 RAM 子账号 AccessKey。',
    certHint: '使用 ECDSA P-256。需要阿里云云解析 DNS 的域名读取与 TXT 解析记录增删权限，与 DDNS 组凭据分开。'
  },
  {
    id: 'tencentcloud',
    name: '腾讯云 DNS (DNSPod)',
    requiresKeyId: true,
    keyIdLabel: 'SecretId',
    keyIdSecretName: 'SecretId',
    tokenLabel: 'SecretKey',
    tokenSecretName: 'SecretKey',
    ddnsHint: '请使用腾讯云访问管理（CAM）API 密钥（SecretId / SecretKey），并授予 QcloudDNSPodFullAccess 或对应解析权限。',
    certHint: '使用 ECDSA P-256。需要腾讯云 DNSPod 域名读取与 TXT 解析记录增删权限，与 DDNS 组凭据分开。'
  },
  {
    id: 'huaweicloud',
    name: '华为云 DNS',
    requiresKeyId: true,
    keyIdLabel: 'Access Key ID',
    keyIdSecretName: 'Access Key ID',
    tokenLabel: 'Secret Access Key',
    tokenSecretName: 'Secret Access Key',
    ddnsHint: '请填写华为云 IAM 访问密钥（AK / SK），并确保账号具有公网域名解析 Zone 与 RecordSet 管理权限。',
    certHint: '使用 ECDSA P-256。需要华为云公网域名解析 TXT RecordSet 读写与删除权限，与 DDNS 组凭据分开。'
  },
  {
    id: 'porkbun',
    name: 'Porkbun',
    requiresKeyId: true,
    keyIdLabel: 'API Key',
    keyIdSecretName: 'API Key',
    tokenLabel: 'Secret API Key',
    tokenSecretName: 'Secret API Key',
    ddnsHint: '需在 Porkbun 域名管理控制台为对应域名开启 API Access，并填写 API Key 与 Secret API Key。',
    certHint: '使用 ECDSA P-256。需为对应域名在 Porkbun 开启 API Access，与 DDNS 组凭据分开。'
  }
]

export function dnsProviderMeta(provider) {
  return DNS_PROVIDERS.find(item => item.id === provider) || DNS_PROVIDERS[0]
}

export function dnsProviderLabel(provider) {
  return DNS_PROVIDERS.find(item => item.id === provider)?.name || provider || 'Cloudflare'
}

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

export function tokenChange(field, id, value) {
  return value === undefined ? {} : { [field]: { [id]: value.trim() } }
}

export function credentialChange(tokenField, credField, id, { keyId, token } = {}, requiresKeyId = false) {
  if (!requiresKeyId) {
    return tokenChange(tokenField, id, token)
  }
  const patch = {}
  if (keyId !== undefined) patch.key_id = keyId.trim()
  if (token !== undefined) patch.token = token.trim()
  return Object.keys(patch).length ? { [credField]: { [id]: patch } } : {}
}

export function jobState(enabled, job) {
  if (!enabled) return { label: '已停用', tone: 'badge-neutral' }
  if (!job || !job.last_run || job.last_run.startsWith('0001')) return { label: '等待运行', tone: 'badge-neutral' }
  if (job.running) return { label: '执行中', tone: 'badge-brand' }
  return Date.parse(job.last_success) >= Date.parse(job.last_run) ? { label: '执行成功', tone: 'badge-ok' } : { label: '执行失败', tone: 'badge-warn' }
}
