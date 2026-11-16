import assert from 'node:assert/strict'
import { randomBytes } from 'node:crypto'
import { mkdtemp, readFile, writeFile, rm } from 'node:fs/promises'
import { networkInterfaces, tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawn, spawnSync } from 'node:child_process'
import { createServer, request } from 'node:http'
import { createServer as createSecureServer } from 'node:https'
import { chromium } from 'playwright'

const repository = fileURLToPath(new URL('../../', import.meta.url))
const root = await mkdtemp(path.join(tmpdir(), 'gatehome-cross-domain-check-'))
const data = path.join(root, 'data')
const password = randomBytes(24).toString('hex')
let server, browser, gateway, page, leakedManagementCookie = false
const upstream = createServer((req, res) => { res.setHeader('Content-Type', 'text/html'); res.end('TEST_ONLY_UPSTREAM') })
const listen = app => new Promise(resolve => app.listen(0, '127.0.0.1', () => resolve(app.address().port)))
async function freePort() { const listener = createServer(); const port = await listen(listener); await new Promise(resolve => listener.close(resolve)); return port }
async function loginGateHome(target, origin) {
  if (!target.url().includes('/console/')) await target.goto(origin + '/console/#/routes')
  await target.locator('#admin-username').fill('admin')
  await target.locator('#admin-password').fill(password)
  await target.locator('.login-form button[type=submit]').click()
}
async function loginPanel(target, origin) {
  await target.goto(origin + '/sunpanel/#/login')
  await target.locator('.login-card input:not([type=password])').fill('browser-fixture@example.test')
  await target.locator('.login-card input[type=password]').fill(password)
  await target.locator('.login-card button[type=button]').filter({ hasText: /^登录$|^Login$/ }).click()
  await target.getByRole('button', { name: 'GateHome 联动', exact: true }).waitFor()
}
async function closeModal(target) { await target.locator('.n-modal .n-base-close').last().click() }
async function startConnection(target) {
  const opened = target.waitForEvent('popup')
  await target.getByRole('button', { name: /^(连接 GateHome|刷新反代列表)$/ }).click()
  return opened
}
async function finishedConnection(target) { if (!target.isClosed()) await target.waitForEvent('close') }
try {
  const initialized = spawnSync(process.env.GO || 'go', ['run', 'scripts/console_fixture.go', data], { cwd: repository, input: password, stdio: 'pipe', env: { ...process.env, CGO_ENABLED: '0' } })
  assert.equal(initialized.status, 0, 'GateHome fixture initialization failed')
  const [adminPort, panelPort, webPort] = await Promise.all([freePort(), freePort(), freePort()])
  const upstreamPort = await listen(upstream)
  const cert = path.join(root, 'certificate.pem'), key = path.join(root, 'key.pem')
  assert.equal(spawnSync('openssl', ['req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1', '-subj', '/CN=gatehome-console.test', '-keyout', key, '-out', cert], { stdio: 'ignore' }).status, 0, 'fixture TLS setup failed')
  gateway = createSecureServer({ cert: await readFile(cert), key: await readFile(key) }, (req, res) => {
    const hostname = req.headers.host?.split(':')[0]
    if (hostname === 'sunpanel-home.test' && (req.headers.cookie || '').includes('gatehouse_session=')) leakedManagementCookie = true
    const port = hostname === 'gatehome-console.test' ? adminPort : hostname === 'sunpanel-home.test' ? panelPort : 0
    if (!port) { res.writeHead(404); res.end(); return }
    const forwarded = request({ hostname: '127.0.0.1', port, path: req.url, method: req.method, headers: req.headers }, response => {
      res.writeHead(response.statusCode, response.headers); response.pipe(res)
    })
    forwarded.on('error', () => { res.writeHead(502); res.end() })
    req.pipe(forwarded)
  })
  const securePort = await listen(gateway)
  const adminOrigin = `https://gatehome-console.test:${securePort}`, panelOrigin = `https://sunpanel-home.test:${securePort}`
  const statePath = path.join(data, 'state.json')
  const state = JSON.parse(await readFile(statePath, 'utf8'))
  state.config.admin_access = { enabled: true, origins: [adminOrigin] }
  state.config.sunpanel = { enabled: true, port: panelPort, external_url: panelOrigin }
  state.config.groups = [{ id: 'fixture-web', name: 'Fixture', enabled: true, http_port: webPort, https_port: 0, domain_suffix: 'example.test' }]
  state.config.routes = [{ id: 'fixture-nas', group_id: 'fixture-web', name: 'Cross-domain NAS', host: 'nas.example.test', upstream: `http://127.0.0.1:${upstreamPort}/`, enabled: true, tls: false }]
  state.config.ddns.ip_monitor = { group_id: '', interface: Object.entries(networkInterfaces()).find(([, rows]) => rows.some(row => !row.internal))?.[0] || 'eth0', ipv4_source: 'interface', ipv6_source: 'interface' }
  await writeFile(statePath, JSON.stringify(state), { mode: 0o600 })
  server = spawn(path.resolve(repository, process.env.GATEHOME_TEST_BINARY || 'dist/gatehouse'), ['-data', data, '-admin', `127.0.0.1:${adminPort}`], { cwd: root, stdio: 'ignore' })
  let ready = false
  for (let attempt = 0; attempt < 200; attempt++) {
    try { ready = (await fetch(`http://127.0.0.1:${adminPort}/healthz`)).ok } catch {}
    if (ready) break
    await new Promise(resolve => setTimeout(resolve, 100))
  }
  assert.ok(ready, 'cross-domain fixture server did not start')
  const seeded = spawnSync(process.env.GO || 'go', ['run', 'scripts/sunpanel_fixture.go', root], { cwd: repository, input: password, stdio: 'pipe', env: { ...process.env, CGO_ENABLED: '0' } })
  assert.equal(seeded.status, 0, 'SunPanel fixture account initialization failed')
  browser = await chromium.launch({ ...(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : { channel: 'chromium' }),
    args: ['--host-resolver-rules=MAP gatehome-console.test 127.0.0.1,MAP sunpanel-home.test 127.0.0.1', '--no-proxy-server'] })
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1440, height: 1000 }, reducedMotion: 'reduce' })
  const errors = []
  context.on('page', target => {
    target.on('pageerror', () => errors.push('page exception'))
    target.on('console', message => { if (message.type() === 'error' && !/^Failed to load resource:.*status of (401|403)/.test(message.text())) errors.push('console error') })
  })
  const admin = await context.newPage()
  await admin.goto(adminOrigin + '/console/#/routes')
  await loginGateHome(admin, adminOrigin)
  await admin.locator('.routes-page').waitFor()
  page = await context.newPage()
  page.setDefaultTimeout(10000)
  await loginPanel(page, panelOrigin)
  await page.getByRole('button', { name: 'GateHome 联动', exact: true }).click()
  await page.getByRole('button', { name: '连接 GateHome', exact: true }).waitFor()
  const opened = page.waitForEvent('popup')
  await page.getByRole('button', { name: '连接 GateHome', exact: true }).click()
  const popup = await opened
  await popup.waitForEvent('close')
  await page.getByRole('button', { name: '刷新反代列表', exact: true }).waitFor()
  assert.equal(await page.getByRole('alert').filter({ hasText: '未登录' }).count(), 0)
  await page.getByLabel('批量选择反代项').click()
  await page.getByText('Fixture / Cross-domain NAS', { exact: true }).click()
  const imported = page.waitForResponse(response => new URL(response.url()).pathname === '/sunpanel/api/panel/itemIcon/gateHome/import')
  await page.getByRole('button', { name: '导入 1 个项目', exact: true }).click()
  const importResult = await (await imported).json()
  assert.equal(importResult.code, 0)
  assert.equal(importResult.data.created, 1)
  await page.waitForFunction(() => !document.querySelector('.n-modal'))
  await page.getByText('Cross-domain NAS', { exact: true }).first().waitFor()
  console.log('PASS SunPanel independent HTTPS domains: separate real logins, first-party management session, popup return and real bookmark import')

  const embedded = await context.newPage()
  await loginPanel(embedded, adminOrigin)
  await embedded.getByRole('button', { name: 'GateHome 联动', exact: true }).click()
  await embedded.getByRole('button', { name: '刷新反代列表', exact: true }).waitFor()
  assert.equal(await embedded.getByRole('button', { name: '连接 GateHome', exact: true }).count(), 0)
  await embedded.close()
  console.log('PASS SunPanel same-origin regression: existing GateHome session reads directly and shares the imported desktop')

  await page.getByRole('button', { name: /^(添加|Add) · Browser fixture$/ }).click()
  await page.getByText('从 GateHome 导入', { exact: true }).click()
  await page.getByRole('button', { name: '连接 GateHome', exact: true }).waitFor()
  await finishedConnection(await startConnection(page))
  await page.getByLabel('选择 GateHome 反代项', { exact: true }).click()
  await page.getByText(new RegExp(`^Fixture / Cross-domain NAS · http://nas.example.test:${webPort}/$`)).click()
  await page.getByRole('button', { name: '填入网站信息', exact: true }).click()
  assert.equal(await page.locator('.n-modal input').evaluateAll(inputs => inputs.some(input => input.value === 'Cross-domain NAS')), true)
  await closeModal(page)
  console.log('PASS SunPanel single-site import: the same cross-domain connection fills the real edit form')

  const configResult = await admin.evaluate(async () => (await fetch('/api/config')).json())
  configResult.config.routes[0].name = 'Updated NAS'
  configResult.config.routes[0].host = 'updated.example.test'
  configResult.config.routes[0].upstream = `http://localhost:${upstreamPort}`
  const changed = await admin.evaluate(async value => {
    const response = await fetch('/api/config', { method: 'PUT', headers: { 'Content-Type': 'application/json', 'X-Gatehouse-Request': '1' }, body: JSON.stringify(value) })
    return { status: response.status, error: response.ok ? '' : (await response.json()).error }
  }, { revision: configResult.revision, config: configResult.config })
  assert.equal(changed.status, 200, changed.error)
  await page.getByRole('button', { name: 'GateHome 联动', exact: true }).click()
  await page.getByRole('button', { name: '连接 GateHome', exact: true }).waitFor()
  await finishedConnection(await startConnection(page))
  await page.getByText('关联更新', { exact: true }).click()
  await page.getByRole('checkbox', { name: 'Cross-domain NAS', exact: true }).check()
  const synced = page.waitForResponse(response => new URL(response.url()).pathname === '/sunpanel/api/panel/itemIcon/gateHome/sync')
  await page.getByRole('button', { name: '应用 1 个项目的更新', exact: true }).click()
  const syncResult = await (await synced).json()
  assert.equal(syncResult.code, 0)
  assert.equal(syncResult.data.updated, 1)
  await page.waitForFunction(() => !document.querySelector('.n-modal'))
  await page.getByText('Updated NAS', { exact: true }).first().waitFor()
  console.log('PASS SunPanel linked updates: a fresh cross-domain read previews and applies real route changes')

  let hold
  await context.route('**/api/sunpanel/connect', async route => {
    if (hold) { const current = hold; current.arrived(); await current.gate }
    await route.continue().catch(() => {})
  })
  function holdResponse() {
    let release, arrived
    const gate = new Promise(resolve => { release = resolve })
    const arrival = new Promise(resolve => { arrived = resolve })
    hold = { gate, arrived }
    return { arrival, release: () => { hold = undefined; release() } }
  }
  await page.getByRole('button', { name: 'GateHome 联动', exact: true }).click()
  await page.getByRole('button', { name: '连接 GateHome', exact: true }).waitFor()
  for (const theme of ['light', 'dark']) {
    if (!await admin.locator('body').evaluate((body, value) => body.classList.contains('theme-' + value), theme))
      await admin.getByTitle(theme === 'dark' ? '切换为深色模式' : '切换为浅色模式').click()
    for (const width of [320, 390]) {
      await page.setViewportSize({ width, height: 844 })
      const paused = holdResponse()
      const connection = await startConnection(page)
      await paused.arrival
      await connection.setViewportSize({ width, height: 667 })
      await connection.locator('#connection-title').waitFor()
      assert.equal(await connection.locator('body').evaluate((body, value) => body.classList.contains('theme-' + value), theme), true)
      assert.equal(await connection.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), true)
      const bounds = await connection.getByRole('button', { name: '关闭连接窗口', exact: true }).boundingBox()
      assert.ok(bounds.height >= 44)
      if (process.env.GATEHOME_BROWSER_ARTIFACTS) {
        await connection.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, `sunpanel-connect-${width}-${theme}.png`), animations: 'disabled' })
      }
      const layout = await page.locator('.n-modal').evaluate(el => {
        const bounds = el.getBoundingClientRect(), body = el.querySelector('.n-card__content')
        return { left: bounds.left, right: bounds.right, top: bounds.top, bottom: bounds.bottom, width: innerWidth, height: innerHeight, scroll: body.scrollWidth, client: body.clientWidth }
      })
      assert.ok(layout.left >= -1 && layout.right <= layout.width + 1 && layout.top >= -1 && layout.bottom <= layout.height + 1 && layout.scroll <= layout.client + 1, JSON.stringify(layout))
      paused.release()
      await finishedConnection(connection)
      await page.getByRole('button', { name: '刷新反代列表', exact: true }).waitFor()
    }
  }
  console.log('PASS SunPanel mobile connection: 320px/390px, both themes, no overflow and 44px actions')

  const cancelled = holdResponse()
  const child = await startConnection(page)
  await cancelled.arrival
  await closeModal(page)
  await finishedConnection(child)
  cancelled.release()
  console.log('PASS SunPanel modal cleanup: closing the importer cancels its pending popup')

  assert.equal(await admin.evaluate(async () => (await fetch('/api/logout', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Gatehouse-Request': '1' }, body: '{}' })).status), 200)
  await page.getByRole('button', { name: 'GateHome 联动', exact: true }).click()
  await page.getByRole('button', { name: '连接 GateHome', exact: true }).waitFor()
  const reauthenticate = await startConnection(page)
  await reauthenticate.locator('#admin-username').waitFor()
  assert.equal(await page.getByRole('checkbox', { name: 'Updated NAS', exact: true }).count(), 0)
  await loginGateHome(reauthenticate, adminOrigin)
  await finishedConnection(reauthenticate)
  await page.getByRole('button', { name: '刷新反代列表', exact: true }).waitFor()
  console.log('PASS SunPanel expired management session: login resumes the original cross-domain request without relogging into SunPanel')

  await page.evaluate(() => { window.testOriginalOpen = window.open; window.open = () => null })
  await page.getByRole('button', { name: '刷新反代列表', exact: true }).click()
  await page.getByText('浏览器拦截了连接窗口，请允许弹出窗口后重试。', { exact: true }).waitFor()
  await page.evaluate(() => { window.open = window.testOriginalOpen; delete window.testOriginalOpen })
  assert.equal(leakedManagementCookie, false, 'management cookie crossed to the SunPanel domain')
  console.log('PASS SunPanel blocked-window recovery and credential boundary: actionable retry and no management cookie on the independent domain')
  await context.route('**/api/sunpanel/connection', route => route.fulfill({ status: 401, contentType: 'application/json', body: JSON.stringify({ error: 'SunPanel 登录已过期，请重新登录' }) }))
  await finishedConnection(await startConnection(page))
  await page.getByText('SunPanel 登录已过期，请重新登录', { exact: true }).waitFor()
  assert.equal(await page.getByRole('button', { name: '导入 0 个项目', exact: true }).isDisabled(), true)
  await context.unroute('**/api/sunpanel/connection')
  console.log('PASS SunPanel expired homepage session: management login alone cannot complete the connection')
  await closeModal(page)
  const guest = await browser.newContext({ ignoreHTTPSErrors: true })
  const guestPage = await guest.newPage()
  await guestPage.goto(panelOrigin + '/sunpanel/#/login')
  const guestStatus = await guestPage.evaluate(async () => (await fetch('/api/sunpanel/connection', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Gatehouse-Request': '1' }, body: '{}' })).status)
  assert.equal(guestStatus, 401)
  await guest.close()
  console.log('PASS SunPanel login boundary: a missing homepage session cannot discover or read the connection')
  const mobile = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, reducedMotion: 'reduce' })
  mobile.on('page', target => target.on('pageerror', () => errors.push('mobile page exception')))
  const mobileAdmin = await mobile.newPage()
  await mobileAdmin.goto(adminOrigin + '/console/#/routes')
  await loginGateHome(mobileAdmin, adminOrigin)
  await mobileAdmin.locator('.routes-page').waitFor()
  const mobileHome = await mobile.newPage()
  await loginPanel(mobileHome, panelOrigin)
  await mobileHome.getByRole('button', { name: 'GateHome 联动', exact: true }).tap()
  await mobileHome.getByRole('button', { name: '连接 GateHome', exact: true }).waitFor()
  const mobileOpened = mobileHome.waitForEvent('popup')
  await mobileHome.getByRole('button', { name: '连接 GateHome', exact: true }).tap()
  await finishedConnection(await mobileOpened)
  await mobileHome.getByRole('button', { name: '刷新反代列表', exact: true }).waitFor()
  await mobile.close()
  console.log('PASS SunPanel touch browser: independent logins and tap-initiated cross-domain connection return successfully')
  assert.deepEqual(errors, [], 'cross-domain pages emitted exceptions')
} catch (error) {
  console.error(String(error.stack || error.message).replaceAll(password, '<REDACTED>'))
  if (page) console.error(JSON.stringify(await page.evaluate(() => ({ path: location.pathname + location.hash.split('?')[0], headings: [...document.querySelectorAll('h1,h2')].map(el => el.textContent) })).catch(() => ({}))))
  process.exitCode = 1
} finally {
  await browser?.close()
  if (server && server.exitCode === null) { const stopped = new Promise(resolve => server.once('exit', resolve)); server.kill('SIGTERM'); await stopped }
  await Promise.all([gateway, upstream].filter(app => app?.listening).map(app => new Promise(resolve => app.close(resolve))))
  await rm(root, { recursive: true, force: true })
}
