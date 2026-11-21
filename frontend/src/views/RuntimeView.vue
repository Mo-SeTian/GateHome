<template>
  <div class="report-page runtime-page animate-fade-in">
    <header class="page-head">
      <div class="page-head-title">
        <div class="title-line">
          <h1>运行状态</h1>
          <span
            v-if="data"
            class="badge mono"
            :class="pollingPaused ? 'badge-neutral' : 'badge-ok'"
          >
            <span class="status-dot" :class="pollingPaused ? 'neutral' : 'ok'" />
            {{ pollingPaused ? '自动刷新已暂停' : `每 10s 自动同步 · ${sampledClock}` }}
          </span>
        </div>
        <p v-if="hostSummary" class="page-sub mono">{{ hostSummary }}</p>
        <p v-else-if="!data && !runtimeError" class="page-sub" role="status">正在读取运行状态…</p>
      </div>
      <div class="runtime-actions">
        <button type="button" class="btn btn-secondary" @click="togglePolling">
          {{ pollingPaused ? '恢复自动刷新' : '暂停刷新' }}
        </button>
        <button type="button" class="btn btn-secondary" :disabled="loading" @click="load(true)">刷新数据</button>
        <router-link class="btn btn-primary" to="/notifications?tab=push">故障推送</router-link>
      </div>
    </header>

    <p v-if="runtimeError" class="report-error" role="alert">{{ runtimeError }}</p>
    <p v-if="data?.restart_required" class="report-warning">监听配置待重启。列表显示当前运行中的端口。</p>

    <div class="kpi-grid-4">
      <SpotlightCard class="kpi-card">
        <div class="kpi-top">
          <span>CPU 占用</span>
          <span v-if="cpuMeta" class="mono">{{ cpuMeta }}</span>
        </div>
        <div class="kpi-val mono">{{ percentText(resources.cpu_percent) }}</div>
        <progress
          v-if="resources.cpu_percent != null"
          class="gh-progress"
          :class="progressTone(resources.cpu_percent)"
          :value="resources.cpu_percent"
          max="100"
          aria-label="CPU 占用"
        />
        <p v-if="resources.cpu_error" class="kpi-foot error-text">{{ resources.cpu_error }}</p>
      </SpotlightCard>

      <SpotlightCard class="kpi-card">
        <div class="kpi-top">
          <span>内存占用</span>
          <span v-if="memoryMeta" class="mono">{{ memoryMeta }}</span>
        </div>
        <div class="kpi-val mono">{{ percentText(memoryPercent) }}</div>
        <progress
          v-if="memoryPercent != null"
          class="gh-progress"
          :class="progressTone(memoryPercent)"
          :value="memoryPercent"
          max="100"
          aria-label="内存占用"
        />
        <p v-if="resources.memory_error" class="kpi-foot error-text">{{ resources.memory_error }}</p>
      </SpotlightCard>

      <SpotlightCard class="kpi-card">
        <div class="kpi-top">
          <span>监听端口</span>
          <span v-if="data" class="mono">TCP {{ listenCount('TCP') }} / UDP {{ listenCount('UDP') }}</span>
        </div>
        <div class="kpi-val mono">{{ data ? uniqueListenerPorts : '—' }}</div>
        <p class="kpi-foot mono">{{ data ? `全部 ${data.listeners.length} 条绑定` : '' }}</p>
      </SpotlightCard>

      <SpotlightCard class="kpi-card" :class="{ 'kpi-card-bad': failingRoutesCount > 0 }">
        <div class="kpi-top">
          <span>反代健康</span>
          <span v-if="data" class="mono">{{ data.health.enabled ? `${data.health.interval}s 间隔` : '已停用' }}</span>
        </div>
        <div class="kpi-val mono">{{ monitoredRoutesCount ? `${healthyRoutesCount} / ${monitoredRoutesCount}` : '—' }}</div>
        <p class="kpi-foot">
          <button
            v-if="failingRoutesCount > 0"
            type="button"
            class="kpi-filter-btn"
            @click="focusFailingRoutes"
          >
            {{ failingRoutesCount }} 个异常 · 点击筛选 →
          </button>
          <span v-else-if="pendingRoutesCount > 0">{{ pendingRoutesCount }} 个等待检测</span>
          <span v-else-if="monitoredRoutesCount > 0">全部可达</span>
        </p>
      </SpotlightCard>
    </div>

    <SpotlightCard v-if="data" ref="routesSectionRef" class="report-panel">
      <div class="runtime-section-head">
        <div class="section-title-wrap">
          <h2>反代上游连通性</h2>
          <span class="badge badge-neutral mono">{{ healthSchedule }}</span>
        </div>
        <div class="route-tools">
          <div class="chips-group" role="group" aria-label="反代状态筛选">
            <button
              v-for="tab in routeTabs"
              :key="tab.key"
              type="button"
              class="chip-btn"
              :class="{ active: routeFilter === tab.key }"
              :aria-pressed="routeFilter === tab.key"
              @click="routeFilter = tab.key"
            >
              {{ tab.label }}
              <span class="chip-count">{{ tab.count }}</span>
            </button>
          </div>
          <div class="search-box route-search">
            <Search :size="13" class="search-icon" aria-hidden="true" />
            <input
              v-model="routeQuery"
              type="search"
              class="search-input"
              aria-label="搜索反代"
              placeholder="搜索域名/上游…"
              @keydown.esc="routeQuery = ''"
            />
          </div>
        </div>
      </div>

      <div v-if="filteredRoutes.length" class="route-list">
        <div
          v-for="route in filteredRoutes"
          :key="route.id"
          class="route-item"
          :class="{ 'is-failing': isFailingRoute(route) }"
        >
          <button
            type="button"
            class="route-row-btn history-toggle"
            :aria-expanded="openRoute === route.id"
            :aria-controls="'route-history-' + route.id"
            @click="toggleRoute(route.id)"
          >
            <div class="route-info">
              <div class="route-id-line">
                <span class="status-dot" :class="routeDotTone(route)" />
                <span class="route-name">{{ route.name }}</span>
                <span v-if="route.host !== route.name" class="route-host mono">{{ route.host }}</span>
              </div>
              <div class="route-path-line mono">
                <template v-if="route.entries?.length">
                  <span v-for="entry in route.entries" :key="entry" class="entry-pill">{{ entry }}</span>
                </template>
                <span v-else class="no-entry">无运行入口</span>
                <span class="path-arrow" aria-hidden="true">→</span>
                <code class="upstream-pill">{{ route.upstream }}</code>
              </div>
            </div>

            <div class="route-status-col">
              <div class="route-health-text mono" :class="routeHealthTone(route)">
                <span>{{ healthLabel(route) }}</span>
                <small v-if="route.health">{{ reportDate(route.health.checked_at) }}</small>
              </div>
              <ChevronRight :size="16" class="route-chevron" :class="{ open: openRoute === route.id }" aria-hidden="true" />
            </div>
          </button>

          <div v-if="openRoute === route.id" :id="'route-history-' + route.id">
            <RouteHealthHistory :key="route.id" :route-id="route.id" />
          </div>
        </div>
      </div>
      <p v-else class="report-empty">{{ data.routes.length ? '暂无符合条件的反代服务' : '暂无反代服务' }}</p>
    </SpotlightCard>

    <div class="runtime-split">
      <SpotlightCard v-if="data" class="report-panel">
        <div class="runtime-section-head">
          <div class="section-title-wrap">
            <h2>GateHome 监听端口</h2>
            <span class="badge badge-neutral mono">{{ filteredListeners.length }} / {{ data.listeners.length }}</span>
          </div>
          <div class="listener-tools">
            <div class="chips-group" role="group" aria-label="监听分类筛选">
              <button
                v-for="tab in listenerTabs"
                :key="tab.key"
                type="button"
                class="chip-btn"
                :class="{ active: listenerFilter === tab.key }"
                :aria-pressed="listenerFilter === tab.key"
                @click="listenerFilter = tab.key"
              >
                {{ tab.label }}
              </button>
            </div>
            <select v-model="listenerProto" class="form-select proto-select" aria-label="监听协议">
              <option value="all">全部协议</option>
              <option value="TCP">TCP</option>
              <option value="UDP">UDP</option>
            </select>
          </div>
        </div>

        <div v-if="filteredListeners.length" class="listener-list" role="list" aria-label="GateHome 监听端口">
          <div
            v-for="row in filteredListeners"
            :key="row.port + '/' + row.protocol + '/' + row.ip + '/' + row.purpose"
            class="listener-row"
            role="listitem"
          >
            <div class="listener-left">
              <span class="port-chip mono">:{{ row.port }}</span>
              <span class="badge mono" :class="row.protocol === 'UDP' ? 'badge-brand' : 'badge-ok'">{{ row.protocol }}</span>
              <div class="listener-meta">
                <div class="listener-purpose">{{ row.purpose }}</div>
                <div class="listener-bind mono">绑定 {{ row.ip }}</div>
              </div>
            </div>
            <div class="listener-services">
              <template v-if="row.services?.length">
                <span v-for="svc in row.services" :key="svc" class="service-tag">{{ svc }}</span>
              </template>
              <span v-else class="service-tag muted-tag">—</span>
            </div>
          </div>
        </div>
        <p v-else class="report-empty">暂无符合条件的监听端口</p>
      </SpotlightCard>

      <SpotlightCard class="report-panel">
        <div class="runtime-section-head inline-head">
          <h2>管理员登录</h2>
          <RouterLink class="btn btn-secondary btn-sm" to="/settings/security">管理会话</RouterLink>
        </div>
        <p v-if="sessionError" class="report-error" role="alert">{{ sessionError }}</p>
        <div v-if="sessions?.sessions?.length" class="session-list" aria-label="管理员登录会话">
          <div
            v-for="session in sessions.sessions"
            :key="session.id"
            class="session-card"
            :class="{ current: sessions.current === session.id }"
          >
            <div class="session-top">
              <strong>{{ deviceName(session.device) }}</strong>
              <span class="badge" :class="sessions.current === session.id ? 'badge-ok' : 'badge-neutral'">
                {{ sessions.current === session.id ? '当前会话' : '已登录' }}
              </span>
            </div>
            <div class="session-meta">
              <span>{{ session.method === 'passkey' ? '通行密钥' : '密码' }}</span>
              <span class="mono">最近活动 {{ reportDate(session.last_seen) }}</span>
            </div>
            <div class="session-started mono">登录于 {{ reportDate(session.started) }}</div>
          </div>
        </div>
        <p v-else-if="sessions" class="report-empty">暂无活动会话</p>
      </SpotlightCard>
    </div>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ChevronRight, Search } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import RouteHealthHistory from '@/components/RouteHealthHistory.vue'
import { adminApi } from '@/api/admin'
import { deviceName, formatByteRate, formatBytes, reportDate } from '@/api/reports'
import '@/styles/reports.css'

const data = ref(null)
const sessions = ref(null)
const runtimeError = ref('')
const sessionError = ref('')
const loading = ref(false)
const pollingPaused = ref(false)
const openRoute = ref('')
const routeFilter = ref('all')
const routeQuery = ref('')
const listenerFilter = ref('all')
const listenerProto = ref('all')
const routesSectionRef = ref(null)
const controller = new AbortController()

const resources = computed(() => data.value?.resources || {})
const sampledClock = computed(() => {
  const raw = data.value?.sampled_at
  if (!raw) return '—'
  return new Date(raw).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })
})

const hostSummary = computed(() => {
  if (!data.value) return ''
  const parts = []
  if (resources.value.platform) parts.push(resources.value.platform)
  if (resources.value.cpus) parts.push(`${resources.value.cpus} 个逻辑 CPU`)
  if (resources.value.interface && !resources.value.network_error) {
    const rx = resources.value.rx_rate != null ? `↓ ${formatByteRate(resources.value.rx_rate)}` : ''
    const tx = resources.value.tx_rate != null ? `↑ ${formatByteRate(resources.value.tx_rate)}` : ''
    const net = [rx, tx].filter(Boolean).join(' · ')
    parts.push(`网卡 ${resources.value.interface}${net ? ' ' + net : ''}`)
  }
  return parts.join(' · ')
})

const memoryPercent = computed(() => {
  const res = resources.value
  return !res.memory_error && res.memory_total > 0 ? (100 * res.memory_used) / res.memory_total : null
})
const cpuMeta = computed(() => resources.value.cpus ? `${resources.value.cpus} 核` : '')
const memoryMeta = computed(() =>
  memoryPercent.value == null ? '' : `${formatBytes(resources.value.memory_used)} / ${formatBytes(resources.value.memory_total)}`
)
const percentText = value => value == null ? '—' : value.toFixed(1) + '%'
const progressTone = value => value >= 90 ? 'bad' : value >= 70 ? 'warn' : ''

const listenCount = protocol => data.value?.listeners.filter(row => row.protocol === protocol).length ?? 0
const uniqueListenerPorts = computed(() => new Set((data.value?.listeners || []).map(row => row.port)).size)

const isFailingRoute = route => Boolean(route.down || (route.health && !route.health.healthy))
const isHealthyRoute = route => Boolean(route.health?.healthy)
const monitoredRoutesCount = computed(() =>
  (data.value?.routes || []).filter(route => route.health || route.state === 'pending').length
)
const healthyRoutesCount = computed(() => (data.value?.routes || []).filter(isHealthyRoute).length)
const failingRoutesCount = computed(() => (data.value?.routes || []).filter(isFailingRoute).length)
const pendingRoutesCount = computed(() =>
  Math.max(0, monitoredRoutesCount.value - healthyRoutesCount.value - failingRoutesCount.value)
)

const healthSchedule = computed(() =>
  data.value?.health.enabled ? `检测间隔 ${data.value.health.interval} 秒` : '健康检查已停用'
)

const routeTabs = computed(() => {
  const all = data.value?.routes || []
  const failing = all.filter(isFailingRoute).length
  const healthy = all.filter(isHealthyRoute).length
  const inactive = all.length - failing - healthy
  return [
    { key: 'all', label: '全部', count: all.length },
    { key: 'failing', label: '异常', count: failing },
    { key: 'healthy', label: '正常', count: healthy },
    { key: 'inactive', label: '未绑定/停用', count: inactive }
  ]
})

const filteredRoutes = computed(() => {
  const q = routeQuery.value.trim().toLowerCase()
  return (data.value?.routes || []).filter(route => {
    if (routeFilter.value === 'failing' && !isFailingRoute(route)) return false
    if (routeFilter.value === 'healthy' && !isHealthyRoute(route)) return false
    if (routeFilter.value === 'inactive' && (isFailingRoute(route) || isHealthyRoute(route))) return false
    if (!q) return true
    return (
      String(route.name || '').toLowerCase().includes(q) ||
      String(route.host || '').toLowerCase().includes(q) ||
      String(route.upstream || '').toLowerCase().includes(q)
    )
  })
})

function listenerCategory(row) {
  const purpose = String(row.purpose || '')
  if (purpose.startsWith('管理台') || purpose.startsWith('独立入口') || purpose.startsWith('内部桥接')) return 'system'
  if (purpose === '端口转发') return 'forward'
  return 'proxy'
}

const listenerTabs = [
  { key: 'all', label: '全部' },
  { key: 'proxy', label: '反代组' },
  { key: 'forward', label: '端口转发' },
  { key: 'system', label: '管理' }
]

const filteredListeners = computed(() =>
  (data.value?.listeners || []).filter(row => {
    if (listenerFilter.value !== 'all' && listenerCategory(row) !== listenerFilter.value) return false
    if (listenerProto.value !== 'all' && row.protocol !== listenerProto.value) return false
    return true
  })
)

function routeDotTone(route) {
  if (isFailingRoute(route)) return 'bad'
  if (isHealthyRoute(route)) return 'ok'
  return 'neutral'
}

function routeHealthTone(route) {
  if (isFailingRoute(route)) return 'runtime-bad'
  if (isHealthyRoute(route)) return 'runtime-ok'
  return ''
}

function healthLabel(route) {
  if (route.health) {
    if (route.health.healthy) return route.health.label + ' · ' + route.health.duration_ms + ' ms'
    return route.health.label + (route.down ? ' · 连续失败' : '')
  }
  return { disabled: '已停用', unbound: '未绑定运行入口', unchecked: '检测已停用', pending: '等待检测' }[route.state]
}

function toggleRoute(id) {
  openRoute.value = openRoute.value === id ? '' : id
}

function focusFailingRoutes() {
  routeFilter.value = 'failing'
  routesSectionRef.value?.$el?.scrollIntoView?.({ behavior: 'smooth', block: 'start' })
}

function togglePolling() {
  pollingPaused.value = !pollingPaused.value
  if (!pollingPaused.value) load(true)
}

let timer
async function load(force = false) {
  if (loading.value || document.hidden || (!force && pollingPaused.value)) return
  loading.value = true
  const signal = controller.signal
  try {
    const [runtime, account] = await Promise.allSettled([adminApi.runtime(signal), adminApi.sessions(signal)])
    if (signal.aborted) return
    if (runtime.status === 'fulfilled') { data.value = runtime.value; runtimeError.value = '' }
    else runtimeError.value = runtime.reason.message
    if (account.status === 'fulfilled') { sessions.value = account.value; sessionError.value = '' }
    else sessionError.value = account.reason.message
  } finally {
    loading.value = false
  }
}

function onVisibilityChange() {
  if (!document.hidden && !pollingPaused.value) load()
}

onMounted(() => {
  load(true)
  timer = setInterval(() => load(), 10000)
  document.addEventListener('visibilitychange', onVisibilityChange)
})
onBeforeUnmount(() => {
  controller.abort()
  clearInterval(timer)
  document.removeEventListener('visibilitychange', onVisibilityChange)
})
</script>

<style scoped>
.page-head { display: flex; align-items: flex-start; justify-content: space-between; flex-wrap: wrap; gap: 12px; min-width: 0; }
.page-head-title { min-width: 0; flex: 1; }
.title-line { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.title-line h1 { font-size: 20px; font-weight: 700; color: var(--ink); margin: 0; line-height: 1.3; }
.page-sub { font-size: 12px; color: var(--muted); margin-top: 4px; overflow-wrap: anywhere; }
.runtime-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; flex-shrink: 0; }
.mono { font-family: var(--mono); font-variant-numeric: tabular-nums; }

.kpi-grid-4 { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 14px; }
.kpi-card { padding: 16px; }
.kpi-card :deep(.card-inner) { display: flex; flex-direction: column; justify-content: space-between; gap: 8px; }
.kpi-card-bad { border-color: rgba(244, 63, 94, 0.38) !important; }
.kpi-top { display: flex; align-items: center; justify-content: space-between; gap: 6px; font-size: 12px; color: var(--muted); }
.kpi-val { font-size: 24px; font-weight: 700; line-height: 1.15; color: var(--ink); }
.kpi-foot { font-size: 11.5px; color: var(--muted); min-height: 18px; margin: 0; }
.error-text { color: var(--bad); }
.kpi-filter-btn {
  border: 0; background: transparent; padding: 0; font-size: 11.5px; font-weight: 600;
  color: var(--bad); cursor: pointer; text-align: left;
}
.kpi-filter-btn:hover { text-decoration: underline; }

.gh-progress { margin: 2px 0 0 !important; }
.gh-progress.warn::-webkit-progress-value { background: var(--warn) !important; }
.gh-progress.bad::-webkit-progress-value { background: var(--bad) !important; }

.runtime-section-head {
  display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap;
  gap: 10px; margin-bottom: 14px;
}
.runtime-section-head h2 { margin: 0; font-size: 15px; font-weight: 700; color: var(--ink); }
.section-title-wrap { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }

.route-tools, .listener-tools { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.chips-group { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; }
.chip-btn {
  display: inline-flex; align-items: center; gap: 6px; padding: 5px 11px; min-height: 32px;
  border-radius: var(--radius-pill); background: var(--strip-bg);
  border: 1px solid var(--line); color: var(--muted); font-size: 12px; font-weight: 500;
}
.chip-btn.active {
  background: rgba(99, 102, 241, 0.18); border-color: rgba(99, 102, 241, 0.42);
  color: #fff; font-weight: 600;
}
.theme-light .chip-btn.active { background: rgba(99, 102, 241, 0.12); color: var(--brand); }
.chip-count {
  font-family: var(--mono); font-size: 10.5px; background: rgba(148, 163, 184, 0.16);
  padding: 1px 6px; border-radius: 999px;
}

.route-search {
  display: flex; align-items: center; gap: 8px; padding: 0 10px; height: 34px;
  background: var(--input-bg); border: 1px solid var(--line); border-radius: var(--radius-sm);
  width: 200px; max-width: 100%;
}
.route-search .search-icon { color: var(--faint); flex-shrink: 0; }
.route-search .search-input {
  border: 0; background: transparent; color: var(--ink); font-size: 12.5px;
  flex: 1; min-width: 0; width: 100%; height: 100%; padding: 0;
}

.route-list { display: grid; grid-template-columns: minmax(0, 1fr); gap: 10px; min-width: 0; }
.route-item {
  border: 1px solid var(--line); border-radius: var(--radius-md); background: var(--strip-bg);
  overflow: hidden; transition: border-color 0.2s ease; min-width: 0;
}
.route-item.is-failing { border-color: rgba(244, 63, 94, 0.4); }
.route-row-btn {
  width: 100%; padding: 13px 14px; border: 0; background: transparent; color: inherit;
  text-align: left; display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 8px 14px; align-items: center;
}
.route-row-btn:hover { background: rgba(148, 163, 184, 0.05); }
.route-info { min-width: 0; }
.route-id-line { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; min-width: 0; }
.route-name { font-size: 13.5px; font-weight: 650; color: var(--ink); }
.route-host { font-size: 12px; color: var(--muted); overflow-wrap: anywhere; }
.route-path-line {
  display: flex; align-items: center; flex-wrap: wrap; gap: 6px;
  margin-top: 6px; padding-left: 15px; font-size: 11.5px;
}
.entry-pill {
  padding: 2px 7px; border-radius: 5px; border: 1px solid var(--line);
  color: var(--ink-secondary); background: var(--card-bg);
}
.no-entry, .path-arrow { color: var(--faint); }
.upstream-pill {
  padding: 2px 8px; border-radius: 5px; background: rgba(99, 102, 241, 0.11);
  color: var(--brand-light); overflow-wrap: anywhere; max-width: 100%;
}
.theme-light .upstream-pill { color: var(--brand); background: rgba(99, 102, 241, 0.08); }

.route-status-col { display: flex; align-items: center; gap: 10px; flex-shrink: 0; }
.route-health-text { text-align: right; font-size: 12px; color: var(--muted); }
.route-health-text small { display: block; color: var(--faint); font-size: 11px; margin-top: 2px; }
.runtime-ok { color: var(--ok); }
.runtime-bad { color: var(--bad) !important; }
.route-chevron { color: var(--brand-light); transition: transform 0.18s ease; flex-shrink: 0; }
.route-chevron.open { transform: rotate(90deg); }

.runtime-split { display: grid; grid-template-columns: minmax(0, 1.65fr) minmax(0, 1fr); gap: 16px; align-items: start; }
.proto-select {
  width: 104px !important; height: 32px !important; min-height: 32px !important;
  line-height: 30px !important; font-size: 12px !important; padding: 0 28px 0 10px !important;
}

.listener-list {
  border: 1px solid var(--line); border-radius: var(--radius-md);
  overflow: hidden; background: var(--strip-bg);
}
.listener-row {
  padding: 11px 14px; border-bottom: 1px solid var(--line);
  display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px;
}
.listener-row:last-child { border-bottom: 0; }
.listener-left { display: flex; align-items: center; gap: 10px; min-width: 0; }
.port-chip {
  padding: 3px 8px; border-radius: 6px; font-size: 12px; font-weight: 700;
  background: rgba(99, 102, 241, 0.12); color: var(--brand-light); border: 1px solid rgba(99, 102, 241, 0.25);
}
.theme-light .port-chip { color: var(--brand); }
.listener-meta { min-width: 0; }
.listener-purpose { font-size: 12.5px; font-weight: 600; color: var(--ink); overflow-wrap: anywhere; }
.listener-bind { font-size: 11px; color: var(--faint); }
.listener-services { display: flex; flex-wrap: wrap; gap: 5px; justify-content: flex-end; }
.service-tag {
  padding: 2px 8px; border-radius: 6px; font-size: 11.5px;
  background: var(--card-bg); border: 1px solid var(--line); color: var(--ink-secondary);
}
.muted-tag { color: var(--faint); }

.session-list { display: grid; grid-template-columns: minmax(0, 1fr); gap: 10px; min-width: 0; }
.session-card {
  padding: 12px; border-radius: var(--radius-md); background: var(--strip-bg);
  border: 1px solid var(--line); display: grid; grid-template-columns: minmax(0, 1fr); gap: 6px; min-width: 0;
}
.session-card.current { border-color: rgba(16, 185, 129, 0.38); }
.session-top { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.session-top strong { font-size: 13px; color: var(--ink); overflow-wrap: anywhere; }
.session-meta { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 6px; font-size: 11.5px; color: var(--muted); }
.session-started { font-size: 11px; color: var(--faint); }

.report-warning { margin: 0; }

@media (max-width: 1080px) {
  .kpi-grid-4 { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
  .runtime-split { grid-template-columns: minmax(0, 1fr); }
}

@media (max-width: 650px) {
  .page-head { flex-direction: column; align-items: stretch; gap: 10px; }
  .runtime-actions { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; width: 100%; }
  .runtime-actions .btn-primary { grid-column: 1 / -1; }
  .runtime-actions .btn { width: 100%; min-height: 40px; }

  .kpi-card { padding: 12px; }
  .kpi-val { font-size: 20px; }
  .kpi-top { font-size: 11px; }

  .runtime-section-head:not(.inline-head) { flex-direction: column; align-items: stretch; }
  .runtime-section-head.inline-head { flex-direction: row; align-items: center; justify-content: space-between; }
  .route-tools, .listener-tools { flex-direction: column; align-items: stretch; }
  .route-search { width: 100%; }
  .proto-select { width: 100% !important; }

  .route-row-btn { grid-template-columns: minmax(0, 1fr); padding: 12px; gap: 8px; }
  .route-path-line { padding-left: 0; }
  .route-status-col { justify-content: space-between; border-top: 1px dashed var(--line); padding-top: 8px; }
  .route-health-text { text-align: left; }

  .listener-row { flex-direction: column; align-items: flex-start; padding: 12px; gap: 8px; }
  .listener-services { justify-content: flex-start; }
}
</style>
