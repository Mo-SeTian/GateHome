<template>
  <div>
    <!-- Mobile Backdrop Overlay -->
    <transition name="fade">
      <div 
        v-if="isSidebarOpen" 
        class="mobile-backdrop"
        @click="closeSidebar"
      />
    </transition>

    <!-- Sidebar Container -->
    <aside 
      class="app-sidebar"
      :class="{ 'mobile-open': isSidebarOpen }"
    >
      <!-- Brand Logo Section -->
      <div class="sidebar-brand">
        <img class="brand-icon" src="/favicon.svg" width="32" height="32" alt="" />
        <div class="brand-info">
          <span class="brand-title">GateHome</span>
          <span class="brand-version">{{ gatewayVersion }}</span>
        </div>
        <button class="mobile-close-btn" aria-label="关闭侧边栏导航" @click="closeSidebar">
          <X :size="18" />
        </button>
      </div>

      <nav class="sidebar-nav" aria-label="主导航">
        <div v-for="(group, index) in groups" :key="group.label" class="nav-group">
          <button type="button" class="nav-section-title" :class="{ 'section-active': group.items.some(item => item[0] === route.path) }"
            :aria-expanded="openGroups.has(group.label)" :aria-controls="'nav-group-' + index" @click="toggleGroup(group.label)">
            <component :is="group.icon" :size="16" /><span>{{ group.label }}</span><ChevronDown :size="15" class="section-chevron" />
          </button>
          <div v-show="openGroups.has(group.label)" :id="'nav-group-' + index">
            <router-link v-for="[path, label, icon] in group.items" :key="path" :to="path" class="nav-link" exact-active-class="active"
              :aria-label="path === '/certificates' && certificatesExpiring ? `SSL 证书，${certificatesExpiring} 张即将到期或已过期` : undefined"
              @click="onNavClick">
              <component :is="icon" :size="14" class="nav-icon" />
              <span class="nav-text">{{ label }}</span>
              <span v-if="path === '/certificates' && certificatesExpiring" class="nav-badge" title="30 天内到期或已过期">{{ certificatesExpiring }}</span>
              <span v-if="path === '/notifications' && unreadCount" class="nav-badge">{{ unreadCount }}</span>
            </router-link>
          </div>
        </div>
      </nav>

      <!-- Sidebar Footer Status Card -->
      <div class="sidebar-footer">
        <div class="status-card">
          <div class="status-top">
            <span class="status-indicator">
              <span class="status-dot" :class="gatewayState.tone === 'pending' ? 'neutral' : gatewayState.tone" />
              {{ gatewayState.label }}
            </span>
            <span class="uptime-text" title="网关运行时长">{{ uptime }}</span>
          </div>
          <div class="status-details">
            <span class="admin-addr" :title="`管理入口 ${adminAddress}`">入口 {{ adminAddress }}</span>
            <button type="button" class="login-link-btn" :disabled="isLoggingOut" @click="handleLogout" title="退出当前登录">
              <LogOut :size="12" class="logout-mini-icon" />
              <span>退出登录</span>
            </button>
          </div>
        </div>
      </div>
    </aside>
  </div>
</template>

<script setup>
import { useToast } from '@/composables/useToast'
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { 
  House, X, ChevronDown, Sparkles, Cable, LayoutDashboard, Network, ArrowLeftRight, Compass, Images,
  Globe, Lock, Download, ShieldAlert, FileSearch, ScrollText, Ban,
  Bell, BarChart3, Activity, Bot, RefreshCw, HardDriveDownload, Settings, LogOut
} from 'lucide-vue-next'
import { useAppStore } from '@/composables/useAppStore'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useLogout, useSession } from '@/composables/useSession'

const route = useRoute()
const groups = [
  { label: '工作台', icon: LayoutDashboard, items: [
    ['/overview', '概览', LayoutDashboard], ['/sunpanel', '浏览器首页', Compass], ['/icons', '图标库', Images]
  ] },
  { label: '服务接入', icon: Network, items: [
    ['/routes', '反向代理', Network], ['/forwards', '端口转发', ArrowLeftRight],
    ['/remote-home', '远程回家', House], ['/ddns', '动态域名', Globe], ['/certificates', 'SSL 证书', Lock]
  ] },
  { label: '访问安全', icon: ShieldAlert, items: [
    ['/access', '防火墙管理', ShieldAlert], ['/subscriptions', '订阅管理', Download],
    ['/ip-blocks', 'IP 拦截名单', Ban], ['/firewall-logs', '防火墙拦截记录', FileSearch]
  ] },
  { label: '监控与日志', icon: Activity, items: [
    ['/runtime', '运行状态', Activity], ['/notifications', '通知中心', Bell], ['/logs', '日志', ScrollText],
    ['/statistics', '统计', BarChart3], ['/traffic', '流量统计', Activity]
  ] },
  { label: 'AI 与集成', icon: Sparkles, items: [
    ['/assistant', '网关助手', Sparkles], ['/assistant/settings', '模型接入点', Cable], ['/mcp', 'MCP 授权', Bot]
  ] },
  { label: '系统维护', icon: Settings, items: [
    ['/updates', '更新', RefreshCw], ['/backups', '备份与恢复', HardDriveDownload], ['/settings', '设置', Settings]
  ] }
]
const openGroups = ref(new Set(['工作台', '服务接入', 'AI 与集成']))
function toggleGroup(label) {
  if (openGroups.value.has(label)) openGroups.value.delete(label)
  else openGroups.value.add(label)
}
watch(() => route.path, path => {
  const group = groups.find(({ items }) => items.some(([target]) => target === path))
  if (group) openGroups.value.add(group.label)
}, { immediate: true })

const { unreadCount } = useToast()
const { isSidebarOpen, closeSidebar } = useAppStore()
const { gatewayStatus, gatewayVersion, gatewayState } = useGatewayStatus()
const { isLoggingOut } = useSession()
const logout = useLogout()
const adminAddress = window.location.host
const certificatesExpiring = computed(() => (gatewayStatus.value?.certificates ?? []).filter(cert =>
  cert.ready && Date.parse(cert.not_after) <= Date.now() + 30 * 86400000
).length)
const uptime = computed(() => {
  const seconds = gatewayStatus.value?.uptime_seconds
  if (seconds == null) return '—'
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor(seconds / 3600) % 24
  const minutes = Math.floor(seconds / 60) % 60
  if (days) return `${days}天 ${hours}小时`
  if (hours) return `${hours}小时 ${minutes}分`
  return minutes ? `${minutes}分` : `${seconds}秒`
})

const onNavClick = () => {
  if (window.innerWidth < 1024) {
    closeSidebar()
  }
}

const handleLogout = () => {
  closeSidebar()
  return logout()
}
</script>

<style scoped>
.mobile-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.65);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  z-index: 990;
}

.app-sidebar {
  width: var(--sidebar-w);
  height: 100vh;
  height: 100dvh;
  position: fixed;
  top: 0;
  left: 0;
  z-index: 1000;
  background: var(--sidebar-bg);
  backdrop-filter: blur(25px);
  -webkit-backdrop-filter: blur(25px);
  border-right: 1px solid var(--line);
  display: flex;
  flex-direction: column;
  transition: transform 0.3s cubic-bezier(0.16, 1, 0.3, 1), visibility 0.3s ease;
}

.sidebar-brand {
  height: var(--topbar-h);
  padding: 0 12px;
  display: flex;
  align-items: center;
  gap: 10px;
  border-bottom: 1px solid var(--line);
  flex-shrink: 0;
}

.brand-icon {
  flex-shrink: 0;
}

.brand-info {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-width: 0;
}

.brand-title {
  font-size: 14.5px;
  font-weight: 700;
  color: var(--ink);
  letter-spacing: -0.2px;
  line-height: 1.25;
}

.brand-version {
  font-size: 10.5px;
  color: var(--faint);
  font-family: var(--mono);
  line-height: 1.3;
  white-space: nowrap;
}

.mobile-close-btn {
  display: none;
  color: var(--muted);
  padding: 4px;
}

.sidebar-nav {
  flex: 1;
  overflow-y: auto;
  padding: 8px 12px 16px;
}
.nav-group {
  padding: 6px 0;
  border-bottom: 1px solid var(--line);
}
.nav-section-title {
  display: flex;
  align-items: center;
  gap: 9px;
  width: 100%;
  background: transparent;
  border: 0;
  text-align: left;
  padding: 9px 10px;
  color: var(--ink-secondary);
  font-size: 13px;
  font-weight: 650;
  cursor: pointer;
  user-select: none;
}
.nav-section-title:hover, .section-active { color: var(--ink); }
.section-chevron { margin-left: auto; color: var(--muted); transform: rotate(-90deg); }
.nav-section-title[aria-expanded="true"] .section-chevron { transform: none; }
.nav-link {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 10px 7px 25px;
  min-height: 33px;
  border-radius: var(--radius-sm);
  color: var(--muted);
  font-size: 13px;
  font-weight: 500;
  line-height: 1.4;
  transition: var(--transition-smooth);
  position: relative;
  text-decoration: none;
  box-sizing: border-box;
}

.nav-link:hover {
  background: rgba(148, 163, 184, 0.1);
  color: var(--ink);
}

.nav-link.active {
  background: rgba(99, 102, 241, 0.15);
  color: #ffffff;
  font-weight: 600;
}

.theme-light .nav-link.active {
  background: rgba(99, 102, 241, 0.12);
  color: var(--brand);
}

.nav-icon {
  width: 14px;
  height: 14px;
  color: var(--faint);
  transition: color 0.2s ease;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}
.nav-link:hover .nav-icon,
.nav-link.active .nav-icon {
  color: var(--brand-light);
}

.nav-text {
  flex: 1;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  letter-spacing: -0.1px;
}

.nav-badge {
  font-size: 10.5px;
  line-height: 1.2;
  padding: 1.5px 6.5px;
  border-radius: var(--radius-pill);
  background: rgba(245, 158, 11, 0.2);
  color: var(--warn);
  border: 1px solid rgba(245, 158, 11, 0.35);
  font-family: var(--mono);
  font-weight: 600;
  flex-shrink: 0;
}

.sidebar-footer {
  padding: 12px;
  border-top: 1px solid var(--line);
  flex-shrink: 0;
}

.status-card {
  background: var(--strip-bg);
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  padding: 10px 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  box-sizing: border-box;
}

.status-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.status-indicator {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
  font-size: 11.5px;
  color: var(--ok);
}

.uptime-text {
  color: var(--faint);
  font-family: var(--mono);
  font-size: 11px;
}

.status-details {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  color: var(--faint);
  font-size: 11px;
}

.admin-addr {
  flex: 1;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  font-family: var(--mono);
}

.login-link-btn {
  flex-shrink: 0 !important;
  white-space: nowrap !important;
  word-break: keep-all !important;
  background: transparent;
  border: none;
  padding: 2px 6px;
  border-radius: var(--radius-sm);
  font-size: 11px;
  color: var(--brand-light);
  cursor: pointer;
  transition: var(--transition-smooth);
  display: inline-flex;
  align-items: center;
  gap: 3px;
}
.login-link-btn:hover {
  background: rgba(99, 102, 241, 0.12);
  color: var(--ink);
}
.logout-mini-icon {
  flex-shrink: 0;
  opacity: 0.85;
}

.theme-light .login-link-btn,
.theme-light .status-indicator {
  color: var(--ink-secondary);
}

/* ================= Mobile Breakpoints ================= */
@media (max-width: 1023px) {
  .app-sidebar {
    transform: translateX(-105%);
    visibility: hidden;
    box-shadow: none;
  }
  .app-sidebar.mobile-open {
    transform: translateX(0);
    visibility: visible;
    box-shadow: 25px 0 60px rgba(0, 0, 0, 0.7);
  }
  .mobile-close-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    flex-shrink: 0;
    border-radius: var(--radius-sm);
    background: transparent;
    border: none;
    color: var(--muted);
    cursor: pointer;
    transition: var(--transition-smooth);
  }
  .mobile-close-btn:hover {
    color: var(--ink);
    background: rgba(148, 163, 184, 0.12);
  }
  .nav-link, .nav-section-title {
    min-height: 44px;
  }
  .login-link-btn {
    min-height: 44px;
  }
}
</style>
