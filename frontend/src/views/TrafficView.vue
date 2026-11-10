<template>
  <div class="report-page animate-fade-in">
    <div class="report-heading">
      <div>
        <h1>流量统计</h1>
        <p>按反代规则和时间查看 HTTP 请求体上传量与响应体下载量，按小时汇总，保留最近 30 天</p>
      </div>
    </div>
    <form class="report-form" @submit.prevent="load">
      <label
        >反代规则<select v-model="form.rule" name="rule" class="form-select">
          <option value="">全部反代规则</option>
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
    </form>
    <p class="report-note">
      自选时间按浏览器时区输入，服务器会覆盖涉及的整小时。统计不含协议头、拦截响应、WebSocket 数据及 TCP/UDP
      端口转发。
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
      <div class="report-metrics traffic-totals">
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
      </div>
      <div v-if="data.rules.length" class="report-table-wrap">
        <table class="report-table traffic-table">
          <thead>
            <tr>
              <th>反代规则 / 域名</th>
              <th>所属反代组</th>
              <th>上传量</th>
              <th>下载量</th>
              <th>总传输量</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in data.rules" :key="row.id" :data-route-id="row.id">
              <td>
                <strong>{{ route(row.id)?.name || row.host }}</strong
                ><small class="mono-text">{{ row.host }}</small
                ><small class="mono-text">{{ row.id }}</small>
              </td>
              <td>{{ groupName(row.id) }}</td>
              <td class="mono-text" :data-bytes="row.upload_bytes">
                {{ formatBytes(row.upload_bytes) }}<small>{{ row.upload_bytes.toLocaleString() }} 字节</small>
              </td>
              <td class="mono-text" :data-bytes="row.download_bytes">
                {{ formatBytes(row.download_bytes)
                }}<small>{{ row.download_bytes.toLocaleString() }} 字节</small>
              </td>
              <td class="mono-text">{{ formatBytes(row.upload_bytes + row.download_bytes) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else class="report-empty">所选时间和规则暂无 HTTP 流量记录。</p>
    </template>
    <p v-else-if="loading" role="status">正在读取流量统计…</p>
  </div>
</template>
<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, shallowRef, watch } from 'vue'
import { useRoute } from 'vue-router'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { adminApi } from '@/api/admin'
import { formatBytes, reportDate, trafficQuery } from '@/api/reports'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import '@/styles/reports.css'

const { gatewayConfig, refreshConfig } = useGatewayStatus()
const currentRoute = useRoute()
const requestedRule = () => typeof currentRoute.query.rule === 'string' ? currentRoute.query.rule : ''
const form = ref({ rule: requestedRule(), range: '24', from: '', to: '' }),
  applied = shallowRef({ ...form.value })
const data = shallowRef(null),
  loading = ref(false),
  error = ref(''),
  errorElement = ref(null)
const dirty = computed(() => JSON.stringify(form.value) !== JSON.stringify(applied.value))
const ruleOptions = computed(() => {
  const options = new Map(
    (data.value?.rules ?? []).map((row) => [
      row.id,
      { id: row.id, label: `${row.host} · 历史规则 ${row.id}` }
    ])
  )
  for (const item of gatewayConfig.value?.routes ?? [])
    options.set(item.id, {
      id: item.id,
      label: `${groupName(item.id)} / ${item.name || item.host} · ${item.host}`
    })
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
watch(() => currentRoute.query.rule, () => {
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
@media (max-width: 600px) {
  .traffic-totals {
    grid-template-columns: 1fr;
  }
}
</style>
