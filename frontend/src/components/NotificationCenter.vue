<template>
  <teleport to="body">
    <transition name="drawer-mask">
      <div 
        v-if="isCenterOpen" 
        class="notification-backdrop" 
        @click="closeCenter"
        aria-hidden="true"
      />
    </transition>

    <transition name="drawer-slide">
      <aside 
        v-if="isCenterOpen" 
        class="notification-drawer" 
        role="dialog" 
        aria-label="消息通知中心"
        aria-modal="true"
        @keydown.esc="closeCenter"
      >
        <!-- Mobile Drag Handle -->
        <div class="mobile-drag-bar" @click="closeCenter">
          <span class="drag-handle-pill" />
        </div>

        <!-- Drawer Header -->
        <header class="drawer-header">
          <div class="header-title-wrap">
            <h2 class="drawer-title">通知中心</h2>
            <span v-if="unreadCount > 0" class="badge-unread">
              {{ unreadCount }} 条未读
            </span>
          </div>

          <div class="header-actions">
            <button 
              v-if="unreadCount > 0" 
              class="btn-text-action" 
              @click="markAllAsRead"
              title="将全部通知标记为已读"
            >
              全部已读
            </button>
            <button 
              v-if="notifications.length > 0" 
              class="btn-text-action action-danger" 
              @click="clearAllNotifications"
              title="清空所有通知历史"
            >
              清空
            </button>
            <button 
              class="btn-close-drawer" 
              @click="closeCenter"
              title="关闭通知中心 (Esc)"
              aria-label="关闭通知中心"
            >
              <X :size="16" />
            </button>
          </div>
        </header>

        <!-- Category Filter Tabs -->
        <div class="category-tabs" role="tablist">
          <button 
            v-for="tab in filterTabs" 
            :key="tab.key"
            class="tab-btn"
            :class="{ active: currentCategory === tab.key }"
            role="tab"
            :aria-selected="currentCategory === tab.key"
            @click="currentCategory = tab.key"
          >
            {{ tab.label }}
            <span v-if="tab.count > 0" class="tab-count">{{ tab.count }}</span>
          </button>
        </div>

        <!-- Notification List -->
        <div class="drawer-body">
          <div v-if="filteredList.length === 0" class="empty-state">
            <Inbox :size="38" class="empty-icon" />
            <p class="empty-title">暂无通知记录</p>
            <p class="empty-desc">当前分类下没有通知消息，系统运行稳定</p>
          </div>

          <div v-else class="notifications-list">
            <div 
              v-for="item in filteredList" 
              :key="item.id"
              class="notification-card"
              :class="[`type-${item.type}`, { unread: item.unread, clickable: !!item.path }]"
              @click="handleCardClick(item)"
            >
              <!-- Unread status left edge accent strip -->
              <div v-if="item.unread" class="unread-indicator" />

              <div class="card-top-row">
                <div class="card-category">
                  <span class="category-pill" :class="`pill-${item.category || 'system'}`">
                    <span class="pill-dot" />
                    {{ categoryName(item.category) }}
                  </span>
                  <span class="time-ago">{{ formatTime(item.createdAt) }}</span>
                </div>

                <button 
                  class="card-delete-btn" 
                  title="删除此通知"
                  aria-label="删除此通知"
                  @click.stop="removeNotification(item.id)"
                >
                  <X :size="12" />
                </button>
              </div>

              <div class="card-message">{{ item.message }}</div>

              <div v-if="item.path" class="card-footer-hint">
                <span>点击前往相关配置</span>
                <span class="arrow-hint">&rarr;</span>
              </div>
            </div>
          </div>
        </div>
      </aside>
    </transition>
  </teleport>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { X, Inbox } from 'lucide-vue-next'
import { useToast } from '@/composables/useToast'

const router = useRouter()
const {
  notifications,
  isCenterOpen,
  unreadCount,
  closeCenter,
  markAllAsRead,
  markAsRead,
  removeNotification,
  clearAllNotifications
} = useToast()

const currentCategory = ref('all')

const filterTabs = computed(() => {
  const counts = { all: notifications.value.length, waf: 0, ddns: 0, backup: 0, other: 0 }
  notifications.value.forEach(item => {
    if (item.category === 'waf') counts.waf++
    else if (item.category === 'ddns') counts.ddns++
    else if (item.category === 'backup') counts.backup++
    else counts.other++
  })

  return [
    { key: 'all', label: '全部', count: counts.all },
    { key: 'waf', label: '安全拦截', count: counts.waf },
    { key: 'ddns', label: '动态域名', count: counts.ddns },
    { key: 'backup', label: '备份/系统', count: counts.backup + counts.other }
  ]
})

const filteredList = computed(() => {
  if (currentCategory.value === 'all') return notifications.value
  if (currentCategory.value === 'backup') {
    return notifications.value.filter(n => n.category === 'backup' || n.category === 'cert' || n.category === 'system' || n.category === 'mcp')
  }
  return notifications.value.filter(n => n.category === currentCategory.value)
})

function categoryName(cat) {
  switch (cat) {
    case 'waf': return '安全防护 · WAF'
    case 'ddns': return '动态域名 · DDNS'
    case 'backup': return '系统备份 · WebDAV'
    case 'cert': return '证书任务 · ACME'
    case 'mcp': return 'AI 管理 · MCP'
    default: return '系统通知'
  }
}

function formatTime(isoString) {
  if (!isoString) return '刚刚'
  const date = new Date(isoString)
  const now = new Date()
  const diffSec = Math.floor((now - date) / 1000)

  if (diffSec < 60) return '刚刚'
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)} 分钟前`
  if (diffSec < 86400 && date.getDate() === now.getDate()) {
    const hh = String(date.getHours()).padStart(2, '0')
    const mm = String(date.getMinutes()).padStart(2, '0')
    return `今天 ${hh}:${mm}`
  }
  const m = date.getMonth() + 1
  const d = date.getDate()
  const hh = String(date.getHours()).padStart(2, '0')
  const mm = String(date.getMinutes()).padStart(2, '0')
  return `${m}月${d}日 ${hh}:${mm}`
}

function handleCardClick(item) {
  markAsRead(item.id)
  if (item.path) {
    router.push(item.path)
    closeCenter()
  }
}

// Global keyboard Esc handler
function onKeydown(e) {
  if (e.key === 'Escape' && isCenterOpen.value) {
    closeCenter()
  }
}

onMounted(() => {
  window.addEventListener('keydown', onKeydown)
})

onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown)
})
</script>

<style scoped>
/* Backdrop */
.notification-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
  backdrop-filter: blur(4px);
  -webkit-backdrop-filter: blur(4px);
  z-index: 9990;
}

/* Drawer Shell (Desktop Default) */
.notification-drawer {
  position: fixed;
  top: 0;
  right: 0;
  bottom: 0;
  width: 360px;
  background: var(--card-bg);
  backdrop-filter: blur(36px) saturate(190%);
  -webkit-backdrop-filter: blur(36px) saturate(190%);
  border-left: 1px solid var(--card-border);
  box-shadow: -15px 0 45px rgba(0, 0, 0, 0.5), var(--inner-highlight);
  z-index: 9995;
  display: flex;
  flex-direction: column;
  color: var(--ink);
}

.mobile-drag-bar {
  display: none;
}

/* Header */
.drawer-header {
  padding: 16px 18px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.header-title-wrap {
  display: flex;
  align-items: center;
  gap: 8px;
}

.drawer-title {
  font-size: 16px;
  font-weight: 700;
  margin: 0;
  color: var(--ink);
  letter-spacing: -0.2px;
}

.badge-unread {
  font-size: 11px;
  font-weight: 700;
  padding: 2px 7px;
  border-radius: 10px;
  background: rgba(99, 102, 241, 0.18);
  color: #818cf8;
  border: 1px solid rgba(99, 102, 241, 0.3);
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.btn-text-action {
  background: transparent;
  border: none;
  font-size: 12px;
  font-weight: 600;
  color: var(--muted);
  cursor: pointer;
  padding: 4px 6px;
  border-radius: var(--radius-sm);
  transition: all 0.15s ease;
}
.btn-text-action:hover {
  color: var(--ink);
  background: var(--strip-bg);
}
.btn-text-action.action-danger {
  color: var(--bad);
}
.btn-text-action.action-danger:hover {
  background: rgba(239, 68, 68, 0.12);
}

.btn-close-drawer {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background: var(--strip-bg);
  border: 1px solid var(--line);
  color: var(--muted);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.15s ease;
}
.btn-close-drawer:hover {
  color: var(--ink);
  border-color: var(--card-border-hover);
  transform: scale(1.08);
}

/* Category Tabs */
.category-tabs {
  padding: 10px 16px;
  display: flex;
  gap: 6px;
  border-bottom: 1px solid var(--line);
  overflow-x: auto;
}

.tab-btn {
  font-size: 11px;
  font-weight: 600;
  padding: 4px 10px;
  border-radius: 14px;
  background: transparent;
  border: 1px solid transparent;
  color: var(--muted);
  cursor: pointer;
  white-space: nowrap;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  transition: all 0.15s ease;
}
.tab-btn:hover {
  color: var(--ink);
  background: var(--strip-bg);
}
.tab-btn.active {
  background: var(--strip-bg);
  color: var(--ink);
  border-color: var(--line);
}

.tab-count {
  font-size: 9.5px;
  opacity: 0.75;
}

/* Drawer Body & Cards */
.drawer-body {
  flex: 1;
  overflow-y: auto;
  padding: 14px 16px;
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 240px;
  text-align: center;
  color: var(--muted);
}
.empty-icon {
  margin-bottom: 12px;
  opacity: 0.4;
}
.empty-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--ink);
  margin: 0 0 4px 0;
}
.empty-desc {
  font-size: 12px;
  margin: 0;
  opacity: 0.8;
}

.notifications-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.notification-card {
  position: relative;
  background: var(--strip-bg);
  border: 1px solid var(--line);
  border-radius: 14px;
  padding: 11px 13px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  transition: all 0.2s ease;
}
.notification-card:hover {
  border-color: var(--card-border-hover);
  background: var(--card-hover-bg, var(--strip-bg));
  transform: translateY(-1px);
}
.notification-card.clickable {
  cursor: pointer;
}

.unread-indicator {
  position: absolute;
  left: 0;
  top: 10px;
  bottom: 10px;
  width: 3.5px;
  border-radius: 0 4px 4px 0;
  background: var(--brand, #6366f1);
  box-shadow: 0 0 8px rgba(99, 102, 241, 0.45);
}

.notification-card.unread {
  background: rgba(99, 102, 241, 0.035);
  border-color: rgba(99, 102, 241, 0.22);
}

.card-top-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.card-category {
  display: flex;
  align-items: center;
  gap: 7px;
}

.category-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 1.5px 7.5px;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 600;
  line-height: 1.4;
  border: 1px solid transparent;
  transition: all 0.15s ease;
}

.pill-dot {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  flex-shrink: 0;
}

.pill-system {
  background: rgba(99, 102, 241, 0.08);
  border-color: rgba(99, 102, 241, 0.2);
  color: var(--brand, #6366f1);
}
.pill-system .pill-dot {
  background: var(--brand, #6366f1);
  box-shadow: 0 0 5px rgba(99, 102, 241, 0.5);
}

.pill-waf {
  background: rgba(239, 68, 68, 0.08);
  border-color: rgba(239, 68, 68, 0.2);
  color: var(--bad, #ef4444);
}
.pill-waf .pill-dot {
  background: var(--bad, #ef4444);
  box-shadow: 0 0 5px rgba(239, 68, 68, 0.5);
}

.pill-ddns {
  background: rgba(16, 185, 129, 0.08);
  border-color: rgba(16, 185, 129, 0.2);
  color: var(--ok, #10b981);
}
.pill-ddns .pill-dot {
  background: var(--ok, #10b981);
  box-shadow: 0 0 5px rgba(16, 185, 129, 0.5);
}

.pill-backup {
  background: rgba(6, 182, 212, 0.08);
  border-color: rgba(6, 182, 212, 0.2);
  color: var(--brand-cyan, #06b6d4);
}
.pill-backup .pill-dot {
  background: var(--brand-cyan, #06b6d4);
  box-shadow: 0 0 5px rgba(6, 182, 212, 0.5);
}

.pill-cert {
  background: rgba(6, 182, 212, 0.08);
  border-color: rgba(6, 182, 212, 0.2);
  color: var(--brand-cyan, #06b6d4);
}
.pill-cert .pill-dot {
  background: var(--brand-cyan, #06b6d4);
  box-shadow: 0 0 5px rgba(6, 182, 212, 0.5);
}

.pill-mcp {
  background: rgba(139, 92, 246, 0.08);
  border-color: rgba(139, 92, 246, 0.2);
  color: #8b5cf6;
}
.pill-mcp .pill-dot {
  background: #8b5cf6;
  box-shadow: 0 0 5px rgba(139, 92, 246, 0.5);
}

.time-ago {
  font-size: 11px;
  color: var(--muted);
  opacity: 0.85;
}

.card-delete-btn {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: transparent;
  border: none;
  color: var(--muted);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  opacity: 0.6;
  transition: all 0.15s ease;
}
.notification-card:hover .card-delete-btn {
  opacity: 1;
}
.card-delete-btn:hover {
  background: rgba(239, 68, 68, 0.15);
  color: var(--bad);
}

.card-message {
  font-size: 12.5px;
  color: var(--ink);
  line-height: 1.45;
  overflow-wrap: anywhere;
}

.card-footer-hint {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 10.5px;
  color: var(--brand, #818cf8);
  padding: 4px 0 0 6px;
  border-top: 1px solid var(--line);
  opacity: 0.85;
}
.arrow-hint {
  font-weight: 700;
}

/* ---------------- MOBILE RESPONSIVE (BOTTOM SHEET) ---------------- */
@media (max-width: 640px) {
  .notification-drawer {
    top: auto;
    left: 0;
    right: 0;
    bottom: 0;
    width: 100%;
    height: 78%;
    border-left: none;
    border-top: 1px solid var(--card-border);
    border-radius: 24px 24px 0 0;
    padding-bottom: env(safe-area-inset-bottom, 16px);
  }

  .mobile-drag-bar {
    display: flex;
    justify-content: center;
    padding: 8px 0 4px;
    cursor: pointer;
  }
  .drag-handle-pill {
    width: 36px;
    height: 4px;
    border-radius: 2px;
    background: var(--muted);
    opacity: 0.4;
  }
}

/* Transitions */
.drawer-mask-enter-active,
.drawer-mask-leave-active {
  transition: opacity 0.25s ease;
}
.drawer-mask-enter-from,
.drawer-mask-leave-to {
  opacity: 0;
}

.drawer-slide-enter-active,
.drawer-slide-leave-active {
  transition: transform 0.28s cubic-bezier(0.16, 1, 0.3, 1);
}

@media (min-width: 641px) {
  .drawer-slide-enter-from,
  .drawer-slide-leave-to {
    transform: translateX(100%);
  }
}

@media (max-width: 640px) {
  .drawer-slide-enter-from,
  .drawer-slide-leave-to {
    transform: translateY(100%);
  }
}
</style>
