import test from 'node:test'
import assert from 'node:assert/strict'
import { startPolling } from './polling.js'

test('slow polling requests never overlap or restart after stopping', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  let calls = 0, release
  const stop = startPolling(() => {
    calls++
    return new Promise(resolve => { release = resolve })
  }, 1000)
  t.after(stop)
  t.mock.timers.tick(1000)
  assert.equal(calls, 1)
  t.mock.timers.tick(10000)
  assert.equal(calls, 1)
  release()
  await Promise.resolve()
  t.mock.timers.tick(1000)
  assert.equal(calls, 2)
  stop()
  release()
  await Promise.resolve()
  t.mock.timers.tick(10000)
  assert.equal(calls, 2)
})

test('polling pauses while hidden, resumes on visibility, and retries errors', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  const previous = globalThis.document
  const visibility = new EventTarget()
  visibility.visibilityState = 'hidden'
  globalThis.document = visibility
  t.after(() => { globalThis.document = previous })
  let calls = 0
  const stop = startPolling(async () => { calls++; throw new Error('TEST_ONLY_FAILURE') }, 1000)
  t.after(stop)
  t.mock.timers.tick(10000)
  assert.equal(calls, 0)
  visibility.visibilityState = 'visible'
  visibility.dispatchEvent(new Event('visibilitychange'))
  assert.equal(calls, 1)
  await Promise.resolve()
  await Promise.resolve()
  t.mock.timers.tick(1000)
  assert.equal(calls, 2)
  await Promise.resolve()
  visibility.visibilityState = 'hidden'
  visibility.dispatchEvent(new Event('visibilitychange'))
  t.mock.timers.tick(10000)
  assert.equal(calls, 2)
  stop()
  visibility.visibilityState = 'visible'
  visibility.dispatchEvent(new Event('visibilitychange'))
  assert.equal(calls, 2)
})
