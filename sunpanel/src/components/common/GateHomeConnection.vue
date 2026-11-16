<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { NAlert, NButton, NSelect, NSpace } from 'naive-ui'
import { useAuthStore } from '@/store'
import type { GateHomeRoute } from '@/utils/gateHome'
import { GateHomeLoginRequired, loadGateHomeRoutes, openGateHomeConnection } from '@/utils/gateHome'

defineProps<{ disabled?: boolean }>()
const emit = defineEmits<{ (e: 'routes', routes: GateHomeRoute[]): void; (e: 'loading', loading: boolean): void; (e: 'error', message: string): void; (e: 'ready', ready: boolean): void }>()
const auth = useAuthStore()
const origins = ref<string[]>([]), selected = ref(''), loading = ref(false), error = ref(''), connected = ref(false)
const unavailableImages = ref(0)
let generation = 0, controller: AbortController | undefined, cancel: (() => void) | undefined
function stop() { generation++; controller?.abort(); cancel?.(); cancel = undefined }
function start() {
  stop()
  controller = new AbortController()
  loading.value = true
  error.value = ''
  unavailableImages.value = 0
  emit('loading', true)
  emit('error', '')
  emit('routes', [])
  emit('ready', false)
  return generation
}
function failed(message: string) { error.value = message; emit('error', message) }
function settled(current: number) { if (current === generation) { loading.value = false; emit('loading', false) } }
async function connectionInfo() {
  const response = await fetch('/api/sunpanel/connection', { method: 'POST', credentials: 'same-origin', signal: controller?.signal,
    headers: { 'Content-Type': 'application/json', 'X-Gatehouse-Request': '1', token: auth.token || '' }, body: '{}' })
  const data = await response.json()
  if (!response.ok) throw new Error(data.error || '无法读取 GateHome 连接入口。')
  return data
}
async function refresh() {
  const current = start()
  origins.value = []
  connected.value = false
  try {
    const routes = await loadGateHomeRoutes(controller?.signal)
    if (current === generation) { unavailableImages.value = routes.filter(route => route.imageUnavailable).length; emit('routes', routes); emit('ready', true) }
  } catch (err) {
    if (current !== generation) return
    if (err instanceof GateHomeLoginRequired) {
      try {
        const data = await connectionInfo()
        if (current !== generation) return
        origins.value = data.origins
        selected.value = data.origins[0]
      } catch (error) { if (current === generation) failed(error instanceof Error ? error.message : '连接入口读取失败。') }
    } else failed(err instanceof Error ? err.message : '读取失败，请重试。')
  } finally { settled(current) }
}
function connect() {
  const current = start()
  try {
    const request = openGateHomeConnection(selected.value)
    cancel = request.cancel
    request.promise.then(async routes => {
      if (current !== generation) return
      await connectionInfo()
      if (current !== generation) return
      connected.value = true
      unavailableImages.value = routes.filter(route => route.imageUnavailable).length
      emit('routes', routes)
      emit('ready', true)
    }).catch(err => { if (current === generation) failed(err.message) }).finally(() => settled(current))
  } catch (err) {
    failed(err instanceof Error ? err.message : '无法打开连接窗口。')
    settled(current)
  }
}
watch(() => auth.token, refresh)
onMounted(refresh)
onBeforeUnmount(stop)
</script>

<template>
  <NSpace vertical :size="12" class="gatehome-connection">
    <NAlert v-if="error" type="warning" :show-icon="false">{{ error }}</NAlert>
    <NAlert v-if="unavailableImages" type="warning" :show-icon="false">{{ unavailableImages }} 个反代图片暂不可读取；可以继续导入网址，图片恢复后重新联动。</NAlert>
    <NAlert v-else-if="origins.length && !connected" type="info" :show-icon="false">独立域名访问需连接 GateHome；已登录时将自动读取并返回本页。</NAlert>
    <NSelect v-if="origins.length > 1" v-model:value="selected" aria-label="GateHome 管理入口" :options="origins.map(value => ({ label: value, value }))" :disabled="loading || disabled" />
    <span v-else-if="origins.length" class="gatehome-entry">{{ selected }}</span>
    <NButton :loading="loading" :disabled="loading || disabled" @click="origins.length ? connect() : refresh()">
      {{ origins.length && !connected ? '连接 GateHome' : '刷新反代列表' }}
    </NButton>
  </NSpace>
</template>

<style scoped>
.gatehome-connection :deep(.n-button) { min-height: 44px; }
.gatehome-entry { font-size: 12px; opacity: 0.7; overflow-wrap: anywhere; }
</style>
