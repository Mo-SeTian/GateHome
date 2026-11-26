import test from 'node:test'
import assert from 'node:assert/strict'
import {
  deriveConnectionRuleCandidates,
  findDuplicateCustomRule,
  formatMihomoRulePreview
} from './mihomoRuleHelper.js'

test('deriveConnectionRuleCandidates extracts root domain, subdomain suffix, exact host, IP and port', () => {
  const list = deriveConnectionRuleCandidates({
    metadata: {
      host: 'cdn.api.openai.com',
      destinationIP: '104.18.7.192',
      destinationPort: '443'
    }
  })

  assert.deepEqual(
    list.map((c) => ({ type: c.type, value: c.value })),
    [
      { type: 'DOMAIN-SUFFIX', value: 'openai.com' },
      { type: 'DOMAIN-SUFFIX', value: 'api.openai.com' },
      { type: 'DOMAIN', value: 'cdn.api.openai.com' },
      { type: 'IP-CIDR', value: '104.18.7.192/32' },
      { type: 'DST-PORT', value: '443' }
    ]
  )
  assert.equal(list[3].no_resolve, true)
})

test('deriveConnectionRuleCandidates respects multi-part public suffixes like co.uk and com.cn', () => {
  const uk = deriveConnectionRuleCandidates({
    metadata: { host: 'static.bbc.co.uk', destinationIP: '151.101.0.81', destinationPort: '443' }
  })
  assert.equal(uk[0].type, 'DOMAIN-SUFFIX')
  assert.equal(uk[0].value, 'bbc.co.uk')
  assert.equal(uk[1].type, 'DOMAIN')
  assert.equal(uk[1].value, 'static.bbc.co.uk')

  const cn = deriveConnectionRuleCandidates({
    metadata: { host: 'img.example.com.cn', destinationPort: '80' }
  })
  assert.equal(cn[0].value, 'example.com.cn')
})

test('deriveConnectionRuleCandidates falls back to IP-CIDR/IP-CIDR6 for IP-only connections', () => {
  const v4 = deriveConnectionRuleCandidates({
    metadata: { host: '', destinationIP: '1.1.1.1', destinationPort: '443' }
  })
  assert.equal(v4[0].type, 'IP-CIDR')
  assert.equal(v4[0].value, '1.1.1.1/32')
  assert.equal(v4[0].no_resolve, true)

  const v6 = deriveConnectionRuleCandidates({
    metadata: { host: '2606:4700:4700::1111', destinationIP: '2606:4700:4700::1111', destinationPort: '443' }
  })
  assert.equal(v6[0].type, 'IP-CIDR6')
  assert.equal(v6[0].value, '2606:4700:4700::1111/128')
})

test('formatMihomoRulePreview and findDuplicateCustomRule behave consistently', () => {
  assert.equal(
    formatMihomoRulePreview({ type: 'DOMAIN-SUFFIX', value: 'openai.com', target: 'OpenAI', no_resolve: true }),
    '- DOMAIN-SUFFIX,openai.com,OpenAI'
  )
  assert.equal(
    formatMihomoRulePreview({ type: 'IP-CIDR', value: '1.1.1.1/32', target: 'DIRECT', no_resolve: true }),
    '- IP-CIDR,1.1.1.1/32,DIRECT,no-resolve'
  )
  assert.equal(formatMihomoRulePreview({ type: 'MATCH', target: '漏网之鱼' }), '- MATCH,漏网之鱼')

  const dup = findDuplicateCustomRule(
    [{ id: 'r1', type: 'DOMAIN-SUFFIX', value: 'OpenAI.com', target: 'OpenAI' }],
    { type: 'domain-suffix', value: 'openai.com' }
  )
  assert.equal(dup?.id, 'r1')
  assert.equal(findDuplicateCustomRule([], { type: 'DOMAIN', value: 'a.com' }), null)
})
