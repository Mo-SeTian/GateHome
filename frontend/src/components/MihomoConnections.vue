<template>
  <div class="connections-wrap">
    <!-- Pending Reload Banner (visible when rules were saved with deferred reload) -->
    <div v-if="pendingReloadCount > 0 && running" class="pending-reload-banner" role="status">
      <div class="pending-reload-copy">
        <span class="badge badge-warn">待重载</span>
        <span>已新增 {{ pendingReloadCount }} 条自定义分流规则，尚未重载 Mihomo 运行配置</span>
      </div>
      <div class="pending-reload-actions">
        <button type="button" class="btn btn-secondary btn-sm" :disabled="reloadingConfig" @click="pendingReloadCount = 0">
          稍后再说
        </button>
        <button
          type="button"
          class="btn btn-primary btn-sm"
          :disabled="reloadingConfig"
          @click="reloadNowFromBanner"
        >
          <RotateCw :size="14" :class="{ spin: reloadingConfig }" />
          {{ reloadingConfig ? '重载中…' : '立即重载配置' }}
        </button>
      </div>
    </div>

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
            aria-label="关闭所有活跃连接"
            :disabled="closing || !running || !activeList.length"
            @click="closeAll"
          >
            <X :size="14" />
            <span class="label-desktop">关闭所有活跃连接</span>
            <span class="label-mobile">全关</span>
          </button>
          <button
            v-else
            type="button"
            class="btn btn-secondary btn-sm text-bad"
            aria-label="清空记录"
            :disabled="!closedList.length"
            @click="clearClosed"
          >
            <Trash2 :size="14" />
            <span class="label-desktop">清空记录</span>
            <span class="label-mobile">清空</span>
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
          <strong class="conn-host mono-text">{{ formatTarget(item.conn) }}</strong>
          <div class="conn-time-col">
            <span class="conn-rel-time">{{ formatRelativeStart(item.conn.start) }}</span>
            <span class="conn-duration mono-text">{{ formatDuration(item.conn.start, item.closedAt) }}</span>
          </div>
        </div>

        <div class="conn-tags-row">
          <span class="badge conn-net-badge" :class="networkBadgeClass(item.conn.metadata?.network)">
            {{ (item.conn.metadata?.network || 'tcp').toUpperCase() }}
          </span>
          <span v-if="item.conn.metadata?.type" class="badge badge-neutral">
            {{ item.conn.metadata.type }}
          </span>
          <span class="conn-rule-pill" :title="formatRuleText(item.conn)">
            {{ formatRuleText(item.conn) }}
          </span>
        </div>

        <div class="conn-telemetry-strip mono-text">
          <div class="conn-path">
            <span>{{ formatEndpoint(item.conn.metadata?.sourceIP, item.conn.metadata?.sourcePort) }}</span>
            <ArrowRight :size="12" class="conn-arrow" aria-hidden="true" />
            <span>{{ formatEndpoint(item.conn.metadata?.destinationIP, item.conn.metadata?.destinationPort) }}</span>
          </div>
          <div class="conn-metrics-row">
            <span class="conn-speed down">
              <ArrowDown :size="12" aria-hidden="true" />
              <template v-if="subTab === 'active'">{{ formatSpeed(item.dlSpeed) }} · </template>
              {{ formatBytes(item.conn.download) }}
            </span>
            <span class="conn-speed up">
              <ArrowUp :size="12" aria-hidden="true" />
              <template v-if="subTab === 'active'">{{ formatSpeed(item.ulSpeed) }} · </template>
              {{ formatBytes(item.conn.upload) }}
            </span>
          </div>
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

          <div class="conn-card-actions">
            <button
              type="button"
              class="btn btn-quick-rule btn-sm js-add-rule-btn"
              :aria-label="`将 ${formatTarget(item.conn)} 加入自定义分流`"
              title="将该连接的域名/IP 快速加入自定义分流"
              @click="openQuickRuleModal(item.conn)"
            >
              <Plus :size="14" aria-hidden="true" /> 加入自定义分流
            </button>
            <button
              v-if="subTab === 'active'"
              type="button"
              class="btn btn-danger-soft btn-sm conn-close-btn"
              :disabled="closingId === item.conn.id"
              :aria-label="`关闭连接 ${formatTarget(item.conn)}`"
              title="断开此连接"
              @click="closeOne(item.conn)"
            >
              <X :size="14" aria-hidden="true" />
              <span class="close-text">断开</span>
            </button>
          </div>
        </div>
      </SpotlightCard>
    </div>

    <div v-else class="report-empty conn-empty-card">
      {{ currentSourceList.length ? '无符合筛选条件的连接' : subTab === 'active' ? '当前暂无活跃连接' : '暂无已关闭连接记录' }}
    </div>

    <!-- Dialog: Two-Step Quick Add Rule (Bottom Sheet on mobile, centered dialog on desktop) -->
    <dialog
      ref="quickRuleDialog"
      class="modal-dialog quick-rule-sheet"
      aria-labelledby="quick-rule-dialog-title"
      @close="onDialogClose"
    >
      <div class="sheet-handle" aria-hidden="true" />

      <template v-if="modalStep === 'edit'">
        <div class="modal-header">
          <div class="modal-title-row">
            <h2 id="quick-rule-dialog-title" class="modal-title">快速加入自定义分流</h2>
            <span class="badge badge-brand">插入顶部优先</span>
          </div>
          <button type="button" class="close-modal-btn" aria-label="关闭" @click="quickRuleDialog?.close()">
            <X :size="18" />
          </button>
        </div>

        <form id="conn-quick-rule-form" class="modal-body quick-rule-body" @submit.prevent="submitQuickRule">
          <p v-if="modalError" class="report-error" role="alert">{{ modalError }}</p>

          <div class="source-context-box">
            <div class="source-context-top">
              <strong class="mono-text">{{ selectedConnTarget }}</strong>
              <span class="badge badge-neutral">命中: {{ selectedConnRule }} → {{ selectedConnNode }}</span>
            </div>
            <div class="source-endpoints mono-text">
              {{ selectedConnEndpoints }}
            </div>
          </div>

          <div v-if="candidateChips.length" class="form-field">
            <span>匹配粒度快捷选择</span>
            <div class="quick-chips-row">
              <button
                v-for="chip in candidateChips"
                :key="chip.key"
                type="button"
                class="chip-btn"
                :class="{ active: activeChipKey === chip.key }"
                @click="applyCandidateChip(chip)"
              >
                {{ chip.label }}
              </button>
            </div>
          </div>

          <div class="quick-form-grid">
            <label class="form-field">
              <span>规则类型</span>
              <select v-model="quickForm.type" name="quick_rule_type" class="form-input">
                <option value="DOMAIN-SUFFIX">DOMAIN-SUFFIX（域名后缀）</option>
                <option value="DOMAIN">DOMAIN（精确域名）</option>
                <option value="DOMAIN-KEYWORD">DOMAIN-KEYWORD（关键字）</option>
                <option value="IP-CIDR">IP-CIDR（目标 IP 段）</option>
                <option value="IP-CIDR6">IP-CIDR6（IPv6 目标段）</option>
                <option value="DST-PORT">DST-PORT（目标端口）</option>
              </select>
            </label>

            <label class="form-field">
              <span>目标策略组</span>
              <select v-model="quickForm.target" name="quick_rule_target" class="form-input" required>
                <option v-for="t in resolvedTargetOptions" :key="t" :value="t">{{ t }}</option>
              </select>
            </label>
          </div>

          <label class="form-field">
            <span>匹配值</span>
            <input
              v-model.trim="quickForm.value"
              name="quick_rule_value"
              class="form-input mono-text"
              required
              placeholder="例如：openai.com 或 1.1.1.1/32"
              @input="activeChipKey = ''"
            />
          </label>

          <label class="form-field">
            <span>备注（可选）</span>
            <input
              v-model.trim="quickForm.comment"
              name="quick_rule_comment"
              class="form-input"
              maxlength="256"
              placeholder="例如：来自连接记录"
            />
          </label>

          <label v-if="supportsNoResolve" class="toggle-card">
            <span>跳过 DNS 解析 (no-resolve)</span>
            <input v-model="quickForm.no_resolve" name="quick_rule_no_resolve" type="checkbox" />
          </label>

          <p v-if="duplicateExistingRule" class="report-warning duplicate-warn" role="status">
            已存在相同规则：{{ duplicateExistingRule.type }},{{ duplicateExistingRule.value }} → {{ duplicateExistingRule.target }}（再次加入将置顶优先匹配）
          </p>

          <div class="rule-preview-box">
            <span class="rule-preview-label">将写入自定义分流配置</span>
            <span class="rule-preview-code">{{ liveRulePreview }}</span>
          </div>
        </form>

        <div class="modal-footer quick-rule-footer">
          <button type="button" class="btn btn-secondary" :disabled="submittingRule" @click="quickRuleDialog?.close()">
            取消
          </button>
          <button type="submit" form="conn-quick-rule-form" class="btn btn-primary" :disabled="submittingRule">
            {{ submittingRule ? '保存中…' : '加入自定义分流' }}
          </button>
        </div>
      </template>

      <template v-else>
        <div class="modal-header">
          <div class="modal-title-row">
            <h2 id="quick-rule-dialog-title" class="modal-title">规则已加入配置</h2>
            <span class="badge badge-ok">已保存</span>
          </div>
          <button type="button" class="close-modal-btn" aria-label="关闭" @click="deferReload">
            <X :size="18" />
          </button>
        </div>

        <div class="modal-body quick-rule-body">
          <div class="reload-hero">
            <div class="reload-icon-circle" aria-hidden="true">
              <Check :size="16" />
            </div>
            <div class="reload-hero-copy">
              <h3>是否立即重载 Mihomo 运行配置？</h3>
              <p>新分流规则已写入 config.json 并重新合成 runtime.yaml；重载后即可生效。</p>
            </div>
          </div>

          <div class="rule-preview-box">
            <span class="rule-preview-label">新增规则</span>
            <span class="rule-preview-code">{{ savedRulePreview }}</span>
          </div>

          <label v-if="selectedFromActive && selectedConn?.id" class="toggle-card">
            <div class="toggle-copy">
              <strong>重载后断开该条现有连接</strong>
              <span>断开旧 TCP/QUIC 长连接，下一次请求立刻按新规则分流</span>
            </div>
            <input v-model="closeAfterReload" name="close_conn_after_reload" type="checkbox" />
          </label>
        </div>

        <div class="modal-footer quick-rule-footer">
          <button type="button" class="btn btn-secondary" :disabled="reloadingConfig" @click="deferReload">
            稍后重载（继续批量添加）
          </button>
          <button type="button" class="btn btn-primary" :disabled="reloadingConfig" @click="confirmReloadNow">
            <RotateCw :size="14" :class="{ spin: reloadingConfig }" />
            {{ reloadingConfig ? '重载中…' : '立即重载配置' }}
          </button>
        </div>
      </template>
    </dialog>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import {
  ArrowDown,
  ArrowRight,
  ArrowUp,
  Check,
  Pause,
  Play,
  Plus,
  RefreshCw,
  RotateCw,
  Search,
  Trash2,
  X
} from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { adminApi } from '@/api/admin'
import { formatBytes } from '@/api/reports'
import { useToast } from '@/composables/useToast'
import {
  deriveConnectionRuleCandidates,
  findDuplicateCustomRule,
  formatMihomoRulePreview
} from '@/composables/mihomoRuleHelper'

const props = defineProps({
  active: { type: Boolean, default: false },
  running: { type: Boolean, default: false },
  targetOptions: { type: Array, default: () => ['DIRECT', 'REJECT'] },
  customRules: { type: Array, default: () => [] }
})

const emit = defineEmits(['updated'])

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

// Quick-rule modal & pending reload state
const quickRuleDialog = ref(null)
const modalStep = ref('edit')
const modalError = ref('')
const submittingRule = ref(false)
const reloadingConfig = ref(false)
const pendingReloadCount = ref(0)
const selectedConn = ref(null)
const selectedFromActive = ref(true)
const candidateChips = ref([])
const activeChipKey = ref('')
const savedRulePreview = ref('')
const closeAfterReload = ref(true)

const quickForm = reactive({
  type: 'DOMAIN-SUFFIX',
  value: '',
  target: 'DIRECT',
  no_resolve: false,
  comment: ''
})

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

const resolvedTargetOptions = computed(() => {
  const list = Array.isArray(props.targetOptions) && props.targetOptions.length ? props.targetOptions : ['DIRECT', 'REJECT']
  return [...new Set([...list, 'DIRECT', 'REJECT'])]
})

const supportsNoResolve = computed(() => ['IP-CIDR', 'IP-CIDR6', 'SRC-IP-CIDR', 'GEOIP', 'RULE-SET'].includes(quickForm.type))

const liveRulePreview = computed(() =>
  formatMihomoRulePreview({
    type: quickForm.type,
    value: quickForm.value,
    target: quickForm.target,
    no_resolve: quickForm.no_resolve
  })
)

const duplicateExistingRule = computed(() =>
  findDuplicateCustomRule(props.customRules, {
    type: quickForm.type,
    value: quickForm.value
  })
)

const selectedConnTarget = computed(() => (selectedConn.value ? formatTarget(selectedConn.value) : '—'))
const selectedConnRule = computed(() => (selectedConn.value ? formatRuleText(selectedConn.value) : '—'))
const selectedConnNode = computed(() => (selectedConn.value ? finalNode(selectedConn.value) : 'DIRECT'))
const selectedConnEndpoints = computed(() => {
  const meta = selectedConn.value?.metadata || {}
  return `${formatEndpoint(meta.sourceIP, meta.sourcePort)} → ${formatEndpoint(meta.destinationIP, meta.destinationPort)}`
})

function openQuickRuleModal(conn) {
  selectedConn.value = conn
  selectedFromActive.value = subTab.value === 'active'
  modalStep.value = 'edit'
  modalError.value = ''
  closeAfterReload.value = selectedFromActive.value

  const chips = deriveConnectionRuleCandidates(conn)
  candidateChips.value = chips
  const first = chips[0] || { key: '', type: 'DOMAIN-SUFFIX', value: conn?.metadata?.host || '', no_resolve: false }
  activeChipKey.value = first.key || ''
  quickForm.type = first.type
  quickForm.value = first.value
  quickForm.no_resolve = !!first.no_resolve
  quickForm.target = resolvedTargetOptions.value[0] || 'DIRECT'
  const labelHost = conn?.metadata?.host || conn?.metadata?.destinationIP || ''
  quickForm.comment = labelHost ? `来自连接 ${labelHost}` : '来自连接记录'

  quickRuleDialog.value?.showModal()
}

function applyCandidateChip(chip) {
  activeChipKey.value = chip.key
  quickForm.type = chip.type
  quickForm.value = chip.value
  quickForm.no_resolve = !!chip.no_resolve
}

async function submitQuickRule() {
  modalError.value = ''
  submittingRule.value = true
  try {
    const res = await adminApi.addMihomoRule({
      type: quickForm.type,
      value: quickForm.value,
      target: quickForm.target,
      no_resolve: supportsNoResolve.value ? quickForm.no_resolve : false,
      enabled: true,
      comment: quickForm.comment,
      skip_reload: props.running
    })
    emit('updated', res)

    if (props.running) {
      savedRulePreview.value = liveRulePreview.value
      pendingReloadCount.value += 1
      modalStep.value = 'reload'
    } else {
      quickRuleDialog.value?.close()
      toast.success('已加入自定义分流（内核未运行，启动时将自动应用）')
    }
  } catch (err) {
    modalError.value = err.message || '添加分流规则失败'
  } finally {
    submittingRule.value = false
  }
}

function deferReload() {
  quickRuleDialog.value?.close()
  if (props.running && pendingReloadCount.value > 0) {
    toast.info('已写入配置，可继续批量添加后在顶部一键重载')
  }
}

async function performReload(alsoCloseConn = false) {
  reloadingConfig.value = true
  try {
    const res = await adminApi.reloadMihomo()
    emit('updated', res)
    pendingReloadCount.value = 0

    let closedExisting = false
    if (alsoCloseConn && selectedFromActive.value && selectedConn.value?.id) {
      try {
        await adminApi.closeMihomoConnection(selectedConn.value.id)
        closedExisting = true
      } catch {
        // Non-fatal if the connection already closed naturally before reload finished.
      }
    }

    quickRuleDialog.value?.close()
    await fetchSnapshot(true)
    toast.success(closedExisting ? '配置已热重载，并已断开旧连接' : 'Mihomo 配置已热重载生效')
  } catch (err) {
    toast.error(err.message || '重载配置失败')
  } finally {
    reloadingConfig.value = false
  }
}

function confirmReloadNow() {
  return performReload(closeAfterReload.value)
}

function reloadNowFromBanner() {
  return performReload(false)
}

function onDialogClose() {
  modalError.value = ''
  modalStep.value = 'edit'
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
  const currentController = new AbortController()
  activeAbort = currentController
  if (!silent) busy.value = true
  try {
    const snap = await adminApi.mihomoConnections(currentController.signal)
    if (activeAbort !== currentController) return
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
    if (err?.name !== 'AbortError' && activeAbort === currentController) {
      fetchError.value = err.message || '读取连接状态失败'
    }
  } finally {
    if (activeAbort === currentController) {
      busy.value = false
    }
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
    syncPolling()
    fetchSnapshot(true)
  }
}

watch(
  () => [props.active, props.running],
  ([active, running]) => {
    if (!running) {
      pendingReloadCount.value = 0
    }
    if (active && running) {
      syncPolling()
      fetchSnapshot(true)
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

.pending-reload-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px 12px;
  padding: 11px 14px;
  border-radius: var(--radius-card);
  background: rgba(245, 158, 11, 0.14);
  border: 1px solid rgba(245, 158, 11, 0.36);
}

.pending-reload-copy {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  font-size: 13px;
  font-weight: 600;
  color: var(--ink);
  min-width: 0;
}

.pending-reload-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.spin {
  animation: connSpin 0.9s linear infinite;
}

@keyframes connSpin {
  to {
    transform: rotate(360deg);
  }
}

.conn-controls-card {
  padding: 16px;
  min-width: 0;
  max-width: 100%;
}

.conn-controls-card :deep(.card-inner) {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 12px;
  min-width: 0;
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

.label-mobile {
  display: none;
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

.btn-quick-rule {
  background: rgba(99, 102, 241, 0.12);
  color: var(--brand);
  border: 1px solid rgba(99, 102, 241, 0.34);
}

.btn-quick-rule:hover:not(:disabled) {
  background: var(--brand);
  color: #ffffff;
  border-color: var(--brand);
}

.btn-danger-soft {
  background: rgba(239, 68, 68, 0.1);
  color: var(--bad);
  border: 1px solid rgba(239, 68, 68, 0.25);
}

.btn-danger-soft:hover:not(:disabled) {
  background: rgba(239, 68, 68, 0.18);
}

.btn-danger-soft:disabled {
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
  min-width: 0;
  max-width: 100%;
}

.conn-card :deep(.card-inner) {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 9px;
  min-width: 0;
}

.conn-card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  min-width: 0;
}

.conn-host {
  font-size: 14px;
  font-weight: 700;
  color: var(--ink);
  overflow-wrap: anywhere;
  min-width: 0;
}

.conn-tags-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  min-width: 0;
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

.conn-telemetry-strip {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 6px 12px;
  padding: 8px 10px;
  border-radius: 10px;
  background: var(--strip-bg);
  border: 1px solid var(--line);
  font-size: 12px;
  min-width: 0;
}

.conn-path {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 5px;
  color: var(--muted);
  overflow-wrap: anywhere;
  min-width: 0;
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
  font-size: 11.5px;
  color: var(--ink-secondary);
  padding: 2px 8px;
  border-radius: var(--radius-pill);
  background: var(--strip-bg);
  border: 1px solid var(--line);
  overflow-wrap: anywhere;
  max-width: 100%;
}

.conn-card-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
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

.conn-card-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.js-add-rule-btn,
.conn-close-btn {
  min-height: 34px;
  white-space: nowrap;
}

.conn-empty-card {
  padding: 28px 16px;
  border-radius: var(--radius-card);
  background: var(--card-bg);
  border: 1px solid var(--card-border);
  text-align: center;
}

/* Modal & Mobile Bottom Sheet */
dialog.quick-rule-sheet[open] {
  display: flex;
  flex-direction: column;
  width: min(540px, calc(100vw - 24px));
  max-height: calc(100dvh - 32px);
  overflow: hidden;
}

.sheet-handle {
  display: none;
}

.modal-title-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.quick-rule-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
  overflow-y: auto;
  flex: 1;
}

.source-context-box {
  padding: 10px 12px;
  border-radius: 10px;
  background: var(--strip-bg);
  border: 1px solid var(--line);
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
}

.source-context-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 6px;
}

.source-context-top strong {
  overflow-wrap: anywhere;
}

.source-endpoints {
  color: var(--muted);
  overflow-wrap: anywhere;
}

.quick-chips-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.chip-btn {
  border: 1px solid var(--line);
  background: var(--strip-bg);
  color: var(--ink-secondary);
  border-radius: var(--radius-pill);
  padding: 6px 11px;
  min-height: 32px;
  font-size: 12px;
  font-family: var(--mono);
  cursor: pointer;
  transition: var(--transition-smooth);
}

.chip-btn:hover {
  border-color: rgba(99, 102, 241, 0.36);
  color: var(--brand);
}

.chip-btn.active {
  background: rgba(99, 102, 241, 0.14);
  border-color: rgba(99, 102, 241, 0.42);
  color: var(--brand);
  font-weight: 700;
}

.quick-form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.toggle-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 11px 12px;
  border-radius: 10px;
  background: var(--strip-bg);
  border: 1px solid var(--line);
  font-size: 13px;
  cursor: pointer;
}

.toggle-copy {
  display: grid;
  gap: 2px;
}

.toggle-copy span {
  font-size: 12px;
  color: var(--muted);
}

.toggle-card input[type='checkbox'] {
  width: 18px;
  height: 18px;
  flex-shrink: 0;
  accent-color: var(--brand);
}

.duplicate-warn {
  margin: 0;
  font-size: 12.5px;
}

.rule-preview-box {
  padding: 10px 12px;
  border-radius: 10px;
  background: var(--strip-bg);
  border: 1px dashed rgba(99, 102, 241, 0.4);
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.rule-preview-label {
  font-size: 11.5px;
  font-weight: 600;
  color: var(--muted);
}

.rule-preview-code {
  font-family: var(--mono);
  font-size: 12.5px;
  font-weight: 600;
  color: var(--brand);
  overflow-wrap: anywhere;
}

.reload-hero {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 12px 14px;
  border-radius: 12px;
  background: rgba(16, 185, 129, 0.12);
  border: 1px solid rgba(16, 185, 129, 0.3);
}

.reload-icon-circle {
  width: 30px;
  height: 30px;
  border-radius: 50%;
  background: var(--ok);
  color: #ffffff;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.reload-hero-copy h3 {
  margin: 0 0 2px;
  font-size: 14px;
  font-weight: 700;
}

.reload-hero-copy p {
  margin: 0;
  font-size: 12.5px;
  color: var(--ink-secondary);
}

.quick-rule-footer {
  flex-shrink: 0;
}

@media (max-width: 640px) {
  .pending-reload-banner {
    flex-direction: column;
    align-items: stretch;
  }

  .pending-reload-actions {
    width: 100%;
  }

  .pending-reload-actions .btn-primary {
    flex: 1;
    min-height: 40px;
  }

  .pending-reload-actions .btn-secondary {
    min-height: 40px;
  }

  .conn-top-row {
    flex-direction: column;
    align-items: stretch;
  }

  .conn-action-btns {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr)) minmax(0, 1.15fr);
    gap: 6px;
    width: 100%;
  }

  .conn-action-btns .btn {
    min-height: 40px;
    padding: 6px 8px;
    justify-content: center;
  }

  .label-desktop {
    display: none;
  }

  .label-mobile {
    display: inline;
  }

  .conn-subtabs {
    max-width: none;
    width: 100%;
  }

  .conn-subtab {
    min-height: 40px;
  }

  .conn-filters-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .conn-search-wrap {
    grid-column: 1 / -1;
  }

  .conn-telemetry-strip {
    flex-direction: column;
    align-items: flex-start;
    gap: 5px;
  }

  .conn-card-foot {
    flex-direction: column;
    align-items: stretch;
    gap: 9px;
  }

  .conn-card-actions {
    width: 100%;
  }

  .js-add-rule-btn {
    flex: 1;
    min-height: 42px;
    font-size: 13px;
  }

  .conn-close-btn {
    min-width: 78px;
    min-height: 42px;
    font-size: 13px;
  }

  dialog.modal-dialog.quick-rule-sheet[open] {
    margin: auto 0 0 0 !important;
    width: 100% !important;
    max-width: 100vw !important;
    max-height: 88dvh !important;
    border-radius: 20px 20px 0 0 !important;
    border-bottom: 0 !important;
  }

  .sheet-handle {
    display: block;
    width: 38px;
    height: 4px;
    border-radius: 999px;
    background: var(--line);
    margin: 9px auto 2px;
    flex-shrink: 0;
  }

  .quick-form-grid {
    grid-template-columns: 1fr;
  }

  dialog.quick-rule-sheet :is(.form-input, .form-select) {
    min-height: 44px;
    font-size: 14px;
  }

  .quick-rule-footer {
    flex-direction: column-reverse;
    align-items: stretch;
    gap: 8px;
    padding-bottom: max(14px, env(safe-area-inset-bottom)) !important;
  }

  .quick-rule-footer .btn {
    width: 100%;
    min-height: 44px;
  }
}
</style>
