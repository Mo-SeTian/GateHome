import { ref, shallowRef } from 'vue'
import { request } from '@/api/client'
import { useGatewayStatus } from '@/composables/useGatewayStatus'

const panelOpen = ref(false)
const messages = ref([])
const settings = shallowRef(null)
const sending = ref(false)
const applying = ref(false)
const error = ref('')
let session = new AbortController()

async function loadSettings() {
  const { signal } = session
  const result = await request('/api/assistant/settings', { signal })
  if (!signal.aborted) settings.value = result
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

async function send(content, page) {
  content = content.trim()
  if (!content || sending.value || !settings.value?.base_url) return
  messages.value.push({ role: 'user', content })
  const { signal } = session
  sending.value = true
  error.value = ''
  try {
    const history = messages.value.slice(-13).map(({ role, content, outcome }) => ({
      role,
      content: outcome ? `${content}\n操作结果：${outcome}` : content
    }))
    // Keep requests below the server's JSON body limit even with long earlier replies.
    while (new TextEncoder().encode(JSON.stringify(history)).length > 112000) history.shift()
    const reply = await request('/api/assistant/chat', {
      method: 'POST', body: { messages: history, page }, signal
    })
    if (!signal.aborted) messages.value.push({ ...reply, role: 'assistant', content: reply.text })
  } catch (err) {
    if (!signal.aborted) error.value = err.message
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
    if (!signal.aborted) applying.value = false
  }
}

function clearConversation() {
  messages.value = []
  error.value = ''
}

function resetAssistant() {
  session.abort()
  session = new AbortController()
  clearConversation()
  panelOpen.value = false
  settings.value = null
  sending.value = false
  applying.value = false
}

export function useAssistant() {
  return { panelOpen, messages, settings, sending, applying, error, loadSettings, saveSettings, testSettings, send, apply, clearConversation, resetAssistant }
}
