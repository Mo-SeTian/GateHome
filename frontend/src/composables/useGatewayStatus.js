import { computed, onMounted, onBeforeUnmount, ref, shallowRef } from 'vue'
import { adminApi } from '@/api/admin'
import { startPolling } from '@/api/polling'

const gatewayStatus = shallowRef(null)
const gatewayConfigSnapshot = shallowRef(null)
const gatewayConfig = computed(() => gatewayConfigSnapshot.value?.config ?? null)
const isSavingConfig = ref(false)
let configGeneration = 0
const statusUnavailable = ref(false)
const statusError = ref('')
let statusRequest, statusController
let statusGeneration = 0
const statusSubscriptions = new Map()
let stopStatusTimer

export function subscribeStatus(interval) {
  const key = Symbol()
  function schedule() {
    stopStatusTimer?.()
    stopStatusTimer = statusSubscriptions.size
      ? startPolling(() => refreshStatus(), Math.min(...statusSubscriptions.values()))
      : undefined
  }
  statusSubscriptions.set(key, interval)
  schedule()
  return () => {
    statusSubscriptions.delete(key)
    schedule()
  }
}

export function useStatusPolling(interval = 5000) {
  let stop
  onMounted(() => { stop = subscribeStatus(interval) })
  onBeforeUnmount(() => stop?.())
}
const gatewayVersion = computed(() => gatewayStatus.value?.version
  ? `v${gatewayStatus.value.version.replace(/^v/, '')}` : `v${__GATEHOME_VERSION__}`)
const gatewayState = computed(() => {
  if (statusUnavailable.value) return { label: '网关连接不可用', tone: 'warn' }
  if (!gatewayStatus.value) return { label: '正在连接网关', tone: 'pending' }
  if (gatewayStatus.value.maintenance_busy) return { label: '维护操作进行中', tone: 'warn' }
  if (gatewayStatus.value.restart_required) return { label: '配置待重启生效', tone: 'warn' }
  return { label: '网关运行中', tone: 'ok' }
})

async function refreshStatus(signal) {
  signal?.throwIfAborted()
  if (!statusRequest) {
    const generation = statusGeneration
    const controller = statusController = new AbortController()
    const timeout = setTimeout(() => controller.abort(), 15000)
    const pending = adminApi.status(controller.signal).then(response => {
      if (generation === statusGeneration) {
        gatewayStatus.value = response
        statusUnavailable.value = false
        statusError.value = ''
      }
      return response
    }).catch(err => {
      if (generation === statusGeneration) {
        statusUnavailable.value = true
        statusError.value = err.name === 'AbortError' ? '网关状态读取超时，正在重试' : err.message
      }
      throw err
    }).finally(() => {
      clearTimeout(timeout)
      if (statusRequest === pending) {
        statusRequest = undefined
        statusController = undefined
      }
    })
    statusRequest = pending
  }
  const response = await statusRequest
  signal?.throwIfAborted()
  return response
}

async function refreshConfig(signal) {
  if (isSavingConfig.value) throw new Error('正在保存配置，请稍后刷新')
  const generation = ++configGeneration
  const response = await adminApi.config(signal)
  if (!signal?.aborted && generation === configGeneration) gatewayConfigSnapshot.value = response
  return response
}

async function saveConfig(snapshot, config, credentials = {}) {
  if (isSavingConfig.value) throw new Error('正在保存配置，请稍后重试')
  isSavingConfig.value = true
  const generation = ++configGeneration
  try {
    const response = await adminApi.saveConfig({ ...credentials, config, revision: snapshot.revision })
    if (generation === configGeneration) gatewayConfigSnapshot.value = response
    return response
  } finally {
    isSavingConfig.value = false
  }
}

function resetGatewayStatus() {
  statusGeneration++
  statusController?.abort()
  statusRequest = undefined
  statusController = undefined
  gatewayStatus.value = null
  configGeneration++
  gatewayConfigSnapshot.value = null
  statusUnavailable.value = false
  statusError.value = ''
}

export function useGatewayStatus() {
  return {
    gatewayStatus, gatewayConfig, gatewayConfigSnapshot, gatewayVersion, gatewayState, statusError,
    isSavingConfig, refreshStatus, refreshConfig, saveConfig, resetGatewayStatus
  }
}
