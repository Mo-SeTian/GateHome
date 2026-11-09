<template>
  <div class="routes-page animate-fade-in">
    <!-- Top Bar -->
    <div class="page-topbar">
      <div>
        <h1 class="page-title">反向代理与路由组</h1>
        <p class="page-subtitle">按反代组管理公网与局域网域名路由、SSL 卸载、来源 IP 防火墙挂载与专属账号验证</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-primary" @click="openCreateModal">
          <Plus :size="15" />
          新建反代服务
        </button>
      </div>
    </div>

    <!-- Filter & Search Controls -->
    <div class="filter-bar">
      <!-- Group Chips (按反代组筛选) -->
      <div class="chips-group">
        <button 
          class="chip-btn" 
          :class="{ active: selectedGroup === 'ALL' }" 
          @click="selectedGroup = 'ALL'"
        >
          全部路由
          <span class="chip-count">{{ routes.length }}</span>
        </button>
        <button 
          v-for="grp in proxyGroups" 
          :key="grp.id"
          class="chip-btn"
          :class="{ active: selectedGroup === grp.id }"
          @click="selectedGroup = grp.id"
        >
          {{ grp.name }}
          <span class="chip-count">{{ getGroupCount(grp.id) }}</span>
        </button>
        <button 
          class="chip-btn" 
          :class="{ active: selectedGroup === 'INACTIVE' }" 
          @click="selectedGroup = 'INACTIVE'"
        >
          已停用
          <span class="chip-count">{{ inactiveCount }}</span>
        </button>
      </div>

      <!-- Search Input -->
      <div class="search-box">
        <Search :size="14" class="search-box-icon" />
        <input 
          v-model="searchQuery" 
          type="text" 
          placeholder="按服务名、域名或目标IP搜索..."
          class="search-input"
        />
        <button v-if="searchQuery" class="clear-btn" @click="searchQuery = ''">
          <X :size="12" />
        </button>
      </div>
    </div>

    <!-- Routes Cards Grid (4 Columns) -->
    <div class="routes-grid">
      <SpotlightCard 
        v-for="rt in filteredRoutes" 
        :key="rt.id"
        class="route-card"
        :class="{ inactive: rt.status !== 'active' }"
      >
        <!-- Card Header -->
        <div class="card-head">
          <div class="app-icon-wrap" :class="rt.category.toLowerCase()">
            <Shield v-if="rt.icon === 'shield'" :size="18" />
            <HardDrive v-else-if="rt.icon === 'hard-drive'" :size="18" />
            <Key v-else-if="rt.icon === 'key'" :size="18" />
            <Home v-else-if="rt.icon === 'home'" :size="18" />
            <Play v-else-if="rt.icon === 'play'" :size="18" />
            <Code v-else-if="rt.icon === 'code'" :size="18" />
            <Tv v-else-if="rt.icon === 'tv'" :size="18" />
            <Activity v-else :size="18" />
          </div>

          <div class="card-title-group">
            <div class="card-title-row">
              <span class="service-name">{{ rt.name }}</span>
              <span 
                class="status-dot" 
                :class="rt.status === 'active' ? 'ok' : 'neutral'" 
                :title="rt.status === 'active' ? '服务运行中' : '服务已暂停'"
              />
            </div>
            <div class="service-category">所属组: {{ getGroupName(rt.groupId) }}</div>
          </div>

          <!-- Switch Toggle -->
          <label class="switch">
            <input 
              type="checkbox" 
              :checked="rt.status === 'active'"
              @change="toggleStatus(rt)"
            />
            <span class="switch-slider" />
          </label>
        </div>

        <!-- Domain & Target Info -->
        <div class="endpoint-info">
          <!-- Public Domain -->
          <div class="info-row domain-row" @click="copyText(rt.domain, '已复制访问域名')">
            <Globe :size="14" class="info-icon" />
            <span class="info-text mono-text domain-text">{{ rt.domain }}</span>
            <Copy :size="12" class="copy-icon" />
          </div>

          <!-- Upstream Target -->
          <div class="info-row target-row">
            <ArrowRight :size="14" class="info-icon target-arrow" />
            <span class="info-text mono-text">{{ rt.target }}</span>
          </div>
        </div>

        <!-- Security & Feature Badges -->
        <div class="feature-badges">
          <span v-if="rt.ssl" class="feature-badge ssl" title="已启用 SSL 证书与 HTTPS 强制加密">
            <Lock :size="10" /> HTTPS
          </span>
          <span v-if="rt.firewallId" class="feature-badge fw" :title="`挂载防火墙: ${getFwName(rt.firewallId)}`">
            <ShieldCheck :size="10" /> {{ getFwName(rt.firewallId) }}
          </span>
          <span v-if="rt.auth?.enabled" class="feature-badge auth" title="已开启账号密码登录防护">
            <Key :size="10" /> 账号保护
          </span>
          <span v-if="rt.websocket" class="feature-badge ws" title="支持 WebSocket 协议升级">
            <Zap :size="10" /> WS
          </span>
          <span class="feature-badge hits">
            {{ rt.hitCount }} 次
          </span>
        </div>

        <!-- Card Footer -->
        <div class="card-actions">
          <div class="latency-indicator">
            <span class="latency-label">延迟:</span>
            <span class="latency-val" :class="{ 'zero': rt.status !== 'active' }">
              {{ rt.latency }}
            </span>
          </div>
          <div class="action-btn-group">
            <button class="btn btn-ghost btn-sm" @click="openEditModal(rt)">
              <Edit3 :size="13" /> 编辑
            </button>
            <button class="btn btn-ghost btn-sm text-bad" @click="deleteRouteItem(rt)">
              <Trash2 :size="13" />
            </button>
          </div>
        </div>
      </SpotlightCard>
    </div>

    <!-- Empty State -->
    <div v-if="filteredRoutes.length === 0" class="empty-state">
      <AlertCircle :size="32" class="empty-icon" />
      <div class="empty-title">未找到匹配的反代服务</div>
      <div class="empty-desc">请尝试调整搜索条件或切换反代组筛选</div>
      <button class="btn btn-secondary btn-sm" @click="resetFilters">重置筛选</button>
    </div>

    <!-- Create / Edit Modal -->
    <transition name="modal-fade">
      <div v-if="isModalOpen" class="modal-backdrop" @click.self="isModalOpen = false">
        <div class="modal-dialog">
          <div class="modal-header">
            <h3 class="modal-title">{{ isEdit ? '编辑反向代理路由' : '新建反向代理路由' }}</h3>
            <button class="close-modal-btn" @click="isModalOpen = false">
              <X :size="18" />
            </button>
          </div>

          <form class="modal-body" @submit.prevent="saveRoute">
            <div class="form-row-2">
              <div class="form-group">
                <label class="form-label">服务名称</label>
                <input 
                  v-model="formData.name" 
                  type="text" 
                  placeholder="例如: Nextcloud 云盘服务" 
                  class="form-input" 
                  required 
                />
              </div>
              <div class="form-group">
                <label class="form-label">所属反代组</label>
                <select v-model="formData.groupId" class="form-select">
                  <option v-for="g in proxyGroups" :key="g.id" :value="g.id">
                    {{ g.name }} (:{{ g.httpPort }} / :{{ g.httpsPort }})
                  </option>
                </select>
              </div>
            </div>

            <div class="form-group">
              <label class="form-label">访问完整域名 (Host)</label>
              <input 
                v-model="formData.domain" 
                type="text" 
                placeholder="例如: cloud.nas.tech" 
                class="form-input mono-text" 
                required 
              />
            </div>

            <div class="form-group">
              <label class="form-label">内网目标主机与端口 (Upstream)</label>
              <input 
                v-model="formData.target" 
                type="text" 
                placeholder="例如: http://192.168.2.110:80" 
                class="form-input mono-text" 
                required 
              />
            </div>

            <div class="form-group">
              <label class="form-label">来源 IP 防火墙 / WAF 策略</label>
              <select v-model="formData.firewallId" class="form-select">
                <option value="">无防火墙 (直接放行所有来源 IP)</option>
                <option v-for="fw in firewalls" :key="fw.id" :value="fw.id">
                  {{ fw.name }}
                </option>
              </select>
            </div>

            <!-- Toggles Panel -->
            <div class="toggles-panel">
              <label class="toggle-row">
                <div class="toggle-info">
                  <span class="toggle-title">启用 SSL 证书与强制 HTTPS</span>
                  <span class="toggle-desc">自动匹配 ACME 证书并进行 TLS 握手</span>
                </div>
                <input v-model="formData.ssl" type="checkbox" />
              </label>

              <label class="toggle-row">
                <div class="toggle-info">
                  <span class="toggle-title">开启专属账号登录鉴权</span>
                  <span class="toggle-desc">要求在网关前置输入账号密码，连续失败自动冻结来源 IP</span>
                </div>
                <input v-model="formData.auth.enabled" type="checkbox" />
              </label>

              <div v-if="formData.auth.enabled" class="auth-sub-fields">
                <div class="form-row-2">
                  <div class="form-group">
                    <label class="form-label">鉴权用户名</label>
                    <input v-model="formData.auth.username" type="text" placeholder="admin" class="form-input" required />
                  </div>
                  <div class="form-group">
                    <label class="form-label">鉴权密码</label>
                    <input type="password" placeholder="••••••••••••" class="form-input" />
                  </div>
                </div>
              </div>

              <label class="toggle-row">
                <div class="toggle-info">
                  <span class="toggle-title">WebSocket 长连接支持</span>
                  <span class="toggle-desc">允许协议升级双向传输数据</span>
                </div>
                <input v-model="formData.websocket" type="checkbox" />
              </label>
            </div>

            <div class="modal-footer">
              <button type="button" class="btn btn-secondary" @click="isModalOpen = false">
                取消
              </button>
              <button type="submit" class="btn btn-primary">
                {{ isEdit ? '保存更改' : '立即创建' }}
              </button>
            </div>
          </form>
        </div>
      </div>
    </transition>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { 
  Plus, Search, X, Globe, ArrowRight, Lock, ShieldCheck, Zap, 
  Copy, Edit3, Trash2, AlertCircle, Shield, HardDrive, Key, Home, 
  Play, Code, Tv, Activity
} from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { useAppStore } from '@/composables/useAppStore'
import { useToast } from '@/composables/useToast'

const { routes, proxyGroups, firewalls } = useAppStore()
const { success, info } = useToast()

const selectedGroup = ref('ALL')
const searchQuery = ref('')
const isModalOpen = ref(false)
const isEdit = ref(false)

const formData = ref({
  id: '',
  groupId: 'grp_default',
  name: '',
  domain: '',
  target: '',
  category: 'System',
  ssl: true,
  firewallId: 'fw_china_only',
  auth: { enabled: false, username: 'admin', failureLimit: 5, freezeSeconds: 1800 },
  websocket: false
})

const getGroupCount = (grpId) => {
  return routes.value.filter(r => r.groupId === grpId).length
}

const getGroupName = (grpId) => {
  const g = proxyGroups.value.find(p => p.id === grpId)
  return g ? g.name.split(' ')[0] : '默认组'
}

const getFwName = (fwId) => {
  const fw = firewalls.value.find(f => f.id === fwId)
  return fw ? fw.name.split(' ')[0] : 'WAF 防护'
}

const inactiveCount = computed(() => routes.value.filter(r => r.status !== 'active').length)

const filteredRoutes = computed(() => {
  return routes.value.filter(rt => {
    // Group filter
    if (selectedGroup.value === 'INACTIVE') {
      if (rt.status === 'active') return false
    } else if (selectedGroup.value !== 'ALL') {
      if (rt.groupId !== selectedGroup.value) return false
    }

    // Search query
    if (searchQuery.value.trim()) {
      const q = searchQuery.value.toLowerCase()
      return rt.name.toLowerCase().includes(q) ||
             rt.domain.toLowerCase().includes(q) ||
             rt.target.toLowerCase().includes(q)
    }

    return true
  })
})

const toggleStatus = (rt) => {
  rt.status = rt.status === 'active' ? 'inactive' : 'active'
  if (rt.status === 'active') {
    rt.latency = `${(Math.random() * 3 + 1).toFixed(1)}ms`
    success(`已启用服务: ${rt.name}`)
  } else {
    rt.latency = '--'
    info(`已暂停服务: ${rt.name}`)
  }
}

const copyText = (text, message) => {
  navigator.clipboard?.writeText(text)
  success(message || '已复制到剪贴板')
}

const openCreateModal = () => {
  isEdit.value = false
  formData.value = {
    id: `rt_${Date.now()}`,
    groupId: proxyGroups.value[0]?.id || 'grp_default',
    name: '',
    domain: '',
    target: 'http://192.168.2.',
    category: 'System',
    ssl: true,
    firewallId: firewalls.value[0]?.id || '',
    auth: { enabled: false, username: 'admin', failureLimit: 5, freezeSeconds: 1800 },
    websocket: false
  }
  isModalOpen.value = true
}

const openEditModal = (rt) => {
  isEdit.value = true
  formData.value = {
    ...rt,
    auth: { ...(rt.auth || { enabled: false, username: 'admin', failureLimit: 5, freezeSeconds: 1800 }) }
  }
  isModalOpen.value = true
}

const saveRoute = () => {
  if (isEdit.value) {
    const idx = routes.value.findIndex(r => r.id === formData.value.id)
    if (idx !== -1) {
      routes.value[idx] = { ...routes.value[idx], ...formData.value }
      success(`反代服务 "${formData.value.name}" 已更新`)
    }
  } else {
    routes.value.unshift({
      ...formData.value,
      status: 'active',
      hitCount: '0',
      latency: '1.4ms',
      icon: 'shield'
    })
    success(`成功创建反代路由 "${formData.value.name}"`)
  }
  isModalOpen.value = false
}

const deleteRouteItem = (rt) => {
  if (confirm(`确定要移除反代路由 "${rt.name}" (${rt.domain}) 吗？`)) {
    routes.value = routes.value.filter(r => r.id !== rt.id)
    info(`已删除反代服务: ${rt.name}`)
  }
}

const resetFilters = () => {
  selectedGroup.value = 'ALL'
  searchQuery.value = ''
}
</script>

<style scoped>
.routes-page { display: flex; flex-direction: column; gap: 20px; }
.page-topbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 16px; }
.page-title { font-size: 22px; font-weight: 700; color: var(--ink); }
.page-subtitle { font-size: 13px; color: var(--muted); margin-top: 3px; }

.filter-bar { display: flex; align-items: center; justify-content: space-between; gap: 16px; flex-wrap: wrap; }
.chips-group { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.chip-btn {
  display: inline-flex; align-items: center; gap: 6px; padding: 5px 12px;
  border-radius: var(--radius-pill); background: var(--strip-bg);
  border: 1px solid var(--line); color: var(--muted); font-size: 12px; font-weight: 500;
}
.chip-btn.active {
  background: rgba(99, 102, 241, 0.18); border-color: rgba(99, 102, 241, 0.4);
  color: #fff; font-weight: 600;
}
.theme-light .chip-btn.active { color: var(--brand); }
.chip-count {
  font-family: var(--mono); font-size: 10px; background: rgba(148, 163, 184, 0.15);
  padding: 1px 5px; border-radius: 999px;
}

.search-box {
  display: flex; align-items: center; gap: 8px; padding: 6px 12px;
  background: var(--input-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); width: 260px;
}
.search-box-icon { color: var(--faint); }
.search-input { border: none; background: transparent; color: var(--ink); font-size: 12.5px; flex: 1; }
.clear-btn { color: var(--faint); padding: 2px; }

/* Grid */
.routes-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; }
.route-card { padding: 18px; display: flex; flex-direction: column; justify-content: space-between; }
.route-card.inactive { opacity: 0.65; filter: grayscale(0.2); }

.card-head { display: flex; align-items: center; gap: 12px; margin-bottom: 14px; }
.app-icon-wrap {
  width: 36px; height: 36px; border-radius: var(--radius-sm);
  display: flex; align-items: center; justify-content: center;
  border: 1px solid var(--line); flex-shrink: 0;
}
.app-icon-wrap.system { background: rgba(99, 102, 241, 0.12); color: var(--brand-light); }
.app-icon-wrap.storage { background: rgba(6, 182, 212, 0.12); color: var(--brand-cyan); }
.app-icon-wrap.security { background: rgba(245, 158, 11, 0.12); color: var(--warn); }
.app-icon-wrap.iot { background: rgba(16, 185, 129, 0.12); color: var(--ok); }
.app-icon-wrap.media { background: rgba(244, 63, 94, 0.12); color: var(--bad); }
.app-icon-wrap.dev { background: rgba(168, 85, 247, 0.12); color: #c084fc; }
.app-icon-wrap.ops { background: rgba(56, 189, 248, 0.12); color: #38bdf8; }

.card-title-group { flex: 1; min-width: 0; }
.card-title-row { display: flex; align-items: center; gap: 6px; }
.service-name { font-size: 13.5px; font-weight: 600; color: var(--ink); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.service-category { font-size: 11px; color: var(--faint); margin-top: 1px; }

.endpoint-info {
  background: var(--strip-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); padding: 9px 11px; margin-bottom: 12px;
  display: flex; flex-direction: column; gap: 6px;
}
.info-row { display: flex; align-items: center; gap: 7px; font-size: 12px; }
.domain-row { cursor: pointer; color: var(--ink); }
.domain-row:hover .domain-text { color: var(--brand-light); }
.domain-row:hover .copy-icon { opacity: 1; }
.domain-text { font-weight: 500; flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.copy-icon { opacity: 0; color: var(--faint); transition: opacity 0.2s ease; }
.target-row { color: var(--muted); }
.target-arrow { color: var(--brand-cyan); }

.feature-badges { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; margin-bottom: 14px; }
.feature-badge {
  display: inline-flex; align-items: center; gap: 4px; font-size: 10.5px; font-weight: 600;
  padding: 2px 7px; border-radius: 4px;
}
.feature-badge.ssl { background: rgba(16, 185, 129, 0.12); color: var(--ok); border: 1px solid rgba(16, 185, 129, 0.2); }
.feature-badge.fw { background: rgba(99, 102, 241, 0.12); color: var(--brand-light); border: 1px solid rgba(99, 102, 241, 0.2); }
.feature-badge.auth { background: rgba(245, 158, 11, 0.12); color: var(--warn); border: 1px solid rgba(245, 158, 11, 0.2); }
.feature-badge.ws { background: rgba(6, 182, 212, 0.12); color: var(--brand-cyan); border: 1px solid rgba(6, 182, 212, 0.2); }
.feature-badge.hits { background: var(--strip-bg); color: var(--faint); border: 1px solid var(--line); font-family: var(--mono); }

.card-actions {
  display: flex; align-items: center; justify-content: space-between; border-top: 1px solid var(--line); padding-top: 10px;
}
.latency-indicator { display: flex; align-items: center; gap: 4px; font-size: 11.5px; }
.latency-label { color: var(--faint); }
.latency-val { color: var(--ok); font-family: var(--mono); font-weight: 600; }
.latency-val.zero { color: var(--faint); }
.action-btn-group { display: flex; gap: 4px; }
.text-bad { color: var(--bad) !important; }

.empty-state {
  padding: 60px 20px; text-align: center; display: flex; flex-direction: column; align-items: center; gap: 10px;
}
.empty-icon { color: var(--faint); }
.empty-title { font-size: 15px; font-weight: 600; color: var(--ink); }
.empty-desc { font-size: 12.5px; color: var(--muted); }

/* Modal */
.modal-backdrop {
  position: fixed; inset: 0; background: rgba(0,0,0,0.65);
  backdrop-filter: blur(8px); -webkit-backdrop-filter: blur(8px);
  z-index: 1050; display: flex; align-items: center; justify-content: center; padding: 20px;
}
.modal-dialog {
  width: 100%; max-width: 520px; background: var(--card-bg);
  border: 1px solid var(--card-border-hover); border-radius: var(--radius-card);
  backdrop-filter: blur(30px); -webkit-backdrop-filter: blur(30px);
  box-shadow: 0 25px 60px rgba(0, 0, 0, 0.6), var(--inner-highlight); overflow: hidden;
}
.modal-header { padding: 18px 20px; border-bottom: 1px solid var(--line); display: flex; align-items: center; justify-content: space-between; }
.modal-title { font-size: 16px; font-weight: 700; color: var(--ink); }
.close-modal-btn { color: var(--muted); }
.modal-body { padding: 20px; display: flex; flex-direction: column; gap: 14px; }
.form-group { display: flex; flex-direction: column; gap: 6px; }
.form-label { font-size: 12px; font-weight: 600; color: var(--ink-secondary); }
.form-row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }

.toggles-panel {
  background: var(--strip-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); padding: 12px 14px; display: flex; flex-direction: column; gap: 10px;
}
.toggle-row { display: flex; align-items: center; justify-content: space-between; cursor: pointer; }
.toggle-info { display: flex; flex-direction: column; }
.toggle-title { font-size: 12.5px; font-weight: 600; color: var(--ink); }
.toggle-desc { font-size: 11px; color: var(--faint); }
.auth-sub-fields { padding: 8px 0; border-top: 1px dashed var(--line); margin-top: 4px; }
.modal-footer { display: flex; justify-content: flex-end; gap: 10px; padding-top: 8px; }

@media (max-width: 1300px) { .routes-grid { grid-template-columns: repeat(3, 1fr); } }
@media (max-width: 900px) { .routes-grid { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 600px) {
  .routes-grid { grid-template-columns: 1fr; }
  .filter-bar { flex-direction: column; align-items: stretch; }
  .search-box { width: 100%; }
}
</style>
