<template>
  <div class="report-page runtime-page">
    <header class="report-heading">
      <h1>运行状态</h1>
      <div class="report-actions">
        <button type="button" class="btn btn-secondary" :disabled="loading" @click="load">刷新数据</button>
        <button type="button" class="btn btn-primary" @click="openCenter(true)">故障推送</button>
      </div>
    </header>
    <p v-if="runtimeError" class="report-error" role="alert">{{ runtimeError }}</p>
    <p v-else-if="data" class="report-note">最近采样 <time :datetime="data.sampled_at">{{ reportDate(data.sampled_at) }}</time></p>
    <p v-else class="report-note" role="status">正在读取运行状态…</p>
    <div class="report-metrics">
      <SpotlightCard v-for="metric in metrics" :key="metric.label" class="report-metric">
        <h2 class="metric-label">{{ metric.label }}</h2>
        <div class="metric-value">{{ metric.value }}</div>
        <progress v-if="metric.percent != null" :value="metric.percent" max="100" :aria-label="metric.label" />
        <p v-if="metric.detail" class="report-note">{{ metric.detail }}</p>
      </SpotlightCard>
    </div>

    <SpotlightCard v-if="data" class="report-panel">
      <div class="report-heading runtime-section">
        <h2>GateHome 监听端口</h2><span class="report-note">全部 {{ data.listeners.length }} 条</span>
      </div>
      <p v-if="data.restart_required" class="report-warning">监听配置待重启。列表显示当前运行中的端口。</p>
      <div class="report-table-wrap" tabindex="0" aria-label="GateHome 监听端口">
        <table class="report-table">
          <thead><tr><th scope="col">本机端口</th><th scope="col">协议</th><th scope="col">绑定地址</th>
            <th scope="col">用途</th><th scope="col">关联服务</th><th scope="col">监听状态</th></tr></thead>
          <tbody><tr v-for="row in data.listeners" :key="row.port + '/' + row.protocol + '/' + row.ip">
            <th scope="row"><code>{{ row.port }}</code></th><td>{{ row.protocol }}</td><td><code>{{ row.ip }}</code></td>
            <td>{{ row.purpose }}</td><td>{{ row.services?.join('、') || '—' }}</td><td><span class="runtime-ok">监听中</span></td>
          </tr></tbody>
        </table>
      </div>
    </SpotlightCard>

    <SpotlightCard v-if="data" class="report-panel">
      <div class="report-heading runtime-section">
        <h2>反代上游连通性</h2><span class="report-note">{{ healthSchedule }}</span>
      </div>
      <div v-if="data.routes.length" class="report-table-wrap" tabindex="0" aria-label="反代上游连通性">
        <table class="report-table route-table">
          <thead><tr><th scope="col">反代服务</th><th scope="col">本机入口</th><th scope="col">上游目标</th><th scope="col">检测结果</th></tr></thead>
          <tbody>
            <template v-for="route in data.routes" :key="route.id">
              <tr>
                <th scope="row">
                  <button type="button" class="history-toggle" :aria-expanded="openRoute === route.id"
                    :aria-controls="'route-history-' + route.id" @click="toggleRoute(route.id)">
                    <ChevronRight :size="14" aria-hidden="true" />{{ route.name }}
                  </button>
                  <small v-if="route.host !== route.name">{{ route.host }}</small>
                </th>
                <td>{{ route.entries?.join('、') || '—' }}</td><td><code>{{ route.upstream }}</code></td>
                <td :class="route.health ? route.health.healthy ? 'runtime-ok' : 'runtime-bad' : ''">
                  {{ healthLabel(route) }}
                  <small v-if="route.health">{{ reportDate(route.health.checked_at) }}</small>
                </td>
              </tr>
              <tr v-if="openRoute === route.id" :id="'route-history-' + route.id">
                <td colspan="4" class="history-cell"><RouteHealthHistory :key="route.id" :route-id="route.id" /></td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
      <p v-else class="report-empty">暂无反代服务</p>
    </SpotlightCard>

    <SpotlightCard class="report-panel">
      <div class="report-heading runtime-section">
        <h2>管理员登录</h2>
        <RouterLink class="btn btn-secondary btn-sm" to="/settings/security">管理会话</RouterLink>
      </div>
      <p v-if="sessionError" class="report-error" role="alert">{{ sessionError }}</p>
      <div v-if="sessions" class="report-table-wrap" tabindex="0" aria-label="管理员登录会话">
        <table class="report-table">
          <thead><tr><th scope="col">设备 / 浏览器</th><th scope="col">验证方式</th><th scope="col">登录时间</th>
            <th scope="col">最近活动</th><th scope="col">会话状态</th></tr></thead>
          <tbody><tr v-for="session in sessions.sessions" :key="session.id">
            <th scope="row">{{ deviceName(session.device) }}</th><td>{{ session.method === 'passkey' ? '通行密钥' : '密码' }}</td>
            <td>{{ reportDate(session.started) }}</td><td>{{ reportDate(session.last_seen) }}</td>
            <td>{{ sessions.current === session.id ? '当前会话' : '已登录' }}</td>
          </tr></tbody>
        </table>
      </div>
    </SpotlightCard>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ChevronRight } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import RouteHealthHistory from '@/components/RouteHealthHistory.vue'
import { adminApi } from '@/api/admin'
import { deviceName, formatBytes, reportDate } from '@/api/reports'
import { useToast } from '@/composables/useToast'
import '@/styles/reports.css'

const { openCenter } = useToast()
const data = ref(null), sessions = ref(null), runtimeError = ref(''), sessionError = ref('')
const loading = ref(false), openRoute = ref('')
const controller = new AbortController()
const listenCount = protocol => data.value?.listeners.filter(row => row.protocol === protocol).length ?? 0
const healthSchedule = computed(() => data.value?.health.enabled ? `检测间隔 ${data.value.health.interval} 秒` : '健康检查已停用')
const percent = value => value == null ? '—' : value.toFixed(1) + '%'
const metrics = computed(() => {
  const resources = data.value?.resources || {}
  const cpu = resources.cpu_percent
  const memory = !resources.memory_error && resources.memory_total > 0 ? 100 * resources.memory_used / resources.memory_total : null
  const listeners = data.value?.listeners || []
  const monitored = (data.value?.routes || []).filter(route => route.health || route.state === 'pending')
  const healthy = monitored.filter(route => route.health?.healthy).length
  const failing = monitored.filter(route => route.health && !route.health.healthy).length
  const pending = monitored.length - healthy - failing
  const detail = [failing && `${failing} 个检测失败`, pending && `${pending} 个等待检测`].filter(Boolean).join(' · ')
  return [
    { label: 'CPU 占用', value: percent(cpu), percent: cpu, detail: resources.cpu_error || (resources.cpus ? `${resources.cpus} 个逻辑 CPU` : '') },
    { label: '内存占用', value: percent(memory), percent: memory, detail: resources.memory_error || (memory == null ? '' : `${formatBytes(resources.memory_used)} / ${formatBytes(resources.memory_total)}`) },
    { label: '监听端口', value: data.value ? new Set(listeners.map(row => row.port)).size : '—', detail: data.value ? `${listeners.length} 条监听 · TCP ${listenCount('TCP')} / UDP ${listenCount('UDP')}` : '' },
    { label: '反代健康', value: monitored.length ? `${healthy} / ${monitored.length}` : '—', detail }
  ]
})

function healthLabel(route) {
  if (route.health) {
    if (route.health.healthy) return route.health.label + ' · ' + route.health.duration_ms + ' ms'
    return route.health.label + (route.down ? ' · 连续失败' : '')
  }
  return { disabled: '已停用', unbound: '未绑定运行入口', unchecked: '检测已停用', pending: '等待检测' }[route.state]
}

function toggleRoute(id) { openRoute.value = openRoute.value === id ? '' : id }

let timer
async function load() {
  if (loading.value || document.hidden) return
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
onMounted(() => {
  load()
  timer = setInterval(load, 10000)
  document.addEventListener('visibilitychange', load)
})
onBeforeUnmount(() => {
  controller.abort()
  clearInterval(timer)
  document.removeEventListener('visibilitychange', load)
})
</script>

<style scoped>
.runtime-section { margin-bottom: 14px; }
.runtime-section h2 { margin-bottom: 0; }
.report-metric h2 { margin: 0; }
.report-metric progress { display: block; width: 100%; height: 7px; margin-top: 12px; border: 0; appearance: none; }
.report-metric progress::-webkit-progress-bar { background: var(--line); border-radius: 8px; }
.report-metric progress::-webkit-progress-value { background: var(--brand-gradient, var(--brand)); border-radius: 8px; }
.report-metric progress::-moz-progress-bar { background: var(--brand); border-radius: 8px; }
.report-metric p { margin-top: 8px; }
.report-table th[scope="row"] { background: transparent; color: var(--ink); border-top: 1px solid var(--line); white-space: normal; }
.report-table td { vertical-align: middle; }
.route-table { min-width: 680px; }
.route-table .history-cell { padding: 0; max-width: 0; }
.history-toggle { display: inline-flex; align-items: center; gap: 6px; font-weight: 650; text-align: start; }
.history-toggle svg { flex: none; color: var(--brand); }
.history-toggle[aria-expanded="true"] svg { transform: rotate(90deg); }
.runtime-ok { color: var(--ok); }
.runtime-bad { color: var(--bad) !important; }
.report-warning { margin: 0 0 12px; }
</style>
