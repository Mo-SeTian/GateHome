<template>
  <header class="top-header">
    <div class="header-left">
      <!-- Mobile Hamburger Menu Button -->
      <button class="mobile-toggle-btn" @click="toggleSidebar" aria-label="打开侧边栏导航">
        <Menu :size="20" />
      </button>

      <!-- Page Breadcrumb & Title -->
      <div class="breadcrumb-container">
        <span class="crumb-root">GateHome</span>
        <span class="crumb-separator">/</span>
        <span class="crumb-current">{{ currentTitle }}</span>
      </div>
    </div>

    <!-- Center Search Launcher (⌘K) -->
    <div class="header-center">
      <button class="cmdk-trigger-btn" @click="openCmdk">
        <Search :size="15" class="search-icon" />
        <span class="search-placeholder">搜索页面与快捷操作...</span>
        <kbd class="shortcut-pill">⌘K</kbd>
      </button>
    </div>

    <!-- Right Controls -->
    <div class="header-right">
      <!-- Global Gateway Status Badge -->
      <div class="gateway-live-pill" :class="gatewayState.tone" :title="gatewayState.label">
        <span class="pulse-dot" />
        <span class="live-text">{{ gatewayState.label }}</span>
      </div>

      <!-- Theme Switch Button -->
      <button 
        class="icon-action-btn" 
        :title="theme === 'dark' ? '切换为浅色模式' : '切换为深色模式'" 
        @click="toggleTheme"
      >
        <Sun v-if="theme === 'dark'" :size="18" />
        <Moon v-else :size="18" />
      </button>

      <!-- Notification Center Button -->
      <button 
        class="icon-action-btn notification-btn" 
        :class="{ 'has-unread': unreadCount > 0, active: isCenterOpen }"
        :title="unreadCount > 0 ? `通知中心 (${unreadCount} 条未读)` : '通知中心'" 
        @click="toggleCenter"
        aria-label="打开通知中心"
      >
        <Bell :size="18" />
        <span v-if="unreadCount > 0" class="badge-dot">{{ unreadCount > 99 ? '99+' : unreadCount }}</span>
      </button>

      <!-- User Profile Dropdown Menu -->
      <div class="user-menu-wrapper" ref="menuRef">
        <button 
          class="user-profile-badge" 
          @click="isDropdownOpen = !isDropdownOpen"
          :aria-expanded="isDropdownOpen"
        >
          <div class="user-avatar">
            <span>{{ username.slice(0, 1).toUpperCase() }}</span>
            <span class="avatar-status-dot" />
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
                <span>外观模式: {{ theme === 'dark' ? '深色黑曜石' : '浅色晨曦' }}</span>
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
import { computed, ref, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { 
  Menu, Search, Sun, Moon, ChevronDown, 
  KeyRound, LogOut, Terminal, Bell
} from 'lucide-vue-next'
import { useAppStore } from '@/composables/useAppStore'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useLogout, useSession } from '@/composables/useSession'
import { useToast } from '@/composables/useToast'

const route = useRoute()
const { toggleSidebar, openCmdk, theme, toggleTheme } = useAppStore()
const { username, openAccountModal } = useSession()
const { gatewayState } = useGatewayStatus()
const { unreadCount, toggleCenter, isCenterOpen } = useToast()
const logout = useLogout()

const isDropdownOpen = ref(false)
const menuRef = ref(null)

const currentTitle = computed(() => {
  return route.meta?.title || '控制中心'
})

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
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
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
  display: flex;
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

.breadcrumb-container {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13.5px;
}

.crumb-root {
  color: var(--faint);
  font-weight: 500;
}

.crumb-separator {
  color: var(--faint);
  font-size: 12px;
}

.crumb-current {
  color: var(--ink);
  font-weight: 600;
}

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

.notification-btn {
  position: relative;
}
.notification-btn.has-unread {
  color: var(--ink);
}
.notification-btn .badge-dot {
  position: absolute;
  top: -3px;
  right: -3px;
  background: var(--bad);
  color: #fff;
  font-size: 9px;
  font-weight: 700;
  min-width: 15px;
  height: 15px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0 3px;
  border: 2px solid var(--canvas);
  line-height: 1;
}
.notification-btn.active {
  color: var(--brand);
  border-color: var(--card-border-hover);
  background: var(--card-hover-bg, var(--strip-bg));
}

/* Profile Dropdown Wrapper */
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
  position: relative;
}

.avatar-status-dot {
  position: absolute;
  bottom: -1px;
  right: -1px;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--ok);
  border: 1.5px solid var(--canvas);
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

/* Responsive */
@media (max-width: 1023px) {
  .top-header {
    padding-inline: 14px;
    gap: 8px;
  }
  .header-left, .breadcrumb-container { min-width: 0; }
  .crumb-current {
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
@media (max-width: 420px) {
  .crumb-root, .crumb-separator { display: none; }
}
</style>
