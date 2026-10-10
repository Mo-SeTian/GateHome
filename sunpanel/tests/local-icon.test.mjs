import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { pathToFileURL } from 'node:url'
import ts from 'typescript'

const require = createRequire(import.meta.url)
const iconify = require('@iconify/vue')
const source = readFileSync(new URL('../src/utils/localIcon.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ES2022 } })
const moduleText = outputText.replace('@iconify/vue', pathToFileURL(require.resolve('@iconify/vue')).href)
const { createLocalIconPng } = await import(`data:text/javascript;base64,${Buffer.from(moduleText).toString('base64')}`)
iconify.addIcon('test:local-icon', { width: 24, height: 24, body: '<path fill="currentColor" d="M0 0h24v24H0z"/><path fill="#ff0000" d="M0 0h12v12H0z"/>' })

function browserFixture(t, { imageError = false, encodeError = false, width = 128, height = 128 } = {}) {
  const state = { revoked: [], drawn: [], svg: null }
  const canvas = {
    getContext: () => ({ drawImage: (...args) => state.drawn.push(args.slice(1)) }),
    toBlob: (done, type) => done(encodeError ? null : new Blob(['TEST_ONLY_PNG'], { type })),
  }
  const globals = {
    document: {
      createElementNS: () => ({ attributes: {}, setAttribute(key, value) { this.attributes[key] = value } }),
      createElement: () => canvas,
    },
    XMLSerializer: class {
      serializeToString(svg) { return JSON.stringify(svg) }
    },
    Image: class {
      width = width
      height = height
      set src(_value) { queueMicrotask(() => imageError ? this.onerror() : this.onload()) }
    },
  }
  for (const [key, value] of Object.entries(globals)) {
    const previous = Object.getOwnPropertyDescriptor(globalThis, key)
    Object.defineProperty(globalThis, key, { value, configurable: true })
    t.after(() => previous ? Object.defineProperty(globalThis, key, previous) : delete globalThis[key])
  }
  t.mock.method(URL, 'createObjectURL', (blob) => {
    state.svg = blob
    return 'blob:TEST_ONLY_ICON'
  })
  t.mock.method(URL, 'revokeObjectURL', url => state.revoked.push(url))
  return { state, canvas }
}

test('cached online icons become a transparent PNG upload with the current color and multicolor paths', async (t) => {
  const { state, canvas } = browserFixture(t)
  const file = await createLocalIconPng(' test:local-icon ', 'rgb(255, 255, 255)')
  assert.equal(file.name, 'online-icon.png')
  assert.equal(file.type, 'image/png')
  const svg = JSON.parse(await state.svg.text())
  assert.equal(svg.attributes.color, 'rgb(255, 255, 255)')
  assert.match(svg.innerHTML, /fill="currentColor"/)
  assert.match(svg.innerHTML, /fill="#ff0000"/)
  assert.equal(canvas.width, 256)
  assert.equal(canvas.height, 256)
  assert.deepEqual(state.drawn, [[64, 64, 128, 128]])
  assert.deepEqual(state.revoked, ['blob:TEST_ONLY_ICON'])
})

test('wide icons retain their aspect ratio and stay centered', async (t) => {
  const { state } = browserFixture(t, { width: 256, height: 128 })
  await createLocalIconPng('test:local-icon', '#ffffff')
  assert.deepEqual(state.drawn, [[64, 96, 128, 64]])
})

test('image decoding and PNG encoding failures release the temporary image and produce no upload', async (t) => {
  for (const options of [{ imageError: true }, { encodeError: true }]) {
    await t.test(JSON.stringify(options), async (t) => {
      const { state } = browserFixture(t, options)
      await assert.rejects(createLocalIconPng('test:local-icon', '#ffffff'))
      assert.deepEqual(state.revoked, ['blob:TEST_ONLY_ICON'])
    })
  }
})

test('invalid icon names fail before image conversion', async (t) => {
  const { state } = browserFixture(t)
  await assert.rejects(createLocalIconPng('invalid icon name', '#ffffff'))
  assert.equal(state.svg, null)
  assert.deepEqual(state.revoked, [])
})
