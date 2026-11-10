import { ref, computed } from 'vue'

const STORAGE_KEY = 'gatehome_notifications_history'
const MAX_NOTIFICATIONS = 60

// Active floating toasts
const toasts = ref([])
let toastIdCounter = 0

// Historical notifications in Notification Center
const notifications = ref(loadStoredNotifications())

// Notification Center drawer visibility
const isCenterOpen = ref(false)

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
  if (/MCP|AI/i.test(message)) return 'mcp'
  return 'system'
}

function detectPath(category) {
  switch (category) {
    case 'waf': return '/firewall'
    case 'ddns': return '/ddns'
    case 'backup': return '/backups'
    case 'cert': return '/certificates'
    case 'mcp': return '/mcp'
    default: return null
  }
}

export function useToast() {
  const showToast = (message, type = 'info', duration = 3500, category = null, path = null) => {
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

    // 2. Add to Notification Center history
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
  const openCenter = () => { isCenterOpen.value = true }
  const closeCenter = () => { isCenterOpen.value = false }
  const toggleCenter = () => { isCenterOpen.value = !isCenterOpen.value }

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
    isCenterOpen,
    unreadCount,
    showToast,
    removeToast,
    pauseToast,
    resumeToast,
    openCenter,
    closeCenter,
    toggleCenter,
    markAllAsRead,
    markAsRead,
    removeNotification,
    clearAllNotifications,
    success: (msg, cat, p) => showToast(msg, 'success', 3500, cat, p),
    error: (msg, cat, p) => showToast(msg, 'error', 4500, cat, p),
    info: (msg, cat, p) => showToast(msg, 'info', 3500, cat, p),
    warn: (msg, cat, p) => showToast(msg, 'warn', 4000, cat, p)
  }
}
