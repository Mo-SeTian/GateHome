<template>
  <main class="connection-page">
    <section class="connection-card" aria-labelledby="connection-title">
      <Link2 :size="24" class="connection-icon" aria-hidden="true" />
      <h1 id="connection-title">连接 SunPanel</h1>
      <p class="connection-origin">{{ origin }}</p>
      <p v-if="error" class="report-error" role="alert">{{ error }}</p>
      <p v-else role="status">{{ sent ? '反代列表已发送，可返回 SunPanel 继续导入。' : '正在通过 GateHome 管理会话读取反代列表…' }}</p>
      <p class="connection-note">仅发送已启用反代的名称和访问地址。登录凭据保留在 GateHome。</p>
      <button class="btn btn-secondary" @click="close">关闭连接窗口</button>
    </section>
  </main>
</template>

<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { Link2 } from 'lucide-vue-next'
import { adminApi } from '@/api/admin'

const route = useRoute()
const origin = typeof route.query.origin === 'string' ? route.query.origin : ''
const state = typeof route.query.state === 'string' ? route.query.state : ''
const error = ref(''), sent = ref(false)
const controller = new AbortController()
function close() { window.close() }
onMounted(async () => {
  try {
    const target = new URL(origin)
    if (!['http:', 'https:'].includes(target.protocol) || target.origin !== origin || !/^[a-f0-9]{32}$/.test(state) || !window.opener)
      throw new Error('连接请求无效，请从 SunPanel 的“连接 GateHome”按钮重新打开。')
    const routes = await adminApi.connectSunPanel(origin, controller.signal)
    if (controller.signal.aborted) return
    window.opener.postMessage({ type: 'gatehome:sunpanel-routes', state, routes }, origin)
    sent.value = true
  } catch (err) {
    if (!controller.signal.aborted) error.value = err.message || '连接失败，请返回 SunPanel 重试。'
  }
})
onBeforeUnmount(() => controller.abort())
</script>

<style scoped>
.connection-page { display: grid; place-items: center; min-height: 100svh; padding: 24px 16px; background: var(--canvas); }
.connection-card { display: grid; gap: 16px; width: min(100%, 480px); padding: 28px; border: 1px solid var(--line); border-radius: var(--radius-card); background: var(--card-bg); color: var(--ink); box-shadow: var(--shadow-card); }
.connection-icon { color: var(--brand); }
.connection-card h1 { margin: 0; font-size: 20px; }
.connection-card p { margin: 0; font-size: 14px; line-height: 1.7; overflow-wrap: anywhere; }
.connection-origin, .connection-note { color: var(--muted); }
.connection-card .btn { min-height: 44px; }
.report-error { color: var(--bad); }
</style>
