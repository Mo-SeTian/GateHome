import { computed, ref, shallowRef } from 'vue'
import { adminApi } from '@/api/admin'

const gatewayStatus = shallowRef(null)
const gatewayConfig = shallowRef(null)
const statusUnavailable = ref(false)
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
  try {
    const response = await adminApi.status(signal)
    if (!signal?.aborted) {
      gatewayStatus.value = response
      statusUnavailable.value = false
    }
  } catch (err) {
    if (!signal?.aborted) statusUnavailable.value = true
    throw err
  }
}

async function refreshConfig(signal) {
  const response = await adminApi.config(signal)
  if (!signal?.aborted) gatewayConfig.value = response.config
}

function resetGatewayStatus() {
  gatewayStatus.value = null
  gatewayConfig.value = null
  statusUnavailable.value = false
}

export function useGatewayStatus() {
  return { gatewayStatus, gatewayConfig, gatewayVersion, gatewayState, refreshStatus, refreshConfig, resetGatewayStatus }
}
