<template>
  <div class="app-container" :class="`theme-${theme}`">
    <!-- Standalone Layout for Login Page -->
    <div v-if="route.meta.hideLayout" class="standalone-layout">
      <router-view />
    </div>

    <!-- Main Application Shell -->
    <div v-else-if="isAuthenticated" class="app-layout">
      <!-- Left Sidebar (Desktop fixed, Mobile sliding drawer) -->
      <Sidebar />

      <!-- Main Stage Area -->
      <div class="main-wrapper">
        <!-- Top App Bar -->
        <TopHeader />

        <!-- View Content Area with Smooth Page Transition -->
        <main :inert="assistantOverlay" class="page-viewport" :class="{ 'with-assistant': showAssistant }">
          <router-view v-slot="{ Component }">
            <transition name="page-fade" mode="out-in">
              <component :is="Component" />
            </transition>
          </router-view>
        </main>
        <aside v-if="showAssistant" ref="assistantDrawer" class="assistant-drawer" aria-label="网关助手侧边栏"
          tabindex="-1" @keydown.esc.stop="panelOpen = false">
          <AssistantChat />
        </aside>
      </div>
    </div>

    <!-- Command Palette (⌘K) -->
    <CmdkModal v-if="isAuthenticated" />

    <!-- Admin Account Security Modal -->
    <AccountModal v-if="isAuthenticated" />

    <!-- Toast Notifications -->
    <ToastNotification />

  </div>
</template>

<script setup>
import { computed, defineAsyncComponent, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useAssistant } from '@/composables/useAssistant'
import Sidebar from '@/components/Sidebar.vue'
import TopHeader from '@/components/TopHeader.vue'
import CmdkModal from '@/components/CmdkModal.vue'
import AccountModal from '@/components/AccountModal.vue'
import ToastNotification from '@/components/ToastNotification.vue'
import { adminApi } from '@/api/admin'
import { startPolling } from '@/api/polling'
import { useAppStore } from '@/composables/useAppStore'
import { useGatewayStatus, subscribeStatus } from '@/composables/useGatewayStatus'
import { useSession } from '@/composables/useSession'
import { useToast } from '@/composables/useToast'

const AssistantChat = defineAsyncComponent(() => import('@/components/AssistantChat.vue'))
const route = useRoute()
const { panelOpen, resetAssistant } = useAssistant()
const showAssistant = computed(() => panelOpen.value && route.name !== 'Assistant' && route.name !== 'AssistantSettings')
const assistantDrawer = ref(null)
const overlayLayout = window.matchMedia('(max-width: 1279px)')
const narrowLayout = ref(overlayLayout.matches)
const assistantOverlay = computed(() => narrowLayout.value && showAssistant.value)
const updateOverlay = event => { narrowLayout.value = event.matches }
onMounted(() => overlayLayout.addEventListener('change', updateOverlay))
onUnmounted(() => overlayLayout.removeEventListener('change', updateOverlay))
watch(assistantOverlay, async (open, old, onCleanup) => {
  const owner = document.activeElement
  const returnFocus = !open && assistantDrawer.value?.contains(owner)
  let current = true
  onCleanup(() => { current = false })
  await nextTick()
  if (!current) return
  if (open) assistantDrawer.value?.focus()
  else if (returnFocus && !panelOpen.value && route.name !== 'Assistant' && route.name !== 'AssistantSettings')
    document.querySelector('button[aria-label="网关助手"]')?.focus()
})
watch(() => route.name, name => {
  if (name === 'Assistant' || name === 'AssistantSettings') panelOpen.value = false
})
const { theme, closeCmdk, closeSidebar } = useAppStore()
const { isAuthenticated } = useSession()
const { refreshStatus, refreshConfig, resetGatewayStatus } = useGatewayStatus()
const { mergeFaultHistory } = useToast()

watch(isAuthenticated, (authenticated, _, onCleanup) => {
  if (!authenticated) {
    closeCmdk()
    closeSidebar()
    resetGatewayStatus()
    resetAssistant()
    return
  }
  const controller = new AbortController()
  let stopNotifications
  async function readNotifications() {
    const events = await adminApi.notificationHistory(controller.signal)
    if (!controller.signal.aborted) mergeFaultHistory(events)
  }
  readNotifications().catch(() => {}).finally(() => {
    if (!controller.signal.aborted) stopNotifications = startPolling(readNotifications, 15000)
  })
  const pollStatus = () => refreshStatus(controller.signal).catch(() => {})
  pollStatus()
  refreshConfig(controller.signal).catch(() => {})
  const stopPolling = subscribeStatus(10000)
  onCleanup(() => {
    stopNotifications?.()
    stopPolling()
    controller.abort()
  })
}, { immediate: true })

onMounted(() => {
})
</script>

<style>
.app-container {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
}

.standalone-layout {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
}

.app-layout {
  display: flex;
  min-height: 100vh;
}

.main-wrapper {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  margin-left: var(--sidebar-w);
  transition: margin-left 0.3s ease;
}

.page-viewport {
  flex: 1;
  padding: 24px;
  max-width: 1540px;
  width: 100%;
  margin: 0 auto;
}

.assistant-drawer {
  position: fixed;
  inset: var(--topbar-h) 0 0 auto;
  width: min(100vw, clamp(380px, 28vw, 460px));
  display: flex;
  flex-direction: column;
  background: var(--sidebar-bg);
  border-left: 1px solid var(--line);
  box-shadow: -14px 0 35px rgba(0, 0, 0, 0.14);
  z-index: 95;
}
@media (min-width: 1280px) {
  .page-viewport.with-assistant {
    max-width: calc(100% - clamp(380px, 28vw, 460px));
    margin-inline: 0 auto;
  }
}
@media (max-width: 600px) {
  .assistant-drawer { width: 100%; }
}

/* Page transitions */
.page-fade-enter-active,
.page-fade-leave-active {
  transition: opacity 0.2s cubic-bezier(0.16, 1, 0.3, 1), transform 0.2s cubic-bezier(0.16, 1, 0.3, 1);
}
.page-fade-enter-from {
  opacity: 0;
  transform: translateY(6px);
}
.page-fade-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

/* Responsive adjustment for Mobile & Tablet */
@media (max-width: 1023px) {
  .main-wrapper {
    margin-left: 0;
  }
  .page-viewport {
    padding: 16px;
  }
}
</style>
