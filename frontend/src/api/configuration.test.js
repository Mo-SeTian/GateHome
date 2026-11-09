import assert from 'node:assert/strict'
import { test } from 'node:test'
import { firewallDraft, firewallRecord, forwardState, replaceConfigItem, routeHost } from './configuration.js'

test('editing a route from a captured revision preserves unrelated data and the original draft', () => {
  const route = { group_id: 'default', host: 'old.example.com', auth: { enabled: true }, image: 'kept-image' }
  const snapshot = {
    revision: 8,
    config: {
      routes: [route],
      groups: [{ id: 'default' }],
      dashboard: { widgets: [] },
      future_option: { enabled: true }
    }
  }
  const next = replaceConfigItem(snapshot, 'routes', { ...route, name: 'Edited' }, 'default/old.example.com')

  assert.equal(snapshot.revision, 8)
  assert.equal(snapshot.config.routes[0].name, undefined)
  assert.equal(next.routes[0].image, 'kept-image')
  assert.deepEqual(next.future_option, snapshot.config.future_option)
  assert.deepEqual(next.dashboard, { widgets: [] })
  assert.deepEqual(next.groups, snapshot.config.groups)
})

test('a legacy config without forwards can gain a forward without dropping other fields', () => {
  const snapshot = { revision: 2, config: { routes: [], groups: [] } }
  const forward = { id: 'forward-fixture', enabled: true }
  assert.deepEqual(replaceConfigItem(snapshot, 'forwards', forward, null), {
    ...snapshot.config,
    forwards: [forward]
  })
  assert.equal(snapshot.config.forwards, undefined)
})

test('forward status compares saved rules with active listeners and effective UDP timeout', () => {
  const rule = {
    id: 'fixture',
    enabled: true,
    protocol: 'tcp+udp',
    listen_port: 25000,
    upstream: '[::1]:9000',
    firewall_id: ''
  }
  assert.equal(forwardState(rule, []).label, '启用待重启')
  assert.equal(forwardState(rule, null).label, '启用待重启')
  assert.equal(forwardState(rule, [{ ...rule, udp_idle_seconds: 300 }]).label, '正在监听')
  assert.equal(forwardState({ ...rule, udp_idle_seconds: 600 }, [rule]).label, '修改待重启')
  assert.equal(forwardState({ ...rule, enabled: false }, [rule]).label, '停用待重启')
  assert.equal(forwardState({ ...rule, enabled: false }, []).label, '已停用')
})

test('route prefixes support group suffixes while existing complete domains retain their identity', () => {
  const group = { domain_suffix: 'example.com' }
  assert.equal(routeHost('@', group), 'example.com')
  assert.equal(routeHost('NAS', group), 'nas.example.com')
  assert.equal(routeHost('Other.EXAMPLE.NET.', group), 'other.example.net')
})

test('firewall edits preserve unknown fields and group order while converting input units', () => {
  const original = {
    id: 'policy', name: ' Original ', default_action: 'deny', future: true,
    groups: [{ name: ' First ', match: 'include', action: 'allow', cidrs: ['10.0.0.0/8'], subscriptions: ['source'], future: 1 }],
    protection: { future: 2, waf: { mode: 'detect', body_limit: 2097152, exceptions: [
      { rule_id: 942100, host: 'APP.EXAMPLE.COM', path: '/api', parameter: 'content', future: 3 }
    ] }, rate: { enabled: true, requests: 20, window_seconds: 60, path: '/api', future: 4 }, freeze: { enabled: true, failures: 3, window_seconds: 30, seconds: 120 } }
  }
  const draft = firewallDraft(original)
  draft.groups[0].cidrsText = '192.0.2.0/24,\n2001:db8::/32\n'
  draft.groups.unshift({ name: 'Precedence', match: 'exclude', action: 'deny', cidrsText: '10.0.0.0/8', subscriptions: [] })
  draft.bodyKiB = 512
  draft.freezeMinutes = 5
  const record = firewallRecord(draft)
  assert.equal(record.future, true)
  assert.equal(record.protection.future, 2)
  assert.equal(record.protection.rate.future, 4)
  assert.equal(record.protection.waf.exceptions[0].future, 3)
  assert.equal(record.protection.waf.exceptions[0].host, 'app.example.com')
  assert.equal(record.protection.waf.body_limit, 524288)
  assert.equal(record.protection.freeze.seconds, 300)
  assert.equal(record.groups[0].name, 'Precedence')
  assert.deepEqual(record.groups[1].cidrs, ['192.0.2.0/24', '2001:db8::/32'])
  assert.equal(record.groups[1].future, 1)
  assert.deepEqual(original.groups[0].cidrs, ['10.0.0.0/8'])
  assert.equal('cidrsText' in record.groups[0], false)
  assert.equal('bodyKiB' in record, false)
})

test('legacy firewall zero values open with the effective defaults without mutating saved data', () => {
  const original = {
    id: 'legacy', name: 'Legacy', default_action: 'allow', groups: null,
    protection: { waf: { mode: '', level: 0, body_limit: 0, exceptions: null },
      rate: { enabled: false, requests: 0, window_seconds: 0, path: '' },
      freeze: { enabled: false, failures: 0, window_seconds: 0, seconds: 0 }, rules: null }
  }
  const draft = firewallDraft(original)
  assert.equal(draft.protection.waf.mode, 'off')
  assert.equal(draft.protection.waf.level, 1)
  assert.equal(draft.bodyKiB, 1024)
  assert.equal(draft.protection.rate.requests, 120)
  assert.equal(draft.protection.freeze.failures, 5)
  assert.equal(draft.freezeMinutes, 60)
  assert.deepEqual(draft.groups, [])
  assert.deepEqual(draft.protection.rules, [])
  assert.equal(original.protection.rate.requests, 0)
  assert.equal(original.groups, null)
})
