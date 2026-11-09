<template>
  <div class="firewall-logs-page animate-fade-in">
    <div class="page-topbar">
      <div>
        <h1 class="page-title">WAF 拦截审计日志</h1>
        <p class="page-subtitle">审查网关拦截的高危载荷、SQL注入、恶意扫描器与攻击源溯源详情</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-secondary" @click="refreshLogs">
          <RefreshCw :size="14" :class="{ 'spin-icon': isRefreshing }" />
          刷新审计
        </button>
        <button class="btn btn-danger" @click="clearLogs">
          <Trash2 :size="14" />
          清空记录
        </button>
      </div>
    </div>

    <!-- Filter Bar -->
    <div class="filter-bar">
      <div class="chips-group">
        <button 
          v-for="s in severities" 
          :key="s.key"
          class="chip-btn"
          :class="{ active: selectedSeverity === s.key }"
          @click="selectedSeverity = s.key"
        >
          {{ s.label }}
        </button>
      </div>

      <div class="search-box">
        <Search :size="14" class="search-icon" />
        <input v-model="searchQuery" type="text" placeholder="按 IP、规则或路径检索..." class="search-input" />
      </div>
    </div>

    <!-- Audit Log Table Card -->
    <SpotlightCard class="table-card">
      <div class="table-responsive">
        <table class="audit-table">
          <thead>
            <tr>
              <th>时间</th>
              <th>危险级别</th>
              <th>攻击源 IP & 地理归属</th>
              <th>命中拦截规则</th>
              <th>请求路径 (URI)</th>
              <th>处理动作</th>
              <th class="text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="log in filteredLogs" :key="log.id">
              <td class="mono-text time-cell">{{ log.time }}</td>
              <td>
                <span class="badge" :class="severityClass(log.severity)">
                  {{ log.severity }}
                </span>
              </td>
              <td>
                <div class="ip-cell">
                  <span class="ip-text mono-text">{{ log.ip }}</span>
                  <span class="geo-text">{{ log.geo }}</span>
                </div>
              </td>
              <td class="rule-cell">{{ log.rule }}</td>
              <td class="mono-text uri-cell" :title="log.uri">{{ log.uri }}</td>
              <td>
                <span class="badge" :class="log.action === 'BLOCK' ? 'badge-bad' : 'badge-warn'">
                  {{ log.action }}
                </span>
              </td>
              <td class="text-right">
                <button class="btn btn-secondary btn-sm" @click="blockIpImmediately(log)">
                  <Ban :size="12" /> 永久封禁
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="filteredLogs.length === 0" class="empty-state">
        <AlertCircle :size="30" class="empty-icon" />
        <div>没有匹配的拦截审计记录</div>
      </div>
    </SpotlightCard>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { RefreshCw, Trash2, Search, Ban, AlertCircle } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { useAppStore } from '@/composables/useAppStore'
import { useToast } from '@/composables/useToast'

const { firewallLogs, ipBlocks } = useAppStore()
const { success, warn, info } = useToast()

const isRefreshing = ref(false)
const selectedSeverity = ref('ALL')
const searchQuery = ref('')

const severities = [
  { key: 'ALL', label: '全部级别' },
  { key: 'CRITICAL', label: '严重 (CRITICAL)' },
  { key: 'HIGH', label: '高危 (HIGH)' },
  { key: 'MEDIUM', label: '中危 (MEDIUM)' },
  { key: 'LOW', label: '轻微 (LOW)' }
]

const filteredLogs = computed(() => {
  return firewallLogs.value.filter(log => {
    if (selectedSeverity.value !== 'ALL' && log.severity !== selectedSeverity.value) {
      return false
    }
    if (searchQuery.value.trim()) {
      const q = searchQuery.value.toLowerCase()
      return log.ip.toLowerCase().includes(q) ||
             log.rule.toLowerCase().includes(q) ||
             log.uri.toLowerCase().includes(q) ||
             log.geo.toLowerCase().includes(q)
    }
    return true
  })
})

const severityClass = (sev) => {
  if (sev === 'CRITICAL' || sev === 'HIGH') return 'badge-bad'
  if (sev === 'MEDIUM') return 'badge-warn'
  return 'badge-neutral'
}

const refreshLogs = () => {
  isRefreshing.value = true
  setTimeout(() => {
    isRefreshing.value = false
    success('审计日志已同步最新拦截状态')
  }, 600)
}

const clearLogs = () => {
  if (confirm('确定要清空当前的审计拦截日志记录吗？')) {
    firewallLogs.value = []
    info('拦截记录已清空')
  }
}

const blockIpImmediately = (log) => {
  const exists = ipBlocks.value.find(b => b.ip === log.ip)
  if (exists) {
    warn(`IP ${log.ip} 已在黑名单列表中`)
    return
  }
  ipBlocks.value.unshift({
    id: `ip_${Date.now()}`,
    ip: log.ip,
    type: 'BLACK',
    reason: `触发 WAF 规则: ${log.rule}`,
    expireAt: '永久封禁',
    hits: 1,
    createdAt: '刚刚'
  })
  success(`已将高危攻击源 ${log.ip} 加入全局黑名单并永久阻断`)
}
</script>

<style scoped>
.firewall-logs-page { display: flex; flex-direction: column; gap: 20px; }
.page-topbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 16px; }
.page-title { font-size: 22px; font-weight: 700; color: var(--ink); }
.page-subtitle { font-size: 13px; color: var(--muted); margin-top: 3px; }
.header-actions { display: flex; gap: 10px; }
.spin-icon { animation: spin 0.8s linear infinite; }
@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }

.filter-bar { display: flex; align-items: center; justify-content: space-between; gap: 16px; flex-wrap: wrap; }
.chips-group { display: flex; gap: 6px; }
.chip-btn {
  padding: 5px 12px; border-radius: var(--radius-pill);
  background: var(--strip-bg); border: 1px solid var(--line);
  color: var(--muted); font-size: 12px; font-weight: 500;
}
.chip-btn.active {
  background: rgba(99, 102, 241, 0.18); border-color: rgba(99, 102, 241, 0.4);
  color: #fff; font-weight: 600;
}
.theme-light .chip-btn.active { color: var(--brand); }

.search-box {
  display: flex; align-items: center; gap: 8px; padding: 6px 12px;
  background: var(--input-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); width: 280px;
}
.search-icon { color: var(--faint); }
.search-input { border: none; background: transparent; color: var(--ink); font-size: 12.5px; flex: 1; }

.table-card { padding: 0; overflow: hidden; }
.table-responsive { width: 100%; overflow-x: auto; }

.audit-table {
  width: 100%; border-collapse: collapse; text-align: left; font-size: 12.5px;
}
.audit-table th {
  padding: 12px 18px; font-weight: 600; color: var(--faint);
  border-bottom: 1px solid var(--line); background: var(--strip-bg);
  white-space: nowrap; font-size: 11.5px;
}
.audit-table td {
  padding: 12px 18px; border-bottom: 1px solid var(--line);
  color: var(--ink-secondary); vertical-align: middle;
}
.audit-table tr:hover td { background: rgba(148, 163, 184, 0.05); }

.time-cell { color: var(--faint); font-size: 11.5px; }
.ip-cell { display: flex; flex-direction: column; gap: 2px; }
.ip-text { font-weight: 600; color: var(--ink); }
.geo-text { font-size: 11px; color: var(--faint); }
.rule-cell { font-weight: 500; color: var(--ink); }
.uri-cell { max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--brand-cyan); }
.text-right { text-align: right; }

.empty-state {
  padding: 40px; text-align: center; color: var(--muted);
  display: flex; flex-direction: column; align-items: center; gap: 10px;
}
.empty-icon { color: var(--faint); }

@media (max-width: 768px) {
  .filter-bar { flex-direction: column; align-items: stretch; }
  .search-box { width: 100%; }
}
</style>
