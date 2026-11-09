<template>
  <transition name="cmdk-fade">
    <div v-if="isCmdkOpen" class="cmdk-backdrop" @click.self="closeCmdk">
      <div class="cmdk-dialog">
        <!-- Search Input Bar -->
        <div class="cmdk-input-wrapper">
          <Search class="cmdk-search-icon" :size="18" />
          <input 
            ref="inputRef"
            v-model="query"
            type="text"
            placeholder="搜索反代服务、路由、防火墙规则或快速操作..."
            class="cmdk-input"
            @keydown.down.prevent="navigateList(1)"
            @keydown.up.prevent="navigateList(-1)"
            @keydown.enter.prevent="selectActive"
            @keydown.esc.prevent="closeCmdk"
          />
          <kbd class="cmdk-esc-badge" @click="closeCmdk">ESC</kbd>
        </div>

        <!-- Filtered Results List -->
        <div class="cmdk-results">
          <!-- Views Section -->
          <div v-if="filteredPages.length" class="cmdk-group">
            <div class="cmdk-group-title">快捷页面导航</div>
            <div 
              v-for="(item, idx) in filteredPages" 
              :key="item.path"
              class="cmdk-item"
              :class="{ active: selectedIndex === idx }"
              @mouseenter="selectedIndex = idx"
              @click="handleSelectPage(item.path)"
            >
              <component :is="item.icon" :size="16" class="cmdk-item-icon" />
              <div class="cmdk-item-label">{{ item.title }}</div>
              <span class="cmdk-item-meta">{{ item.path }}</span>
            </div>
          </div>

          <!-- Quick Actions Section -->
          <div v-if="filteredActions.length" class="cmdk-group">
            <div class="cmdk-group-title">常用操作指令</div>
            <div 
              v-for="(action, idx) in filteredActions" 
              :key="action.id"
              class="cmdk-item"
              :class="{ active: selectedIndex === filteredPages.length + idx }"
              @mouseenter="selectedIndex = filteredPages.length + idx"
              @click="handleSelectAction(action)"
            >
              <component :is="action.icon" :size="16" class="cmdk-item-icon action" />
              <div class="cmdk-item-label">{{ action.title }}</div>
              <span class="cmdk-item-badge">{{ action.shortcut || 'Action' }}</span>
            </div>
          </div>

          <!-- Empty State -->
          <div v-if="!filteredPages.length && !filteredActions.length" class="cmdk-empty">
            <AlertCircle :size="24" class="cmdk-empty-icon" />
            <div>没有找到匹配 "{{ query }}" 的结果</div>
          </div>
        </div>

        <!-- Footer -->
        <div class="cmdk-footer">
          <span><kbd>↑</kbd> <kbd>↓</kbd> 切换选项</span>
          <span><kbd>↵</kbd> 确认执行</span>
          <span><kbd>ESC</kbd> 关闭面板</span>
        </div>
      </div>
    </div>
  </transition>
</template>

<script setup>
import { ref, computed, nextTick, watch, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/composables/useAppStore'
import { useLogout, useSession } from '@/composables/useSession'
import { useToast } from '@/composables/useToast'
import { 
  Search, AlertCircle, LayoutDashboard, Network, ArrowLeftRight, Compass,
  Globe, Lock, Download, ShieldAlert, FileSearch, ScrollText, Ban,
  BarChart3, Activity, Bot, RefreshCw, HardDriveDownload, Settings, Moon, Sun, PlusCircle,
  KeyRound, LogOut
} from 'lucide-vue-next'

const router = useRouter()
const { isCmdkOpen, closeCmdk, toggleTheme, theme } = useAppStore()
const { openAccountModal } = useSession()
const logout = useLogout()
const { success } = useToast()

const inputRef = ref(null)
const query = ref('')
const selectedIndex = ref(0)

const pages = [
  // 分组 1: 工作空间
  { title: '概览 (Overview)', path: '/overview', icon: LayoutDashboard },
  { title: '反向代理 (Routes)', path: '/routes', icon: Network },
  { title: '端口转发 (Forwards)', path: '/forwards', icon: ArrowLeftRight },
  { title: '浏览器首页 (SunPanel)', path: '/sunpanel', icon: Compass },
  { title: '动态域名 (DDNS)', path: '/ddns', icon: Globe },
  { title: 'SSL 证书 (Certificates)', path: '/certificates', icon: Lock },
  { title: '订阅管理 (Subscriptions)', path: '/subscriptions', icon: Download },
  { title: '防火墙管理 (Firewall)', path: '/access', icon: ShieldAlert },

  // 分组 2: 观察与管理
  { title: '防火墙拦截记录 (Firewall Logs)', path: '/firewall-logs', icon: FileSearch },
  { title: '日志 (Logs)', path: '/logs', icon: ScrollText },
  { title: 'IP 拦截名单 (IP Blocks)', path: '/ip-blocks', icon: Ban },
  { title: '统计 (Statistics)', path: '/statistics', icon: BarChart3 },
  { title: '流量统计 (Traffic)', path: '/traffic', icon: Activity },
  { title: 'AI 管理 (MCP)', path: '/mcp', icon: Bot },
  { title: '更新 (Updates)', path: '/updates', icon: RefreshCw },
  { title: '备份与恢复 (Backups)', path: '/backups', icon: HardDriveDownload },
  { title: '设置 (Settings)', path: '/settings', icon: Settings }
]

const actions = [
  { id: 'act_theme', title: '切换 深色 / 浅色 主题', icon: Moon, run: () => { toggleTheme(); success(`已切换为 ${theme.value === 'dark' ? 'Obsidian 深色' : 'VisionOS 浅色'} 主题`) } },
  { id: 'act_account', title: '修改管理员账户密码', icon: KeyRound, run: () => { closeCmdk(); openAccountModal() } },
  { id: 'act_new_route', title: '新建反向代理路由', icon: PlusCircle, shortcut: '⌘N', run: () => { closeCmdk(); router.push('/routes'); success('已打开新建反代弹窗') } },
  { id: 'act_ddns_sync', title: '立即同步全部 DDNS 动态域名', icon: RefreshCw, run: () => { success('已向各 DNS 服务商发出刷新同步指令') } },
  { id: 'act_logout', title: '退出登录 (Logout)', icon: LogOut, run: () => { closeCmdk(); return logout() } }
]

const filteredPages = computed(() => {
  if (!query.value.trim()) return pages
  const q = query.value.toLowerCase()
  return pages.filter(p => p.title.toLowerCase().includes(q) || p.path.toLowerCase().includes(q))
})

const filteredActions = computed(() => {
  if (!query.value.trim()) return actions
  const q = query.value.toLowerCase()
  return actions.filter(a => a.title.toLowerCase().includes(q))
})

const totalItemsCount = computed(() => filteredPages.value.length + filteredActions.value.length)

watch(isCmdkOpen, (open) => {
  if (open) {
    query.value = ''
    selectedIndex.value = 0
    nextTick(() => {
      inputRef.value?.focus()
    })
  }
})

const navigateList = (direction) => {
  if (totalItemsCount.value === 0) return
  selectedIndex.value = (selectedIndex.value + direction + totalItemsCount.value) % totalItemsCount.value
}

const selectActive = () => {
  const pCount = filteredPages.value.length
  if (selectedIndex.value < pCount) {
    handleSelectPage(filteredPages.value[selectedIndex.value].path)
  } else {
    const actIdx = selectedIndex.value - pCount
    if (filteredActions.value[actIdx]) {
      handleSelectAction(filteredActions.value[actIdx])
    }
  }
}

const handleSelectPage = (path) => {
  router.push(path)
  closeCmdk()
}

const handleSelectAction = (action) => {
  action.run()
  closeCmdk()
}

// Global ⌘K / Ctrl+K listener
const handleGlobalKeydown = (e) => {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
    e.preventDefault()
    if (isCmdkOpen.value) {
      closeCmdk()
    } else {
      isCmdkOpen.value = true
    }
  }
}

onMounted(() => {
  window.addEventListener('keydown', handleGlobalKeydown)
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleGlobalKeydown)
})
</script>

<style scoped>
.cmdk-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.65);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  z-index: 999;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding-top: 12vh;
}

.cmdk-dialog {
  width: 90%;
  max-width: 620px;
  background: var(--card-bg);
  border: 1px solid var(--card-border-hover);
  border-radius: var(--radius-card);
  box-shadow: 0 25px 60px rgba(0, 0, 0, 0.6), var(--inner-highlight);
  backdrop-filter: blur(30px);
  -webkit-backdrop-filter: blur(30px);
  overflow: hidden;
  animation: cmdkPop 0.2s cubic-bezier(0.16, 1, 0.3, 1);
}

@keyframes cmdkPop {
  from { opacity: 0; transform: scale(0.97) translateY(-10px); }
  to { opacity: 1; transform: scale(1) translateY(0); }
}

.cmdk-input-wrapper {
  display: flex;
  align-items: center;
  padding: 14px 18px;
  border-bottom: 1px solid var(--line);
  gap: 12px;
}

.cmdk-search-icon {
  color: var(--brand-light);
  flex-shrink: 0;
}

.cmdk-input {
  flex: 1;
  background: transparent;
  border: none;
  font-size: 14px;
  color: var(--ink);
}
.cmdk-input::placeholder {
  color: var(--faint);
}

.cmdk-esc-badge {
  background: var(--strip-bg);
  border: 1px solid var(--line);
  border-radius: 4px;
  font-size: 10px;
  padding: 2px 6px;
  color: var(--muted);
  cursor: pointer;
}

.cmdk-results {
  max-height: 380px;
  overflow-y: auto;
  padding: 8px;
}

.cmdk-group {
  margin-bottom: 8px;
}

.cmdk-group-title {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  color: var(--faint);
  padding: 6px 10px 4px 10px;
  letter-spacing: 0.5px;
}

.cmdk-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 9px 12px;
  border-radius: var(--radius-sm);
  color: var(--ink-secondary);
  font-size: 13px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.cmdk-item.active, .cmdk-item:hover {
  background: rgba(99, 102, 241, 0.15);
  color: #ffffff;
}

.cmdk-item-icon {
  color: var(--muted);
}
.cmdk-item.active .cmdk-item-icon {
  color: var(--brand-light);
}
.cmdk-item-icon.action {
  color: var(--brand-cyan);
}

.cmdk-item-label {
  flex: 1;
}

.cmdk-item-meta {
  font-family: var(--mono);
  font-size: 11px;
  color: var(--faint);
}

.cmdk-item-badge {
  font-size: 10px;
  padding: 2px 6px;
  border-radius: 4px;
  background: var(--strip-bg);
  color: var(--muted);
}

.cmdk-empty {
  padding: 36px 20px;
  text-align: center;
  color: var(--muted);
  font-size: 13px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
}
.cmdk-empty-icon {
  color: var(--faint);
}

.cmdk-footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 14px;
  padding: 8px 18px;
  background: var(--strip-bg);
  border-top: 1px solid var(--line);
  font-size: 11px;
  color: var(--faint);
}
.cmdk-footer kbd {
  background: rgba(148, 163, 184, 0.15);
  padding: 1px 4px;
  border-radius: 3px;
  font-family: var(--mono);
}

.cmdk-fade-enter-active,
.cmdk-fade-leave-active {
  transition: opacity 0.2s ease;
}
.cmdk-fade-enter-from,
.cmdk-fade-leave-to {
  opacity: 0;
}
</style>
