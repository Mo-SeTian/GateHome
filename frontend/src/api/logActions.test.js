import assert from 'node:assert/strict'
import test from 'node:test'
import {
  LOG_WHITELIST_GROUP_NAME,
  applyFirewallWhitelist,
  canManageLogIP,
  ipToSingleCIDR,
  isIPAllowlistedInFirewall,
  routeFirewallForEntry
} from './logActions.js'

const sampleSnapshot = {
  revision: 5,
  config: {
    routes: [
      { id: 'r1', group_id: 'default', host: 'app.example.test', firewall_id: 'fw-main' },
      { id: 'r2', group_id: 'default', host: 'open.example.test', firewall_id: '' }
    ],
    firewalls: [
      {
        id: 'fw-main',
        name: '公网防护',
        default_action: 'deny',
        groups: [
          { name: '黑名单组', match: 'include', action: 'deny', cidrs: ['198.51.100.0/24'], subscriptions: [] }
        ]
      }
    ]
  }
}

test('logActions validates IPs, resolves bound firewalls and inserts top-priority whitelist rules idempotently', () => {
  assert.equal(ipToSingleCIDR('203.0.113.9'), '203.0.113.9/32')
  assert.equal(ipToSingleCIDR('2001:DB8::1'), '2001:db8::1/128')
  assert.equal(ipToSingleCIDR('未知'), '')
  assert.equal(ipToSingleCIDR('not-an-ip'), '')

  const entry = { rule: 'default/app.example.test', remote: '203.0.113.9' }
  assert.equal(canManageLogIP(entry, sampleSnapshot.config), true)
  assert.equal(canManageLogIP({ rule: 'default/missing', remote: '203.0.113.9' }, sampleSnapshot.config), false)
  assert.equal(routeFirewallForEntry(entry, sampleSnapshot.config)?.id, 'fw-main')
  assert.equal(routeFirewallForEntry({ rule: 'default/open.example.test', remote: '203.0.113.9' }, sampleSnapshot.config), null)

  // First whitelist inserts top-priority allow group ahead of deny rules
  const first = applyFirewallWhitelist(sampleSnapshot, 'fw-main', '203.0.113.9')
  assert.equal(first.changed, true)
  assert.equal(first.cidr, '203.0.113.9/32')
  assert.equal(first.config.firewalls[0].groups[0].name, LOG_WHITELIST_GROUP_NAME)
  assert.equal(first.config.firewalls[0].groups[0].action, 'allow')
  assert.deepEqual(first.config.firewalls[0].groups[0].cidrs, ['203.0.113.9/32'])
  assert.equal(first.config.firewalls[0].groups[1].name, '黑名单组')
  assert.equal(isIPAllowlistedInFirewall(first.config.firewalls[0], '203.0.113.9'), true)

  // Original snapshot is not mutated
  assert.equal(sampleSnapshot.config.firewalls[0].groups.length, 1)

  // Second whitelist of same IP is idempotent
  const repeat = applyFirewallWhitelist({ revision: 6, config: first.config }, 'fw-main', '203.0.113.9')
  assert.equal(repeat.changed, false)

  // Adding a second IP reuses the existing top allowlist group
  const second = applyFirewallWhitelist({ revision: 6, config: first.config }, 'fw-main', '2001:db8::9')
  assert.equal(second.changed, true)
  assert.equal(second.config.firewalls[0].groups.length, 2)
  assert.deepEqual(second.config.firewalls[0].groups[0].cidrs, ['203.0.113.9/32', '2001:db8::9/128'])
})
