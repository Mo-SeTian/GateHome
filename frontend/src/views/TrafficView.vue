<template>
  <div class="traffic-page animate-fade-in">
    <div class="page-topbar">
      <div>
        <h1 class="page-title">流量统计</h1>
        <p class="page-subtitle">按反代规则和时间范围查看上传量与下载量，按小时汇总，保留最近 30 天</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-primary" @click="queryTraffic" :disabled="isLoading">
          <RefreshCw :size="14" :class="{ 'spin-icon': isLoading }" />
          {{ isLoading ? '查询中...' : '查询流量' }}
        </button>
      </div>
    </div>

    <!-- Filter Bar -->
    <SpotlightCard class="filters-card">
      <div class="filters-grid">
        <div class="form-group">
          <label class="form-label">反代规则</label>
          <select v-model="selectedRule" class="form-select">
            <option value="ALL">全部反代规则</option>
            <option v-for="r in routes" :key="r.id" :value="r.id">
              {{ r.name }} ({{ r.domain }})
            </option>
          </select>
        </div>

        <div class="form-group">
          <label class="form-label">时间范围</label>
          <select v-model="selectedRange" class="form-select">
            <option value="24">最近 24 小时</option>
            <option value="168">最近 7 天</option>
            <option value="720">最近 30 天</option>
            <option value="custom">自选起止时间</option>
          </select>
        </div>

        <div v-if="selectedRange === 'custom'" class="form-group">
          <label class="form-label">开始时间</label>
          <input v-model="customFrom" type="datetime-local" class="form-input" />
        </div>

        <div v-if="selectedRange === 'custom'" class="form-group">
          <label class="form-label">结束时间</label>
          <input v-model="customTo" type="datetime-local" class="form-input" />
        </div>
      </div>
    </SpotlightCard>

    <!-- Totals Cards -->
    <div class="totals-grid">
      <SpotlightCard class="total-card">
        <div class="card-icon-box upload">
          <ArrowUp :size="20" />
        </div>
        <div class="total-info">
          <span class="total-label">上传量 (客户端发往上游)</span>
          <div class="total-val mono-text">{{ totals.upload }}</div>
          <span class="total-sub">HTTP 请求体传输总量</span>
        </div>
      </SpotlightCard>

      <SpotlightCard class="total-card">
        <div class="card-icon-box download">
          <ArrowDown :size="20" />
        </div>
        <div class="total-info">
          <span class="total-label">下载量 (上游发往客户端)</span>
          <div class="total-val mono-text">{{ totals.download }}</div>
          <span class="total-sub">HTTP 响应体传输总量</span>
        </div>
      </SpotlightCard>
    </div>

    <!-- Traffic Table Card -->
    <SpotlightCard class="table-card">
      <div class="table-responsive">
        <table class="traffic-table">
          <thead>
            <tr>
              <th>反代规则 / 域名</th>
              <th>所属反代组</th>
              <th>上传量 (TX)</th>
              <th>下载量 (RX)</th>
              <th class="text-right">总传输量</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in ruleTrafficList" :key="item.id">
              <td>
                <div class="rule-title-cell">
                  <span class="rule-name">{{ item.name }}</span>
                  <span class="rule-host mono-text">{{ item.domain }}</span>
                </div>
              </td>
              <td class="group-cell">{{ item.group }}</td>
              <td class="mono-text upload-val">{{ item.upload }}</td>
              <td class="mono-text download-val">{{ item.download }}</td>
              <td class="text-right mono-text total-cell">{{ item.total }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="table-tips-footer">
        <Info :size="13" class="tip-icon" />
        <span>上传为客户端发往上游的 HTTP 请求体，下载为上游发往客户端的 HTTP 响应体。统计不含协议头、拦截响应和 WebSocket 数据。</span>
      </div>
    </SpotlightCard>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { RefreshCw, ArrowUp, ArrowDown, Info } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { useAppStore } from '@/composables/useAppStore'
import { useToast } from '@/composables/useToast'

const { routes } = useAppStore()
const { success } = useToast()

const selectedRule = ref('ALL')
const selectedRange = ref('24')
const customFrom = ref('2026-11-08T00:00')
const customTo = ref('2026-11-09T15:00')
const isLoading = ref(false)

const totals = ref({
  upload: '4.89 GB',
  download: '38.42 GB'
})

const ruleTrafficList = computed(() => {
  return [
    { id: 'rt_1', name: 'Jellyfin 影音媒体中心', domain: 'media.stream.org', group: '默认业务组', upload: '240 MB', download: '28.4 GB', total: '28.64 GB' },
    { id: 'rt_2', name: 'Nextcloud 云盘服务', domain: 'cloud.nas.tech', group: '默认业务组', upload: '3.12 GB', download: '6.45 GB', total: '9.57 GB' },
    { id: 'rt_3', name: 'Prometheus 遥测采集', domain: 'monitor.ops.lan', group: '内部管理组', upload: '890 MB', download: '2.14 GB', total: '3.03 GB' },
    { id: 'rt_4', name: 'GateHome 核心控制台', domain: 'gate.internal.lan', group: '内部管理组', upload: '124 MB', download: '840 MB', total: '964 MB' },
    { id: 'rt_5', name: 'HomeAssistant 智能家居', domain: 'home.iot.tech', group: '默认业务组', upload: '340 MB', download: '420 MB', total: '760 MB' }
  ].filter(item => selectedRule.value === 'ALL' || item.id === selectedRule.value)
})

const queryTraffic = () => {
  isLoading.value = true
  setTimeout(() => {
    isLoading.value = false
    success('流量统计数据已更新')
  }, 500)
}
</script>

<style scoped>
.traffic-page { display: flex; flex-direction: column; gap: 20px; }
.page-topbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 16px; }
.page-title { font-size: 22px; font-weight: 700; color: var(--ink); }
.page-subtitle { font-size: 13px; color: var(--muted); margin-top: 3px; }
.header-actions { display: flex; gap: 10px; }
.spin-icon { animation: spin 0.8s linear infinite; }
@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }

.filters-card { padding: 18px 20px; }
.filters-grid { display: flex; align-items: flex-end; gap: 16px; flex-wrap: wrap; }
.form-group { display: flex; flex-direction: column; gap: 6px; }
.form-label { font-size: 12px; font-weight: 600; color: var(--ink-secondary); }

.totals-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.total-card { padding: 20px; display: flex; align-items: center; gap: 18px; }
.card-icon-box {
  width: 48px; height: 48px; border-radius: var(--radius-sm);
  display: flex; align-items: center; justify-content: center; flex-shrink: 0;
}
.card-icon-box.upload { background: rgba(99, 102, 241, 0.12); color: var(--brand-light); border: 1px solid rgba(99, 102, 241, 0.3); }
.card-icon-box.download { background: rgba(6, 182, 212, 0.12); color: var(--brand-cyan); border: 1px solid rgba(6, 182, 212, 0.3); }

.total-info { display: flex; flex-direction: column; gap: 2px; }
.total-label { font-size: 12px; color: var(--muted); }
.total-val { font-size: 26px; font-weight: 700; color: var(--ink); }
.total-sub { font-size: 11px; color: var(--faint); }

.table-card { padding: 0; overflow: hidden; }
.table-responsive { width: 100%; overflow-x: auto; }
.traffic-table { width: 100%; border-collapse: collapse; text-align: left; font-size: 12.5px; }
.traffic-table th {
  padding: 12px 18px; font-weight: 600; color: var(--faint);
  border-bottom: 1px solid var(--line); background: var(--strip-bg);
  white-space: nowrap; font-size: 11.5px;
}
.traffic-table td {
  padding: 12px 18px; border-bottom: 1px solid var(--line);
  color: var(--ink-secondary); vertical-align: middle;
}
.traffic-table tr:hover td { background: rgba(148, 163, 184, 0.05); }

.rule-title-cell { display: flex; flex-direction: column; gap: 2px; }
.rule-name { font-weight: 600; color: var(--ink); }
.rule-host { font-size: 11px; color: var(--faint); }
.group-cell { color: var(--muted); }
.upload-val { color: var(--brand-light); }
.download-val { color: var(--brand-cyan); }
.total-cell { font-weight: 700; color: var(--ink); }
.text-right { text-align: right; }

.table-tips-footer {
  padding: 12px 18px; background: var(--strip-bg); border-top: 1px solid var(--line);
  display: flex; align-items: center; gap: 8px; font-size: 11px; color: var(--faint); line-height: 1.5;
}
.tip-icon { flex-shrink: 0; color: var(--muted); }

@media (max-width: 768px) {
  .totals-grid { grid-template-columns: 1fr; }
  .filters-grid { flex-direction: column; align-items: stretch; }
}
</style>
