import assert from 'node:assert/strict'
import { randomBytes } from 'node:crypto'
import { mkdtemp, readFile, writeFile, rm } from 'node:fs/promises'
import { networkInterfaces, tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawn, spawnSync } from 'node:child_process'
import { createServer } from 'node:http'
import { chromium } from 'playwright'

const repository = fileURLToPath(new URL('../../', import.meta.url))
const root = await mkdtemp(path.join(tmpdir(), 'gatehome-console-check-'))
const secret = randomBytes(24).toString('hex')
const upstream = createServer((req, res) => {
  if (req.url !== '/') { res.writeHead(404); res.end(); return }
  res.setHeader('Content-Type', 'text/html')
  res.end('<html><head><title>Console test upstream</title></head><body>TEST_ONLY_UPSTREAM</body></html>')
})
let server, browser
const listen = (app) => new Promise(resolve => app.listen(0, '127.0.0.1', () => resolve(app.address().port)))
const pause = (ms) => new Promise(resolve => setTimeout(resolve, ms))
try {
  const initialized = spawnSync(process.env.GO || 'go', ['run', 'scripts/console_fixture.go', root], {
    cwd: repository, input: secret, stdio: ['pipe', 'pipe', 'pipe'], env: { ...process.env, CGO_ENABLED: '0' }
  })
  assert.equal(initialized.status, 0, 'disposable fixture initialization failed')
  const statePath = path.join(root, 'state.json')
  const state = JSON.parse(await readFile(statePath, 'utf8'))
  state.config.groups = [
    { id: 'test-https', name: 'HTTPS 测试组', enabled: false, http_port: 0, https_port: 29443, domain_suffix: 'example.test' },
    { id: 'test-http', name: 'HTTP 测试组', enabled: false, http_port: 29080, https_port: 0, domain_suffix: 'example.net' }
  ]
  state.config.firewalls = [{ id: 'test-policy', name: '测试来源策略', default_action: 'allow', groups: [] }]
  state.config.ddns.ip_monitor = { group_id: '', interface: Object.entries(networkInterfaces()).find(([, rows]) => rows.some(row => !row.internal))?.[0] || 'eth0', ipv4_source: 'interface', ipv6_source: 'interface' }
  await writeFile(statePath, JSON.stringify(state), { mode: 0o600 })
  const upstreamPort = await listen(upstream)
  const reservation = createServer()
  const port = await listen(reservation)
  await new Promise(resolve => reservation.close(resolve))
  server = spawn(path.resolve(repository, process.env.GATEHOME_TEST_BINARY || 'dist/gatehouse'), [
    '-data', root, '-admin', `127.0.0.1:${port}`
  ], { cwd: root, stdio: ['ignore', 'ignore', 'pipe'] })
  let startup = ''
  server.stderr.on('data', data => { startup += data.toString() })
  const origin = `http://gatehome-console.test:${port}`
  for (let attempt = 0; ; attempt++) {
    try { if ((await fetch(`http://127.0.0.1:${port}/healthz`)).ok) break } catch {}
    if (attempt === 99) throw new Error('test server did not become healthy: ' + startup)
    await pause(100)
  }
  browser = await chromium.launch({
    ...(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {}),
    args: ['--host-resolver-rules=MAP gatehome-console.test 127.0.0.1', '--no-proxy-server']
  })
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, reducedMotion: 'reduce' })
  const page = await context.newPage()
  page.setDefaultTimeout(10000)
  const errors = []
  let checkingConflict = false
  page.on('response', response => {
    if (response.status() < 400) return
    const path = new URL(response.url()).pathname
    if (path === '/api/account' && response.status() === 401) return
    if (checkingConflict && path === '/api/config' && response.status() === 400) return
    errors.push(`HTTP ${response.status()} ${path}`)
  })
  page.on('pageerror', () => errors.push('page exception'))
  page.on('console', message => { if (message.type() === 'error' && !/^Failed to load resource:.*status of (400|401)/.test(message.text())) errors.push(message.text().replaceAll(secret, '<REDACTED>')) })
  await page.goto(origin + '/console/#/routes')
  await page.locator('#admin-username').fill('admin')
  await page.locator('#admin-password').fill(secret)
  await page.locator('.login-form button[type=submit]').click()
  await page.locator('.routes-page').waitFor()
  assert.equal(await page.evaluate(() => isSecureContext), false)
  assert.equal(await page.evaluate(() => typeof crypto.randomUUID), 'undefined')
  const dialog = () => page.locator('dialog[open]')
  const button = name => page.getByRole('button', { name, exact: true })
  async function api(endpoint, body) {
    const response = await page.evaluate(async ({ endpoint, body }) => {
      const response = await fetch('/api/' + endpoint, body === undefined ? {} : {
        method: 'PUT', headers: { 'Content-Type': 'application/json', 'X-Gatehouse-Request': '1' }, body: JSON.stringify(body)
      })
      if (!response.ok) throw new Error('fixture API request failed')
      return response.json()
    }, { endpoint, body })
    return response
  }
  async function route(name) {
    await page.goto(origin + '/console/#/' + name)
    await page.locator('.page-viewport h1').waitFor()
  }
  async function closeEditor() {
    await dialog().getByRole('button', { name: /关闭/ }).first().click()
    await page.waitForFunction(() => !document.querySelector('dialog[open]'))
  }
  for (const [view, label] of [
    ['routes', '新建反代组'], ['routes', '新建反代服务'], ['forwards', '添加转发规则'],
    ['subscriptions', '添加 IP 订阅'], ['ddns', '添加 DDNS 组'],
    ['certificates', '添加证书任务'], ['access', '添加防火墙策略']
  ]) {
    await route(view); await button(label).click(); await dialog().waitFor(); await closeEditor()
  }
  console.log('PASS ordinary HTTP: all seven configuration editors open without randomUUID')

  await route('routes'); await button('新建反代服务').click()
  assert.equal(await dialog().locator('[name=tls]').isChecked(), true)
  await dialog().locator('[name=group_id]').selectOption('test-http')
  assert.equal(await dialog().locator('[name=tls]').isChecked(), false)
  await dialog().locator('[name=group_id]').selectOption('test-https')
  assert.equal(await dialog().locator('[name=tls]').isChecked(), true)
  await dialog().locator('[name=name]').fill('Console review route')
  await dialog().locator('[name=host]').fill('app.home')
  await dialog().locator('[name=upstream]').fill(`http://127.0.0.1:${upstreamPort}`)
  await dialog().locator('[name=auth_enabled]').check()
  await dialog().locator('[name=auth_username]').fill('test-visitor')
  await dialog().locator('[name=route_password]').fill(secret)
  await dialog().getByRole('button', { name: '保存路由', exact: true }).click()
  await page.waitForFunction(() => !document.querySelector('dialog[open]'))
  let saved = await api('config')
  const created = saved.config.routes.find(row => row.name === 'Console review route')
  assert.equal(created.host, 'app.home.example.test'); assert.equal(created.tls, true)
  assert.match(created.id, /^[0-9a-f-]{36}$/)
  assert.equal(saved.route_passwords_configured[created.group_id + '/' + created.host], true)
  await button('编辑').last().click()
  await dialog().locator('[name=auth_enabled]').uncheck()
  await dialog().locator('[name=clear_route_password]').check()
  await dialog().getByRole('button', { name: '保存路由', exact: true }).click()
  await page.waitForFunction(() => !document.querySelector('dialog[open]'))
  saved = await api('config')
  assert.equal(saved.route_passwords_configured[created.group_id + '/' + created.host], false)
  await page.getByRole('link', { name: '查看流量', exact: true }).click()
  await page.waitForFunction(id => document.querySelector('select[name=rule]')?.value === id, created.id)
  console.log('PASS real HTTP save: nested prefix, group TLS defaults, password clear and route traffic link')

  await route('routes'); await button('新建反代服务').click()
  await dialog().locator('[name=full_host]').check()
  await dialog().locator('[name=host]').fill('outside.example.org')
  assert.match(await dialog().textContent(), /保存为 outside\.example\.org/)
  await closeEditor()
  await button('发现服务').click()
  await dialog().locator('.scan-fields input').nth(0).fill('127.0.0.1')
  await dialog().locator('.scan-fields input').nth(1).fill(String(upstreamPort))
  await dialog().getByRole('button', { name: '开始扫描', exact: true }).click()
  await dialog().locator('.scan-result').waitFor()
  await dialog().locator('.scan-result input[type=checkbox]').first().check()
  await dialog().locator('[name=service_name]').fill('Console imported route')
  await dialog().locator('.scan-result .form-input').nth(1).fill('scan.home')
  await dialog().locator('[name=firewall_id]').selectOption('test-policy')
  await dialog().getByRole('button', { name: '创建所选反代路由', exact: true }).click()
  await page.waitForFunction(() => !document.querySelector('dialog[open]'))
  saved = await api('config')
  const imported = saved.config.routes.find(row => row.name === 'Console imported route')
  assert.equal(imported.firewall_id, 'test-policy'); assert.equal(imported.host, 'scan.home.example.test')
  assert.equal(imported.tls, true)
  console.log('PASS real discovery scan/import: selected firewall, editable name and nested host retained')

  await route('forwards'); await button('添加转发规则').click()
  await dialog().locator('[name=name]').fill('Conflict draft')
  await dialog().locator('[name=listen_port]').fill('29389')
  await dialog().locator('[name=upstream]').fill('127.0.0.1:9000')
  await dialog().locator('[name=enabled]').uncheck()
  saved = await api('config'); await api('config', { revision: saved.revision, config: saved.config })
  checkingConflict = true
  await dialog().getByRole('button', { name: '保存转发规则', exact: true }).click()
  await dialog().getByRole('alert').waitFor()
  checkingConflict = false
  assert.equal(await dialog().locator('[name=name]').inputValue(), 'Conflict draft')
  await closeEditor()
  console.log('PASS concurrent revision rejection preserves the editor draft')

  const trigger = page.locator('.cmdk-trigger-btn')
  await trigger.click()
  const input = page.locator('.cmdk-input')
  for (let n = 0; n < 10; n++) await input.press('ArrowDown')
  await input.fill('概览'); await input.press('Enter')
  await page.waitForFunction(() => location.hash === '#/overview' && !document.querySelector('dialog[open]'))
  await trigger.click()
  for (let n = 0; n < 8; n++) {
    await page.keyboard.press('Tab')
    assert.equal(await page.evaluate(() => document.activeElement === document.body || Boolean(document.activeElement.closest('dialog[open]'))), true, 'Tab focused background content')
  }
  await page.keyboard.press('Escape')
  await page.waitForFunction(() => !document.querySelector('dialog[open]'))
  assert.equal(await trigger.evaluate(el => el === document.activeElement), true, 'Escape did not restore focus to the command trigger')
  console.log('PASS command palette: filtered Enter, native focus boundary and Escape focus restoration')
  await page.setViewportSize({ width: 390, height: 844 })
  await button('打开侧边栏导航').click()
  await button('关闭侧边栏导航').click()
  await pause(350)
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth === innerWidth), true)
  assert.deepEqual(errors, [], 'browser emitted errors during regression')
  console.log('PASS mobile layout and zero browser errors; disposable fixture cleaned')
} catch (error) {
  console.error(String(error.message).replaceAll(secret, '<REDACTED>'))
  process.exitCode = 1
} finally {
  await browser?.close()
  if (server && server.exitCode === null) {
    const stopped = new Promise(resolve => server.once('exit', resolve))
    server.kill('SIGTERM'); await stopped
  }
  await new Promise(resolve => upstream.close(resolve))
  await rm(root, { recursive: true, force: true })
}
