import assert from 'node:assert/strict'
import { test } from 'node:test'
import { mcpRequest } from './mcp.js'
import { setUnauthorizedHandler } from './client.js'
import { sunPanelLaunchURL } from './sunpanel.js'

test('MCP uses a separate Bearer credential and does not send the admin cookie', async () => {
  const original = globalThis.fetch
  globalThis.fetch = async (path, options) => {
    assert.equal(path, '/mcp')
    assert.equal(options.credentials, 'omit')
    assert.equal(options.headers.Authorization, 'Bearer TEST_ONLY_TOKEN')
    const message = JSON.parse(options.body)
    assert.equal(message.method, 'tools/call')
    assert.deepEqual(message.params, { name: 'gatehome_get_status', arguments: {} })
    return Response.json({ jsonrpc: '2.0', result: { isError: true, content: [] }, id: message.id })
  }
  try {
    assert.equal(
      (await mcpRequest('TEST_ONLY_TOKEN', 'tools/call', { name: 'gatehome_get_status', arguments: {} }))
        .isError,
      true
    )
  } finally {
    globalThis.fetch = original
  }
})

test('an invalid MCP token does not log out the administrator or expose the response body', async () => {
  const original = globalThis.fetch
  let unauthorized = false
  setUnauthorizedHandler(() => {
    unauthorized = true
  })
  globalThis.fetch = async () => new Response('TEST_ONLY_PRIVATE_RESPONSE', { status: 401 })
  try {
    await assert.rejects(
      mcpRequest('TEST_ONLY_TOKEN', 'tools/list', {}),
      (error) => error.message.includes('HTTP 401') && !error.message.includes('TEST_ONLY')
    )
    assert.equal(unauthorized, false)
  } finally {
    globalThis.fetch = original
    setUnauthorizedHandler(undefined)
  }
})

test('SunPanel launch links allow safe local or HTTP addresses and reject unsafe URLs', () => {
  assert.equal(sunPanelLaunchURL({ port: 16680 }, '[::1]'), 'http://[::1]:16680/')
  assert.equal(sunPanelLaunchURL({ launch_url: '/sunpanel/' }, 'localhost'), '/sunpanel/')
  assert.equal(
    sunPanelLaunchURL({ external_url: 'https://home.example.test/' }, 'localhost', true),
    'https://home.example.test/'
  )
  for (const value of [
    'javascript:alert(1)',
    '//other.example.test/',
    '/\\other.example.test/',
    'https://user:pass@example.test/',
    'https://example.test/white space'
  ]) {
    assert.equal(sunPanelLaunchURL({ launch_url: value }, 'localhost'), '')
  }
  assert.equal(sunPanelLaunchURL({ external_url: '/sunpanel/' }, 'localhost', true), '')
})
