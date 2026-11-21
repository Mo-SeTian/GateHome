<template>
  <div class="report-page">
    <header class="report-heading">
      <h1>账号安全</h1>
      <div class="report-actions">
        <RouterLink to="/settings" class="btn btn-secondary">返回设置</RouterLink>
        <button type="button" class="btn btn-secondary" :disabled="busy" @click="load">刷新</button>
      </div>
    </header>
    <p v-if="error" class="report-error" role="alert">{{ error }}</p>
    <AdminVerification ref="verification" />
    <SpotlightCard class="report-panel">
      <div class="report-heading">
        <h2>通行密钥</h2>
        <button v-if="supported" type="button" class="btn btn-primary" :disabled="busy"
          @click="withVerification(() => usePasskey('register', controller.signal))">添加通行密钥</button>
      </div>
      <p v-if="!supported" class="report-note">请使用支持通行密钥的浏览器打开 HTTPS 管理域名，或在 localhost 上操作。</p>
      <p v-if="loaded && !keys.length" class="report-empty">尚未添加通行密钥。</p>
      <div v-if="keys.length" class="report-table-wrap" tabindex="0" aria-label="已注册的通行密钥">
        <table class="report-table">
          <thead><tr><th scope="col">管理域名</th><th scope="col">添加时间</th><th scope="col">最后使用</th><th scope="col">操作</th></tr></thead>
          <tbody>
            <tr v-for="key in keys" :key="key.id">
              <th scope="row">{{ key.rp_id }}</th><td>{{ date(key.created) }}</td><td>{{ date(key.last_used) }}</td>
              <td><button type="button" class="btn btn-secondary" :disabled="busy"
                :aria-label="'移除 ' + key.rp_id + ' 在 ' + date(key.created) + ' 添加的通行密钥'"
                @click="remove(key)">移除</button></td>
            </tr>
          </tbody>
        </table>
      </div>
    </SpotlightCard>
    <SpotlightCard class="report-panel">
      <h2>登录会话</h2>
      <div class="report-table-wrap" tabindex="0" aria-label="登录会话">
        <table class="report-table">
          <thead><tr>
            <th scope="col">设备 / 浏览器</th><th scope="col">验证方式</th><th scope="col">登录时间</th>
            <th scope="col">最近活动</th><th scope="col">操作</th>
          </tr></thead>
          <tbody>
            <tr v-for="session in sessions" :key="session.id">
              <th scope="row" :title="session.device">
                {{ deviceName(session.device) }}
                <small v-if="current === session.id">当前会话</small>
              </th>
              <td>{{ session.method === 'passkey' ? '通行密钥' : '密码' }}</td>
              <td>{{ date(session.started) }}</td><td>{{ date(session.last_seen) }}</td>
              <td><button type="button" class="btn btn-secondary" :disabled="busy"
                :aria-label="current === session.id ? '退出当前会话' : '撤销 ' + deviceName(session.device) + ' 的会话'"
                @click="revoke(session)">{{ current === session.id ? '退出' : '撤销' }}</button></td>
            </tr>
          </tbody>
        </table>
        <p v-if="busy && !loaded" class="report-empty" role="status">正在读取会话…</p>
      </div>
    </SpotlightCard>
  </div>
</template>
<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import SpotlightCard from '@/components/SpotlightCard.vue'
import AdminVerification from '@/components/AdminVerification.vue'
import { adminApi } from '@/api/admin'
import { listPasskeys, passkeySupported, removePasskey, usePasskey } from '@/api/passkeys'
import { useSession } from '@/composables/useSession'
import { deviceName } from '@/api/reports'
import '@/styles/reports.css'

const keys = ref([]), sessions = ref([]), current = ref(''), verification = ref(null)
const error = ref(''), busy = ref(false), loaded = ref(false)
const controller = new AbortController(), supported = passkeySupported()
const dateFormat = new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'medium' })
const date = value => value ? dateFormat.format(new Date(value)) : '—'
const router = useRouter()
const { endSession } = useSession()

async function refresh() {
  const [keyList, data] = await Promise.all([listPasskeys(controller.signal), adminApi.sessions(controller.signal)])
  keys.value = keyList
  sessions.value = data.sessions
  current.value = data.current
  loaded.value = true
}
async function run(action) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try { await action() } catch (err) {
    if (!controller.signal.aborted) error.value = err.message
  } finally { busy.value = false }
}
const load = () => run(refresh)
function withVerification(action) {
  return run(async () => {
    try { await action() }
    catch (err) {
      if (err.status !== 428) throw err
      if (!await verification.value.verify() || controller.signal.aborted) return
      await action()
    }
    await refresh()
  })
}
function remove(key) {
  if (window.confirm(`移除 ${key.rp_id} 的此通行密钥？`))
    return withVerification(() => removePasskey(key.id, controller.signal))
}
function revoke(session) {
  const self = session.id === current.value
  if (self || window.confirm('撤销此设备的登录会话？')) {
    return run(async () => {
      await adminApi.revokeSession(session.id, controller.signal)
      if (self) {
        endSession()
        await router.replace({ name: 'Login' })
      } else await refresh()
    })
  }
}
onMounted(load)
onBeforeUnmount(() => controller.abort())
</script>
