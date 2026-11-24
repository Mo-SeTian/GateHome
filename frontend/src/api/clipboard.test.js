import assert from 'node:assert/strict'
import { test } from 'node:test'
import { copyText } from './clipboard.js'

function withClipboardGlobals({ navigator: navValue, document: docValue }, fn) {
  const prevNav = Object.getOwnPropertyDescriptor(globalThis, 'navigator')
  const prevDoc = Object.getOwnPropertyDescriptor(globalThis, 'document')
  Object.defineProperty(globalThis, 'navigator', { configurable: true, writable: true, value: navValue })
  Object.defineProperty(globalThis, 'document', { configurable: true, writable: true, value: docValue })
  return Promise.resolve()
    .then(fn)
    .finally(() => {
      if (prevNav) Object.defineProperty(globalThis, 'navigator', prevNav)
      else delete globalThis.navigator
      if (prevDoc) Object.defineProperty(globalThis, 'document', prevDoc)
      else delete globalThis.document
    })
}

test('copyText prefers navigator.clipboard.writeText when available', async () => {
  let written = null
  let touchedDocument = false
  await withClipboardGlobals(
    {
      navigator: { clipboard: { writeText: async (value) => { written = value } } },
      document: {
        get visibilityState() {
          touchedDocument = true
          return 'visible'
        }
      }
    },
    async () => {
      await copyText('https://example.test:8443')
      assert.equal(written, 'https://example.test:8443')
      assert.equal(touchedDocument, false)
    }
  )
})

test('copyText falls back to execCommand on HTTP or clipboard rejection and restores focus', async () => {
  let selected = false
  let removed = false
  let restoredFocusOpts = null
  let copiedValue = null
  const created = {
    value: '',
    readOnly: false,
    style: {},
    select() { selected = true },
    remove() { removed = true }
  }
  const activeButton = { focus(opts) { restoredFocusOpts = opts } }
  await withClipboardGlobals(
    {
      navigator: { clipboard: { writeText: async () => { throw new Error('blocked') } } },
      document: {
        visibilityState: 'visible',
        activeElement: activeButton,
        hasFocus: () => true,
        createElement: (tag) => {
          assert.equal(tag, 'textarea')
          return created
        },
        body: { append() {} },
        execCommand: (cmd) => {
          assert.equal(cmd, 'copy')
          copiedValue = created.value
          return true
        }
      }
    },
    async () => {
      await copyText('http://lan.example:8080')
      assert.equal(created.readOnly, true)
      assert.equal(selected, true)
      assert.equal(copiedValue, 'http://lan.example:8080')
      assert.equal(removed, true)
      assert.deepEqual(restoredFocusOpts, { preventScroll: true })
    }
  )
})

test('copyText rejects fallback when document is hidden or unfocused', async () => {
  await withClipboardGlobals(
    {
      navigator: {},
      document: { visibilityState: 'hidden', hasFocus: () => true }
    },
    async () => {
      await assert.rejects(() => copyText('secret'), /Page inactive/)
    }
  )
  await withClipboardGlobals(
    {
      navigator: {},
      document: { visibilityState: 'visible', hasFocus: () => false }
    },
    async () => {
      await assert.rejects(() => copyText('secret'), /Page inactive/)
    }
  )
})
