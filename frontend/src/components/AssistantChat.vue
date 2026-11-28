<template>
  <section class="assistant-chat" :class="{ 'assistant-full': full, 'assistant-embedded': full && embedded }" aria-label="网关助手对话">
    <header class="assistant-header">
      <div class="assistant-heading">
        <Sparkles :size="20" class="assistant-mark" aria-hidden="true" />
        <h1 v-if="full && !embedded">网关助手</h1>
        <h2 v-else>网关助手</h2>
        <div class="assistant-actions">
          <RouterLink v-if="!full" to="/assistant" class="icon-action-btn" aria-label="打开独立页面" title="打开独立页面"><Maximize2 :size="16" /></RouterLink>
          <button type="button" class="icon-action-btn" aria-label="历史对话" title="历史对话" :aria-expanded="showHistory" @click="showHistory = !showHistory"><History :size="17" /></button>
          <button type="button" class="icon-action-btn" aria-label="新建对话" title="新建对话" :disabled="sending || applying || (!messages.length && !draft)" @click="newConversation"><SquarePen :size="17" /></button>
          <button v-if="!full" class="icon-action-btn" aria-label="收起网关助手" title="收起网关助手" @click="panelOpen = false"><X :size="18" /></button>
        </div>
      </div>
      <div class="assistant-model">
        <span v-if="provider" class="model-dot" aria-hidden="true"></span>
        <span class="provider-name">{{ provider ? provider.name : '模型未配置' }}</span>
        <RouterLink to="/assistant/settings" class="model-settings"><Settings2 :size="14" /> 接入设置</RouterLink>
      </div>
    </header>

    <section v-if="showHistory" class="assistant-history" aria-label="历史对话">
      <div class="history-heading"><strong>历史对话</strong><span>当前浏览器</span></div>
      <p v-if="!conversations.length" class="assistant-note">还没有保存的对话。</p>
      <div v-for="row in conversations" :key="row.id" class="history-row">
        <button type="button" class="history-open" :aria-current="conversation?.id === row.id ? 'true' : undefined" :disabled="sending || applying" @click="restore(row)">
          <span>{{ row.title }}</span><small>{{ dateFormat.format(row.updated) }}</small>
        </button>
        <button type="button" class="icon-action-btn" :disabled="sending || applying" :aria-label="`删除对话 ${row.title}`" @click="removeConversation(row)"><Trash2 :size="15" /></button>
      </div>
    </section>

    <div ref="timeline" class="assistant-timeline" role="log" aria-label="对话记录" :aria-busy="sending">
      <div v-if="!messages.length" class="assistant-empty">
        <Sparkles :size="24" class="assistant-mark" aria-hidden="true" />
        <h2>有什么需要查看或调整？</h2>
        <p v-if="loadError" class="assistant-error">{{ loadError }}</p>
        <template v-else-if="settings && !providers.length">
          <p>配置模型接入点后，即可在这里查看网关状态、跳转设置或提出配置变更。</p>
          <RouterLink class="btn btn-primary" to="/assistant/settings">配置模型接入点</RouterLink>
        </template>
        <p v-else>可以查询反代健康、最近采样和运行状态，也可以提出反代及健康检查配置变更。</p>
      </div>

      <article
        v-for="(message, index) in messages"
        :key="index"
        v-show="message.role !== 'assistant' || !message.streaming || message.content"
        class="assistant-message"
        :class="[message.role, { fresh: message.fresh, streaming: message.streaming }]"
      >
        <div v-if="message.role === 'assistant'" class="assistant-byline"><Sparkles :size="15" aria-hidden="true" /> <span>网关助手</span><small v-if="message.model">{{ message.model }}</small></div>
        <div v-if="message.role === 'assistant'" class="assistant-markdown" v-html="renderMarkdown(message.content)" />
        <p v-else class="assistant-copy">{{ message.content }}</p>
        <p v-if="message.hint" class="assistant-note">{{ message.hint }}</p>
        <RouterLink v-if="message.link" class="assistant-link" :to="linkTo(message.link)" @click="followLink">
          <ExternalLink :size="14" /> {{ message.link.label }}
        </RouterLink>
        <div v-if="message.change" class="assistant-plan">
          <div class="plan-head">
            <strong>{{ message.change.name }}</strong>
            <span class="badge" :class="message.outcome === '已应用' ? 'badge-ok' : message.outcome ? 'badge-neutral' : 'badge-warn'">{{ message.outcome || '待确认' }}</span>
          </div>
          <div class="plan-diff mono">
            <div class="old"><span>原值</span> {{ changeValue(message.change, 'before') }}</div>
            <div class="new"><span>新值</span> {{ changeValue(message.change, 'after') }}</div>
          </div>
          <p v-if="message.changeError" class="assistant-error" role="alert">{{ message.changeError }}</p>
          <div v-if="!message.outcome" class="plan-actions">
            <button class="btn btn-primary btn-sm" :disabled="applying || sending" @click="apply(message)"><Check :size="14" /> 确认应用</button>
            <button class="btn btn-secondary btn-sm" :disabled="applying" @click="cancelChange(message)">取消</button>
          </div>
        </div>
      </article>
      <div v-if="sending && !streamingContentStarted" class="assistant-wait" role="status"><span aria-hidden="true"><i></i><i></i><i></i></span>正在等待模型回复</div>
      <p v-if="error" class="assistant-error" role="alert">{{ error }}</p>
    </div>

    <footer class="assistant-footer">
      <div v-if="provider" class="assistant-suggestions">
        <button v-for="question in questions" :key="question" class="btn btn-secondary btn-sm" :disabled="sending || applying" @click="submit(question)">{{ question }}</button>
      </div>
      <form class="assistant-composer" @submit.prevent="submit()">
        <textarea v-model="draft" aria-label="给网关助手发送消息" placeholder="询问网关状态，或描述想修改的配置…" maxlength="4000" rows="2"
          :disabled="!provider" @keydown.enter="handleEnter" />
        <div class="composer-actions">
          <template v-if="provider">
            <label class="sr-only" for="assistant-model-select">模型</label>
            <select id="assistant-model-select" :title="model" :value="JSON.stringify([provider.id, model])" :disabled="sending || applying" @change="changeModel">
              <optgroup v-for="row in providers" :key="row.id" :label="row.name">
                <option v-for="id in row.models" :key="id" :value="JSON.stringify([row.id, id])">{{ id }}</option>
              </optgroup>
            </select>
            <label class="sr-only" for="assistant-effort-select">思考强度</label>
            <select id="assistant-effort-select" title="思考强度" v-model="effortChoice" :disabled="sending || applying">
              <option :value="null">默认{{ provider.effort ? ' · ' + effortLabels[provider.effort] : '' }}</option>
              <option value="">模型默认</option>
              <option value="low">低</option><option value="medium">中</option><option value="high">高</option>
            </select>
          </template>
          <button v-if="sending" class="btn btn-secondary btn-sm" type="button" aria-label="停止生成" title="停止生成" @click="stopGeneration"><Square :size="14" /></button>
          <button v-else class="btn btn-primary btn-sm" type="submit" aria-label="发送消息" :disabled="applying || !draft.trim() || !provider"><ArrowUp :size="16" /></button>
        </div>
      </form>
    </footer>
  </section>
</template>

<script setup>
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import MarkdownIt from 'markdown-it'
import { ArrowUp, Check, ExternalLink, History, Maximize2, Settings2, Sparkles, Square, SquarePen, Trash2, X } from 'lucide-vue-next'
import { useAssistant } from '@/composables/useAssistant'

const props = defineProps({ full: Boolean, embedded: Boolean })
const route = useRoute()
const { panelOpen, draft, messages, settings, providers, provider, model, effortChoice, conversations, conversation,
  sending, applying, error, loadSettings, send, stopGeneration, apply, cancelChange, clearConversation,
  openConversation, removeConversation, selectModel } = useAssistant()
const showHistory = ref(false)
const effortLabels = { low: '低', medium: '中', high: '高' }
const dateFormat = new Intl.DateTimeFormat('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
const markdown = new MarkdownIt({ html: false, breaks: true }).disable(['image', 'link'])
const markdownCache = new Map()
const streamingContentStarted = computed(() => {
  const latest = messages.value.at(-1)
  return Boolean(latest?.role === 'assistant' && latest.streaming && latest.content)
})

function renderMarkdown(text) {
  const source = String(text || '')
  if (markdownCache.has(source)) return markdownCache.get(source)
  const html = markdown.render(source)
  if (markdownCache.size >= 120) markdownCache.delete(markdownCache.keys().next().value)
  markdownCache.set(source, html)
  return html
}

function changeModel(event) {
  selectModel(...JSON.parse(event.target.value))
}
function restore(row) {
  openConversation(row)
  showHistory.value = false
  nextTick(scrollToLatest)
}
function newConversation() {
  clearConversation()
  showHistory.value = false
}
const timeline = ref(null)
const loadError = ref('')
const questions = ['查看反代状态', '查看网关运行状态', '打开健康检查设置']

function linkTo(link) {
  return { path: link.path, query: link.health ? { health: link.health } : {} }
}
function followLink() {
  if (window.innerWidth < 1280) panelOpen.value = false
}
function changeValue(change, key) {
  return change[key] + (change.field === 'health_interval' ? ' 秒' : '')
}
function submit(content = draft.value) {
  if (!content.trim() || sending.value || applying.value || !provider.value) return
  send(content, props.full ? '' : route.path)
}
function handleEnter(event) {
  if (!event.isComposing && !event.shiftKey) {
    event.preventDefault()
    submit()
  }
}
function scrollToLatest() {
  if (timeline.value) timeline.value.scrollTop = timeline.value.scrollHeight
}
watch([() => messages.value.length, sending], () => nextTick(scrollToLatest))
watch(() => messages.value.at(-1)?.content, () => {
  const el = timeline.value
  if (!el) return
  if (el.scrollHeight - el.scrollTop - el.clientHeight < 120) {
    nextTick(scrollToLatest)
  }
})
onMounted(() => {
  scrollToLatest()
  loadSettings().catch(err => { if (err.name !== 'AbortError') loadError.value = err.message })
})
</script>

<style scoped>
.assistant-chat { display: flex; flex-direction: column; min-height: 0; min-width: 0; background: var(--sidebar-bg); height: 100%; }
.assistant-header { padding: 16px 20px 12px; border-bottom: 1px solid var(--line); }
.assistant-heading, .assistant-model, .assistant-actions, .assistant-byline, .assistant-link, .plan-head, .plan-actions, .composer-actions, .assistant-suggestions, .model-settings { display: flex; align-items: center; gap: 8px; }
.assistant-heading h1, .assistant-heading h2 { margin: 0; font-size: 20px; line-height: 1.3; white-space: nowrap; }
.assistant-mark, .assistant-byline, .assistant-link { color: var(--brand-light); }
.assistant-heading .assistant-mark, .model-dot { flex: none; }
.assistant-actions { margin-left: auto; }
.assistant-actions .icon-action-btn {
  display: grid; place-items: center; width: 32px; height: 32px;
  border: 1px solid transparent; border-radius: var(--radius-sm); background: transparent; color: var(--muted);
}
.assistant-actions .icon-action-btn:disabled { opacity: .45; }
.assistant-actions .icon-action-btn:hover { border-color: var(--line); color: var(--ink); background: var(--strip-bg); }
.assistant-model { margin-top: 9px; color: var(--muted); font-size: 12px; }
.provider-name { min-width: 0; overflow-wrap: anywhere; }
.model-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--ok); }
.model-settings { margin-left: auto; color: var(--ink-secondary); white-space: nowrap; }
.assistant-timeline { flex: 1; min-height: 0; overflow: auto; padding: 20px; }
.assistant-empty { padding: 26px 0; color: var(--muted); }
.assistant-empty h2 { color: var(--ink); margin: 8px 0; font-size: 18px; }
.assistant-empty p { margin: 8px 0 14px; }
.assistant-message { margin-bottom: 18px; overflow-wrap: anywhere; }
.assistant-message.user { margin-left: auto; width: fit-content; max-width: 90%; padding: 9px 12px; border: 1px solid color-mix(in srgb, var(--brand) 45%, var(--line)); border-radius: 12px 12px 4px 12px; background: color-mix(in srgb, var(--brand) 21%, var(--card-bg)); }
.assistant-byline { margin-bottom: 9px; color: var(--ink-secondary); font-size: 12px; font-weight: 650; }
.assistant-byline small { margin-left: auto; color: var(--muted); font-weight: 400; overflow-wrap: anywhere; }
.assistant-copy { margin: 0; white-space: pre-wrap; font-size: 14px; }
.assistant-note, .assistant-error { margin: 8px 0; font-size: 13px; color: var(--muted); }
.assistant-error { color: var(--bad); }
.assistant-link { width: fit-content; margin-top: 8px; text-decoration: underline; font-size: 13px; }
.assistant-plan { border: 1px solid var(--line); background: var(--card-bg); padding: 12px; border-radius: var(--radius-md); margin-top: 9px; }
.plan-head, .composer-actions { justify-content: space-between; }
.plan-diff { border: 1px solid var(--line); background: var(--strip-bg); border-radius: 7px; margin: 10px 0; padding: 8px 10px; font-size: 12px; }
.plan-diff > div { display: grid; grid-template-columns: 2.5em minmax(0, 1fr); gap: 8px; padding: 3px 0; color: var(--ink); }
.plan-diff span { color: var(--muted); font-family: var(--sans); }
.plan-diff .new { font-weight: 650; }
.assistant-footer { padding: 10px 16px max(14px, env(safe-area-inset-bottom)); }
.assistant-suggestions { flex-wrap: wrap; margin-bottom: 8px; gap: 6px; }
.assistant-suggestions .btn { border-radius: var(--radius-pill); font-size: 11px; }
.assistant-composer { border: 1px solid var(--card-border-hover); background: var(--input-bg); border-radius: var(--radius-md); padding: 8px; }
.assistant-composer textarea { width: 100%; padding: 4px; border: 0; background: transparent; color: var(--ink); resize: vertical; min-height: 42px; max-height: 130px; font: inherit; }
.composer-actions select { color: var(--ink-secondary); background: var(--strip-bg); border: 1px solid var(--line); border-radius: 7px; min-height: 34px; padding: 0 5px; font: inherit; font-size: 12px; }
#assistant-model-select { flex: 1; min-width: 0; max-width: 245px; text-overflow: ellipsis; }
#assistant-effort-select { flex: none; width: auto !important; min-width: 108px; max-width: 132px; padding: 0 26px 0 8px; text-overflow: ellipsis; }
.composer-actions button { margin-left: auto; }
.assistant-full { border: 1px solid var(--line); border-radius: var(--radius-card); overflow: hidden; min-height: 490px; height: calc(100dvh - var(--topbar-h) - 48px); }
.assistant-full.assistant-embedded { min-height: 460px; height: calc(100dvh - var(--topbar-h) - 188px); }
.assistant-full .assistant-timeline, .assistant-full .assistant-footer { width: min(100%, 820px); margin-inline: auto; }
.assistant-full .assistant-heading h1, .assistant-full.assistant-embedded .assistant-heading h2 { font-size: 20px; }
.assistant-message.assistant { background: var(--card-bg); border: 1px solid var(--line); border-radius: 4px 14px 14px; padding: 13px 15px; }
.assistant-markdown { line-height: 1.65; font-size: 14px; overflow-x: auto; }
.assistant-markdown :deep(> :first-child) { margin-top: 0; }
.assistant-markdown :deep(> :last-child) { margin-bottom: 0; }
.assistant-markdown :deep(pre) { padding: 10px 12px; overflow-x: auto; border-radius: 7px; background: var(--strip-bg); border: 1px solid var(--line); }
.assistant-markdown :deep(code) { font-family: var(--mono); }
.assistant-markdown :deep(blockquote) { margin-inline: 0; border-left: 3px solid var(--line); padding-inline: 12px; color: var(--ink-secondary); }
.assistant-markdown :deep(th), .assistant-markdown :deep(td) { border: 1px solid var(--line); padding: 4px 8px; }
.assistant-markdown :deep(a) { text-decoration: underline; color: inherit; }
.assistant-history { flex: 0 1 auto; border-bottom: 1px solid var(--line); max-height: min(32dvh, 260px); overflow: auto; padding: 10px 16px; }
.history-heading, .history-row, .history-open { display: flex; gap: 8px; align-items: center; }
.history-heading { justify-content: space-between; font-size: 13px; padding: 0 6px 6px; }
.history-heading span, .history-open small { color: var(--muted); white-space: nowrap; font-size: 11px; }
.history-open { min-width: 0; flex: 1; justify-content: space-between; min-height: 38px; text-align: left; padding: 6px; border: 0; background: transparent; color: var(--ink); border-radius: 6px; }
.history-open span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.history-open:hover, .history-open[aria-current] { background: var(--strip-bg); }
.history-row > .icon-action-btn { color: var(--muted); background: transparent; border: 0; padding: 7px; }
.assistant-wait { display: flex; align-items: center; gap: 10px; color: var(--muted); font-size: 13px; padding: 4px 0 10px; }
.assistant-wait span { display: flex; gap: 4px; }
.assistant-wait i { width: 5px; height: 5px; border-radius: 50%; background: var(--brand-light); animation: pulse-dot 1s ease-in-out infinite alternate; }
.assistant-wait i:nth-child(2) { animation-delay: .16s; }
.assistant-wait i:nth-child(3) { animation-delay: .32s; }
.fresh { animation: reveal-reply .35s ease-out both; }
.assistant-message.streaming .assistant-markdown::after {
  content: '';
  display: inline-block;
  width: 2px;
  height: 1em;
  margin-left: 3px;
  vertical-align: -0.15em;
  background: var(--brand-light);
  border-radius: 1px;
  animation: stream-caret 0.8s steps(1) infinite;
}
@keyframes stream-caret { 50% { opacity: 0; } }
@keyframes pulse-dot { from { opacity: .3; transform: translateY(2px); } to { opacity: 1; transform: translateY(-2px); } }
@keyframes reveal-reply { from { opacity: 0; transform: translateY(5px); } to { opacity: 1; transform: none; } }
@media (prefers-reduced-motion: reduce) { .assistant-wait i, .fresh, .assistant-message.streaming .assistant-markdown::after { animation: none; } }
@media (max-width: 600px) {
  .assistant-header { padding: 10px 12px; }
  .assistant-heading { gap: 6px; }
  .assistant-heading h2 { font-size: 18px; }
  .assistant-actions { gap: 2px; }
  .assistant-timeline { padding: 14px; }
  .assistant-actions .icon-action-btn { width: 44px; min-height: 44px; }
  .composer-actions select, .composer-actions button { min-height: 44px !important; }
  .composer-actions button { min-width: 44px; }
  .assistant-footer { padding-inline: 12px; }
  .assistant-suggestions { flex-wrap: nowrap; overflow: auto; scrollbar-width: thin; }
  .assistant-suggestions .btn { flex: none; }
  .assistant-full { min-height: 0; height: calc(100dvh - var(--topbar-h) - 32px); }
  .assistant-full.assistant-embedded { min-height: 420px; height: calc(100dvh - var(--topbar-h) - 196px); }
}
@media (max-width: 400px) {
  .assistant-suggestions { flex-wrap: wrap; overflow: visible; }
  .composer-actions { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 6px; }
  #assistant-model-select { grid-column: 1 / -1; width: 100%; max-width: none; }
  .composer-actions button { grid-column: 2; }
}
</style>
