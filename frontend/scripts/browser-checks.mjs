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
    ...(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : { channel: 'chromium' }),
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

  await route('access')
  await button('添加防火墙策略').click()
  const firewallEditor = page.getByRole('dialog', { name: '添加防火墙策略', exact: true })
  const sectionNavigation = () => dialog().getByRole('navigation', { name: '防火墙配置分区' })
  await firewallEditor.locator('[name=name]').fill('UI review firewall')
  assert.equal(await firewallEditor.locator('[name=default_action]').inputValue(), 'deny')
  await firewallEditor.getByRole('button', { name: '添加 IP 组', exact: true }).click()
  await sectionNavigation().getByRole('button', { name: '限速与冻结', exact: true }).click()
  await firewallEditor.getByRole('button', { name: '保存策略', exact: true }).click()
  await page.waitForFunction(() => document.activeElement?.name === 'group_name')
  assert.equal(await sectionNavigation().getByRole('button', { name: '来源 IP', exact: true }).getAttribute('aria-pressed'), 'true')
  await firewallEditor.locator('[name=group_name]').fill('Manual fixture ranges')
  await firewallEditor.locator('[name=cidrs]').fill('192.0.2.0/24\n2001:db8::/32')
  await sectionNavigation().getByRole('button', { name: 'Web 防护', exact: true }).click()
  await firewallEditor.locator('[name=waf_mode]').selectOption('detect')
  await firewallEditor.getByRole('button', { name: '添加规则例外', exact: true }).click()
  await firewallEditor.locator('[name=exception_id]').fill('942100')
  await firewallEditor.locator('[name=exception_path]').fill('/api')
  await sectionNavigation().getByRole('button', { name: 'HTTP 规则', exact: true }).click()
  await firewallEditor.getByRole('button', { name: '添加 HTTP 规则', exact: true }).click()
  await firewallEditor.locator('[name=http_name]').fill('Test bot match')
  await firewallEditor.locator('[name=http_pattern]').fill('(?i)test-only-bot')
  await firewallEditor.locator('[name=http_action]').selectOption('detect')
  await sectionNavigation().getByRole('button', { name: '限速与冻结', exact: true }).click()
  assert.equal(await firewallEditor.locator('[name=rate_window]').isDisabled(), true)
  await firewallEditor.getByRole('switch', { name: '启用访问频率限制' }).check()
  await firewallEditor.locator('[name=rate_window]').fill('45')
  await firewallEditor.locator('[name=rate_requests]').fill('90')
  await firewallEditor.locator('[name=rate_path]').fill('/api')
  await firewallEditor.getByRole('switch', { name: '启用防护自动冻结' }).check()
  await firewallEditor.locator('[name=security_freeze_window]').fill('600')
  await firewallEditor.locator('[name=security_freeze_failures]').fill('8')
  await firewallEditor.locator('[name=security_freeze_minutes]').fill('90')
  await firewallEditor.getByRole('switch', { name: '启用防护自动冻结' }).uncheck()
  await firewallEditor.getByRole('switch', { name: '启用防护自动冻结' }).press('Space')
  assert.equal(await firewallEditor.locator('[name=security_freeze_minutes]').inputValue(), '90')
  await firewallEditor.getByRole('button', { name: '保存策略', exact: true }).click()
  await firewallEditor.waitFor({ state: 'hidden' })
  const storedFirewall = (await api('config')).config.firewalls.find(item => item.name === 'UI review firewall')
  assert.deepEqual(storedFirewall.groups[0].cidrs, ['192.0.2.0/24', '2001:db8::/32'])
  assert.equal(storedFirewall.protection.waf.mode, 'detect')
  assert.equal(storedFirewall.protection.waf.exceptions[0].rule_id, 942100)
  assert.equal(storedFirewall.protection.rules[0].pattern, '(?i)test-only-bot')
  assert.equal(storedFirewall.protection.rate.requests, 90)
  assert.equal(storedFirewall.protection.freeze.seconds, 5400)
  await button('编辑策略').click()
  await sectionNavigation().getByRole('button', { name: '限速与冻结', exact: true }).click()
  assert.equal(await dialog().locator('[name=rate_window]').inputValue(), '45')
  assert.equal(await dialog().locator('[name=security_freeze_minutes]').inputValue(), '90')
  await closeEditor()
  console.log('PASS firewall editor: section drafts, hidden-field validation, keyboard switches and real create/edit/save')

  for (const [width, height] of [[1276, 956], [768, 1024], [390, 844], [320, 667], [390, 568]]) {
    await page.setViewportSize({ width, height })
    for (const theme of ['light', 'dark']) {
      if (!await page.locator('body').evaluate((el, theme) => el.classList.contains('theme-' + theme), theme))
        await page.getByTitle(theme === 'dark' ? '切换为深色模式' : '切换为浅色模式').click()
      await button('添加防火墙策略').click()
      await dialog().locator('[name=name]').fill('家庭服务防护')
      await sectionNavigation().getByRole('button', { name: '限速与冻结', exact: true }).click()
      await dialog().getByRole('switch', { name: '启用访问频率限制' }).check()
      const bounds = await dialog().boundingBox()
      const footer = await dialog().locator('.firewall-footer').boundingBox()
      const close = await dialog().getByRole('button', { name: '关闭', exact: true }).boundingBox()
      assert.ok(bounds.x >= 0 && bounds.x + bounds.width <= width + 1)
      assert.ok(footer.y >= bounds.y && footer.y + footer.height <= height + 1, 'firewall actions left the viewport')
      assert.equal(await dialog().evaluate(el => el.scrollHeight <= el.clientHeight + 1), true)
      assert.equal(await dialog().locator('.editor-content').evaluate(el => el.scrollWidth <= el.clientWidth), true)
      if (width <= 700) {
        assert.ok(close.width >= 44 && close.height >= 44)
        assert.ok(await sectionNavigation().getByRole('button').evaluateAll(buttons => buttons.every(button => button.getBoundingClientRect().height >= 44)))
      }
      await dialog().getByRole('button', { name: '保存策略', exact: true }).focus()
      const focusedFooter = await dialog().locator('.firewall-footer').boundingBox()
      assert.equal(focusedFooter.y, footer.y)
      if (process.env.GATEHOME_BROWSER_ARTIFACTS) {
        await page.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, `firewall-${width}-${height}-${theme}.png`), animations: 'disabled' })
      }
      await dialog().locator('[name=security_freeze_minutes]').scrollIntoViewIfNeeded()
      const lastControl = await dialog().locator('[name=security_freeze_minutes]').boundingBox()
      assert.ok(lastControl.y + lastControl.height <= footer.y, 'last firewall parameter is hidden under the actions')
      await dialog().press('Escape')
      await dialog().waitFor({ state: 'hidden' })
    }
  }
  await page.setViewportSize({ width: 1440, height: 1000 })
  console.log('PASS firewall themes/mobile: 320px–1276px, short screens, fixed actions and touch-sized navigation')

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

  const aspectImages = await page.evaluate(() => [[256, 128], [128, 256], [128, 128]].map(([width, height]) => {
    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = height
    const ctx = canvas.getContext('2d')
    ctx.fillStyle = '#10b981'
    ctx.fillRect(0, 0, width, height)
    for (const [color, x, y, w, h] of [
      ['#ef4444', 0, 0, 8, height], ['#3b82f6', width - 8, 0, 8, height],
      ['#facc15', 0, 0, width, 8], ['#d946ef', 0, height - 8, width, 8]
    ]) {
      ctx.fillStyle = color
      ctx.fillRect(x, y, w, h)
    }
    return canvas.toDataURL('image/png').split(',')[1]
  }))
  const aspectFile = (index, name) => ({ name, mimeType: 'image/png', buffer: Buffer.from(aspectImages[index], 'base64') })

  await button('上传本地图标').click()
  const upload = page.getByRole('dialog', { name: '上传本地图标' })
  await upload.getByLabel('选择图标文件').setInputFiles([
    { name: '测试图标.svg', mimeType: 'image/svg+xml', buffer: Buffer.from(fixtureSVG(7)) },
    { name: 'delete-fixture.svg', mimeType: 'image/svg+xml', buffer: Buffer.from(fixtureSVG(12)) },
    aspectFile(0, 'aspect-wide.png'),
    aspectFile(2, 'aspect-square.png')
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

  async function assertCompleteIcon(image, ratio) {
    await image.evaluate(img => img.decode())
    assert.equal(await image.evaluate(img => img.naturalWidth / img.naturalHeight), ratio)
    const pixels = await image.screenshot({ animations: 'disabled', scale: 'css' })
    const rendered = await page.evaluate(async base64 => {
      const image = new Image()
      image.src = 'data:image/png;base64,' + base64
      await image.decode()
      const canvas = document.createElement('canvas')
      canvas.width = image.width
      canvas.height = image.height
      const ctx = canvas.getContext('2d')
      ctx.drawImage(image, 0, 0)
      const data = ctx.getImageData(0, 0, image.width, image.height).data
      const colors = [[239, 68, 68], [59, 130, 246], [250, 204, 21], [217, 70, 239]]
      const edges = colors.map(() => 0)
      let left = image.width, right = -1, top = image.height, bottom = -1
      for (let i = 0; i < data.length; i += 4) {
        const color = colors.findIndex(color => color.every((value, channel) => Math.abs(value - data[i + channel]) < 30))
        if (color < 0) continue
        edges[color]++
        const x = (i / 4) % image.width, y = Math.floor(i / 4 / image.width)
        left = Math.min(left, x); right = Math.max(right, x)
        top = Math.min(top, y); bottom = Math.max(bottom, y)
      }
      return { edges, ratio: (right - left + 1) / (bottom - top + 1) }
    }, pixels.toString('base64'))
    assert.ok(rendered.edges.every(count => count > 0), 'an icon edge was cropped')
    assert.ok(Math.abs(rendered.ratio - ratio) < 0.12, 'rendered icon aspect ratio was distorted')
  }

  for (const [library, name, fixture, ratio] of [
    [true, 'aspect-wide', 0, 2], [true, 'aspect-square', 2, 1],
    [false, 'direct-wide.png', 0, 2], [false, 'direct-tall.png', 1, 0.5]
  ]) {
    await button('编辑').first().click()
    if (library) {
      await routeDialog.getByRole('button', { name: '从图标库选择…' }).click()
      await pickerSearch.fill(name)
      await picker.getByRole('option', { name, exact: true }).waitFor()
      await pickerSearch.press('Enter')
    } else {
      const response = page.waitForResponse(response => response.url().endsWith('/api/route-images/upload') && response.request().method() === 'POST')
      await routeDialog.locator('input[type=file]').setInputFiles(aspectFile(fixture, name))
      const uploaded = await (await response).json()
      await page.waitForFunction(id => document.querySelector('.route-icon-img')?.getAttribute('src')?.endsWith(id), uploaded.id)
    }
    await assertCompleteIcon(routePreview, ratio)
    const imageURL = await routePreview.getAttribute('src')
    assert.match(imageURL, library ? /^\/api\/icons\/file\// : /^\/api\/route-images\//)
    await routeDialog.getByRole('button', { name: '保存路由', exact: true }).click()
    await routeDialog.waitFor({ state: 'hidden' })
    const cardImage = page.locator('.routes-grid img').filter({ visible: true }).first()
    await page.waitForFunction(url => document.querySelector('.routes-grid img')?.getAttribute('src') === url, imageURL)
    await assertCompleteIcon(cardImage, ratio)
  }
  console.log('PASS route icon proportions: square, wide and tall images keep all four edges in library/upload previews and cards')

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

  const notices = [
    { id: 'expired', type: 'error', category: 'system', message: '登录已过期，请重新登录', unread: true },
    { id: 'certificate', type: 'warn', category: 'cert', message: '网关证书将在 7 天内到期', unread: true, path: '/certificates' },
    { id: 'backup', type: 'success', category: 'backup', message: 'WebDAV 定时备份已完成', unread: false, path: '/backups' },
    { id: 'update', type: 'info', category: 'system', message: '在线更新正在下载安装包', unread: false },
    { id: 'blocked', type: 'warning', category: 'waf', message: '安全策略已拦截异常请求', unread: true, path: '/access' },
    { id: 'domain', type: 'system', category: 'ddns', message: '动态域名任务已加入队列', unread: false, path: '/ddns' },
    { id: 'long', type: 'info', message: '诊断记录' + '界面布局验证'.repeat(36), unread: false }
  ].map((notice, i) => ({ ...notice, createdAt: new Date(Date.now() - i * 72 * 60000).toISOString() }))
  await page.evaluate(items => localStorage.setItem('gatehome_notifications_history', JSON.stringify(items)), notices)
  await page.reload()
  await page.setViewportSize({ width: 1440, height: 1000 })
  const notificationTrigger = page.locator('.notification-btn')
  const center = page.getByRole('dialog', { name: '通知中心', exact: true })
  const noticeCards = center.locator('.notification-card')
  const notice = message => noticeCards.filter({ hasText: message })
  const centerClose = center.getByRole('button', { name: '关闭通知中心' })
  await page.getByTitle('切换为深色模式').click()
  await notificationTrigger.click()
  await center.waitFor()
  assert.equal(await notificationTrigger.getAttribute('aria-expanded'), 'true')
  assert.equal(await centerClose.evaluate(el => el === document.activeElement), true)
  assert.equal(await noticeCards.count(), notices.length)
  assert.equal(await center.locator('.pill-dot, .unread-indicator').count(), 0)
  assert.ok((await center.locator('.category-pill').allTextContents()).every(label => !label.includes('·')))
  assert.equal(await center.locator('.category-pill svg').count(), notices.length)
  assert.equal(await center.locator('time[datetime]').count(), notices.length)
  assert.equal(await notice('网关证书').locator('.type-label').textContent().then(value => value.trim()), '警告')
  assert.ok(await notice('安全策略').evaluate(el => el.classList.contains('type-warning')))
  assert.ok(await notice('动态域名任务').evaluate(el => el.classList.contains('type-info')))

  async function checkNotificationColors(drawer = center) {
    const presentation = await drawer.evaluate(el => {
      const ctx = document.createElement('canvas').getContext('2d')
      const luminance = ([r, g, b]) => [r, g, b].map(value => value / 255)
        .map(value => value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4)
        .reduce((total, value, i) => total + value * [0.2126, 0.7152, 0.0722][i], 0)
      return [...el.querySelectorAll('.notification-card')].map(card => {
        const pill = card.querySelector('.category-pill')
        const pillStyle = getComputedStyle(pill)
        const cardStyle = getComputedStyle(card)
        ctx.clearRect(0, 0, 1, 1)
        for (const layer of [getComputedStyle(el).backgroundColor, cardStyle.backgroundColor, pillStyle.backgroundColor]) {
          ctx.fillStyle = layer
          ctx.fillRect(0, 0, 1, 1)
        }
        const bg = luminance(ctx.getImageData(0, 0, 1, 1).data)
        ctx.fillStyle = pillStyle.color
        ctx.fillRect(0, 0, 1, 1)
        const fg = luminance(ctx.getImageData(0, 0, 1, 1).data)
        return { ink: pillStyle.color, contrast: (Math.max(fg, bg) + 0.05) / (Math.min(fg, bg) + 0.05) }
      })
    })
    assert.equal(new Set(presentation.map(row => row.ink)).size, 4, 'notification severity colors are not distinct')
    assert.ok(presentation.every(row => row.contrast >= 4.5), 'notification badge text contrast fell below 4.5:1')
  }
  await checkNotificationColors()
  if (process.env.GATEHOME_BROWSER_ARTIFACTS) {
    await page.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, 'notifications-desktop-dark.png') })
  }
  for (const key of ['Tab', 'Shift+Tab']) {
    for (let i = 0; i < 12; i++) {
      await page.keyboard.press(key)
      assert.equal(await page.evaluate(() => document.activeElement === document.body ||
        Boolean(document.activeElement.closest('#notification-center[open]'))), true, 'notification focus left the drawer')
    }
  }
  await page.keyboard.press('Escape')
  await center.waitFor({ state: 'hidden' })
  assert.equal(await notificationTrigger.getAttribute('aria-expanded'), 'false')
  assert.equal(await notificationTrigger.evaluate(el => el === document.activeElement), true)
  console.log('PASS notification drawer: four contrast-checked severities, legacy types, native focus and Escape')

  const mobileContext = await browser.newContext({
    storageState: await context.storageState(),
    viewport: { width: 390, height: 844 },
    isMobile: true,
    hasTouch: true,
    deviceScaleFactor: 2,
    reducedMotion: 'reduce'
  })
  {
    const page = await mobileContext.newPage()
    page.setDefaultTimeout(10000)
    page.on('pageerror', () => errors.push('mobile notification page exception'))
    page.on('console', message => {
      if (message.type() === 'error') errors.push(message.text().replaceAll(secret, '<REDACTED>'))
    })
    await page.goto(origin + '/console/#/routes')
    const notificationTrigger = page.locator('.notification-btn')
    const center = page.getByRole('dialog', { name: '通知中心', exact: true })
    const noticeCards = center.locator('.notification-card')
    const notice = message => noticeCards.filter({ hasText: message })
    const centerClose = center.getByRole('button', { name: '关闭通知中心' })
    const touchSession = await mobileContext.newCDPSession(page)
    await touchSession.send('Emulation.setSafeAreaInsetsOverride', { insets: { top: 44, bottom: 34, left: 18, right: 18 } })
    async function touch(type, points) {
      await touchSession.send('Input.dispatchTouchEvent', { type, touchPoints: points })
    }
    for (const [width, theme] of [[390, 'dark'], [320, 'light']]) {
      await page.setViewportSize({ width, height: 844 })
      if (theme === 'light') await page.getByTitle('切换为浅色模式').click()
      await notificationTrigger.click()
      await center.waitFor()
      const bounds = await center.boundingBox()
      assert.equal(bounds.x, 0)
      assert.equal(bounds.width, width)
      assert.equal(Math.round(bounds.y + bounds.height), 844)
      const deleteBounds = await noticeCards.first().getByRole('button', { name: '删除此通知' }).boundingBox()
      assert.ok(deleteBounds.width >= 44 && deleteBounds.height >= 44)
      assert.ok(deleteBounds.x + deleteBounds.width <= width - 18)
      assert.equal(await center.locator('.drawer-panel').evaluate(el => getComputedStyle(el).paddingBottom), '34px')
      assert.ok(await center.locator('.drawer-body').evaluate(el => el.scrollWidth <= el.clientWidth))
      assert.ok(await notice('诊断记录').locator('.card-content').evaluate(el => el.scrollWidth <= el.clientWidth))
      await checkNotificationColors(center)
      if (process.env.GATEHOME_BROWSER_ARTIFACTS) {
        await page.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, `notifications-${width}-${theme}.png`) })
      }
      if (theme === 'dark') {
        await centerClose.click()
        await center.waitFor({ state: 'hidden' })
      }
    }

    await notice('网关证书').getByRole('button', { name: '删除此通知' }).tap({ position: { x: 6, y: 6 } })
    await notice('网关证书').waitFor({ state: 'detached' })
    assert.ok(await center.isVisible(), 'deletion accidentally activated the notification link')
    await touchSession.send('Emulation.setSafeAreaInsetsOverride', { insets: {} })
    console.log('PASS notification sheet: 320px/390px themes, safe-area layout and 44px touch deletion')

    const expired = notice('登录已过期')
    const unreadBackground = await expired.evaluate(el => getComputedStyle(el).backgroundColor)
    await expired.locator('.card-content').press('Enter')
    assert.equal(await expired.evaluate(el => el.classList.contains('unread')), false)
    assert.notEqual(await expired.evaluate(el => getComputedStyle(el).backgroundColor), unreadBackground)
    await center.getByRole('button', { name: '全部已读', exact: true }).click()
    assert.equal(await center.locator('.notification-card.unread').count(), 0)
    assert.equal(await notificationTrigger.locator('.notification-count').count(), 0)
    await center.getByRole('button', { name: /^备份与系统/ }).click()
    assert.equal(await noticeCards.count(), 4)
    await notice('WebDAV 定时备份').locator('.card-content').click()
    await center.waitFor({ state: 'hidden' })
    await page.waitForFunction(() => location.hash === '#/backups')
    await notificationTrigger.click()
    await center.getByRole('button', { name: '清空', exact: true }).click()
    await center.getByText('暂无通知记录').waitFor()
    assert.deepEqual(await page.evaluate(() => JSON.parse(localStorage.getItem('gatehome_notifications_history'))), [])
    // Finish with the synthetic swipe, which can briefly suppress subsequent taps in Chromium.
    const handle = await center.locator('.drawer-handle').boundingBox()
    const point = { x: handle.x + handle.width / 2, y: handle.y + handle.height / 2 }
    await touch('touchStart', [point])
    await touch('touchMove', [{ ...point, y: point.y + 72 }])
    await touch('touchEnd', [])
    await center.waitFor({ state: 'hidden' })
    await touchSession.detach()
    console.log('PASS notification history: keyboard mark-read, bulk read, category counts, navigation, clear and swipe dismissal')
  }
  await mobileContext.close()

  await route('routes')
  await page.getByRole('button', { name: '关闭此通知' }).evaluateAll(buttons => buttons.forEach(button => button.click()))
  for (const width of [1440, 1024, 390, 320]) {
    await page.setViewportSize({ width, height: 900 })
    for (const theme of ['dark', 'light']) {
      if (!await page.locator('body').evaluate((body, theme) => body.classList.contains('theme-' + theme), theme))
        await page.getByTitle(theme === 'dark' ? '切换为深色模式' : '切换为浅色模式').click()
      const sidebar = page.locator('.app-sidebar')
      const navigation = page.getByRole('navigation', { name: '主导航' })
      if (width < 1024) {
        await page.getByRole('button', { name: '打开侧边栏导航' }).click()
        await page.waitForFunction(() => Math.abs(document.querySelector('.app-sidebar').getBoundingClientRect().x) < 1)
        const close = await page.getByRole('button', { name: '关闭侧边栏导航' }).boundingBox()
        assert.ok(close.width >= 44 && close.height >= 44)
        assert.ok(await navigation.locator('.nav-link').evaluateAll(links => links.every(link => link.getBoundingClientRect().height >= 44)))
      }
      assert.equal(await navigation.locator('.nav-group').count(), 2)
      assert.equal(await navigation.getByRole('link').count(), 18)
      if (theme === 'light') await page.waitForFunction(() => {
        const active = document.querySelector('.sidebar-nav .nav-link.active')
        return active && getComputedStyle(active).color !== 'rgb(255, 255, 255)'
      })
      const settings = navigation.getByRole('link', { name: '设置', exact: true })
      await settings.scrollIntoViewIfNeeded()
      const bounds = await navigation.boundingBox()
      const last = await settings.boundingBox()
      const footer = await sidebar.locator('.sidebar-footer').boundingBox()
      assert.ok(last.y >= bounds.y && last.y + last.height <= bounds.y + bounds.height + 1, 'last navigation item is clipped')
      assert.ok(bounds.y + bounds.height <= footer.y + 1, 'navigation overlaps the footer')
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
      if (process.env.GATEHOME_BROWSER_ARTIFACTS)
        await page.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, `sidebar-${width}-${theme}.png`) })
      if (width < 1024) {
        await settings.click()
        await page.waitForFunction(() => !document.querySelector('.app-sidebar').classList.contains('mobile-open'))
        await route('routes')
      }
    }
  }
  console.log('PASS sidebar: both themes, 18 links, 320px–1440px, scrollable groups and 44px mobile targets')

  await page.setViewportSize({ width: 1440, height: 1000 })
  await route('updates')
  await page.evaluate(() => { window.upgradeDocumentMarker = 'old document' })
  const runningStatus = await api('status')
  let switched = false
  await page.route('**/api/status', async route => {
    if (switched) return route.continue()
    switched = true
    await route.fulfill({
      status: 200, contentType: 'application/json',
      headers: { 'X-GateHome-Version': '99.0.0-beta.1' }, body: JSON.stringify(runningStatus)
    })
  })
  const refreshed = page.waitForEvent('domcontentloaded')
  await button('刷新维护状态').click()
  await refreshed
  await page.locator('.report-heading h1').waitFor()
  assert.equal(await page.evaluate(() => window.upgradeDocumentMarker), undefined)
  assert.equal(new URL(page.url()).hash, '#/updates')
  assert.equal(await page.locator('.brand-version').textContent(), '家庭网关 · v' + (await api('status')).version)
  await page.unroute('**/api/status')
  console.log('PASS update resource synchronization: a new server version reloads the document and preserves the current route')

  const loginContext = await browser.newContext({ viewport: { width: 390, height: 844 } })
  const loginPage = await loginContext.newPage()
  loginPage.on('pageerror', () => errors.push('login version polling exception'))
  await loginPage.goto(origin + '/console/#/login?redirect=/updates')
  await loginPage.locator('#admin-username').fill('test-admin-renamed')
  await loginPage.evaluate(() => { window.upgradeDocumentMarker = 'waiting for restart' })
  let healthSwitched = false
  await loginPage.route('**/healthz', async route => {
    if (healthSwitched) return route.continue()
    healthSwitched = true
    await route.fulfill({
      status: 200, contentType: 'application/json',
      headers: { 'X-GateHome-Version': '99.0.0-beta.1' }, body: JSON.stringify({ ok: true, version: '99.0.0-beta.1' })
    })
  })
  await loginPage.waitForEvent('domcontentloaded')
  await loginPage.locator('#admin-username').waitFor()
  assert.equal(await loginPage.evaluate(() => window.upgradeDocumentMarker), undefined)
  assert.equal(new URL(loginPage.url()).hash, '#/login?redirect=/updates')
  await loginPage.unroute('**/healthz')
  await loginPage.locator('#admin-username').fill('test-admin-renamed')
  await loginPage.locator('#admin-password').fill(secret)
  await loginPage.locator('.login-form button[type=submit]').click()
  await loginPage.locator('.report-heading h1').waitFor()
  assert.equal(new URL(loginPage.url()).hash, '#/updates')
  await loginContext.close()
  console.log('PASS restart login: readiness polling reloads stale resources before login and retains the update redirect')

  assert.deepEqual(errors, [], 'browser emitted errors during regression')
  console.log('PASS mobile layout and zero browser errors; disposable fixture cleaned')
} catch (error) {
  console.error(String(error.stack || error.message).replaceAll(secret, '<REDACTED>')
    .replace(/gatehouse_session=[^;\s'"\x1b]+/g, 'gatehouse_session=<REDACTED>'))
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
