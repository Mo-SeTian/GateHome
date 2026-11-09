<template>
  <section class="maintenance-preview" aria-live="polite">
    <h3>{{ kind === 'update' ? '更新包预检' : '恢复预检' }} · v{{ preview.version }}</h3>
    <p class="report-note">{{ preview.message }}</p>
    <template v-if="kind === 'restore'"
      ><p class="report-note">备份时间：{{ reportDate(preview.created_at) }}</p>
      <dl>
        <div v-for="[key, label] in counts" :key="key">
          <dt>{{ label }}</dt>
          <dd>{{ preview[key] || 0 }}</dd>
        </div>
      </dl>
      <p v-if="!preview.includes_files" class="report-warning">
        此旧备份未包含日志、图片与缓存，恢复时保留服务器现有文件。
      </p>
      <p class="report-note">
        恢复会覆盖当前数据并重启，之后使用备份时的管理员账号和密码登录。历史流量统计需随完整目录另行备份。
      </p></template
    >
    <button
      type="button"
      class="btn btn-primary"
      :disabled="disabled || !preview.can_apply"
      @click="$emit('apply')"
    >
      {{ busy ? '正在安排…' : kind === 'update' ? '应用更新并重启' : '恢复数据并重启' }}
    </button>
  </section>
</template>
<script setup>
import { reportDate } from '@/api/reports'
defineProps({
  preview: { type: Object, required: true },
  kind: { type: String, required: true },
  disabled: Boolean,
  busy: Boolean
})
defineEmits(['apply'])
const counts = [
  ['groups', '反代组'],
  ['routes', '反代服务'],
  ['forwards', '端口转发'],
  ['ddns_groups', 'DDNS 组'],
  ['firewalls', '防火墙'],
  ['subscriptions', '订阅'],
  ['certificates', '证书文件'],
  ['sunpanel_files', '首页文件'],
  ['sunpanel_images', '首页图片'],
  ['images', '反代图片'],
  ['log_entries', '日志'],
  ['subscription_caches', '订阅缓存']
]
</script>
<style scoped>
.maintenance-preview {
  display: grid;
  gap: 14px;
  padding: 18px;
  border: 1px solid var(--brand);
  border-radius: var(--radius-md);
  min-width: 0;
}
h3 {
  font-size: 15px;
}
dl {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(110px, 1fr));
  gap: 12px;
  margin: 0;
  font-size: 12px;
}
dl > div {
  display: grid;
  gap: 6px;
}
dt {
  color: var(--muted);
}
dd {
  margin: 0;
  color: var(--ink);
}
</style>
