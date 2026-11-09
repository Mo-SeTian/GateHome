<template>
  <div class="logs-page animate-fade-in">
    <div class="page-topbar">
      <div>
        <h1 class="page-title">系统与访问调用日志</h1>
        <p class="page-subtitle">按反代规则、访问结果、WAF 安全命中筛选请求记录；审查 DDNS、证书与 IP 订阅的执行结果</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-secondary" @click="isLive = !isLive">
          <span class="status-dot" :class="isLive ? 'ok' : 'neutral'" />
          {{ isLive ? '实时监听中' : '已暂停' }}
        </button>
        <button class="btn btn-secondary" @click="simulateLog">
          <Play :size="14" />
          模拟新调用
        </button>
        <button class="btn btn-ghost btn-sm" @click="clearLogs">
          <Trash2 :size="14" />
        </button>
      </div>
    </div>

    <!-- Dual Tab Switcher: 反代日志 vs 项目日志 -->
    <div class="log-scope-tabs">
      <button 
        class="scope-tab-btn" 
        :class="{ active: currentScope === 'access' }"
        @click="currentScope = 'access'"
      >
        <Network :size="15" />
        反代访问日志 (Access Logs)
        <span class="tab-badge">{{ accessLogs.length }}</span>
      </button>
      <button 
        class="scope-tab-btn" 
        :class="{ active: currentScope === 'project' }"
        @click="currentScope = 'project'"
      >
        <ScrollText :size="15" />
        项目系统日志 (Project Logs)
        <span class="tab-badge">{{ projectLogs.length }}</span>
      </button>
    </div>

    <!-- Filter Bar (Context-aware based on currentScope) -->
    <div class="filter-bar">
      <!-- If Access Scope -->
      <template v-if="currentScope === 'access'">
        <div class="chips-group">
          <button 
            v-for="st in statusFilters" 
            :key="st.key"
            class="chip-btn"
            :class="{ active: selectedStatus === st.key }"
            @click="selectedStatus = st.key"
          >
            {{ st.label }}
          </button>
        </div>

        <div class="search-box">
          <Search :size="14" class="search-icon" />
          <input v-model="searchQuery" type="text" placeholder="按路径、域名、IP 或地理归属搜索..." class="search-input" />
        </div>
      </template>

      <!-- If Project Scope -->
      <template v-else>
        <div class="chips-group">
          <button 
            v-for="c in projectCategories" 
            :key="c.key"
            class="chip-btn"
            :class="{ active: selectedCategory === c.key }"
            @click="selectedCategory = c.key"
          >
            {{ c.label }}
          </button>
        </div>

        <div class="search-box">
          <Search :size="14" class="search-icon" />
          <input v-model="searchQuery" type="text" placeholder="按操作、目标或消息关键词搜索..." class="search-input" />
        </div>
      </template>
    </div>

    <!-- Log Table Card -->
    <SpotlightCard class="table-card">
      <div class="table-responsive">
        <!-- 1. Access Logs Table -->
        <table v-if="currentScope === 'access'" class="log-table">
          <thead>
            <tr>
              <th>时间 / 序号</th>
              <th>方法</th>
              <th>状态</th>
              <th>请求路径 (URI)</th>
              <th>反向代理目标</th>
              <th>来源 IP & 归属地</th>
              <th class="text-right">网关耗时</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="log in filteredAccessLogs" :key="log.id">
              <td class="mono-text time-cell">{{ log.time }}</td>
              <td>
                <span class="method-tag" :class="log.method.toLowerCase()">
                  {{ log.method }}
                </span>
              </td>
              <td>
                <span class="status-badge" :class="statusClass(log.status)">
                  {{ log.status }}
                </span>
              </td>
              <td class="mono-text path-cell" :title="log.path">{{ log.path }}</td>
              <td>
                <div class="target-cell">
                  <span class="target-name">{{ log.route }}</span>
                  <span class="target-upstream mono-text">{{ log.target }}</span>
                </div>
              </td>
              <td>
                <div class="ip-cell">
                  <span class="mono-text ip-val">{{ log.ip }}</span>
                  <span class="geo-val">{{ log.geo }}</span>
                </div>
              </td>
              <td class="text-right mono-text latency-cell" :class="latencyClass(log.duration)">
                {{ log.duration }} ms
              </td>
            </tr>
          </tbody>
        </table>

        <!-- 2. Project Logs Table -->
        <table v-else class="log-table">
          <thead>
            <tr>
              <th>时间</th>
              <th>分类</th>
              <th>操作调用</th>
              <th>目标对象</th>
              <th>执行结果</th>
              <th>详细说明</th>
              <th class="text-right">耗时</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="pj in filteredProjectLogs" :key="pj.id">
              <td class="mono-text time-cell">{{ pj.time }}</td>
              <td>
                <span class="badge" :class="categoryBadgeClass(pj.category)">
                  {{ categoryName(pj.category) }}
                </span>
              </td>
              <td class="action-cell"><b>{{ pj.action }}</b></td>
              <td class="mono-text target-cell">{{ pj.target }}</td>
              <td>
                <span class="badge" :class="pj.ok ? 'badge-ok' : 'badge-bad'">
                  {{ pj.ok ? '成功' : '失败' }}
                </span>
              </td>
              <td class="desc-cell">{{ pj.message }}</td>
              <td class="text-right mono-text latency-cell lat-fast">
                {{ pj.duration }} ms
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="(currentScope === 'access' ? filteredAccessLogs.length : filteredProjectLogs.length) === 0" class="empty-state">
        <AlertCircle :size="30" class="empty-icon" />
        <div>暂无匹配的调用记录</div>
      </div>
    </SpotlightCard>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { Play, Trash2, Search, AlertCircle, Network, ScrollText } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { useAppStore } from '@/composables/useAppStore'
import { useToast } from '@/composables/useToast'

const { accessLogs, projectLogs, routes } = useAppStore()
const { info } = useToast()

const currentScope = ref('access') // 'access' | 'project'
const isLive = ref(true)
const selectedStatus = ref('ALL')
const selectedCategory = ref('ALL')
const searchQuery = ref('')
let liveInterval = null

const statusFilters = [
  { key: 'ALL', label: '全部状态' },
  { key: '2xx', label: '2xx 正常' },
  { key: '3xx', label: '3xx 重定向' },
  { key: '4xx', label: '4xx 客户端异常' },
  { key: '5xx', label: '5xx 网关故障' }
]

const projectCategories = [
  { key: 'ALL', label: '全部项目分类' },
  { key: 'ddns', label: 'DDNS 域名' },
  { key: 'subscriptions', label: 'IP 订阅' },
  { key: 'certificates', label: 'SSL 证书' },
  { key: 'admin', label: '管理操作' },
  { key: 'forward', label: '端口转发' }
]

const filteredAccessLogs = computed(() => {
  return accessLogs.value.filter(log => {
    if (selectedStatus.value !== 'ALL') {
      const code = String(log.status)
      if (selectedStatus.value === '2xx' && !code.startsWith('2') && code !== '101') return false
      if (selectedStatus.value === '3xx' && !code.startsWith('3')) return false
      if (selectedStatus.value === '4xx' && !code.startsWith('4')) return false
      if (selectedStatus.value === '5xx' && !code.startsWith('5')) return false
    }
    if (searchQuery.value.trim()) {
      const q = searchQuery.value.toLowerCase()
      return log.path.toLowerCase().includes(q) ||
             log.route.toLowerCase().includes(q) ||
             log.ip.toLowerCase().includes(q) ||
             log.geo.toLowerCase().includes(q)
    }
    return true
  })
})

const filteredProjectLogs = computed(() => {
  return projectLogs.value.filter(pj => {
    if (selectedCategory.value !== 'ALL' && pj.category !== selectedCategory.value) {
      return false
    }
    if (searchQuery.value.trim()) {
      const q = searchQuery.value.toLowerCase()
      return pj.action.toLowerCase().includes(q) ||
             pj.target.toLowerCase().includes(q) ||
             pj.message.toLowerCase().includes(q)
    }
    return true
  })
})

const statusClass = (code) => {
  if (code >= 200 && code < 300) return 's-2xx'
  if (code === 101) return 's-101'
  if (code >= 300 && code < 400) return 's-3xx'
  if (code >= 400 && code < 500) return 's-4xx'
  return 's-5xx'
}

const latencyClass = (dur) => {
  if (dur < 10) return 'lat-fast'
  if (dur < 100) return 'lat-med'
  return 'lat-slow'
}

const categoryName = (c) => {
  const map = {
    ddns: 'DDNS',
    subscriptions: 'IP 订阅',
    certificates: 'SSL 证书',
    admin: '管理操作',
    forward: '端口转发'
  }
  return map[c] || c
}

const categoryBadgeClass = (c) => {
  if (c === 'ddns') return 'badge-brand'
  if (c === 'subscriptions') return 'badge-ok'
  if (c === 'certificates') return 'badge-warn'
  return 'badge-neutral'
}

const simulateLog = () => {
  const samplePaths = ['/api/v1/ping', '/index.html', '/static/bundle.js', '/download/archive.zip', '/ws/events']
  const randomRoute = routes.value[Math.floor(Math.random() * routes.value.length)]
  const now = new Date()
  const timeStr = `${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}:${String(now.getSeconds()).padStart(2, '0')}`

  accessLogs.value.unshift({
    id: `log_${Date.now()}`,
    time: timeStr,
    method: Math.random() > 0.3 ? 'GET' : 'POST',
    status: Math.random() > 0.1 ? 200 : 404,
    duration: parseFloat((Math.random() * 8 + 0.8).toFixed(1)),
    path: samplePaths[Math.floor(Math.random() * samplePaths.length)],
    ip: `192.168.2.${Math.floor(Math.random() * 200 + 10)}`,
    geo: '局域网 · 本地私网',
    route: randomRoute ? randomRoute.name : 'GateHome API',
    target: randomRoute ? randomRoute.target : 'http://127.0.0.1:16666',
    ok: true
  })

  if (accessLogs.value.length > 50) accessLogs.value.pop()
}

const clearLogs = () => {
  if (currentScope.value === 'access') {
    accessLogs.value = []
    info('反代访问日志已清空')
  } else {
    projectLogs.value = []
    info('项目系统日志已清空')
  }
}

onMounted(() => {
  liveInterval = setInterval(() => {
    if (isLive.value && Math.random() > 0.4) {
      simulateLog()
    }
  }, 3500)
})

onUnmounted(() => {
  if (liveInterval) clearInterval(liveInterval)
})
</script>

<style scoped>
.logs-page { display: flex; flex-direction: column; gap: 20px; }
.page-topbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 16px; }
.page-title { font-size: 22px; font-weight: 700; color: var(--ink); }
.page-subtitle { font-size: 13px; color: var(--muted); margin-top: 3px; }
.header-actions { display: flex; align-items: center; gap: 10px; }

/* Scope Tabs */
.log-scope-tabs {
  display: flex; gap: 8px; border-bottom: 1px solid var(--line); padding-bottom: 12px;
}
.scope-tab-btn {
  display: flex; align-items: center; gap: 8px; padding: 7px 16px;
  border-radius: var(--radius-sm); background: var(--strip-bg);
  border: 1px solid var(--line); color: var(--muted); font-size: 13px;
  font-weight: 500; transition: var(--transition-smooth);
}
.scope-tab-btn:hover { background: var(--card-hover-bg); color: var(--ink); }
.scope-tab-btn.active {
  background: rgba(99, 102, 241, 0.18); border-color: rgba(99, 102, 241, 0.4);
  color: #fff; font-weight: 600;
}
.theme-light .scope-tab-btn.active { color: var(--brand); }

.tab-badge {
  font-family: var(--mono); font-size: 11px; padding: 1px 6px;
  border-radius: 999px; background: rgba(148, 163, 184, 0.15);
}

.filter-bar { display: flex; align-items: center; justify-content: space-between; gap: 16px; flex-wrap: wrap; }
.chips-group { display: flex; gap: 6px; }
.chip-btn {
  padding: 5px 12px; border-radius: var(--radius-pill);
  background: var(--strip-bg); border: 1px solid var(--line);
  color: var(--muted); font-size: 12px; font-weight: 500;
}
.chip-btn.active {
  background: rgba(99, 102, 241, 0.18); border-color: rgba(99, 102, 241, 0.4);
  color: #fff; font-weight: 600;
}
.theme-light .chip-btn.active { color: var(--brand); }

.search-box {
  display: flex; align-items: center; gap: 8px; padding: 6px 12px;
  background: var(--input-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); width: 280px;
}
.search-icon { color: var(--faint); }
.search-input { border: none; background: transparent; color: var(--ink); font-size: 12.5px; flex: 1; }

.table-card { padding: 0; overflow: hidden; }
.table-responsive { width: 100%; overflow-x: auto; }

.log-table {
  width: 100%; border-collapse: collapse; text-align: left; font-size: 12.5px;
}
.log-table th {
  padding: 12px 18px; font-weight: 600; color: var(--faint);
  border-bottom: 1px solid var(--line); background: var(--strip-bg);
  white-space: nowrap; font-size: 11.5px;
}
.log-table td {
  padding: 11px 18px; border-bottom: 1px solid var(--line);
  color: var(--ink-secondary); vertical-align: middle;
}
.log-table tr:hover td { background: rgba(148, 163, 184, 0.05); }

.time-cell { color: var(--faint); font-size: 11.5px; }

.method-tag {
  font-family: var(--mono); font-size: 11px; font-weight: 700;
  padding: 2px 6px; border-radius: 4px;
}
.method-tag.get { background: rgba(6, 182, 212, 0.15); color: var(--brand-cyan); }
.method-tag.post { background: rgba(99, 102, 241, 0.15); color: var(--brand-light); }

.status-badge {
  font-family: var(--mono); font-size: 11px; font-weight: 700;
  padding: 2px 7px; border-radius: 999px;
}
.status-badge.s-2xx, .status-badge.s-101 { background: rgba(16, 185, 129, 0.15); color: var(--ok); }
.status-badge.s-3xx { background: rgba(56, 189, 248, 0.15); color: #38bdf8; }
.status-badge.s-4xx { background: rgba(245, 158, 11, 0.15); color: var(--warn); }
.status-badge.s-5xx { background: rgba(244, 63, 94, 0.15); color: var(--bad); }

.path-cell { max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--ink); }
.target-cell { display: flex; flex-direction: column; gap: 2px; }
.target-name { font-weight: 600; color: var(--ink); }
.target-upstream { font-size: 11px; color: var(--faint); }

.ip-cell { display: flex; flex-direction: column; gap: 2px; }
.ip-val { font-weight: 500; color: var(--ink); }
.geo-val { font-size: 10.5px; color: var(--faint); }

.action-cell { color: var(--ink); }
.desc-cell { color: var(--muted); }

.latency-cell.lat-fast { color: var(--ok); font-weight: 600; }
.latency-cell.lat-med { color: var(--warn); font-weight: 600; }
.latency-cell.lat-slow { color: var(--bad); font-weight: 600; }

.text-right { text-align: right; }

.empty-state {
  padding: 40px; text-align: center; color: var(--muted);
  display: flex; flex-direction: column; align-items: center; gap: 10px;
}
.empty-icon { color: var(--faint); }

@media (max-width: 768px) {
  .filter-bar { flex-direction: column; align-items: stretch; }
  .search-box { width: 100%; }
}
</style>
