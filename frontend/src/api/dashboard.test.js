import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  dashboardWidgets,
  dashboardWidgetIDs,
  dashboardRecord,
  reorderWidget,
  serviceURL
} from './dashboard.js'

test('legacy null widgets retain all fourteen components while an explicit empty layout stays empty', () => {
  assert.equal(dashboardWidgetIDs({ widgets: null }).length, 14)
  assert.deepEqual(dashboardWidgetIDs({ widgets: [] }), [])
  assert.equal(new Set(dashboardWidgets.map((item) => item.id)).size, 14)
})

test('layout writes preserve other business configuration and unknown dashboard fields', () => {
  const snapshot = {
    revision: 12,
    config: {
      routes: [{ id: 'existing' }],
      dashboard: { widgets: ['cpu'], future_field: true, network_interface: '' }
    }
  }
  const result = dashboardRecord(
    snapshot,
    { network_interface: 'eth0', map_province: '江苏' },
    ['memory', 'cpu', 'map'],
    ['map', 'cpu']
  )
  assert.deepEqual(result.routes, snapshot.config.routes)
  assert.deepEqual(result.dashboard, {
    widgets: ['cpu', 'map'],
    future_field: true,
    network_interface: 'eth0',
    map_province: '江苏'
  })
  assert.deepEqual(snapshot.config.dashboard.widgets, ['cpu'])
})

test('drag reorder moves only the chosen widget and ignores unknown targets without mutating the draft', () => {
  const order = ['server', 'cpu', 'map']
  assert.deepEqual(reorderWidget(order, 'map', 'server'), ['map', 'server', 'cpu'])
  assert.deepEqual(reorderWidget(order, 'map', 'missing'), order)
  assert.deepEqual(order, ['server', 'cpu', 'map'])
})

test('service launch links only allow HTTP addresses without embedded credentials', () => {
  assert.equal(serviceURL('http://127.0.0.1:8080/'), 'http://127.0.0.1:8080/')
  for (const value of [
    'javascript:alert(1)',
    'https://user:TEST_ONLY_PASSWORD@example.test',
    'file:///tmp/site'
  ])
    assert.equal(serviceURL(value), '')
})
