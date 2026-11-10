// Schedule after completion so slow requests never overlap. Hidden pages resume
// with a fresh read when visible, and stopping never schedules another tick.
export function startPolling(task, interval) {
  const visibility = globalThis.document
  let stopped = false
  let running = false
  let timer
  async function tick() {
    clearTimeout(timer)
    if (stopped || running || visibility?.visibilityState === 'hidden') return
    running = true
    try { await task() } catch { /* The caller owns visible error feedback. */ }
    finally {
      running = false
      if (!stopped && visibility?.visibilityState !== 'hidden') timer = setTimeout(tick, interval)
    }
  }
  function changed() {
    clearTimeout(timer)
    if (visibility.visibilityState !== 'hidden') tick()
  }
  visibility?.addEventListener('visibilitychange', changed)
  timer = setTimeout(tick, interval)
  return () => {
    stopped = true
    clearTimeout(timer)
    visibility?.removeEventListener('visibilitychange', changed)
  }
}
