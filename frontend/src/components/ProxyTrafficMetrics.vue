<template>
  <dl class="proxy-metrics" aria-label="反代实时汇总">
    <div v-for="{ key, label, icon, value, points } in metrics" :key="key" class="metric" :class="key">
      <span class="metric-icon"><component :is="icon" :size="19" aria-hidden="true" /></span>
      <div class="metric-reading">
        <dt>{{ label }}</dt>
        <dd :data-metric="key">
          {{ value }}
          <span v-if="key === 'active' && activeDelta" class="metric-delta" :class="{ lower: activeDelta < 0 }"
            :title="'较上次采样'">{{ activeDelta > 0 ? '+' : '−' }}{{ Math.abs(activeDelta) }}</span>
        </dd>
      </div>
      <ResourceSparkline v-if="points.length > 1" class="metric-trend" :samples="points" :label="label"
        :tone="key === 'download' ? 'cyan' : 'ok'" compact />
    </div>
  </dl>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { Download, Upload, Users } from 'lucide-vue-next'
import ResourceSparkline from '@/components/ResourceSparkline.vue'
import { formatByteRate } from '@/api/reports'

const props = defineProps({ sample: Object, sampled: Boolean })
const history = ref([])
watch(() => props.sample, sample => {
  if (!sample || !props.sampled) {
    history.value = []
    return
  }
  history.value = [...history.value.slice(-19), sample]
}, { immediate: true })

const activeDelta = computed(() => {
  const previous = history.value.at(-2)?.active
  return previous == null ? 0 : props.sample?.active - previous
})

function trend(field) {
  const values = history.value.map(sample => sample[field]).filter(Number.isFinite)
  const max = Math.max(1, ...values)
  return values.map(value => ({ value: 100 * value / max }))
}
const metrics = computed(() => [
  { key: 'upload', label: '上传速率', icon: Upload, value: formatByteRate(props.sampled ? props.sample?.upload_bytes_per_second : null), points: trend('upload_bytes_per_second') },
  { key: 'download', label: '下载速率', icon: Download, value: formatByteRate(props.sampled ? props.sample?.download_bytes_per_second : null), points: trend('download_bytes_per_second') },
  { key: 'active', label: '活跃连接', icon: Users, value: props.sample?.active?.toLocaleString() ?? '—', points: trend('active') }
])
</script>

<style scoped>
.proxy-metrics { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); margin: 0; }
.metric { display: flex; align-items: center; gap: 12px; padding: 0 14px; border-left: 1px solid var(--line); min-width: 0; }
.metric:first-child { border-left: 0; padding-left: 0; }
.metric:last-child { padding-right: 0; }
.metric-icon { display: grid; place-items: center; flex: 0 0 40px; height: 40px; border-radius: 50%; background: color-mix(in srgb, var(--muted) 12%, var(--card-bg)); color: var(--ink-secondary); }
.metric-reading { min-width: 0; }
.metric dt { color: var(--muted); font-size: 12px; }
.metric dd { margin: 2px 0 0; font-size: 19px; font-weight: 650; font-variant-numeric: tabular-nums; }
.metric-delta { color: var(--ok); font-size: 11px; white-space: nowrap; }
.metric-delta.lower { color: var(--warn); }
.metric-trend { margin: 0 0 0 auto; width: clamp(28px, 5vw, 72px); flex: 0 1 auto; }
.metric-trend :deep(.sparkline-svg) { height: 28px; }
@container routes (max-width: 650px) {
  .metric { gap: 6px; padding-inline: 8px; }
  .metric-icon { flex-basis: 29px; height: 29px; }
  .metric-icon svg { width: 15px; }
  .metric dd { font-size: 15px; }
  .metric-trend { display: none; }
}
@container routes (max-width: 385px) {
  .metric { align-items: flex-start; flex-direction: column; }
  .metric dt { font-size: 11px; }
  .metric dd { overflow-wrap: anywhere; }
}
</style>
