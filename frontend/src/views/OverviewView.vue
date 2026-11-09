<template>
  <div class="overview-page animate-fade-in">
    <div class="page-topbar">
      <div>
        <h1 class="page-title">系统概览</h1>
        <p class="page-subtitle">GateHome 网关状态、资源遥测与访问统计</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-secondary" :disabled="isRefreshing" @click="refresh(true)">
          <RefreshCw :size="14" :class="{ 'spin-icon': isRefreshing }" />
          {{ isRefreshing ? '刷新中…' : '刷新数据' }}
        </button>
        <router-link to="/routes" class="btn btn-primary"><Plus :size="14" /> 管理反向代理</router-link>
      </div>
    </div>

    <p v-if="loadError" class="overview-message" role="alert">{{ loadError }}{{ dashboard ? '，当前显示最近一次采样。' : '。' }}</p>
    <p v-else-if="!dashboard" class="overview-message" role="status">正在读取网关遥测…</p>
    <p v-else class="overview-message">最近采样 {{ sampledAt }} · {{ resources.platform }}</p>

    <div class="bento-grid">
      <SpotlightCard v-for="metric in resourceCards" :key="metric.label" class="metric-card">
        <div class="card-header">
          <div class="metric-info">
            <span class="metric-label">{{ metric.label }}</span>
            <div class="metric-value">
              {{ metric.percent == null ? '—' : metric.percent.toFixed(1) }}<span v-if="metric.percent != null" class="metric-unit">%</span>
            </div>
          </div>
          <div class="metric-icon-box" :class="metric.kind"><component :is="metric.icon" :size="20" /></div>
        </div>
        <div class="progress-track">
          <div v-if="metric.percent != null" class="progress-fill" :class="metric.fill" :style="{ width: metric.percent + '%' }" />
        </div>
        <div class="metric-footer"><span>{{ metric.detail }}</span><span>{{ metric.total }}</span></div>
      </SpotlightCard>

      <SpotlightCard class="metric-card">
        <div class="card-header">
          <div class="metric-info">
            <span class="metric-label">运行期请求</span>
            <div class="metric-value">{{ count(gatewayStatus?.requests) }}</div>
          </div>
          <div class="metric-icon-box net"><Zap :size="20" /></div>
        </div>
        <div class="metric-extra-badges">
          <span class="badge badge-brand">24 小时访问 {{ count(access?.total) }}</span>
        </div>
        <div class="metric-footer"><span>拦截 {{ count(gatewayStatus?.blocked) }}</span><span>失败 {{ count(gatewayStatus?.failures) }}</span></div>
      </SpotlightCard>
    </div>

    <div class="middle-grid">
      <SpotlightCard class="chart-card">
        <div class="section-title-bar">
          <div class="title-with-icon"><Radio :size="17" class="brand-text" /><span class="section-title">实时网卡流量</span></div>
          <div class="chart-legend">
            <span class="legend-item"><span class="legend-dot rx" />下行 {{ rate(resources.rx_rate) }}</span>
            <span class="legend-item"><span class="legend-dot tx" />上行 {{ rate(resources.tx_rate) }}</span>
          </div>
        </div>

        <div class="chart-container">
          <svg v-if="samples.length >= 2" viewBox="0 0 700 160" class="traffic-svg" preserveAspectRatio="none" aria-hidden="true">
            <line v-for="y in [40, 80, 120]" :key="y" x1="0" :y1="y" x2="700" :y2="y" stroke="var(--line)" stroke-dasharray="4 4" />
            <polyline :points="networkLines.rx" fill="none" stroke="var(--brand-cyan)" stroke-width="2" />
            <polyline :points="networkLines.tx" fill="none" stroke="var(--brand-light)" stroke-width="2.5" />
          </svg>
          <p v-else class="chart-empty">{{ resources.network_error || '等待连续的流量采样' }}</p>
        </div>
        <div class="chart-footer-stats">
          <div class="stat-pill"><span class="label">累计接收 (RX)</span><span class="val mono-text">{{ bytes(networkAvailable ? resources.rx_bytes : null) }}</span></div>
          <div class="stat-pill"><span class="label">累计发送 (TX)</span><span class="val mono-text">{{ bytes(networkAvailable ? resources.tx_bytes : null) }}</span></div>
          <div class="stat-pill"><span class="label">采集网卡</span><span class="val">{{ resources.interface || '—' }}</span></div>
        </div>
      </SpotlightCard>

      <SpotlightCard class="status-summary-card">
        <div class="section-title-bar">
          <span class="section-title">网关与服务</span>
          <span class="badge" :class="gatewayState.tone === 'ok' ? 'badge-ok' : 'badge-warn'">{{ gatewayState.label }}</span>
        </div>
        <div class="status-rows">
          <router-link to="/routes" class="status-item">
            <span class="item-name">反向代理上游</span>
            <span class="item-val">{{ healthLabel }}</span>
          </router-link>
          <router-link to="/access" class="status-item">
            <span class="item-name">24 小时防火墙拦截</span>
            <span class="item-val">{{ count(access?.firewall_blocked) }} 次</span>
          </router-link>
          <router-link to="/certificates" class="status-item">
            <span class="item-name">已就绪证书</span>
            <span class="item-val">{{ count(certificateCount) }}</span>
          </router-link>
          <div class="status-item listener-row">
            <span class="item-name">当前反代监听</span>
            <span class="item-val">{{ listenerLabel }}</span>
          </div>
          <router-link to="/forwards" class="status-item">
            <span class="item-name">端口转发监听</span>
            <span class="item-val">{{ count(forwardListenerCount) }}</span>
          </router-link>
        </div>
        <div class="quick-links-bar">
          <router-link to="/logs" class="btn btn-secondary btn-sm"><ScrollText :size="13" /> 访问明细</router-link>
          <router-link to="/firewall-logs" class="btn btn-secondary btn-sm"><ShieldAlert :size="13" /> 拦截审计</router-link>
          <router-link to="/backups" class="btn btn-secondary btn-sm"><HardDriveDownload :size="13" /> 备份快照</router-link>
        </div>
      </SpotlightCard>
    </div>

    <div class="middle-grid">
      <SpotlightCard class="chart-card">
        <div class="section-title-bar">
          <span class="section-title">24 小时访问趋势</span>
          <span class="item-val">共 {{ count(access?.total) }} 次</span>
        </div>
        <div class="chart-container">
          <svg v-if="access?.total" viewBox="0 0 700 160" class="traffic-svg" preserveAspectRatio="none" aria-hidden="true">
            <line v-for="y in [40, 80, 120]" :key="y" x1="0" :y1="y" x2="700" :y2="y" stroke="var(--line)" stroke-dasharray="4 4" />
            <polyline :points="accessLine" fill="none" stroke="var(--brand-light)" stroke-width="2.5" />
          </svg>
          <p v-else class="chart-empty">{{ access ? '最近 24 小时暂无访问记录' : '等待访问统计' }}</p>
        </div>
      </SpotlightCard>
      <SpotlightCard class="chart-card">
        <div class="section-title-bar">
          <span class="section-title">24 小时访问地区</span>
          <span class="item-val">{{ count(dashboard?.regions?.unique_ips) }} 个来源 IP</span>
        </div>
        <ul v-if="provinces.length" class="region-list">
          <li v-for="province in provinces" :key="province.name">
            <span>{{ province.name }}</span><span>{{ count(province.requests) }} 次访问</span>
          </li>
        </ul>
        <p v-else class="overview-message">{{ dashboard ? '暂无可归属省份的访问记录' : '等待地区统计' }}</p>
        <p v-if="dashboard?.regions?.unmapped" class="overview-message">{{ count(dashboard.regions.unmapped) }} 次访问未归属省份</p>
      </SpotlightCard>
    </div>

    <div class="bottom-section">
      <div class="section-title-bar">
        <span class="section-title">24 小时高频访问服务</span>
        <router-link to="/routes" class="btn btn-ghost btn-sm">查看反代规则 →</router-link>
      </div>
      <p v-if="!topRoutes.length" class="overview-message">{{ routesEmptyLabel }}</p>
      <div v-else class="recent-routes-grid">
        <SpotlightCard v-for="route in topRoutes" :key="route.key" class="mini-route-card">
          <div class="mini-card-top">
            <div class="mini-card-icon"><Network :size="16" /></div>
            <span v-if="route.health?.state !== 'disabled' && route.health?.checked_at" class="status-dot"
              :class="route.health.healthy ? 'ok' : 'warn'" :title="route.health.label" />
          </div>
          <div class="mini-card-name">{{ route.name || route.host }}</div>
          <div class="mini-card-domain mono-text">{{ route.host }}</div>
          <div class="mini-card-footer">
            <span>{{ count(route.total) }} 次请求</span>
            <span v-if="route.health?.healthy">上游探测 {{ count(route.health.duration_ms) }} ms</span>
          </div>
        </SpotlightCard>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, shallowRef } from 'vue'
import {
  RefreshCw, Plus, Cpu, Activity, HardDrive, Zap, Radio, Network,
  ScrollText, ShieldAlert, HardDriveDownload
} from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { adminApi } from '@/api/admin'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useToast } from '@/composables/useToast'

const { gatewayStatus, gatewayConfig, gatewayState, refreshStatus, refreshConfig } = useGatewayStatus()
const toast = useToast()
const dashboard = shallowRef(null)
const serviceHealth = shallowRef(null)
const healthUnavailable = ref(false)
const samples = ref([])
const isRefreshing = ref(false)
const loadError = ref('')
const controller = new AbortController()
const resources = computed(() => dashboard.value?.resources ?? {})
const access = computed(() => dashboard.value?.access)
const provinces = computed(() => (dashboard.value?.regions?.provinces ?? []).slice(0, 5))
const networkAvailable = computed(() => resources.value.interface && !resources.value.network_error)
const sampledAt = computed(() => new Date(dashboard.value?.sampled_at).toLocaleTimeString('zh-CN', { hour12: false }))
const numberFormat = new Intl.NumberFormat('zh-CN')

function count(value) {
  return value == null ? '—' : numberFormat.format(value)
}

function bytes(value) {
  if (!Number.isFinite(value) || value < 0) return '—'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  const index = value < 1 ? 0 : Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`
}

function rate(value) {
  return value == null ? '—' : `${bytes(value)}/s`
}

function usage(used, total) {
  return Number.isFinite(used) && total > 0 ? Math.min(100, used / total * 100) : null
}

const resourceCards = computed(() => {
  const resource = resources.value
  const disk = resource.storage?.find(item => item.name === '数据')
  return [
    {
      label: 'CPU 负载率', kind: 'cpu', icon: Cpu, fill: 'brand', percent: resource.cpu_percent,
      detail: resource.cpu_error || (resource.cpus ? `${resource.cpus} 个逻辑 CPU` : '暂无采样')
    },
    {
      label: '内存占用', kind: 'mem', icon: Activity, fill: 'cyan',
      percent: resource.memory_error ? null : usage(resource.memory_used, resource.memory_total),
      detail: resource.memory_error || (resource.memory_total ? `已用 ${bytes(resource.memory_used)}` : '暂无采样'),
      total: resource.memory_total ? `共 ${bytes(resource.memory_total)}` : ''
    },
    {
      label: '数据盘空间', kind: 'disk', icon: HardDrive, fill: 'ok',
      percent: disk?.error ? null : usage(disk?.used, disk?.total),
      detail: disk?.error || (disk?.total ? `已用 ${bytes(disk.used)}` : '暂无采样'),
      total: disk?.total ? `共 ${bytes(disk.total)}` : ''
    }
  ]
})

const certificateCount = computed(() => gatewayStatus.value
  ? (gatewayStatus.value.certificates ?? []).filter(cert => cert.ready).length : null)
const forwardListenerCount = computed(() => gatewayStatus.value
  ? (gatewayStatus.value.forward_listeners ?? []).filter(listener => listener.enabled).length : null)
const listenerLabel = computed(() => {
  if (!gatewayStatus.value) return '—'
  const listeners = (gatewayStatus.value.listeners ?? []).filter(listener => listener.enabled)
  return listeners.map(listener => `HTTP ${listener.http_port} / HTTPS ${listener.https_port}`).join(' · ') || '无启用监听'
})

const healthLabel = computed(() => {
  if (healthUnavailable.value) return '读取失败'
  if (!serviceHealth.value) return '—'
  if (!serviceHealth.value.options?.enabled) return '检测已关闭'
  const results = Object.values(serviceHealth.value.routes ?? {}).filter(item => item.state !== 'disabled' && item.state !== 'cancelled')
  if (!results.length) return '暂无探测'
  return `${results.filter(item => item.healthy).length} / ${results.length} 可达`
})

const topRoutes = computed(() => {
  const routes = new Map((gatewayConfig.value?.routes ?? []).map(route => [`${route.group_id}/${route.host}`, route]))
  return (access.value?.rules ?? []).filter(item => routes.has(item.rule))
    .sort((a, b) => b.total - a.total)
    .slice(0, 4)
    .map(item => {
      const route = routes.get(item.rule)
      return { ...route, key: item.rule, total: item.total, health: serviceHealth.value?.routes?.[route.id] }
    })
})
const routesEmptyLabel = computed(() => {
  if (!access.value) return '等待访问统计'
  if (!gatewayConfig.value) return '反代配置未加载，请刷新数据'
  return '暂无可关联到当前反代规则的访问记录'
})

function linePoints(values, max) {
  return values.map((value, index) => `${700 * index / Math.max(1, values.length - 1)},${152 - 144 * value / max}`).join(' ')
}

const networkLines = computed(() => {
  const rx = samples.value.map(sample => sample.rx)
  const tx = samples.value.map(sample => sample.tx)
  const max = Math.max(1, ...rx, ...tx)
  return { rx: linePoints(rx, max), tx: linePoints(tx, max) }
})
const accessLine = computed(() => {
  const totals = (access.value?.trend ?? []).map(bucket => bucket.total)
  return linePoints(totals, Math.max(1, ...totals))
})

function recordSample(data) {
  const { interface: iface, rx_rate: rx, tx_rate: tx, network_error: error } = data.resources
  const last = samples.value.at(-1)
  if (error || !Number.isFinite(rx) || !Number.isFinite(tx)) {
    samples.value = []
    return
  }
  if (last && last.iface !== iface) samples.value = []
  const time = Date.parse(data.sampled_at)
  if (samples.value.length && time - last.time < 5000) return
  samples.value = [...samples.value.slice(-23), { iface, time, rx, tx }]
}

async function refresh(manual = false) {
  if (isRefreshing.value) return
  isRefreshing.value = true
  const signal = controller.signal
  try {
    const tasks = [
      adminApi.dashboard(signal).then(data => {
        dashboard.value = data
        recordSample(data)
        loadError.value = ''
      }).catch(err => {
        if (!signal.aborted) loadError.value = err.message
        throw err
      }),
      adminApi.serviceHealth(signal).then(data => {
        serviceHealth.value = data
        healthUnavailable.value = false
      }).catch(err => {
        if (!signal.aborted) healthUnavailable.value = true
        throw err
      })
    ]
    if (manual) tasks.push(refreshStatus(signal), refreshConfig(signal))
    const results = await Promise.allSettled(tasks)
    if (!manual || signal.aborted || results.some(result => result.reason?.status === 401)) return
    const failure = results.find(result => result.status === 'rejected')
    if (failure) toast.error(`概览刷新失败：${failure.reason.message}`)
    else toast.success('概览数据已刷新')
  } finally {
    isRefreshing.value = false
  }
}

let timer
onMounted(() => {
  refresh()
  timer = setInterval(refresh, 10000)
})
onUnmounted(() => {
  clearInterval(timer)
  controller.abort()
})
</script>

<style scoped>
.overview-page {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.page-topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 16px;
}

.page-title {
  font-size: 22px;
  font-weight: 700;
  color: var(--ink);
  letter-spacing: -0.3px;
}

.page-subtitle {
  font-size: 13px;
  color: var(--muted);
  margin-top: 3px;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.spin-icon {
  animation: spin 0.8s linear infinite;
}
@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

/* Bento Grid */
.bento-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
}

.metric-card {
  padding: 18px 20px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
}

.card-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
}

.metric-label {
  font-size: 12px;
  color: var(--muted);
  font-weight: 500;
}

.metric-value {
  font-size: 26px;
  font-weight: 700;
  color: var(--ink);
  font-family: var(--sans);
  margin-top: 4px;
}

.metric-unit {
  font-size: 14px;
  font-weight: 500;
  color: var(--faint);
  margin-left: 4px;
}

.metric-icon-box {
  width: 38px;
  height: 38px;
  border-radius: var(--radius-sm);
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--line);
}
.metric-icon-box.cpu { background: rgba(99, 102, 241, 0.12); color: var(--brand-light); }
.metric-icon-box.mem { background: rgba(6, 182, 212, 0.12); color: var(--brand-cyan); }
.metric-icon-box.disk { background: rgba(16, 185, 129, 0.12); color: var(--ok); }
.metric-icon-box.net { background: rgba(245, 158, 11, 0.12); color: var(--warn); }

.progress-track {
  width: 100%;
  height: 6px;
  background: var(--strip-bg);
  border-radius: 999px;
  overflow: hidden;
  margin: 14px 0 10px 0;
}

.progress-fill {
  height: 100%;
  border-radius: 999px;
  transition: width 0.5s ease;
}
.progress-fill.brand { background: var(--brand-gradient); }
.progress-fill.cyan { background: linear-gradient(90deg, #06b6d4, #38bdf8); }
.progress-fill.ok { background: linear-gradient(90deg, #10b981, #34d399); }

.metric-extra-badges {
  display: flex;
  gap: 8px;
  margin: 12px 0 8px 0;
}

.metric-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 11.5px;
  color: var(--faint);
}

.mono-text {
  font-family: var(--mono);
}

/* Middle Grid */
.middle-grid {
  display: grid;
  grid-template-columns: 2fr 1fr;
  gap: 16px;
}

.chart-card {
  padding: 20px;
  display: flex;
  flex-direction: column;
}

.section-title-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 16px;
}

.title-with-icon {
  display: flex;
  align-items: center;
  gap: 8px;
}
.brand-text { color: var(--brand-light); }

.section-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--ink);
}

.chart-legend {
  display: flex;
  gap: 14px;
  font-size: 12px;
  color: var(--muted);
}
.legend-item {
  display: flex;
  align-items: center;
  gap: 6px;
}
.legend-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}
.legend-dot.rx { background: #06b6d4; box-shadow: 0 0 8px rgba(6, 182, 212, 0.5); }
.legend-dot.tx { background: #6366f1; box-shadow: 0 0 8px rgba(99, 102, 241, 0.5); }

.chart-container {
  height: 160px;
  width: 100%;
}

.traffic-svg {
  width: 100%;
  height: 100%;
}

.chart-empty {
  display: grid;
  place-items: center;
  height: 100%;
  margin: 0;
  color: var(--muted);
  text-align: center;
}

.overview-message {
  color: var(--muted);
  font-size: 13px;
  margin: 0;
}
.overview-message[role="alert"] { color: var(--bad); }

.region-list {
  list-style: none;
  padding: 0;
  margin: 0 0 8px;
  color: var(--ink-secondary);
}
.region-list li {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 0;
  border-bottom: 1px solid var(--line);
}

.chart-footer-stats {
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-top: 1px solid var(--line);
  padding-top: 14px;
  margin-top: 14px;
}

.stat-pill {
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.stat-pill .label { font-size: 11px; color: var(--faint); }
.stat-pill .val { font-size: 12.5px; font-weight: 600; color: var(--ink); }

/* Status Summary Card */
.status-summary-card {
  padding: 20px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
}

.status-rows {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-bottom: 16px;
}

.status-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 10px;
  border-radius: var(--radius-sm);
  background: var(--strip-bg);
  cursor: pointer;
  transition: var(--transition-smooth);
}
.status-item:hover {
  background: var(--card-hover-bg);
  transform: translateX(2px);
}

.listener-row { cursor: default; }
.listener-row:hover { transform: none; }

.item-name {
  font-size: 12.5px;
  color: var(--ink-secondary);
  font-weight: 500;
}

.item-val {
  font-size: 11.5px;
  color: var(--muted);
  font-family: var(--mono);
  text-align: end;
}

.quick-links-bar {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding-top: 12px;
  border-top: 1px solid var(--line);
}

/* Bottom Recent Routes */
.bottom-section {
  display: flex;
  flex-direction: column;
}

.recent-routes-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
}

.mini-route-card {
  padding: 16px;
}

.mini-card-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}

.mini-card-icon {
  width: 30px;
  height: 30px;
  border-radius: var(--radius-sm);
  background: rgba(99, 102, 241, 0.12);
  color: var(--brand-light);
  display: flex;
  align-items: center;
  justify-content: center;
}

.mini-card-name {
  font-size: 13.5px;
  font-weight: 600;
  color: var(--ink);
  margin-bottom: 2px;
}

.mini-card-domain {
  font-size: 11.5px;
  color: var(--faint);
  margin-bottom: 12px;
}

.mini-card-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 11px;
  color: var(--muted);
  border-top: 1px solid var(--line);
  padding-top: 8px;
}

/* Responsive */
@media (max-width: 1200px) {
  .bento-grid {
    grid-template-columns: repeat(2, 1fr);
  }
  .middle-grid {
    grid-template-columns: 1fr;
  }
  .recent-routes-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

@media (max-width: 767px) {
  .bento-grid {
    grid-template-columns: 1fr;
  }
  .recent-routes-grid {
    grid-template-columns: 1fr;
  }
  .chart-footer-stats {
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
  }
}
</style>
