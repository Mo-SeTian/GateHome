<template>
  <div class="overview-wrap">
    <!-- Row 1: 4 Telemetry KPI Cards -->
    <section aria-label="核心运行指标" class="kpi-grid">
      <!-- KPI 1: Running Status -->
      <SpotlightCard class="report-panel kpi-card">
        <div class="kpi-head">
          <span class="kpi-label">Mihomo 状态</span>
          <span class="badge" :class="isRunning ? 'badge-ok' : status?.core_installed ? 'badge-neutral' : 'badge-warn'">
            <span class="status-dot" :class="isRunning ? 'ok' : status?.core_installed ? 'neutral' : 'warn'" />
            {{ isRunning ? '运行中' : status?.core_installed ? '已停止' : '未安装' }}
          </span>
        </div>
        <div class="kpi-body">
          <div class="kpi-value mono-text tabular-nums">{{ shortCoreVersion }}</div>
          <div class="kpi-sub tabular-nums">{{ uptimeText }}</div>
        </div>
        <div class="kpi-foot">
          <span class="mono-text tabular-nums">PID {{ isRunning && currentPid ? currentPid : '—' }}</span>
          <span
            v-if="status?.core_update?.latest_version"
            class="badge"
            :class="status.core_update.update_available ? 'badge-warn' : 'badge-ok'"
          >
            {{ status.core_update.update_available ? `可更新 ${status.core_update.latest_version}` : '已是最新' }}
          </span>
        </div>
      </SpotlightCard>

      <!-- KPI 2: Live Throughput -->
      <SpotlightCard class="report-panel kpi-card">
        <div class="kpi-head">
          <span class="kpi-label">实时速率</span>
          <span class="badge" :class="!isRunning ? 'badge-neutral' : paused ? 'badge-warn' : 'badge-brand'">
            {{ !isRunning ? '离线' : paused ? '已暂停' : '实时 2s' }}
          </span>
        </div>
        <div class="kpi-rates">
          <div class="rate-line">
            <span class="rate-dir down">↓ 下行</span>
            <strong class="rate-val mono-text tabular-nums">{{ fmtRate(currentDownRate) }}</strong>
          </div>
          <div class="rate-line">
            <span class="rate-dir up">↑ 上行</span>
            <strong class="rate-val-sub mono-text tabular-nums">{{ fmtRate(currentUpRate) }}</strong>
          </div>
        </div>
        <div class="kpi-foot mono-text tabular-nums kpi-foot-totals">
          <span class="nowrap-chunk">累计 ↓ {{ isRunning ? formatBytes(telemetry?.download_total || 0) : '—' }}</span>
          <span class="foot-sep" aria-hidden="true">·</span>
          <span class="nowrap-chunk">↑ {{ isRunning ? formatBytes(telemetry?.upload_total || 0) : '—' }}</span>
        </div>
      </SpotlightCard>

      <!-- KPI 3: Process CPU & Memory -->
      <SpotlightCard class="report-panel kpi-card">
        <div class="kpi-head">
          <span class="kpi-label">进程 CPU / 内存</span>
          <span class="badge badge-neutral mono-text tabular-nums">
            Heap {{ isRunning && telemetry?.heap_inuse_bytes ? formatBytes(telemetry.heap_inuse_bytes) : '—' }}
          </span>
        </div>
        <div class="kpi-bars">
          <div class="bar-block">
            <div class="bar-label-row">
              <span>CPU 占用</span>
              <strong class="mono-text tabular-nums">{{ isRunning ? `${Number(telemetry?.cpu_percent || 0).toFixed(1)}%` : '0.0%' }}</strong>
            </div>
            <div class="progress-track">
              <div class="progress-fill cpu" :style="{ width: `${cpuBarWidth}%` }" />
            </div>
          </div>
          <div class="bar-block">
            <div class="bar-label-row">
              <span>物理内存 (RSS)</span>
              <strong class="mono-text tabular-nums">{{ isRunning && telemetry?.memory_rss_bytes ? formatBytes(telemetry.memory_rss_bytes) : '0 B' }}</strong>
            </div>
            <div class="progress-track">
              <div class="progress-fill mem" :style="{ width: `${memBarWidth}%` }" />
            </div>
          </div>
        </div>
        <div class="kpi-foot tabular-nums">
          <span v-if="isRunning && telemetry?.host_mem_total_bytes">
            占宿主机内存 {{ Number(telemetry.memory_percent || 0).toFixed(1) }}% ({{ formatBytes(telemetry.memory_rss_bytes || 0) }} / {{ formatBytes(telemetry.host_mem_total_bytes) }})
          </span>
          <span v-else>{{ isRunning ? '进程内存遥测已就绪' : '启动内核后采集 CPU 与内存遥测' }}</span>
        </div>
      </SpotlightCard>

      <!-- KPI 4: Active Connections & Scale -->
      <SpotlightCard class="report-panel kpi-card">
        <div class="kpi-head">
          <span class="kpi-label">活跃连接</span>
          <span class="badge badge-neutral">{{ modeLabel }}</span>
        </div>
        <div class="kpi-body">
          <div class="kpi-value mono-text tabular-nums">{{ isRunning ? (telemetry?.active_connections ?? 0) : 0 }}</div>
          <div class="kpi-sub">当前并发代理会话数</div>
        </div>
        <div class="kpi-foot tabular-nums">
          <span>
            {{ telemetry?.group_count ?? 0 }} 策略组 · {{ telemetry?.node_count ?? 0 }} 节点 ·
            {{ formatNumber(telemetry?.total_rules_count ?? 0) }} 条规则
          </span>
        </div>
      </SpotlightCard>
    </section>

    <!-- Row 2: Traffic Chart (8 cols) + Ports & Access (4 cols) -->
    <section class="split-12-grid">
      <!-- Traffic Chart Panel -->
      <SpotlightCard class="report-panel chart-panel">
        <div class="panel-head">
          <div class="panel-title-wrap">
            <div class="title-line">
              <h2>实时流量统计</h2>
              <span class="badge" :class="!isRunning ? 'badge-neutral' : paused ? 'badge-warn' : 'badge-ok'">
                <span class="status-dot" :class="!isRunning ? 'neutral' : paused ? 'warn' : 'ok'" />
                {{ !isRunning ? '内核已停止' : paused ? '已暂停采样' : '实时采样 (近 2 分钟)' }}
              </span>
            </div>
            <p class="panel-sub">基于 Mihomo 控制器实时流量增量采样，悬停图表查看逐点速率</p>
          </div>
          <div class="chart-legend-actions">
            <span class="legend-item"><span class="legend-dot down" />下行</span>
            <span class="legend-item"><span class="legend-dot up" />上行</span>
            <button type="button" class="btn btn-secondary btn-sm" @click="togglePause">
              <Play v-if="paused" :size="13" />
              <Pause v-else :size="13" />
              {{ paused ? '继续采样' : '暂停采样' }}
            </button>
          </div>
        </div>

        <div class="chart-stage">
          <canvas
            ref="chartCanvas"
            class="traffic-canvas"
            @mousemove="onChartMove"
            @mouseleave="onChartLeave"
          />
          <div v-if="hoverInfo" class="chart-tooltip mono-text tabular-nums">
            {{ hoverInfo }}
          </div>
          <div v-if="!isRunning" class="chart-offline-overlay">
            <span class="chart-offline-pill">Mihomo 内核未运行，启动后自动开始采集实时流量速率</span>
          </div>
        </div>

        <div class="chart-summary-grid">
          <div class="summary-tile">
            <span class="summary-label">当前下载</span>
            <strong class="mono-text tabular-nums down-text">{{ fmtRate(currentDownRate) }}</strong>
          </div>
          <div class="summary-tile">
            <span class="summary-label">当前上传</span>
            <strong class="mono-text tabular-nums up-text">{{ fmtRate(currentUpRate) }}</strong>
          </div>
          <div class="summary-tile">
            <span class="summary-label">近 2 分钟峰值</span>
            <strong class="mono-text tabular-nums">{{ isRunning ? fmtRate(peakDownRate) : '0 B/s' }}</strong>
          </div>
          <div class="summary-tile">
            <span class="summary-label">会话累计总流量</span>
            <strong class="mono-text tabular-nums">{{ isRunning ? formatBytes((telemetry?.download_total || 0) + (telemetry?.upload_total || 0)) : '—' }}</strong>
          </div>
        </div>
      </SpotlightCard>

      <!-- Ports & Security Panel -->
      <SpotlightCard class="report-panel ports-panel">
        <div class="panel-head">
          <div class="panel-title-wrap">
            <h2>监听端口与接入地址</h2>
            <p class="panel-sub">点击任意地址一键复制</p>
          </div>
          <button type="button" class="btn btn-secondary btn-sm" @click="$emit('navigate', 'settings')">
            编辑
          </button>
        </div>

        <div class="ports-list">
          <button
            v-for="entry in portItems"
            :key="entry.key"
            type="button"
            class="port-row-btn"
            :title="`复制 ${entry.address}`"
            @click="copyPort(entry)"
          >
            <div class="port-row-meta">
              <strong class="port-row-title">{{ entry.title }}</strong>
              <span class="port-row-addr mono-text tabular-nums">{{ entry.address }}</span>
            </div>
            <div class="port-row-right">
              <span class="badge" :class="entry.badgeClass">{{ entry.badgeText }}</span>
              <Copy :size="13" class="port-copy-icon" aria-hidden="true" />
            </div>
          </button>
        </div>

        <div class="security-badges">
          <span class="badge" :class="status?.settings?.allow_lan ? 'badge-ok' : 'badge-neutral'">
            局域网访问: {{ status?.settings?.allow_lan ? '开启' : '仅本机' }}
          </span>
          <span class="badge" :class="status?.controller_allow_lan ? 'badge-warn' : 'badge-neutral'">
            管理 API: {{ status?.controller_allow_lan ? `0.0.0.0:${status?.controller_port || 19090}` : '仅本机' }}
          </span>
          <span class="badge" :class="status?.settings?.auth_enabled ? 'badge-brand' : 'badge-neutral'">
            代理认证: {{ status?.settings?.auth_enabled ? (status.settings.auth_username || '已启用') : '未开启' }}
          </span>
          <span class="badge" :class="status?.outbound_takeover_active ? 'badge-ok' : status?.takeover_outbound ? 'badge-warn' : 'badge-neutral'">
            出站联动: {{ status?.outbound_takeover_active ? '接管中' : status?.takeover_outbound ? '待内核启动' : '未启用' }}
          </span>
        </div>
      </SpotlightCard>
    </section>

    <!-- Row 3: Running Configuration (6 cols) + Core Update & Backup (6 cols) -->
    <section class="split-6-grid">
      <!-- Left: Running Configuration Summary -->
      <SpotlightCard class="report-panel info-panel">
        <div class="panel-head">
          <div class="panel-title-wrap">
            <h2>运行配置信息</h2>
            <p class="panel-sub">当前生效配置、订阅状态与内核特性</p>
          </div>
          <button type="button" class="btn btn-secondary btn-sm" @click="$emit('navigate', 'yaml')">
            查看 runtime.yaml
          </button>
        </div>

        <dl class="config-grid">
          <div class="config-item">
            <dt>配置合成来源</dt>
            <dd>
              <strong>{{ status?.manual_override ? '手动直接编辑' : '流水线自动合成' }}</strong>
              <span class="badge" :class="status?.manual_override ? 'badge-warn' : 'badge-ok'">
                {{ status?.manual_override ? '手改覆盖' : '已同步' }}
              </span>
            </dd>
            <div class="config-sub mono-text">runtime.yaml · 控制器端口 {{ status?.controller_port || 19090 }}</div>
          </div>

          <div class="config-item">
            <dt>当前激活订阅</dt>
            <dd>
              <strong>{{ activeSubscription ? activeSubscription.name : '未选择订阅（冷启动基线）' }}</strong>
              <span v-if="activeSubscription?.exclude_groups_and_rules" class="badge badge-warn">仅导入节点</span>
            </dd>
            <template v-if="activeSubscription?.total">
              <div class="quota-row tabular-nums">
                <span>已用 {{ formatBytes((activeSubscription.upload || 0) + (activeSubscription.download || 0)) }} / {{ formatBytes(activeSubscription.total) }}</span>
                <span>{{ subQuotaPct }}%</span>
              </div>
              <div class="progress-track">
                <div class="progress-fill brand" :style="{ width: `${subQuotaPct}%` }" />
              </div>
            </template>
            <div v-else class="config-sub">
              {{ activeSubscription ? `${activeSubscription.node_count || 0} 个节点` : '可在「订阅管理」导入机场订阅' }}
            </div>
          </div>

          <div class="config-item">
            <dt>DNS 与网络特性</dt>
            <dd class="badge-wrap">
              <span class="badge badge-brand">{{ dnsModeLabel }}</span>
              <span class="badge" :class="status?.settings?.ipv6 ? 'badge-ok' : 'badge-neutral'">
                IPv6 {{ status?.settings?.ipv6 ? '开启' : '关闭' }}
              </span>
              <span v-if="status?.settings?.unified_delay" class="badge badge-ok">统一延迟</span>
              <span v-if="status?.settings?.tcp_concurrent" class="badge badge-ok">TCP 并发</span>
            </dd>
            <div class="config-sub">
              专线 DNS 策略：自定义 {{ status?.custom_dns_policies?.length || 0 }} 条 · 继承 {{ status?.inherited_dns_policies?.length || 0 }} 条
            </div>
          </div>

          <div class="config-item">
            <dt>规则集与 GeoIP 数据库</dt>
            <dd>
              <strong class="tabular-nums">
                {{ status?.custom_rule_sets?.length || 0 }} 个规则集 ({{ formatNumber(telemetry?.total_rules_count ?? 0) }} 条) ·
                {{ status?.custom_rules?.length || 0 }} 条自定义规则
              </strong>
            </dd>
            <div class="config-sub tabular-nums">
              {{ status?.mmdb_installed ? `country.mmdb 就绪 (${formatBytes(status.mmdb_size || 0)})` : 'country.mmdb 未下载（启动时自动拉取）' }}
            </div>
          </div>
        </dl>
      </SpotlightCard>

      <!-- Right: Core Update + Standalone Mihomo Backup & Restore -->
      <SpotlightCard class="report-panel actions-panel">
        <div class="action-section">
          <div class="panel-head">
            <div class="title-line">
              <h2>Mihomo 内核更新</h2>
              <span v-if="status?.core_update?.latest_version" class="badge" :class="status.core_update.update_available ? 'badge-warn' : 'badge-ok'">
                {{ status.core_update.update_available ? `发现新版本 ${status.core_update.latest_version}` : '已是最新版本' }}
              </span>
            </div>
            <span class="mono-text muted-tiny">MetaCubeX/mihomo</span>
          </div>

          <div class="action-box">
            <div class="action-box-copy">
              <div class="version-compare">
                <span>当前内核：<strong class="mono-text">{{ shortCoreVersion }}</strong></span>
                <span v-if="status?.core_update?.latest_version" class="latest-tag">
                  → 最新：<strong class="mono-text ok-text">{{ status.core_update.latest_version }}</strong>
                </span>
              </div>
              <div class="config-sub">
                <template v-if="status?.core_update?.checked_at">
                  上次检查：{{ formatTime(status.core_update.checked_at) }} · 原地升级并自动热重启内核
                </template>
                <template v-else>
                  点击「检查更新」获取最新发行版；更新时保留全部配置文件
                </template>
              </div>
            </div>
            <div class="action-box-btns">
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="checkingCoreUpdate || coreBusy"
                @click="$emit('check-core-update')"
              >
                <RefreshCw :size="13" :class="{ spinning: checkingCoreUpdate }" />
                {{ checkingCoreUpdate ? '检查中…' : '检查更新' }}
              </button>
              <button
                type="button"
                class="btn btn-primary btn-sm"
                :disabled="coreBusy"
                @click="$emit('upgrade-core')"
              >
                <Download :size="13" />
                {{ coreBusy ? '更新中…' : status?.core_update?.update_available ? '立即更新内核' : '下载/更新内核' }}
              </button>
            </div>
          </div>
        </div>

        <div class="section-divider" />

        <div class="action-section">
          <div class="panel-head">
            <div class="title-line">
              <h2>Mihomo 配置单独备份与恢复</h2>
              <span class="badge badge-ok">支持 WebDAV 联动</span>
            </div>
          </div>

          <div class="action-box stacked">
            <p class="backup-desc">
              独立导出/恢复 Mihomo 全套配置（<span class="mono-text">config.json</span>、订阅源、自定义策略组、分流规则、手动规则集与 <span class="mono-text">runtime.yaml</span>）；同时已自动纳入 GateHome「备份与恢复」全量包与 WebDAV 云同步。
            </p>
            <div class="backup-btn-row">
              <button type="button" class="btn btn-primary btn-sm" :disabled="backupBusy" @click="exportBackup">
                <Download :size="13" /> {{ backupBusy ? '导出中…' : '导出 Mihomo 备份' }}
              </button>
              <button type="button" class="btn btn-secondary btn-sm" :disabled="restoreBusy" @click="triggerRestore">
                <Upload :size="13" /> {{ restoreBusy ? '恢复中…' : '导入恢复配置' }}
              </button>
              <RouterLink to="/backups" class="btn btn-secondary btn-sm">
                全局备份 / WebDAV →
              </RouterLink>
              <input
                ref="restoreFileInput"
                type="file"
                accept=".json,application/json"
                class="sr-only"
                @change="handleRestoreFile"
              />
            </div>
          </div>
        </div>
      </SpotlightCard>
    </section>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Copy, Download, Pause, Play, RefreshCw, Upload } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { adminApi } from '@/api/admin'
import { copyText as copyToClipboard } from '@/api/clipboard'
import { formatBytes } from '@/api/reports'
import { useToast } from '@/composables/useToast'

const props = defineProps({
  status: { type: Object, default: null },
  activePortEntries: { type: Array, default: () => [] },
  active: { type: Boolean, default: false },
  coreBusy: { type: Boolean, default: false },
  checkingCoreUpdate: { type: Boolean, default: false }
})

const emit = defineEmits(['navigate', 'updated', 'check-core-update', 'upgrade-core'])

const toast = useToast()
const chartCanvas = ref(null)
const restoreFileInput = ref(null)
const telemetry = ref(null)
const paused = ref(false)
const backupBusy = ref(false)
const restoreBusy = ref(false)
const downHistory = ref(new Array(60).fill(0))
const upHistory = ref(new Array(60).fill(0))
const hoverIndex = ref(-1)

let pollTimer = null
let activeRequest = null
let themeObserver = null

const isRunning = computed(() => !!(telemetry.value?.running ?? props.status?.running))
const currentPid = computed(() => telemetry.value?.pid || props.status?.pid || 0)

const shortCoreVersion = computed(() => {
  const raw = props.status?.core_version || ''
  if (!raw) return props.status?.core_installed ? '已安装' : '未安装'
  return raw.replace(/^Mihomo\s+Meta\s+/i, '').split(/\s+/)[0] || raw
})

const uptimeText = computed(() => {
  if (!isRunning.value) return '内核当前未启动'
  const sec = Number(telemetry.value?.uptime_seconds ?? props.status?.uptime_seconds ?? 0)
  if (sec <= 0) return '刚刚启动'
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  if (d > 0) return `运行时长 ${d}天 ${h}小时 ${m}分`
  if (h > 0) return `运行时长 ${h}小时 ${m}分`
  if (m > 0) return `运行时长 ${m}分 ${s}秒`
  return `运行时长 ${s}秒`
})

const currentDownRate = computed(() => (isRunning.value ? Number(telemetry.value?.down_rate || 0) : 0))
const currentUpRate = computed(() => (isRunning.value ? Number(telemetry.value?.up_rate || 0) : 0))
const peakDownRate = computed(() => Math.max(...downHistory.value, 0))

const cpuBarWidth = computed(() => {
  if (!isRunning.value) return 0
  return Math.min(100, Math.max(0, Number(telemetry.value?.cpu_percent || 0)))
})

const memBarWidth = computed(() => {
  if (!isRunning.value) return 0
  const pct = Number(telemetry.value?.memory_percent || 0)
  if (pct > 0) return Math.min(100, pct * 4)
  return telemetry.value?.memory_rss_bytes ? 12 : 0
})

const modeLabel = computed(() => {
  const m = props.status?.settings?.mode || 'rule'
  if (m === 'global') return 'Global 全局'
  if (m === 'direct') return 'Direct 直连'
  return 'Rule 模式'
})

const dnsModeLabel = computed(() => {
  const m = props.status?.settings?.dns_enhanced_mode || 'keep'
  if (m === 'fake-ip') return 'Fake-IP'
  if (m === 'redir-host') return 'Redir-Host'
  return 'DNS 保持原样'
})

const activeSubscription = computed(() => {
  const id = props.status?.active_subscription_id
  if (!id || !props.status?.subscriptions) return null
  return props.status.subscriptions.find((s) => s.id === id) || null
})

const subQuotaPct = computed(() => {
  const sub = activeSubscription.value
  if (!sub || !sub.total) return 0
  const used = (sub.upload || 0) + (sub.download || 0)
  return Math.min(100, Math.round((used / sub.total) * 100))
})

const portItems = computed(() => {
  const entries = (props.activePortEntries || []).map((e) => {
    const titleMap = {
      Mixed: 'Mixed 混合代理',
      HTTP: 'HTTP 代理',
      SOCKS5: 'SOCKS5 代理',
      DNS: 'DNS 监听端口'
    }
    return {
      key: e.label,
      title: titleMap[e.label] || e.label,
      address: e.address,
      badgeText: e.label === 'DNS' ? dnsModeLabel.value : isRunning.value ? '已监听' : '已配置',
      badgeClass: e.label === 'DNS' ? 'badge-brand' : isRunning.value ? 'badge-ok' : 'badge-neutral'
    }
  })
  const ctrlPort = props.status?.controller_port || 19090
  const ctrlAllowLAN = !!props.status?.controller_allow_lan
  const ctrlHost = ctrlAllowLAN ? props.status?.lan_ip || '127.0.0.1' : '127.0.0.1'
  entries.push({
    key: 'Controller',
    title: ctrlAllowLAN ? '管理 API (局域网)' : '管理 API (仅本机)',
    address: `${ctrlHost}:${ctrlPort}`,
    badgeText: ctrlAllowLAN ? '0.0.0.0' : '127.0.0.1',
    badgeClass: ctrlAllowLAN ? 'badge-warn' : 'badge-neutral'
  })
  return entries
})

const hoverInfo = computed(() => {
  if (!isRunning.value || hoverIndex.value < 0 || hoverIndex.value >= downHistory.value.length) return ''
  const secsAgo = (downHistory.value.length - 1 - hoverIndex.value) * 2
  const label = secsAgo === 0 ? '当前' : `-${secsAgo}s`
  return `${label} · ↓ ${fmtRate(downHistory.value[hoverIndex.value])} · ↑ ${fmtRate(upHistory.value[hoverIndex.value])}`
})

function fmtRate(bytesPerSec) {
  if (!isRunning.value) return '0 B/s'
  const val = Number(bytesPerSec) || 0
  if (val <= 0) return '0 B/s'
  return `${formatBytes(val)}/s`
}

function formatNumber(n) {
  return Number(n || 0).toLocaleString()
}

function formatTime(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString()
}

async function copyPort(entry) {
  const ok = await copyToClipboard(entry.address)
  if (ok) toast.success(`已复制 ${entry.title}：${entry.address}`)
  else toast.error('复制失败，请手动复制')
}

function togglePause() {
  paused.value = !paused.value
  if (!paused.value && props.active) {
    fetchTelemetry()
  }
}

async function fetchTelemetry() {
  if (activeRequest) {
    activeRequest.abort()
  }
  const ac = new AbortController()
  activeRequest = ac
  try {
    const data = await adminApi.mihomoTelemetry(ac.signal)
    if (ac.signal.aborted) return
    telemetry.value = data
    if (!paused.value) {
      downHistory.value = [...downHistory.value.slice(1), data.running ? Number(data.down_rate || 0) : 0]
      upHistory.value = [...upHistory.value.slice(1), data.running ? Number(data.up_rate || 0) : 0]
      drawChart()
    }
  } catch (err) {
    if (err?.name !== 'AbortError') {
      // Non-fatal background telemetry error
    }
  } finally {
    if (activeRequest === ac) activeRequest = null
  }
}

function startPolling() {
  stopPolling()
  if (!props.active || document.hidden) return
  fetchTelemetry()
  pollTimer = setInterval(() => {
    if (props.active && !document.hidden && !paused.value) {
      fetchTelemetry()
    }
  }, 2000)
}

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
  if (activeRequest) {
    activeRequest.abort()
    activeRequest = null
  }
}

function drawChart() {
  const canvas = chartCanvas.value
  if (!canvas) return
  const rect = canvas.getBoundingClientRect()
  if (!rect.width) return
  const dpr = window.devicePixelRatio || 1
  const w = Math.floor(rect.width)
  const h = 192
  canvas.width = w * dpr
  canvas.height = h * dpr
  const ctx = canvas.getContext('2d')
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0)

  const isLight = document.body.classList.contains('theme-light')
  const gridColor = isLight ? 'rgba(15, 23, 42, 0.08)' : 'rgba(255, 255, 255, 0.08)'
  const axisColor = isLight ? '#64748b' : '#94a3b8'
  const downStroke = '#10b981'
  const upStroke = '#6366f1'

  ctx.clearRect(0, 0, w, h)
  const pad = { top: 14, right: 10, bottom: 24, left: 54 }
  const plotW = Math.max(40, w - pad.left - pad.right)
  const plotH = Math.max(40, h - pad.top - pad.bottom)

  const downs = isRunning.value ? downHistory.value : new Array(60).fill(0)
  const ups = isRunning.value ? upHistory.value : new Array(60).fill(0)
  const maxY = Math.max(64 * 1024, ...downs, ...ups) * 1.15

  ctx.font = '11px var(--mono), monospace'
  ctx.textAlign = 'right'
  ctx.textBaseline = 'middle'
  for (let step = 0; step <= 4; step++) {
    const y = pad.top + (plotH * step) / 4
    ctx.strokeStyle = gridColor
    ctx.lineWidth = 1
    ctx.beginPath()
    ctx.moveTo(pad.left, y)
    ctx.lineTo(pad.left + plotW, y)
    ctx.stroke()

    const val = maxY * (1 - step / 4)
    ctx.fillStyle = axisColor
    ctx.fillText(formatAxisRate(val), pad.left - 8, y)
  }

  ctx.textAlign = 'center'
  ctx.textBaseline = 'top'
  const xTicks = [0, 29, 59]
  xTicks.forEach((idx, pos) => {
    const x = pad.left + (idx / 59) * plotW
    ctx.fillStyle = axisColor
    ctx.fillText(pos === 0 ? '-2m' : pos === 1 ? '-1m' : '现在', x, pad.top + plotH + 7)
  })

  function plotSeries(vals, stroke, fillTop, fillBottom, dashed) {
    const pts = vals.map((v, i) => ({
      x: pad.left + (i / (vals.length - 1)) * plotW,
      y: pad.top + plotH - (v / maxY) * plotH
    }))
    const grad = ctx.createLinearGradient(0, pad.top, 0, pad.top + plotH)
    grad.addColorStop(0, fillTop)
    grad.addColorStop(1, fillBottom)
    ctx.fillStyle = grad
    ctx.beginPath()
    ctx.moveTo(pts[0].x, pad.top + plotH)
    pts.forEach((p) => ctx.lineTo(p.x, p.y))
    ctx.lineTo(pts[pts.length - 1].x, pad.top + plotH)
    ctx.closePath()
    ctx.fill()

    ctx.save()
    ctx.strokeStyle = stroke
    ctx.lineWidth = 2
    if (dashed) ctx.setLineDash([5, 4])
    ctx.beginPath()
    pts.forEach((p, idx) => (idx === 0 ? ctx.moveTo(p.x, p.y) : ctx.lineTo(p.x, p.y)))
    ctx.stroke()
    ctx.restore()
  }

  plotSeries(downs, downStroke, 'rgba(16, 185, 129, 0.24)', 'rgba(16, 185, 129, 0.01)', false)
  plotSeries(ups, upStroke, 'rgba(99, 102, 241, 0.16)', 'rgba(99, 102, 241, 0.01)', true)

  if (hoverIndex.value >= 0 && hoverIndex.value < downs.length && isRunning.value) {
    const hx = pad.left + (hoverIndex.value / (downs.length - 1)) * plotW
    const hyD = pad.top + plotH - (downs[hoverIndex.value] / maxY) * plotH
    const hyU = pad.top + plotH - (ups[hoverIndex.value] / maxY) * plotH
    ctx.strokeStyle = isLight ? 'rgba(15, 23, 42, 0.24)' : 'rgba(255, 255, 255, 0.28)'
    ctx.lineWidth = 1
    ctx.beginPath()
    ctx.moveTo(hx, pad.top)
    ctx.lineTo(hx, pad.top + plotH)
    ctx.stroke()

    const hoverDots = [
      [hyD, downStroke],
      [hyU, upStroke]
    ]
    hoverDots.forEach(([py, color]) => {
      ctx.fillStyle = color
      ctx.beginPath()
      ctx.arc(hx, py, 3.5, 0, Math.PI * 2)
      ctx.fill()
    })
  }
}

function formatAxisRate(bytes) {
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(bytes >= 10 * 1024 * 1024 ? 0 : 1)}M`
  if (bytes >= 1024) return `${Math.round(bytes / 1024)}K`
  return `${Math.round(bytes)}B`
}

function onChartMove(ev) {
  const canvas = chartCanvas.value
  if (!canvas) return
  const rect = canvas.getBoundingClientRect()
  const padLeft = 54
  const plotW = Math.max(40, rect.width - padLeft - 10)
  const rel = (ev.clientX - rect.left - padLeft) / plotW
  hoverIndex.value = Math.max(0, Math.min(59, Math.round(rel * 59)))
  drawChart()
}

function onChartLeave() {
  hoverIndex.value = -1
  drawChart()
}

async function exportBackup() {
  backupBusy.value = true
  try {
    const data = await adminApi.exportMihomoBackup()
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const ts = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19)
    const link = document.createElement('a')
    link.href = url
    link.download = `gatehome-mihomo-backup-${ts}.json`
    document.body.appendChild(link)
    link.click()
    link.remove()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
    toast.success('Mihomo 配置备份已导出')
  } catch (err) {
    toast.error(err.message)
  } finally {
    backupBusy.value = false
  }
}

function triggerRestore() {
  restoreFileInput.value?.click()
}

async function handleRestoreFile(event) {
  const file = event.target.files?.[0]
  event.target.value = ''
  if (!file) return
  restoreBusy.value = true
  try {
    const text = await file.text()
    const parsed = JSON.parse(text)
    const res = await adminApi.restoreMihomoBackup(parsed)
    if (res.status) emit('updated', res.status)
    toast.success('Mihomo 配置已恢复并同步生效')
    await fetchTelemetry()
  } catch (err) {
    toast.error(err instanceof SyntaxError ? '备份文件不是有效的 JSON 格式' : err.message)
  } finally {
    restoreBusy.value = false
  }
}

function onVisibilityChange() {
  if (document.hidden) {
    stopPolling()
  } else if (props.active) {
    startPolling()
  }
}

watch(
  () => props.active,
  (isActive) => {
    if (isActive) {
      startPolling()
      setTimeout(drawChart, 40)
    } else {
      stopPolling()
    }
  }
)

watch(
  () => [props.status?.running, props.status?.core_version, props.status?.active_subscription_id],
  () => {
    if (props.active) fetchTelemetry()
  }
)

onMounted(() => {
  window.addEventListener('resize', drawChart)
  document.addEventListener('visibilitychange', onVisibilityChange)
  themeObserver = new MutationObserver(() => drawChart())
  themeObserver.observe(document.body, { attributes: true, attributeFilter: ['class'] })
  if (props.active) {
    startPolling()
  }
  drawChart()
})

onBeforeUnmount(() => {
  stopPolling()
  window.removeEventListener('resize', drawChart)
  document.removeEventListener('visibilitychange', onVisibilityChange)
  themeObserver?.disconnect()
})
</script>

<style scoped>
.overview-wrap {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 16px;
  min-width: 0;
  max-width: 100%;
}

.kpi-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
  min-width: 0;
}

.kpi-card {
  padding: 16px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  gap: 10px;
  min-width: 0;
}

.kpi-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px;
  row-gap: 4px;
}

.kpi-label {
  font-size: 12px;
  font-weight: 500;
  color: var(--muted);
  white-space: nowrap;
}

.kpi-foot-totals {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 2px 6px;
}

.nowrap-chunk {
  white-space: nowrap;
}

.kpi-body {
  display: grid;
  gap: 4px;
  min-width: 0;
}

.kpi-value {
  font-size: 21px;
  font-weight: 700;
  color: var(--ink);
  letter-spacing: -0.02em;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kpi-sub {
  font-size: 12px;
  color: var(--muted);
}

.kpi-rates {
  display: grid;
  gap: 4px;
}

.rate-line {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}

.rate-dir {
  font-size: 12px;
  font-weight: 600;
}

.rate-dir.down,
.down-text {
  color: var(--ok);
}

.rate-dir.up,
.up-text {
  color: var(--brand);
}

.rate-val {
  font-size: 19px;
  color: var(--ink);
}

.rate-val-sub {
  font-size: 14px;
  color: var(--ink-secondary);
}

.kpi-bars {
  display: grid;
  gap: 10px;
}

.bar-block {
  display: grid;
  gap: 4px;
}

.bar-label-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 2px 6px;
  font-size: 12px;
  color: var(--ink-secondary);
}

.bar-label-row strong {
  white-space: nowrap;
}

.progress-track {
  height: 6px;
  border-radius: 999px;
  background: var(--line);
  overflow: hidden;
}

.progress-fill {
  height: 100%;
  border-radius: 999px;
  transition: width 0.35s ease;
}

.progress-fill.cpu { background: var(--brand); }
.progress-fill.mem { background: var(--brand-cyan); }
.progress-fill.brand { background: var(--brand-gradient); }

.kpi-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 6px;
  padding-top: 8px;
  border-top: 1px solid var(--line);
  font-size: 11.5px;
  color: var(--muted);
  min-width: 0;
}

.kpi-foot.kpi-foot-totals {
  justify-content: flex-start;
  gap: 2px 6px;
}

.split-12-grid {
  display: grid;
  grid-template-columns: minmax(0, 8fr) minmax(0, 4fr);
  gap: 16px;
  min-width: 0;
}

.split-6-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
  min-width: 0;
}

.chart-panel,
.ports-panel,
.info-panel,
.actions-panel {
  padding: 18px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-width: 0;
}

.panel-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px;
}

.panel-title-wrap {
  display: grid;
  gap: 3px;
  min-width: 0;
}

.panel-head h2 {
  margin: 0;
  font-size: 15px;
  font-weight: 700;
}

.title-line {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.panel-sub {
  margin: 0;
  font-size: 12px;
  color: var(--muted);
}

.chart-legend-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
}

.legend-item {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  color: var(--ink-secondary);
}

.legend-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.legend-dot.down { background: var(--ok); }
.legend-dot.up { background: var(--brand); }

.chart-stage {
  position: relative;
  border-radius: var(--radius-md);
  border: 1px solid var(--line);
  background: var(--strip-bg);
  padding: 10px;
  min-width: 0;
}

.traffic-canvas {
  display: block;
  width: 100%;
  height: 192px;
}

.chart-tooltip {
  position: absolute;
  top: 10px;
  right: 10px;
  padding: 4px 10px;
  border-radius: 8px;
  background: var(--popover-bg);
  border: 1px solid var(--line);
  color: var(--ink);
  font-size: 11.5px;
  pointer-events: none;
  box-shadow: var(--shadow-card);
}

.chart-offline-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 12px 14px 12px 64px;
  text-align: center;
  background: rgba(15, 23, 42, 0.08);
  border-radius: var(--radius-md);
}

.chart-offline-pill {
  display: inline-block;
  max-width: 100%;
  padding: 6px 12px;
  border-radius: var(--radius-md);
  background: var(--card-bg);
  border: 1px solid var(--line);
  color: var(--muted);
  font-size: 12px;
  line-height: 1.45;
  box-shadow: var(--shadow-card);
}

.chart-summary-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
}

.summary-tile {
  display: grid;
  gap: 3px;
  padding: 10px 12px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--line);
  background: var(--strip-bg);
  min-width: 0;
}

.summary-label {
  font-size: 11px;
  color: var(--faint);
}

.summary-tile strong {
  font-size: 13.5px;
  color: var(--ink);
}

.ports-list {
  display: grid;
  gap: 8px;
  min-width: 0;
}

.port-row-btn {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  width: 100%;
  padding: 10px 12px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--line);
  background: var(--strip-bg);
  color: inherit;
  text-align: left;
  cursor: pointer;
  transition: var(--transition-smooth);
  min-width: 0;
}

.port-row-btn:hover {
  border-color: var(--brand);
}

.port-row-meta {
  display: grid;
  gap: 2px;
  min-width: 0;
}

.port-row-title {
  font-size: 12.5px;
  color: var(--ink);
}

.port-row-addr {
  font-size: 11.5px;
  color: var(--muted);
  overflow-wrap: anywhere;
}

.port-row-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.port-copy-icon {
  color: var(--faint);
}

.security-badges {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding-top: 8px;
  border-top: 1px solid var(--line);
}

.config-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  margin: 0;
}

.config-item {
  display: grid;
  gap: 6px;
  padding: 12px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--line);
  background: var(--strip-bg);
  min-width: 0;
}

.config-item dt {
  font-size: 11px;
  color: var(--faint);
}

.config-item dd {
  margin: 0;
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  font-size: 12.5px;
  color: var(--ink);
}

.config-sub {
  font-size: 11.5px;
  color: var(--muted);
  overflow-wrap: anywhere;
}

.quota-row {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  font-size: 11px;
  color: var(--muted);
}

.action-section {
  display: grid;
  gap: 10px;
}

.section-divider {
  border-top: 1px solid var(--line);
}

.action-box {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
  padding: 12px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--line);
  background: var(--strip-bg);
}

.action-box.stacked {
  flex-direction: column;
  align-items: stretch;
}

.action-box-copy {
  display: grid;
  gap: 4px;
  min-width: 0;
  flex: 1 1 220px;
}

.version-compare {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  font-size: 12.5px;
}

.ok-text {
  color: var(--ok);
}

.action-box-btns,
.backup-btn-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.backup-desc {
  margin: 0;
  font-size: 12px;
  line-height: 1.55;
  color: var(--muted);
}

.muted-tiny {
  font-size: 11.5px;
  color: var(--faint);
}

.badge-wrap {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.spinning {
  animation: overview-spin 0.85s linear infinite;
}

@keyframes overview-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 980px) {
  .kpi-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .split-12-grid,
  .split-6-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}

@media (max-width: 640px) {
  .kpi-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 10px;
  }

  .kpi-card,
  .chart-panel,
  .ports-panel,
  .info-panel,
  .actions-panel {
    padding: 12px;
  }

  .kpi-value {
    font-size: 17.5px;
  }

  .bar-label-row {
    font-size: 11.5px;
    gap: 2px 4px;
  }

  .bar-label-row span {
    white-space: nowrap;
  }

  .rate-val {
    font-size: 16.5px;
  }

  .chart-summary-grid,
  .config-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 420px) {
  .config-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
