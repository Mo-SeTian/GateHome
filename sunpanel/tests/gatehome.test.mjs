import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import ts from 'typescript'

const source = readFileSync(new URL('../src/utils/gateHome.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ES2022 } })
const { routeLink, isImported, routeChanges } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`)
const route = { id: 'TEST_ONLY_ROUTE', title: 'NAS', group: 'Home', url: 'https://nas.example.test/', lanUrl: 'http://192.168.1.2:5000/' }

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
