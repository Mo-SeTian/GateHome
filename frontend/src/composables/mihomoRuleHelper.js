const MULTI_PART_PUBLIC_SUFFIXES = new Set([
  'co.uk',
  'org.uk',
  'ac.uk',
  'gov.uk',
  'com.cn',
  'net.cn',
  'org.cn',
  'gov.cn',
  'edu.cn',
  'com.hk',
  'org.hk',
  'com.tw',
  'org.tw',
  'com.sg',
  'com.au',
  'net.au',
  'org.au',
  'co.jp',
  'ne.jp',
  'or.jp',
  'co.kr',
  'com.br',
  'com.mx',
  'com.tr'
])

function isIPv4(host) {
  return /^(?:\d{1,3}\.){3}\d{1,3}$/.test(host)
}

function isIPLiteral(host) {
  if (!host) return false
  const clean = String(host).trim().replace(/^\[|\]$/g, '')
  return isIPv4(clean) || clean.includes(':')
}

function extractRootDomain(host) {
  const parts = String(host || '')
    .trim()
    .replace(/\.$/, '')
    .toLowerCase()
    .split('.')
    .filter(Boolean)
  if (parts.length < 2) return parts.join('.')
  if (parts.length >= 3) {
    const tail2 = parts.slice(-2).join('.')
    if (MULTI_PART_PUBLIC_SUFFIXES.has(tail2)) {
      return parts.slice(-3).join('.')
    }
  }
  return parts.slice(-2).join('.')
}

/**
 * Derive ordered quick-pick Mihomo rule candidates from a connection snapshot.
 */
export function deriveConnectionRuleCandidates(conn) {
  const meta = conn?.metadata || {}
  const host = String(meta.host || '').trim().replace(/^\[|\]$/g, '').replace(/\.$/, '')
  const dstIP = String(meta.destinationIP || '').trim().replace(/^\[|\]$/g, '')
  const port = meta.destinationPort ? String(meta.destinationPort).trim() : ''

  const candidates = []
  const seen = new Set()
  const pushUnique = (item) => {
    const key = `${item.type}:${item.value.toLowerCase()}`
    if (!item.value || seen.has(key)) return
    seen.add(key)
    candidates.push(item)
  }

  if (host && !isIPLiteral(host)) {
    const normalizedHost = host.toLowerCase()
    const rootDomain = extractRootDomain(normalizedHost)
    if (rootDomain && rootDomain.includes('.')) {
      pushUnique({
        key: `suffix:${rootDomain}`,
        label: `${rootDomain} · 根域名`,
        type: 'DOMAIN-SUFFIX',
        value: rootDomain,
        no_resolve: false
      })
    }

    const hostParts = normalizedHost.split('.').filter(Boolean)
    const rootParts = rootDomain ? rootDomain.split('.').filter(Boolean) : []
    if (hostParts.length > rootParts.length + 1) {
      const parentSub = hostParts.slice(1).join('.')
      if (parentSub !== rootDomain && parentSub !== normalizedHost) {
        pushUnique({
          key: `suffix:${parentSub}`,
          label: `${parentSub} · 子域后缀`,
          type: 'DOMAIN-SUFFIX',
          value: parentSub,
          no_resolve: false
        })
      }
    }

    pushUnique({
      key: `domain:${normalizedHost}`,
      label: `${normalizedHost} · 精确域名`,
      type: 'DOMAIN',
      value: normalizedHost,
      no_resolve: false
    })
  }

  const effectiveIP = dstIP || (isIPLiteral(host) ? host : '')
  if (effectiveIP && isIPLiteral(effectiveIP)) {
    const isV6 = effectiveIP.includes(':')
    const cidr = `${effectiveIP}${isV6 ? '/128' : '/32'}`
    pushUnique({
      key: `ip:${cidr}`,
      label: `${cidr} · 目标 IP`,
      type: isV6 ? 'IP-CIDR6' : 'IP-CIDR',
      value: cidr,
      no_resolve: true
    })
  }

  if (port && /^\d+$/.test(port)) {
    pushUnique({
      key: `port:${port}`,
      label: `端口 ${port}`,
      type: 'DST-PORT',
      value: port,
      no_resolve: false
    })
  }

  return candidates
}

/**
 * Format a custom rule preview line matching Mihomo YAML syntax.
 */
export function formatMihomoRulePreview({ type, value, target, no_resolve: noResolve } = {}) {
  const cleanType = String(type || 'DOMAIN-SUFFIX').trim().toUpperCase()
  const cleanTarget = String(target || 'DIRECT').trim() || 'DIRECT'
  if (cleanType === 'MATCH') {
    return `- MATCH,${cleanTarget}`
  }
  const cleanValue = String(value || '').trim() || '<匹配值>'
  const supportsNoResolve = ['IP-CIDR', 'IP-CIDR6', 'SRC-IP-CIDR', 'GEOIP', 'RULE-SET'].includes(cleanType)
  const suffix = supportsNoResolve && noResolve ? ',no-resolve' : ''
  return `- ${cleanType},${cleanValue},${cleanTarget}${suffix}`
}

/**
 * Check if customRules already contains a matching (type, value) rule.
 */
export function findDuplicateCustomRule(customRules, { type, value } = {}) {
  if (!Array.isArray(customRules) || !customRules.length) return null
  const cleanType = String(type || '').trim().toUpperCase()
  const cleanValue = String(value || '').trim().toLowerCase()
  if (!cleanType) return null
  return (
    customRules.find((rule) => {
      const ruleType = String(rule?.type || '').trim().toUpperCase()
      if (cleanType === 'MATCH') return ruleType === 'MATCH'
      return ruleType === cleanType && String(rule?.value || '').trim().toLowerCase() === cleanValue
    }) || null
  )
}
