import assert from 'node:assert/strict'
import { test } from 'node:test'
import { ddnsHosts, ddnsPrefixes, domainList, jobState, replaceCertificate, replaceDDNS, tokenChange } from './dns.js'

test('DDNS domains expand prefixes and preserve root, nested and full host names', () => {
  const hosts = ddnsHosts('Example.TEST', '@, NAS\napp.home\nfull.example.test')
  assert.deepEqual(hosts, ['example.test', 'nas.example.test', 'app.home.example.test', 'full.example.test'])
  assert.equal(ddnsPrefixes({ zone: 'example.test', hosts }), '@\nnas\napp.home\nfull')
  assert.throws(() => ddnsHosts('example.test', '@,example.test'), /重复/)
  assert.throws(() => ddnsHosts('', 'nas'), /根域名/)
  assert.deepEqual(domainList('*.Example.TEST, example.test'), ['*.example.test', 'example.test'])
})
test('DDNS and certificate changes preserve full config, credentials stay in separate write fields', () => {
  const snapshot = { revision: 5, config: { ddns: { ip_monitor: { group_id: 'dns' }, groups: [{ id: 'dns', future: true }] }, acme: { enabled: true, email: 'test@example.test', requests: [{ id: 'cert' }] }, routes: [{ id: 'kept' }], future: 4 } }
  const next = replaceDDNS(snapshot, { ...snapshot.config.ddns.groups[0], name: 'Edited' }, 'dns')
  assert.equal(next.ddns.groups[0].future, true)
  assert.equal(next.future, 4)
  assert.equal(snapshot.config.ddns.groups[0].name, undefined)
  assert.deepEqual(next.routes, snapshot.config.routes)
  const cert = replaceCertificate(snapshot, { id: 'cert', domains: ['example.test'] }, 'cert')
  assert.equal(cert.acme.email, 'test@example.test')
  assert.deepEqual(cert.ddns, snapshot.config.ddns)
  assert.throws(() => replaceCertificate(snapshot, {}, 'gone'), /不存在/)
  assert.deepEqual(tokenChange('dns_tokens', 'dns', '', false), {})
  assert.deepEqual(tokenChange('certificate_tokens', 'cert', '', true), { certificate_tokens: { cert: '' } })
  assert.deepEqual(tokenChange('dns_tokens', 'dns', ' TEST_ONLY_PLACEHOLDER ', false), { dns_tokens: { dns: 'TEST_ONLY_PLACEHOLDER' } })
  assert.throws(() => tokenChange('dns_tokens', 'dns', 'TEST_ONLY_PLACEHOLDER', true), /同时/)
})
test('queued jobs never appear successful until the server records completion', () => {
  assert.equal(jobState(true).label, '等待运行')
  assert.equal(jobState(false, { running: true }).label, '已停用')
  const job = { last_run: '2026-11-09T10:00:00Z', last_success: '2026-11-09T09:00:00Z' }
  assert.equal(jobState(true, job).label, '执行失败')
  assert.equal(jobState(true, { ...job, running: true }).label, '执行中')
  assert.equal(jobState(true, { ...job, last_success: '2026-11-09T10:00:01Z' }).label, '执行成功')
})
