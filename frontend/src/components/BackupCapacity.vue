<template>
  <SpotlightCard class="report-panel capacity-card">
    <div class="report-heading">
      <h2>备份与更新容量预检</h2>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="$emit('refresh')">
        {{ loading ? '读取中…' : '刷新容量' }}
      </button>
    </div>
    <p v-if="error" class="report-error" role="alert">{{ error }}</p>
    <template v-if="capacity"
      ><p :class="capacity.can_proceed ? 'report-note' : 'report-warning'">
        预计快照 {{ formatBytes(capacity.snapshot_bytes) }} / 上限 {{ formatBytes(capacity.limit_bytes) }} ·
        {{ capacity.can_proceed ? '容量检查通过' : '容量超限，请清理后重试' }}
      </p>
      <progress :value="capacity.snapshot_bytes" :max="capacity.limit_bytes" aria-label="预计快照容量" />
      <div class="capacity-parts">
        <span>配置 {{ formatBytes(capacity.config_bytes) }}</span
        ><span>日志 {{ formatBytes(capacity.log_bytes) }}（磁盘保留）</span
        ><span>首页 {{ formatBytes(capacity.sunpanel_bytes) }}</span
        ><span>图片 {{ formatBytes(capacity.image_bytes) }}</span
        ><span>其他 {{ formatBytes(capacity.other_bytes) }}</span>
      </div>
      <p class="report-note">
        检查时间：{{ reportDate(capacity.checked_at) }}。日志原地保存在磁盘中不占快照配额，快照仅包含配置、证书与应用数据。
      </p></template
    >
    <p v-else-if="!error" class="report-note">正在检查持久化数据容量…</p>
  </SpotlightCard>
</template>
<script setup>
import SpotlightCard from '@/components/SpotlightCard.vue'
import { formatBytes, reportDate } from '@/api/reports'
defineProps({ capacity: Object, error: String, loading: Boolean })
defineEmits(['refresh'])
</script>
<style scoped>
.capacity-card :deep(.card-inner) {
  display: grid;
  gap: 14px;
}
h2 {
  margin-bottom: 0;
}
progress {
  width: 100%;
  height: 9px;
  accent-color: var(--brand);
}
.capacity-parts {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  font-size: 12px;
  color: var(--ink-secondary);
}
</style>
