<template>
  <div class="report-page" :class="{ 'animate-fade-in': !embedded }">
    <header v-if="!embedded" class="report-heading">
      <h1>流量统计</h1>
    </header>
    <form class="report-form" @submit.prevent="load">
      <label>流量类型
        <select v-model="form.source" name="source" class="form-select" @change="changeSource">
          <option value="http">反向代理</option>
          <option value="forward">端口转发</option>
        </select>
      </label>
      <label v-if="form.source === 'forward'">协议
        <select v-model="form.protocol" name="protocol" class="form-select">
          <option value="">全部协议</option>
          <option value="tcp">TCP</option>
          <option value="udp">UDP</option>
        </select>
      </label>
      <label
        >{{ form.source === 'forward' ? '转发规则' : '反代规则' }}<select v-model="form.rule" name="rule" class="form-select">
          <option value="">全部{{ form.source === 'forward' ? '转发' : '反代' }}规则</option>
          <option v-for="item in ruleOptions" :key="item.id" :value="item.id">{{ item.label }}</option>
        </select></label
      >
      <label
        >时间范围<select v-model="form.range" name="range" class="form-select">
          <option value="24">最近 24 小时</option>
          <option value="168">最近 7 天</option>
          <option value="720">最近 30 天</option>
          <option value="custom">自选起止时间</option>
        </select></label
      >
      <template v-if="form.range === 'custom'"
        ><label
          >开始时间<input
            v-model="form.from"
            name="from"
            class="form-input"
            type="datetime-local"
            required /></label
        ><label
          >结束时间<input
            v-model="form.to"
            name="to"
            class="form-input"
            type="datetime-local"
            required /></label
      ></template>
      <button type="submit" class="btn btn-primary" :disabled="loading">
        {{ loading ? '查询中…' : '查询流量' }}
      </button>
      <router-link v-if="form.source === 'http'" class="btn btn-secondary" :to="statsLinkTo">
        查看访问统计
      </router-link>
    </form>
    <p class="report-note">
      自选时间按浏览器时区输入，统计覆盖涉及的整小时。
      <template v-if="(data ? applied : form).source === 'forward'">
        转发流量按写入目标连接的有效数据字节计数，每 5 秒汇总，不含协议头。
      </template>
      <template v-else>反代统计口径为 HTTP 请求体和响应体，不含拦截响应及 WebSocket 数据。</template>
    </p>
    <p v-if="error" ref="errorElement" class="report-error" role="alert" tabindex="-1">
      {{ error }}{{ data ? '。当前显示上次成功查询的流量。' : '' }}
    </p>
    <p v-if="dirty && data" class="report-note">筛选条件尚未应用，当前仍显示上次查询结果。</p>
    <template v-if="data">
      <p class="report-note">实际统计范围 {{ reportDate(data.from) }} — {{ reportDate(data.to) }}</p>
      <p v-if="data.storage_error" class="report-warning" role="alert">
        流量统计文件读取或保存失败，结果可能不完整，请检查数据目录和磁盘空间。
      </p>
      <div class="report-metrics traffic-totals" :class="{ 'with-total': applied.source === 'forward' }">
        <SpotlightCard class="report-metric"
          ><span class="metric-label">上传 · 客户端发往上游</span>
          <div class="metric-value" data-total="upload" :data-bytes="data.upload_bytes">
            {{ formatBytes(data.upload_bytes) }}
          </div>
          <p class="report-note">{{ data.upload_bytes.toLocaleString() }} 字节</p></SpotlightCard
        ><SpotlightCard class="report-metric"
          ><span class="metric-label">下载 · 上游发往客户端</span>
          <div class="metric-value" data-total="download" :data-bytes="data.download_bytes">
            {{ formatBytes(data.download_bytes) }}
          </div>
          <p class="report-note">{{ data.download_bytes.toLocaleString() }} 字节</p></SpotlightCard
        >
        <SpotlightCard v-if="applied.source === 'forward'" class="report-metric">
          <span class="metric-label">总传输量</span>
          <div class="metric-value" data-total="combined">{{ formatBytes(data.upload_bytes + data.download_bytes) }}</div>
        </SpotlightCard>
      </div>
      <ReportTableScroll v-if="data.rules.length" label="流量统计规则">
        <table class="report-table traffic-table">
          <thead>
            <tr>
              <th>{{ applied.source === 'forward' ? '转发规则' : '反代规则 / 域名' }}</th>
              <th>{{ applied.source === 'forward' ? '协议' : '所属反代组' }}</th>
              <th>上传量</th>
              <th>下载量</th>
              <th>总传输量</th>
              <th v-if="applied.source === 'http'">关联统计</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in data.rules" :key="row.id + '/' + (row.protocol || '')" :data-route-id="row.id"
              :data-protocol="row.protocol">
              <td v-if="applied.source === 'forward'">
                <strong>{{ forwardRules.get(row.id)?.name || row.host }}</strong>
                <small class="mono-text">{{ row.id }}</small>
              </td>
              <td v-else>
                <strong>{{ route(row.id)?.name || row.host }}</strong
                ><small class="mono-text">{{ row.host }}</small
                ><small class="mono-text">{{ row.id }}</small>
              </td>
              <td>{{ applied.source === 'forward' ? row.protocol.toUpperCase() : groupName(row.id) }}</td>
              <td class="mono-text" :data-bytes="row.upload_bytes">
                {{ formatBytes(row.upload_bytes) }}<small>{{ row.upload_bytes.toLocaleString() }} 字节</small>
              </td>
              <td class="mono-text" :data-bytes="row.download_bytes">
                {{ formatBytes(row.download_bytes)
                }}<small>{{ row.download_bytes.toLocaleString() }} 字节</small>
              </td>
              <td class="mono-text">{{ formatBytes(row.upload_bytes + row.download_bytes) }}</td>
              <td v-if="applied.source === 'http'">
                <router-link
                  v-if="route(row.id)"
                  class="btn btn-ghost btn-sm"
                  :to="{ path: '/statistics', query: { rule: routeKey(route(row.id)) } }"
                >
                  访问统计
                </router-link>
                <span v-else class="muted-text">—</span>
              </td>
            </tr>
          </tbody>
        </table>
      </ReportTableScroll>
      <p v-else class="report-empty">所选时间和规则暂无{{ applied.source === 'forward' ? '端口转发' : ' HTTP ' }}流量记录。</p>
    </template>
    <p v-else-if="loading" role="status">正在读取流量统计…</p>
  </div>
</template>
<script setup>
import ReportTableScroll from '@/components/ReportTableScroll.vue'
import { computed, nextTick, onMounted, onUnmounted, ref, shallowRef, watch } from 'vue'
import { useRoute } from 'vue-router'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { adminApi } from '@/api/admin'
import { routeKey } from '@/api/configuration'
import { formatBytes, reportDate, trafficQuery } from '@/api/reports'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import '@/styles/reports.css'

defineProps({
  embedded: { type: Boolean, default: false }
})

const { gatewayConfig, refreshConfig } = useGatewayStatus()
const currentRoute = useRoute()
const requestedRule = () => typeof currentRoute.query.rule === 'string' ? currentRoute.query.rule : ''
const requestedSource = () => currentRoute.query.source === 'forward' ? 'forward' : 'http'
const form = ref({ source: requestedSource(), protocol: '', rule: requestedRule(), range: '24', from: '', to: '' }),
  applied = shallowRef({ ...form.value })
const statsLinkTo = computed(() => {
  const matched = route(form.value.rule)
  return matched ? { path: '/statistics', query: { rule: routeKey(matched) } } : { path: '/statistics' }
})
const data = shallowRef(null),
  loading = ref(false),
  error = ref(''),
  errorElement = ref(null)
const dirty = computed(() => JSON.stringify(form.value) !== JSON.stringify(applied.value))
const forwardRules = computed(() => new Map((gatewayConfig.value?.forwards ?? []).map(item => [item.id, item])))
const ruleOptions = computed(() => {
  const history = form.value.source === applied.value.source ? data.value?.rules ?? [] : []
  const options = new Map(
    history.map((row) => [
      row.id,
      { id: row.id, label: `${row.host} · 历史规则 ${row.id}` }
    ])
  )
  if (form.value.source === 'forward') {
    for (const item of forwardRules.value.values())
      options.set(item.id, { id: item.id, label: `${item.name} · :${item.listen_port}` })
  } else {
    for (const item of gatewayConfig.value?.routes ?? [])
      options.set(item.id, {
        id: item.id,
        label: `${groupName(item.id)} / ${item.name || item.host} · ${item.host}`
      })
  }
  if (form.value.rule && !options.has(form.value.rule))
    options.set(form.value.rule, { id: form.value.rule, label: `历史规则 ${form.value.rule}` })
  return [...options.values()]
})
function route(id) {
  return gatewayConfig.value?.routes.find((item) => item.id === id)
}
function groupName(id) {
  const item = route(id)
  return item
    ? gatewayConfig.value?.groups.find((group) => group.id === item.group_id)?.name || item.group_id
    : '历史规则'
}
const controller = new AbortController()
let version = 0
async function load() {
  const request = ++version,
    filters = { ...form.value }
  loading.value = true
  error.value = ''
  try {
    const result = await adminApi.traffic(trafficQuery(filters), controller.signal)
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
function changeSource() {
  form.value.protocol = ''
  form.value.rule = ''
  load()
}
watch([() => currentRoute.query.source, () => currentRoute.query.rule], () => {
  form.value.source = requestedSource()
  form.value.protocol = ''
  form.value.rule = requestedRule()
  load()
})
onMounted(() => {
  load()
  refreshConfig(controller.signal).catch((err) => {
    if (!controller.signal.aborted) error.value = err.message
  })
})
onUnmounted(() => controller.abort())
</script>
<style scoped>
.traffic-totals {
  grid-template-columns: 1fr 1fr;
}
.traffic-totals.with-total { grid-template-columns: repeat(3, minmax(0, 1fr)); }
@media (max-width: 600px) {
  .traffic-totals, .traffic-totals.with-total {
    grid-template-columns: 1fr;
  }
}
</style>
