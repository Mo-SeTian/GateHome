import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { readFile } from 'node:fs/promises'
import { chromium } from 'playwright'
import ts from '../../sunpanel/node_modules/typescript/lib/typescript.js'

const source = await readFile(new URL('../../sunpanel/src/utils/network.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ES2022 } })
const moduleURL = `data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`
const origin = createServer((req, res) => { res.setHeader('Content-Type', 'text/html'); res.end('<title>LAN probe fixture</title>') })
let requests = 0
const target = createServer((req, res) => {
  requests++
  assert.equal(req.headers.cookie, undefined, 'LAN probes must omit credentials')
  assert.equal(req.method, 'HEAD')
  if (req.url === '/redirect') res.writeHead(307, { Location: '/desktop/' })
  else res.writeHead(req.url === '/auth' ? 401 : 200)
  res.end()
})
const listen = server => new Promise(resolve => server.listen(0, '127.0.0.1', () => resolve(server.address().port)))
let browser
try {
  const [originPort, targetPort] = await Promise.all([listen(origin), listen(target)])
  browser = await chromium.launch({ ...(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : { channel: 'chromium' }) })
  const context = await browser.newContext()
  await context.addCookies([{ name: 'TEST_ONLY_LAN_COOKIE', value: 'fake', url: `http://127.0.0.1:${targetPort}` }])
  const page = await context.newPage()
  await page.goto(`http://127.0.0.1:${originPort}`)
  for (const pathname of ['/', '/auth', '/redirect']) {
    const lanUrl = `http://127.0.0.1:${targetPort}${pathname}`
    const chosen = await page.evaluate(async ({ moduleURL, lanUrl }) => {
      const { resolveAutoUrl } = await import(moduleURL)
      return resolveAutoUrl('https://default.example.test/', lanUrl, async () => true)
    }, { moduleURL, lanUrl })
    assert.equal(chosen, lanUrl, 'a reachable cross-origin LAN service must be selected')
  }
  const before = requests
  const blocked = await page.evaluate(async ({ moduleURL, targetPort }) => {
    const { resolveAutoUrl } = await import(moduleURL)
    return resolveAutoUrl('https://default.example.test/', `http://127.0.0.1:${targetPort}/redirect`, async () => false)
  }, { moduleURL, targetPort })
  assert.equal(blocked, 'https://default.example.test/')
  assert.equal(requests, before, 'an unverified redirect chain must never be probed by the browser')
  console.log('PASS LAN browser probes: cross-origin HTTP, login responses and 307 redirects select the LAN address')
  console.log('PASS LAN browser boundary: unverified redirect chains are not requested and cookies remain private')
} finally {
  await browser?.close()
  await Promise.all([new Promise(resolve => origin.close(resolve)), new Promise(resolve => target.close(resolve))])
}
