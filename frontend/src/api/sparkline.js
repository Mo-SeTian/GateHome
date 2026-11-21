export const MAX_SPARKLINE_SAMPLES = 30

export function pushSparklineSample(history, value, time = '', maxSamples = MAX_SPARKLINE_SAMPLES) {
  const current = Array.isArray(history) ? history : []
  if (typeof value !== 'number' || !Number.isFinite(value)) return current
  const clamped = Math.max(0, Math.min(100, Number(value.toFixed(2))))
  const stamp = time ? String(time) : ''
  if (stamp && current.length && current[current.length - 1].time === stamp) {
    const next = current.slice()
    next[next.length - 1] = { value: clamped, time: stamp }
    return next
  }
  const next = [...current, { value: clamped, time: stamp }]
  return next.length > maxSamples ? next.slice(next.length - maxSamples) : next
}

export function buildSparklineGeometry(
  samples,
  { width = 180, height = 36, padX = 2, padY = 4 } = {}
) {
  const list = (Array.isArray(samples) ? samples : []).filter(
    (item) => typeof item?.value === 'number' && Number.isFinite(item.value)
  )
  if (!list.length) {
    return {
      count: 0,
      points: [],
      linePath: '',
      areaPath: '',
      lastPoint: null,
      min: null,
      max: null,
      avg: null,
      delta: null,
      scaleMax: 100
    }
  }

  let min = list[0].value
  let max = list[0].value
  let sum = 0
  for (const item of list) {
    if (item.value < min) min = item.value
    if (item.value > max) max = item.value
    sum += item.value
  }
  const avg = sum / list.length
  const delta = list.length >= 2 ? list[list.length - 1].value - list[list.length - 2].value : null

  // Use a smart ceiling so low-usage fluctuations remain legible while still conveying scale.
  const scaleMax = Math.min(100, Math.max(20, Math.ceil((max * 1.25) / 10) * 10))
  const innerW = Math.max(1, width - padX * 2)
  const innerH = Math.max(1, height - padY * 2)
  const baselineY = height - padY

  const toY = (val) => Number((baselineY - (Math.max(0, Math.min(scaleMax, val)) / scaleMax) * innerH).toFixed(2))

  let points
  if (list.length === 1) {
    const y = toY(list[0].value)
    points = [
      { x: padX, y, value: list[0].value, time: list[0].time },
      { x: width - padX, y, value: list[0].value, time: list[0].time }
    ]
  } else {
    const step = innerW / (list.length - 1)
    points = list.map((item, idx) => ({
      x: Number((padX + idx * step).toFixed(2)),
      y: toY(item.value),
      value: item.value,
      time: item.time
    }))
  }

  const linePath = points.map((pt, idx) => `${idx === 0 ? 'M' : 'L'} ${pt.x} ${pt.y}`).join(' ')
  const first = points[0]
  const last = points[points.length - 1]
  const areaPath = `${linePath} L ${last.x} ${baselineY} L ${first.x} ${baselineY} Z`

  return {
    count: list.length,
    points,
    linePath,
    areaPath,
    lastPoint: last,
    min,
    max,
    avg,
    delta,
    scaleMax
  }
}
