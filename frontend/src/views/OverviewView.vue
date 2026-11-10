<template>
  <div class="report-page overview-page animate-fade-in">
    <header class="report-heading">
      <div>
        <h1>系统概览</h1>
        <p>网关状态、资源遥测与访问统计 · 每 5 秒采样</p>
      </div>
      <div class="report-actions">
        <button class="btn btn-secondary" :disabled="isRefreshing || isSavingConfig" @click="refresh(true)">
          {{ isRefreshing ? '刷新中…' : '刷新数据' }}</button
        ><button
          class="btn btn-secondary"
          :disabled="!gatewayConfigSnapshot || isSavingConfig"
          @click="configure"
        >
          配置组件与顺序</button
        ><RouterLink class="btn btn-primary" to="/routes">管理反向代理</RouterLink>
      </div>
    </header>
    <p v-if="loadError" class="report-error" role="alert">
      {{ loadError }}{{ dashboard ? '，保留上次采样' : '' }}
    </p>
    <p v-else class="report-note">
      {{
        dashboard
          ? `最近采样 ${reportDate(dashboard.sampled_at)} · ${resources.platform}`
          : '正在读取网关遥测…'
      }}
    </p>
    <div class="dashboard-grid">
      <SpotlightCard
        v-for="id in widgetIDs"
        :key="id"
        class="report-panel dashboard-widget"
        :class="{ wide: !compact.includes(id) }"
        :data-widget="id"
      >
        <div class="report-heading">
          <h2>{{ titles[id] }}</h2>
          <RouterLink v-if="id === 'services'" class="btn btn-ghost btn-sm" to="/routes">管理服务</RouterLink>
        </div>
        <template v-if="id === 'server'"
          ><div class="metric-value">{{ gatewayState.label }}</div>
          <p class="report-note">
            {{ gatewayVersion }} · {{ resources.platform || '等待采样' }} · {{ resources.cpus || '—' }} 个逻辑
            CPU
          </p>
          <p class="report-note">
            已运行
            {{
              gatewayStatus
                ? Math.floor(gatewayStatus.uptime_seconds / 3600) +
                  ' 小时 ' +
                  Math.floor((gatewayStatus.uptime_seconds % 3600) / 60) +
                  ' 分钟'
                : '—'
            }}
          </p>
          <p class="report-note">资源来自程序运行环境；Docker 的 /proc 可能反映宿主机资源。</p></template
        >
        <template v-else-if="id === 'cpu' || id === 'memory'"
          ><div class="metric-value">
            {{ metrics[id].percent == null ? '—' : metrics[id].percent.toFixed(1) + '%' }}
          </div>
          <progress
            v-if="metrics[id].percent != null"
            :value="metrics[id].percent"
            max="100"
            :aria-label="titles[id]"
          />
          <p class="report-note">{{ metrics[id].detail }}</p></template
        >
        <template v-else-if="id === 'storage'"
          ><div v-for="disk in resources.storage || []" :key="disk.name" class="storage-row">
            <strong>{{ disk.name }}文件系统</strong
            ><span>{{
              percent(disk.used, disk.total) == null ? '—' : percent(disk.used, disk.total).toFixed(1) + '%'
            }}</span
            ><progress
              v-if="disk.total"
              :value="disk.used"
              :max="disk.total"
              :aria-label="disk.name + '空间占用'"
            />
            <p class="report-note">
              {{ disk.error || `已用 ${bytes(disk.used)} · 可用 ${bytes(disk.available)}` }}
            </p>
          </div>
          <p v-if="!resources.storage?.length" class="report-note">等待存储采样</p></template
        >
        <template v-else-if="id === 'traffic' || id === 'transfer'"
          ><div class="transfer-pair">
            <div>
              <span>上传 TX</span
              ><strong>{{
                resources.network_error
                  ? '—'
                  : id === 'traffic'
                    ? rate(resources.tx_rate)
                    : bytes(resources.tx_bytes)
              }}</strong>
            </div>
            <div>
              <span>下载 RX</span
              ><strong>{{
                resources.network_error
                  ? '—'
                  : id === 'traffic'
                    ? rate(resources.rx_rate)
                    : bytes(resources.rx_bytes)
              }}</strong>
            </div>
          </div>
          <p class="report-note">
            {{
              resources.network_error ||
              `采集网卡 ${resources.interface || '自动选择'} · ${id === 'traffic' ? '两次采样间的实际速率，首次采样后显示' : '网卡启动以来的全部累计数据量'}`
            }}
          </p>
          <svg
            v-if="id === 'traffic' && samples.length > 1"
            viewBox="0 0 700 160"
            class="trend"
            role="img"
            aria-label="最近网卡速率趋势"
          >
            <polyline :points="networkLines.rx" fill="none" stroke="var(--brand-cyan)" stroke-width="2" />
            <polyline
              :points="networkLines.tx"
              fill="none"
              stroke="var(--brand-light)"
              stroke-width="2"
            /></svg
        ></template>
        <template v-else-if="id === 'requests'"
          ><div class="metric-value">{{ count(gatewayStatus?.requests) }}</div>
          <p class="report-note">本次启动后的业务请求；失败 {{ count(gatewayStatus?.failures) }}</p>
          <RouterLink class="report-note" to="/statistics"
            >24 小时保留记录 {{ count(access?.total) }} 次 · {{ count(regions.unique_ips) }} 个来源
            IP</RouterLink
          ></template
        >
        <template v-else-if="id === 'blocked'"
          ><div class="metric-value">{{ count(gatewayStatus?.blocked) }}</div>
          <p class="report-note">本次启动后的拦截数量</p>
          <RouterLink class="report-note" to="/firewall-logs"
            >保留记录：防火墙 {{ count(access?.firewall_blocked) }} · 冻结
            {{ count(access?.frozen_blocked) }} · 账号失败 {{ count(access?.auth_failed) }}</RouterLink
          ></template
        >
        <template v-else-if="id === 'trend'"
          ><div class="report-actions" role="group" aria-label="访问趋势时间范围">
            <button
              v-for="hours in [24, 6]"
              :key="hours"
              class="btn btn-secondary btn-sm"
              :aria-pressed="trendHours === hours"
              @click="trendHours = hours"
            >
              近 {{ hours }} 小时
            </button>
          </div>
          <svg v-if="access?.total" class="trend" viewBox="0 0 700 160" role="img" aria-label="访问数量趋势">
            <line v-for="y in [40, 80, 120]" :key="y" x1="0" :y1="y" x2="700" :y2="y" stroke="var(--line)" />
            <polyline :points="accessLine" fill="none" stroke="var(--brand-light)" stroke-width="2.5" />
          </svg>
          <p v-else class="report-empty">{{ access ? '最近 24 小时暂无访问记录' : '等待访问统计' }}</p>
          <p class="report-note">所选时段 {{ count(trendTotal) }} 次访问 · 数据基于保留日志</p></template
        >
        <template v-else-if="id === 'provinces'"
          ><p class="report-note">
            {{ count(regions.unique_ips) }} 个来源 IP · {{ count(regions.sample) }} 次访问
          </p>
          <div class="scroll-list">
            <div v-for="province in regions.provinces || []" :key="province.name" class="province-row">
              <strong>{{ province.name }}</strong
              ><meter
                min="0"
                :max="Math.max(1, regions.provinces?.[0]?.requests || 1)"
                :value="province.requests"
                :aria-label="province.name + '访问次数'"
              /><span>{{ count(province.requests) }} 次 / {{ count(province.ips) }} IP</span>
            </div>
          </div>
          <p v-if="!regions.provinces?.length" class="report-empty">暂无可归属省份的访问记录</p>
          <p class="report-note">
            IP 离线估算 · 未定位 {{ count(regions.unmapped) }} 次，包含内网及境外地址。
          </p></template
        >
        <template v-else-if="id === 'map'"
          ><svg class="access-map" viewBox="0 0 520 320" role="img" aria-label="中国访问省份汇聚示意地图">
            <title>
              省份示意位置到{{
                gatewayConfig?.dashboard?.map_province || '示意中心'
              }}的访问，非精确位置或实际网络线路
            </title>
            <image :href="'/china-outline.svg'" width="520" height="320" />
            <g v-for="visit in mapVisits" :key="visit.id" :class="visit.blocked ? 'blocked' : 'allowed'">
              <path :d="visit.path" fill="none" stroke-width="1.5" />
              <circle :cx="visit.point[0]" :cy="visit.point[1]" r="3.5" />
              <title>
                {{ visit.ip }} · {{ visit.province }} · {{ reportDate(visit.time) }} ·
                {{ visit.blocked ? '拦截' : '普通访问' }}
              </title>
            </g>
            <circle :cx="mapHub[0]" :cy="mapHub[1]" r="6" fill="var(--brand)" />
          </svg>
          <p class="report-note">
            {{ gatewayConfig?.dashboard?.map_province || '示意中心' }} · 蓝色为普通访问，红色为拦截 ·
            {{ mapVisits.length }} 条已定位记录
          </p>
          <div class="scroll-list">
            <div v-for="visit in (regions.visits || []).slice(0, 5)" :key="visit.id" class="event-row">
              <strong>{{ visit.ip }} · {{ visit.province || visit.region }}</strong
              ><span>{{ reportDate(visit.time) }}</span>
            </div>
          </div>
          <p v-if="!regions.visits?.length" class="report-note">等待最近访问记录。</p></template
        >
        <template v-else-if="id === 'services'"
          ><p v-if="healthError" class="report-error" role="alert">
            健康状态读取失败，保留上次结果：{{ healthError }}
          </p>
          <div class="scroll-list">
            <div v-for="route in serviceRoutes" :key="route.id" class="service-row">
              <strong>{{ route.name || route.host }}</strong
              ><span class="report-note">{{
                health?.routes?.[route.id]?.label || (!route.enabled ? '路由已停用' : '等待上游检测')
              }}</span
              ><a
                v-if="serviceURL(route.upstream)"
                :href="serviceURL(route.upstream)"
                target="_blank"
                rel="noopener noreferrer"
                class="service-link"
                >内网：{{ route.upstream }}</a
              ><a
                v-if="route.publicURL"
                :href="route.publicURL"
                target="_blank"
                rel="noopener noreferrer"
                class="service-link"
                >域名：{{ route.publicURL }}</a
              >
            </div>
          </div>
          <p v-if="!serviceRoutes.length" class="report-empty">暂无反向代理服务</p></template
        >
        <template v-else-if="id === 'listeners'"
          ><div class="scroll-list">
            <div v-for="group in gatewayConfig?.groups || []" :key="group.id" class="event-row">
              <strong>{{ group.name }} · {{ group.enabled ? '启用' : '停用' }}</strong
              ><span>配置 HTTP {{ group.http_port || '关闭' }} / HTTPS {{ group.https_port || '关闭' }}</span>
            </div>
            <div
              v-for="forward in gatewayStatus?.forward_listeners || []"
              :key="forward.id"
              class="event-row"
            >
              <strong>{{ forward.name }} · {{ forward.protocol.toUpperCase() }}</strong
              ><span>实际监听 :{{ forward.listen_port }} → {{ forward.upstream }}</span>
            </div>
          </div>
          <p v-if="gatewayStatus?.restart_required" class="report-warning">监听配置待重启，请到设置查看。</p>
          <RouterLink class="btn btn-secondary btn-sm" to="/settings">监听与服务设置</RouterLink></template
        >
        <template v-else-if="id === 'events'"
          ><div class="scroll-list">
            <div v-for="(event, index) in gatewayStatus?.events || []" :key="index" class="event-row">
              <strong :class="{ 'report-error': !event.ok }"
                >{{ event.kind }} · {{ event.ok ? '成功' : '失败' }}</strong
              ><span>{{ event.message }}</span
              ><small>{{ reportDate(event.time) }}</small>
            </div>
          </div>
          <p v-if="!gatewayStatus?.events?.length" class="report-empty">暂无近期任务活动</p>
          <RouterLink class="btn btn-secondary btn-sm" to="/logs">查看项目日志</RouterLink></template
        >
      </SpotlightCard>
    </div>
    <p v-if="!widgetIDs.length" class="report-empty">尚未显示组件，请通过「配置组件与顺序」选择。</p>
    <dialog
      ref="dialog"
      class="report-dialog"
      aria-labelledby="dashboard-title"
      @cancel="isSavingConfig && $event.preventDefault()"
      @close="editSnapshot = null"
    >
      <header>
        <h2 id="dashboard-title">概览组件与顺序</h2>
        <button class="btn btn-secondary btn-sm" :disabled="isSavingConfig" @click="dialog.close()">
          关闭
        </button>
      </header>
      <form @submit.prevent="saveLayout">
        <fieldset :disabled="isSavingConfig">
          <p class="report-note">
            勾选显示组件。拖动行首按钮，或使用方向键、上下移动按钮排序；保存后在其他设备与重启后沿用。
          </p>
          <div
            v-for="(id, index) in order"
            :key="id"
            class="layout-row"
            :data-layout-id="id"
            @dragover.prevent
            @drop.prevent="drop(id)"
          >
            <button
              type="button"
              draggable="true"
              class="btn btn-ghost btn-sm drag-handle"
              :aria-label="'拖动或方向键排序 ' + titles[id]"
              @dragstart="startDrag($event, id)"
              @dragend="dragged = ''"
              @keydown.up.prevent="move(id, -1)"
              @keydown.down.prevent="move(id, 1)"
            >
              <GripVertical :size="15" /></button
            ><label class="check-label"
              ><input v-model="selected" type="checkbox" :value="id" :name="'widget_' + id" /><span>{{
                titles[id]
              }}</span></label
            ><div class="order-btn-group"><button
              type="button"
              class="btn btn-secondary btn-sm"
              :aria-label="'上移 ' + titles[id]"
              :disabled="index === 0"
              @click="move(id, -1)"
            >
              <ArrowUp :size="13" /></button
            ><button
              type="button"
              class="btn btn-secondary btn-sm"
              :aria-label="'下移 ' + titles[id]"
              :disabled="index === order.length - 1"
              @click="move(id, 1)"
            >
              <ArrowDown :size="13" />
            </button></div>
          </div>
          <label class="form-field"
            >流量采集网卡<select
              v-model="form.network_interface"
              name="network_interface"
              class="form-select"
            >
              <option value="">自动选择</option>
              <option v-for="network in resources.networks || []" :key="network.name" :value="network.name">
                {{ network.name }}
              </option>
              <option
                v-if="
                  form.network_interface &&
                  !resources.networks?.some((network) => network.name === form.network_interface)
                "
                :value="form.network_interface"
              >
                {{ form.network_interface }} · 当前不可用
              </option>
            </select></label
          ><label class="form-field"
            >地图汇聚省份<select v-model="form.map_province" name="map_province" class="form-select">
              <option value="">示意中心</option>
              <option v-for="province in Object.keys(provincePoints)" :key="province" :value="province">
                {{ province }}
              </option>
            </select></label
          >
          <p v-if="formError" class="report-error" role="alert">{{ formError }}</p>
          <footer>
            <button class="btn btn-secondary" type="button" @click="defaults">恢复默认布局</button
            ><button class="btn btn-primary" type="submit">
              {{ isSavingConfig ? '保存中…' : '保存概览布局' }}
            </button>
          </footer>
        </fieldset>
      </form>
    </dialog>
  </div>
</template>
<script setup>
import { computed, nextTick, onMounted, onBeforeUnmount, reactive, ref, shallowRef } from 'vue'
import { GripVertical, ArrowUp, ArrowDown } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { adminApi } from '@/api/admin'
import { startPolling } from '@/api/polling'
import {
  dashboardWidgets,
  dashboardWidgetIDs,
  dashboardRecord,
  reorderWidget,
  provincePoints,
  mapPoint,
  serviceURL
} from '@/api/dashboard'
import { formatBytes, reportDate } from '@/api/reports'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useToast } from '@/composables/useToast'
import '@/styles/reports.css'
const {
  gatewayStatus,
  gatewayConfig,
  gatewayConfigSnapshot,
  gatewayState,
  gatewayVersion,
  refreshStatus,
  refreshConfig,
  saveConfig,
  isSavingConfig
} = useGatewayStatus()
const toast = useToast(),
  controller = new AbortController(),
  dialog = ref(null)
const dashboard = shallowRef(null),
  health = shallowRef(null),
  samples = ref([]),
  isRefreshing = ref(false),
  loadError = ref(''),
  healthError = ref(''),
  trendHours = ref(24)
const resources = computed(() => dashboard.value?.resources || {}),
  access = computed(() => dashboard.value?.access),
  regions = computed(() => dashboard.value?.regions || {})
const titles = Object.fromEntries(dashboardWidgets.map((item) => [item.id, item.title])),
  compact = ['cpu', 'memory', 'requests', 'blocked']
const widgetIDs = computed(() => dashboardWidgetIDs(gatewayConfig.value?.dashboard))
const form = reactive({ network_interface: '', map_province: '' }),
  order = ref([]),
  selected = ref([]),
  formError = ref(''),
  dragged = ref('')
let editSnapshot = null,
  timer
function count(value) {
  return value == null ? '—' : value.toLocaleString()
}
function bytes(value) {
  return Number.isFinite(value) ? formatBytes(value) : '—'
}
function rate(value) {
  return Number.isFinite(value) ? bytes(value) + '/s' : '—'
}
function percent(used, total) {
  return Number.isFinite(used) && total > 0 ? Math.max(0, Math.min(100, (used / total) * 100)) : null
}
const metrics = computed(() => ({
  cpu: {
    percent: resources.value.cpu_percent,
    detail: resources.value.cpu_error || '整体 CPU 使用率，两次采样差值；首次采样后显示。'
  },
  memory: {
    percent: resources.value.memory_error
      ? null
      : percent(resources.value.memory_used, resources.value.memory_total),
    detail:
      resources.value.memory_error ||
      `${bytes(resources.value.memory_used)} / ${bytes(resources.value.memory_total)}`
  }
}))
function line(values, max) {
  return values
    .map((value, index) => `${(700 * index) / Math.max(1, values.length - 1)},${152 - (144 * value) / max}`)
    .join(' ')
}
const networkLines = computed(() => {
  const rx = samples.value.map((item) => item.rx),
    tx = samples.value.map((item) => item.tx),
    max = Math.max(1, ...rx, ...tx)
  return { rx: line(rx, max), tx: line(tx, max) }
})
const trend = computed(() => (access.value?.trend || []).slice(-trendHours.value)),
  trendTotal = computed(() =>
    access.value ? trend.value.reduce((sum, bucket) => sum + bucket.total, 0) : null
  )
const accessLine = computed(() => {
  const values = trend.value.map((item) => item.total)
  return line(values, Math.max(1, ...values))
})
const mapHub = computed(() => mapPoint(gatewayConfig.value?.dashboard?.map_province))
const mapVisits = computed(() =>
  (regions.value.visits || [])
    .filter((visit) => provincePoints[visit.province])
    .map((visit) => {
      const point = mapPoint(visit.province),
        [x, y] = point,
        [hx, hy] = mapHub.value
      return {
        ...visit,
        point,
        path: `M${x},${y} Q${(x + hx) / 2},${(y + hy) / 2 - Math.min(65, Math.hypot(x - hx, y - hy) / 3)} ${hx},${hy}`
      }
    })
)
const serviceRoutes = computed(() =>
  (gatewayConfig.value?.routes || []).map((route) => {
    const group = gatewayConfig.value.groups.find((item) => item.id === route.group_id),
      secure = route.tls || !group?.http_port,
      port = secure ? group?.https_port : group?.http_port
    return {
      ...route,
      publicURL: port ? serviceURL(`${secure ? 'https' : 'http'}://${route.host}:${port}/`) : ''
    }
  })
)
function recordSample(data) {
  const { interface: iface, rx_rate: rx, tx_rate: tx, network_error: error } = data.resources
  const last = samples.value.at(-1),
    time = Date.parse(data.sampled_at)
  if (error || !Number.isFinite(rx) || !Number.isFinite(tx)) {
    samples.value = []
    return
  }
  if (last?.iface !== iface) samples.value = []
  if (samples.value.length && time - last.time < 5000) return
  samples.value = [...samples.value.slice(-23), { iface, time, rx, tx }]
}
async function refresh(manual = false) {
  if (isRefreshing.value) return
  isRefreshing.value = true
  const signal = controller.signal
  try {
    const tasks = [
      adminApi
        .dashboard(signal)
        .then((data) => {
          if (!signal.aborted) {
            dashboard.value = data
            recordSample(data)
            loadError.value = ''
          }
        })
        .catch((err) => {
          if (!signal.aborted) loadError.value = err.message
        }),
      adminApi
        .serviceHealth(signal)
        .then((data) => {
          if (!signal.aborted) {
            health.value = data
            healthError.value = ''
          }
        })
        .catch((err) => {
          if (!signal.aborted) healthError.value = err.message
        })
    ]
    if (manual) tasks.push(refreshStatus(signal), refreshConfig(signal))
    const results = await Promise.allSettled(tasks)
    const failure = results.find(result => result.status === 'rejected')
    if (failure && !signal.aborted) loadError.value ||= failure.reason.message
  } finally {
    isRefreshing.value = false
  }
}
async function configure() {
  editSnapshot = structuredClone(gatewayConfigSnapshot.value)
  selected.value = [...dashboardWidgetIDs(editSnapshot.config.dashboard)]
  order.value = [
    ...selected.value,
    ...dashboardWidgets.map((item) => item.id).filter((id) => !selected.value.includes(id))
  ]
  Object.assign(form, {
    network_interface: editSnapshot.config.dashboard?.network_interface || '',
    map_province: editSnapshot.config.dashboard?.map_province || ''
  })
  formError.value = ''
  await nextTick()
  dialog.value.showModal()
}
function defaults() {
  order.value = dashboardWidgets.map((item) => item.id)
  selected.value = [...order.value]
  Object.assign(form, { network_interface: '', map_province: '' })
}
async function move(id, direction) {
  const index = order.value.indexOf(id),
    target = index + direction
  if (target < 0 || target >= order.value.length) return
  const next = [...order.value]
  ;[next[index], next[target]] = [next[target], next[index]]
  order.value = next
  await nextTick()
  dialog.value.querySelector(`[data-layout-id="${id}"] [draggable]`)?.focus()
}
function startDrag(event, id) {
  dragged.value = id
  event.dataTransfer.setData('text/plain', id)
  event.dataTransfer.effectAllowed = 'move'
}
function drop(id) {
  order.value = reorderWidget(order.value, dragged.value, id)
  dragged.value = ''
}
async function saveLayout() {
  formError.value = ''
  try {
    await saveConfig(editSnapshot, dashboardRecord(editSnapshot, form, order.value, selected.value))
    dialog.value.close()
    toast.success('概览布局已保存')
    samples.value = []
    await refresh(true)
  } catch (err) {
    if (!controller.signal.aborted) formError.value = err.message
  }
}
onMounted(() => {
  refresh(true)
  timer = startPolling(() => refresh(), 5000)
})
onBeforeUnmount(() => {
  controller.abort()
  timer?.()
  editSnapshot = null
})
</script>
<style scoped>
.dashboard-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  grid-auto-flow: dense;
  gap: 18px;
  align-items: stretch;
}
.dashboard-widget {
  min-width: 0;
  display: flex;
  flex-direction: column;
  height: 100%;
}
.dashboard-widget.wide {
  grid-column: span 2;
}
.dashboard-widget :deep(.card-inner) {
  display: flex;
  flex-direction: column;
  gap: 14px;
  height: 100%;
  box-sizing: border-box;
}
.dashboard-widget :deep(.report-note:last-child) {
  margin-top: auto;
}
h2 {
  margin-bottom: 0;
}
.metric-value {
  font-size: 27px;
  font-weight: 700;
  color: var(--ink);
}
progress,
meter {
  width: 100%;
  height: 8px;
  accent-color: var(--brand);
}
.transfer-pair {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.transfer-pair > div {
  display: grid;
  gap: 10px;
}
.transfer-pair span {
  font-size: 12px;
  color: var(--muted);
}
.transfer-pair strong {
  font-size: 20px;
  color: var(--ink);
  overflow-wrap: anywhere;
}
.storage-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  font-size: 12px;
}
.storage-row progress,
.storage-row p {
  grid-column: 1/-1;
}
.scroll-list {
  display: grid;
  gap: 12px;
  max-height: 350px;
  overflow: auto;
}
.event-row,
.service-row {
  display: grid;
  gap: 6px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--line);
  font-size: 12px;
  overflow-wrap: anywhere;
}
.event-row span,
.event-row small,
.service-link {
  color: var(--muted);
}
.service-link:hover {
  color: var(--brand-light);
}
.province-row {
  display: grid;
  grid-template-columns: 46px minmax(30px, 1fr) auto;
  gap: 10px;
  align-items: center;
  font-size: 12px;
}
.trend {
  width: 100%;
  height: 165px;
}
.access-map {
  width: 100%;
  max-height: 320px;
}
.access-map .allowed {
  stroke: var(--brand-cyan);
  fill: var(--brand-cyan);
}
.access-map .blocked {
  stroke: var(--bad);
  fill: var(--bad);
}
.layout-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  background: var(--strip-bg);
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  gap: 10px;
  transition: var(--transition-smooth);
}
.layout-row:hover {
  background: var(--card-hover-bg);
  border-color: var(--card-border-hover);
}
.drag-handle {
  color: var(--faint);
  display: flex;
  align-items: center;
  cursor: grab;
  padding: 4px;
}
.drag-handle:hover {
  color: var(--ink);
}
.order-btn-group {
  display: flex;
  gap: 4px;
}
[draggable] {
  cursor: grab;
  touch-action: none;
}
.btn[aria-pressed='true'] {
  border-color: var(--brand);
}
button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
@media (max-width: 1399px) {
  .dashboard-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 16px;
  }
}
@media (max-width: 767px) {
  .dashboard-grid {
    grid-template-columns: 1fr;
    gap: 14px;
  }
  .dashboard-widget.wide {
    grid-column: 1 / -1 !important;
  }
  .province-row {
    grid-template-columns: 40px minmax(24px, 1fr) auto;
  }
  .overview-page .report-actions {
    display: grid !important;
    grid-template-columns: 1fr 1fr !important;
    gap: 8px !important;
    width: 100% !important;
  }
  .overview-page .report-actions .btn:last-child {
    grid-column: 1 / -1 !important;
  }
  .overview-page .report-actions .btn {
    justify-content: center !important;
    width: 100% !important;
    padding-inline: 8px !important;
    font-size: 12px !important;
  }
}
</style>
