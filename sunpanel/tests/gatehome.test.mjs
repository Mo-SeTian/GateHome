import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import ts from 'typescript'

const source = readFileSync(new URL('../src/utils/gateHome.ts', import.meta.url), 'utf8')
const shared = ts.transpileModule(readFileSync(new URL('../../shared/sunpanel-images.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2022 } }).outputText
const sharedURL = `data:text/javascript;base64,${Buffer.from(shared).toString('base64')}`
const { withRouteImages } = await import(sharedURL)
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2022 } })
const { routeLink, isImported, routeChanges, loadGateHomeRoutes, GateHomeLoginRequired, openGateHomeConnection } = await import(`data:text/javascript;base64,${Buffer.from(outputText.replace('../../../shared/sunpanel-images', sharedURL)).toString('base64')}`)
const route = { id: 'TEST_ONLY_ROUTE', title: 'NAS', group: 'Home', url: 'https://nas.example.test/', lanUrl: 'http://192.168.1.2:5000/', image: '' }

test('duplicates match a stable source ID or an existing canonical default URL', () => {
  assert.equal(isImported(route, [{ url: 'https://custom.example.test', gateHome: routeLink(route) }]), true)
  assert.equal(isImported(route, [{ url: 'https://NAS.example.test/#bookmark' }]), true)
  assert.equal(isImported(route, [{ url: 'https://other.example.test/' }]), false)
})

test('source changes show custom values as preserved and leave icons/groups outside synchronization', () => {
  const item = { title: 'My NAS', url: route.url, lanUrl: route.lanUrl, gateHome: routeLink(route), icon: { src: 'TEST_ONLY_ICON' }, itemIconGroupId: 42 }
  const next = { ...route, title: 'New NAS', url: 'https://renamed.example.test/', lanUrl: 'http://192.168.1.3:5000/' }
  const changes = routeChanges(item, next)
  assert.deepEqual(changes.map(change => [change.field, change.preserved]), [['title', true], ['url', false], ['lanUrl', false]])
  assert.equal(item.title, 'My NAS')
  assert.equal(item.itemIconGroupId, 42)
  assert.deepEqual(routeChanges(item, route), [])
  assert.deepEqual(routeChanges(item, { ...next, id: 'TEST_ONLY_ANOTHER_ROUTE' }), [])
})

test('imported names are truncated by Unicode characters consistently with the form', () => {
  assert.equal(Array.from(routeLink({ ...route, title: '网站😀'.repeat(15) }).title).length, 20)
})

function connectionWindow(t, blocked = false) {
  const previous = globalThis.window
  const listeners = new Set()
  const popup = { closed: false, close() { this.closed = true } }
  let opened
  globalThis.window = {
    location: { origin: 'https://page.example.test' },
    open(url) { opened = new URL(url); return blocked ? null : popup },
    addEventListener(type, callback) { if (type === 'message') listeners.add(callback) },
    removeEventListener(type, callback) { if (type === 'message') listeners.delete(callback) },
  }
  t.after(() => { globalThis.window = previous })
  return { popup, listeners, url: () => opened, send: event => { for (const listener of listeners) listener(event) } }
}

test('cross-domain connection accepts only its exact origin, window and nonce, returning bookmark fields only', async (t) => {
  const fixture = connectionWindow(t)
  const connection = openGateHomeConnection('https://gatehome.example.test')
  const query = new URLSearchParams(fixture.url().hash.split('?')[1])
  assert.equal(query.get('origin'), 'https://page.example.test')
  assert.match(query.get('state'), /^[a-f0-9]{32}$/)
  const imported = { ...route, token: 'TEST_ONLY_UNEXPECTED_FIELD' }
  const event = { origin: 'https://gatehome.example.test', source: fixture.popup,
    data: { type: 'gatehome:sunpanel-routes', state: query.get('state'), routes: [imported] } }
  fixture.send({ ...event, origin: 'https://evil.example' })
  fixture.send({ ...event, source: {} })
  fixture.send({ ...event, data: { ...event.data, state: 'wrong-request' } })
  fixture.send({ ...event, data: { ...event.data, routes: [{ id: 'missing-fields' }] } })
  assert.equal(fixture.popup.closed, false)
  assert.equal(fixture.listeners.size, 1)
  fixture.send(event)
  assert.deepEqual(await connection.promise, [route])
  assert.equal(fixture.popup.closed, true)
  assert.equal(fixture.listeners.size, 0)
})

test('blocked windows and unsafe management URLs give actionable failures', (t) => {
  connectionWindow(t, true)
  for (const origin of ['javascript:alert(1)', 'https://user:password@example.test', 'https://example.test/path', 'https://example.test/?next=other'])
    assert.throws(() => openGateHomeConnection(origin), /入口无效/)
  assert.throws(() => openGateHomeConnection('https://gatehome.example.test'), /允许弹出窗口/)
})

test('cancelled and manually closed connections release listeners and reject instead of accepting stale data', async (t) => {
  const fixture = connectionWindow(t)
  const cancelled = openGateHomeConnection('https://gatehome.example.test')
  cancelled.cancel()
  await assert.rejects(cancelled.promise, { name: 'AbortError' })
  assert.equal(fixture.listeners.size, 0)
  fixture.popup.closed = false
  const closed = openGateHomeConnection('https://gatehome.example.test')
  fixture.popup.closed = true
  await assert.rejects(closed.promise, /窗口已关闭/)
  assert.equal(fixture.listeners.size, 0)
})

test('same-origin loading retains cookie auth and distinguishes a missing management session', async (t) => {
  const previous = globalThis.fetch
  t.after(() => { globalThis.fetch = previous })
  const controller = new AbortController()
  globalThis.fetch = async (url, options) => {
    assert.equal(url, '/api/sunpanel/routes')
    assert.equal(options.credentials, 'same-origin')
    assert.equal(options.signal, controller.signal)
    return new Response('', { status: 401 })
  }
  await assert.rejects(loadGateHomeRoutes(controller.signal), GateHomeLoginRequired)
  globalThis.fetch = async () => new Response(JSON.stringify([route]))
  assert.deepEqual(await loadGateHomeRoutes(), [route])
})

test('timed-out connections close their window and release the message listener', async (t) => {
  const fixture = connectionWindow(t)
  const previous = Date.now
  let now = previous()
  Date.now = () => now
  t.after(() => { Date.now = previous })
  const connection = openGateHomeConnection('https://gatehome.example.test')
  now += 120001
  await assert.rejects(connection.promise, /连接超时/)
  assert.equal(fixture.popup.closed, true)
  assert.equal(fixture.listeners.size, 0)
})

test('source picture updates fill legacy blanks while preserving custom icons and explicit clears', () => {
  const image = 'a'.repeat(64), next = { ...route, image: 'b'.repeat(64), imageData: 'data:image/png;base64,VEVTVF9PTkxZ' }
  const icon = { itemType: 2, src: '/sunpanel/uploads/original.png', backgroundColor: 'transparent' }
  const linked = { title: route.title, url: route.url, lanUrl: route.lanUrl, icon, gateHome: routeLink({ ...route, image }, icon) }
  assert.equal(routeChanges(linked, next).find(row => row.field === 'image').preserved, false)
  assert.equal(routeChanges({ ...linked, icon: { ...icon, src: '/sunpanel/uploads/custom.png' } }, next).find(row => row.field === 'image').preserved, true)
  assert.equal(routeChanges({ ...linked, icon: null }, next).find(row => row.field === 'image').preserved, true)
  assert.equal(routeChanges({ ...linked, icon: null, gateHome: { routeId: route.id, title: route.title, url: route.url, lanUrl: route.lanUrl } }, next).find(row => row.field === 'image').preserved, false)
  assert.equal(JSON.stringify(routeLink(next, icon)).includes('base64'), false, 'preview image data must not enter the stored association')
  assert.equal(routeChanges(linked, { ...next, imageUnavailable: true }).some(row => row.field === 'image'), false)
})

test('missing images do not block website import and never advance the source baseline', async (t) => {
  const previous = globalThis.fetch
  t.after(() => { globalThis.fetch = previous })
  const source = { ...route, image: '/icons/uploads/missing.svg' }
  globalThis.fetch = async (url, options) => {
    assert.equal(url, '/api/sunpanel/routes/TEST_ONLY_ROUTE/image')
    assert.equal(options.credentials, 'same-origin')
    return new Response('', { status: 404 })
  }
  const [result] = await withRouteImages([source])
  assert.equal(result.imageUnavailable, true)
  assert.equal(routeLink(result).image, undefined)
  assert.equal(source.imageUnavailable, undefined)
  const controller = new AbortController()
  controller.abort()
  await assert.rejects(withRouteImages([source], controller.signal), { name: 'AbortError' })
})
