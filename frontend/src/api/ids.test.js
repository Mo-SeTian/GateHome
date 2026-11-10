import assert from 'node:assert/strict'
import { test } from 'node:test'
import { newID } from './ids.js'

test('configuration IDs remain unique UUIDv4 values when randomUUID is unavailable', () => {
  const previous = Object.getOwnPropertyDescriptor(globalThis, 'crypto')
  const source = globalThis.crypto
  Object.defineProperty(globalThis, 'crypto', { configurable: true, value: { getRandomValues: source.getRandomValues.bind(source) } })
  try {
    const ids = new Set(Array.from({ length: 100 }, newID))
    assert.equal(ids.size, 100)
    for (const id of ids) assert.match(id, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
  } finally {
    Object.defineProperty(globalThis, 'crypto', previous)
  }
})
