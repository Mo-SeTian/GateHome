<template>
  <div v-if="gatewayStatus?.restart_required" class="restart-notice" role="status">
    <div class="notice-main">
      <AlertTriangle :size="18" class="notice-icon" />
      <span>监听配置已保存，重启服务后生效。Docker bridge 部署还需发布对应 TCP / UDP 端口。</span>
    </div>
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
import { AlertTriangle } from 'lucide-vue-next'
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
  gap: 14px;
  padding: 14px 18px;
  background: rgba(245, 158, 11, 0.08);
  border: 1px solid rgba(245, 158, 11, 0.28);
  border-radius: var(--radius-md);
  box-shadow: 0 4px 20px rgba(245, 158, 11, 0.06);
  color: var(--ink);
  font-size: 13px;
}
.notice-main {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 1;
  min-width: 260px;
}
.notice-icon {
  color: var(--warn);
  flex-shrink: 0;
}
</style>
