import assert from 'node:assert/strict'
import test from 'node:test'
import { buildSparklineGeometry, MAX_SPARKLINE_SAMPLES, pushSparklineSample } from './sparkline.js'

test('pushSparklineSample clamps values, deduplicates same timestamps and retains at most 30 points', () => {
  let history = []
  for (let i = 0; i < 35; i++) {
    history = pushSparklineSample(history, i * 3, `2026-11-21T00:00:${String(i).padStart(2, '0')}Z`)
  }
  assert.equal(history.length, MAX_SPARKLINE_SAMPLES)
  assert.equal(history[0].value, 15)
  assert.equal(history[history.length - 1].value, 100)

  // Duplicate timestamp updates last entry instead of growing the array
  const deduped = pushSparklineSample(history, 42.5, history[history.length - 1].time)
  assert.equal(deduped.length, MAX_SPARKLINE_SAMPLES)
  assert.equal(deduped[deduped.length - 1].value, 42.5)

  // Invalid values leave history unchanged
  assert.equal(pushSparklineSample(deduped, null, 't-null').length, MAX_SPARKLINE_SAMPLES)
})

test('buildSparklineGeometry generates valid SVG paths for single and multi-point histories', () => {
  const empty = buildSparklineGeometry([])
  assert.equal(empty.count, 0)
  assert.equal(empty.linePath, '')

  const single = buildSparklineGeometry([{ value: 12.5, time: 't1' }])
  assert.equal(single.count, 1)
  assert.equal(single.points.length, 2)
  assert.ok(single.linePath.startsWith('M '))
  assert.ok(single.areaPath.endsWith(' Z'))
  assert.equal(single.scaleMax, 20)

  const multi = buildSparklineGeometry([
    { value: 20, time: 't1' },
    { value: 50, time: 't2' },
    { value: 35, time: 't3' }
  ])
  assert.equal(multi.count, 3)
  assert.equal(multi.min, 20)
  assert.equal(multi.max, 50)
  assert.equal(multi.avg, 35)
  assert.equal(multi.delta, -15)
  assert.equal(multi.scaleMax, 70)
  assert.equal(multi.lastPoint.x, 178)
})
