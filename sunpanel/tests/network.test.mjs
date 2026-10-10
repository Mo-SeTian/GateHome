import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import ts from 'typescript'

const source = readFileSync(new URL('../src/utils/network.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ES2022 } })
const { resolveAutoUrl, createAutoUrlResolver, startAutoUrlRefresh } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`)
const defaultUrl = 'https://example.test/'
const lanUrl = 'http://192.168.1.2:5000/'

test('missing, identical or unsupported LAN URLs use the default without probing', async (t) => {
  const probe = t.mock.method(globalThis, 'fetch', () => {
    throw new Error('unexpected probe')
  })
  for (const url of [undefined, '', defaultUrl, 'not a URL', 'javascript:void(0)', 'http://user:FAKE_PASSWORD@example.test/'])
    assert.equal(await resolveAutoUrl(defaultUrl, url), defaultUrl)
  assert.equal(probe.mock.callCount(), 0)
})

test('opaque and authentication responses prove LAN reachability', async (t) => {
  let response
  const probe = t.mock.method(globalThis, 'fetch', async () => response)
  for (response of [{ type: 'opaque', status: 0 }, { status: 401 }, { status: 405 }])
    assert.equal(await resolveAutoUrl(defaultUrl, lanUrl), lanUrl)
  const [url, options] = probe.mock.calls[0].arguments
  assert.equal(url, lanUrl)
  assert.equal(options.method, 'HEAD')
  assert.equal(options.mode, 'no-cors')
  assert.equal(options.cache, 'no-store')
  assert.equal(options.credentials, 'omit')
  assert.equal(options.referrerPolicy, 'no-referrer')
  assert.equal(options.redirect, 'follow')
})

test('network or browser policy failure uses the default URL', async (t) => {
  t.mock.method(globalThis, 'fetch', async () => {
    throw new TypeError('Failed to fetch')
  })
  assert.equal(await resolveAutoUrl(defaultUrl, lanUrl), defaultUrl)
})

test('a stalled probe falls back within 1500 ms and is aborted', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  let signal
  t.mock.method(globalThis, 'fetch', (_url, options) => {
    signal = options.signal
    return new Promise(() => {})
  })
  const chosen = resolveAutoUrl(defaultUrl, lanUrl)
  t.mock.timers.tick(1500)
  assert.equal(await chosen, defaultUrl)
  assert.equal(signal.aborted, true)
})

test('a successful probe cancels its timeout', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  let signal
  t.mock.method(globalThis, 'fetch', async (_url, options) => {
    signal = options.signal
    return { type: 'opaque' }
  })
  assert.equal(await resolveAutoUrl(defaultUrl, lanUrl), lanUrl)
  t.mock.timers.tick(1500)
  assert.equal(signal.aborted, false)
})

test('background checks populate cached choices; opening sites never starts or waits for a probe', async (t) => {
  let finish
  const probe = t.mock.method(globalThis, 'fetch', () => new Promise(resolve => { finish = resolve }))
  const network = createAutoUrlResolver()
  assert.equal(network.resolve(defaultUrl, lanUrl), defaultUrl)
  assert.equal(probe.mock.callCount(), 0)
  const checking = network.check([{ url: defaultUrl, lanUrl }])
  assert.equal(network.resolve(defaultUrl, lanUrl), defaultUrl)
  finish({ type: 'opaque' })
  await checking
  for (let i = 0; i < 3; i++)
    assert.equal(network.resolve(defaultUrl, lanUrl), lanUrl)
  assert.equal(probe.mock.callCount(), 1)
})

test('a batch checks each LAN address once across groups, duplicate entries and repeated loads', async (t) => {
  const unreachable = 'http://192.168.1.3:5000/'
  const probe = t.mock.method(globalThis, 'fetch', async (url) => {
    if (url === unreachable)
      throw new TypeError('Failed to fetch')
    return { type: 'opaque' }
  })
  const network = createAutoUrlResolver()
  const entries = [{ url: defaultUrl, lanUrl }, { url: `${defaultUrl}other`, lanUrl: ` ${lanUrl} ` }, { url: defaultUrl, lanUrl: unreachable }, { url: defaultUrl }, { url: lanUrl, lanUrl }]
  await Promise.all([network.check(entries), network.check(entries)])
  await network.check(entries)
  assert.equal(probe.mock.callCount(), 2)
  assert.equal(network.resolve(defaultUrl, lanUrl), lanUrl)
  assert.equal(network.resolve(`${defaultUrl}other`, ` ${lanUrl} `), lanUrl)
  assert.equal(network.resolve(defaultUrl, unreachable), defaultUrl)
  assert.equal(network.resolve(defaultUrl), defaultUrl)
})

test('new addresses are checked after edits; results belong to the current home page', async (t) => {
  const probe = t.mock.method(globalThis, 'fetch', async () => ({ type: 'opaque' }))
  const firstPage = createAutoUrlResolver()
  await firstPage.check([{ url: defaultUrl, lanUrl }])
  const changedLanUrl = 'http://192.168.1.4:5000/'
  assert.equal(firstPage.resolve(defaultUrl, changedLanUrl), defaultUrl)
  await firstPage.check([{ url: defaultUrl, lanUrl: changedLanUrl }])
  assert.equal(firstPage.resolve(defaultUrl, changedLanUrl), changedLanUrl)
  const nextPage = createAutoUrlResolver()
  assert.equal(nextPage.resolve(defaultUrl, lanUrl), defaultUrl)
  await nextPage.check([{ url: defaultUrl, lanUrl }])
  assert.equal(nextPage.resolve(defaultUrl, lanUrl), lanUrl)
  assert.equal(probe.mock.callCount(), 3)
})

test('expired results fall back immediately and a background recheck detects loss and recovery', async (t) => {
  let now = 1000
  let available = true
  t.mock.method(Date, 'now', () => now)
  const probe = t.mock.method(globalThis, 'fetch', async () => {
    if (!available)
      throw new TypeError('TEST_ONLY_NETWORK_LOSS')
    return { type: 'opaque' }
  })
  const network = createAutoUrlResolver()
  const items = [{ url: defaultUrl, lanUrl }]
  await network.check(items)
  now += 59_999
  assert.equal(network.resolve(defaultUrl, lanUrl), lanUrl)
  await network.check(items)
  assert.equal(probe.mock.callCount(), 1)
  now++
  available = false
  assert.equal(network.resolve(defaultUrl, lanUrl), defaultUrl)
  assert.equal(probe.mock.callCount(), 1)
  await network.check(items)
  assert.equal(network.resolve(defaultUrl, lanUrl), defaultUrl)
  now += 60_000
  available = true
  await network.check(items)
  assert.equal(network.resolve(defaultUrl, lanUrl), lanUrl)
  assert.equal(probe.mock.callCount(), 3)
})

test('a probe from the previous network cannot overwrite a newer unreachable result', async (t) => {
  let finishOld
  const probe = t.mock.method(globalThis, 'fetch', () => new Promise(resolve => { finishOld = resolve }))
  const network = createAutoUrlResolver()
  const items = [{ url: defaultUrl, lanUrl }]
  const oldCheck = network.check(items)
  network.invalidate()
  probe.mock.mockImplementation(async () => { throw new TypeError('TEST_ONLY_NETWORK_LOSS') })
  await network.check(items)
  finishOld({ type: 'opaque' })
  await oldCheck
  assert.equal(network.resolve(defaultUrl, lanUrl), defaultUrl)
  assert.equal(probe.mock.callCount(), 2)
})

function browserNetworkFixture(t) {
  const fixture = {
    document: Object.assign(new EventTarget(), { visibilityState: 'visible' }),
    window: new EventTarget(),
    navigator: { onLine: true, connection: new EventTarget() },
  }
  for (const [key, value] of Object.entries(fixture)) {
    const previous = Object.getOwnPropertyDescriptor(globalThis, key)
    Object.defineProperty(globalThis, key, { value, configurable: true })
    t.after(() => previous ? Object.defineProperty(globalThis, key, previous) : delete globalThis[key])
  }
  t.mock.timers.enable({ apis: ['setTimeout', 'setInterval'] })
  return fixture
}

test('foreground checks, periodic refresh and debounced reconnection run silently; cleanup stops them', async (t) => {
  const browser = browserNetworkFixture(t)
  const probe = t.mock.method(globalThis, 'fetch', async () => ({ type: 'opaque' }))
  const network = createAutoUrlResolver()
  const items = [{ url: defaultUrl, lanUrl }]
  await network.check(items)
  const stop = startAutoUrlRefresh(network, () => items, () => true)
  t.mock.timers.tick(60_000)
  t.mock.timers.tick(200)
  assert.equal(probe.mock.callCount(), 2)
  await network.check(items)
  browser.document.visibilityState = 'hidden'
  browser.document.dispatchEvent(new Event('visibilitychange'))
  t.mock.timers.tick(60_000)
  assert.equal(probe.mock.callCount(), 2)
  browser.navigator.onLine = false
  browser.window.dispatchEvent(new Event('offline'))
  assert.equal(network.resolve(defaultUrl, lanUrl), defaultUrl)
  t.mock.timers.tick(200)
  assert.equal(probe.mock.callCount(), 2)
  browser.navigator.onLine = true
  browser.document.visibilityState = 'visible'
  browser.window.dispatchEvent(new Event('online'))
  browser.navigator.connection.dispatchEvent(new Event('change'))
  t.mock.timers.tick(199)
  assert.equal(probe.mock.callCount(), 2)
  t.mock.timers.tick(1)
  assert.equal(probe.mock.callCount(), 3)
  await network.check(items)
  browser.document.dispatchEvent(new Event('visibilitychange'))
  t.mock.timers.tick(200)
  assert.equal(probe.mock.callCount(), 4)
  await network.check(items)
  const restored = Object.assign(new Event('pageshow'), { persisted: true })
  browser.window.dispatchEvent(restored)
  browser.document.dispatchEvent(new Event('visibilitychange'))
  t.mock.timers.tick(200)
  assert.equal(probe.mock.callCount(), 5)
  await network.check(items)
  stop()
  browser.window.dispatchEvent(new Event('online'))
  browser.document.dispatchEvent(new Event('visibilitychange'))
  browser.navigator.connection.dispatchEvent(new Event('change'))
  browser.window.dispatchEvent(restored)
  t.mock.timers.tick(120_000)
  assert.equal(probe.mock.callCount(), 5)
})

test('manual network modes never start automatic probes, including browsers without connection events', (t) => {
  const browser = browserNetworkFixture(t)
  delete browser.navigator.connection
  const probe = t.mock.method(globalThis, 'fetch', async () => ({ type: 'opaque' }))
  const stop = startAutoUrlRefresh(createAutoUrlResolver(), () => [{ url: defaultUrl, lanUrl }], () => false)
  browser.document.dispatchEvent(new Event('visibilitychange'))
  browser.window.dispatchEvent(new Event('online'))
  t.mock.timers.tick(120_000)
  assert.equal(probe.mock.callCount(), 0)
  stop()
})
