import { computed, ref, shallowRef } from 'vue'
import { request } from '@/api/client'
import { useGatewayStatus } from '@/composables/useGatewayStatus'

const panelOpen = ref(false)
const messages = ref([])
const settings = shallowRef(null)
const sending = ref(false)
const applying = ref(false)
const error = ref('')
const draft = ref('')
const conversations = ref([])
const conversation = ref(null)
const modelChoice = ref(null)
const effortChoice = ref(null)
const providers = computed(() => (settings.value?.providers ?? []).filter(row => row.base_url))
const provider = computed(() => providers.value.find(row => row.id === modelChoice.value?.provider) ?? providers.value[0])
const model = computed(() => provider.value?.models?.includes(modelChoice.value?.model) ? modelChoice.value.model : provider.value?.model)
const effort = computed(() => effortChoice.value ?? provider.value?.effort ?? '')
const historyKey = 'gatehome.assistant.history.v1'
let historyLoaded = false
let session = new AbortController()

function readHistory() {
  try {
    const entries = JSON.parse(localStorage.getItem(historyKey) || '[]')
    return Array.isArray(entries) ? entries.filter(row => row?.id && Number.isFinite(row.updated) && Array.isArray(row.messages)) : []
  } catch {
    return null
  }
}

function updateHistory(entry, remove = false) {
  const rows = (readHistory() ?? conversations.value).filter(row => row.id !== entry.id)
  if (!remove) rows.push(entry)
  rows.sort((a, b) => b.updated - a.updated)
  const bounded = rows.slice(0, 24)
  while (bounded.length > 1 && JSON.stringify(bounded).length > 1200000) bounded.pop()
  conversations.value = bounded
  try { localStorage.setItem(historyKey, JSON.stringify(bounded)) } catch {
    // Keep history available for this tab when browser persistence is unavailable.
  }
}

function persistConversation() {
  if (!conversation.value || !messages.value.length) return
  const history = messages.value.slice(-40).map(({ changeError, fresh, ...message }) => ({
    ...message, ...(message.change && !message.outcome ? { outcome: '请重新发起修改' } : {})
  }))
  updateHistory({ ...conversation.value, model: model.value, updated: Date.now(), messages: history })
}

function openConversation(row) {
  if (sending.value || applying.value) return
  clearConversation()
  conversation.value = { id: row.id, binding: row.binding, provider: row.provider, title: row.title }
  messages.value = row.messages.map(message => ({ ...message }))
  modelChoice.value = { provider: row.provider, model: row.model }
  effortChoice.value = null
}

function removeConversation(row) {
  if (sending.value || applying.value) return
  if (conversation.value?.id === row.id) clearConversation()
  updateHistory(row, true)
}

function selectModel(providerID, modelID) {
  if (sending.value || applying.value) return
  const next = providers.value.find(row => row.id === providerID)
  if (!next || !next.models.includes(modelID)) return
  if (conversation.value && conversation.value.binding !== next.binding) {
    const currentDraft = draft.value
    clearConversation()
    draft.value = currentDraft
  }
  if (provider.value?.id !== providerID) effortChoice.value = null
  modelChoice.value = { provider: providerID, model: modelID }
}

async function loadSettings() {
  const { signal } = session
  const result = await request('/api/assistant/settings', { signal })
  if (!signal.aborted) {
    settings.value = result
    if (!historyLoaded) {
      conversations.value = readHistory() ?? []
      historyLoaded = true
    }
  }
  return result
}

async function saveSettings(body) {
  const { signal } = session
  const result = await request('/api/assistant/settings', { method: 'PUT', body, signal })
  if (!signal.aborted) settings.value = result
  await useGatewayStatus().refreshConfig(signal).catch(() => {})
  return result
}

function testSettings(body) {
  return request('/api/assistant/test', { method: 'POST', body, signal: session.signal })
}

function listModels(body) {
  return request('/api/assistant/models', { method: 'POST', body, signal: session.signal })
}

async function send(content, page) {
  content = content.trim()
  const selected = provider.value
  if (!content || sending.value || applying.value || !selected) return
  if (conversation.value && conversation.value.binding !== selected.binding) {
    error.value = '原接入点已变化。请新建对话，或切换到其他接入点。'
    return
  }
  if (!conversation.value) {
    conversation.value = {
      id: globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`,
      binding: selected.binding, provider: selected.id, title: content.replace(/\s+/g, ' ').slice(0, 48)
    }
  }
  draft.value = ''
  messages.value.push({ role: 'user', content })
  persistConversation()
  const { signal } = session
  const modelID = model.value
  sending.value = true
  error.value = ''
  try {
    const encoder = new TextEncoder()
    const history = messages.value.slice(-13).map(({ role, content, change, outcome }) => {
      const details = change ? `\n配置建议：${change.name}，原值 ${change.before}，新值 ${change.after}` : ''
      const text = content + details + (outcome ? `\n操作结果：${outcome}` : '')
      return { role, content: encoder.encode(text).length <= 20000 ? text : content }
    })
    // Keep requests below the server's JSON body limit even with long earlier replies.
    while (new TextEncoder().encode(JSON.stringify(history)).length > 112000) history.shift()
    const reply = await request('/api/assistant/chat', {
      method: 'POST', body: {
        messages: history, page, provider: selected.id, binding: selected.binding, model: modelID, effort: effort.value
      }, signal
    })
    if (!signal.aborted) {
      messages.value.push({ ...reply, role: 'assistant', content: reply.text, model: modelID, fresh: true })
      persistConversation()
    }
  } catch (err) {
    if (!signal.aborted) {
      error.value = err.message
      if (err.status === 409) await loadSettings().catch(() => {})
    }
  } finally {
    if (!signal.aborted) sending.value = false
  }
}

async function apply(message) {
  if (applying.value || message.outcome) return
  const { signal } = session
  const { refreshConfig, saveConfig, refreshStatus } = useGatewayStatus()
  const change = message.change
  applying.value = true
  message.changeError = ''
  try {
    const snapshot = await refreshConfig(signal)
    signal.throwIfAborted()
    if (snapshot.revision !== change.revision) {
      message.outcome = '配置已变化，请重新发起修改'
      return
    }
    const config = { ...snapshot.config }
    if (change.field === 'health_interval') {
      config.service_health = { enabled: config.service_health?.interval ? config.service_health.enabled : true, interval: Number(change.after) }
    } else if (change.field === 'upstream' && config.routes.some(route => route.id === change.id && route.upstream === change.before)) {
      config.routes = config.routes.map(route => route.id === change.id ? { ...route, upstream: change.after } : route)
    } else {
      throw new Error('变更目标已失效，请重新发起修改')
    }
    await saveConfig(snapshot, config)
    if (!signal.aborted) {
      message.outcome = '已应用'
      refreshStatus(signal).catch(() => {})
    }
  } catch (err) {
    if (!signal.aborted) message.changeError = err.message
  } finally {
    if (!signal.aborted) {
      applying.value = false
      if (message.outcome) persistConversation()
    }
  }
}

function cancelChange(message) {
  message.outcome = '已取消'
  persistConversation()
}

function clearConversation() {
  messages.value = []
  conversation.value = null
  draft.value = ''
  error.value = ''
}

function resetAssistant() {
  session.abort()
  session = new AbortController()
  clearConversation()
  panelOpen.value = false
  settings.value = null
  conversations.value = []
  modelChoice.value = null
  effortChoice.value = null
  historyLoaded = false
  sending.value = false
  applying.value = false
}

export function useAssistant() {
  return { panelOpen, draft, messages, conversations, conversation, settings, providers, provider, model, effort,
    effortChoice, sending, applying, error, loadSettings, saveSettings, testSettings, listModels, send, apply,
    clearConversation, cancelChange, openConversation, removeConversation, selectModel, resetAssistant }
}
