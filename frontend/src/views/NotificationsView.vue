<template>
  <div class="report-page notice-page animate-fade-in">
    <header class="page-head">
      <div class="page-head-row">
        <div class="page-head-title">
          <h1>通知中心</h1>
          <p class="page-sub">服务端保留最近 60 条故障与恢复记录；阅读状态保存在当前浏览器</p>
        </div>
        <router-link to="/runtime" class="btn btn-secondary btn-sm">运行状态</router-link>
      </div>
    </header>

    <nav class="notice-tabs" aria-label="通知中心页面">
      <router-link to="/notifications" :aria-current="!push ? 'page' : null" :class="{ selected: !push }">
        <span>通知记录</span>
        <span v-if="unreadCount" class="badge badge-brand mono" aria-hidden="true">{{ unreadCount }}</span>
      </router-link>
      <router-link to="/notifications?tab=push" :aria-current="push ? 'page' : null" :class="{ selected: push }">
        <span>故障推送</span>
        <span
          v-if="enabledChannelsText"
          class="badge mono"
          :class="enabledChannelsCount > 0 ? 'badge-ok' : 'badge-neutral'"
          aria-hidden="true"
        >{{ enabledChannelsText }}</span>
      </router-link>
    </nav>

    <div v-if="push" class="push-layout">
      <div class="push-channel-list" role="group" aria-label="推送渠道">
        <button
          v-for="channel in channels"
          :key="channel.id"
          type="button"
          class="channel-card"
          :class="{ selected: selected === channel.id }"
          :aria-pressed="selected === channel.id"
          @click="selected = channel.id"
        >
          <div class="channel-head">
            <strong>{{ channel.name }}</strong>
            <span
              class="badge"
              :class="channelMeta[channel.id]?.enabled == null ? 'badge-neutral' : channelMeta[channel.id].enabled ? 'badge-ok' : 'badge-neutral'"
            >
              {{ channelMeta[channel.id]?.enabled == null ? '—' : channelMeta[channel.id].enabled ? '已启用' : '已停用' }}
            </span>
          </div>
          <p class="channel-desc">{{ channel.desc }}</p>
          <div v-if="channelMeta[channel.id]" class="channel-stats">
            <span class="badge badge-neutral mono">事件 {{ channelMeta[channel.id].events?.length ?? 0 }} 项</span>
            <span
              v-if="channelMeta[channel.id].last_error"
              class="badge badge-bad"
            >待重试 {{ channelMeta[channel.id].pending || 0 }}</span>
            <span
              v-else-if="channelMeta[channel.id].pending"
              class="badge badge-warn mono"
            >待发 {{ channelMeta[channel.id].pending }}</span>
            <span v-else-if="channelMeta[channel.id].enabled" class="badge badge-ok">队列正常</span>
          </div>
        </button>
      </div>
      <SpotlightCard class="report-panel push-editor-panel">
        <WebhookSettings :key="selected" :channel="selected" @loaded="onChannelLoaded(selected, $event)" />
      </SpotlightCard>
    </div>

    <template v-else>
      <div class="kpi-grid-4">
        <SpotlightCard class="kpi-card">
          <div class="kpi-top"><span>未读提醒</span></div>
          <div class="kpi-val mono" :class="{ 'kpi-brand': unreadCount > 0 }">{{ unreadCount }}</div>
        </SpotlightCard>
        <SpotlightCard class="kpi-card">
          <div class="kpi-top"><span>告警 / 预警</span></div>
          <div class="kpi-val mono" :class="{ 'kpi-warn': alertCount > 0 }">{{ alertCount }}</div>
        </SpotlightCard>
        <SpotlightCard class="kpi-card">
          <div class="kpi-top"><span>已恢复 / 完成</span></div>
          <div class="kpi-val mono kpi-ok">{{ resolvedCount }}</div>
        </SpotlightCard>
        <SpotlightCard class="kpi-card">
          <div class="kpi-top"><span>记录容量</span></div>
          <div class="kpi-val mono">{{ notifications.length }}<span class="kpi-cap"> / 60</span></div>
        </SpotlightCard>
      </div>

      <SpotlightCard class="report-panel notice-board">
        <div class="notice-toolbar">
          <div class="notice-filters">
            <div class="notice-segments" role="group" aria-label="阅读状态">
              <button
                v-for="tab in filters"
                :key="tab.key"
                type="button"
                :aria-pressed="filter === tab.key"
                @click="filter = tab.key"
              >
                {{ tab.label }} <span class="mono">{{ tab.count }}</span>
              </button>
            </div>
            <select v-model="category" class="form-select notice-cat-select" aria-label="通知分类">
              <option value="">全部分类</option>
              <option v-for="(label, key) in categories" :key="key" :value="key">{{ label }}</option>
            </select>
            <div class="search-box notice-search">
              <Search :size="14" class="search-icon" aria-hidden="true" />
              <input
                v-model="searchQuery"
                type="search"
                class="search-input"
                aria-label="搜索通知"
                placeholder="搜索资源或通知内容…"
                @keydown.esc="searchQuery = ''"
              />
            </div>
          </div>
          <div class="notice-bulk-actions">
            <button type="button" class="btn btn-secondary btn-sm" :disabled="!unreadCount" @click="markAllAsRead">
              全部标为已读
            </button>
            <button type="button" class="btn btn-ghost btn-sm" :disabled="!notifications.length" @click="clearAllNotifications">
              清空
            </button>
          </div>
        </div>

        <div v-if="visible.length" class="notice-feed">
          <article
            v-for="item in visible"
            :key="item.id"
            class="notice-item"
            :class="[{ unread: item.unread }, 'tone-' + severity(item).tone]"
          >
            <div class="notice-main">
              <div class="notice-meta">
                <span class="badge" :class="'badge-' + severity(item).tone">{{ severity(item).label }}</span>
                <span class="badge badge-neutral">{{ categories[item.category] || categories.system }}</span>
                <span class="notice-source">{{ isServerNotice(item) ? '服务端' : '本机' }}</span>
                <span v-if="item.unread" class="badge badge-brand">未读</span>
                <time class="notice-time-inline mono" :datetime="item.createdAt">{{ relativeTime(item.createdAt) }}</time>
              </div>
              <div class="notice-msg">{{ item.message }}</div>
              <span v-if="item.resource" class="notice-resource mono">{{ item.resource }}</span>
            </div>

            <div class="notice-aside">
              <div class="notice-time">
                <span>{{ relativeTime(item.createdAt) }}</span>
                <time :datetime="item.createdAt">{{ reportDate(item.createdAt) }}</time>
              </div>
              <div class="notice-actions">
                <button
                  v-if="item.unread"
                  type="button"
                  class="btn btn-secondary btn-sm"
                  @click="markAsRead(item.id)"
                >
                  标为已读
                </button>
                <router-link
                  v-if="item.path"
                  :to="item.path"
                  class="btn btn-ghost btn-sm"
                  @click="markAsRead(item.id)"
                >
                  查看详情
                </router-link>
                <button
                  type="button"
                  class="btn btn-ghost btn-sm"
                  aria-label="删除此通知"
                  @click="removeNotification(item.id)"
                >
                  <Trash2 :size="15" aria-hidden="true" />
                </button>
              </div>
            </div>
          </article>
        </div>
        <p v-else class="report-empty">{{ notifications.length ? '暂无符合条件的通知' : '暂无通知记录' }}</p>
      </SpotlightCard>
    </template>
  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { Search, Trash2 } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import WebhookSettings from '@/components/WebhookSettings.vue'
import { useToast } from '@/composables/useToast'
import { adminApi } from '@/api/admin'
import { reportDate } from '@/api/reports'
import '@/styles/reports.css'

const route = useRoute()
const push = computed(() => route.query.tab === 'push')
const { notifications, unreadCount, markAllAsRead, markAsRead, clearAllNotifications, removeNotification } = useToast()

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
  error: { label: '告警触发', tone: 'bad' },
  warning: { label: '预警提醒', tone: 'warn' },
  success: { label: '已恢复 / 完成', tone: 'ok' },
  info: { label: '系统与安全', tone: 'brand' }
}

function severity(item) {
  return tones[item.type === 'warn' ? 'warning' : item.type] || tones.info
}

function isServerNotice(item) {
  return Boolean(item?.id && !String(item.id).startsWith('n_') && item.resource)
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

const category = ref('')
const filter = ref('all')
const searchQuery = ref('')

const alertCount = computed(() =>
  notifications.value.filter(item => item.type === 'error' || item.type === 'warning' || item.type === 'warn').length
)
const resolvedCount = computed(() =>
  notifications.value.filter(item => item.type === 'success').length
)

const filtered = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  return notifications.value.filter(item => {
    if (category.value && (categories[item.category] ? item.category : 'system') !== category.value) return false
    if (!q) return true
    return String(item.message || '').toLowerCase().includes(q) || String(item.resource || '').toLowerCase().includes(q)
  })
})

const filters = computed(() => {
  const total = filtered.value.length
  const unread = filtered.value.filter(item => item.unread).length
  return [
    { key: 'all', label: '全部', count: total },
    { key: 'unread', label: '未读', count: unread },
    { key: 'read', label: '已读', count: total - unread }
  ]
})

const visible = computed(() =>
  filtered.value.filter(item => filter.value === 'all' || (filter.value === 'unread') === item.unread)
)

const channels = [
  { id: 'wxpusher', name: 'WxPusher', desc: '微信实时告警卡片推送' },
  { id: 'webhook', name: 'Webhook', desc: '通用 JSON POST 回调（支持企业微信 / 飞书 / 自建脚本）' }
]
const selected = ref('wxpusher')
const channelMeta = ref({})

function onChannelLoaded(id, payload) {
  channelMeta.value = { ...channelMeta.value, [id]: payload }
}

const loadedChannelEntries = computed(() => Object.values(channelMeta.value).filter(Boolean))
const enabledChannelsCount = computed(() => loadedChannelEntries.value.filter(item => item.enabled).length)
const enabledChannelsText = computed(() =>
  loadedChannelEntries.value.length ? `${enabledChannelsCount.value}/2 已启用` : ''
)

watch(push, async (open, _, cleanup) => {
  if (!open) return
  const controller = new AbortController()
  cleanup(() => controller.abort())
  const other = selected.value === 'wxpusher' ? 'webhook' : 'wxpusher'
  try {
    const data = await (other === 'webhook' ? adminApi.webhook : adminApi.wxPusher)(controller.signal)
    if (!controller.signal.aborted && !channelMeta.value[other]) {
      onChannelLoaded(other, {
        enabled: data.enabled,
        pending: data.pending || 0,
        last_error: data.last_error || '',
        events: data.events || []
      })
    }
  } catch { /* Channel editor offers retry on load failure. */ }
}, { immediate: true })
</script>

<style scoped>
.page-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; min-width: 0; }
.page-head-row { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; width: 100%; min-width: 0; }
.page-head-title { min-width: 0; }
.page-head-title h1 { font-size: 20px; font-weight: 700; color: var(--ink); margin: 0; line-height: 1.3; }
.page-sub { font-size: 12px; color: var(--muted); margin-top: 3px; overflow-wrap: anywhere; }

.notice-tabs { display: flex; align-items: center; gap: 24px; border-bottom: 1px solid var(--line); }
.notice-tabs a {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 10px 2px; color: var(--muted); border-bottom: 2px solid transparent;
  font-size: 13.5px; font-weight: 500; white-space: nowrap;
}
.notice-tabs a.selected { color: var(--brand-light); border-color: var(--brand); font-weight: 650; }
.theme-light .notice-tabs a.selected { color: var(--brand); }

.kpi-grid-4 { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 14px; }
.kpi-card { padding: 16px; }
.kpi-card :deep(.card-inner) { display: flex; flex-direction: column; justify-content: space-between; gap: 8px; }
.kpi-top { font-size: 12px; color: var(--muted); }
.kpi-val { font-size: 24px; font-weight: 700; line-height: 1.15; color: var(--ink); }
.kpi-brand { color: var(--brand-light); }
.theme-light .kpi-brand { color: var(--brand); }
.kpi-warn { color: var(--warn); }
.kpi-ok { color: var(--ok); }
.kpi-cap { font-size: 12px; color: var(--faint); font-weight: 400; margin-left: 2px; }
.mono { font-family: var(--mono); font-variant-numeric: tabular-nums; }

.notice-page .report-panel { min-width: 0; }
.notice-toolbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; margin-bottom: 16px; }
.notice-filters { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; flex: 1; min-width: 0; }
.notice-segments {
  display: inline-flex; background: var(--strip-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); padding: 3px; gap: 2px;
}
.notice-segments button {
  padding: 6px 10px; color: var(--muted); border-radius: 5px; font-size: 12px; white-space: nowrap;
}
.notice-segments [aria-pressed="true"] { background: var(--card-hover-bg); color: var(--ink); font-weight: 650; }
.notice-cat-select { width: 132px !important; flex-shrink: 0; }
.notice-search {
  display: flex; align-items: center; gap: 8px; padding: 0 12px; height: 40px;
  background: var(--input-bg); border: 1px solid var(--line); border-radius: var(--radius-sm);
  flex: 1; min-width: 170px; max-width: 260px;
}
.notice-search .search-icon { color: var(--faint); flex-shrink: 0; }
.notice-search .search-input {
  border: 0; background: transparent; color: var(--ink); font-size: 12.5px;
  flex: 1; min-width: 0; width: 100%; height: 100%; padding: 0;
}
.notice-bulk-actions { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }

.notice-feed { display: grid; grid-template-columns: minmax(0, 1fr); gap: 10px; min-width: 0; }
.notice-item {
  background: var(--strip-bg); border: 1px solid var(--line); border-left-width: 3px;
  border-radius: var(--radius-md); padding: 14px 16px; min-width: 0;
  display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 10px 16px; align-items: start;
  transition: var(--transition-smooth);
}
.notice-item.unread { background: rgba(99, 102, 241, 0.08); border-color: rgba(99, 102, 241, 0.3); }
.theme-light .notice-item.unread { background: rgba(99, 102, 241, 0.05); }
.notice-item.tone-bad { border-left-color: var(--bad); }
.notice-item.tone-warn { border-left-color: var(--warn); }
.notice-item.tone-ok { border-left-color: var(--ok); }
.notice-item.tone-brand { border-left-color: var(--info); }

.notice-main { min-width: 0; }
.notice-meta { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; }
.notice-source { font-size: 11px; color: var(--faint); }
.notice-time-inline { display: none; font-size: 11px; color: var(--faint); margin-left: auto; white-space: nowrap; }
.notice-msg { font-size: 13.5px; font-weight: 600; color: var(--ink); margin-top: 6px; line-height: 1.45; overflow-wrap: anywhere; }
.notice-resource {
  display: inline-block; max-width: 100%; margin-top: 6px; padding: 2px 8px;
  border-radius: 6px; font-size: 11.5px;
  background: rgba(99, 102, 241, 0.1); color: var(--brand-light); overflow-wrap: anywhere;
}
.theme-light .notice-resource { color: var(--brand); background: rgba(99, 102, 241, 0.08); }

.notice-aside { display: flex; flex-direction: column; align-items: flex-end; gap: 8px; }
.notice-time { font-size: 12px; color: var(--ink-secondary); white-space: nowrap; text-align: right; }
.notice-time time { display: block; font-size: 11px; color: var(--faint); font-family: var(--mono); margin-top: 2px; }
.notice-actions { display: flex; align-items: center; gap: 6px; }

.push-layout { display: grid; grid-template-columns: minmax(220px, 270px) minmax(0, 1fr); gap: 16px; align-items: start; min-width: 0; }
.push-channel-list { display: grid; grid-template-columns: minmax(0, 1fr); gap: 10px; min-width: 0; }
.channel-card {
  text-align: left; padding: 14px; border-radius: var(--radius-md); min-width: 0;
  background: var(--card-bg); border: 1px solid var(--card-border);
  box-shadow: var(--shadow-card); width: 100%;
  display: grid; grid-template-columns: minmax(0, 1fr); gap: 6px; transition: var(--transition-smooth);
}
.channel-card:hover { border-color: var(--card-border-hover); background: var(--card-hover-bg); }
.channel-card.selected { border-color: var(--brand); background: var(--card-hover-bg); box-shadow: 0 0 0 1px rgba(99, 102, 241, 0.25); }
.channel-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.channel-head strong { font-size: 14px; color: var(--ink); }
.channel-desc { font-size: 12px; color: var(--muted); line-height: 1.45; }
.channel-stats { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; margin-top: 2px; }

@media (max-width: 960px) {
  .kpi-grid-4 { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
  .push-layout { grid-template-columns: minmax(0, 1fr); gap: 12px; }
  .push-channel-list { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
}

@media (max-width: 650px) {
  .kpi-card { padding: 12px; }
  .kpi-val { font-size: 20px; }
  .notice-toolbar { flex-direction: column; align-items: stretch; gap: 8px; }
  .notice-filters { display: grid; grid-template-columns: 112px minmax(0, 1fr); gap: 8px; }
  .notice-segments { grid-column: 1 / -1; display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); width: 100%; }
  .notice-segments button { text-align: center; padding: 7px 4px; }
  .notice-cat-select { width: 100% !important; }
  .notice-search { max-width: none; min-width: 0; width: 100%; }
  .notice-bulk-actions { display: grid; grid-template-columns: minmax(0, 1fr) auto; width: 100%; }
  .notice-bulk-actions .btn { min-height: 40px; }

  .notice-item { grid-template-columns: minmax(0, 1fr); padding: 12px; gap: 8px; }
  .notice-time-inline { display: inline; }
  .notice-aside { align-items: stretch; border-top: 1px dashed var(--line); padding-top: 8px; }
  .notice-time { display: none; }
  .notice-actions { justify-content: flex-end; flex-wrap: wrap; width: 100%; }
  .notice-actions .btn { min-height: 38px; }

  .channel-card { padding: 10px; }
  .channel-desc { display: none; }
}

@media (max-width: 360px) {
  .notice-filters { grid-template-columns: minmax(0, 1fr); }
  .push-channel-list { grid-template-columns: minmax(0, 1fr); }
}
</style>
