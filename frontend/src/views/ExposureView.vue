<template>
  <div class="report-page">
    <header class="report-heading">
      <h1>入口暴露总览</h1>
      <div class="report-actions">
        <RouterLink to="/settings" class="btn btn-secondary">返回设置</RouterLink>
        <button type="button" class="btn btn-secondary" :disabled="loading" @click="load">刷新</button>
      </div>
    </header>
    <p v-if="error" class="report-error" role="alert">{{ error }}</p>
    <p v-if="data?.restart_required" class="report-note">监听配置待重启，表中显示当前运行的入口。</p>
    <SpotlightCard class="report-panel">
      <p class="report-note">此表显示 GateHome 运行中的入口。外部代理、路由器端口映射和主机防火墙会影响实际可达范围。</p>
      <ReportTableScroll label="运行中的入口">
        <table class="report-table">
          <thead><tr>
            <th scope="col">服务</th><th scope="col">入口</th><th scope="col">监听地址</th>
            <th scope="col">协议 / TLS</th><th scope="col">登录保护</th><th scope="col">防火墙</th>
          </tr></thead>
          <tbody>
            <tr v-for="(entry, index) in data?.entries || []" :key="index">
              <th scope="row">{{ entry.service }}</th>
              <td>{{ entry.entrance }}<small v-if="entry.host">{{ entry.host }}</small></td>
              <td><code>{{ entry.address }}</code></td><td>{{ entry.transport }}</td>
              <td>{{ entry.login }}</td><td>{{ entry.firewall || '未绑定' }}</td>
            </tr>
          </tbody>
        </table>
        <p v-if="loading && !data" class="report-empty" role="status">正在读取入口…</p>
      </ReportTableScroll>
    </SpotlightCard>
  </div>
</template>
<script setup>
import ReportTableScroll from '@/components/ReportTableScroll.vue'
import { onBeforeUnmount, onMounted, ref } from 'vue'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { adminApi } from '@/api/admin'
import '@/styles/reports.css'

const data = ref(null), error = ref(''), loading = ref(false)
const controller = new AbortController()
async function load() {
  loading.value = true
  error.value = ''
  try {
    data.value = await adminApi.exposure(controller.signal)
  } catch (err) {
    if (!controller.signal.aborted) error.value = err.message
  } finally { loading.value = false }
}
onMounted(load)
onBeforeUnmount(() => controller.abort())
</script>
