import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import ts from 'typescript'

const source = readFileSync(new URL('../src/utils/network.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ES2022 } })
const { resolveAutoUrl, createAutoUrlResolver } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`)
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
