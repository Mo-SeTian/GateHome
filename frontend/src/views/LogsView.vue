<template>
  <div class="report-page logs-page animate-fade-in">
    <div class="page-topbar">
      <div class="title-wrap">
        <h1 class="page-title">系统与访问调用日志</h1>
        <p class="page-desc">按规则、来源、结果和时间实时审计（不记录请求正文与认证头；WebSocket 结束时记录）</p>
      </div>
      <!-- 右侧优雅分段切换器，彻底消除右侧空白闲置 -->
      <div class="scope-segmented" role="group" aria-label="日志范围">
        <button
          v-for="item in scopes"
          :key="item.value"
          type="button"
          class="scope-seg-btn"
          :class="{ active: scope === item.value }"
          :aria-pressed="scope === item.value"
          @click="scope = item.value"
        >
          {{ item.label }}
        </button>
      </div>
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
.page-topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 16px;
}
.title-wrap {
  display: flex;
  flex-direction: column;
}
.page-title {
  font-size: 20px;
  font-weight: 700;
  color: var(--ink);
  margin: 0;
}
.page-desc {
  font-size: 12.5px;
  color: var(--muted);
  margin: 4px 0 0;
}
.scope-segmented {
  display: inline-flex;
  align-items: center;
  padding: 3px;
  background: var(--strip-bg);
  border: 1px solid var(--line);
  border-radius: var(--radius-pill);
}
.scope-seg-btn {
  padding: 6px 16px;
  font-size: 12.5px;
  font-weight: 500;
  color: var(--muted);
  border-radius: var(--radius-pill);
  transition: var(--transition-smooth);
}
.scope-seg-btn:hover {
  color: var(--ink);
}
.scope-seg-btn.active {
  background: var(--brand-gradient);
  color: #ffffff;
  font-weight: 600;
  box-shadow: 0 2px 8px rgba(99, 102, 241, 0.4);
}

@media (max-width: 640px) {
  .page-topbar {
    flex-direction: column;
    align-items: stretch;
    gap: 12px;
  }
  .scope-segmented {
    width: 100%;
    display: flex;
  }
  .scope-seg-btn {
    flex: 1;
    text-align: center;
    padding: 7px 10px;
    font-size: 12px;
  }
}
</style>
