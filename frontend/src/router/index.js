import { createRouter, createWebHashHistory } from 'vue-router'
import { setUnauthorizedHandler } from '@/api/client'
import { useSession } from '@/composables/useSession'
import { useToast } from '@/composables/useToast'

const routes = [
  { path: '/', redirect: '/overview' },
  // Group 1: 工作空间
  {
    path: '/overview',
    name: 'Overview',
    component: () => import('@/views/OverviewView.vue'),
    meta: { title: '概览', icon: 'LayoutDashboard' }
  },
  {
    path: '/routes',
    name: 'Routes',
    component: () => import('@/views/RoutesView.vue'),
    meta: { title: '反向代理', icon: 'Network' }
  },
  {
    path: '/icons',
    name: 'Icons',
    component: () => import('@/views/IconsView.vue'),
    meta: { title: '图标库', icon: 'Images' }
  },
  {
    path: '/forwards',
    name: 'Forwards',
    component: () => import('@/views/ForwardsView.vue'),
    meta: { title: '端口转发', icon: 'ArrowLeftRight' }
  },
  {
    path: '/sunpanel',
    name: 'SunPanel',
    component: () => import('@/views/SunPanelView.vue'),
    meta: { title: '浏览器首页', icon: 'Compass' }
  },
  {
    path: '/remote-home', name: 'RemoteHome', component: () => import('@/views/RemoteHomeView.vue'),
    meta: { title: '远程回家', icon: 'House' }
  },
  {
    path: '/mihomo',
    name: 'Mihomo',
    component: () => import('@/views/MihomoView.vue'),
    meta: { title: '代理分流', icon: 'GitFork' }
  },
  {
    path: '/ddns',
    name: 'Ddns',
    component: () => import('@/views/DdnsView.vue'),
    meta: { title: '动态域名', icon: 'Globe' }
  },
  {
    path: '/certificates',
    name: 'Certificates',
    component: () => import('@/views/CertificatesView.vue'),
    meta: { title: 'SSL 证书', icon: 'Lock' }
  },
  {
    path: '/subscriptions',
    name: 'Subscriptions',
    component: () => import('@/views/SubscriptionsView.vue'),
    meta: { title: '订阅管理', icon: 'Download' }
  },
  {
    path: '/access',
    alias: '/firewall',
    name: 'Firewall',
    component: () => import('@/views/FirewallView.vue'),
    meta: { title: '防火墙管理', icon: 'ShieldAlert' }
  },

  // Group 2: 观察与管理
  {
    path: '/notifications', name: 'Notifications', component: () => import('@/views/NotificationsView.vue'),
    meta: { title: '通知中心', icon: 'Bell' }
  },
  {
    path: '/runtime', name: 'Runtime', component: () => import('@/views/RuntimeView.vue'),
    meta: { title: '运行状态', icon: 'Activity' }
  },
  {
    path: '/logs',
    name: 'Logs',
    component: () => import('@/views/LogsView.vue'),
    meta: { title: '日志与拦截', icon: 'ScrollText', logTab: 'access' }
  },
  {
    path: '/logs/firewall',
    name: 'FirewallLogs',
    component: () => import('@/views/LogsView.vue'),
    meta: { title: '防火墙拦截记录', icon: 'FileSearch', logTab: 'firewall' }
  },
  {
    path: '/logs/ip-blocks',
    name: 'IpBlocks',
    component: () => import('@/views/LogsView.vue'),
    meta: { title: 'IP 拦截名单', icon: 'Ban', logTab: 'ip-blocks' }
  },
  {
    path: '/logs/system',
    name: 'SystemLogs',
    component: () => import('@/views/LogsView.vue'),
    meta: { title: '系统日志', icon: 'ScrollText', logTab: 'system' }
  },
  {
    path: '/firewall-logs',
    redirect: to => ({ path: '/logs/firewall', query: to.query })
  },
  {
    path: '/ip-blocks',
    redirect: to => ({ path: '/logs/ip-blocks', query: to.query })
  },
  {
    path: '/statistics',
    name: 'Statistics',
    component: () => import('@/views/StatisticsHubView.vue'),
    meta: { title: '数据统计', icon: 'BarChart3', statTab: 'access' }
  },
  {
    path: '/statistics/traffic',
    name: 'Traffic',
    component: () => import('@/views/StatisticsHubView.vue'),
    meta: { title: '流量统计', icon: 'Activity', statTab: 'traffic' }
  },
  {
    path: '/traffic',
    redirect: to => ({ path: '/statistics/traffic', query: to.query })
  },
  {
    path: '/assistant',
    name: 'Assistant',
    component: () => import('@/views/AiHubView.vue'),
    meta: { title: 'AI 与集成', icon: 'Sparkles', aiTab: 'chat' }
  },
  {
    path: '/assistant/settings',
    name: 'AssistantSettings',
    component: () => import('@/views/AiHubView.vue'),
    meta: { title: '模型接入点', icon: 'Settings', aiTab: 'settings' }
  },
  {
    path: '/assistant/mcp',
    name: 'Mcp',
    component: () => import('@/views/AiHubView.vue'),
    meta: { title: 'MCP 授权', icon: 'Bot', aiTab: 'mcp' }
  },
  {
    path: '/mcp',
    redirect: to => ({ path: '/assistant/mcp', query: to.query })
  },
  {
    path: '/updates',
    name: 'Updates',
    component: () => import('@/views/UpdatesView.vue'),
    meta: { title: '更新', icon: 'RefreshCw' }
  },
  {
    path: '/backups',
    name: 'Backups',
    component: () => import('@/views/BackupsView.vue'),
    meta: { title: '备份与恢复', icon: 'HardDriveDownload' }
  },
  {
    path: '/settings',
    name: 'Settings',
    component: () => import('@/views/SettingsView.vue'),
    meta: { title: '设置', icon: 'Settings' }
  },

  {
    path: '/settings/exposure',
    component: () => import('@/views/ExposureView.vue'),
    meta: { title: '入口暴露总览' }
  },
  {
    path: '/settings/security',
    component: () => import('@/views/AccountSecurityView.vue'),
    meta: { title: '账号安全' }
  },

  // Standalone
  {
    path: '/sunpanel-connect',
    name: 'SunPanelConnect',
    component: () => import('@/views/SunPanelConnectView.vue'),
    meta: { title: 'SunPanel 联动', hideLayout: true }
  },
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/LoginView.vue'),
    meta: { title: '登录', hideLayout: true }
  }
]

const router = createRouter({
  history: createWebHashHistory(),
  routes
})

const { isAuthenticated, restoreSession, endSession } = useSession()

function loginRoute(redirect) {
  return { name: 'Login', query: { redirect } }
}

export function postLoginPath(redirect) {
  if (typeof redirect === 'string' && redirect.startsWith('/') && !redirect.startsWith('//')) {
    const target = router.resolve(redirect)
    if (target.matched.length && target.name !== 'Login') return target.fullPath
  }
  return '/overview'
}

setUnauthorizedHandler(message => {
  if (!isAuthenticated.value) return
  const current = router.currentRoute.value
  endSession()
  useToast().warn(message)
  if (current.name !== 'Login') router.replace(loginRoute(current.fullPath))
})

router.beforeEach(async to => {
  try {
    await restoreSession()
  } catch (err) {
    if (to.name !== 'Login') useToast().error(err.message)
  }

  if (to.name === 'Login' && isAuthenticated.value) return postLoginPath(to.query.redirect)
  if (to.name !== 'Login' && !isAuthenticated.value) return loginRoute(to.fullPath)
})

router.afterEach((to) => {
  document.title = to.meta.title ? `${to.meta.title} · GateHome` : 'GateHome'
})

export default router
