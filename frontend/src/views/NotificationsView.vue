<template>
  <div class="report-page notice-page">
    <header class="report-heading"><h1>通知中心</h1></header>
    <nav class="notice-tabs" aria-label="通知中心页面">
      <router-link to="/notifications" :aria-current="!push ? 'page' : null" :class="{ selected: !push }">通知记录</router-link>
      <router-link to="/notifications?tab=push" :aria-current="push ? 'page' : null" :class="{ selected: push }">故障推送</router-link>
    </nav>
    <div v-if="push" class="push-layout">
      <SpotlightCard class="report-panel">
        <h2>推送渠道</h2>
        <div class="push-channels">
          <button v-for="channel in channels" :key="channel.id" type="button" class="btn btn-secondary"
            :aria-pressed="selected === channel.id" @click="selected = channel.id">
            <strong>{{ channel.name }}</strong>
            <small>{{ states[channel.id] == null ? '—' : states[channel.id] ? '已启用' : '已停用' }}</small>
          </button>
        </div>
      </SpotlightCard>
      <SpotlightCard class="report-panel"><WebhookSettings :key="selected" :channel="selected" @loaded="states[selected] = $event" /></SpotlightCard>
    </div>
    <SpotlightCard v-else class="report-panel notice-board">
      <div class="report-heading notice-toolbar">
        <div class="notice-filters">
          <div class="notice-segments" role="group" aria-label="阅读状态">
            <button v-for="tab in filters" :key="tab.key" type="button" :aria-pressed="filter === tab.key" @click="filter = tab.key">
              {{ tab.label }} <span>{{ tab.count }}</span>
            </button>
          </div>
          <select v-model="category" class="form-input" aria-label="通知分类">
            <option value="">全部分类</option><option v-for="(label, key) in categories" :key="key" :value="key">{{ label }}</option>
          </select>
        </div>
        <div class="report-actions">
          <button class="btn btn-secondary" :disabled="!unreadCount" @click="markAllAsRead">全部标为已读</button>
          <button class="btn btn-ghost" :disabled="!notifications.length" @click="clearAllNotifications">清空</button>
        </div>
      </div>
      <div v-if="visible.length" class="report-table-wrap">
        <table class="report-table"><thead><tr><th>通知</th><th>分类</th><th>级别</th><th>时间</th><th>阅读状态</th><th>操作</th></tr></thead>
          <tbody><tr v-for="item in visible" :key="item.id">
            <td class="message"><strong>{{ item.message }}</strong><small v-if="item.resource">{{ item.resource }}</small></td>
            <td>{{ categories[item.category] || categories.system }}</td>
            <td><span class="badge" :class="'badge-' + severity(item).tone">{{ severity(item).label }}</span></td>
            <td><time :datetime="item.createdAt">{{ reportDate(item.createdAt) }}</time></td>
            <td><button v-if="item.unread" class="btn btn-ghost unread" aria-label="标为已读" @click="markAsRead(item.id)">未读</button><span v-else class="read">已读</span></td>
            <td><div class="row-actions"><router-link v-if="item.path" :to="item.path" class="btn btn-ghost" @click="markAsRead(item.id)">查看详情</router-link>
              <button class="btn btn-ghost" aria-label="删除此通知" @click="removeNotification(item.id)"><Trash2 :size="15" /></button></div></td>
          </tr></tbody>
        </table>
      </div>
      <p v-else class="report-empty">{{ notifications.length ? '暂无符合条件的通知' : '暂无通知记录' }}</p>
    </SpotlightCard>
  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { Trash2 } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import WebhookSettings from '@/components/WebhookSettings.vue'
import { useToast } from '@/composables/useToast'
import { adminApi } from '@/api/admin'
import { reportDate } from '@/api/reports'
import '@/styles/reports.css'

const route = useRoute()
const push = computed(() => route.query.tab === 'push')
const { notifications, unreadCount, markAllAsRead, markAsRead, clearAllNotifications, removeNotification } = useToast()
const categories = { upstream: '反向代理', backup: '备份与恢复', cert: 'SSL 证书', waf: '安全防护', ddns: '动态域名', mcp: 'AI 管理', system: '系统通知' }
const tones = { error: { label: '错误', tone: 'bad' }, warning: { label: '警告', tone: 'warn' }, success: { label: '成功', tone: 'ok' }, info: { label: '信息', tone: 'neutral' } }
function severity(item) {
  return tones[item.type === 'warn' ? 'warning' : item.type] || tones.info
}
const category = ref(''), filter = ref('all')
const filtered = computed(() => notifications.value.filter(item => !category.value || (categories[item.category] ? item.category : 'system') === category.value))
const filters = computed(() => {
  const total = filtered.value.length
  const unread = filtered.value.filter(item => item.unread).length
  return [{ key: 'all', label: '全部', count: total }, { key: 'unread', label: '未读', count: unread }, { key: 'read', label: '已读', count: total - unread }]
})
const visible = computed(() => filtered.value.filter(item => filter.value === 'all' || (filter.value === 'unread') === item.unread))
const channels = [{ id: 'wxpusher', name: 'WxPusher' }, { id: 'webhook', name: 'Webhook' }]
const selected = ref('wxpusher'), states = ref({})
watch(push, async (open, _, cleanup) => {
  if (!open) return
  const controller = new AbortController()
  cleanup(() => controller.abort())
  const other = selected.value === 'wxpusher' ? 'webhook' : 'wxpusher'
  try {
    const data = await (other === 'webhook' ? adminApi.webhook : adminApi.wxPusher)(controller.signal)
    if (states.value[other] == null) states.value[other] = data.enabled
  } catch { /* Channel editor offers retry on load failure. */ }
}, { immediate: true })
</script>

<style scoped>
.notice-tabs,.notice-filters,.notice-segments,.row-actions { display:flex;align-items:center }
.notice-tabs { gap:24px;border-bottom:1px solid var(--line) }
.notice-tabs a { padding:10px 3px;color:var(--muted);border-bottom:2px solid transparent }
.notice-tabs a.selected { color:var(--brand);border-color:var(--brand);font-weight:650 }
.notice-toolbar { gap:12px;margin-bottom:16px;flex-wrap:wrap }
.notice-filters { flex-wrap:wrap;gap:10px }
.notice-filters select { width:auto !important; max-width:100% }
.notice-segments { background:var(--strip-bg);border:1px solid var(--line);border-radius:var(--radius-sm);padding:3px }
.notice-segments button { padding:7px 9px;color:var(--muted);border-radius:5px }
.notice-segments [aria-pressed=true] { background:var(--card-bg);color:var(--ink);font-weight:650 }
.message { overflow-wrap:anywhere;min-width:140px }
.read { color:var(--muted) }
.unread { color:var(--brand) }
.row-actions { gap:4px }
.push-layout { display:grid;grid-template-columns:minmax(180px,260px) minmax(0,1fr);gap:16px;align-items:start }
.push-channels { display:grid;gap:8px }
.push-channels button { justify-content:space-between }
.push-channels [aria-pressed=true] { border-color:var(--brand);color:var(--brand) }
.notice-page .report-panel { min-width:0 }
@media(max-width:800px){.push-layout{grid-template-columns:1fr}}
@media(max-width:650px){
  .notice-board table,.notice-board tbody{display:block;min-width:0}
  .notice-board thead{display:none}
  .notice-board tr{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:3px 8px;padding:10px;border-bottom:1px solid var(--line)}
  .notice-board td{border:0;padding:3px;align-self:center}
  .notice-board .message,.notice-board td:has(time){grid-column:1/-1;white-space:normal}
  .row-actions .btn { min-height:44px;min-width:44px }
}
</style>
