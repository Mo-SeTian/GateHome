import assert from 'node:assert/strict'
import { randomBytes } from 'node:crypto'
import { mkdir, mkdtemp, readFile, writeFile, rm } from 'node:fs/promises'
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
async function startServer(port) {
  server = spawn(path.resolve(repository, process.env.GATEHOME_TEST_BINARY || 'dist/gatehouse'), [
    '-data', root, '-admin', `127.0.0.1:${port}`
  ], { cwd: root, stdio: ['ignore', 'ignore', 'pipe'] })
  let startup = ''
  server.stderr.on('data', data => { startup += data.toString() })
  for (let attempt = 0; ; attempt++) {
    try { if ((await fetch(`http://127.0.0.1:${port}/healthz`)).ok) break } catch {}
    if (attempt === 99) throw new Error('test server did not become healthy: ' + startup)
    await pause(100)
  }
}
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
  const iconPack = path.join(root, 'icons', 'imported', 'BrowserFixture', 'svg')
  await mkdir(iconPack, { recursive: true })
  const fixtureSVG = i => `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48"><rect width="48" height="48" rx="10" fill="hsl(${i * 23 % 360} 57% 42%)"/><path d="M12 24h24m-12-12v24" stroke="#fff" stroke-width="3"/></svg>`
  await Promise.all(Array.from({ length: 60 }, (_, i) =>
    writeFile(path.join(iconPack, `fixture-service-${String(i).padStart(2, '0')}.svg`), fixtureSVG(i))))
  const upstreamPort = await listen(upstream)
  const reservation = createServer()
  const port = await listen(reservation)
  await new Promise(resolve => reservation.close(resolve))
  const origin = `http://gatehome-console.test:${port}`
  await startServer(port)
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

  await route('settings')
  const accountTrigger = button('修改账号与密码')
  await accountTrigger.click()
  const accountDialog = page.locator('dialog.account-dialog-card[open]')
  await accountDialog.waitFor()
  if (process.env.GATEHOME_BROWSER_ARTIFACTS) {
    await mkdir(process.env.GATEHOME_BROWSER_ARTIFACTS, { recursive: true })
    await page.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, 'account-desktop.png') })
  }
  assert.equal(await page.locator('#account-current-password').evaluate(el => el === document.activeElement), true, 'account current password did not receive focus')
  for (const key of ['Tab', 'Shift+Tab']) {
    for (let n = 0; n < 10; n++) {
      await page.keyboard.press(key)
      assert.equal(await page.evaluate(() => document.activeElement === document.body || Boolean(document.activeElement.closest('dialog.account-dialog-card[open]'))), true, 'account focus moved to the background')
    }
  }
  await page.locator('.app-sidebar a').first().evaluate(el => el.focus())
  assert.equal(await page.evaluate(() => !document.activeElement.closest('.app-sidebar')), true, 'modal background was not inert')
  await page.keyboard.press('Escape')
  await page.waitForFunction(() => !document.querySelector('dialog[open]'))
  assert.equal(await accountTrigger.evaluate(el => el === document.activeElement), true, 'account Escape did not restore the settings trigger')
  await page.locator('.user-profile-badge').click()
  await button('修改管理账户密码').click()
  await accountDialog.waitFor()
  await accountDialog.getByRole('button', { name: '取消', exact: true }).click()
  await page.waitForFunction(() => !document.querySelector('dialog[open]'))
  assert.equal(await page.locator('.user-profile-badge').evaluate(el => el === document.activeElement), true, 'account menu close did not restore the profile trigger')
  await trigger.click()
  await input.fill('修改管理员账户密码')
  await input.press('Enter')
  await accountDialog.waitFor()
  await page.keyboard.press('Escape')
  await page.waitForFunction(() => !document.querySelector('dialog[open]'))
  assert.equal(await trigger.evaluate(el => el === document.activeElement), true, 'account command action did not restore the command trigger')
  console.log('PASS administrator modal: Tab/Shift+Tab boundary, inert background and focus restoration from settings/menu/command palette')

  await route('mcp')
  await button('创建客户端').click()
  await dialog().waitFor()
  assert.equal(await dialog().locator('.permission-grid input[type=checkbox]').count(), 6)
  assert.equal(await dialog().locator('[name=read]').isChecked(), true)
  assert.equal(await dialog().locator('[name=write]').isChecked(), false)
  assert.equal(await dialog().locator('[name=security_write]').isDisabled(), true)
  await dialog().locator('[name=name]').fill('Console scoped client')
  await dialog().locator('[name=sunpanel_write]').check()
  await dialog().getByRole('button', { name: '保存客户端', exact: true }).click()
  await page.waitForFunction(() => !document.querySelector('dialog[open]'))
  let clients = await api('mcp')
  let scoped = clients.clients.find(row => row.name === 'Console scoped client')
  assert.equal(scoped.config.sunpanel_write, true)
  assert.equal(scoped.config.write, false)
  assert.equal(scoped.config.security_write, false)
  await button('关闭凭据').click()
  const clientRow = () => page.locator(`[data-client-id="${scoped.id}"]`)
  await clientRow().getByRole('button', { name: '编辑', exact: true }).click()
  await dialog().locator('[name=write]').check()
  await dialog().locator('[name=security_write]').check()
  await dialog().locator('[name=sunpanel_write]').uncheck()
  await dialog().getByRole('button', { name: '保存客户端', exact: true }).click()
  await page.waitForFunction(() => !document.querySelector('dialog[open]'))
  clients = await api('mcp')
  scoped = clients.clients.find(row => row.id === scoped.id)
  assert.equal(scoped.config.sunpanel_write, false)
  assert.equal(scoped.config.write, true)
  assert.equal(scoped.config.security_write, true)
  console.log('PASS MCP scoped client: read-only defaults, independent panel/security grants and real saves')

  const globalGrant = page.locator('.mcp-layout .mcp-permissions').first()
  await globalGrant.locator('[name=enabled]').check()
  await globalGrant.locator('[name=read]').uncheck()
  await globalGrant.locator('[name=write]').uncheck()
  await globalGrant.locator('[name=maintenance]').uncheck()
  await globalGrant.locator('[name=sunpanel_write]').check()
  const panelOnlySaved = page.waitForResponse(response => response.url().endsWith('/api/mcp') && response.request().method() === 'PUT')
  await button('保存授权设置').click()
  assert.equal((await panelOnlySaved).status(), 200)
  const globalSettings = await api('mcp')
  assert.equal(globalSettings.config.sunpanel_write, true)
  assert.equal(globalSettings.config.read, false)
  await page.goto('about:blank')
  const stopped = new Promise(resolve => server.once('exit', resolve))
  server.kill('SIGTERM')
  await stopped
  await startServer(port)
  await page.goto(origin + '/console/#/settings')
  await page.locator('#admin-username').fill('admin')
  await page.locator('#admin-password').fill(secret)
  await page.locator('.login-form button[type=submit]').click()
  await page.locator('.settings-layout').waitFor()
  console.log('PASS panel-only global grant: real form save, process shutdown/startup and original account login')

  await route('settings')
  const proxyField = page.locator('[name=trusted_proxies]')
  await proxyField.fill('192.0.2.10/32\n2001:db8:1::/64')
  const proxySaved = page.waitForResponse(response => response.url().endsWith('/api/config') && response.request().method() === 'PUT')
  await button('保存全局配置').click()
  assert.equal((await proxySaved).status(), 200)
  saved = await api('config')
  assert.deepEqual(saved.config.trusted_proxies, ['192.0.2.10/32', '2001:db8:1::/64'])
  await proxyField.fill('')
  const proxyCleared = page.waitForResponse(response => response.url().endsWith('/api/config') && response.request().method() === 'PUT')
  await button('保存全局配置').click()
  assert.equal((await proxyCleared).status(), 200)
  assert.deepEqual((await api('config')).config.trusted_proxies || [], [])
  console.log('PASS trusted proxy settings: IPv4/IPv6 save and clear use the real config API')

  await route('subscriptions')
  await button('添加 IP 订阅').waitFor()
  await pause(250)
  let statusCalls = 0
  const pendingStatus = new Set()
  const countStatus = request => {
    if (new URL(request.url()).pathname === '/api/status') {
      statusCalls++
      pendingStatus.add(request)
    }
  }
  const finishStatus = request => pendingStatus.delete(request)
  page.on('request', countStatus)
  page.on('requestfinished', finishStatus)
  page.on('requestfailed', finishStatus)
  await pause(11000)
  assert.ok(statusCalls >= 1 && statusCalls <= 2, 'App and page duplicated status polling')
  for (let attempt = 0; pendingStatus.size && attempt < 150; attempt++) await pause(100)
  assert.equal(pendingStatus.size, 0, 'status request did not settle')
  await page.evaluate(() => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  const beforeHidden = statusCalls
  await pause(5500)
  assert.equal(statusCalls, beforeHidden, 'hidden page continued polling')
  await page.evaluate(() => {
    delete document.visibilityState
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await pause(250)
  assert.equal(statusCalls, beforeHidden + 1, 'visible page did not refresh immediately')
  page.off('request', countStatus)
  page.off('requestfinished', finishStatus)
  page.off('requestfailed', finishStatus)
  console.log('PASS shared status polling: at most two requests in 11 seconds, hidden pause and visible refresh')

  for (const width of [320, 390]) {
    await page.setViewportSize({ width, height: 844 })
    for (const [view, label] of [
      ['routes', '新建反代服务'], ['forwards', '添加转发规则'],
      ['subscriptions', '添加 IP 订阅'], ['ddns', '添加 DDNS 组'],
      ['certificates', '添加证书任务'], ['access', '添加防火墙策略'], ['mcp', '创建客户端']
    ]) {
      await route(view)
      await button(label).click()
      await dialog().waitFor()
      assert.equal(await dialog().evaluate(el => {
        const bounds = el.getBoundingClientRect()
        return bounds.left >= 0 && bounds.right <= innerWidth + 1
      }), true, `${view} dialog overflowed ${width}px viewport`)
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), true)
      if (view === 'routes') {
        assert.equal((await dialog().locator('input[type=file]').getAttribute('accept')).includes('svg'), false)
      }
      if (view === 'mcp') {
        const gridColumns = await dialog().locator('.permission-grid').evaluate(el => getComputedStyle(el).gridTemplateColumns.split(' ').length)
        assert.equal(gridColumns, 2)
        assert.equal(await dialog().locator('.permission-card').first().evaluate(el => el.getBoundingClientRect().height), 44)
      }
      if (view === 'ddns') {
        const sourceFields = await dialog().evaluate(el =>
          ['interface', 'ipv4_source', 'ipv6_source', 'ipv4_urls', 'ipv6_urls'].map(name => {
            const label = el.querySelector(`[name="${name}"]`).closest('label')
            const style = getComputedStyle(label)
            return { explicit: label.classList.contains('form-field'), display: style.display, gap: style.gap }
          }))
        assert.ok(sourceFields.every(item => item.explicit && item.display === 'flex' && item.gap !== 'normal'))
      }
      if (process.env.GATEHOME_BROWSER_ARTIFACTS && ['routes', 'access', 'mcp', 'ddns'].includes(view)) {
        await mkdir(process.env.GATEHOME_BROWSER_ARTIFACTS, { recursive: true })
        await page.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, `${view}-${width}.png`) })
      }
      await closeEditor()
    }
  }
  console.log('PASS 320px/390px editors: explicit field layouts, single checkboxes, image formats and no overflow')

  await page.setViewportSize({ width: 320, height: 844 })
  await route('mcp')
  const emulation = await context.newCDPSession(page)
  await emulation.send('Emulation.setSafeAreaInsetsOverride', { insets: { top: 44, bottom: 34, left: 12, right: 12 } })
  await button('保存授权设置').click()
  const toastItem = page.locator('.toast-item').filter({ hasText: 'MCP 设置已保存' }).last()
  await toastItem.waitFor()
  await page.waitForFunction(() => {
    const toast = document.querySelector('.toast-item:last-child')
    return toast && Number(getComputedStyle(toast).opacity) === 1
  })
  // Stress the real toast's text box without storing artificial content.
  await toastItem.locator('.toast-text').evaluate(el => { el.textContent = 'TEST_ONLY_LONG_NOTIFICATION_'.repeat(80) })
  const toastBounds = await page.locator('.toast-container').boundingBox()
  assert.ok(toastBounds.x >= 24 && toastBounds.x + toastBounds.width <= 296 + 1)
  assert.ok(toastBounds.y >= 56 && toastBounds.y + toastBounds.height <= 798 + 1)
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
  if (process.env.GATEHOME_BROWSER_ARTIFACTS) {
    await page.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, 'toast-safe-area-320.png') })
  }
  await emulation.send('Emulation.setSafeAreaInsetsOverride', { insets: {} })
  await emulation.detach()
  console.log('PASS mobile toast: emulated notch/gesture insets, long text wrapping and bounded scroll area')

  await page.setViewportSize({ width: 390, height: 844 })
  await button('打开侧边栏导航').click()
  await button('关闭侧边栏导航').click()
  await pause(350)
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth === innerWidth), true)

  await route('settings')
  await button('修改账号与密码').click()
  await accountDialog.waitFor()
  if (process.env.GATEHOME_BROWSER_ARTIFACTS) {
    await page.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, 'account-mobile.png') })
  }
  await page.locator('#account-username').fill('test-admin-renamed')
  await page.locator('#account-current-password').fill(secret)
  await accountDialog.getByRole('button', { name: '保存管理账户', exact: true }).click()
  await page.locator('#admin-username').waitFor()
  await page.locator('#admin-username').fill('test-admin-renamed')
  await page.locator('#admin-password').fill(secret)
  await page.locator('.login-form button[type=submit]').click()
  await page.locator('.app-layout').waitFor()
  assert.equal((await api('account')).username, 'test-admin-renamed')
  console.log('PASS administrator account save: original password retained, session revoked and new username can log in')

  await page.setViewportSize({ width: 1440, height: 1000 })
  for (const view of [
    'overview', 'routes', 'forwards', 'sunpanel', 'ddns', 'certificates', 'subscriptions', 'access',
    'firewall-logs', 'logs', 'ip-blocks', 'statistics', 'traffic', 'mcp', 'updates', 'backups', 'settings'
  ]) {
    await route(view)
    await page.waitForFunction(view => document.querySelector('.sidebar-nav a.active')?.hash === '#/' + view, view)
  }
  await page.getByRole('button', { name: '关闭此通知' }).evaluateAll(buttons => buttons.forEach(button => button.click()))
  await page.getByRole('link', { name: '图标库', exact: true }).click()
  await page.locator('.icons-page h1').waitFor()
  const cards = page.locator('.icons-grid .icon-tile')
  const iconSearch = page.getByRole('searchbox', { name: '搜索图标' })
  await page.waitForFunction(() => document.querySelectorAll('.icons-grid .icon-tile').length === 72)
  assert.equal(await page.locator('.icons-grid').evaluate(el => getComputedStyle(el).gridTemplateColumns.split(' ').length), 12)
  assert.deepEqual(await cards.first().evaluate(el => [...el.children].map(child => child.tagName)), ['IMG', 'SPAN'])
  await cards.first().locator('img').evaluate(img => img.decode())
  if (process.env.GATEHOME_BROWSER_ARTIFACTS) {
    await page.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, 'icons-dark-1440.png') })
  }
  await page.getByTitle('切换为浅色模式').click()
  await page.waitForFunction(() => document.body.classList.contains('theme-light') && getComputedStyle(document.body).backgroundColor === 'rgb(241, 245, 249)')
  assert.equal(await page.locator('.icons-storage code').evaluate(el => getComputedStyle(el).color), 'rgb(79, 70, 229)')
  if (process.env.GATEHOME_BROWSER_ARTIFACTS) {
    await page.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, 'icons-light-1440.png') })
  }
  await iconSearch.fill('wangguan')
  await cards.getByText('gateway', { exact: true }).waitFor()
  assert.equal(await cards.count(), 1)
  await cards.first().click()
  const detail = page.getByRole('dialog', { name: '图标详情' })
  await detail.getByText('/icons/builtin/gateway.svg', { exact: true }).waitFor()
  assert.match(await detail.getByLabel('访问 URL').inputValue(), /\/api\/icons\/file\/builtin\/gateway\.svg$/)
  await detail.getByRole('button', { name: '关闭详情' }).click()

  await button('从 GitHub 导入').click()
  const importDialog = page.getByRole('dialog', { name: '从 GitHub 导入' })
  assert.equal(await importDialog.getByLabel('GitHub 仓库').inputValue(), 'https://github.com/xushier/HD-Icons')
  await importDialog.getByLabel('下载方式').selectOption('ghproxy')
  if (process.env.GATEHOME_REAL_ICON_IMPORT === '1') {
    await importDialog.getByLabel('下载方式').selectOption('direct')
    await importDialog.getByRole('button', { name: '开始导入' }).click()
    await page.getByText('图标导入完成', { exact: true }).waitFor({ timeout: 180000 })
    await iconSearch.fill('guodongyingyin')
    await cards.filter({ hasText: /jellyfin/i }).first().waitFor()
    const counts = await api('icons/stats')
    assert.ok(counts.sources.imported > 1000)
    if (process.env.GATEHOME_BROWSER_ARTIFACTS) {
      const importedSource = page.getByRole('group', { name: '来源筛选' }).getByRole('button', { name: /^GitHub 导入/ })
      await importedSource.click()
      await page.getByRole('button', { name: '圆角方块', exact: true }).click()
      await iconSearch.fill('')
      await page.waitForFunction(() => {
        const images = [...document.querySelectorAll('.icons-grid .icon-tile img')].slice(0, 48)
        return images.length === 48 && images.every(img => img.src.includes('/HD-Icons/') && img.naturalWidth)
      })
      await page.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, 'icons-hd-icons-light-1440.png') })
    }
    console.log(`PASS live GitHub import: ${counts.sources.imported - 60} HD-Icons and Chinese aliases`)
  } else {
    await importDialog.getByRole('button', { name: '取消', exact: true }).click()
  }

  await button('上传本地图标').click()
  const upload = page.getByRole('dialog', { name: '上传本地图标' })
  await upload.getByLabel('选择图标文件').setInputFiles([
    { name: '测试图标.svg', mimeType: 'image/svg+xml', buffer: Buffer.from(fixtureSVG(7)) },
    { name: 'delete-fixture.svg', mimeType: 'image/svg+xml', buffer: Buffer.from(fixtureSVG(12)) }
  ])
  await upload.getByRole('button', { name: '上传图标', exact: true }).click()
  await cards.getByText('测试图标', { exact: true }).waitFor()
  await iconSearch.fill('delete-fixture')
  await cards.getByText('delete-fixture', { exact: true }).click()
  await detail.getByRole('button', { name: '删除图标', exact: true }).click()
  await detail.getByRole('button', { name: '确认删除', exact: true }).click()
  await page.getByText('没有符合条件的图标', { exact: true }).waitFor()

  await route('routes')
  await button('编辑').first().click()
  const routeDialog = page.getByRole('dialog', { name: '编辑反向代理路由' })
  await routeDialog.getByRole('button', { name: '从图标库选择…' }).click()
  const picker = page.getByRole('dialog', { name: '从图标库选择' })
  const pickerSearch = picker.getByRole('combobox', { name: '搜索图标' })
  assert.equal(await pickerSearch.evaluate(el => el === document.activeElement), true)
  await picker.getByRole('option').first().waitFor()
  assert.equal(await picker.locator('.icon-picker-grid').evaluate(el => getComputedStyle(el).gridTemplateColumns.split(' ').length), 9)
  const selected = picker.locator('[aria-selected=true]')
  await pickerSearch.press('ArrowDown')
  assert.equal(await selected.getAttribute('id'), 'picker-icon-9')
  await pickerSearch.press('ArrowUp')
  await pickerSearch.press('ArrowRight')
  assert.equal(await selected.getAttribute('id'), 'picker-icon-1')
  await pickerSearch.press('ArrowLeft')
  assert.equal(await selected.getAttribute('id'), 'picker-icon-0')
  await pickerSearch.fill('cstb')
  await picker.getByRole('option', { name: '测试图标', exact: true }).waitFor()
  await pickerSearch.press('Enter')
  const routePreview = routeDialog.locator('.route-icon-img')
  await routePreview.waitFor()
  const previewURL = await routePreview.getAttribute('src')
  assert.match(previewURL, /^\/api\/icons\/file\/uploads\//)
  await routePreview.evaluate(img => img.decode())
  await routeDialog.getByRole('button', { name: '保存路由', exact: true }).click()
  await routeDialog.waitFor({ state: 'hidden' })
  assert.ok((await api('config')).config.routes.some(row => row.image?.startsWith('/icons/uploads/')))
  await page.locator(`.routes-grid img[src="${previewURL}"]`).waitFor()

  await page.setViewportSize({ width: 390, height: 844 })
  await route('icons')
  await page.locator('.icons-grid .icon-tile').first().waitFor()
  await page.waitForFunction(() => getComputedStyle(document.querySelector('.icons-page')).opacity === '1')
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth === innerWidth), true)
  assert.equal(await page.locator('.icons-grid').evaluate(el => getComputedStyle(el).gridTemplateColumns.split(' ').length), 5)
  if (process.env.GATEHOME_BROWSER_ARTIFACTS) {
    await page.getByRole('button', { name: '关闭此通知' }).evaluateAll(buttons => buttons.forEach(button => button.click()))
    await page.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, 'icons-light-390.png'), animations: 'disabled' })
  }
  await route('routes')
  await button('新建反代服务').click()
  await page.getByRole('button', { name: '从图标库选择…' }).click()
  await picker.getByRole('option').first().waitFor()
  const pickerBounds = await picker.boundingBox()
  assert.equal(pickerBounds.x, 0)
  assert.equal(Math.round(pickerBounds.y + pickerBounds.height), 844)
  assert.equal(await picker.locator('.icon-picker-grid').evaluate(el => getComputedStyle(el).gridTemplateColumns.split(' ').length), 5)
  if (process.env.GATEHOME_BROWSER_ARTIFACTS) {
    await page.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, 'icon-picker-light-390.png') })
  }
  await pickerSearch.press('Escape')
  await picker.waitFor({ state: 'hidden' })
  await closeEditor()
  console.log('PASS 17 existing views and Icon Library: themes, upload, search, delete, keyboard, route persistence and mobile sheet')

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
