<template>
  <div v-if="gatewayStatus?.restart_required" class="restart-notice" role="status">
    <span>监听配置已保存，重启服务后生效。Docker bridge 部署还需发布对应 TCP / UDP 端口。</span>
    <button
      v-if="gatewayStatus.maintenance_available"
      type="button"
      class="btn btn-secondary btn-sm"
      :disabled="isRestarting || isSavingConfig"
      @click="restart"
    >
      {{ isRestarting ? '正在安排重启…' : '重启服务' }}
    </button>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { adminApi } from '@/api/admin'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useSession } from '@/composables/useSession'
import { useToast } from '@/composables/useToast'

const { gatewayStatus, isSavingConfig } = useGatewayStatus()
const { endSession } = useSession()
const router = useRouter()
const toast = useToast()
const isRestarting = ref(false)

async function restart() {
  if (!confirm('重启将中断当前连接并使管理会话失效，确定继续？')) return
  isRestarting.value = true
  try {
    const result = await adminApi.restart()
    const redirect = router.currentRoute.value.fullPath
    endSession()
    await router.replace({ name: 'Login', query: { redirect } })
    toast.info(result.message)
  } catch (err) {
    toast.error(err.message)
  } finally {
    isRestarting.value = false
  }
}
</script>

<style scoped>
.restart-notice {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
  padding: 14px 16px;
  border: 1px solid var(--warn);
  border-radius: var(--radius-sm);
  color: var(--ink);
  font-size: 13px;
}
</style>
