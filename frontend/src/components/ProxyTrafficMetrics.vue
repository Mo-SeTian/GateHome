<template>
  <dl class="proxy-metrics">
    <div>
      <dt>上传速率</dt>
      <dd data-metric="upload">{{ formatByteRate(sampled ? sample?.upload_bytes_per_second : null) }}</dd>
    </div>
    <div>
      <dt>下载速率</dt>
      <dd data-metric="download">{{ formatByteRate(sampled ? sample?.download_bytes_per_second : null) }}</dd>
    </div>
    <div>
      <dt>活跃连接</dt>
      <dd data-metric="active">{{ sample?.active?.toLocaleString() ?? '—' }}</dd>
    </div>
  </dl>
</template>

<script setup>
import { formatByteRate } from '@/api/reports'

defineProps({ sample: Object, sampled: Boolean })
</script>

<style scoped>
.proxy-metrics {
  display: grid;
  min-width: 0;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
  margin: 0;
  text-align: center;
}
.proxy-metrics dt { color: var(--muted); font-size: 12px; }
.proxy-metrics dd {
  margin: 2px 0 0;
  color: var(--ink);
  font-size: var(--proxy-metric-size, 14px);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  overflow-wrap: anywhere;
}
</style>
