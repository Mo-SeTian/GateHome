<template>
  <header class="top-header">
    <div class="header-left">
      <!-- Mobile Hamburger Menu Button -->
      <button class="mobile-toggle-btn" @click="toggleSidebar" aria-label="打开侧边栏导航">
        <Menu :size="20" />
      </button>

      <span class="current-page">{{ currentTitle }}</span>
    </div>

    <div id="home-breadcrumb"></div>
    <div class="header-center">
      <button
        class="cmdk-trigger-btn" type="button" aria-haspopup="dialog"
        aria-keyshortcuts="Meta+K Control+K" @click="openCmdk"
      >
        <Search :size="15" class="search-icon" aria-hidden="true" />
        <span class="search-placeholder">全局搜索页面、反代、转发与操作…</span>
        <kbd class="shortcut-pill" aria-hidden="true">⌘K</kbd>
      </button>
    </div>

    <!-- Right Controls -->
    <div class="header-right">
      <!-- Global Gateway Status Badge -->
      <div class="gateway-live-pill" :class="gatewayState.tone" :title="gatewayState.label">
        <span class="pulse-dot" />
        <span class="live-text">{{ gatewayState.label }}</span>
      </div>

      <!-- Mobile Search Button -->
      <button
        type="button"
        class="icon-action-btn mobile-search-btn"
        title="全局搜索 (⌘K)"
        aria-label="打开全局搜索"
        aria-haspopup="dialog"
        @click="openCmdk"
      >
        <Search :size="18" aria-hidden="true" />
      </button>

      <button v-if="!['Assistant', 'AssistantSettings'].includes(route.name)" class="icon-action-btn assistant-toggle"
        type="button" title="网关助手" aria-label="网关助手" :aria-expanded="panelOpen" @click="panelOpen = !panelOpen">
        <Sparkles :size="17" /><span>网关助手</span>
      </button>

      <!-- Theme Switch Button -->
      <button 
        class="icon-action-btn" 
        :title="theme === 'dark' ? '切换为浅色模式' : '切换为深色模式'" 
        @click="toggleTheme"
      >
        <Sun v-if="theme === 'dark'" :size="18" />
        <Moon v-else :size="18" />
      </button>

      <!-- Notification Center Popover Trigger -->
      <div class="notification-wrapper" ref="notificationRef">
        <button
          ref="bellBtnRef"
          type="button"
          class="icon-action-btn notification-btn"
          :class="{ 'has-unread': unreadCount > 0, active: isNotificationOpen || route.path === '/notifications' }"
          :title="unreadCount > 0 ? `通知中心 (${unreadCount} 条未读)` : '通知中心'"
          :aria-label="unreadCount ? `打开通知中心，${unreadCount} 条未读` : '打开通知中心'"
          aria-haspopup="dialog"
          :aria-expanded="isNotificationOpen"
          @click="toggleNotificationPopover"
        >
          <Bell :size="18" aria-hidden="true" />
          <span v-if="unreadCount > 0" class="notification-count" aria-hidden="true">{{ unreadCount > 99 ? '99+' : unreadCount }}</span>
        </button>

        <transition name="popover-fade">
          <NotificationPopover v-if="isNotificationOpen" @close="closeNotificationPopover(false)" />
        </transition>
      </div>

      <!-- User Profile Dropdown Menu -->
      <div class="user-menu-wrapper" ref="menuRef">
        <button 
          class="user-profile-badge" 
          @click="toggleUserMenu"
          :aria-label="`账户菜单，${username}`"
          :aria-expanded="isDropdownOpen"
        >
          <div class="user-avatar" aria-hidden="true">
            <span>{{ username.slice(0, 1).toUpperCase() }}</span>
          </div>
          <span class="username">{{ username }}</span>
          <ChevronDown :size="14" class="chevron-icon" :class="{ 'rotate-180': isDropdownOpen }" />
        </button>

        <!-- Profile Dropdown Popup -->
        <transition name="popover-fade">
          <div v-if="isDropdownOpen" class="user-dropdown-popover">
            <div class="popover-user-info">
              <div class="popover-avatar">
                <span>{{ username.slice(0, 1).toUpperCase() }}</span>
              </div>
              <div class="popover-user-meta">
                <span class="popover-username">{{ username }}</span>
                <span class="popover-role">管理员</span>
              </div>
            </div>

            <div class="popover-divider" />

            <div class="popover-menu-list">
              <button class="menu-item-btn" @click="handleOpenAccount">
                <KeyRound :size="15" class="menu-item-icon" />
                <span>修改管理账户密码</span>
              </button>

              <button class="menu-item-btn" @click="handleToggleThemeFromMenu">
                <Sun v-if="theme === 'dark'" :size="15" class="menu-item-icon" />
                <Moon v-else :size="15" class="menu-item-icon" />
                <span>{{ theme === 'dark' ? '切换为浅色模式' : '切换为深色模式' }}</span>
              </button>

              <button class="menu-item-btn" @click="handleOpenCmdkFromMenu">
                <Terminal :size="15" class="menu-item-icon" />
                <span>打开命令面板 (⌘K)</span>
              </button>
            </div>

            <div class="popover-divider" />

            <button class="menu-item-btn danger-item" @click="handleLogout">
              <LogOut :size="15" class="menu-item-icon" />
              <span>退出当前登录</span>
            </button>
          </div>
        </transition>
      </div>
    </div>
  </header>
</template>

<script setup>
import { computed, ref, watch, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { 
  Menu, Search, Sun, Moon, ChevronDown, 
  KeyRound, LogOut, Terminal, Bell, Sparkles
} from 'lucide-vue-next'
import NotificationPopover from '@/components/NotificationPopover.vue'
import { useAssistant } from '@/composables/useAssistant'
import { useAppStore } from '@/composables/useAppStore'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useLogout, useSession } from '@/composables/useSession'
import { useToast } from '@/composables/useToast'

const route = useRoute()
const { panelOpen } = useAssistant()
const { toggleSidebar, openCmdk, theme, toggleTheme } = useAppStore()
const { username, openAccountModal } = useSession()
const { gatewayState } = useGatewayStatus()
const { unreadCount } = useToast()
const logout = useLogout()

const isDropdownOpen = ref(false)
const isNotificationOpen = ref(false)
const menuRef = ref(null)
const notificationRef = ref(null)
const bellBtnRef = ref(null)

const currentTitle = computed(() => {
  return route.meta?.title || '控制中心'
})

const toggleNotificationPopover = () => {
  isNotificationOpen.value = !isNotificationOpen.value
  if (isNotificationOpen.value) isDropdownOpen.value = false
}

const closeNotificationPopover = (restoreFocus = false) => {
  isNotificationOpen.value = false
  if (restoreFocus) bellBtnRef.value?.focus()
}

const toggleUserMenu = () => {
  isDropdownOpen.value = !isDropdownOpen.value
  if (isDropdownOpen.value) isNotificationOpen.value = false
}

const handleOpenAccount = () => {
  isDropdownOpen.value = false
  openAccountModal()
}

const handleToggleThemeFromMenu = () => {
  toggleTheme()
}

const handleOpenCmdkFromMenu = () => {
  isDropdownOpen.value = false
  openCmdk()
}

const handleLogout = () => {
  isDropdownOpen.value = false
  return logout()
}

const handleClickOutside = (e) => {
  if (menuRef.value && !menuRef.value.contains(e.target)) {
    isDropdownOpen.value = false
  }
  if (notificationRef.value && !notificationRef.value.contains(e.target)) {
    isNotificationOpen.value = false
  }
}

const handleKeydown = (e) => {
  if (e.key === 'Escape' && isNotificationOpen.value) {
    e.stopPropagation()
    closeNotificationPopover(true)
  }
}

watch(() => route.fullPath, () => {
  isDropdownOpen.value = false
  isNotificationOpen.value = false
})

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('keydown', handleKeydown)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleKeydown)
})
</script>

<style scoped>
.top-header {
  height: var(--topbar-h);
  background: var(--sidebar-bg);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
  position: sticky;
  top: 0;
  z-index: 100;
  gap: 16px;
}

.header-left {
  display: none;
  align-items: center;
  gap: 12px;
}

.mobile-toggle-btn {
  display: none;
  color: var(--ink);
  padding: 6px;
  border-radius: var(--radius-sm);
  background: var(--strip-bg);
  border: 1px solid var(--line);
  cursor: pointer;
}
.mobile-toggle-btn:hover {
  background: var(--card-hover-bg);
}

.current-page {
  font-size: 13.5px;
  color: var(--ink);
  font-weight: 600;
}

#home-breadcrumb:empty { display: none; }
@media (max-width: 1100px) { #home-breadcrumb { display: none; } }

/* Center ⌘K Trigger */
.header-center {
  flex: 1;
  max-width: 440px;
}

.cmdk-trigger-btn {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 14px;
  background: var(--strip-bg);
  border: 1px solid var(--line);
  border-radius: var(--radius-pill);
  color: var(--muted);
  font-size: 12.5px;
  cursor: pointer;
  transition: var(--transition-smooth);
}
.cmdk-trigger-btn:hover {
  border-color: var(--card-border-hover);
  background: var(--card-hover-bg);
  color: var(--ink);
}

.search-icon {
  color: var(--faint);
}

.search-placeholder {
  flex: 1;
  text-align: left;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.shortcut-pill {
  background: var(--input-bg);
  border: 1px solid var(--line);
  border-radius: 4px;
  padding: 1px 6px;
  font-size: 10.5px;
  font-family: var(--mono);
  color: var(--faint);
}

/* Header Right Controls */
.header-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.gateway-live-pill {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 4px 10px;
  border-radius: var(--radius-pill);
  background: rgba(16, 185, 129, 0.1);
  border: 1px solid rgba(16, 185, 129, 0.25);
  font-size: 11.5px;
  color: var(--ok);
  font-weight: 600;
}

.pulse-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--ok);
  box-shadow: var(--ok-glow);
  animation: pulseDot 2s infinite ease-in-out;
}

.gateway-live-pill.warn, .gateway-live-pill.pending {
  background: var(--strip-bg);
  border-color: var(--line);
}
.gateway-live-pill.warn { color: var(--warn); }
.gateway-live-pill.pending { color: var(--muted); }
.gateway-live-pill:not(.ok) .pulse-dot {
  background: currentColor;
  box-shadow: none;
  animation: none;
}

@keyframes pulseDot {
  0%, 100% { opacity: 1; transform: scale(1); }
  50% { opacity: 0.4; transform: scale(0.85); }
}

.icon-action-btn {
  padding: 7px;
  border-radius: var(--radius-sm);
  color: var(--muted);
  background: var(--strip-bg);
  border: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: var(--transition-smooth);
}
.icon-action-btn:hover {
  color: var(--ink);
  background: var(--card-hover-bg);
  border-color: var(--card-border-hover);
}

.mobile-search-btn {
  display: none;
}

.notification-btn {
  position: relative;
}
.notification-btn.has-unread {
  color: var(--ink);
}
.notification-count {
  position: absolute;
  top: -3px;
  right: -3px;
  background: color-mix(in srgb, var(--brand) 18%, var(--popover-bg));
  color: var(--ink);
  font-size: 10px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  min-width: 17px;
  height: 17px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0 3px;
  border: 1px solid var(--card-border-hover);
  line-height: 1;
}
.notification-btn.active {
  color: var(--brand);
  border-color: var(--card-border-hover);
  background: var(--card-hover-bg, var(--strip-bg));
}

/* Profile Dropdown & Notification Popover Wrapper */
.notification-wrapper,
.user-menu-wrapper {
  position: relative;
}

.user-profile-badge {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 10px 4px 5px;
  border-radius: var(--radius-pill);
  background: var(--strip-bg);
  border: 1px solid var(--line);
  cursor: pointer;
  transition: var(--transition-smooth);
}
.user-profile-badge:hover {
  background: var(--card-hover-bg);
  border-color: var(--card-border-hover);
}

.user-avatar {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  background: var(--brand-gradient);
  color: #fff;
  font-size: 11.5px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
}

.username {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--ink);
}

.chevron-icon {
  color: var(--faint);
  transition: transform 0.2s ease;
}
.chevron-icon.rotate-180 {
  transform: rotate(180deg);
}

/* Dropdown Popover */
.user-dropdown-popover {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  width: 240px;
  background: var(--popover-bg, #111827);
  backdrop-filter: blur(25px);
  -webkit-backdrop-filter: blur(25px);
  border: 1px solid var(--popover-border, rgba(255, 255, 255, 0.12));
  border-radius: 14px;
  padding: 8px;
  box-shadow: 0 16px 36px rgba(0, 0, 0, 0.45);
  z-index: 1000;
  display: flex;
  flex-direction: column;
}

body.theme-light .user-dropdown-popover {
  background: #ffffff !important;
  border-color: rgba(226, 232, 240, 1) !important;
  box-shadow: 0 12px 30px rgba(15, 23, 42, 0.15) !important;
}

body.theme-dark .user-dropdown-popover {
  background: #111827 !important;
  border-color: rgba(255, 255, 255, 0.12) !important;
  box-shadow: 0 20px 48px rgba(0, 0, 0, 0.6) !important;
}

.popover-user-info {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
}

.popover-avatar {
  width: 34px;
  height: 34px;
  border-radius: 50%;
  background: var(--brand-gradient);
  color: #fff;
  font-size: 14px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
}

.popover-user-meta {
  display: flex;
  flex-direction: column;
}

.popover-username {
  font-size: 13px;
  font-weight: 700;
  color: var(--ink);
}

.popover-role {
  font-size: 11px;
  color: var(--muted);
}

.popover-divider {
  height: 1px;
  background: var(--line);
  margin: 6px 0;
}

.popover-menu-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.menu-item-btn {
  display: flex;
  align-items: center;
  gap: 9px;
  width: 100%;
  padding: 8px 10px;
  background: transparent;
  border: none;
  border-radius: 8px;
  color: var(--ink-secondary);
  font-size: 12.5px;
  cursor: pointer;
  text-align: left;
  transition: var(--transition-smooth);
}
.menu-item-btn:hover {
  background: var(--strip-bg);
  color: var(--ink);
}

.menu-item-icon {
  color: var(--faint);
}

.menu-item-btn:hover .menu-item-icon {
  color: var(--brand-light);
}

.danger-item {
  color: var(--danger);
}
.danger-item:hover {
  background: rgba(244, 63, 94, 0.1);
  color: var(--danger);
}
.danger-item .menu-item-icon {
  color: var(--danger);
}

/* Animations */
.popover-fade-enter-active,
.popover-fade-leave-active {
  transition: opacity 0.18s ease, transform 0.18s ease;
}
.popover-fade-enter-from,
.popover-fade-leave-to {
  opacity: 0;
  transform: translateY(-6px) scale(0.97);
}

.assistant-toggle { color: var(--brand-light); }
.assistant-toggle[aria-expanded="true"] { border-color: var(--card-border-hover); background: var(--strip-bg); }
.assistant-toggle span { display: none; }
@media (min-width: 1280px) {
  .assistant-toggle { width: auto; gap: 6px; padding-inline: 9px; }
  .assistant-toggle span { display: inline; font-size: 12px; white-space: nowrap; }
}
@media (max-width: 420px) {
  .top-header .gateway-live-pill { display: none; }
}

/* Responsive */
@media (max-width: 1023px) {
  .top-header {
    padding-inline: 14px;
    gap: 8px;
  }
  .header-left { display: flex; min-width: 0; }
  .current-page {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mobile-toggle-btn {
    display: flex;
  }
  .header-center {
    display: none;
  }
  .mobile-search-btn {
    display: flex;
  }
  .header-right {
    gap: 8px;
    flex-shrink: 0;
  }
  .live-text {
    display: none;
  }
  .gateway-live-pill {
    width: 32px;
    height: 32px;
    padding: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .icon-action-btn {
    width: 32px;
    height: 32px;
    padding: 0;
    flex-shrink: 0;
  }
  .user-profile-badge {
    height: 32px;
    box-sizing: border-box;
  }
}
@media (max-width: 600px) {
  .user-profile-badge .username {
    display: none;
  }
  .user-profile-badge {
    padding: 3px 6px 3px 3px;
    gap: 4px;
  }
  .user-profile-badge .chevron-icon {
    width: 12px;
    height: 12px;
  }
}
</style>
