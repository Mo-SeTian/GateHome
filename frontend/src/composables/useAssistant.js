import { computed, ref, shallowRef } from 'vue'
import { request, requestStream } from '@/api/client'
import { useGatewayStatus } from '@/composables/useGatewayStatus'

const maxConversationMessages = 40
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
let turnController = null

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
  if (messages.value.length > maxConversationMessages) {
    messages.value = messages.value.slice(-maxConversationMessages)
  }
  const history = messages.value.slice(-maxConversationMessages).map(({ changeError, fresh, streaming, ...message }) => ({
    ...message, ...(message.change && !message.outcome ? { outcome: '请重新发起修改' } : {})
  }))
  updateHistory({ ...conversation.value, model: model.value, updated: Date.now(), messages: history })
}

function openConversation(row) {
  if (sending.value || applying.value) return
  clearConversation()
  conversation.value = { id: row.id, binding: row.binding, provider: row.provider, title: row.title }
  messages.value = row.messages.slice(-maxConversationMessages).map(message => ({ ...message }))
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

function stopGeneration() {
  turnController?.abort()
}

async function send(content, page) {
  content = content.trim()
  const selected = provider.value
  if (!content || sending.value || applying.value || !selected) return
  if (conversation.value && conversation.value.binding !== selected.binding) {
    error.value = '原接入点已变化。请新建对话，或切换到其他接入点。'
    return
  }
  const startedNew = !conversation.value
  if (startedNew) {
    conversation.value = {
      id: globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`,
      binding: selected.binding, provider: selected.id, title: content.replace(/\s+/g, ' ').slice(0, 48)
    }
  }
  const encoder = new TextEncoder()
  const priorHistory = messages.value.slice(-12).map(({ role, content: textBody, change, outcome }) => {
    const details = change ? `\n配置建议：${change.name}，原值 ${change.before}，新值 ${change.after}` : ''
    const text = textBody + details + (outcome ? `\n操作结果：${outcome}` : '')
    return { role, content: encoder.encode(text).length <= 20000 ? text : textBody }
  })
  const history = [...priorHistory, { role: 'user', content }]
  // Keep requests below the server's JSON body limit even with long earlier replies.
  while (history.length > 1 && encoder.encode(JSON.stringify(history)).length > 112000) history.shift()

  draft.value = ''
  messages.value.push({ role: 'user', content })
  const placeholder = { role: 'assistant', content: '', model: model.value, streaming: true, fresh: false }
  messages.value.push(placeholder)
  if (messages.value.length > maxConversationMessages) {
    messages.value = messages.value.slice(-maxConversationMessages)
  }
  const assistantMessage = messages.value[messages.value.length - 1]

  const sessionSignal = session.signal
  const currentTurn = new AbortController()
  turnController = currentTurn
  const abortTurn = () => currentTurn.abort()
  sessionSignal.addEventListener('abort', abortTurn, { once: true })

  const modelID = model.value
  sending.value = true
  error.value = ''
  let completed = false
  try {
    await requestStream('/api/assistant/chat', {
      method: 'POST',
      body: {
        messages: history, page, provider: selected.id, binding: selected.binding,
        model: modelID, effort: effort.value, stream: true
      },
      signal: currentTurn.signal,
      onEvent(eventName, payload) {
        if (currentTurn.signal.aborted || sessionSignal.aborted) return
        if (eventName === 'delta' && payload?.text) {
          assistantMessage.content += payload.text
        } else if (eventName === 'done' && payload) {
          completed = true
          if (payload.text) assistantMessage.content = payload.text
          if (payload.link) assistantMessage.link = payload.link
          if (payload.change) assistantMessage.change = payload.change
          if (payload.hint) assistantMessage.hint = payload.hint
          assistantMessage.streaming = false
          assistantMessage.fresh = true
          persistConversation()
          if (turnController === currentTurn) sending.value = false
        }
      }
    })
    if (!sessionSignal.aborted && !currentTurn.signal.aborted && !completed) {
      if (!assistantMessage.content.trim()) {
        throw new Error('模型回复为空，请重试')
      }
      assistantMessage.streaming = false
      persistConversation()
    }
  } catch (err) {
    if (!sessionSignal.aborted && !completed) {
      assistantMessage.streaming = false
      const hasPartial = assistantMessage.content.trim().length > 0
      const removeUnanswered = () => {
        const idx = messages.value.indexOf(assistantMessage)
        if (idx > 0 && messages.value[idx - 1]?.role === 'user') messages.value.splice(idx - 1, 2)
        else if (idx >= 0) messages.value.splice(idx, 1)
        if (startedNew && messages.value.length === 0) conversation.value = null
        if (!draft.value) draft.value = content
      }
      if (err.name === 'AbortError') {
        if (hasPartial) {
          persistConversation()
        } else {
          removeUnanswered()
        }
      } else {
        if (hasPartial) {
          persistConversation()
        } else {
          removeUnanswered()
        }
        error.value = err.message
        if (err.status === 409) await loadSettings().catch(() => {})
      }
    }
  } finally {
    sessionSignal.removeEventListener('abort', abortTurn)
    if (turnController === currentTurn) {
      turnController = null
      if (!sessionSignal.aborted) sending.value = false
    }
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
      config.service_health = {
        ...config.service_health,
        enabled: config.service_health?.interval ? config.service_health.enabled : true,
        interval: Number(change.after)
      }
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
  turnController?.abort()
  messages.value = []
  conversation.value = null
  draft.value = ''
  error.value = ''
}

function clearAssistantHistory() {
  try {
    localStorage.removeItem(historyKey)
  } catch {
    // Ignore storage failures when unavailable.
  }
}

function resetAssistant({ clearStorage = false } = {}) {
  turnController?.abort()
  session.abort()
  session = new AbortController()
  clearConversation()
  if (clearStorage) clearAssistantHistory()
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
    effortChoice, sending, applying, error, loadSettings, saveSettings, testSettings, listModels, send, stopGeneration, apply,
    clearConversation, cancelChange, openConversation, removeConversation, selectModel, resetAssistant, clearAssistantHistory }
}
