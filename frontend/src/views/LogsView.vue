<template>
  <div class="report-page logs-page animate-fade-in">
    <header class="report-heading">
      <h1>日志</h1>
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
    </header>
    <LogExplorer :key="scope" :scope="scope" />
  </div>
</template>

<script setup>
import { ref } from 'vue'
import LogExplorer from '@/components/LogExplorer.vue'

const scope = ref('access')
const scopes = [
  { value: 'access', label: '反代访问' },
  { value: 'project', label: '系统日志' }
]
</script>

<style scoped>
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
