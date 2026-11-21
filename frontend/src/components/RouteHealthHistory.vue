<template>
  <div class="history-box">
    <div class="history-top">
      <div class="history-summary">
        <strong>逐分钟在线历史</strong>
        <span v-if="history" class="badge badge-neutral mono">可用率 {{ uptimeLabel }}</span>
        <span v-if="history" class="badge badge-neutral mono">平均 {{ avgLatencyLabel }}</span>
      </div>
      <div class="history-range-seg" role="group" aria-label="历史时段">
        <button
          v-for="range in [15, 30, 60]"
          :key="range"
          type="button"
          :class="{ active: range === minutes }"
          :aria-pressed="range === minutes"
          @click="changeRange(range)"
        >
          {{ range === 60 ? '1 小时' : range + ' 分钟' }}
        </button>
      </div>
    </div>

    <p v-if="error" class="report-error" role="alert">{{ error }}</p>
    <p v-if="!history && !error" class="report-note" role="status">正在读取在线历史…</p>

    <template v-if="history">
      <div ref="dots" class="uptime-bars" :style="{ '--samples': minutes }" role="group" aria-label="逐分钟检测结果">
        <button
          v-for="(point, index) in points"
          :key="index"
          type="button"
          class="uptime-bar"
          :class="[point.state, { selected: selected === index }]"
          :aria-label="point.label"
          :title="point.label"
          :tabindex="selected === index ? 0 : -1"
          @pointerenter="selected = index"
          @focus="selected = index"
          @click="selected = index"
          @keydown.left.prevent="move(index - 1)"
          @keydown.right.prevent="move(index + 1)"
          @keydown.home.prevent="move(0)"
          @keydown.end.prevent="move(points.length - 1)"
        />
      </div>

      <div class="history-foot">
        <time class="history-edge">{{ clock(points[0].time) }}</time>
        <span class="history-sample-readout">{{ points[selected]?.label }}</span>
        <time class="history-edge">{{ clock(points.at(-1).time) }}</time>
      </div>
      <div class="history-legend">
        <span class="up">可达</span>
        <span class="down">失败</span>
        <span class="gap">无采样</span>
      </div>
    </template>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { adminApi } from '@/api/admin'

const props = defineProps({ routeId: { type: String, required: true } })
const minutes = ref(60)
const selected = ref(59)
const history = ref(null)
const error = ref('')
const dots = ref(null)
const controller = new AbortController()
const minute = 60000

const clock = time => new Date(time).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })

const points = computed(() => {
  if (!history.value) return []
  const end = Math.floor(Date.parse(history.value.now) / minute) * minute
  const byMinute = new Map((history.value.samples || []).map(sample => [
    Math.floor(Date.parse(sample.checked_at) / minute) * minute, sample
  ]))
  return Array.from({ length: minutes.value }, (_, index) => {
    const time = end - (minutes.value - index - 1) * minute
    const sample = byMinute.get(time)
    const state = !sample ? 'gap' : sample.healthy ? 'up' : 'down'
    const result = sample ? sample.label + (sample.healthy ? ' · ' + sample.duration_ms + ' ms' : '') : '无采样'
    return { time, state, sample, label: clock(sample?.checked_at || time) + '　' + result }
  })
})

const uptimeLabel = computed(() => {
  const sampled = points.value.filter(point => point.state !== 'gap')
  if (!sampled.length) return '—'
  const healthy = sampled.filter(point => point.state === 'up').length
  return `${((healthy / sampled.length) * 100).toFixed(1)}%`
})

const avgLatencyLabel = computed(() => {
  const durations = points.value
    .filter(point => point.sample?.healthy && typeof point.sample.duration_ms === 'number')
    .map(point => point.sample.duration_ms)
  if (!durations.length) return '—'
  return `${Math.round(durations.reduce((sum, val) => sum + val, 0) / durations.length)} ms`
})

function changeRange(range) {
  minutes.value = range
  selected.value = range - 1
}

function move(index) {
  dots.value?.children[Math.max(0, Math.min(index, points.value.length - 1))]?.focus()
}

let loading = false
let timer
async function load() {
  if (loading || document.hidden) return
  loading = true
  try {
    history.value = await adminApi.routeHistory(props.routeId, controller.signal)
    error.value = ''
  } catch (err) {
    if (err.name !== 'AbortError') error.value = err.message
  } finally {
    loading = false
  }
}

onMounted(() => {
  load()
  timer = setInterval(load, 10000)
  document.addEventListener('visibilitychange', load)
})
onBeforeUnmount(() => {
  controller.abort()
  clearInterval(timer)
  document.removeEventListener('visibilitychange', load)
})
</script>

<style scoped>
.history-box {
  padding: 14px;
  border-top: 1px solid var(--line);
  background: var(--card-bg);
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 10px;
  min-width: 0;
}
.history-top { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px; }
.history-summary { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; font-size: 12px; }
.history-summary strong { color: var(--ink); font-size: 13px; }
.mono { font-family: var(--mono); font-variant-numeric: tabular-nums; }

.history-range-seg {
  display: inline-flex; padding: 2px; border-radius: 7px;
  border: 1px solid var(--line); background: var(--strip-bg); gap: 2px;
}
.history-range-seg button {
  border: 0; background: transparent; color: var(--muted); font-size: 11.5px;
  padding: 4px 10px; border-radius: 5px; cursor: pointer; white-space: nowrap;
}
.history-range-seg button.active { background: var(--card-hover-bg); color: var(--ink); font-weight: 600; }

.uptime-bars {
  display: grid;
  grid-template-columns: repeat(var(--samples), minmax(0, 1fr));
  gap: 3px;
  width: 100%;
  min-width: 0;
  align-items: end;
}
.uptime-bar {
  width: 100%; min-width: 0; height: 22px; padding: 0; border: 0; border-radius: 2.5px;
  background: var(--sample-color);
  cursor: pointer;
  transition: transform 0.12s ease, filter 0.12s ease;
}
.uptime-bar.up, .history-legend .up { --sample-color: var(--ok); }
.uptime-bar.down, .history-legend .down { --sample-color: var(--bad); }
.uptime-bar.gap, .history-legend .gap { --sample-color: rgba(148, 163, 184, 0.35); }
.uptime-bar.selected {
  transform: scaleY(1.18);
  filter: brightness(1.15);
  box-shadow: 0 0 0 1.5px var(--ink);
}

.history-foot {
  display: flex; align-items: center; justify-content: space-between; gap: 8px;
  font-size: 11.5px; color: var(--muted); font-family: var(--mono);
}
.history-edge { flex-shrink: 0; }
.history-sample-readout { color: var(--ink-secondary); text-align: center; overflow-wrap: anywhere; }

.history-legend { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; font-size: 11.5px; color: var(--muted); }
.history-legend span { display: inline-flex; align-items: center; gap: 5px; }
.history-legend span::before { content: ''; width: 8px; height: 8px; border-radius: 2px; background: var(--sample-color); }

@media (prefers-reduced-motion: reduce) {
  .uptime-bar { transition: none; }
}

@media (max-width: 650px) {
  .history-box { padding: 12px; }
  .history-top { flex-direction: column; align-items: stretch; }
  .history-range-seg { display: grid; grid-template-columns: repeat(3, 1fr); width: 100%; }
  .history-range-seg button { text-align: center; min-height: 32px; }
  .uptime-bars { gap: 1.5px; }
  .history-foot { flex-wrap: wrap; }
  .history-sample-readout {
    order: 3; flex-basis: 100%; text-align: left;
    padding: 6px 8px; background: var(--strip-bg);
    border: 1px solid var(--line); border-radius: 6px;
  }
}
</style>
