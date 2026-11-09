import assert from 'node:assert/strict'
import { test } from 'node:test'
import { setUnauthorizedHandler } from './client.js'
import { maintenanceApi, webDAVPayload, onlineUpdateActive } from './maintenance.js'

test('WebDAV edits preserve the captured revision and omit unchanged write-only secrets', () => {
  const snapshot = { revision: 17, config: { url: 'http://storage.test/dav/', keep: 7, future_field: true } }
  const draft = { ...snapshot.config, keep: 3 }
  const body = webDAVPayload(snapshot, draft, { password: '', backupPassword: '', confirmPassword: '' })
  assert.deepEqual(body, { revision: 17, config: draft })
  assert.equal(snapshot.config.keep, 7)
  assert.equal(Object.hasOwn(body, 'password'), false)
  assert.equal(Object.hasOwn(body, 'backup_password'), false)
})

test('clearing storage authentication is explicit and encryption confirmation is checked before a write', () => {
  const secrets = {
    clearPassword: true,
    password: 'TEST_ONLY_IGNORED',
    backupPassword: 'TEST_ONLY_BACKUP_PASSWORD',
    confirmPassword: 'TEST_ONLY_BACKUP_PASSWORD'
  }
  const body = webDAVPayload({ revision: 4 }, { keep: 2 }, secrets)
  assert.equal(body.password, '')
  assert.equal(body.backup_password, secrets.backupPassword)
  assert.throws(() => webDAVPayload({ revision: 4 }, {}, { ...secrets, confirmPassword: '' }), /不一致/)
})

test('binary backup download uses the shared cookie and CSRF contract', async () => {
  const original = globalThis.fetch
  globalThis.fetch = async (path, options) => {
    assert.equal(path, '/api/maintenance/backup')
    assert.equal(options.credentials, 'same-origin')
    assert.equal(options.headers['X-Gatehouse-Request'], '1')
    assert.deepEqual(JSON.parse(options.body), { password: 'TEST_ONLY_PASSWORD' })
    return new Response('TEST_ONLY_ARCHIVE', { headers: { 'Content-Type': 'application/zip' } })
  }
  try {
    assert.equal(await (await maintenanceApi.backup('TEST_ONLY_PASSWORD')).text(), 'TEST_ONLY_ARCHIVE')
  } finally {
    globalThis.fetch = original
  }
})

test('a failed binary request still reports JSON errors and expires an unauthorized admin session', async () => {
  const original = globalThis.fetch
  let expired = false
  setUnauthorizedHandler(() => {
    expired = true
  })
  globalThis.fetch = async () => Response.json({ error: '会话已过期' }, { status: 401 })
  try {
    await assert.rejects(maintenanceApi.downloadRemote('TEST_ONLY_ARCHIVE.zip'), /会话已过期/)
    assert.equal(expired, true)
  } finally {
    globalThis.fetch = original
    setUnauthorizedHandler(undefined)
  }
})

test('queued, verifying and restarting online updates remain pending until the restarted service reports its result', () => {
  for (const phase of ['checking', 'downloading', 'verifying', 'restarting'])
    assert.equal(onlineUpdateActive(phase), true)
  for (const phase of ['', 'error', undefined]) assert.equal(onlineUpdateActive(phase), false)
})
