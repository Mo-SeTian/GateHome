<template>
  <div class="connections-wrap">
    <SpotlightCard class="report-panel conn-controls-card">
      <div class="conn-top-row">
        <div class="conn-title-group">
          <h2>连接</h2>
          <div class="conn-meta-line">
            <span class="meta-pill">{{ activeList.length }} 活跃</span>
            <span class="meta-pill">{{ closedList.length }} 已关闭</span>
            <span class="badge" :class="!running ? 'badge-neutral' : paused ? 'badge-warn' : 'badge-ok'">
              <span class="status-dot" :class="!running ? 'neutral' : paused ? 'warn' : 'ok'" />
              {{ !running ? '未运行' : paused ? '已暂停' : '实时' }}
            </span>
          </div>
          <div v-if="running" class="conn-totals mono-text">
            <span>↓ {{ formatBytes(downloadTotal) }}</span>
            <span>↑ {{ formatBytes(uploadTotal) }}</span>
          </div>
        </div>

        <div class="report-actions conn-action-btns">
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="busy || !running"
            title="立即刷新连接"
            @click="refreshNow"
          >
            <RefreshCw :size="14" /> 刷新
          </button>
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="!running"
            :aria-pressed="paused"
            @click="togglePause"
          >
            <Play v-if="paused" :size="14" />
            <Pause v-else :size="14" />
            {{ paused ? '继续' : '暂停' }}
          </button>
          <button
            v-if="subTab === 'active'"
            type="button"
            class="btn btn-danger btn-sm"
            :disabled="closing || !running || !activeList.length"
            @click="closeAll"
          >
            <X :size="14" /> 关闭所有活跃连接
          </button>
          <button
            v-else
            type="button"
            class="btn btn-secondary btn-sm text-bad"
            :disabled="!closedList.length"
            @click="clearClosed"
          >
            <Trash2 :size="14" /> 清空记录
          </button>
        </div>
      </div>

      <div class="conn-subtabs" role="tablist" aria-label="连接状态分类">
        <button
          type="button"
          role="tab"
          :aria-selected="subTab === 'active'"
          class="conn-subtab"
          :class="{ active: subTab === 'active' }"
          @click="subTab = 'active'"
        >
          活跃 ({{ activeList.length }})
        </button>
        <button
          type="button"
          role="tab"
          :aria-selected="subTab === 'closed'"
          class="conn-subtab"
          :class="{ active: subTab === 'closed' }"
          @click="subTab = 'closed'"
        >
          已关闭 ({{ closedList.length }})
        </button>
      </div>

      <div class="conn-filters-grid">
        <select v-model="networkFilter" class="form-select conn-filter-select" aria-label="按协议筛选">
          <option value="">全部协议</option>
          <option value="tcp">TCP</option>
          <option value="udp">UDP</option>
        </select>

        <select v-model="nodeFilter" class="form-select conn-filter-select" aria-label="按最终节点筛选">
          <option value="">全部节点</option>
          <option v-for="node in nodeOptions" :key="node" :value="node">{{ node }}</option>
        </select>

        <div class="conn-search-wrap">
          <Search :size="14" class="conn-search-icon" aria-hidden="true" />
          <input
            v-model.trim="searchQuery"
            type="search"
            class="form-input conn-search-input"
            aria-label="搜索连接"
            placeholder="搜索 IP / 网站 / 规则 / 节点，多个关键词用空格分隔"
          />
        </div>
      </div>
    </SpotlightCard>

    <p v-if="fetchError" class="report-warning" role="status">{{ fetchError }}</p>

    <div v-if="!running && subTab === 'active'" class="report-empty conn-empty-card">
      Mihomo 内核未运行，请先点击右上角「启动」后再查看实时连接。
    </div>

    <div v-else-if="filteredConnections.length" class="conn-list">
      <SpotlightCard v-for="item in filteredConnections" :key="item.uid" class="conn-card">
        <div class="conn-card-head">
          <div class="conn-target-row">
            <strong class="conn-host mono-text">{{ formatTarget(item.conn) }}</strong>
            <span class="badge conn-net-badge" :class="networkBadgeClass(item.conn.metadata?.network)">
              {{ (item.conn.metadata?.network || 'tcp').toUpperCase() }}
            </span>
            <span v-if="item.conn.metadata?.type" class="badge badge-neutral">
              {{ item.conn.metadata.type }}
            </span>
          </div>
          <div class="conn-time-col">
            <span class="conn-rel-time">{{ formatRelativeStart(item.conn.start) }}</span>
            <span class="conn-duration mono-text">{{ formatDuration(item.conn.start, item.closedAt) }}</span>
          </div>
        </div>

        <div class="conn-path mono-text">
          <span>{{ formatEndpoint(item.conn.metadata?.sourceIP, item.conn.metadata?.sourcePort) }}</span>
          <ArrowRight :size="12" class="conn-arrow" aria-hidden="true" />
          <span>{{ formatEndpoint(item.conn.metadata?.destinationIP, item.conn.metadata?.destinationPort) }}</span>
        </div>

        <div class="conn-metrics-row">
          <span class="conn-speed down mono-text">
            <ArrowDown :size="12" aria-hidden="true" />
            <template v-if="subTab === 'active'">{{ formatSpeed(item.dlSpeed) }} · </template>
            {{ formatBytes(item.conn.download) }}
          </span>
          <span class="conn-speed up mono-text">
            <ArrowUp :size="12" aria-hidden="true" />
            <template v-if="subTab === 'active'">{{ formatSpeed(item.ulSpeed) }} · </template>
            {{ formatBytes(item.conn.upload) }}
          </span>
          <span class="conn-rule-pill" :title="formatRuleText(item.conn)">
            {{ formatRuleText(item.conn) }}
          </span>
        </div>

        <div class="conn-card-foot">
          <div class="conn-chains-wrap">
            <span class="conn-chain-label">代理链</span>
            <span v-if="chainRoute(item.conn).length > 1" class="conn-chain-prefix">
              {{ chainRoute(item.conn).slice(0, -1).join(' → ') }} →
            </span>
            <span class="conn-chain-final badge badge-brand">
              {{ finalNode(item.conn) }}
            </span>
          </div>
          <button
            v-if="subTab === 'active'"
            type="button"
            class="btn btn-ghost btn-sm text-bad conn-close-btn"
            :disabled="closingId === item.conn.id"
            :aria-label="`关闭连接 ${formatTarget(item.conn)}`"
            title="断开此连接"
            @click="closeOne(item.conn)"
          >
            <X :size="14" />
          </button>
        </div>
      </SpotlightCard>
    </div>

    <div v-else class="report-empty conn-empty-card">
      {{ currentSourceList.length ? '无符合筛选条件的连接' : subTab === 'active' ? '当前暂无活跃连接' : '暂无已关闭连接记录' }}
    </div>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ArrowDown, ArrowRight, ArrowUp, Pause, Play, RefreshCw, Search, Trash2, X } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { adminApi } from '@/api/admin'
import { useToast } from '@/composables/useToast'

const props = defineProps({
  active: { type: Boolean, default: false },
  running: { type: Boolean, default: false }
})

const toast = useToast()
const subTab = ref('active')
const paused = ref(false)
const busy = ref(false)
const closing = ref(false)
const closingId = ref('')
const fetchError = ref('')

const networkFilter = ref('')
const nodeFilter = ref('')
const searchQuery = ref('')

const downloadTotal = ref(0)
const uploadTotal = ref(0)
const activeList = ref([])
const closedList = ref([])
const nowTick = ref(Date.now())

let prevById = new Map()
let pollTimer = null
let tickTimer = null
let activeAbort = null
let closedSeq = 0

function finalNode(conn) {
  const chains = conn?.chains
  if (Array.isArray(chains) && chains.length > 0) {
    return chains[0]
  }
  return 'DIRECT'
}

function chainRoute(conn) {
  const chains = Array.isArray(conn?.chains) ? conn.chains : []
  if (!chains.length) return ['DIRECT']
  return [...chains].reverse()
}

function formatEndpoint(ip, port) {
  const addr = ip || '—'
  if (!port) return addr
  return addr.includes(':') ? `[${addr}]:${port}` : `${addr}:${port}`
}

function formatTarget(conn) {
  const meta = conn?.metadata || {}
  const host = meta.host || meta.destinationIP || '—'
  const port = meta.destinationPort
  if (!port) return host
  return host.includes(':') && !host.startsWith('[') ? `[${host}]:${port}` : `${host}:${port}`
}

function formatRuleText(conn) {
  const rule = conn?.rule || 'MATCH'
  return conn?.rulePayload ? `${rule}: ${conn.rulePayload}` : rule
}

function networkBadgeClass(network) {
  return String(network || '').toLowerCase() === 'udp' ? 'badge-warn' : 'badge-info'
}

function formatBytes(bytes) {
  const n = Number(bytes) || 0
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(2)} MB`
  return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GB`
}

function formatSpeed(bps) {
  const n = Math.max(0, Number(bps) || 0)
  if (n < 1024) return `${Math.round(n)} B/s`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB/s`
  return `${(n / (1024 * 1024)).toFixed(2)} MB/s`
}

function formatRelativeStart(startISO) {
  if (!startISO) return '—'
  const ts = Date.parse(startISO)
  if (Number.isNaN(ts)) return '—'
  const sec = Math.max(0, Math.floor((nowTick.value - ts) / 1000))
  if (sec < 60) return `${sec} 秒前`
  if (sec < 3600) return `${Math.floor(sec / 60)} 分钟前`
  if (sec < 86400) return `${Math.floor(sec / 3600)} 小时前`
  return `${Math.floor(sec / 86400)} 天前`
}

function formatDuration(startISO, endTs = null) {
  if (!startISO) return '—'
  const ts = Date.parse(startISO)
  if (Number.isNaN(ts)) return '—'
  const end = endTs || nowTick.value
  const totalSec = Math.max(0, Math.floor((end - ts) / 1000))
  if (totalSec < 60) return `${totalSec}s`
  const min = Math.floor(totalSec / 60)
  const sec = totalSec % 60
  if (min < 60) return `${min}m ${sec}s`
  const hr = Math.floor(min / 60)
  return `${hr}h ${min % 60}m`
}

const nodeOptions = computed(() => {
  const set = new Set()
  for (const entry of activeList.value) {
    set.add(finalNode(entry.conn))
  }
  for (const entry of closedList.value) {
    set.add(finalNode(entry.conn))
  }
  return [...set].sort((a, b) => a.localeCompare(b))
})

const currentSourceList = computed(() => (subTab.value === 'active' ? activeList.value : closedList.value))

const filteredConnections = computed(() => {
  const net = networkFilter.value.toLowerCase()
  const node = nodeFilter.value
  const terms = searchQuery.value.toLowerCase().split(/\s+/).filter(Boolean)

  return currentSourceList.value.filter((entry) => {
    const c = entry.conn
    const meta = c.metadata || {}
    if (net && String(meta.network || '').toLowerCase() !== net) return false
    if (node && finalNode(c) !== node) return false
    if (!terms.length) return true
    const haystack = [
      meta.sourceIP,
      meta.sourcePort,
      meta.destinationIP,
      meta.destinationPort,
      meta.host,
      meta.network,
      meta.type,
      meta.processPath,
      c.rule,
      c.rulePayload,
      ...(Array.isArray(c.chains) ? c.chains : [])
    ]
      .filter(Boolean)
      .join(' ')
      .toLowerCase()
    return terms.every((term) => haystack.includes(term))
  })
})

async function fetchSnapshot(silent = true) {
  if (!props.running) {
    if (prevById.size > 0) {
      archiveDisappeared(new Set(), Date.now())
      prevById.clear()
      activeList.value = []
    }
    return
  }
  if (activeAbort) {
    activeAbort.abort()
  }
  activeAbort = new AbortController()
  if (!silent) busy.value = true
  try {
    const snap = await adminApi.mihomoConnections(activeAbort.signal)
    fetchError.value = ''
    nowTick.value = Date.now()

    if (!snap?.running) {
      archiveDisappeared(new Set(), nowTick.value)
      prevById.clear()
      activeList.value = []
      return
    }

    downloadTotal.value = snap.download_total || 0
    uploadTotal.value = snap.upload_total || 0

    const incoming = Array.isArray(snap.connections) ? snap.connections : []
    const currentIds = new Set(incoming.map((c) => c.id))
    archiveDisappeared(currentIds, nowTick.value)

    const nextMap = new Map()
    const nextList = incoming.map((conn) => {
      const prev = prevById.get(conn.id)
      let dlSpeed = 0
      let ulSpeed = 0
      if (prev && nowTick.value > prev.ts) {
        const dtSec = (nowTick.value - prev.ts) / 1000
        dlSpeed = Math.max(0, (conn.download - prev.conn.download) / dtSec)
        ulSpeed = Math.max(0, (conn.upload - prev.conn.upload) / dtSec)
      }
      const item = { uid: `active:${conn.id}`, conn, dlSpeed, ulSpeed, ts: nowTick.value, closedAt: null }
      nextMap.set(conn.id, item)
      return item
    })
    prevById = nextMap
    activeList.value = nextList
  } catch (err) {
    if (err?.name !== 'AbortError') {
      fetchError.value = err.message || '读取连接状态失败'
    }
  } finally {
    busy.value = false
  }
}

function archiveDisappeared(currentIds, closedAt) {
  const newlyClosed = []
  for (const [id, entry] of prevById.entries()) {
    if (!currentIds.has(id)) {
      closedSeq += 1
      newlyClosed.push({
        uid: `closed:${id}:${closedSeq}`,
        conn: entry.conn,
        dlSpeed: 0,
        ulSpeed: 0,
        ts: entry.ts,
        closedAt
      })
    }
  }
  if (newlyClosed.length) {
    closedList.value = [...newlyClosed, ...closedList.value].slice(0, 200)
  }
}

function refreshNow() {
  fetchSnapshot(false)
}

function togglePause() {
  paused.value = !paused.value
  syncPolling()
  if (!paused.value && props.active && props.running) {
    fetchSnapshot(false)
  }
}

async function closeOne(conn) {
  if (!conn?.id) return
  closingId.value = conn.id
  try {
    await adminApi.closeMihomoConnection(conn.id)
    await fetchSnapshot(true)
  } catch (err) {
    toast.error(err.message || '关闭连接失败')
  } finally {
    closingId.value = ''
  }
}

async function closeAll() {
  closing.value = true
  try {
    await adminApi.closeAllMihomoConnections()
    toast.success('已关闭所有活跃连接')
    await fetchSnapshot(true)
  } catch (err) {
    toast.error(err.message || '关闭所有连接失败')
  } finally {
    closing.value = false
  }
}

function clearClosed() {
  closedList.value = []
}

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
  if (tickTimer) {
    clearInterval(tickTimer)
    tickTimer = null
  }
  if (activeAbort) {
    activeAbort.abort()
    activeAbort = null
  }
}

function syncPolling() {
  stopPolling()
  if (!props.active || !props.running || paused.value || document.hidden) return
  pollTimer = setInterval(() => {
    if (props.active && props.running && !paused.value && !document.hidden) {
      fetchSnapshot(true)
    }
  }, 2000)
  tickTimer = setInterval(() => {
    nowTick.value = Date.now()
  }, 1000)
}

function onVisibilityChange() {
  if (document.hidden) {
    stopPolling()
  } else if (props.active && props.running && !paused.value) {
    fetchSnapshot(true)
    syncPolling()
  }
}

watch(
  () => [props.active, props.running],
  ([active, running]) => {
    if (active && running) {
      fetchSnapshot(true)
      syncPolling()
    } else if (!running) {
      stopPolling()
      if (prevById.size > 0) {
        archiveDisappeared(new Set(), Date.now())
        prevById.clear()
        activeList.value = []
      }
    } else {
      stopPolling()
    }
  },
  { immediate: true }
)

onMounted(() => {
  document.addEventListener('visibilitychange', onVisibilityChange)
})

onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onVisibilityChange)
  stopPolling()
})
</script>

<style scoped>
.connections-wrap {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 12px;
  min-width: 0;
  max-width: 100%;
}

.conn-controls-card {
  padding: 16px;
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 12px;
  min-width: 0;
  max-width: 100%;
}

.conn-top-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px;
}

.conn-title-group {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  min-width: 0;
}

.conn-title-group h2 {
  margin: 0;
  font-size: 16px;
  font-weight: 700;
}

.conn-meta-line {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}

.meta-pill {
  font-size: 12px;
  color: var(--muted);
}

.conn-totals {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  font-size: 12px;
  color: var(--muted);
}

.conn-action-btns {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}

.btn-danger {
  background: var(--bad);
  color: #ffffff;
  border: 1px solid transparent;
}

.btn-danger:hover:not(:disabled) {
  filter: brightness(0.92);
}

.btn-danger:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.conn-subtabs {
  display: inline-grid;
  grid-template-columns: 1fr 1fr;
  padding: 3px;
  border-radius: var(--radius-sm);
  background: var(--strip-bg);
  border: 1px solid var(--line);
  gap: 4px;
  width: 100%;
  max-width: 340px;
}

.conn-subtab {
  min-height: 36px;
  padding: 6px 12px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--muted);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: var(--transition-smooth);
}

.conn-subtab.active {
  background: var(--card-bg);
  color: var(--ink);
  box-shadow: 0 1px 3px rgba(15, 23, 42, 0.08);
}

.conn-filters-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 140px)) minmax(0, 1fr);
  gap: 8px;
  min-width: 0;
}

.conn-filter-select {
  min-width: 0;
  width: 100%;
}

.conn-search-wrap {
  position: relative;
  min-width: 0;
}

.conn-search-icon {
  position: absolute;
  left: 10px;
  top: 50%;
  transform: translateY(-50%);
  color: var(--faint);
  pointer-events: none;
}

.conn-search-input {
  width: 100%;
  padding-left: 30px !important;
}

.conn-list {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 10px;
  min-width: 0;
}

.conn-card {
  padding: 14px 16px;
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 8px;
  min-width: 0;
  max-width: 100%;
}

.conn-card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  min-width: 0;
}

.conn-target-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  min-width: 0;
  flex: 1;
}

.conn-host {
  font-size: 14px;
  font-weight: 700;
  color: var(--ink);
  overflow-wrap: anywhere;
}

.badge-info {
  color: var(--brand);
  background: rgba(99, 102, 241, 0.12);
  border-color: rgba(99, 102, 241, 0.28);
}

.conn-time-col {
  display: grid;
  justify-items: end;
  flex-shrink: 0;
  gap: 2px;
  font-size: 11.5px;
  color: var(--muted);
}

.conn-duration {
  font-size: 11px;
  color: var(--faint);
}

.conn-path {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 5px;
  font-size: 12px;
  color: var(--muted);
  overflow-wrap: anywhere;
}

.conn-arrow {
  color: var(--faint);
  flex-shrink: 0;
}

.conn-metrics-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  font-size: 12px;
}

.conn-speed {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 12px;
}

.conn-speed.down {
  color: var(--ok);
}

.conn-speed.up {
  color: var(--info);
}

.conn-rule-pill {
  font-size: 12px;
  color: var(--ink-secondary);
  padding: 2px 8px;
  border-radius: var(--radius-pill);
  background: var(--strip-bg);
  border: 1px solid var(--line);
  overflow-wrap: anywhere;
}

.conn-card-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding-top: 6px;
  border-top: 1px solid var(--line);
  min-width: 0;
}

.conn-chains-wrap {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  font-size: 12px;
  min-width: 0;
  overflow-wrap: anywhere;
}

.conn-chain-label {
  color: var(--muted);
}

.conn-chain-prefix {
  color: var(--ink-secondary);
  overflow-wrap: anywhere;
}

.conn-chain-final {
  max-width: 100%;
  overflow-wrap: anywhere;
}

.conn-close-btn {
  min-width: 32px;
  min-height: 32px;
  padding: 4px 8px;
  flex-shrink: 0;
}

.conn-empty-card {
  padding: 28px 16px;
  border-radius: var(--radius-card);
  background: var(--card-bg);
  border: 1px solid var(--card-border);
  text-align: center;
}

@media (max-width: 640px) {
  .conn-filters-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .conn-search-wrap {
    grid-column: 1 / -1;
  }

  .conn-subtabs {
    max-width: none;
  }
}
</style>
