<template>
  <div class="history-box">
    <div class="history-header">
      <strong>在线历史</strong>
      <div class="report-actions" role="group" aria-label="历史时段">
        <button v-for="range in [15, 30, 60]" :key="range" type="button" class="btn btn-sm"
          :class="range === minutes ? 'btn-secondary' : 'btn-ghost'" :aria-pressed="range === minutes"
          @click="changeRange(range)">{{ range === 60 ? '1 小时' : range + ' 分钟' }}</button>
      </div>
    </div>
    <p v-if="error" class="report-error" role="alert">{{ error }}</p>
    <p v-if="!history && !error" class="report-note" role="status">正在读取在线历史…</p>
    <template v-if="history">
      <div class="history-scroll">
        <div class="history-track" :style="{ '--samples': minutes }">
          <div ref="dots" class="history-dots" role="group" aria-label="逐分钟检测结果">
            <button v-for="(point, index) in points" :key="index" type="button"
              :class="point.state" :aria-label="point.label" :title="point.label"
              :tabindex="selected === index ? 0 : -1"
              @pointerenter="selected = index" @focus="selected = index" @click="selected = index"
              @keydown.left.prevent="move(index - 1)" @keydown.right.prevent="move(index + 1)"
              @keydown.home.prevent="move(0)" @keydown.end.prevent="move(points.length - 1)" />
          </div>
          <div class="history-times"><time>{{ clock(points[0].time) }}</time><time>{{ clock(points.at(-1).time) }}</time></div>
        </div>
      </div>
      <div class="history-footer">
        <div class="history-legend"><span class="up">可达</span><span class="down">失败</span><span class="gap">无采样</span></div>
        <span class="history-sample">{{ points[selected]?.label }}</span>
      </div>
    </template>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { adminApi } from '@/api/admin'

const props = defineProps({ routeId: { type: String, required: true } })
const minutes = ref(60), selected = ref(59), history = ref(null), error = ref(''), dots = ref(null)
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
    return { time, state, label: clock(sample?.checked_at || time) + '　' + result }
  })
})

function changeRange(range) {
  minutes.value = range
  selected.value = range - 1
}

function move(index) {
  dots.value?.children[Math.max(0, Math.min(index, points.value.length - 1))]?.focus()
}

let loading = false, timer
async function load() {
  if (loading || document.hidden) return
  loading = true
  try {
    history.value = await adminApi.routeHistory(props.routeId, controller.signal)
    error.value = ''
  } catch (err) {
    if (err.name !== 'AbortError') error.value = err.message
  } finally { loading = false }
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
.history-box { padding: 12px 16px; background: var(--strip-bg); border-inline-start: 2px solid var(--brand); }
.history-header, .history-times, .history-footer, .history-legend { display: flex; align-items: center; gap: 8px; }
.history-header, .history-footer { justify-content: space-between; flex-wrap: wrap; }
.history-times { justify-content: space-between; }
.history-header .report-actions { gap: 4px; }
.history-scroll { overflow-x: auto; }
.history-track { min-width: calc(var(--samples) * 14px); }
.history-dots { display: grid; grid-template-columns: repeat(var(--samples), minmax(0, 1fr)); margin-top: 8px; }
.history-dots button { display: grid; height: 24px; place-items: center; border-radius: var(--radius-sm); }
.history-legend span { display: inline-flex; align-items: center; gap: 5px; }
.history-dots button::before, .history-legend span::before { content: ''; width: 8px; height: 8px; border-radius: 50%; background: var(--sample-color); }
.up { --sample-color: var(--ok); }
.down { --sample-color: var(--bad); }
.gap { --sample-color: var(--muted); }
.history-dots button[tabindex="0"]::before { outline: 1px solid var(--sample-color); outline-offset: 2px; }
.history-times, .history-footer { color: var(--muted); font-size: 12px; }
.history-footer { margin-top: 8px; }
</style>
