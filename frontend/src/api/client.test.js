import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'
import { ApiError, markSessionChanged, request, setUnauthorizedHandler } from './client.js'

const originalFetch = globalThis.fetch

afterEach(() => {
  globalThis.fetch = originalFetch
  setUnauthorizedHandler(undefined)
})

function response(status, data) {
  return Response.json(data, { status })
}

test('write requests use the browser cookie and the Go request verification header', async () => {
  let sent
  globalThis.fetch = async (path, options) => {
    sent = { path, ...options }
    return response(200, { ok: true })
  }

  await request('/api/logout', { method: 'POST', body: {} })

  assert.equal(sent.path, '/api/logout')
  assert.equal(sent.credentials, 'same-origin')
  assert.equal(sent.headers['X-Gatehouse-Request'], '1')
  assert.equal(sent.headers['Content-Type'], 'application/json')
  assert.equal(sent.body, '{}')
  assert.equal(sent.headers.Authorization, undefined)
})

test('image upload preserves FormData so the browser supplies its multipart boundary', async () => {
  let sent
  globalThis.fetch = async (_, options) => {
    sent = options
    return response(201, { id: 'image-fixture' })
  }
  const body = new FormData()
  body.append('file', new Blob(['fake image fixture'], { type: 'image/png' }), 'fixture.png')

  await request('/api/route-images/upload', { method: 'POST', body })

  assert.equal(sent.body, body)
  assert.equal(sent.headers['Content-Type'], undefined)
  assert.equal(sent.headers['X-Gatehouse-Request'], '1')
})

test('unauthorized responses from the same session notify only once after it ends', async () => {
  let notifications = 0
  setUnauthorizedHandler(() => {
    notifications++
    markSessionChanged()
  })
  globalThis.fetch = async () => response(401, { error: '请先登录' })

  const results = await Promise.allSettled([request('/api/status'), request('/api/dashboard')])

  assert.ok(results.every(result => result.status === 'rejected' && result.reason.status === 401))
  assert.equal(notifications, 1)
})

test('a delayed unauthorized response cannot end a newly established session', async () => {
  let resolveResponse
  let notifications = 0
  setUnauthorizedHandler(() => { notifications++ })
  globalThis.fetch = () => new Promise(resolve => { resolveResponse = resolve })
  const pending = request('/api/status')

  markSessionChanged()
  resolveResponse(response(401, { error: '旧会话已过期' }))

  await assert.rejects(pending, error => error instanceof ApiError && error.status === 401)
  assert.equal(notifications, 0)
})

test('failed login keeps the server error available to the form without redirecting', async () => {
  let notifications = 0
  setUnauthorizedHandler(() => { notifications++ })
  globalThis.fetch = async () => response(401, { error: '管理员账号或密码不正确' })

  await assert.rejects(request('/api/login', { notifyUnauthorized: false }), {
    message: '管理员账号或密码不正确', status: 401
  })
  assert.equal(notifications, 0)
})
