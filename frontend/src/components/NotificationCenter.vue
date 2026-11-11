<template>
  <Teleport to="body">
    <dialog
      id="notification-center"
      ref="drawer"
      class="notification-drawer"
      aria-labelledby="notification-center-title"
      @cancel.prevent="closeCenter"
      @click.self="closeCenter"
    >
      <div class="drawer-panel">
        <div class="drawer-handle" aria-hidden="true" @pointerdown="startDrag" @pointerup="finishDrag">
          <span />
        </div>

        <div class="drawer-header">
          <div class="drawer-heading">
            <h2 id="notification-center-title">通知中心</h2>
            <span v-if="unreadCount" class="unread-count">{{ unreadCount }} 条未读</span>
          </div>
          <div class="header-actions">
            <button type="button" class="header-action" :disabled="!unreadCount" @click="markAllAsRead">
              全部已读
            </button>
            <button type="button" class="header-action" :disabled="!notifications.length" @click="clearAllNotifications">
              清空
            </button>
            <button type="button" class="drawer-close" aria-label="关闭通知中心" autofocus @click="closeCenter">
              <X :size="16" aria-hidden="true" />
            </button>
          </div>
        </div>

        <div class="category-tabs" role="group" aria-label="通知分类">
          <button
            v-for="tab in filterTabs"
            :key="tab.key"
            type="button"
            class="tab-btn"
            :aria-pressed="currentCategory === tab.key"
            @click="currentCategory = tab.key"
          >
            {{ tab.label }}
            <span v-if="tab.count" class="tab-count">{{ tab.count }}</span>
          </button>
        </div>

        <div class="drawer-body">
          <div v-if="!filteredList.length" class="empty-state">
            <Inbox :size="30" aria-hidden="true" />
            <p>暂无通知记录</p>
          </div>

          <ul v-else class="notifications-list">
            <li
              v-for="item in filteredList"
              :key="item.id"
              class="notification-card"
              :class="[`type-${item.severity}`, { unread: item.unread }]"
            >
              <button
                type="button"
                class="card-content"
                :aria-disabled="!item.unread && !item.path ? true : undefined"
                @click="handleCardClick(item)"
              >
                <span class="card-meta">
                  <span class="category-pill">
                    <component :is="item.categoryDetails.icon" :size="12" aria-hidden="true" />
                    {{ item.categoryDetails.label }}
                  </span>
                  <span class="type-label" :class="{ 'screen-reader-only': item.severity === 'info' }">
                    {{ typeLabels[item.severity] }}
                  </span>
                  <time :datetime="item.createdAt" class="time-ago">{{ formatTime(item.createdAt) }}</time>
                </span>
                <span class="card-message">{{ item.message }}</span>
                <span class="screen-reader-only">{{ item.unread ? '未读' : '已读' }}</span>
                <span v-if="item.path" class="card-link">查看详情 <ArrowUpRight :size="12" aria-hidden="true" /></span>
              </button>

              <button
                type="button"
                class="card-delete-btn"
                aria-label="删除此通知"
                title="删除此通知"
                @click="removeNotification(item.id)"
              >
                <X :size="14" aria-hidden="true" />
              </button>
            </li>
          </ul>
        </div>
      </div>
    </dialog>
  </Teleport>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowUpRight, Bell, Bot, Globe, HardDrive, Inbox, Lock, Shield, X } from 'lucide-vue-next'
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

const categories = {
  system: { label: '系统通知', icon: Bell, tab: 'system' },
  waf: { label: '安全防护', icon: Shield, tab: 'waf' },
  ddns: { label: '动态域名', icon: Globe, tab: 'ddns' },
  backup: { label: '系统备份', icon: HardDrive, tab: 'system' },
  cert: { label: '证书任务', icon: Lock, tab: 'system' },
  mcp: { label: 'AI 管理', icon: Bot, tab: 'system' }
}
const typeLabels = { info: '信息', success: '成功', warning: '警告', error: '错误' }
const drawer = ref(null)
const currentCategory = ref('all')
let dragStart = 0

watch(isCenterOpen, open => {
  if (open) drawer.value.showModal()
  else drawer.value.close()
}, { flush: 'post' })

const entries = computed(() => notifications.value.map(item => {
  const type = item.type === 'warn' ? 'warning' : item.type
  return {
    ...item,
    severity: Object.hasOwn(typeLabels, type) ? type : 'info',
    categoryDetails: Object.hasOwn(categories, item.category) ? categories[item.category] : categories.system
  }
}))

const filterTabs = computed(() => {
  const counts = { waf: 0, ddns: 0, system: 0 }
  for (const item of entries.value) counts[item.categoryDetails.tab]++
  return [
    { key: 'all', label: '全部', count: entries.value.length },
    { key: 'waf', label: '安全拦截', count: counts.waf },
    { key: 'ddns', label: '动态域名', count: counts.ddns },
    { key: 'system', label: '备份与系统', count: counts.system }
  ]
})

const filteredList = computed(() => currentCategory.value === 'all'
  ? entries.value
  : entries.value.filter(item => item.categoryDetails.tab === currentCategory.value))

function formatTime(isoString) {
  if (!isoString) return '刚刚'
  const date = new Date(isoString)
  const now = new Date()
  const diffSec = Math.floor((now - date) / 1000)
  if (diffSec < 60) return '刚刚'
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)} 分钟前`
  const hh = String(date.getHours()).padStart(2, '0')
  const mm = String(date.getMinutes()).padStart(2, '0')
  if (diffSec < 86400 && date.getDate() === now.getDate()) return `今天 ${hh}:${mm}`
  return `${date.getMonth() + 1}月${date.getDate()}日 ${hh}:${mm}`
}

function handleCardClick(item) {
  if (item.unread) markAsRead(item.id)
  if (item.path) {
    router.push(item.path)
    closeCenter()
  }
}

function startDrag(event) {
  if (!event.isPrimary || event.button !== 0) return
  event.currentTarget.setPointerCapture(event.pointerId)
  dragStart = event.clientY
}

function finishDrag(event) {
  if (!event.isPrimary || !event.currentTarget.hasPointerCapture(event.pointerId)) return
  const distance = event.clientY - dragStart
  if (distance > 48 || Math.abs(distance) < 4) closeCenter()
}
</script>

<style scoped>
:global(body:has(.notification-drawer[open])) {
  overflow: hidden;
}

dialog.notification-drawer {
  --notice-info: #4338ca;
  --notice-success: #047857;
  --notice-warning: #92400e;
  --notice-error: #be123c;
  --drawer-start: translateX(100%);
  --drawer-inset-left: 16px;
  --drawer-inset-right: 16px;
  inset: 0 0 0 auto;
  width: min(390px, 100%) !important;
  max-width: none;
  height: 100dvh;
  max-height: none !important;
  margin: 0 !important;
  border: 0 !important;
  border-inline-start: 1px solid var(--line) !important;
  border-radius: 0 !important;
  background: var(--popover-bg) !important;
  backdrop-filter: none !important;
  -webkit-backdrop-filter: none !important;
  box-shadow: -10px 0 36px rgb(0 0 0 / 18%) !important;
}

:global(body.theme-dark #notification-center) {
  --notice-info: #a5b4fc;
  --notice-success: #6ee7b7;
  --notice-warning: #fcd34d;
  --notice-error: #fda4af;
}

.notification-drawer::backdrop {
  background: rgb(4 7 13 / 44%) !important;
  backdrop-filter: none !important;
  -webkit-backdrop-filter: none !important;
}

.notification-drawer[open] {
  animation: drawer-enter 220ms cubic-bezier(0.16, 1, 0.3, 1);
}

@keyframes drawer-enter {
  from { transform: var(--drawer-start); }
  to { transform: translate(0); }
}

.drawer-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
  flex: 1;
}

.drawer-handle {
  display: none;
}

.drawer-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  padding: 10px var(--drawer-inset-right) 10px var(--drawer-inset-left);
  border-bottom: 1px solid var(--line);
}

.drawer-heading {
  min-width: 0;
}

.drawer-heading h2 {
  font-size: 17px;
  line-height: 1.3;
  font-weight: 700;
  white-space: nowrap;
}

.unread-count {
  display: block;
  margin-top: 2px;
  color: var(--notice-info);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.header-action, .drawer-close, .card-delete-btn {
  border-radius: var(--radius-sm);
  color: var(--muted);
}

.header-action {
  min-height: 32px;
  padding: 0 8px;
  font-size: 12px;
}

.header-action:disabled {
  opacity: 0.6;
  cursor: default;
}

.header-action:enabled:hover, .drawer-close:hover, .card-delete-btn:hover {
  background: var(--spotlight-color);
  color: var(--ink);
}

.drawer-close, .card-delete-btn {
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
}

.category-tabs {
  display: flex;
  gap: 4px;
  padding: 6px var(--drawer-inset-right) 6px var(--drawer-inset-left);
  border-bottom: 1px solid var(--line);
  overflow-x: auto;
  flex-shrink: 0;
}

.tab-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  min-height: 30px;
  padding: 0 7px;
  border: 1px solid transparent;
  border-radius: var(--radius-sm);
  color: var(--muted);
  font-size: 12px;
  white-space: nowrap;
}

.tab-btn:hover, .tab-btn[aria-pressed="true"] {
  color: var(--ink);
  background: var(--strip-bg);
}

.tab-btn[aria-pressed="true"] {
  border-color: var(--line);
  font-weight: 600;
}

.tab-count {
  font-size: 11px;
  color: var(--muted);
  font-variant-numeric: tabular-nums;
}

.drawer-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: 10px var(--drawer-inset-right) 12px var(--drawer-inset-left);
}

.empty-state {
  display: grid;
  place-items: center;
  gap: 8px;
  padding: 48px 8px;
  color: var(--muted);
}

.notifications-list {
  display: grid;
  gap: 8px;
  list-style: none;
}

.notification-card {
  --type-ink: var(--notice-info);
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: start;
  gap: 4px;
  padding: 10px 8px 10px 12px;
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
  background: var(--strip-bg);
}

.type-success { --type-ink: var(--notice-success); }
.type-warning { --type-ink: var(--notice-warning); }
.type-error { --type-ink: var(--notice-error); }

.notification-card:is(.type-warning, .type-error, .type-success) {
  border-color: color-mix(in srgb, var(--type-ink) 20%, var(--line));
}

.notification-card.unread {
  background: color-mix(in srgb, var(--notice-info) 5%, var(--strip-bg));
  box-shadow: inset 2px 0 color-mix(in srgb, var(--notice-info) 55%, transparent);
}

.card-content {
  display: grid;
  justify-items: start;
  gap: 4px;
  min-width: 0;
  border-radius: var(--radius-sm);
  text-align: start;
  color: var(--ink);
}

.card-content[aria-disabled="true"] {
  cursor: default;
}

.card-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 3px 7px;
  min-height: 22px;
  max-width: 100%;
}

.category-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 6px;
  border-radius: var(--radius-pill);
  background: color-mix(in srgb, var(--type-ink) 9%, transparent);
  color: var(--type-ink);
  font-size: 11px;
  font-weight: 600;
  line-height: 1.45;
  white-space: nowrap;
}

.type-label, .time-ago {
  font-size: 11px;
  white-space: nowrap;
}

.type-label {
  color: var(--type-ink);
  font-weight: 600;
}

.time-ago {
  color: var(--muted);
  font-variant-numeric: tabular-nums;
}

.card-message {
  max-width: 100%;
  font-size: 13px;
  line-height: 1.45;
  overflow-wrap: anywhere;
}

.unread .card-message {
  font-weight: 600;
}

.card-link {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  color: var(--notice-info);
  font-size: 11px;
}

.card-content:hover .card-link {
  text-decoration: underline;
}

.screen-reader-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}

@media (max-width: 640px) {
  dialog.notification-drawer {
    --drawer-start: translateY(100%);
    --drawer-inset-left: max(12px, env(safe-area-inset-left));
    --drawer-inset-right: max(12px, env(safe-area-inset-right));
    inset: auto 0 0;
    width: 100% !important;
    height: 80dvh;
    max-height: calc(100dvh - env(safe-area-inset-top) - 8px) !important;
    border-inline-start: 0 !important;
    border-top: 1px solid var(--line) !important;
    border-radius: 18px 18px 0 0 !important;
  }

  .drawer-panel {
    padding-bottom: max(4px, env(safe-area-inset-bottom));
  }

  .drawer-handle {
    display: grid;
    place-items: center;
    min-height: 22px;
    flex-shrink: 0;
    touch-action: none;
    cursor: pointer;
  }

  .drawer-handle span {
    width: 32px;
    height: 4px;
    border-radius: var(--radius-pill);
    background: var(--faint);
  }

  .drawer-header {
    padding-block: 2px 8px;
  }

  .header-action, .drawer-close, .card-delete-btn {
    min-height: 44px;
  }

  .drawer-close, .card-delete-btn {
    width: 44px;
  }

  .tab-btn {
    min-height: 36px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .notification-drawer[open] {
    animation: none;
  }
}
</style>
