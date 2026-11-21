<template>
  <div class="notification-popover" role="dialog" aria-label="快速通知中心">
    <div class="popover-head">
      <div class="popover-title-wrap">
        <h3>通知中心</h3>
        <span v-if="unreadCount" class="badge badge-brand mono">{{ unreadCount }}</span>
      </div>
      <div class="popover-head-actions">
        <button type="button" class="btn btn-ghost btn-sm" :disabled="!unreadCount" @click="markAllAsRead">
          全部已读
        </button>
        <router-link to="/notifications?tab=push" class="btn btn-ghost btn-sm" @click="emit('close')">
          故障推送
        </router-link>
      </div>
    </div>

    <div v-if="recentNotifications.length" class="popover-list">
      <article
        v-for="item in recentNotifications"
        :key="item.id"
        class="mini-notice"
        :class="[{ unread: item.unread }, 'tone-' + severity(item).tone]"
      >
        <div class="mini-head">
          <span class="badge" :class="'badge-' + severity(item).tone">{{ severity(item).label }}</span>
          <span class="mini-cat">{{ categories[item.category] || categories.system }}</span>
          <time class="mini-time mono" :datetime="item.createdAt">{{ relativeTime(item.createdAt) }}</time>
        </div>
        <div class="mini-msg">{{ item.message }}</div>
        <div v-if="item.resource" class="mini-resource mono">{{ item.resource }}</div>
        <div class="mini-actions">
          <button
            v-if="item.unread"
            type="button"
            class="mini-link-btn"
            @click="markAsRead(item.id)"
          >
            标为已读
          </button>
          <router-link
            v-if="item.path"
            :to="item.path"
            class="mini-link-btn"
            @click="openDetail(item)"
          >
            查看详情 <ArrowUpRight :size="12" aria-hidden="true" />
          </router-link>
          <button
            type="button"
            class="mini-del-btn"
            aria-label="删除此通知"
            @click="removeNotification(item.id)"
          >
            <X :size="13" aria-hidden="true" />
          </button>
        </div>
      </article>
    </div>

    <div v-else class="popover-empty">
      <Inbox :size="26" class="empty-icon" aria-hidden="true" />
      <p>暂无通知记录</p>
      <router-link to="/notifications?tab=push" class="btn btn-secondary btn-sm" @click="emit('close')">
        配置故障推送
      </router-link>
    </div>

    <div class="popover-foot">
      <router-link to="/notifications" class="popover-all-link" @click="emit('close')">
        <span>查看全部通知</span>
        <ChevronRight :size="14" aria-hidden="true" />
      </router-link>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { ArrowUpRight, ChevronRight, Inbox, X } from 'lucide-vue-next'
import { useToast } from '@/composables/useToast'

const emit = defineEmits(['close'])
const { notifications, unreadCount, markAllAsRead, markAsRead, removeNotification } = useToast()

const categories = {
  upstream: '反向代理',
  backup: '备份与恢复',
  cert: 'SSL 证书',
  waf: '安全防护',
  ddns: '动态域名',
  mcp: 'AI 管理',
  system: '系统通知'
}

const tones = {
  error: { label: '告警', tone: 'bad' },
  warning: { label: '预警', tone: 'warn' },
  success: { label: '恢复', tone: 'ok' },
  info: { label: '系统', tone: 'brand' }
}

const recentNotifications = computed(() => notifications.value.slice(0, 20))

function severity(item) {
  return tones[item.type === 'warn' ? 'warning' : item.type] || tones.info
}

function relativeTime(iso) {
  const ts = Date.parse(iso)
  if (!Number.isFinite(ts)) return '—'
  const diffSec = Math.max(0, Math.floor((Date.now() - ts) / 1000))
  if (diffSec < 60) return '刚刚'
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)} 分钟前`
  if (diffSec < 86400) return `${Math.floor(diffSec / 3600)} 小时前`
  const days = Math.floor(diffSec / 86400)
  return days === 1 ? '昨天' : `${days} 天前`
}

function openDetail(item) {
  markAsRead(item.id)
  emit('close')
}
</script>

<style scoped>
.notification-popover {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  width: 390px;
  max-width: calc(100vw - 24px);
  background: var(--popover-bg, #111827);
  border: 1px solid var(--popover-border, var(--line));
  border-radius: 14px;
  box-shadow: 0 20px 48px rgba(0, 0, 0, 0.55);
  z-index: 1000;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

body.theme-light .notification-popover {
  background: #ffffff;
  border-color: #e2e8f0;
  box-shadow: 0 12px 32px rgba(15, 23, 42, 0.16);
}

.popover-head {
  padding: 12px 14px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.popover-title-wrap {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.popover-title-wrap h3 {
  font-size: 14px;
  font-weight: 700;
  color: var(--ink);
  margin: 0;
  white-space: nowrap;
}
.popover-head-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.popover-list {
  max-height: min(360px, 60dvh);
  overflow-y: auto;
  padding: 10px;
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 8px;
}

.mini-notice {
  background: var(--strip-bg);
  border: 1px solid var(--line);
  border-left-width: 3px;
  border-radius: var(--radius-sm);
  padding: 10px 11px;
  display: grid;
  gap: 5px;
  min-width: 0;
}
.mini-notice.unread {
  background: rgba(99, 102, 241, 0.08);
  border-color: rgba(99, 102, 241, 0.3);
}
.theme-light .mini-notice.unread {
  background: rgba(99, 102, 241, 0.05);
}
.mini-notice.tone-bad { border-left-color: var(--bad); }
.mini-notice.tone-warn { border-left-color: var(--warn); }
.mini-notice.tone-ok { border-left-color: var(--ok); }
.mini-notice.tone-brand { border-left-color: var(--info); }

.mini-head {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}
.mini-cat {
  font-size: 11px;
  color: var(--muted);
}
.mini-time {
  margin-left: auto;
  font-size: 11px;
  color: var(--faint);
  white-space: nowrap;
}
.mono {
  font-family: var(--mono);
  font-variant-numeric: tabular-nums;
}
.mini-msg {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--ink);
  line-height: 1.4;
  overflow-wrap: anywhere;
}
.mini-resource {
  font-size: 11px;
  color: var(--brand-light);
  overflow-wrap: anywhere;
}
.theme-light .mini-resource {
  color: var(--brand);
}
.mini-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 2px;
}
.mini-link-btn {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 11.5px;
  font-weight: 500;
  color: var(--brand-light);
  padding: 2px 0;
  background: transparent;
  border: 0;
  cursor: pointer;
}
.theme-light .mini-link-btn {
  color: var(--brand);
}
.mini-link-btn:hover {
  text-decoration: underline;
}
.mini-del-btn {
  margin-left: auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border-radius: 6px;
  color: var(--faint);
  background: transparent;
  border: 0;
  cursor: pointer;
}
.mini-del-btn:hover {
  color: var(--ink);
  background: rgba(148, 163, 184, 0.14);
}

.popover-empty {
  padding: 28px 16px;
  display: grid;
  justify-items: center;
  gap: 10px;
  text-align: center;
  color: var(--muted);
  font-size: 12.5px;
}
.empty-icon {
  color: var(--faint);
  opacity: 0.75;
}

.popover-foot {
  border-top: 1px solid var(--line);
  background: var(--strip-bg);
}
.popover-all-link {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  padding: 10px 14px;
  font-size: 12.5px;
  font-weight: 600;
  color: var(--brand-light);
}
.theme-light .popover-all-link {
  color: var(--brand);
}
.popover-all-link:hover {
  background: rgba(99, 102, 241, 0.08);
}

@media (max-width: 600px) {
  .notification-popover {
    position: fixed;
    top: calc(var(--topbar-h) + 8px);
    left: 12px;
    right: 12px;
    width: auto;
    max-width: none;
  }
}
</style>
