<template>
  <section class="assistant-chat" :class="{ 'assistant-full': full }" aria-label="网关助手对话">
    <header class="assistant-header">
      <div class="assistant-heading">
        <Sparkles :size="20" class="assistant-mark" aria-hidden="true" />
        <h1 v-if="full">网关助手</h1>
        <h2 v-else>网关助手</h2>
        <div class="assistant-actions">
          <RouterLink v-if="!full" to="/assistant" class="icon-action-btn" aria-label="打开独立页面" title="打开独立页面"><Maximize2 :size="16" /></RouterLink>
          <button v-else class="btn btn-secondary btn-sm" :disabled="sending || applying || !messages.length" @click="clearConversation">新建对话</button>
          <button v-if="!full" class="icon-action-btn" aria-label="收起网关助手" title="收起网关助手" @click="panelOpen = false"><X :size="18" /></button>
        </div>
      </div>
      <div class="assistant-model">
        <span v-if="settings?.base_url" class="model-dot" aria-hidden="true"></span>
        <span>{{ settings?.base_url ? settings.name : '模型未配置' }}</span>
        <RouterLink to="/assistant/settings" class="model-settings"><Settings2 :size="14" /> 接入设置</RouterLink>
      </div>
    </header>

    <div ref="timeline" class="assistant-timeline" role="log" aria-label="对话记录" :aria-busy="sending">
      <div v-if="!messages.length" class="assistant-empty">
        <Sparkles :size="24" class="assistant-mark" aria-hidden="true" />
        <h2>有什么需要查看或调整？</h2>
        <p v-if="loadError" class="assistant-error">{{ loadError }}</p>
        <template v-else-if="settings && !settings.base_url">
          <p>配置模型接入点后，即可在这里查看网关状态、跳转设置或提出配置变更。</p>
          <RouterLink class="btn btn-primary" to="/assistant/settings">配置模型接入点</RouterLink>
        </template>
        <p v-else>可以查询反代健康、最近采样和运行状态，也可以提出反代及健康检查配置变更。</p>
      </div>

      <article v-for="(message, index) in messages" :key="index" class="assistant-message" :class="message.role">
        <div v-if="message.role === 'assistant'" class="assistant-byline"><Sparkles :size="15" aria-hidden="true" /> 网关助手</div>
        <p class="assistant-copy">{{ message.content }}</p>
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
            <div class="old"><span class="sr-only">原值</span>− {{ changeValue(message.change, 'before') }}</div>
            <div class="new"><span class="sr-only">新值</span>+ {{ changeValue(message.change, 'after') }}</div>
          </div>
          <p v-if="message.changeError" class="assistant-error" role="alert">{{ message.changeError }}</p>
          <div v-if="!message.outcome" class="plan-actions">
            <button class="btn btn-primary btn-sm" :disabled="applying || sending" @click="apply(message)"><Check :size="14" /> 确认应用</button>
            <button class="btn btn-secondary btn-sm" :disabled="applying" @click="message.outcome = '已取消'">取消</button>
          </div>
        </div>
      </article>
      <p v-if="sending" class="assistant-note" role="status">正在等待模型回复…</p>
      <p v-if="error" class="assistant-error" role="alert">{{ error }}</p>
    </div>

    <footer class="assistant-footer">
      <div v-if="settings?.base_url" class="assistant-suggestions">
        <button v-for="question in questions" :key="question" class="btn btn-secondary btn-sm" :disabled="sending || applying" @click="submit(question)">{{ question }}</button>
      </div>
      <form class="assistant-composer" @submit.prevent="submit()">
        <textarea v-model="draft" aria-label="给网关助手发送消息" placeholder="询问网关状态，或描述想修改的配置…" maxlength="4000" rows="2"
          :disabled="!settings?.base_url" @keydown.enter="handleEnter" />
        <div class="composer-actions">
          <span v-if="!full"><PanelLeft :size="13" /> 当前页 · {{ route.meta.title }}</span>
          <button class="btn btn-primary btn-sm" type="submit" aria-label="发送消息" :disabled="sending || applying || !draft.trim() || !settings?.base_url"><ArrowUp :size="16" /></button>
        </div>
      </form>
    </footer>
  </section>
</template>

<script setup>
import { nextTick, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ArrowUp, Check, ExternalLink, Maximize2, PanelLeft, Settings2, Sparkles, X } from 'lucide-vue-next'
import { useAssistant } from '@/composables/useAssistant'

const props = defineProps({ full: Boolean })
const route = useRoute()
const { panelOpen, messages, settings, sending, applying, error, loadSettings, send, apply, clearConversation } = useAssistant()
const draft = ref('')
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
  if (!content.trim() || sending.value || applying.value || !settings.value?.base_url) return
  draft.value = ''
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
onMounted(() => {
  scrollToLatest()
  loadSettings().catch(err => { if (err.name !== 'AbortError') loadError.value = err.message })
})
</script>

<style scoped>
.assistant-chat { display: flex; flex-direction: column; min-height: 0; background: var(--sidebar-bg); height: 100%; }
.assistant-header { padding: 16px 20px 12px; border-bottom: 1px solid var(--line); }
.assistant-heading, .assistant-model, .assistant-actions, .assistant-byline, .assistant-link, .plan-head, .plan-actions, .composer-actions, .assistant-suggestions, .model-settings { display: flex; align-items: center; gap: 8px; }
.assistant-heading h1, .assistant-heading h2 { margin: 0; font-size: 20px; line-height: 1.3; }
.assistant-mark, .assistant-byline, .assistant-link { color: var(--brand-light); }
.assistant-actions { margin-left: auto; }
.assistant-actions .icon-action-btn {
  display: grid; place-items: center; width: 32px; height: 32px;
  border: 1px solid transparent; border-radius: var(--radius-sm); background: transparent; color: var(--muted);
}
.assistant-actions .icon-action-btn:hover { border-color: var(--line); color: var(--ink); background: var(--strip-bg); }
.assistant-model { margin-top: 9px; color: var(--muted); font-size: 12px; }
.model-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--ok); }
.model-settings { margin-left: auto; color: var(--ink-secondary); white-space: nowrap; }
.assistant-timeline { flex: 1; min-height: 0; overflow: auto; padding: 20px; }
.assistant-empty { padding: 26px 0; color: var(--muted); }
.assistant-empty h2 { color: var(--ink); margin: 8px 0; font-size: 18px; }
.assistant-empty p { margin: 8px 0 14px; }
.assistant-message { margin-bottom: 18px; overflow-wrap: anywhere; }
.assistant-message.user { margin-left: auto; width: fit-content; max-width: 90%; padding: 9px 12px; border: 1px solid color-mix(in srgb, var(--brand) 45%, var(--line)); border-radius: 12px 12px 4px 12px; background: color-mix(in srgb, var(--brand) 21%, var(--card-bg)); }
.assistant-byline { margin-bottom: 7px; font-size: 12px; font-weight: 600; }
.assistant-copy { margin: 0; white-space: pre-wrap; font-size: 14px; }
.assistant-note, .assistant-error { margin: 8px 0; font-size: 13px; color: var(--muted); }
.assistant-error { color: var(--bad); }
.assistant-link { width: fit-content; margin-top: 8px; text-decoration: underline; font-size: 13px; }
.assistant-plan { border: 1px solid var(--line); background: var(--card-bg); padding: 12px; border-radius: var(--radius-md); margin-top: 9px; }
.plan-head, .composer-actions { justify-content: space-between; }
.plan-diff { border: 1px solid var(--line); background: var(--strip-bg); border-radius: 7px; margin: 10px 0; padding: 8px 10px; font-size: 12px; }
.plan-diff .old { color: var(--bad); }
.plan-diff .new { color: var(--ok); }
.assistant-footer { padding: 10px 16px max(14px, env(safe-area-inset-bottom)); }
.assistant-suggestions { flex-wrap: wrap; margin-bottom: 8px; gap: 6px; }
.assistant-suggestions .btn { border-radius: var(--radius-pill); font-size: 11px; }
.assistant-composer { border: 1px solid var(--card-border-hover); background: var(--input-bg); border-radius: var(--radius-md); padding: 8px; }
.assistant-composer textarea { width: 100%; padding: 4px; border: 0; background: transparent; color: var(--ink); resize: vertical; min-height: 42px; max-height: 130px; font: inherit; }
.composer-actions span { display: flex; align-items: center; gap: 6px; color: var(--muted); font-size: 11px; }
.composer-actions button { margin-left: auto; }
.assistant-full { border: 1px solid var(--line); border-radius: var(--radius-card); overflow: hidden; min-height: 490px; height: calc(100dvh - var(--topbar-h) - 48px); }
.assistant-full .assistant-timeline, .assistant-full .assistant-footer { width: min(100%, 820px); margin-inline: auto; }
.assistant-full .assistant-heading h1 { font-size: 23px; }
</style>
