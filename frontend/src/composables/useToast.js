import { ref, computed } from 'vue'

const STORAGE_KEY = 'gatehome_notifications_history'
const MAX_NOTIFICATIONS = 60

// Active floating toasts
const toasts = ref([])
let toastIdCounter = 0

// Historical notifications in Notification Center
const notifications = ref(loadStoredNotifications())

const CURSOR_KEY = 'gatehome_notification_cursor'
let cursor
try { cursor = localStorage.getItem(CURSOR_KEY) } catch { /* storage disabled */ }

const faultEventMeta = {
  upstream: { category: 'upstream', path: '/runtime' },
  backup: { category: 'backup', path: '/backups' },
  certificate: { category: 'cert', path: '/certificates', warnType: 'warning' },
  ddns: { category: 'ddns', path: '/ddns' },
  mihomo: { category: 'mihomo', path: '/mihomo' },
  security: { category: 'waf', path: '/logs/ip-blocks', warnType: 'warning' }
}

function mergeFaultHistory(events) {
  if (!events.length || events.at(-1).id === cursor) return
  const start = events.findIndex(event => event.id === cursor) + 1
  const incoming = events.slice(start).map(event => {
    const meta = faultEventMeta[event.type] || { category: 'system', path: null }
    return {
      id: event.id, message: event.message, resource: event.resource,
      type: event.status === 'resolved' ? 'success' : (meta.warnType || 'error'),
      category: meta.category,
      path: meta.path,
      createdAt: event.time, unread: true
    }
  })
  const known = new Set(notifications.value.map(item => item.id))
  notifications.value = [...notifications.value, ...incoming.filter(item => !known.has(item.id))]
    .sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt)).slice(0, MAX_NOTIFICATIONS)
  saveNotifications(notifications.value)
  cursor = events.at(-1).id
  try { localStorage.setItem(CURSOR_KEY, cursor) } catch { /* storage disabled */ }
}

function loadStoredNotifications() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

function saveNotifications(items) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(items.slice(0, MAX_NOTIFICATIONS)))
  } catch {
    // Ignore storage quota or disabled errors
  }
}

function detectCategory(message) {
  if (!message) return 'system'
  if (/拦截|防护|黑名单|封禁|冻结|防火墙|WAF/i.test(message)) return 'waf'
  if (/DDNS|动态域名|DNS|解析/i.test(message)) return 'ddns'
  if (/备份|WebDAV|快照|恢复/i.test(message)) return 'backup'
  if (/证书|SSL|TLS|续期|续签/i.test(message)) return 'cert'
  if (/Mihomo|策略组|规则集/i.test(message)) return 'mihomo'
  if (/反代|上游|代理|端口转发/i.test(message)) return 'upstream'
  if (/MCP|AI/i.test(message)) return 'mcp'
  return 'system'
}

function detectPath(category) {
  switch (category) {
    case 'waf': return '/firewall'
    case 'ddns': return '/ddns'
    case 'backup': return '/backups'
    case 'cert': return '/certificates'
    case 'mihomo': return '/mihomo'
    case 'upstream': return '/runtime'
    case 'mcp': return '/mcp'
    default: return null
  }
}

function shouldPersistNotification(type, options) {
  if (typeof options?.persist === 'boolean') return options.persist
  return type === 'warn' || type === 'warning' || type === 'error'
}

export function useToast() {
  const showToast = (message, type = 'info', duration = 3500, category = null, path = null, options = {}) => {
    if (!message) return null
    const id = ++toastIdCounter
    const cat = category || detectCategory(message)
    const targetPath = path || detectPath(cat)
    const now = new Date()

    // 1. Add to active floating toasts
    const toastItem = {
      id,
      message,
      type,
      category: cat,
      path: targetPath,
      createdAt: now.toISOString(),
      duration,
      remaining: duration,
      startTime: Date.now(),
      timer: null,
      paused: false
    }

    toastItem.timer = setTimeout(() => {
      removeToast(id)
    }, duration)

    toasts.value.push(toastItem)

    // 2. Persist only warnings, errors, or explicit notifications into Notification Center
    if (shouldPersistNotification(type, options)) {
      const notificationItem = {
        id: `n_${id}_${Date.now()}`,
        message,
        type,
        category: cat,
        path: targetPath,
        createdAt: now.toISOString(),
        unread: true
      }

      notifications.value.unshift(notificationItem)
      if (notifications.value.length > MAX_NOTIFICATIONS) {
        notifications.value = notifications.value.slice(0, MAX_NOTIFICATIONS)
      }
      saveNotifications(notifications.value)
    }

    return id
  }

  const removeToast = (id) => {
    const idx = toasts.value.findIndex(t => t.id === id)
    if (idx !== -1) {
      if (toasts.value[idx].timer) clearTimeout(toasts.value[idx].timer)
      toasts.value.splice(idx, 1)
    }
  }

  const pauseToast = (id) => {
    const item = toasts.value.find(t => t.id === id)
    if (item && !item.paused) {
      clearTimeout(item.timer)
      item.remaining -= (Date.now() - item.startTime)
      if (item.remaining < 500) item.remaining = 500
      item.paused = true
    }
  }

  const resumeToast = (id) => {
    const item = toasts.value.find(t => t.id === id)
    if (item && item.paused) {
      item.paused = false
      item.startTime = Date.now()
      item.timer = setTimeout(() => {
        removeToast(id)
      }, item.remaining)
    }
  }

  // Notification Center controls
  const markAllAsRead = () => {
    notifications.value.forEach(item => { item.unread = false })
    saveNotifications(notifications.value)
  }

  const markAsRead = (id) => {
    const item = notifications.value.find(n => n.id === id)
    if (item) {
      item.unread = false
      saveNotifications(notifications.value)
    }
  }

  const removeNotification = (id) => {
    notifications.value = notifications.value.filter(n => n.id !== id)
    saveNotifications(notifications.value)
  }

  const clearAllNotifications = () => {
    notifications.value = []
    saveNotifications([])
  }

  const unreadCount = computed(() => {
    return notifications.value.filter(n => n.unread).length
  })

  return {
    toasts,
    notifications,
    mergeFaultHistory,
    unreadCount,
    showToast,
    removeToast,
    pauseToast,
    resumeToast,
    markAllAsRead,
    markAsRead,
    removeNotification,
    clearAllNotifications,
    success: (msg, cat, p, opts) => showToast(msg, 'success', 3500, cat, p, opts),
    error: (msg, cat, p, opts) => showToast(msg, 'error', 4500, cat, p, opts),
    info: (msg, cat, p, opts) => showToast(msg, 'info', 3500, cat, p, opts),
    warn: (msg, cat, p, opts) => showToast(msg, 'warn', 4000, cat, p, opts)
  }
}
