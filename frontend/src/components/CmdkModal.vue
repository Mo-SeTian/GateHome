<template>
  <dialog
    ref="dialogRef"
    class="cmdk-backdrop"
    aria-label="全局搜索与命令面板"
    @click.self="closeCmdk"
    @cancel.prevent="closeCmdk"
    @close="closeCmdk"
    @keydown.esc.prevent="closeCmdk"
  >
    <div class="cmdk-dialog">
      <!-- Search Input Bar -->
      <div class="cmdk-input-wrapper">
        <Search class="cmdk-search-icon" :size="18" aria-hidden="true" />
        <input
          ref="inputRef"
          v-model="query"
          type="text"
          placeholder="搜索页面、反代服务、域名、端口转发、防火墙或操作指令…"
          class="cmdk-input"
          role="combobox"
          aria-autocomplete="list"
          aria-expanded="true"
          aria-controls="cmdk-results"
          :aria-activedescendant="totalItemsCount ? `cmdk-option-${selectedIndex}` : undefined"
          @keydown.down.prevent="navigateList(1)"
          @keydown.up.prevent="navigateList(-1)"
          @keydown.enter.prevent="selectActive"
        />
        <button
          v-if="query"
          type="button"
          class="cmdk-clear-btn"
          aria-label="清空搜索关键词"
          @click="clearQuery"
        >
          清空
        </button>
        <button type="button" class="cmdk-esc-badge" aria-label="关闭命令面板" @click="closeCmdk">ESC</button>
      </div>

      <!-- Category Scope Filter Pills -->
      <div class="cmdk-categories" role="group" aria-label="搜索分类筛选">
        <button
          v-for="cat in SEARCH_CATEGORIES"
          :key="cat.id"
          type="button"
          class="cmdk-cat-btn"
          :class="{ active: selectedCategory === cat.id }"
          :aria-pressed="selectedCategory === cat.id"
          @click="selectCategory(cat.id)"
        >
          <span>{{ cat.label }}</span>
          <span v-if="query.trim() || cat.id !== 'all'" class="cmdk-cat-count mono">
            {{ searchResult.counts[cat.id] || 0 }}
          </span>
        </button>
      </div>

      <!-- Filtered Results List -->
      <div id="cmdk-results" class="cmdk-results" role="listbox" aria-label="全局搜索结果">
        <div v-for="group in searchResult.groups" :key="group.key" class="cmdk-group">
          <div class="cmdk-group-title">
            <span>{{ group.title }}</span>
            <span class="cmdk-group-count mono">{{ group.items.length }}</span>
          </div>
          <div
            v-for="item in group.items"
            :key="item.id"
            :id="`cmdk-option-${item.flatIndex}`"
            class="cmdk-item"
            role="option"
            :aria-selected="selectedIndex === item.flatIndex"
            :class="{ active: selectedIndex === item.flatIndex }"
            @mouseenter="selectedIndex = item.flatIndex"
            @click="handleSelectItem(item)"
          >
            <component
              :is="resolveItemIcon(item)"
              :size="16"
              class="cmdk-item-icon"
              :class="[item.kind, item.tone]"
              aria-hidden="true"
            />
            <div class="cmdk-item-body">
              <div class="cmdk-item-label">{{ item.title }}</div>
              <div v-if="item.subtitle" class="cmdk-item-sub">{{ item.subtitle }}</div>
            </div>
            <span v-if="item.meta" class="cmdk-item-meta">{{ item.meta }}</span>
            <span v-if="item.badge" class="cmdk-item-badge" :class="`tone-${item.tone}`">
              {{ item.badge }}
            </span>
          </div>
        </div>

        <!-- Empty State -->
        <div v-if="!totalItemsCount" class="cmdk-empty">
          <AlertCircle :size="24" class="cmdk-empty-icon" aria-hidden="true" />
          <div>
            没有找到匹配 <strong v-if="query.trim()">"{{ query }}"</strong> 的{{ currentCategoryLabel }}结果
          </div>
          <button
            v-if="selectedCategory !== 'all'"
            type="button"
            class="cmdk-reset-cat-btn"
            @click="selectCategory('all')"
          >
            切换到「全部」分类查看 ({{ searchResult.counts.all }} 项匹配)
          </button>
        </div>
      </div>

      <!-- Footer -->
      <div class="cmdk-footer">
        <span class="cmdk-footer-stat mono">共 {{ totalItemsCount }} 项结果</span>
        <div class="cmdk-footer-hints">
          <span><kbd>↑</kbd> <kbd>↓</kbd> 切换选项</span>
          <span><kbd>↵</kbd> 确认执行</span>
          <span><kbd>ESC</kbd> 关闭面板</span>
        </div>
      </div>
    </div>
  </dialog>
</template>

<script setup>
import { ref, computed, nextTick, watch, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/composables/useAppStore'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useLogout, useSession } from '@/composables/useSession'
import { useToast } from '@/composables/useToast'
import { adminApi } from '@/api/admin'
import { SEARCH_CATEGORIES, buildSearchItems, searchGlobalItems } from '@/api/globalSearch'
import {
  Search,
  AlertCircle,
  LayoutDashboard,
  Network,
  Images,
  ArrowLeftRight,
  Compass,
  Globe,
  Lock,
  Download,
  ShieldAlert,
  FileSearch,
  ScrollText,
  Ban,
  Bell,
  BarChart3,
  Activity,
  Bot,
  RefreshCw,
  HardDriveDownload,
  Settings,
  Moon,
  PlusCircle,
  KeyRound,
  LogOut
} from 'lucide-vue-next'

const router = useRouter()
const { isCmdkOpen, closeCmdk, toggleTheme, theme } = useAppStore()
const { gatewayConfig, refreshConfig } = useGatewayStatus()
const { openAccountModal } = useSession()
const logout = useLogout()
const toast = useToast()
const { success, notifications } = toast
const syncingDDNS = ref(false)

async function syncDDNS() {
  if (syncingDDNS.value) return
  syncingDDNS.value = true
  try {
    const result = await adminApi.runJobs('ddns')
    toast.info(result.message || '同步任务已加入队列，请在动态域名页面查看执行结果')
  } catch (err) {
    toast.error(err.message)
  } finally {
    syncingDDNS.value = false
  }
}

const inputRef = ref(null)
const dialogRef = ref(null)
const query = ref('')
const selectedCategory = ref('all')
const selectedIndex = ref(0)

const pages = [
  // 分组 1: 工作空间
  {
    title: '概览 (Overview)',
    subtitle: '网关大盘、流量摘要与服务快捷入口',
    path: '/overview',
    icon: LayoutDashboard,
    keywords: ['首页', '大盘', 'dashboard', 'home', '概览']
  },
  {
    title: '反向代理 (Routes)',
    subtitle: '管理 HTTP/HTTPS 域名反代路由、反代组与健康检查',
    path: '/routes',
    icon: Network,
    keywords: ['反代', '路由', '域名', 'proxy', 'routes', 'upstream']
  },
  {
    title: '图标库 (Icons)',
    subtitle: '管理内置、GitHub HD-Icons 导入与本地上传图标',
    path: '/icons',
    icon: Images,
    keywords: ['图标', 'logo', 'hd-icons', 'image', 'icons']
  },
  {
    title: '端口转发 (Forwards)',
    subtitle: '管理 TCP / UDP 四层端口映射与实时速率',
    path: '/forwards',
    icon: ArrowLeftRight,
    keywords: ['转发', '端口映射', 'tcp', 'udp', 'forward', 'nat']
  },
  {
    title: '浏览器首页 (SunPanel)',
    subtitle: '配置导航页联动与内外部服务卡片同步',
    path: '/sunpanel',
    icon: Compass,
    keywords: ['导航', '主页', 'sunpanel', '面板']
  },
  {
    title: '动态域名 (DDNS)',
    subtitle: '公网 IPv4 / IPv6 监控与 DNS 服务商自动解析',
    path: '/ddns',
    icon: Globe,
    keywords: ['ddns', 'dns', 'cloudflare', 'aliyun', 'dnspod', '公网ip']
  },
  {
    title: 'SSL 证书 (Certificates)',
    subtitle: 'ACME 自动申请、泛域名 DNS 验证与到期续期',
    path: '/certificates',
    icon: Lock,
    keywords: ['ssl', 'tls', 'https', 'acme', '证书', 'letsencrypt']
  },
  {
    title: '订阅管理 (Subscriptions)',
    subtitle: '远程 IP / CIDR 名单定时拉取与防火墙联动',
    path: '/subscriptions',
    icon: Download,
    keywords: ['订阅', 'cidr', 'ip列表', '远程规则']
  },
  {
    title: '防火墙管理 (Firewall)',
    subtitle: '来源 IP 访问控制、WAF 防护、速率限制与失败冻结',
    path: '/access',
    icon: ShieldAlert,
    keywords: ['防火墙', 'waf', '黑白名单', '限流', '安全', 'access']
  },

  // 分组 2: 观察与管理
  {
    title: '通知中心 (Notifications)',
    subtitle: '查看服务端故障恢复历史与系统异常提醒',
    path: '/notifications',
    icon: Bell,
    keywords: ['通知', '告警', '消息', '故障记录', 'alert']
  },
  {
    title: '故障推送设置 (Alert Push)',
    subtitle: '配置 Webhook 与 WxPusher 微信实时告警推送',
    id: 'notifications-push',
    path: '/notifications?tab=push',
    to: { path: '/notifications', query: { tab: 'push' } },
    icon: Bell,
    keywords: ['webhook', 'wxpusher', '微信推送', '故障推送', '告警通知']
  },
  {
    title: '运行状态 (Runtime)',
    subtitle: 'CPU、内存、实际监听端口与反代上游健康巡检',
    path: '/runtime',
    icon: Activity,
    keywords: ['运行状态', '监控', 'cpu', '内存', '监听端口', '健康检查', 'runtime']
  },
  {
    title: '防火墙拦截记录 (Firewall Logs)',
    subtitle: 'WAF 规则命中、来源拦截与限流审计日志',
    path: '/firewall-logs',
    icon: FileSearch,
    keywords: ['拦截记录', 'waf日志', '攻击记录', '安全日志']
  },
  {
    title: '日志 (Logs)',
    subtitle: '反向代理访问日志检索与多维筛选',
    path: '/logs',
    icon: ScrollText,
    keywords: ['访问日志', '请求日志', 'access log', '排查']
  },
  {
    title: 'IP 拦截名单 (IP Blocks)',
    subtitle: '查看与解除自动冻结 IP 或手动添加封禁',
    path: '/ip-blocks',
    icon: Ban,
    keywords: ['封禁', '拉黑', '解封', '冻结', 'ip block', 'ban']
  },
  {
    title: '统计 (Statistics)',
    subtitle: '请求量趋势、状态码分布与热门反代服务排行',
    path: '/statistics',
    icon: BarChart3,
    keywords: ['统计', '报表', '请求数', '排行', 'analytics']
  },
  {
    title: '流量统计 (Traffic)',
    subtitle: '反代与端口转发上传下载速率及历史流量明细',
    path: '/traffic',
    icon: Activity,
    keywords: ['流量', '带宽', '网速', '上传', '下载', 'traffic']
  },
  {
    title: 'AI 管理 (MCP)',
    subtitle: 'Model Context Protocol 接入凭证、权限范围与审计',
    path: '/mcp',
    icon: Bot,
    keywords: ['ai', 'mcp', 'claude', 'agent', '智能体', 'token']
  },
  {
    title: '更新 (Updates)',
    subtitle: '在线检查正式版 / Beta 更新、上传更新包与版本回滚',
    path: '/updates',
    icon: RefreshCw,
    keywords: ['更新', '升级', '回滚', '版本', 'beta', 'stable', 'update']
  },
  {
    title: '备份与恢复 (Backups)',
    subtitle: '本地加密备份导出、恢复预检与 WebDAV 自动云备份',
    path: '/backups',
    icon: HardDriveDownload,
    keywords: ['备份', '恢复', 'webdav', '迁移', '导出', 'backup']
  },
  {
    title: '设置 (Settings)',
    subtitle: '系统参数、可信代理、出站代理与日志保留策略',
    path: '/settings',
    icon: Settings,
    keywords: ['设置', '配置', '出站代理', '日志保留', 'settings']
  },
  {
    title: '入口暴露总览 (Exposure)',
    subtitle: '审视所有对外监听端口、认证保护与防火墙覆盖状态',
    path: '/settings/exposure',
    icon: ShieldAlert,
    keywords: ['暴露', '安全审计', '端口检查', 'exposure']
  },
  {
    title: '账号安全 (Account Security)',
    subtitle: '管理员密码、Passkey 通行密钥与登录会话管理',
    path: '/settings/security',
    icon: KeyRound,
    keywords: ['账号安全', '密码', 'passkey', '通行密钥', '会话', 'security']
  }
]

const actions = [
  {
    id: 'act_theme',
    title: '切换 深色 / 浅色 主题',
    subtitle: '在 Obsidian 深色与 VisionOS 浅色外观之间切换',
    icon: Moon,
    keywords: ['theme', 'dark', 'light', '主题', '深色', '浅色', '夜间模式'],
    run: () => {
      toggleTheme()
      success(`已切换为 ${theme.value === 'dark' ? 'Obsidian 深色' : 'VisionOS 浅色'} 主题`)
    }
  },
  {
    id: 'act_new_route',
    title: '新建反向代理路由',
    subtitle: '打开反向代理页面并立即弹出新建服务窗口',
    icon: PlusCircle,
    shortcut: '⌘N',
    keywords: ['新建反代', '添加路由', 'create route', 'new proxy'],
    run: () => {
      closeCmdk()
      router.push({ path: '/routes', query: { new: '1' } })
    }
  },
  {
    id: 'act_new_forward',
    title: '添加端口转发规则',
    subtitle: '打开端口转发页面并立即弹出新建 TCP/UDP 转发窗口',
    icon: PlusCircle,
    keywords: ['新建转发', '添加端口转发', 'create forward', 'tcp', 'udp'],
    run: () => {
      closeCmdk()
      router.push({ path: '/forwards', query: { new: '1' } })
    }
  },
  {
    id: 'act_ddns_sync',
    title: '立即同步全部 DDNS 动态域名',
    subtitle: '触发后台任务立即检测公网 IP 并更新 DNS 记录',
    icon: RefreshCw,
    keywords: ['同步ddns', '刷新dns', 'sync ddns'],
    run: syncDDNS
  },
  {
    id: 'act_check_update',
    title: '前往检查系统版本更新',
    subtitle: '打开版本更新与回滚页面检测最新发行包',
    icon: RefreshCw,
    keywords: ['检查更新', '升级版本', 'check update'],
    run: () => {
      closeCmdk()
      router.push('/updates')
    }
  },
  {
    id: 'act_account',
    title: '修改管理员账户密码',
    subtitle: '打开管理员凭据安全修改弹窗',
    icon: KeyRound,
    keywords: ['修改密码', '账号', 'password', 'admin'],
    run: () => {
      closeCmdk()
      openAccountModal()
    }
  },
  {
    id: 'act_logout',
    title: '退出登录 (Logout)',
    subtitle: '结束当前管理员会话并返回登录页',
    icon: LogOut,
    keywords: ['退出', '注销', 'logout', 'signout'],
    run: () => {
      closeCmdk()
      return logout()
    }
  }
]

const KIND_ICONS = {
  route: Network,
  group: Network,
  forward: ArrowLeftRight,
  ddns: Globe,
  certificate: Lock,
  firewall: ShieldAlert,
  subscription: Download,
  notification: Bell
}

function resolveItemIcon(item) {
  return item.icon || KIND_ICONS[item.kind] || Search
}

const allSearchItems = computed(() =>
  buildSearchItems({
    config: gatewayConfig.value,
    notifications: notifications.value,
    pages,
    actions
  })
)

const searchResult = computed(() =>
  searchGlobalItems(allSearchItems.value, {
    query: query.value,
    category: selectedCategory.value
  })
)

const totalItemsCount = computed(() => searchResult.value.flatItems.length)
const currentCategoryLabel = computed(
  () => SEARCH_CATEGORIES.find((cat) => cat.id === selectedCategory.value)?.label || ''
)

function selectCategory(id) {
  selectedCategory.value = id
  selectedIndex.value = 0
  inputRef.value?.focus()
}

function clearQuery() {
  query.value = ''
  selectedIndex.value = 0
  inputRef.value?.focus()
}

watch(isCmdkOpen, async (open) => {
  if (open) {
    query.value = ''
    selectedCategory.value = 'all'
    selectedIndex.value = 0
    if (!gatewayConfig.value) {
      refreshConfig().catch(() => {})
    }
    await nextTick()
    if (!isCmdkOpen.value) return
    if (!dialogRef.value?.open) {
      dialogRef.value?.showModal()
    }
    inputRef.value?.focus()
  } else if (dialogRef.value?.open) {
    dialogRef.value.close()
  }
})

watch([query, selectedCategory], () => {
  selectedIndex.value = 0
})

watch(
  [query, selectedCategory, selectedIndex],
  () => {
    dialogRef.value?.querySelector('.cmdk-item.active')?.scrollIntoView({ block: 'nearest' })
  },
  { flush: 'post' }
)

const navigateList = (direction) => {
  if (totalItemsCount.value === 0) return
  selectedIndex.value = (selectedIndex.value + direction + totalItemsCount.value) % totalItemsCount.value
}

const selectActive = () => {
  const item = searchResult.value.flatItems[selectedIndex.value]
  if (item) handleSelectItem(item)
}

const handleSelectItem = async (item) => {
  if (item.kind === 'action' && typeof item.run === 'function') {
    closeCmdk()
    await nextTick()
    item.run()
    return
  }
  if (item.to) {
    router.push(item.to)
    closeCmdk()
  }
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
  margin: 0;
  border: 0;
  width: 100vw;
  height: 100dvh;
  max-width: none;
  max-height: none;
  background: transparent;
  z-index: 999;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding-top: 10vh;
}
.cmdk-backdrop:not([open]) {
  display: none;
}
.cmdk-backdrop::backdrop {
  background: rgba(0, 0, 0, 0.65);
  backdrop-filter: blur(8px);
}

.cmdk-dialog {
  width: 92%;
  max-width: 660px;
  background: var(--popover-bg, var(--card-bg));
  border: 1px solid var(--card-border-hover);
  border-radius: var(--radius-card);
  box-shadow: 0 25px 60px rgba(0, 0, 0, 0.6), var(--inner-highlight);
  backdrop-filter: blur(30px);
  -webkit-backdrop-filter: blur(30px);
  overflow: hidden;
  animation: cmdkPop 0.2s cubic-bezier(0.16, 1, 0.3, 1);
}

body.theme-light .cmdk-dialog {
  background: #ffffff;
  border-color: rgba(203, 213, 225, 0.9);
  box-shadow: 0 20px 50px rgba(15, 23, 42, 0.16);
}

@keyframes cmdkPop {
  from {
    opacity: 0;
    transform: scale(0.97) translateY(-10px);
  }
  to {
    opacity: 1;
    transform: scale(1) translateY(0);
  }
}

.cmdk-input-wrapper {
  display: flex;
  align-items: center;
  padding: 14px 18px;
  border-bottom: 1px solid var(--line);
  gap: 10px;
}

.cmdk-search-icon {
  color: var(--brand-light);
  flex-shrink: 0;
}

.cmdk-input {
  flex: 1;
  min-width: 0;
  background: transparent;
  border: none;
  font-size: 14px;
  color: var(--ink);
  outline: none;
}
.cmdk-input::placeholder {
  color: var(--faint);
}

.cmdk-clear-btn {
  background: transparent;
  border: 1px solid var(--line);
  border-radius: 4px;
  font-size: 11px;
  padding: 2px 7px;
  color: var(--muted);
  cursor: pointer;
  flex-shrink: 0;
}
.cmdk-clear-btn:hover {
  color: var(--ink);
  background: var(--strip-bg);
}

.cmdk-esc-badge {
  background: var(--strip-bg);
  border: 1px solid var(--line);
  border-radius: 4px;
  font-size: 10px;
  padding: 2px 6px;
  color: var(--muted);
  cursor: pointer;
  flex-shrink: 0;
}

.cmdk-categories {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 14px;
  border-bottom: 1px solid var(--line);
  background: var(--strip-bg);
  overflow-x: auto;
  scrollbar-width: none;
}
.cmdk-categories::-webkit-scrollbar {
  display: none;
}

.cmdk-cat-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 4px 10px;
  border-radius: 999px;
  border: 1px solid var(--line);
  background: transparent;
  color: var(--muted);
  font-size: 12px;
  font-weight: 500;
  white-space: nowrap;
  cursor: pointer;
  transition: var(--transition-smooth);
}
.cmdk-cat-btn:hover {
  color: var(--ink);
  background: var(--card-hover-bg);
}
.cmdk-cat-btn.active {
  color: var(--ink);
  background: rgba(99, 102, 241, 0.16);
  border-color: rgba(99, 102, 241, 0.45);
  font-weight: 600;
}

.cmdk-cat-count {
  font-size: 10.5px;
  opacity: 0.8;
}

.cmdk-results {
  max-height: 400px;
  overflow-y: auto;
  padding: 8px;
}

.cmdk-group {
  margin-bottom: 8px;
}

.cmdk-group-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  color: var(--faint);
  padding: 6px 10px 4px 10px;
  letter-spacing: 0.5px;
}

.cmdk-group-count {
  font-size: 10.5px;
}

.cmdk-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 12px;
  border-radius: var(--radius-sm);
  color: var(--ink-secondary);
  font-size: 13px;
  cursor: pointer;
  transition: background 0.14s ease, color 0.14s ease;
}

.cmdk-item.active,
.cmdk-item:hover {
  background: rgba(99, 102, 241, 0.14);
  color: var(--ink);
}

.cmdk-item-icon {
  color: var(--muted);
  flex-shrink: 0;
}
.cmdk-item.active .cmdk-item-icon {
  color: var(--brand-light);
}
.cmdk-item-icon.action {
  color: var(--brand-cyan);
}
.cmdk-item-icon.bad {
  color: var(--danger);
}
.cmdk-item-icon.warn {
  color: var(--warn);
}

.cmdk-item-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.cmdk-item-label {
  font-weight: 500;
  color: var(--ink);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.cmdk-item-sub {
  font-size: 11.5px;
  color: var(--muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.cmdk-item-meta {
  font-family: var(--mono);
  font-size: 11px;
  color: var(--faint);
  flex-shrink: 0;
}

.cmdk-item-badge {
  font-size: 10.5px;
  font-weight: 600;
  padding: 2px 7px;
  border-radius: 999px;
  background: var(--strip-bg);
  border: 1px solid var(--line);
  color: var(--muted);
  flex-shrink: 0;
}
.cmdk-item-badge.tone-ok {
  color: var(--ok);
  background: rgba(16, 185, 129, 0.1);
  border-color: rgba(16, 185, 129, 0.25);
}
.cmdk-item-badge.tone-warn {
  color: var(--warn);
  background: rgba(245, 158, 11, 0.12);
  border-color: rgba(245, 158, 11, 0.28);
}
.cmdk-item-badge.tone-bad {
  color: var(--danger);
  background: rgba(244, 63, 94, 0.12);
  border-color: rgba(244, 63, 94, 0.28);
}
.cmdk-item-badge.tone-brand {
  color: var(--brand-light);
  background: rgba(99, 102, 241, 0.12);
  border-color: rgba(99, 102, 241, 0.28);
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

.cmdk-reset-cat-btn {
  margin-top: 4px;
  padding: 5px 12px;
  border-radius: 999px;
  border: 1px solid var(--card-border-hover);
  background: var(--strip-bg);
  color: var(--ink);
  font-size: 12px;
  cursor: pointer;
}
.cmdk-reset-cat-btn:hover {
  background: var(--card-hover-bg);
}

.cmdk-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 18px;
  background: var(--strip-bg);
  border-top: 1px solid var(--line);
  font-size: 11px;
  color: var(--faint);
}

.cmdk-footer-hints {
  display: flex;
  align-items: center;
  gap: 14px;
}

.cmdk-footer kbd {
  background: rgba(148, 163, 184, 0.15);
  padding: 1px 4px;
  border-radius: 3px;
  font-family: var(--mono);
}

@media (max-width: 640px) {
  .cmdk-backdrop {
    padding-top: 4vh;
  }
  .cmdk-dialog {
    width: 95%;
  }
  .cmdk-results {
    max-height: 60dvh;
  }
  .cmdk-footer-hints {
    display: none;
  }
}
</style>
