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
        <main class="page-viewport">
          <router-view v-slot="{ Component }">
            <transition name="page-fade" mode="out-in">
              <component :is="Component" />
            </transition>
          </router-view>
        </main>
      </div>
    </div>

    <!-- Command Palette (⌘K) -->
    <CmdkModal v-if="isAuthenticated" />

    <!-- Admin Account Security Modal -->
    <AccountModal v-if="isAuthenticated" />

    <!-- Toast Notifications -->
    <ToastNotification />

    <!-- Notification Management Center (macOS Drawer / Mobile Bottom Sheet) -->
    <NotificationCenter v-if="isAuthenticated" />
  </div>
</template>

<script setup>
import { onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import Sidebar from '@/components/Sidebar.vue'
import TopHeader from '@/components/TopHeader.vue'
import CmdkModal from '@/components/CmdkModal.vue'
import AccountModal from '@/components/AccountModal.vue'
import ToastNotification from '@/components/ToastNotification.vue'
import NotificationCenter from '@/components/NotificationCenter.vue'
import { useAppStore } from '@/composables/useAppStore'
import { useGatewayStatus, subscribeStatus } from '@/composables/useGatewayStatus'
import { useSession } from '@/composables/useSession'
import { useToast } from '@/composables/useToast'

const route = useRoute()
const { theme, closeCmdk, closeSidebar } = useAppStore()
const { isAuthenticated } = useSession()
const { refreshStatus, refreshConfig, resetGatewayStatus } = useGatewayStatus()
const { closeCenter } = useToast()

watch(isAuthenticated, (authenticated, _, onCleanup) => {
  if (!authenticated) {
    closeCmdk()
    closeSidebar()
    closeCenter()
    resetGatewayStatus()
    return
  }
  const controller = new AbortController()
  const pollStatus = () => refreshStatus(controller.signal).catch(() => {})
  pollStatus()
  refreshConfig(controller.signal).catch(() => {})
  const stopPolling = subscribeStatus(10000)
  onCleanup(() => {
    stopPolling()
    controller.abort()
  })
}, { immediate: true })

onMounted(() => {
  document.body.className = `theme-${theme.value}`
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
