<template>
  <div class="report-page animate-fade-in">
    <div class="report-heading">
      <div>
        <h1>系统与访问调用日志</h1>
        <p>查看持久化项目调用和反代访问记录，按规则、来源、结果和时间查询</p>
      </div>
    </div>
    <p class="report-note">日志不记录请求正文、查询参数和认证头；WebSocket 记录在连接结束后出现。</p>
    <div class="report-actions" role="group" aria-label="日志范围">
      <button
        v-for="item in scopes"
        :key="item.value"
        class="btn btn-secondary"
        :aria-pressed="scope === item.value"
        @click="scope = item.value"
      >
        {{ item.label }}
      </button>
    </div>
    <LogExplorer :key="scope" :scope="scope" />
  </div>
</template>
<script setup>
import { ref } from 'vue'
import LogExplorer from '@/components/LogExplorer.vue'
const scope = ref('access')
const scopes = [
  { value: 'access', label: '反代访问日志' },
  { value: 'project', label: '项目系统日志' }
]
</script>
<style scoped>
[aria-pressed='true'] {
  border-color: var(--brand);
  color: var(--ink);
}
</style>
