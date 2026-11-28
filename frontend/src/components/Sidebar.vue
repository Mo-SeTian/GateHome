<template>
  <div>
    <!-- Mobile Backdrop Overlay -->
    <transition name="fade">
      <div 
        v-if="isSidebarOpen" 
        class="mobile-backdrop"
        @click="dismissSidebar"
      />
    </transition>

    <!-- Sidebar Container -->
    <aside 
      id="app-sidebar"
      class="app-sidebar"
      :class="{ 'mobile-open': isSidebarOpen }"
      :inert="isMobile && !isSidebarOpen"
      @keydown.esc="isSidebarOpen && dismissSidebar()"
    >
      <!-- Brand Logo Section -->
      <div class="sidebar-brand">
        <img class="brand-icon" src="/favicon.svg" width="32" height="32" alt="" />
        <div class="brand-info">
          <span class="brand-title">GateHome</span>
          <span class="brand-version">{{ gatewayVersion }}</span>
        </div>
        <button ref="closeButton" class="mobile-close-btn" type="button" aria-label="关闭侧边栏导航" @click="dismissSidebar">
          <X :size="18" />
        </button>
      </div>

      <nav class="sidebar-nav" aria-label="主导航">
        <div v-for="(group, index) in groups" :key="group.label" class="nav-group">
          <button type="button" class="nav-section-title" :class="{ 'section-active': group === activeGroup }"
            :aria-expanded="isGroupOpen(group.label)" :aria-controls="'nav-group-' + index" @click="toggleGroup(group.label)">
            <component :is="group.icon" :size="16" /><span>{{ group.label }}</span><ChevronDown :size="15" class="section-chevron" />
          </button>
          <div v-show="isGroupOpen(group.label)" :id="'nav-group-' + index">
            <router-link v-for="[path, label, icon] in group.items" :key="path" :to="path" class="nav-link"
              :class="{ active: path === activeNav.path }"
              :aria-current="path === activeNav.path ? (activeNav.exact ? 'page' : 'location') : undefined"
              :aria-label="path === '/certificates' && certificatesExpiring ? `SSL 证书，${certificatesExpiring} 张即将到期或已过期` : undefined"
              @click="closeSidebar">
              <component :is="icon" :size="14" class="nav-icon" />
              <span class="nav-text">{{ label }}</span>
              <span v-if="path === '/certificates' && certificatesExpiring" class="nav-badge" title="30 天内到期或已过期">{{ certificatesExpiring }}</span>
              <span v-if="path === '/notifications' && unreadCount" class="nav-badge">{{ unreadCount }}</span>
            </router-link>
          </div>
        </div>
      </nav>
    </aside>
  </div>
</template>

<script setup>
import { useToast } from '@/composables/useToast'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { 
  House, GitFork, X, ChevronDown, Sparkles, Cable, LayoutDashboard, Network, ArrowLeftRight, Compass, Images,
  Globe, Lock, Download, ShieldAlert, ScrollText,
  Bell, BarChart3, Activity, Bot, RefreshCw, HardDriveDownload, Settings
} from 'lucide-vue-next'
import { useAppStore } from '@/composables/useAppStore'
import { useGatewayStatus } from '@/composables/useGatewayStatus'

const route = useRoute()
const groups = [
  { label: '工作台', icon: LayoutDashboard, items: [
    ['/overview', '概览', LayoutDashboard], ['/sunpanel', '浏览器首页', Compass], ['/icons', '图标库', Images]
  ] },
  { label: '服务接入', icon: Network, items: [
    ['/routes', '反向代理', Network], ['/forwards', '端口转发', ArrowLeftRight],
    ['/remote-home', '远程回家', House], ['/mihomo', '代理分流', GitFork], ['/ddns', '动态域名', Globe], ['/certificates', 'SSL 证书', Lock]
  ] },
  { label: '访问安全', icon: ShieldAlert, items: [
    ['/access', '防火墙管理', ShieldAlert], ['/subscriptions', '订阅管理', Download]
  ] },
  { label: '监控与日志', icon: Activity, items: [
    ['/runtime', '运行状态', Activity], ['/notifications', '通知中心', Bell],
    ['/logs', '日志与拦截', ScrollText], ['/statistics', '数据统计', BarChart3]
  ] },
  { label: 'AI 与集成', icon: Sparkles, items: [
    ['/assistant', '网关助手', Sparkles], ['/assistant/settings', '模型接入点', Cable], ['/mcp', 'MCP 授权', Bot]
  ] },
  { label: '系统维护', icon: Settings, items: [
    ['/updates', '更新', RefreshCw], ['/backups', '备份与恢复', HardDriveDownload], ['/settings', '设置', Settings]
  ] }
]
const groupPreferenceKey = 'gh_sidebar_expanded_groups'
function readExpandedGroups() {
  try {
    const saved = JSON.parse(localStorage.getItem(groupPreferenceKey) || '[]')
    if (Array.isArray(saved)) return new Set(groups.map(group => group.label).filter(label => saved.includes(label)))
  } catch {}
  return new Set()
}
const expandedGroups = ref(readExpandedGroups())
const collapsedActiveGroup = ref('')
const activeNav = computed(() => {
  const current = route.matched.at(-1)?.aliasOf?.path ?? route.path
  let path = ''
  let group
  for (const item of groups) {
    for (const [target] of item.items) {
      if ((current === target || current.startsWith(target + '/')) && target.length > path.length) {
        path = target
        group = item
      }
    }
  }
  return { group, path, exact: path === current }
})
const activeGroup = computed(() => activeNav.value.group)
function isGroupOpen(label) {
  return expandedGroups.value.has(label) ||
    (activeGroup.value?.label === label && collapsedActiveGroup.value !== label)
}
function toggleGroup(label) {
  if (isGroupOpen(label)) {
    expandedGroups.value.delete(label)
    if (activeGroup.value?.label === label) collapsedActiveGroup.value = label
  } else {
    expandedGroups.value.add(label)
    if (collapsedActiveGroup.value === label) collapsedActiveGroup.value = ''
  }
  try { localStorage.setItem(groupPreferenceKey, JSON.stringify([...expandedGroups.value])) } catch {}
}

const { unreadCount } = useToast()
const { isSidebarOpen, closeSidebar } = useAppStore()
const closeButton = ref(null)
const mobileLayout = window.matchMedia('(max-width: 1023px)')
const isMobile = ref(mobileLayout.matches)
const onLayoutChange = ({ matches }) => {
  isMobile.value = matches
  if (!matches) closeSidebar()
}
onMounted(() => mobileLayout.addEventListener('change', onLayoutChange))
onUnmounted(() => mobileLayout.removeEventListener('change', onLayoutChange))
watch(() => route.path, () => {
  collapsedActiveGroup.value = ''
  closeSidebar()
})
watch(isSidebarOpen, open => {
  if (open) requestAnimationFrame(() => {
    if (isSidebarOpen.value) closeButton.value?.focus({ preventScroll: true })
  })
}, { flush: 'post' })
function dismissSidebar() {
  closeSidebar()
  document.getElementById('mobile-nav-toggle')?.focus()
}
const { gatewayStatus, gatewayVersion } = useGatewayStatus()
const certificatesExpiring = computed(() => (gatewayStatus.value?.certificates ?? []).filter(cert =>
  cert.ready && Date.parse(cert.not_after) <= Date.now() + 30 * 86400000
).length)
</script>

<style scoped>
.mobile-backdrop {
  display: none;
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
  transition: transform 0.3s cubic-bezier(0.16, 1, 0.3, 1);
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

/* ================= Mobile Breakpoints ================= */
@media (max-width: 1023px) {
  .mobile-backdrop {
    display: block;
  }
  .app-sidebar {
    transform: translateX(-105%);
    box-shadow: none;
  }
  .app-sidebar.mobile-open {
    transform: translateX(0);
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
}
</style>
