import assert from 'node:assert/strict'
import { randomBytes } from 'node:crypto'
import { mkdir, mkdtemp, readFile, writeFile, rm } from 'node:fs/promises'
import { networkInterfaces, tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawn, spawnSync } from 'node:child_process'
import { createServer } from 'node:http'
import { createSocket } from 'node:dgram'
import { chromium } from 'playwright'

const repository = fileURLToPath(new URL('../../', import.meta.url))
const root = await mkdtemp(path.join(tmpdir(), 'gatehome-console-check-'))
const secret = randomBytes(24).toString('hex')
const linkedSVG = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 24"><style>.mark{fill:#6754ed}</style><rect class="mark" width="48" height="24"/></svg>'
let linkedContent = linkedSVG
let linkedRequests = 0
let webhookTests = 0
let assistantTarget = ''
let assistantCredentialAccepted = false
let responseRequest
let claudeRequest
const upstream = createServer((req, res) => {
  if (req.url === '/assistant-fixture/v1/models' && req.method === 'GET') {
    res.setHeader('Content-Type', 'application/json')
    res.end(JSON.stringify({ data: [{ id: 'browser-fixture' }, { id: 'browser-reasoning' }] }))
    return
  }
  if (['/assistant-fixture/v1/responses', '/anthropic-fixture/v1/messages'].includes(req.url) && req.method === 'POST') {
    let body = ''
    req.on('data', chunk => { body += chunk })
    req.on('end', () => {
      const text = JSON.stringify({ text: '### 模型已连接\n\n**加粗**与 `代码`' })
      res.setHeader('Content-Type', 'application/json')
      if (req.url.endsWith('/responses')) {
        responseRequest = JSON.parse(body)
        if (responseRequest.stream) {
          res.setHeader('Content-Type', 'text/event-stream; charset=utf-8')
          res.write(`data: ${JSON.stringify({ type: 'response.output_text.delta', delta: text.slice(0, 12) })}\n\n`)
          res.write(`data: ${JSON.stringify({ type: 'response.output_text.delta', delta: text.slice(12) })}\n\n`)
          res.end(`data: ${JSON.stringify({ type: 'response.completed', response: { output: [{ content: [{ type: 'output_text', text }] }] } })}\n\n`)
        } else {
          res.end(JSON.stringify({ output: [{ content: [{ type: 'output_text', text }] }] }))
        }
      } else {
        claudeRequest = { payload: JSON.parse(body), version: req.headers['anthropic-version'] }
        res.end(JSON.stringify({ content: [{ type: 'text', text }] }))
      }
    })
    return
  }
  if (req.url === '/assistant-fixture/v1/chat/completions' && req.method === 'POST') {
    let body = ''
    req.on('data', chunk => { body += chunk })
    req.on('end', () => {
      assistantCredentialAccepted = req.headers.authorization === 'Bearer TEST_ONLY_MODEL_KEY'
      const { messages } = JSON.parse(body)
      const prompt = messages.at(-1).content
      let content = 'OK'
      if (prompt === '查看反代状态') {
        const snapshot = JSON.parse(messages[0].content.split('\n网关快照：')[1])
        content = JSON.stringify({ text: snapshot.routes.some(route => route.id === assistantTarget) ? '已读取反代运行状态' : '反代状态缺失' })
      } else if (prompt === '打开健康检查设置') {
        content = JSON.stringify({ text: '可以打开反向代理的健康检查设置。', action: 'health_settings' })
      } else if (prompt.startsWith('修改上游端口')) {
        content = JSON.stringify({ text: '请确认上游端口修改。', action: 'route_port', target: assistantTarget, value: prompt.split(' ').at(-1) })
      }
      res.setHeader('Content-Type', 'application/json')
      res.end(JSON.stringify({ choices: [{ message: { role: 'assistant', content } }] }))
    })
    return
  }
  if (req.url === '/test-webhook' && req.method === 'POST') {
    let body = ''
    req.on('data', chunk => { body += chunk })
    req.on('end', () => {
      if (JSON.parse(body).type === 'test') webhookTests++
      res.writeHead(204).end()
    })
    return
  }
  if (req.url.startsWith('/linked-icon')) {
    linkedRequests++
    res.setHeader('Content-Type', 'application/octet-stream')
    res.end(linkedContent)
    return
  }
  if (req.url !== '/') { res.writeHead(404); res.end(); return }
  res.setHeader('Content-Type', 'text/html')
  res.end('<html><head><title>Console test upstream</title></head><body>TEST_ONLY_UPSTREAM</body></html>')
})
let server, browser, upstreamUDP
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
  const upstreamPort = await listen(upstream)
  upstreamUDP = createSocket('udp4')
  upstreamUDP.on('message', (message, peer) => upstreamUDP.send(message, peer.port, peer.address))
  await new Promise((resolve, reject) => upstreamUDP.once('error', reject).bind(upstreamPort, '127.0.0.1', resolve))
  const forwardReservation = createServer()
  const forwardPort = await listen(forwardReservation)
  await new Promise(resolve => forwardReservation.close(resolve))
  state.config.forwards = [{
    id: 'traffic-fixture', name: '流量测试转发', protocol: 'tcp+udp', enabled: true,
    listen_port: forwardPort, upstream: '127.0.0.1:' + upstreamPort
  }]
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
  const reservation = createServer()
  const port = await listen(reservation)
  await new Promise(resolve => reservation.close(resolve))
  const origin = `http://gatehome-console.test:${port}`
  await startServer(port)
  browser = await chromium.launch({
    ...(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : { channel: 'chromium' }),
    args: ['--host-resolver-rules=MAP gatehome-console.test 127.0.0.1', '--no-proxy-server']
  })
  const passkeyContext = await browser.newContext()
  const passkeyPage = await passkeyContext.newPage()
  passkeyPage.setDefaultTimeout(10000)
  const cdp = await passkeyContext.newCDPSession(passkeyPage)
  await cdp.send('WebAuthn.enable')
  await cdp.send('WebAuthn.addVirtualAuthenticator', { options: {
    protocol: 'ctap2', transport: 'internal', hasResidentKey: true,
    hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true
  } })
  const passkeyOrigin = `http://localhost:${port}`
  await passkeyPage.goto(passkeyOrigin + '/console/#/settings/security')
  await passkeyPage.locator('#admin-username').fill('admin')
  await passkeyPage.locator('#admin-password').fill(secret)
  await passkeyPage.locator('.login-form button[type=submit]').click()
  await passkeyPage.getByRole('button', { name: '添加通行密钥', exact: true }).click()
  await passkeyPage.getByRole('rowheader', { name: 'localhost', exact: true }).waitFor()
  await passkeyPage.getByRole('button', { name: '退出当前会话', exact: true }).click()
  const passkeyLogin = passkeyPage.waitForResponse(response => response.url().endsWith('/api/account/passkeys/login/finish'))
  await passkeyPage.getByRole('button', { name: '使用通行密钥登录', exact: true }).click()
  assert.equal((await passkeyLogin).status(), 200)
  await passkeyPage.waitForURL('**/#/overview')
  await passkeyPage.goto(passkeyOrigin + '/console/#/settings/security')
  await passkeyPage.getByRole('cell', { name: '通行密钥', exact: true }).waitFor()
  await passkeyPage.goto(passkeyOrigin + '/console/#/settings/exposure')
  await passkeyPage.getByRole('rowheader', { name: '管理台', exact: true }).waitFor()
  assert.equal(await passkeyPage.getByRole('rowheader', { name: '流量测试转发', exact: true }).count(), 2)
  await passkeyContext.close()
  console.log('PASS administrator Passkey registration and login, session metadata and TCP/UDP exposure overview')
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, reducedMotion: 'reduce' })
  const page = await context.newPage()
  page.setDefaultTimeout(10000)
  const errors = []
  let checkingConflict = false
  page.on('response', response => {
    if (response.status() < 400) return
    const path = new URL(response.url()).pathname
    if (path === '/api/account' && response.status() === 401) return
    if (path === '/api/credentials/reveal' && response.status() === 428) return
    if (checkingConflict && path === '/api/config' && response.status() === 400) return
    errors.push(`HTTP ${response.status()} ${path}`)
  })
  page.on('pageerror', () => errors.push('page exception'))
  page.on('console', message => {
    const text = message.text()
    if (message.type() !== 'error' || /^Failed to load resource:.*status of (400|401)/.test(text)) return
    if (/^Failed to load resource:.*status of 428/.test(text) &&
        message.location().url.endsWith('/api/credentials/reveal')) return
    errors.push(text.replaceAll(secret, '<REDACTED>'))
  })
  await page.goto(origin + '/console/#/routes')
  await page.locator('#admin-username').fill('admin')
  await page.locator('#admin-password').fill(secret)
  await page.locator('.login-form button[type=submit]').click()
  await page.locator('.routes-page').waitFor()
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.waitForSelector('body.theme-dark')
  await page.emulateMedia({ colorScheme: 'light' })
  await page.waitForSelector('body.theme-light')
  let remoteHomeState = {}
  await page.route('**/api/remote-home', r => r.fulfill({ json: remoteHomeState }))
  await page.locator('.nav-link[href$="/remote-home"]').click()
  await page.getByRole('button', { name: '配置 WireGuard' }).click()
  const wireGuardDialog = page.getByRole('dialog', { name: 'WireGuard 设置' })
  const saveWireGuard = wireGuardDialog.getByRole('button', { name: '保存并应用' })
  await wireGuardDialog.waitFor()
  assert.equal(await saveWireGuard.isEnabled(), true)
  const initialViewport = page.viewportSize()
  await page.setViewportSize({ width: 320, height: 620 })
  const beforeScroll = await saveWireGuard.boundingBox()
  const heights = await wireGuardDialog.locator('.settings-fields input').evaluateAll(inputs =>
    inputs.map(input => input.getBoundingClientRect().height))
  assert.deepEqual(heights, [44, 44, 44, 44])
  const endpoint = await wireGuardDialog.getByLabel('公网域名或 IP').boundingBox()
  const udpPort = await wireGuardDialog.getByLabel('UDP 端口').boundingBox()
  assert.ok(endpoint.width >= 260 && endpoint.y + endpoint.height < udpPort.y, 'WireGuard endpoint and port did not stack')
  const downloadGuide = wireGuardDialog.getByRole('link', { name: '下载连接说明' })
  assert.equal(await downloadGuide.isVisible(), false)
  await wireGuardDialog.getByText('公网入口与端口转发', { exact: true }).click()
  await downloadGuide.scrollIntoViewIfNeeded()
  const body = await wireGuardDialog.locator('.modal-body').boundingBox()
  const guide = await downloadGuide.boundingBox()
  assert.ok(guide.y >= body.y && guide.y + guide.height <= body.y + body.height + 1)
  const downloadEvent = page.waitForEvent('download')
  await downloadGuide.click()
  assert.equal((await downloadEvent).suggestedFilename(), 'GateHome-WireGuard.md')
  assert.deepEqual(await saveWireGuard.boundingBox(), beforeScroll)
  assert.ok(beforeScroll.y + beforeScroll.height <= 620)
  assert.ok(await wireGuardDialog.evaluate(el => el.scrollWidth <= el.clientWidth))
  await wireGuardDialog.getByRole('button', { name: '关闭', exact: true }).click()
  await page.setViewportSize(initialViewport)
  remoteHomeState = {
    running: true, listen_port: 51820,
    settings: { endpoint: 'vpn.example.test', port: 51820, lan: '192.168.2.0/24', tunnel: '10.77.0.0/24' },
    devices: [{ id: 'phone', name: '测试手机', address: '10.77.0.2', mode: 'lan', rx: 1024, tx: 2048, handshake: Math.floor(Date.now() / 1000) }]
  }
  const refreshRemoteHome = () => page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')))
  await refreshRemoteHome()
  await page.getByRole('button', { name: '测试手机', exact: true }).waitFor()
  await page.getByText('已收到握手', { exact: true }).waitFor()
  const connectedState = remoteHomeState
  remoteHomeState = { ...connectedState, running: false, fault: 'WireGuard 端口已占用' }
  await refreshRemoteHome()
  await page.getByText('WireGuard 端口已占用', { exact: true }).waitFor()
  assert.equal(await page.getByRole('button', { name: '测试手机', exact: true }).count(), 1)
  assert.equal(await page.getByRole('button', { name: '添加设备', exact: true }).isDisabled(), true)
  remoteHomeState = connectedState
  await refreshRemoteHome()
  await page.getByText('已收到握手', { exact: true }).waitFor()
  await page.getByRole('button', { name: '设置', exact: true }).click()
  await wireGuardDialog.getByText('内置 WireGuard', { exact: true }).waitFor()
  assert.equal(await wireGuardDialog.getByLabel('家庭局域网段').getAttribute('readonly'), '')
  await wireGuardDialog.getByLabel('公网域名或 IP').fill('vpn-edited.example.test')
  await page.route('**/api/remote-home/settings', r => {
    const input = r.request().postDataJSON()
    assert.equal(input.endpoint, 'vpn-edited.example.test')
    assert.equal(input.lan, '192.168.2.0/24')
    remoteHomeState = { ...connectedState, settings: input }
    return r.fulfill({ json: {} })
  })
  await saveWireGuard.click()
  await wireGuardDialog.waitFor({ state: 'hidden' })
  await page.getByText('vpn-edited.example.test:51820').waitFor()
  await page.unroute('**/api/remote-home/settings')
  await page.unroute('**/api/remote-home')
  console.log('PASS built-in WireGuard setup and runtime recovery, with operating system theme updates')
  await page.locator('.nav-link[href$="/routes"]').click()
  await page.locator('.routes-page').waitFor()
  assert.equal(await page.evaluate(() => isSecureContext), false)
  assert.equal(await page.evaluate(() => typeof crypto.randomUUID), 'undefined')
  const lifecycleHistoryWrites = await page.evaluate(() => {
    const replaceState = history.replaceState
    let writes = 0
    history.replaceState = function (...args) { writes++; return replaceState.apply(this, args) }
    try {
      Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
      document.dispatchEvent(new Event('visibilitychange'))
      const hidden = writes
      window.dispatchEvent(new Event('pagehide'))
      return { hidden, pagehide: writes - hidden }
    } finally {
      history.replaceState = replaceState
      delete document.visibilityState
      document.dispatchEvent(new Event('visibilitychange'))
    }
  })
  assert.equal(lifecycleHistoryWrites.hidden, 0, 'hidden page wrote history, triggering Edge window focus steal')
  assert.equal(lifecycleHistoryWrites.pagehide, 1, 'leaving the page no longer saves scroll state')
  console.log('PASS Edge minimize regression: hiding the page never writes history, leaving still saves scroll state')
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
  const routeMeta = {
    overview: ['概览 · GateHome', ['系统概览']],
    sunpanel: ['浏览器首页 · GateHome', ['SunPanel 浏览器首页']],
    icons: ['图标库 · GateHome', ['图标库']],
    routes: ['反向代理 · GateHome', ['反向代理']],
    forwards: ['端口转发 · GateHome', ['端口转发']],
    'remote-home': ['远程回家 · GateHome', ['远程回家', 'WireGuard']],
    ddns: ['动态域名 · GateHome', ['动态域名（DDNS）']],
    certificates: ['SSL 证书 · GateHome', ['SSL / TLS 证书']],
    access: ['防火墙管理 · GateHome', ['防火墙与 WAF']],
    firewall: ['防火墙管理 · GateHome', ['防火墙与 WAF']],
    subscriptions: ['订阅管理 · GateHome', ['IP 规则订阅']],
    'ip-blocks': ['IP 拦截名单 · GateHome', ['IP 拦截名单']],
    'firewall-logs': ['防火墙拦截记录 · GateHome', ['防火墙拦截记录']],
    runtime: ['运行状态 · GateHome', ['运行状态']],
    notifications: ['通知中心 · GateHome', ['通知中心']],
    logs: ['日志 · GateHome', ['日志']],
    statistics: ['统计 · GateHome', ['访问统计']],
    traffic: ['流量统计 · GateHome', ['流量统计']],
    assistant: ['网关助手 · GateHome', ['网关助手']],
    'assistant/settings': ['模型接入点 · GateHome', ['模型接入点']],
    mcp: ['MCP 授权 · GateHome', ['MCP 授权']],
    updates: ['更新 · GateHome', ['版本更新与回滚']],
    backups: ['备份与恢复 · GateHome', ['备份与恢复']],
    settings: ['设置 · GateHome', ['设置']],
    'settings/exposure': ['入口暴露总览 · GateHome', ['入口暴露总览']],
    'settings/security': ['账号安全 · GateHome', ['账号安全']]
  }
  async function route(name) {
    const expectedHash = '#/' + name
    const [expectedTitle, expectedHeadings] = routeMeta[name] ?? []
    await page.goto(origin + '/console/' + expectedHash)
    await page.waitForFunction(({ expectedHash, expectedTitle, expectedHeadings }) => {
      if (window.location.hash !== expectedHash) return false
      if (expectedTitle && document.title !== expectedTitle) return false
      const viewport = document.querySelector('.page-viewport')
      if (!viewport || viewport.querySelector('.page-fade-leave-active')) return false
      const heading = viewport.querySelector('h1')?.textContent?.trim()
      if (!heading) return false
      return !expectedHeadings || expectedHeadings.includes(heading)
    }, { expectedHash, expectedTitle, expectedHeadings })
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

  const ddnsBefore = await api('config')
  const compactGroups = [
    { id: 'compact-home', name: '家庭域名', zone: 'example.test', hosts: ['nas', 'photos', 'media', 'cloud', 'home', 'vault', 'status'] },
    { id: 'compact-lab', name: '实验室', zone: 'example.net', hosts: ['dev', 'git'] }
  ].map(group => ({
    ...group, hosts: group.hosts.map(host => `${host}.${group.zone}`), provider: 'cloudflare',
    enabled: false, interval: 300, mode: 'dual', interface: state.config.ddns.ip_monitor.interface,
    ipv4_source: 'interface', ipv6_source: 'interface'
  }))
  const ddnsFixture = await api('config', {
    revision: ddnsBefore.revision,
    config: { ...ddnsBefore.config, ddns: { ...ddnsBefore.config.ddns, groups: compactGroups } }
  })
  await page.evaluate(() => localStorage.setItem('gatehome_ddns_collapsed_groups', '[]'))
  await route('ddns')
  const ddnsCards = page.locator('.ddns-card')
  const homeDdns = page.locator('[data-group-id=compact-home]')
  const ddnsToggle = () => homeDdns.locator('button[aria-controls]')
  await ddnsCards.nth(1).waitFor()
  assert.equal(await page.locator('.ddns-card-body:visible').count(), 0, 'DDNS domains were expanded by default')
  assert.equal(await ddnsToggle().getAttribute('aria-expanded'), 'false')
  await homeDdns.getByText('7 个域名', { exact: true }).waitFor()
  await button('展开全部组').waitFor()
  await ddnsToggle().click()
  assert.equal(await ddnsToggle().getAttribute('aria-expanded'), 'true')
  assert.equal(await homeDdns.locator('.dns-records tbody tr:visible').count(), 7, 'expanding a group omitted domains')
  await homeDdns.getByRole('button', { name: '编辑域名', exact: true }).click()
  assert.equal((await dialog().locator('[name=hosts]').inputValue()).split('\n').length, 7)
  await closeEditor()
  await page.reload()
  await homeDdns.locator('.ddns-card-body').waitFor()
  assert.equal(await ddnsToggle().getAttribute('aria-expanded'), 'true', 'expanded preference was lost on reload')
  await ddnsToggle().press('Enter')
  assert.equal(await homeDdns.locator('.ddns-card-body').isVisible(), false)
  await ddnsToggle().press('Space')
  assert.equal(await homeDdns.locator('.dns-records tbody tr:visible').count(), 7)
  await button('收起全部组').click()
  assert.equal(await page.locator('.ddns-card-body:visible').count(), 0)
  await page.reload()
  await ddnsCards.nth(1).waitFor()
  assert.equal(await page.locator('.ddns-card-body:visible').count(), 0, 'collapsed preference was lost on reload')
  await button('展开全部组').click()
  assert.equal(await page.locator('.dns-records tbody tr:visible').count(), 9)
  await button('收起全部组').click()
  assert.deepEqual((await api('config')).config, ddnsFixture.config, 'folding DDNS cards changed configuration')
  console.log('PASS compact DDNS: default collapse, one-click full domain list, keyboard, persisted preferences and unchanged configuration')

  for (const [width, height] of [[1440, 1000], [390, 844], [320, 667], [844, 390]]) {
    await page.setViewportSize({ width, height })
    // The shell margin animates across the mobile breakpoint; measure its final layout.
    await page.locator('.main-wrapper').evaluate(async el => {
      await document.fonts.ready
      await Promise.all(el.getAnimations().map(animation => animation.finished.catch(() => {})))
    })
    for (const theme of ['light', 'dark']) {
      if (!await page.locator('body').evaluate((el, theme) => el.classList.contains('theme-' + theme), theme))
        await page.getByTitle(theme === 'dark' ? '切换为深色模式' : '切换为浅色模式').click()
      assert.ok(await ddnsCards.evaluateAll(cards => cards.every(card => card.getBoundingClientRect().height <= 120)), `collapsed DDNS card is too tall at ${width}px/${theme}`)
      assert.ok((await ddnsToggle().boundingBox()).height >= 44)
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), true)
      if (process.env.GATEHOME_BROWSER_ARTIFACTS) {
        await mkdir(process.env.GATEHOME_BROWSER_ARTIFACTS, { recursive: true })
        await page.locator('.ddns-grid').screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, `ddns-compact-${width}-${theme}.png`), animations: 'disabled' })
      }
      await ddnsToggle().click()
      assert.equal(await homeDdns.locator('.dns-records tbody tr:visible').count(), 7)
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), true)
      if (process.env.GATEHOME_BROWSER_ARTIFACTS && [1440, 390].includes(width)) {
        await homeDdns.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, `ddns-expanded-${width}-${theme}.png`), animations: 'disabled' })
      }
      await ddnsToggle().click()
    }
  }
  const ddnsAfter = await api('config')
  await api('config', { revision: ddnsAfter.revision, config: ddnsBefore.config })
  await page.setViewportSize({ width: 1440, height: 1000 })
  console.log('PASS compact DDNS themes/mobile: compact summaries, full lists, 44px toggles and no page overflow in portrait/landscape')

  const credentialsBefore = await api('config')
  const fakeTokens = { ddns: 'TEST_ONLY_BROWSER_DDNS_TOKEN', certificate: 'TEST_ONLY_BROWSER_CERT_TOKEN', github: 'TEST_ONLY_BROWSER_GITHUB_TOKEN' }
  await api('config', {
    revision: credentialsBefore.revision,
    config: { ...credentialsBefore.config,
      ddns: { ...credentialsBefore.config.ddns, groups: [compactGroups[0]] },
      acme: { ...credentialsBefore.config.acme, requests: [{ id: 'reveal-cert', provider: 'cloudflare', domains: ['*.example.test'], enabled: false }] }
    },
    dns_tokens: { 'compact-home': fakeTokens.ddns }, certificate_tokens: { 'reveal-cert': fakeTokens.certificate }, github_token: fakeTokens.github
  })
  await page.reload()
  await route('ddns')
  const credentialDdns = page.locator('[data-group-id=compact-home]')
  await credentialDdns.waitFor()
  if (await credentialDdns.locator('button[aria-controls]').getAttribute('aria-expanded') === 'false')
    await credentialDdns.locator('button[aria-controls]').click()
  let revealCalls = 0
  const countReveals = request => { if (new URL(request.url()).pathname === '/api/credentials/reveal') revealCalls++ }
  page.on('request', countReveals)
  await credentialDdns.getByRole('button', { name: '编辑组', exact: true }).click()
  assert.equal(revealCalls, 0, 'opening an editor fetched a stored token without a click')
  assert.equal(await dialog().locator('[name=token]').inputValue(), '')
  await dialog().getByRole('button', { name: '显示已保存的 Token', exact: true }).click()
  await dialog().getByRole('button', { name: '隐藏 Token', exact: true }).waitFor()
  assert.ok((await dialog().locator('[name=token]').inputValue()) === fakeTokens.ddns, 'DDNS reveal did not return its own credential')
  assert.equal(revealCalls, 1)
  const unchangedTokenSave = page.waitForResponse(response => new URL(response.url()).pathname === '/api/config' && response.request().method() === 'PUT')
  await dialog().getByRole('button', { name: '保存 DDNS 组', exact: true }).click()
  const unchangedTokenResponse = await unchangedTokenSave
  assert.equal(unchangedTokenResponse.status(), 200)
  assert.ok(!JSON.parse(unchangedTokenResponse.request().postData()).dns_tokens, 'merely revealing a token rewrote credentials')
  await page.waitForFunction(() => !document.querySelector('dialog[open]'))
  await credentialDdns.getByRole('button', { name: '编辑组', exact: true }).click()
  assert.equal(await dialog().locator('[name=token]').inputValue(), '')
  await page.route('**/api/credentials/reveal', route => route.fulfill({
    status: 428, json: { error: '请验证身份后继续' }
  }), { times: 1 })
  await dialog().getByRole('button', { name: '显示已保存的 Token', exact: true }).press('Space')
  const verifyDialog = page.getByRole('dialog', { name: '验证管理员身份' })
  await verifyDialog.getByLabel('当前管理密码').fill(secret)
  await verifyDialog.getByRole('button', { name: '验证密码', exact: true }).click()
  await verifyDialog.waitFor({ state: 'hidden' })
  await dialog().getByRole('button', { name: '隐藏 Token', exact: true }).waitFor()
  assert.ok((await dialog().locator('[name=token]').inputValue()) === fakeTokens.ddns)
  await page.evaluate(() => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    document.dispatchEvent(new Event('visibilitychange'))
    delete document.visibilityState
    document.dispatchEvent(new Event('visibilitychange'))
  })
  assert.equal(await dialog().locator('[name=token]').inputValue(), '')
  assert.equal(await dialog().locator('[name=token]').getAttribute('type'), 'password')
  await closeEditor()
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 1000 })
    for (const theme of ['light', 'dark']) {
      if (!await page.locator('body').evaluate((el, theme) => el.classList.contains('theme-' + theme), theme))
        await page.getByTitle(theme === 'dark' ? '切换为深色模式' : '切换为浅色模式').click()
      await credentialDdns.getByRole('button', { name: '编辑组', exact: true }).click()
      const eyeBounds = await dialog().getByRole('button', { name: '显示已保存的 Token', exact: true }).boundingBox()
      assert.ok(eyeBounds.width >= 44 && eyeBounds.height >= 44)
      assert.equal(await dialog().evaluate(el => el.scrollWidth <= el.clientWidth), true)
      if (process.env.GATEHOME_BROWSER_ARTIFACTS) {
        await dialog().screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, `token-eye-${width}-${theme}.png`), animations: 'disabled' })
      }
      await closeEditor()
    }
  }
  await page.setViewportSize({ width: 1440, height: 1000 })
  await route('certificates')
  await page.locator('[data-request-id=reveal-cert]').getByRole('button', { name: '编辑任务', exact: true }).click()
  await dialog().getByRole('button', { name: '显示已保存的 Token', exact: true }).click()
  await dialog().getByRole('button', { name: '隐藏 Token', exact: true }).waitFor()
  assert.ok((await dialog().locator('[name=token]').inputValue()) === fakeTokens.certificate, 'certificate reveal returned the wrong credential')
  await dialog().getByRole('button', { name: '隐藏 Token', exact: true }).click()
  assert.equal(await dialog().locator('[name=token]').inputValue(), '')
  await closeEditor()
  await route('updates')
  const githubInput = page.locator('[name=github_token]')
  const githubField = page.locator('.secret-field').filter({ has: githubInput })
  await githubField.getByRole('button', { name: '显示已保存的 Token', exact: true }).click()
  await githubField.getByRole('button', { name: '隐藏 Token', exact: true }).waitFor()
  assert.ok((await githubInput.inputValue()) === fakeTokens.github, 'GitHub reveal returned the wrong credential')
  assert.equal(await button('保存 GitHub Token').isDisabled(), true, 'viewing GitHub token dirtied the form')
  await githubField.getByRole('button', { name: '隐藏 Token', exact: true }).click()
  const beforeTyping = revealCalls
  await githubInput.fill('TEST_ONLY_UNSAVED_TOKEN')
  await githubField.getByRole('button', { name: '显示 Token', exact: true }).click()
  assert.equal(await githubInput.getAttribute('type'), 'text')
  assert.equal(revealCalls, beforeTyping, 'showing an unsaved token fetched or overwrote it')
  await githubInput.fill('')
  page.off('request', countReveals)
  const credentialsAfter = await api('config')
  assert.ok(Object.values(fakeTokens).every(value => !JSON.stringify(credentialsAfter).includes(value)), 'ordinary config disclosed a token')
  await api('config', { revision: credentialsAfter.revision, config: credentialsBefore.config, github_token: '' })
  await page.reload()
  await page.locator('.page-viewport h1').waitFor()
  console.log('PASS stored token eyes: DDNS/certificate/GitHub on-demand reads, keyboard, hide/close cleanup, background hide and no accidental credential writes')

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
  assert.equal(await page.evaluate(() => typeof navigator.clipboard), 'undefined')
  const clipboardPage = await context.newPage()
  await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: `http://127.0.0.1:${port}` })
  await clipboardPage.goto(`http://127.0.0.1:${port}/healthz`)
  await clipboardPage.evaluate(() => navigator.clipboard.writeText('TEST_ONLY_PREVIOUS_CLIPBOARD'))
  const createdURL = `https://${created.host}:29443`
  const domainButton = page.getByRole('button', { name: '复制访问地址 ' + createdURL, exact: true })
  assert.equal(await domainButton.textContent().then(text => text.trim()), createdURL)
  await page.bringToFront()
  await domainButton.click()
  await page.locator('.toast-desktop-row .toast-text').filter({ hasText: /^访问地址已复制$/ }).waitFor()
  await clipboardPage.bringToFront()
  assert.equal(await clipboardPage.evaluate(() => navigator.clipboard.readText()), createdURL)
  await page.bringToFront()
  assert.equal(await domainButton.evaluate(el => el === document.activeElement), true)
  console.log('PASS route domain copy on ordinary HTTP: actual clipboard contains the full HTTPS URL/port and button focus is preserved')

  const healthButton = page.getByRole('button', { name: /查看 Console review route 的健康诊断/ })
  await healthButton.focus()
  await page.keyboard.press('Enter')
  const healthDialog = page.getByRole('dialog', { name: '上游健康诊断' })
  await healthDialog.waitFor()
  await page.keyboard.press('Escape')
  await healthDialog.waitFor({ state: 'hidden' })

  await clipboardPage.bringToFront()
  await clipboardPage.evaluate(() => navigator.clipboard.writeText('TEST_ONLY_PREVIOUS_CLIPBOARD'))
  await page.bringToFront()
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: {
      writeText: async () => { throw new DOMException('TEST_ONLY_PERMISSION_DENIED', 'NotAllowedError') }
    } })
  })
  await domainButton.click()
  await clipboardPage.bringToFront()
  assert.equal(await clipboardPage.evaluate(() => navigator.clipboard.readText()), createdURL)
  await page.bringToFront()
  const copySuccesses = await page.locator('.toast-success').count()
  await page.evaluate(() => { document.execCommand = () => false })
  await domainButton.click()
  await page.locator('.toast-desktop-row .toast-text').filter({ hasText: /^复制失败，请手动复制访问地址$/ }).waitFor()
  assert.ok(await page.locator('.toast-success').count() <= copySuccesses, 'rejected copy was reported as successful')
  assert.equal(await page.locator('textarea').count(), 0, 'temporary copy input was retained')
  assert.equal(await domainButton.evaluate(el => el === document.activeElement), true)
  await page.evaluate(() => { delete navigator.clipboard; delete document.execCommand })

  await clipboardPage.goto(`http://127.0.0.1:${port}/console/#/routes`)
  await clipboardPage.bringToFront()
  await clipboardPage.locator('#admin-username').fill('admin')
  await clipboardPage.locator('#admin-password').fill(secret)
  await clipboardPage.locator('.login-form button[type=submit]').click()
  await clipboardPage.locator('.routes-page').waitFor()
  assert.equal(await clipboardPage.evaluate(() => isSecureContext), true)
  await clipboardPage.evaluate(() => navigator.clipboard.writeText('TEST_ONLY_PREVIOUS_CLIPBOARD'))
  await clipboardPage.getByRole('button', { name: '复制访问地址 ' + createdURL, exact: true }).click()
  await clipboardPage.locator('.toast-desktop-row .toast-text').filter({ hasText: /^访问地址已复制$/ }).waitFor()
  assert.equal(await clipboardPage.evaluate(() => navigator.clipboard.readText()), createdURL)
  await clipboardPage.close()
  await page.bringToFront()
  console.log('PASS route copy compatibility: denied Clipboard API falls back, rejected copy reports failure, secure-context API still works')
  await button('编辑').last().click()
  await dialog().locator('[name=auth_enabled]').uncheck()
  assert.equal(await dialog().locator('[name=clear_route_password]').count(), 0)
  await dialog().getByRole('button', { name: '保存路由', exact: true }).click()
  await page.waitForFunction(() => !document.querySelector('dialog[open]'))
  saved = await api('config')
  assert.equal(saved.route_passwords_configured[created.group_id + '/' + created.host], true)
  await page.getByRole('link', { name: '查看流量', exact: true }).click()
  await page.waitForFunction(id => document.querySelector('select[name=rule]')?.value === id, created.id)
  console.log('PASS real HTTP save: nested prefix, group TLS defaults, password retention and route traffic link')

  const routesBefore = await api('config')
  const httpFixture = {
    ...created, id: 'copy-http-fixture', group_id: 'test-http', name: 'HTTP copy fixture',
    host: 'plain.example.net', tls: false, enabled: false, auth: { enabled: false }
  }
  const groupedFixture = await api('config', {
    revision: routesBefore.revision,
    config: { ...routesBefore.config,
      groups: [...routesBefore.config.groups, { id: 'empty-fixture', name: '空反代组', enabled: false, http_port: 0, https_port: 0 }],
      routes: [...routesBefore.config.routes, httpFixture]
    }
  })
  await route('routes')
  const httpsSection = page.locator('.route-group[data-group-id=test-https]')
  const httpSection = page.locator('.route-group[data-group-id=test-http]')
  const httpsFold = () => httpsSection.locator('.route-group-toggle')
  const routeSearch = page.getByRole('textbox', { name: '搜索反代服务' })
  await httpsSection.waitFor()
  assert.equal(await httpsSection.locator('.route-card').count(), 1)
  assert.equal(await httpSection.locator('.route-card').count(), 1)
  await page.locator('.route-group[data-group-id=empty-fixture]').getByText('此组暂无反代服务', { exact: true }).waitFor()
  assert.equal(await httpsSection.getByRole('button', { name: '删除反代组 HTTPS 测试组', exact: true }).isDisabled(), true)
  const clipboardProbe = await context.newPage()
  await clipboardProbe.goto(`http://127.0.0.1:${port}/healthz`)
  await clipboardProbe.evaluate(() => navigator.clipboard.writeText('TEST_ONLY_PREVIOUS_CLIPBOARD'))
  await page.bringToFront()
  await httpSection.getByRole('button', { name: '复制访问地址 http://plain.example.net:29080', exact: true }).click()
  await clipboardProbe.bringToFront()
  assert.equal(await clipboardProbe.evaluate(() => navigator.clipboard.readText()), 'http://plain.example.net:29080')
  await clipboardProbe.close()
  await page.bringToFront()
  await httpsFold().click()
  assert.equal(await httpsSection.locator('.route-group-body').isVisible(), false)
  assert.equal(await httpSection.locator('.route-group-body').isVisible(), true)
  await page.reload()
  await httpsSection.waitFor()
  assert.equal(await httpsFold().getAttribute('aria-expanded'), 'false')
  await httpsFold().press('Space')
  assert.equal(await httpsSection.locator('.route-card').isVisible(), true)
  await httpsFold().press('Enter')
  await httpsSection.getByRole('button', { name: '编辑组', exact: true }).click()
  assert.equal(await dialog().locator('[name=name]').inputValue(), 'HTTPS 测试组')
  await closeEditor()
  assert.equal(await httpsFold().getAttribute('aria-expanded'), 'false', 'editing a group toggled its fold state')
  await routeSearch.fill(createdURL)
  assert.equal(await page.locator('.route-group').count(), 1)
  assert.equal(await httpsFold().getAttribute('aria-expanded'), 'true', 'search hid the matching service inside a collapsed group')
  await routeSearch.fill('')
  await page.getByRole('button', { name: /^HTTP 测试组/ }).click()
  assert.equal(await page.locator('.route-group').count(), 1)
  assert.equal(await httpSection.locator('.route-card').isVisible(), true)
  await page.getByRole('button', { name: /^已停用/ }).click()
  assert.equal(await httpSection.locator('.route-card').count(), 1)
  await page.getByRole('button', { name: /^全部路由/ }).click()
  await routeSearch.fill('no-such-route-fixture')
  await page.getByText('暂无匹配的反代服务', { exact: true }).waitFor()
  await routeSearch.fill('')
  assert.deepEqual((await api('config')).config, groupedFixture.config, 'group display or copying changed gateway configuration')
  console.log('PASS route groups: correct membership, HTTP/HTTPS full URL copy, independent folds, persisted state, keyboard, search and filters')

  await httpSection.locator('.route-group-toggle').click()
  for (const [width, height] of [[1440, 1000], [390, 844], [320, 667], [844, 390]]) {
    await page.setViewportSize({ width, height })
    // Wait for the sidebar margin transition before checking the responsive layout.
    await page.locator('.main-wrapper').evaluate(async el => {
      await document.fonts.ready
      await Promise.all(el.getAnimations().map(animation => animation.finished.catch(() => {})))
    })
    for (const theme of ['light', 'dark']) {
      if (!await page.locator('body').evaluate((el, theme) => el.classList.contains('theme-' + theme), theme))
        await page.getByTitle(theme === 'dark' ? '切换为深色模式' : '切换为浅色模式').click()
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `route groups overflow at ${width}px/${theme}`)
      assert.ok((await httpsFold().boundingBox()).height >= 44)
      assert.equal(await httpsSection.locator('.domain-text').textContent(), createdURL)
      if (width <= 640) {
        assert.ok(await httpsSection.locator('.group-actions button').evaluateAll(buttons => buttons.every(button => button.getBoundingClientRect().height >= 44)))
        assert.ok(await page.locator('.proxy-metrics .metric-icon').evaluateAll(icons => icons.every(icon => {
          const parent = icon.getBoundingClientRect()
          const child = icon.firstElementChild.getBoundingClientRect()
          return parent.width === parent.height && child.width === child.height && child.width <= parent.width
        })))
        assert.ok(await httpsSection.locator('.service-active').count(), 'per-route active connections missing')
      }
      if (process.env.GATEHOME_BROWSER_ARTIFACTS) {
        await page.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, `route-groups-${width}-${theme}.png`), animations: 'disabled', fullPage: true })
      }
    }
  }
  await page.setViewportSize({ width: 1440, height: 1000 })
  await httpsSection.getByRole('button', { name: '编辑组', exact: true }).click()
  await dialog().locator('[name=https_port]').fill('30443')
  await dialog().getByRole('button', { name: '保存反代组', exact: true }).click()
  await page.waitForFunction(() => !document.querySelector('dialog[open]'))
  await httpsSection.getByRole('button', { name: `复制访问地址 https://${created.host}:30443`, exact: true }).waitFor()
  assert.deepEqual((await api('config')).config.routes, groupedFixture.config.routes)
  const routesAfter = await api('config')
  await api('config', { revision: routesAfter.revision, config: routesBefore.config })
  await page.reload()
  await page.getByRole('button', { name: '复制访问地址 ' + createdURL, exact: true }).waitFor()
  console.log('PASS grouped route themes/mobile: no overflow, 44px controls, full addresses and listener edits update displayed URLs')

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

  await route('forwards')
  const forwardMetrics = page.locator('[data-forward-id="traffic-fixture"]')
  await forwardMetrics.getByText('合计', { exact: true }).waitFor()
  assert.match(await (await fetch(`http://127.0.0.1:${forwardPort}/`)).text(), /TEST_ONLY_UPSTREAM/)
  const udpPayload = Buffer.from('TEST_ONLY_UDP_TRAFFIC')
  const udpClient = createSocket('udp4')
  try {
    const reply = await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('UDP traffic fixture did not reply')), 3000)
      udpClient.once('error', error => { clearTimeout(timer); reject(error) })
      udpClient.once('message', message => { clearTimeout(timer); resolve(message) })
      udpClient.send(udpPayload, forwardPort, '127.0.0.1')
    })
    assert.deepEqual(reply, udpPayload)
  } finally {
    udpClient.close()
  }
  await page.waitForFunction(() => {
    const samples = document.querySelectorAll('[data-forward-id="traffic-fixture"] [data-protocol]')
    return [...samples].some(row => row.textContent.includes('B/s') && !row.textContent.includes('下载 0 B/s'))
  }, null, { timeout: 15000 })
  await page.waitForFunction(async () => {
    const report = await fetch('/api/statistics/traffic?source=forward&rule=traffic-fixture').then(response => response.json())
    return report.rules?.length === 2
  }, null, { polling: 500 })
  await forwardMetrics.getByRole('link', { name: '流量明细' }).click()
  await page.locator('.traffic-table [data-protocol="tcp"]').waitFor()
  await page.locator('.traffic-table [data-protocol="udp"]').waitFor()
  assert.equal(await page.locator('select[name=rule]').inputValue(), 'traffic-fixture')
  await page.locator('select[name=protocol]').selectOption('udp')
  await button('查询流量').click()
  await page.locator('.traffic-table [data-protocol="tcp"]').waitFor({ state: 'hidden' })
  assert.equal(await page.locator('[data-total="upload"]').getAttribute('data-bytes'), String(udpPayload.length))
  assert.equal(await page.locator('[data-total="download"]').getAttribute('data-bytes'), String(udpPayload.length))
  await page.setViewportSize({ width: 320, height: 844 })
  await page.locator('.main-wrapper').evaluate(el => Promise.all(el.getAnimations().map(animation => animation.finished)))
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
  await route('forwards')
  const forwardCard = page.locator('.forward-card').filter({ hasText: '流量测试转发' })
  const pipeline = forwardCard.locator('.pipeline-box')
  await pipeline.waitFor()
  assert.ok(await pipeline.evaluate(el => {
    const values = [...el.querySelectorAll('.node-val')]
    return values[0].getBoundingClientRect().bottom < values[1].getBoundingClientRect().top &&
      values.every(value => {
        const text = document.createRange()
        text.selectNodeContents(value)
        return text.getClientRects().length === 1 && value.scrollWidth <= value.clientWidth + 1
      })
  }), 'forwarding endpoints did not fit on separate rows')
  assert.equal(await forwardCard.locator('.status-indicator').evaluate(el => getComputedStyle(el).whiteSpace), 'nowrap')
  await page.setViewportSize({ width: 1440, height: 1000 })
  console.log('PASS port forwarding traffic: real TCP and UDP, live rates, per-rule history link, protocol filter and mobile layout')

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
  await page.getByRole('heading', { name: '系统概览', exact: true }).waitFor()
  await trigger.click()
  for (let n = 0; n < 8; n++) {
    await page.keyboard.press('Tab')
    assert.equal(await page.evaluate(() => document.activeElement === document.body || Boolean(document.activeElement.closest('dialog[open]'))), true, 'Tab focused background content')
  }
  await page.keyboard.press('Escape')
  await page.waitForFunction(() => !document.querySelector('dialog[open]'))
  assert.equal(await trigger.evaluate(el => el === document.activeElement), true, 'Escape did not restore focus to the command trigger')
  await trigger.click()
  await page.locator('.cmdk-cat-btn', { hasText: '反代与转发' }).click()
  await input.fill(String(forwardPort))
  await input.press('Enter')
  await page.waitForFunction(() => location.hash.startsWith('#/forwards') && !document.querySelector('dialog[open]'))
  await page.waitForFunction(
    expected => document.querySelector('input[aria-label="搜索端口转发"]')?.value === expected,
    String(forwardPort)
  )
  await page.getByRole('searchbox', { name: '搜索端口转发' }).fill('')
  await trigger.click()
  await input.fill('scan.home.example.test')
  await input.press('Enter')
  await page.waitForFunction(() => location.hash.startsWith('#/routes') && !document.querySelector('dialog[open]'))
  await page.waitForFunction(() => document.querySelector('input[aria-label="搜索反代服务"]')?.value === 'scan.home.example.test')
  await page.getByRole('textbox', { name: '搜索反代服务' }).fill('')
  await page.locator('.chip-btn', { hasText: '全部路由' }).click()
  console.log('PASS command palette: filtered Enter, entity deep-linking, native focus boundary and Escape focus restoration')

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
  const currentPasswordEye = accountDialog.locator('[aria-controls="account-current-password"]')
  await currentPasswordEye.click()
  assert.equal(await page.locator('#account-current-password').getAttribute('type'), 'text')
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
  assert.equal(await page.locator('#account-current-password').getAttribute('type'), 'password')
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
  const issuedClientToken = await page.getByLabel('新 API Token', { exact: true }).inputValue()
  assert.ok(!!issuedClientToken && scoped.revealable, 'new MCP token was not marked revealable')
  await button('关闭凭据').click()
  const clientRow = () => page.locator(`[data-client-id="${scoped.id}"]`)
  await clientRow().getByRole('button', { name: '编辑', exact: true }).click()
  await dialog().getByRole('button', { name: '显示已保存的 Token', exact: true }).click()
  await dialog().getByRole('button', { name: '隐藏 Token', exact: true }).waitFor()
  assert.ok((await dialog().getByLabel('客户端 Token', { exact: true }).inputValue()) === issuedClientToken, 'saved MCP client token could not be read after closing its creation panel')
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
  const issuedLegacyToken = await page.getByLabel('新 API Token', { exact: true }).inputValue()
  assert.ok(!!issuedLegacyToken && globalSettings.token_reveal_available, 'new compatibility token was not marked revealable')
  await button('关闭凭据').click()
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

  await route('mcp')
  const legacyField = page.locator('.secret-field').filter({ has: page.locator('#mcp-legacy-token') })
  await legacyField.getByRole('button', { name: '显示已保存的 Token', exact: true }).click()
  await legacyField.getByRole('button', { name: '隐藏 Token', exact: true }).waitFor()
  assert.ok((await page.locator('#mcp-legacy-token').inputValue()) === issuedLegacyToken, 'compatibility token did not survive a service restart')
  await legacyField.getByRole('button', { name: '隐藏 Token', exact: true }).click()
  assert.equal(await page.locator('#mcp-legacy-token').inputValue(), '')
  await clientRow().getByRole('button', { name: '编辑', exact: true }).click()
  await dialog().getByRole('button', { name: '显示已保存的 Token', exact: true }).click()
  await dialog().getByRole('button', { name: '隐藏 Token', exact: true }).waitFor()
  assert.ok((await dialog().getByLabel('客户端 Token', { exact: true }).inputValue()) === issuedClientToken, 'client token did not survive a service restart')
  await closeEditor()
  console.log('PASS MCP token eyes: compatibility/client tokens remain revealable after a service restart and temporary values clear on hide/close')

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

  for (const [width, height] of [[320, 640], [1280, 640]]) {
    await page.setViewportSize({ width, height })
    for (const [view, openLabel, saveLabel] of [
      ['overview', '配置组件与顺序', '保存概览布局'],
      ['routes', '新建反代服务', '保存路由'],
      ['ddns', '添加 DDNS 组', '保存 DDNS 组']
    ]) {
      await route(view)
      await button(openLabel).click()
      const save = dialog().getByRole('button', { name: saveLabel, exact: true })
      await save.waitFor()
      const before = await save.boundingBox()
      assert.ok(before.y >= 0 && before.y + before.height <= height, `${view}: save is clipped at ${width}px`)
      const form = dialog().locator('form')
      const scrolled = await form.evaluate(el => {
        el.scrollTop = el.scrollHeight
        return el.scrollTop
      })
      assert.ok(scrolled > 0, `${view}: form did not scroll at ${width}px`)
      assert.deepEqual(await save.boundingBox(), before, `${view}: footer moved with the form`)
      assert.ok(await save.evaluate(el => !!el.form), `${view}: save is detached from its form`)
      await closeEditor()
    }
    const account = page.getByRole('button', { name: /^账户菜单/ })
    await account.click()
    const popup = page.locator('.user-dropdown-popover')
    await popup.waitFor()
    await popup.getByRole('button').first().focus()
    await page.keyboard.press('Escape')
    await popup.waitFor({ state: 'hidden' })
    assert.equal(await account.getAttribute('aria-expanded'), 'false')
    assert.ok(await account.evaluate(el => el === document.activeElement))
  }
  console.log('PASS long dialogs and account menu on compact desktop and mobile: anchored form actions and Escape focus return')

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
    'firewall-logs', 'logs', 'ip-blocks', 'statistics', 'traffic', 'assistant', 'assistant/settings', 'mcp', 'updates', 'backups', 'settings'
  ]) {
    await route(view)
    await page.waitForFunction(view => document.querySelector('.sidebar-nav a.active')?.hash === '#/' + view, view)
  }
  for (const [view, field] of [['logs', '请求方法'], ['firewall-logs', '检测引擎']]) {
    await route(view)
    const explorer = page.locator('.log-explorer')
    await explorer.getByRole('combobox', { name: '反代规则' }).waitFor()
    await explorer.getByRole('button', { name: '更多筛选', expanded: false }).click()
    await explorer.getByRole('button', { name: '收起', expanded: true }).waitFor()
    await explorer.getByRole('combobox', { name: field }).waitFor()
    const drawerLayout = await explorer.evaluate(el => {
      const bar = el.querySelector('.report-filter-bar')?.getBoundingClientRect()
      const drawer = el.querySelector('.filter-advanced-drawer')?.getBoundingClientRect()
      const cols = [...el.querySelectorAll('.advanced-grid .filter-col')].map(node => node.getBoundingClientRect())
      return {
        barWidth: bar?.width || 0,
        drawerWidth: drawer?.width || 0,
        drawerLeftGap: drawer && bar ? drawer.left - bar.left : 999,
        distinctRows: new Set(cols.map(rect => Math.round(rect.top / 10))).size,
        colCount: cols.length
      }
    })
    assert.ok(drawerLayout.drawerWidth >= drawerLayout.barWidth - 48, `advanced filter drawer collapsed: ${JSON.stringify(drawerLayout)}`)
    assert.ok(drawerLayout.drawerLeftGap < 40, `advanced filter drawer shifted right: ${JSON.stringify(drawerLayout)}`)
    assert.ok(drawerLayout.distinctRows < drawerLayout.colCount, `advanced filter fields stacked vertically: ${JSON.stringify(drawerLayout)}`)
  }
  await page.route('**/api/logs?*', async reqRoute => {
    const url = reqRoute.request().url()
    if (!url.includes('scope=access')) return reqRoute.continue()
    await reqRoute.fulfill({
      json: {
        total: 1, page: 1, pages: 1, size: 20, through: 1,
        entries: [{
          id: 9901, time: new Date().toISOString(), category: 'access', action: '反向代理请求',
          target: 'scan.home.example.test', rule: 'test-https/scan.home.example.test',
          method: 'GET', path: '/wp-login.php', remote: '198.51.100.77', remote_region: '测试网络',
          status: 403, ok: false, duration_ms: 4, message: '恶意扫描探测', outcome: 'blocked'
        }]
      }
    })
  })
  await route('logs')
  const quickBanBtn = page.locator('.log-explorer .quick-ban-btn').first()
  await quickBanBtn.waitFor()
  const logsViewport = page.getByRole('region', { name: '日志记录' })
  await page.setViewportSize({ width: 320, height: 844 })
  await page.getByText('向左滑动查看右侧列', { exact: true }).waitFor()
  assert.equal(await logsViewport.getAttribute('tabindex'), '0')
  await logsViewport.focus()
  await page.keyboard.press('ArrowRight')
  await page.waitForFunction(() => document.querySelector('.log-explorer .report-table-wrap').scrollLeft > 0)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.getByText('向左滑动查看右侧列', { exact: true }).waitFor({ state: 'hidden' })
  await quickBanBtn.click()
  const logDialog = page.locator('dialog.report-dialog[open]')
  await logDialog.waitFor()
  await logDialog.getByRole('button', { name: '15 分钟', exact: true }).click()
  await logDialog.getByRole('button', { name: '封禁此 IP', exact: true }).click()
  await page.waitForFunction(() => !document.querySelector('dialog.report-dialog[open]'))
  await page.locator('.log-explorer .quick-unblock-btn').first().waitFor()
  await page.locator('.log-explorer .quick-unblock-btn').first().click()
  await quickBanBtn.waitFor()
  await page.unroute('**/api/logs?*')
  console.log('PASS log explorer quick IP ban/unblock loop on access logs')
  await page.getByRole('button', { name: '关闭此通知' }).evaluateAll(buttons => buttons.forEach(button => button.click()))
  await route('icons')
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

  const beforeLinkImport = await api('icons/stats')
  await button('通过链接导入').click()
  const linkDialog = page.getByRole('dialog', { name: '通过链接导入', exact: true })
  const imageLink = linkDialog.getByLabel('图片链接', { exact: true })
  const linkedURL = `http://127.0.0.1:${upstreamPort}/linked-icon?theme=light`
  await imageLink.fill(linkedURL)
  await linkDialog.getByRole('button', { name: '预览图标', exact: true }).click()
  const linkedPreview = linkDialog.getByRole('img', { name: '待导入图标预览' })
  await linkedPreview.evaluate(img => img.decode())
  assert.match(await linkedPreview.getAttribute('src'), /^data:image\/svg\+xml;base64,/)
  assert.equal((await api('icons/stats')).total, beforeLinkImport.total, 'preview saved an icon')
  await linkDialog.getByRole('button', { name: '取消', exact: true }).click()
  assert.equal((await api('icons/stats')).total, beforeLinkImport.total, 'cancelling saved an icon')

  await button('通过链接导入').click()
  await imageLink.fill(linkedURL)
  await linkDialog.getByRole('button', { name: '预览图标', exact: true }).click()
  await linkedPreview.evaluate(img => img.decode())
  await imageLink.fill(linkedURL + '&changed=1')
  assert.equal(await linkedPreview.count(), 0, 'changing the URL retained a stale preview')
  assert.equal(await linkDialog.getByRole('button', { name: '确认导入', exact: true }).count(), 0)
  await linkDialog.getByRole('button', { name: '预览图标', exact: true }).click()
  await linkedPreview.evaluate(img => img.decode())
  for (const [width, theme] of [[1440, 'light'], [390, 'light'], [320, 'dark']]) {
    await page.setViewportSize({ width, height: 900 })
    // A modal intentionally traps focus, so set the existing theme preference directly.
    await page.locator('body').evaluate((body, theme) => { body.className = 'theme-' + theme }, theme)
    await linkedPreview.evaluate(img => img.decode())
    assert.equal(await linkDialog.evaluate(el => el.scrollWidth <= el.clientWidth), true)
    const bounds = await linkDialog.boundingBox()
    assert.ok(bounds.x >= 0 && bounds.x + bounds.width <= width)
    if (process.env.GATEHOME_BROWSER_ARTIFACTS) {
      await page.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, `icons-link-${width}-${theme}.png`) })
    }
  }
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.locator('body').evaluate(body => { body.className = 'theme-light' })
  const previewedRequests = linkedRequests
  linkedContent = linkedSVG.replace('#6754ed', '#ef4444')
  await linkDialog.getByRole('button', { name: '确认导入', exact: true }).click()
  await linkDialog.waitFor({ state: 'hidden' })
  await cards.getByText('linked-icon', { exact: true }).waitFor()
  assert.equal(linkedRequests, previewedRequests, 'confirmation downloaded a different image')
  const linkedIcons = await api('icons?source=uploads&keyword=linked-icon')
  assert.equal(linkedIcons.items.length, 1)
  assert.equal(linkedIcons.items[0].format, 'svg')
  const localSVG = await page.evaluate(url => fetch(url).then(response => response.text()), linkedIcons.items[0].url)
  assert.equal(localSVG, linkedSVG, 'imported icon differs from its preview')
  assert.equal((await api('icons/stats')).total, beforeLinkImport.total + 1)
  linkedContent = linkedSVG
  console.log('PASS URL icon import: SVG preview, cancel without saving, changed-link reset and exact preview content on confirmation')

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

  const iconRequestURL = new URL(previewURL, page.url()).href
  const firstCardIcon = page.locator('.route-card').first().locator('.app-icon-wrap')
  await page.route(iconRequestURL, request => request.fulfill({ contentType: 'image/svg+xml', body: 'invalid icon' }))
  await page.reload()
  await firstCardIcon.locator('svg').waitFor()
  assert.equal(await firstCardIcon.locator('img').count(), 0)
  await page.unroute(iconRequestURL)
  await button('刷新').click()
  await firstCardIcon.locator('img').evaluate(img => img.decode())
  console.log('PASS unavailable route icon: default icon displays and refresh retries the custom asset')

  await button('编辑').first().click()
  await routeDialog.getByRole('textbox', { name: '图片地址', exact: true }).fill(linkedURL)
  await routeDialog.getByRole('button', { name: '导入', exact: true }).click()
  await routePreview.waitFor()
  await page.waitForFunction(() => document.querySelector('.route-icon-img')?.getAttribute('src')?.startsWith('/api/route-images/'))
  await routePreview.evaluate(img => img.decode())
  assert.equal(await routePreview.evaluate(img => img.naturalWidth / img.naturalHeight), 2)
  const onlineSVGRef = await routePreview.getAttribute('src')
  assert.match(onlineSVGRef, /^\/api\/route-images\/[a-f0-9]{64}$/)
  linkedContent = ''
  await routeDialog.getByRole('button', { name: '保存路由', exact: true }).click()
  await routeDialog.waitFor({ state: 'hidden' })
  await page.reload()
  await page.locator(`.routes-grid img[src="${onlineSVGRef}"]`).first().evaluate(img => img.decode())
  const routeSVGResponse = await page.evaluate(async url => {
    const response = await fetch(url)
    return { type: response.headers.get('content-type'), content: await response.text() }
  }, onlineSVGRef)
  assert.equal(routeSVGResponse.type, 'image/svg+xml')
  assert.equal(routeSVGResponse.content, linkedSVG)
  linkedContent = linkedSVG
  console.log('PASS route URL SVG: query URL import, full aspect ratio, route save and reload after the source disappears')

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
  const popover = page.locator('.notification-popover')
  const center = page.locator('.notice-page')
  await notificationTrigger.click()
  await popover.waitFor()
  assert.ok(!new URL(page.url()).hash.startsWith('#/notifications'))
  await page.keyboard.press('Escape')
  await popover.waitFor({ state: 'hidden' })
  assert.equal(await notificationTrigger.evaluate(el => el === document.activeElement), true)
  await notificationTrigger.click()
  await popover.getByRole('link', { name: '查看全部通知' }).click()
  await center.getByRole('heading', { name: '通知中心' }).waitFor()
  assert.ok(await page.locator('.nav-link.active[href$="/notifications"]').isVisible())
  const rows = center.locator('.notice-item')
  await rows.filter({ hasText: '登录已过期' }).getByRole('button', { name: '标为已读' }).press('Enter')
  await center.getByRole('button', { name: /^已读/ }).click()
  assert.ok(await rows.filter({ hasText: '登录已过期' }).isVisible())
  await center.getByLabel('通知分类').selectOption('backup')
  await rows.filter({ hasText: 'WebDAV 定时备份' }).getByRole('link', { name: '查看详情' }).click()
  await page.waitForURL('**/#/backups')
  await notificationTrigger.click()
  await popover.getByRole('link', { name: '查看全部通知' }).click()
  await center.getByLabel('搜索通知').fill('定时备份')
  assert.equal(await rows.count(), 1)
  await center.getByLabel('搜索通知').press('Escape')
  await center.getByRole('button', { name: '全部标为已读' }).click()
  assert.equal(await notificationTrigger.locator('.notification-count').count(), 0)
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 844 })
    await page.locator('.main-wrapper').evaluate(el => Promise.all(el.getAnimations().map(animation => animation.finished.catch(() => {}))))
    assert.ok(await center.evaluate(el => el.scrollWidth <= el.clientWidth))
    await notificationTrigger.click()
    await popover.waitFor()
    const box = await popover.boundingBox()
    assert.ok(box.x >= 0 && box.x + box.width <= width + 1)
    await page.keyboard.press('Escape')
    await popover.waitFor({ state: 'hidden' })
    await center.getByRole('link', { name: '故障推送' }).click()
    await center.getByLabel('启用推送').waitFor()
    assert.ok(await center.evaluate(el => el.scrollWidth <= el.clientWidth))
    await center.getByRole('link', { name: '通知记录' }).click()
  }
  await rows.filter({ hasText: '网关证书' }).getByRole('button', { name: '删除此通知' }).click()
  await rows.filter({ hasText: '网关证书' }).waitFor({ state: 'detached' })
  await center.getByRole('button', { name: '清空', exact: true }).click()
  await center.getByText('暂无通知记录').waitFor()
  await page.reload()
  await center.getByText('暂无通知记录').waitFor()
  await page.setViewportSize({ width: 1440, height: 1000 })
  await center.getByRole('link', { name: '故障推送' }).click()
  await center.getByRole('button', { name: /Webhook/ }).click()
  await center.getByLabel('启用推送').check()
  await center.getByLabel('证书即将到期').uncheck()
  await center.getByLabel('Webhook 地址', { exact: true }).fill(`http://127.0.0.1:${upstreamPort}/test-webhook`)
  await center.getByRole('button', { name: '保存', exact: true }).click()
  await center.getByText('推送设置已保存').waitFor()
  assert.equal(await center.getByLabel('Webhook 地址', { exact: true }).inputValue(), '')
  await center.getByRole('button', { name: '发送测试通知' }).click()
  await center.getByText('接收端已接受测试通知').waitFor()
  assert.equal(webhookTests, 1)
  console.log('PASS notification webhook: saved address stays masked and the local receiver accepts the test POST')
  assert.deepEqual((await api('notifications/webhook')).events, ['upstream', 'backup'])
  console.log('PASS notification page: sidebar, persistent read history, filters, details, mobile layout and channel subscriptions')

  await route('routes')
  await page.getByRole('button', { name: '关闭此通知' }).evaluateAll(buttons => buttons.forEach(button => button.click()))
  const sidebarNav = page.getByRole('navigation', { name: '主导航' })
  const expandedSidebarGroups = () => sidebarNav.locator('.nav-section-title[aria-expanded="true"]').allTextContents()
  const monitoringGroup = sidebarNav.getByRole('button', { name: '监控与日志', exact: true })
  assert.deepEqual(await expandedSidebarGroups(), ['服务接入'])
  await sidebarNav.getByRole('button', { name: '服务接入', exact: true }).click()
  assert.deepEqual(await expandedSidebarGroups(), [])
  await route('forwards')
  assert.deepEqual(await expandedSidebarGroups(), ['服务接入'])
  await monitoringGroup.click()
  await route('settings/security')
  assert.deepEqual(await expandedSidebarGroups(), ['监控与日志', '系统维护'])
  assert.equal(await sidebarNav.getByRole('link', { name: '设置', exact: true }).getAttribute('aria-current'), 'location')
  await page.reload()
  await sidebarNav.getByRole('link', { name: '设置', exact: true }).waitFor()
  assert.deepEqual(await expandedSidebarGroups(), ['监控与日志', '系统维护'])
  await monitoringGroup.click()
  await route('firewall')
  assert.deepEqual(await expandedSidebarGroups(), ['访问安全'])
  assert.equal(await sidebarNav.getByRole('link', { name: '防火墙管理', exact: true }).getAttribute('aria-current'), 'page')
  await route('routes')
  await page.setViewportSize({ width: 390, height: 900 })
  const sidebarTrigger = button('打开侧边栏导航')
  await sidebarTrigger.click()
  await page.waitForFunction(() => document.activeElement?.getAttribute('aria-label') === '关闭侧边栏导航')
  await page.keyboard.press('Escape')
  assert.equal(await sidebarTrigger.getAttribute('aria-expanded'), 'false')
  assert.equal(await sidebarTrigger.evaluate(el => el === document.activeElement), true)
  await sidebarTrigger.click()
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.waitForFunction(() => !document.querySelector('.app-sidebar').classList.contains('mobile-open'))
  await page.setViewportSize({ width: 390, height: 900 })
  assert.equal(await sidebarTrigger.getAttribute('aria-expanded'), 'false')
  console.log('PASS sidebar expansion: active group, persisted manual groups, nested settings, aliases, Escape and resize')

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
      }
      const groups = navigation.locator('.nav-group')
      assert.equal(await groups.count(), 6)
      const service = navigation.getByRole('button', { name: '服务接入', exact: true })
      await service.click()
      assert.equal(await service.getAttribute('aria-expanded'), 'false')
      for (const group of await groups.all()) {
        const toggle = group.getByRole('button')
        if (await toggle.getAttribute('aria-expanded') === 'false') await toggle.click()
      }
      assert.equal(await navigation.getByRole('link').count(), 23)
      if (width < 1024)
        assert.ok(await navigation.locator('.nav-link, .nav-section-title').evaluateAll(controls => controls.every(control => control.getBoundingClientRect().height >= 44)))
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
        await page.waitForFunction(() =>
          window.location.hash === '#/settings' &&
          !document.querySelector('.app-sidebar').classList.contains('mobile-open'))
        await route('routes')
      }
    }
  }
  console.log('PASS sidebar: both themes, 23 links in six collapsible groups and 44px mobile targets from 320px to 1440px')

  await page.setViewportSize({ width: 1440, height: 1000 })
  const wireGuardSocket = createSocket('udp4')
  await new Promise((resolve, reject) => wireGuardSocket.once('error', reject).bind(0, '127.0.0.1', resolve))
  const wireGuardPort = wireGuardSocket.address().port
  await new Promise(resolve => wireGuardSocket.close(resolve))
  assert.equal(await page.evaluate(async port => {
    const response = await fetch('/api/remote-home/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Gatehouse-Request': '1' },
      body: JSON.stringify({ endpoint: 'vpn.example.test', port, lan: '192.168.2.0/24', tunnel: '10.77.0.0/24' })
    })
    return response.status
  }, wireGuardPort), 200)
  const baseRuntime = await api('runtime')
  let runtimeSampleTick = 0
  await page.route('**/api/runtime', async reqRoute => {
    const payload = structuredClone(baseRuntime)
    runtimeSampleTick++
    if (payload?.resources?.cpu_error || payload?.resources?.cpu_percent == null) {
      payload.resources.cpu_error = ''
      payload.resources.cpu_percent = runtimeSampleTick === 1 ? 18.4 : 26.8
    }
    if (payload?.resources?.memory_error || !payload?.resources?.memory_total) {
      payload.resources.memory_error = ''
      payload.resources.memory_total = 8 * 1024 * 1024 * 1024
      payload.resources.memory_used = (runtimeSampleTick === 1 ? 3.2 : 3.6) * 1024 * 1024 * 1024
    }
    payload.sampled_at = new Date(Date.now() + runtimeSampleTick * 1000).toISOString()
    await reqRoute.fulfill({ json: payload })
  })
  await route('runtime')
  const runtime = page.locator('.runtime-page')
  const ports = runtime.getByRole('list', { name: 'GateHome 监听端口' })
  await runtime.getByText('当前会话', { exact: true }).waitFor()
  await page.setViewportSize({ width: 320, height: 844 })
  await runtime.getByRole('button', { name: '查看监听清单 →', exact: true }).click()
  const listenerHeading = runtime.getByRole('heading', { name: 'GateHome 监听端口' })
  assert.ok(await listenerHeading.evaluate(el => document.activeElement === el &&
    el.getBoundingClientRect().top >= 60 && el.getBoundingClientRect().bottom <= innerHeight))
  await page.setViewportSize({ width: 1440, height: 1000 })
  await runtime.locator('.sparkline-svg').first().waitFor()
  assert.equal(await runtime.locator('.sparkline-svg').count(), 2, 'CPU and memory sparklines did not both render')
  await runtime.getByRole('button', { name: '刷新数据', exact: true }).click()
  await page.waitForFunction(() => [...document.querySelectorAll('.runtime-page .sparkline-meta')].some(el => el.textContent.includes('2/30')))
  await page.unroute('**/api/runtime')
  for (const protocol of ['TCP', 'UDP']) {
    const row = ports.locator('.listener-row').filter({ hasText: '流量测试转发' }).filter({ hasText: protocol })
    assert.equal(await row.count(), 1)
  }
  const wireGuardRow = ports.getByRole('listitem').filter({ hasText: 'WireGuard' })
  await wireGuardRow.getByText(`:${wireGuardPort}`, { exact: true }).waitFor()
  await wireGuardRow.getByText('UDP', { exact: true }).waitFor()
  await wireGuardRow.getByText('绑定 所有网卡', { exact: true }).waitFor()
  const listenerFilters = runtime.getByRole('group', { name: '监听分类筛选' })
  const remoteHomeFilter = listenerFilters.getByRole('button', { name: '远程回家', exact: true })
  await remoteHomeFilter.click()
  assert.equal(await remoteHomeFilter.getAttribute('aria-pressed'), 'true')
  assert.equal(await ports.getByRole('listitem').count(), 1)
  const protocolFilter = runtime.getByRole('combobox', { name: '监听协议' })
  await protocolFilter.selectOption('TCP')
  await runtime.getByText('暂无符合条件的监听端口', { exact: true }).waitFor()
  await protocolFilter.selectOption('UDP')
  await wireGuardRow.waitFor()
  const openHistory = runtime.locator('.history-toggle').first()
  await openHistory.waitFor()
  await openHistory.click()
  const timeline = runtime.getByRole('group', { name: '逐分钟检测结果' })
  await timeline.waitFor()
  assert.equal(await timeline.getByRole('button').count(), 60)
  await runtime.getByRole('button', { name: '15 分钟', exact: true }).click()
  assert.equal(await timeline.getByRole('button').count(), 15)
  await timeline.locator('button[tabindex="0"]').focus()
  await page.keyboard.press('ArrowLeft')
  assert.equal(await timeline.locator('button[tabindex="0"]').evaluate(el => el === document.activeElement), true)
  if (process.env.GATEHOME_BROWSER_ARTIFACTS)
    await page.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, 'runtime-status.png') })
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 844 })
    await page.locator('.main-wrapper').evaluate(el => Promise.all(el.getAnimations().map(animation => animation.finished.catch(() => {}))))
    assert.ok(await runtime.evaluate(el => el.scrollWidth <= el.clientWidth))
    assert.ok(await page.evaluate(() => document.querySelector('.page-viewport').scrollWidth <= innerWidth))
    assert.ok((await remoteHomeFilter.boundingBox()).height >= 44)
  }
  await page.setViewportSize({ width: 1440, height: 1000 })
  await runtime.getByRole('link', { name: '故障推送', exact: true }).click()
  await center.getByRole('button', { name: /^WxPusher/ }).click()
  await center.getByLabel('AppToken', { exact: true }).fill('AT_TEST_ONLY_BROWSER')
  await center.getByLabel('接收用户 UID', { exact: true }).fill('UID_TEST_ONLY_BROWSER')
  await center.getByLabel('WebDAV 备份失败', { exact: true }).uncheck()
  await center.getByRole('button', { name: '保存', exact: true }).click()
  await center.getByText('推送设置已保存').waitFor()
  assert.equal(await center.getByLabel('AppToken', { exact: true }).inputValue(), '')
  assert.deepEqual((await api('notifications/wxpusher')).events, ['upstream', 'certificate'])
  await page.route('**/api/notifications/wxpusher/test', route => route.fulfill({ json: { sent: true } }))
  await center.getByRole('button', { name: '发送测试通知' }).click()
  await center.getByText('WxPusher 已受理测试通知').waitFor()
  if (process.env.GATEHOME_BROWSER_ARTIFACTS)
    await page.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, 'runtime-wxpusher.png') })
  console.log('PASS runtime status: shared TCP/UDP port, session data, history range and keyboard, WxPusher setup')
  await route('updates')
  const updateChannelSelect = page.locator('select[name=channel]')
  await updateChannelSelect.waitFor()
  if ((await api('status')).version.includes('-beta.')) {
    assert.equal(await updateChannelSelect.inputValue(), 'beta')
    let checkedChannel = ''
    await page.route('**/api/maintenance/check-online-update', async route => {
      checkedChannel = JSON.parse(route.request().postData() || '{}').channel
      await route.fulfill({
        json: {
          channel: 'stable',
          version: '99.0.0',
          size: 1024,
          published_at: new Date().toISOString(),
          update_available: true,
          checked_at: new Date().toISOString(),
          release_url: 'https://github.com/Mo-SeTian/GateHome/releases/tag/v99.0.0'
        }
      })
    })
    await button('检查更新').click()
    await page.getByText('检测到更高的正式稳定版 v99.0.0').waitFor()
    assert.equal(checkedChannel, 'beta')
    assert.equal(await updateChannelSelect.inputValue(), 'stable')
    await page.unroute('**/api/maintenance/check-online-update')
  }
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
  assert.equal(await page.locator('.brand-version').textContent(), 'v' + (await api('status')).version)
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

  await route('assistant/settings')
  await page.getByLabel('接入点名称').fill('测试模型')
  await page.getByLabel('接口地址（Base URL）').fill(`http://127.0.0.1:${upstreamPort}/assistant-fixture/v1`)
  await page.getByLabel('API Key').fill('TEST_ONLY_MODEL_KEY')
  await button('获取模型').click()
  await page.getByLabel('选择获取到的模型').selectOption('browser-fixture')
  assert.equal(await page.getByLabel('模型 ID').inputValue(), 'browser-fixture')
  await button('测试连接').click()
  await page.getByText('连接成功，模型已回复。', { exact: true }).waitFor()
  assert.ok(assistantCredentialAccepted)
  await button('保存接入点').click()
  await page.getByText('接入点已保存。', { exact: true }).waitFor()
  const endpointState = await api('assistant/settings')
  assert.equal(endpointState.key_configured, true)
  assert.ok(!JSON.stringify(endpointState).includes('TEST_ONLY_MODEL_KEY'))
  await route('routes')
  let snapshot = await api('config')
  const assistantRoute = snapshot.config.routes[0]
  assistantTarget = assistantRoute.id
  const nextPort = Number(new URL(assistantRoute.upstream).port) === 2284 ? 2285 : 2284
  await button('网关助手').click()
  const assistant = page.getByRole('region', { name: '网关助手对话' })
  await assistant.getByRole('button', { name: '查看反代状态', exact: true }).click()
  await assistant.getByText('已读取反代运行状态', { exact: true }).waitFor()
  await assistant.getByRole('button', { name: '打开健康检查设置', exact: true }).click()
  await assistant.getByRole('link', { name: '服务健康检查', exact: true }).click()
  await dialog().getByRole('heading', { name: '服务健康检查' }).waitFor()
  await page.keyboard.press('Escape')
  const composer = assistant.getByRole('textbox', { name: '给网关助手发送消息' })
  await composer.fill('修改上游端口 ' + nextPort)
  await composer.press('Enter')
  await assistant.getByText('待确认', { exact: true }).waitFor()
  if (process.env.GATEHOME_BROWSER_ARTIFACTS)
    await page.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, 'gatehome-assistant-drawer.png'), animations: 'disabled' })
  assert.equal((await api('config')).config.routes.find(route => route.id === assistantTarget).upstream, assistantRoute.upstream)
  await composer.fill('保留未发送草稿')
  await assistant.getByRole('link', { name: '打开独立页面' }).click()
  await page.waitForURL('**/#/assistant')
  await assistant.getByRole('heading', { name: '网关助手', exact: true }).waitFor()
  await assistant.getByText('已读取反代运行状态', { exact: true }).waitFor()
  assert.equal(await composer.inputValue(), '保留未发送草稿')
  await assistant.getByRole('button', { name: '确认应用' }).click()
  await assistant.getByText('已应用', { exact: true }).waitFor()
  snapshot = await api('config')
  assert.equal(new URL(snapshot.config.routes.find(route => route.id === assistantTarget).upstream).port, String(nextPort))
  await composer.fill('修改上游端口 ' + (nextPort + 1))
  await composer.press('Enter')
  await assistant.getByRole('button', { name: '取消', exact: true }).click()
  await assistant.getByText('已取消', { exact: true }).waitFor()
  assert.equal((await api('config')).revision, snapshot.revision)
  if (process.env.GATEHOME_BROWSER_ARTIFACTS)
    await page.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, 'gatehome-assistant-page.png'), animations: 'disabled' })
  await page.getByRole('link', { name: '反向代理', exact: true }).click()
  await button('网关助手').click()
  const drawer = page.getByRole('complementary', { name: '网关助手侧边栏' })
  await drawer.getByText('已应用', { exact: true }).waitFor()
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 844 })
    await drawer.getByRole('button', { name: '收起网关助手' }).waitFor()
    await page.waitForFunction(() => document.documentElement.scrollWidth <= innerWidth && document.querySelector('.page-viewport').inert)
    assert.equal(await page.locator('.page-viewport').evaluate(el => el.contains(document.activeElement)), false)
    assert.ok(await drawer.locator('.assistant-heading').evaluate(el => {
      const heading = el.querySelector('h2')
      const lastControl = el.querySelector('.assistant-actions').lastElementChild
      return heading.offsetHeight <= Math.ceil(parseFloat(getComputedStyle(heading).lineHeight)) &&
        lastControl.getBoundingClientRect().right <= el.getBoundingClientRect().right + 1
    }), 'assistant title or controls did not fit on one line')
    const model = await drawer.getByLabel('模型', { exact: true }).boundingBox()
    const effort = await drawer.getByLabel('思考强度').boundingBox()
    assert.ok(model.width >= 260 && model.y + model.height < effort.y, 'model selector did not get a full row')
  }
  await page.setViewportSize({ width: 1440, height: 1000 })
  await route('assistant/settings')
  for (const [protocol, name, base] of [
    ['responses', 'Responses 测试', '/assistant-fixture/v1'],
    ['anthropic', 'Claude 测试', '/anthropic-fixture/v1']
  ]) {
    await button('添加供应商').click()
    await page.getByLabel('接入点名称').fill(name)
    await page.getByLabel('请求方式').selectOption(protocol)
    await page.getByLabel('接口地址（Base URL）').fill('http://127.0.0.1:' + upstreamPort + base)
    await page.getByLabel('模型 ID').fill(protocol + '-fixture')
    await page.getByLabel('默认思考强度').selectOption('medium')
    await button('测试连接').click()
    await page.getByText('连接成功，模型已回复。', { exact: true }).waitFor()
    await button('保存接入点').click()
    await page.getByText('接入点已保存。', { exact: true }).waitFor()
  }
  assert.equal(claudeRequest.version, '2023-06-01')
  assert.equal(claudeRequest.payload.thinking.type, 'adaptive')
  assert.equal(claudeRequest.payload.output_config.effort, 'medium')
  assert.equal(responseRequest.reasoning.effort, 'medium')
  await route('assistant')
  await assistant.getByRole('button', { name: '历史对话', exact: true }).click()
  await assistant.getByRole('region', { name: '历史对话', exact: true }).getByRole('button', { name: /^查看反代状态/ }).click()
  await assistant.getByText('已应用', { exact: true }).waitFor()
  const configured = await api('assistant/settings')
  const alternate = configured.providers.find(row => row.protocol === 'responses')
  await assistant.getByLabel('模型', { exact: true }).selectOption(JSON.stringify([alternate.id, alternate.model]))
  assert.equal(await assistant.getByText('已应用', { exact: true }).count(), 0)
  await assistant.getByLabel('思考强度').selectOption('high')
  await composer.fill('检查 Markdown')
  await composer.press('Enter')
  await assistant.getByRole('heading', { name: '模型已连接' }).waitFor()
  await assistant.getByRole('button', { name: '发送消息', exact: true }).waitFor()
  assert.equal(await assistant.locator('.assistant-markdown strong').textContent(), '加粗')
  assert.equal(responseRequest.reasoning.effort, 'high')
  assert.equal(responseRequest.input.length, 2, 'history from another provider reached Responses')
  await page.setViewportSize({ width: 390, height: 844 })
  await page.waitForFunction(() => {
    const model = document.querySelector('#assistant-model-select').getBoundingClientRect()
    const effort = document.querySelector('#assistant-effort-select').getBoundingClientRect()
    return model.width > effort.width && model.width >= 125 && model.height >= 44 && effort.height >= 44
  }, null, { timeout: 3000 })
  if (process.env.GATEHOME_BROWSER_ARTIFACTS)
    await page.screenshot({ path: path.join(process.env.GATEHOME_BROWSER_ARTIFACTS, 'assistant-markdown-390.png'), animations: 'disabled' })
  console.log('PASS assistant models, protocols, Markdown, saved conversations, provider isolation and effort')
  console.log('PASS built-in assistant: endpoint test, shared conversation, settings link, confirmations, real save and mobile drawer')

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
  upstreamUDP?.close()
  await rm(root, { recursive: true, force: true })
}
