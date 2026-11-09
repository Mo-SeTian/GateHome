import assert from 'node:assert/strict'
import { test } from 'node:test'
import { logQuery, trafficQuery, subscriptionState, formatBytes, reportDate } from './reports.js'

test('log pagination keeps its snapshot and rule key while project filters stay scoped', () => {
  const filters = { rule: 'group/service.example.test', category: 'ddns', ip: '2001:db8::1', status: 403, search: 'a&b', from: '2026-11-09T00:00:00Z', to: '2026-11-09T01:00:00Z' }
  const access = logQuery('access', filters, 2, 20, 145)
  assert.equal(access.through, 145)
  assert.equal(access.rule, filters.rule)
  assert.equal(access.ip, '2001:db8::1')
  assert.equal(access.category, undefined)
  assert.equal(access.from, '2026-11-09T00:00:00.000Z')
  assert.equal(filters.from, '2026-11-09T00:00:00Z')
  const project = logQuery('project', filters, 1, 50)
  assert.equal(project.category, 'ddns')
  assert.equal(project.rule, undefined)
  assert.equal(project.ip, undefined)
  assert.throws(() => logQuery('access', { from: filters.to, to: filters.from }, 1, 20), /开始时间/)
})

test('security filters and traffic use their distinct backend identifiers and time contracts', () => {
  const security = logQuery('firewall', { engine: 'waf', rule_id: 941100, decision: 'detect', firewall: 'policy' }, 1, 20)
  assert.equal(security.engine, 'waf')
  assert.equal(security.rule_id, 941100)
  assert.equal(security.decision, 'detect')
  const id = 'route-fixture-id'
  assert.deepEqual(trafficQuery({ rule: id, range: '168', from: 'ignored' }), { rule: id, hours: 168 })
  assert.deepEqual(trafficQuery({ rule: id, range: 'custom', from: '2026-11-09T00:00Z', to: '2026-11-09T01:00Z' }), {
    rule: id, from: '2026-11-09T00:00:00.000Z', to: '2026-11-09T01:00:00.000Z'
  })
  assert.throws(() => trafficQuery({ range: 'custom', from: '', to: '' }), /起止时间/)
  assert.throws(() => trafficQuery({ range: 'custom', from: '2026-11-09', to: '2026-11-08' }), /起止时间/)
})

test('subscription states distinguish first load, cache fallback, active refresh and disabled sources', () => {
  const subscription = { enabled: true }
  assert.equal(subscriptionState(subscription).label, '正在读取状态')
  assert.equal(subscriptionState(subscription, { ready: false, last_run: '0001-01-01T00:00:00Z' }).label, '等待首次更新')
  const failed = { last_run: '2026-11-09T01:00:00Z', last_success: '2026-11-09T00:00:00Z' }
  assert.equal(subscriptionState(subscription, { ...failed, ready: true }).label, '更新失败，使用缓存')
  assert.equal(subscriptionState(subscription, { ...failed, ready: false }).label, '拉取失败，暂无缓存')
  assert.equal(subscriptionState(subscription, { ...failed, ready: true, running: true }).label, '正在拉取')
  assert.equal(subscriptionState({ enabled: false }, { running: true }).label, '已停用')
  assert.equal(subscriptionState(subscription, { ready: true, last_run: failed.last_success, last_success: failed.last_run }).label, '规则可用')
  assert.equal(formatBytes(2048), '2 KiB')
  assert.equal(reportDate('0001-01-01T00:00:00Z'), '—')
})
