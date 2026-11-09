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
        <div class="brand-icon">
          <svg viewBox="0 0 32 32" width="22" height="22">
            <defs>
              <linearGradient id="brand-grad" x1="0%" y1="0%" x2="100%" y2="100%">
                <stop offset="0%" stop-color="#6366f1" />
                <stop offset="100%" stop-color="#06b6d4" />
              </linearGradient>
            </defs>
            <path d="M16 4L4 10.5v11L16 28l12-6.5v-11L16 4zm0 3.5l8.5 4.6-8.5 4.7-8.5-4.7L16 7.5zM6.5 19.8V12l8 4.4v7.8l-8-4.4zm10.5 4.4v-7.8l8-4.4v7.8l-8 4.4z" fill="url(#brand-grad)" />
          </svg>
        </div>
        <div class="brand-info">
          <span class="brand-title">GateHome</span>
          <span class="brand-version">家庭网关 · {{ gatewayVersion }}</span>
        </div>
        <button class="mobile-close-btn" @click="closeSidebar">
          <X :size="18" />
        </button>
      </div>

      <!-- Navigation Menus (严格 1:1 对齐 GateHome 实际两大分组) -->
      <nav class="sidebar-nav">
        <!-- 分组 1: 工作空间 -->
        <div class="nav-section-title">工作空间</div>
        <router-link to="/overview" class="nav-link" active-class="active" @click="onNavClick">
          <LayoutDashboard :size="17" class="nav-icon" />
          <span class="nav-text">概览</span>
        </router-link>
        <router-link to="/routes" class="nav-link" active-class="active" @click="onNavClick">
          <Network :size="17" class="nav-icon" />
          <span class="nav-text">反向代理</span>
          <span v-if="gatewayConfig" class="nav-badge brand">{{ gatewayConfig.routes?.length ?? 0 }}</span>
        </router-link>
        <router-link to="/forwards" class="nav-link" active-class="active" @click="onNavClick">
          <ArrowLeftRight :size="17" class="nav-icon" />
          <span class="nav-text">端口转发</span>
        </router-link>
        <router-link to="/sunpanel" class="nav-link" active-class="active" @click="onNavClick">
          <Compass :size="17" class="nav-icon" />
          <span class="nav-text">浏览器首页</span>
        </router-link>
        <router-link to="/ddns" class="nav-link" active-class="active" @click="onNavClick">
          <Globe :size="17" class="nav-icon" />
          <span class="nav-text">动态域名</span>
        </router-link>
        <router-link to="/certificates" class="nav-link" active-class="active" @click="onNavClick">
          <Lock :size="17" class="nav-icon" />
          <span class="nav-text">SSL 证书</span>
          <span v-if="certificatesExpiring" class="nav-badge warn" title="30 天内到期或已过期">{{ certificatesExpiring }}</span>
        </router-link>
        <router-link to="/subscriptions" class="nav-link" active-class="active" @click="onNavClick">
          <Download :size="17" class="nav-icon" />
          <span class="nav-text">订阅管理</span>
        </router-link>
        <router-link to="/access" class="nav-link" active-class="active" @click="onNavClick">
          <ShieldAlert :size="17" class="nav-icon" />
          <span class="nav-text">防火墙管理</span>
          <span v-if="firewallAttached" class="nav-status-dot active" title="已关联启用的反代规则" />
        </router-link>

        <!-- 分组 2: 观察与管理 -->
        <div class="nav-section-title">观察与管理</div>
        <router-link to="/firewall-logs" class="nav-link" active-class="active" @click="onNavClick">
          <FileSearch :size="17" class="nav-icon" />
          <span class="nav-text">防火墙拦截记录</span>
        </router-link>
        <router-link to="/logs" class="nav-link" active-class="active" @click="onNavClick">
          <ScrollText :size="17" class="nav-icon" />
          <span class="nav-text">日志</span>
        </router-link>
        <router-link to="/ip-blocks" class="nav-link" active-class="active" @click="onNavClick">
          <Ban :size="17" class="nav-icon" />
          <span class="nav-text">IP 拦截名单</span>
        </router-link>
        <router-link to="/statistics" class="nav-link" active-class="active" @click="onNavClick">
          <BarChart3 :size="17" class="nav-icon" />
          <span class="nav-text">统计</span>
        </router-link>
        <router-link to="/traffic" class="nav-link" active-class="active" @click="onNavClick">
          <Activity :size="17" class="nav-icon" />
          <span class="nav-text">流量统计</span>
        </router-link>
        <router-link to="/mcp" class="nav-link" active-class="active" @click="onNavClick">
          <Bot :size="17" class="nav-icon" />
          <span class="nav-text">AI 管理</span>
          <span class="nav-badge ai">AI</span>
        </router-link>
        <router-link to="/updates" class="nav-link" active-class="active" @click="onNavClick">
          <RefreshCw :size="17" class="nav-icon" />
          <span class="nav-text">更新</span>
        </router-link>
        <router-link to="/backups" class="nav-link" active-class="active" @click="onNavClick">
          <HardDriveDownload :size="17" class="nav-icon" />
          <span class="nav-text">备份与恢复</span>
        </router-link>
        <router-link to="/settings" class="nav-link" active-class="active" @click="onNavClick">
          <Settings :size="17" class="nav-icon" />
          <span class="nav-text">设置</span>
        </router-link>
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
import { computed } from 'vue'
import { 
  X, LayoutDashboard, Network, ArrowLeftRight, Compass,
  Globe, Lock, Download, ShieldAlert, FileSearch, ScrollText, Ban,
  BarChart3, Activity, Bot, RefreshCw, HardDriveDownload, Settings, LogOut
} from 'lucide-vue-next'
import { useAppStore } from '@/composables/useAppStore'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useLogout, useSession } from '@/composables/useSession'

const { isSidebarOpen, closeSidebar } = useAppStore()
const { gatewayConfig, gatewayStatus, gatewayVersion, gatewayState } = useGatewayStatus()
const { isLoggingOut } = useSession()
const logout = useLogout()
const adminAddress = window.location.host
const firewallAttached = computed(() => gatewayConfig.value?.routes?.some(route => route.enabled && route.firewall_id))
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
  padding: 0 18px;
  display: flex;
  align-items: center;
  gap: 12px;
  border-bottom: 1px solid var(--line);
  flex-shrink: 0;
}

.brand-icon {
  width: 32px;
  height: 32px;
  border-radius: 9px;
  background: rgba(99, 102, 241, 0.12);
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid rgba(99, 102, 241, 0.25);
}

.brand-info {
  display: flex;
  flex-direction: column;
  flex: 1;
}

.brand-title {
  font-size: 14.5px;
  font-weight: 700;
  color: var(--ink);
  letter-spacing: -0.2px;
}

.brand-version {
  font-size: 10px;
  color: var(--faint);
  font-family: var(--mono);
}

.mobile-close-btn {
  display: none;
  color: var(--muted);
  padding: 4px;
}

.sidebar-nav {
  flex: 1;
  overflow-y: auto;
  padding: 10px 10px 16px 10px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.nav-section-title {
  font-size: 10.5px;
  font-weight: 700;
  text-transform: uppercase;
  color: var(--faint);
  letter-spacing: 0.8px;
  padding: 12px 10px 4px 10px;
}

.nav-link {
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 7px 12px;
  border-radius: var(--radius-sm);
  color: var(--muted);
  font-size: 13px;
  font-weight: 500;
  transition: var(--transition-smooth);
  position: relative;
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
  color: var(--faint);
  transition: color 0.2s ease;
  flex-shrink: 0;
}
.nav-link:hover .nav-icon,
.nav-link.active .nav-icon {
  color: var(--brand-light);
}

.nav-text {
  flex: 1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.nav-badge {
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 999px;
  background: var(--strip-bg);
  border: 1px solid var(--line);
  color: var(--muted);
  font-family: var(--mono);
}

.nav-badge.brand {
  background: rgba(99, 102, 241, 0.2);
  color: var(--brand-light);
  border-color: rgba(99, 102, 241, 0.35);
}

.nav-badge.warn {
  background: rgba(245, 158, 11, 0.2);
  color: var(--warn);
  border-color: rgba(245, 158, 11, 0.35);
}

.nav-badge.bad {
  background: rgba(244, 63, 94, 0.2);
  color: var(--bad);
  border-color: rgba(244, 63, 94, 0.35);
}

.nav-badge.ai {
  background: linear-gradient(135deg, rgba(99, 102, 241, 0.3), rgba(6, 182, 212, 0.3));
  color: #38bdf8;
  border: 1px solid rgba(6, 182, 212, 0.4);
  font-weight: 700;
}

.nav-status-dot.active {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--ok);
  box-shadow: var(--ok-glow);
}

.sidebar-footer {
  padding: 12px 14px;
  border-top: 1px solid var(--line);
  flex-shrink: 0;
}

.status-card {
  background: var(--strip-bg);
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  padding: 10px 12px;
  font-size: 11.5px;
}

.status-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 5px;
}

.status-indicator {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
  color: var(--ok);
}

.uptime-text {
  color: var(--faint);
  font-family: var(--mono);
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
}

.login-link-btn {
  flex-shrink: 0 !important;
  white-space: nowrap !important;
  word-break: keep-all !important;
  background: transparent;
  border: none;
  padding: 2px 5px;
  border-radius: 4px;
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
  opacity: 0.8;
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
    display: block;
  }
}
</style>
