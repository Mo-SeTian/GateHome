import assert from 'node:assert/strict'
import test from 'node:test'
import { routeImageUrl } from './icons.js'

test('route icons retain legacy hashes and escape each library path segment', () => {
  const hash = 'af'.repeat(32)
  assert.equal(routeImageUrl(hash), '/api/route-images/' + hash)
  assert.equal(
    routeImageUrl('/icons/uploads/202611/测试 应用.svg'),
    '/api/icons/file/uploads/202611/%E6%B5%8B%E8%AF%95%20%E5%BA%94%E7%94%A8.svg'
  )
})
