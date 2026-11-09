import assert from 'node:assert/strict'
import { test } from 'node:test'
import { forwardState, replaceConfigItem, routeHost } from './configuration.js'

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
