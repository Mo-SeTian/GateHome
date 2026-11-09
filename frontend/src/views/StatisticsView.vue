<template>
  <div class="report-page animate-fade-in">
    <div class="report-heading">
      <div>
        <h1>访问统计</h1>
        <p>按保留日志核对请求、账号验证和安全事件，趋势显示实际请求次数</p>
      </div>
      <button class="btn btn-secondary" :disabled="!data || loading || !!error" @click="exportSVG">
        导出图表 SVG
      </button>
    </div>
    <form class="report-form" @submit.prevent="load">
      <label
        >统计时间<select v-model="form.hours" name="hours" class="form-select">
          <option v-for="item in ranges" :key="item.value" :value="item.value">{{ item.label }}</option>
        </select></label
      >
      <label
        >反代规则<select v-model="form.rule" name="rule" class="form-select">
          <option value="">全部反代规则</option>
          <option v-for="route in gatewayConfig?.routes" :key="routeKey(route)" :value="routeKey(route)">
            {{ ruleName(routeKey(route)) }}
          </option>
        </select></label
      >
      <button type="submit" class="btn btn-primary" :disabled="loading">
        {{ loading ? '查询中…' : '查询统计' }}
      </button>
    </form>
    <p v-if="error" ref="errorElement" class="report-error" role="alert" tabindex="-1">
      {{ error }}{{ data ? '。当前显示上次成功查询的统计。' : '' }}
    </p>
    <p v-if="dirty && data" class="report-note">筛选条件尚未应用，当前仍显示上次查询结果。</p>
    <template v-if="data">
      <p class="report-note">
        统计范围 {{ reportDate(data.from) }} — {{ reportDate(data.to) }}；最早保留记录
        {{ reportDate(data.retained_from) }}，当前保留
        {{ data.retained_entries.toLocaleString() }} 条项目与反代日志。日志清理后的记录不包含在统计中。
      </p>
      <p v-if="data.write_error || data.freeze_write_error" class="report-warning" role="alert">
        {{ data.write_error ? '日志写入或清理失败，统计可能仅反映内存记录。' : ''
        }}{{ data.freeze_write_error ? '冻结记录保存失败，最新变更可能无法在重启后恢复。' : '' }}
      </p>
      <div class="report-metrics">
        <SpotlightCard v-for="metric in primaryMetrics" :key="metric.key" class="report-metric"
          ><span class="metric-label">{{ metric.label }}</span>
          <div class="metric-value" :data-metric="metric.key">
            {{ data[metric.key].toLocaleString() }}
          </div></SpotlightCard
        >
      </div>
      <p class="report-note">
        正常为 HTTP
        1xx–3xx；安全拦截为防火墙和冻结拒绝；其他失败包含未验证请求、错误账号与后端错误。验证和冻结指标是请求总数的子集，不能相加。TLS
        握手失败不产生 HTTP 记录。
      </p>
      <div ref="charts" class="report-page">
        <SpotlightCard class="report-panel"
          ><h2>访问趋势 · {{ formDescription }}</h2>
          <div class="chart-legend">
            <span v-for="series in seriesList" :key="series.key"
              ><i :style="{ background: series.color }" />{{ series.label }}</span
            >
          </div>
          <svg
            :viewBox="`0 0 ${chartWidth} 270`"
            class="statistics-chart"
            role="img"
            aria-label="访问趋势"
            data-chart="访问趋势"
          >
            <title>访问趋势，共 {{ data.total }} 次请求</title>
            <g v-for="ratio in [0, 0.5, 1]" :key="ratio">
              <line
                x1="40"
                :x2="chartWidth - 20"
                :y1="220 - ratio * 170"
                :y2="220 - ratio * 170"
                stroke="var(--line)"
              />
              <text x="34" :y="225 - ratio * 170" text-anchor="end" fill="var(--muted)" font-size="12">
                {{ Math.round(maximum * ratio) }}
              </text>
            </g>
            <g v-for="series in seriesList" :key="series.key">
              <polyline
                :points="trendPoints(series.key)"
                fill="none"
                :stroke="series.color"
                stroke-width="2.5"
              />
              <circle
                v-for="(bucket, index) in data.trend"
                :key="bucket.time"
                :cx="trendX(index)"
                :cy="220 - (bucket[series.key] * 170) / maximum"
                r="3"
                :fill="series.color"
              >
                <title>{{ reportDate(bucket.time) }} · {{ series.label }} {{ bucket[series.key] }} 次</title>
              </circle>
            </g>
            <text
              v-for="index in axisIndices"
              :key="index"
              :x="trendX(index)"
              y="252"
              :text-anchor="index === 0 ? 'start' : index === data.trend.length - 1 ? 'end' : 'middle'"
              fill="var(--muted)"
              font-size="12"
            >
              {{ axisDate(data.trend[index].time) }}
            </text>
          </svg>
        </SpotlightCard>
        <div class="chart-grid">
          <SpotlightCard class="report-panel"
            ><h2>HTTP 状态码分布</h2>
            <svg
              :viewBox="`0 0 ${chartWidth} 260`"
              class="statistics-chart"
              role="img"
              aria-label="HTTP 状态码分布"
              data-chart="HTTP 状态码分布"
            >
              <title>HTTP 状态码分布</title>
              <g v-for="(status, index) in data.statuses" :key="status.label">
                <rect
                  :x="40 + index * statusStep"
                  :y="210 - (status.count * 150) / statusMaximum"
                  :width="statusStep * 0.6"
                  :height="(status.count * 150) / statusMaximum"
                  rx="4"
                  :fill="['4xx', '5xx'].includes(status.label) ? 'var(--warn)' : 'var(--ok)'"
                />
                <text
                  :x="40 + (index + 0.3) * statusStep"
                  :y="200 - (status.count * 150) / statusMaximum"
                  text-anchor="middle"
                  fill="var(--muted)"
                  font-size="14"
                >
                  {{ status.count }}
                </text>
                <text
                  :x="40 + (index + 0.3) * statusStep"
                  y="240"
                  text-anchor="middle"
                  fill="var(--muted)"
                  font-size="14"
                >
                  {{ status.label }}
                </text>
              </g>
            </svg></SpotlightCard
          >
          <SpotlightCard class="report-panel"
            ><h2>反代规则访问排行 · 前 8 条</h2>
            <svg
              :viewBox="`0 0 ${chartWidth} ${Math.max(130, topRules.length * rankHeight + 20)}`"
              class="statistics-chart"
              role="img"
              aria-label="反代规则排行"
              data-chart="反代规则访问排行"
            >
              <title>反代规则访问排行</title>
              <g v-for="(row, index) in topRules" :key="row.rule">
                <text x="12" :y="index * rankHeight + (narrow ? 20 : 30)" fill="var(--muted)" font-size="13">
                  <title>{{ ruleName(row.rule) }}</title>
                  {{ shortRule(row.rule) }}
                </text>
                <rect
                  v-for="(segment, part) in rankSegments(row)"
                  :key="part"
                  :x="segment.x"
                  :y="index * rankHeight + (narrow ? 32 : 12)"
                  :width="segment.width"
                  height="24"
                  :fill="segment.color"
                >
                  <title>{{ segment.label }} {{ segment.value }} 次</title>
                </rect>
                <text
                  :x="rankStart + (row.total * rankSpan) / rankMaximum + 8"
                  :y="index * rankHeight + (narrow ? 50 : 30)"
                  fill="var(--muted)"
                  font-size="13"
                >
                  {{ row.total }}
                </text>
              </g>
              <text v-if="!topRules.length" x="20" y="60" fill="var(--muted)" font-size="14">
                暂无访问记录
              </text>
            </svg></SpotlightCard
          >
        </div>
      </div>
      <h2 class="section-title">账号验证与安全防护</h2>
      <div class="report-metrics">
        <SpotlightCard v-for="metric in securityMetrics" :key="metric.key" class="report-metric"
          ><span class="metric-label">{{ metric.label }}</span>
          <div class="metric-value" :data-metric="metric.key">
            {{ data[metric.key].toLocaleString() }}
          </div></SpotlightCard
        >
      </div>
      <p class="report-note">
        账号验证仅统计 GateHome 独立服务账号，后端返回 401 不会算作账号验证失败。当前冻结 IP
        为实时值，不受所选时间范围限制。
      </p>
      <SpotlightCard class="report-panel"
        ><h2>安全规则命中汇总</h2>
        <div v-if="data.security_rules?.length" class="report-table-wrap">
          <table class="report-table">
            <thead>
              <tr>
                <th>检测引擎 / 规则</th>
                <th>命中请求</th>
                <th>拦截请求</th>
                <th>最近命中</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in data.security_rules" :key="row.engine + '/' + row.rule_id + '/' + row.name">
                <td>
                  {{ engineLabel(row.engine) }} #{{ row.rule_id }}<small>{{ row.name }}</small>
                </td>
                <td>{{ row.count }}</td>
                <td>{{ row.blocked }}</td>
                <td>{{ reportDate(row.last_seen) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-else class="report-note">暂无安全规则命中。</p></SpotlightCard
      >
      <SpotlightCard class="report-panel"
        ><h2>防火墙策略命中汇总</h2>
        <div v-if="data.firewalls.length" class="report-table-wrap">
          <table class="report-table">
            <thead>
              <tr>
                <th>防火墙 / IP 组</th>
                <th>原因</th>
                <th>拦截请求</th>
                <th>最近命中</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, index) in data.firewalls" :key="index">
                <td>
                  {{ row.name || '历史规则'
                  }}<small
                    >{{ row.group || row.kind }}{{ row.order ? ' · 第 ' + row.order + ' 组' : '' }}</small
                  >
                </td>
                <td>{{ row.reason }}</td>
                <td>{{ row.count }}</td>
                <td>{{ reportDate(row.last_seen) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-else class="report-note">暂无防火墙拦截。</p></SpotlightCard
      >
    </template>
    <LogExplorer scope="security" title="安全事件明细" />
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, shallowRef } from 'vue'
import SpotlightCard from '@/components/SpotlightCard.vue'
import LogExplorer from '@/components/LogExplorer.vue'
import { adminApi } from '@/api/admin'
import { routeKey } from '@/api/configuration'
import { engineLabel, reportDate } from '@/api/reports'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useToast } from '@/composables/useToast'
import '@/styles/reports.css'

const { gatewayConfig, refreshConfig } = useGatewayStatus()
const toast = useToast()
const form = ref({ hours: '24', rule: '' }),
  applied = shallowRef({ hours: '24', rule: '' })
const data = shallowRef(null),
  loading = ref(false),
  error = ref(''),
  errorElement = ref(null),
  charts = ref(null)
const ranges = [
  { value: '24', label: '最近 24 小时' },
  { value: '168', label: '最近 7 天' },
  { value: '720', label: '最近 30 天' }
]
const primaryMetrics = [
  { key: 'total', label: '请求总数' },
  { key: 'normal', label: '正常访问' },
  { key: 'blocked', label: '安全拦截' },
  { key: 'failed', label: '其他失败' }
]
const securityMetrics = Object.entries({
  waf_matched: 'WAF 命中请求',
  waf_blocked: 'WAF 拦截',
  rate_blocked: '访问限速拦截',
  custom_blocked: 'HTTP 规则拦截',
  auth_success: '账号验证成功',
  auth_failed: '账号验证失败',
  auth_required: '等待账号验证',
  auth_limited: '验证速率限制',
  firewall_blocked: '防火墙拦截',
  frozen_blocked: '冻结 IP 拦截',
  freeze_created: '新增自动冻结',
  active_freezes: '当前冻结 IP'
}).map(([key, label]) => ({ key, label }))
const seriesList = [
  { key: 'normal', label: '正常访问', color: 'var(--ok)' },
  { key: 'blocked', label: '安全拦截', color: 'var(--bad)' },
  { key: 'failed', label: '其他失败', color: 'var(--warn)' }
]
const media = matchMedia('(max-width: 600px)'),
  narrow = ref(media.matches)
const chartWidth = computed(() => (narrow.value ? 400 : 800))
const dirty = computed(() => JSON.stringify(form.value) !== JSON.stringify(applied.value))
const formDescription = computed(() => (applied.value.hours === '24' ? '每小时请求次数' : '每天请求次数'))
const maximum = computed(() =>
  Math.max(1, ...(data.value?.trend ?? []).flatMap((row) => seriesList.map((series) => row[series.key])))
)
const axisIndices = computed(() =>
  [
    ...new Set(
      narrow.value
        ? [0, data.value.trend.length - 1]
        : [0, Math.floor(data.value.trend.length / 2), data.value.trend.length - 1]
    )
  ].filter((index) => data.value.trend[index])
)
const statusStep = computed(() => (chartWidth.value - 60) / Math.max(1, data.value?.statuses.length || 0))
const statusMaximum = computed(() => Math.max(1, ...(data.value?.statuses ?? []).map((row) => row.count)))
const topRules = computed(() => data.value?.rules.slice(0, 8) ?? [])
const rankMaximum = computed(() => Math.max(1, ...topRules.value.map((row) => row.total)))
const rankStart = computed(() => (narrow.value ? 20 : 300)),
  rankSpan = computed(() => chartWidth.value - (narrow.value ? 90 : 340)),
  rankHeight = computed(() => (narrow.value ? 80 : 58))
const controller = new AbortController()
let version = 0
function trendX(index) {
  return 40 + (index * (chartWidth.value - 60)) / Math.max(1, data.value.trend.length - 1)
}
function trendPoints(key) {
  return data.value.trend
    .map((row, index) => `${trendX(index)},${220 - (row[key] * 170) / maximum.value}`)
    .join(' ')
}
function axisDate(value) {
  return new Date(value).toLocaleString(undefined, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  })
}
function ruleName(key) {
  const route = gatewayConfig.value?.routes.find((item) => routeKey(item) === key)
  const group = gatewayConfig.value?.groups.find((item) => item.id === route?.group_id)
  return route ? `${group?.name || route.group_id} / ${route.name || route.host}` : key || '未匹配反代规则'
}
function shortRule(key) {
  const name = ruleName(key)
  const limit = narrow.value ? 26 : 20
  return name.length > limit ? name.slice(0, limit - 1) + '…' : name
}
function rankSegments(row) {
  let x = rankStart.value
  return seriesList.map((series) => {
    const width = (row[series.key] * rankSpan.value) / rankMaximum.value
    const part = { x, width, color: series.color, value: row[series.key], label: series.label }
    x += width
    return part
  })
}
async function load() {
  const request = ++version,
    filters = { ...form.value }
  loading.value = true
  error.value = ''
  try {
    const result = await adminApi.statistics(
      { hours: Number(filters.hours), rule: filters.rule },
      controller.signal
    )
    if (!controller.signal.aborted && request === version) {
      data.value = result
      applied.value = filters
    }
  } catch (err) {
    if (!controller.signal.aborted && request === version) {
      error.value = err.message
      await nextTick()
      errorElement.value?.focus()
    }
  } finally {
    if (request === version) loading.value = false
  }
}
function exportSVG() {
  const ns = 'http://www.w3.org/2000/svg',
    root = document.createElementNS(ns, 'svg')
  const theme = getComputedStyle(document.body),
    background = document.createElementNS(ns, 'rect')
  root.setAttribute('xmlns', ns)
  root.setAttribute('width', '900')
  root.setAttribute('font-family', 'sans-serif')
  background.setAttribute('width', '100%')
  background.setAttribute('height', '100%')
  background.setAttribute('fill', theme.getPropertyValue('--canvas').trim())
  root.append(background)
  let y = 34
  for (const source of charts.value.querySelectorAll('svg[data-chart]')) {
    const heading = document.createElementNS(ns, 'text')
    heading.setAttribute('x', '20')
    heading.setAttribute('y', String(y))
    heading.setAttribute('fill', theme.getPropertyValue('--ink').trim())
    heading.setAttribute('font-size', '18')
    heading.textContent = `${source.dataset.chart} · ${reportDate(data.value.from)} — ${reportDate(data.value.to)}`
    root.append(heading)
    const legendHeight = source.dataset.chart === '访问趋势' ? 26 : 0
    if (legendHeight)
      seriesList.forEach((series, index) => {
        const label = document.createElementNS(ns, 'text')
        label.setAttribute('x', String(20 + index * 180))
        label.setAttribute('y', String(y + 26))
        label.setAttribute('font-size', '14')
        const variable = series.color.slice(4, -1)
        label.setAttribute('fill', theme.getPropertyValue(variable).trim())
        label.textContent = `● ${series.label}`
        root.append(label)
      })
    const clone = source.cloneNode(true),
      originals = [source, ...source.querySelectorAll('*')],
      copies = [clone, ...clone.querySelectorAll('*')]
    originals.forEach((node, index) => {
      const style = getComputedStyle(node)
      for (const attr of ['fill', 'stroke'])
        if (node.getAttribute(attr)?.includes('var(')) copies[index].setAttribute(attr, style[attr])
    })
    const height = (source.viewBox.baseVal.height * 860) / source.viewBox.baseVal.width
    clone.setAttribute('x', '20')
    clone.setAttribute('y', String(y + 16 + legendHeight))
    clone.setAttribute('width', '860')
    clone.setAttribute('height', String(height))
    root.append(clone)
    y += height + 70 + legendHeight
  }
  root.setAttribute('height', String(y))
  root.setAttribute('viewBox', `0 0 900 ${y}`)
  const url = URL.createObjectURL(
    new Blob([new XMLSerializer().serializeToString(root)], { type: 'image/svg+xml;charset=utf-8' })
  )
  const link = document.createElement('a')
  link.href = url
  link.download = 'gatehome-statistics.svg'
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
  toast.success('已导出当前查询的统计图表')
}
function mediaChanged(event) {
  narrow.value = event.matches
}
onMounted(() => {
  load()
  media.addEventListener('change', mediaChanged)
  refreshConfig(controller.signal).catch((err) => {
    if (!controller.signal.aborted) error.value = err.message
  })
})
onUnmounted(() => {
  media.removeEventListener('change', mediaChanged)
  controller.abort()
})
</script>
<style scoped>
.statistics-chart {
  width: 100%;
  height: auto;
  display: block;
}
.chart-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
  min-width: 0;
}
.chart-legend {
  display: flex;
  gap: 16px;
  flex-wrap: wrap;
  font-size: 12px;
  color: var(--muted);
}
.chart-legend span {
  display: flex;
  align-items: center;
  gap: 6px;
}
.chart-legend i {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}
.section-title {
  font-size: 16px;
  color: var(--ink);
}
@media (max-width: 1100px) {
  .chart-grid {
    grid-template-columns: 1fr;
  }
}
</style>
