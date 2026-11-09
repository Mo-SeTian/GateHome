<template>
  <div class="subscriptions-page animate-fade-in">
    <div class="page-topbar">
      <div>
        <h1 class="page-title">IP 规则订阅管理</h1>
        <p class="page-subtitle">逐条管理远程 IP / CIDR 威胁黑名单与运营商地址段，定时自动拉取并应用到防火墙组</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-secondary" @click="updateAllSubscriptions">
          <RefreshCw :size="14" :class="{ 'spin-icon': isUpdatingAll }" />
          全部立即更新
        </button>
        <button class="btn btn-primary" @click="openCreateModal">
          <Plus :size="15" />
          添加 IP 订阅
        </button>
      </div>
    </div>

    <!-- Presets Hub Panel (快捷预设导入) -->
    <SpotlightCard class="preset-panel">
      <div class="preset-left">
        <div class="preset-icon-box">
          <Download :size="18" />
        </div>
        <div class="preset-info">
          <span class="preset-title">快捷预设源 · 中国大陆 IP 列表</span>
          <span class="preset-desc">基于 mayaxcn/china-ip-list 提供亚太互联网络信息中心分配列表，阻断海外扫描直达国内服务</span>
        </div>
      </div>
      <div class="preset-actions">
        <button class="btn btn-secondary btn-sm" @click="addPreset('v4')">
          <Plus :size="12" /> 中国大陆 IPv4
        </button>
        <button class="btn btn-secondary btn-sm" @click="addPreset('v6')">
          <Plus :size="12" /> 中国大陆 IPv6
        </button>
        <a 
          href="https://github.com/mayaxcn/china-ip-list" 
          target="_blank" 
          rel="noopener noreferrer" 
          class="btn btn-ghost btn-sm"
        >
          查看 GitHub <ExternalLink :size="12" />
        </a>
      </div>
    </SpotlightCard>

    <!-- Subscriptions Grid -->
    <div class="subs-grid">
      <SpotlightCard 
        v-for="sub in subscriptions" 
        :key="sub.id"
        class="sub-card"
        :class="{ inactive: !sub.enabled }"
      >
        <!-- Card Header -->
        <div class="card-head">
          <div class="sub-icon-box" :class="{ active: sub.enabled }">
            <Globe :size="18" />
          </div>
          <div class="sub-head-info">
            <span class="sub-title">{{ sub.name }}</span>
            <span class="badge" :class="statusBadgeClass(sub)">
              <span class="status-dot" :class="statusDotClass(sub)" />
              {{ statusText(sub) }}
            </span>
          </div>
          <label class="switch">
            <input 
              type="checkbox" 
              :checked="sub.enabled"
              @change="toggleSubscription(sub)"
            />
            <span class="switch-slider" />
          </label>
        </div>

        <!-- URL Source Box -->
        <div class="url-source-box" @click="copyUrl(sub.url)">
          <span class="url-text mono-text">{{ sub.url }}</span>
          <Copy :size="12" class="copy-icon" />
        </div>

        <!-- Metrics & Firewall Usage -->
        <div class="meta-stats-box">
          <div class="meta-row">
            <span class="label">包含 CIDR 记录条目:</span>
            <span class="val mono-text count-highlight">{{ sub.count.toLocaleString() }} 条</span>
          </div>
          <div class="meta-row">
            <span class="label">防火墙策略引用:</span>
            <span class="val">{{ sub.usedCount }} 个防火墙组已引用</span>
          </div>
          <div class="meta-row">
            <span class="label">更新周期:</span>
            <span class="val">每 {{ (sub.interval / 3600).toFixed(0) }} 小时自动更新</span>
          </div>
          <div class="meta-row">
            <span class="label">最近成功更新:</span>
            <span class="val mono-text">{{ sub.lastSuccess }}</span>
          </div>
        </div>

        <!-- Card Footer -->
        <div class="card-footer">
          <button 
            class="btn btn-secondary btn-sm" 
            :disabled="!sub.enabled || sub.running"
            @click="refreshSingle(sub)"
          >
            <RefreshCw :size="12" :class="{ 'spin-icon': sub.running }" />
            {{ sub.running ? '更新拉取中...' : '立即拉取' }}
          </button>
          <div class="actions-right">
            <button class="btn btn-ghost btn-sm" @click="openEditModal(sub)">
              <Edit3 :size="13" /> 编辑
            </button>
            <button class="btn btn-ghost btn-sm text-bad" @click="deleteSub(sub)">
              <Trash2 :size="13" />
            </button>
          </div>
        </div>
      </SpotlightCard>
    </div>

    <!-- Create / Edit Modal -->
    <transition name="modal-fade">
      <div v-if="isModalOpen" class="modal-backdrop" @click.self="isModalOpen = false">
        <div class="modal-dialog">
          <div class="modal-header">
            <h3 class="modal-title">{{ isEdit ? '编辑 IP 规则订阅' : '添加 IP 规则订阅' }}</h3>
            <button class="close-modal-btn" @click="isModalOpen = false"><X :size="18" /></button>
          </div>
          <form class="modal-body" @submit.prevent="saveSubscription">
            <div class="form-group">
              <label class="form-label">订阅名称</label>
              <input 
                v-model="form.name" 
                type="text" 
                placeholder="例如: 某省 IP / 中国 IPv4 / 恶意扫描黑名单" 
                class="form-input" 
                required 
              />
            </div>
            <div class="form-group">
              <label class="form-label">订阅下载链接 (HTTPS 文本，每行一个 IP 或 CIDR)</label>
              <input 
                v-model="form.url" 
                type="url" 
                placeholder="https://example.com/ips.txt" 
                class="form-input mono-text" 
                required 
              />
              <small class="form-hint">支持每行一个 IPv4/IPv6 或 CIDR 网段，支持 # 注释。下载不跟随重定向。</small>
            </div>
            <div class="form-group">
              <label class="form-label">更新间隔 (秒)</label>
              <input 
                v-model.number="form.interval" 
                type="number" 
                min="300" 
                max="604800" 
                class="form-input mono-text" 
                required 
              />
              <small class="form-hint">推荐 86400 秒 (24 小时)，最短 300 秒。</small>
            </div>
            <div class="form-group">
              <label class="checkbox-row">
                <input v-model="form.enabled" type="checkbox" />
                <span>启用此订阅并按间隔自动在后台拉取更新</span>
              </label>
            </div>
            <div class="modal-footer">
              <button type="button" class="btn btn-secondary" @click="isModalOpen = false">取消</button>
              <button type="submit" class="btn btn-primary">保存订阅</button>
            </div>
          </form>
        </div>
      </div>
    </transition>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { Plus, RefreshCw, Download, Globe, ExternalLink, Copy, Edit3, Trash2, X } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { useAppStore } from '@/composables/useAppStore'
import { useToast } from '@/composables/useToast'

const { subscriptions, firewalls } = useAppStore()
const { success, warn, info } = useToast()

const isUpdatingAll = ref(false)
const isModalOpen = ref(false)
const isEdit = ref(false)

const form = ref({
  id: '',
  name: '',
  url: '',
  interval: 86400,
  enabled: true
})

const statusBadgeClass = (sub) => {
  if (!sub.enabled) return 'badge-neutral'
  if (sub.running) return 'badge-brand'
  if (sub.ready) return 'badge-ok'
  return 'badge-warn'
}

const statusDotClass = (sub) => {
  if (!sub.enabled) return 'neutral'
  if (sub.running) return 'warn'
  if (sub.ready) return 'ok'
  return 'warn'
}

const statusText = (sub) => {
  if (!sub.enabled) return '已停用'
  if (sub.running) return '更新拉取中'
  if (sub.ready) return '规则可用'
  return '更新失败，使用缓存'
}

const toggleSubscription = (sub) => {
  sub.enabled = !sub.enabled
  if (sub.enabled) {
    success(`已启用订阅: ${sub.name}`)
  } else {
    // Check if referenced by firewalls
    const referenced = firewalls.value.filter(f => 
      f.groups.some(g => g.subscriptions.includes(sub.id))
    )
    if (referenced.length > 0) {
      warn(`注意: 该订阅正被 ${referenced.length} 个防火墙组引用，停用后防火墙将使用已有缓存`)
    } else {
      info(`已暂停订阅: ${sub.name}`)
    }
  }
}

const refreshSingle = (sub) => {
  sub.running = true
  setTimeout(() => {
    sub.running = false
    sub.ready = true
    sub.lastSuccess = '刚刚'
    success(`订阅 "${sub.name}" 更新成功，已加载 ${sub.count.toLocaleString()} 条 CIDR`)
  }, 800)
}

const updateAllSubscriptions = () => {
  isUpdatingAll.value = true
  subscriptions.value.forEach(s => { s.running = true })
  setTimeout(() => {
    isUpdatingAll.value = false
    subscriptions.value.forEach(s => {
      s.running = false
      s.ready = true
      s.lastSuccess = '刚刚'
    })
    success('所有已启用的 IP 订阅源均已重新拉取并重新装载到防火墙匹配桶')
  }, 900)
}

const copyUrl = (url) => {
  navigator.clipboard?.writeText(url)
  success('已复制订阅源下载地址')
}

const addPreset = (type) => {
  if (type === 'v4') {
    const exists = subscriptions.value.find(s => s.id === 'sub_cn_v4')
    if (exists) {
      warn('中国大陆 IPv4 订阅源已存在')
      return
    }
    subscriptions.value.unshift({
      id: 'sub_cn_v4',
      name: '中国大陆 IPv4 地址段 (chnroute)',
      url: 'https://raw.githubusercontent.com/mayaxcn/china-ip-list/master/chnroute.txt',
      enabled: true,
      interval: 86400,
      count: 8624,
      usedCount: 0,
      ready: true,
      running: false,
      lastSuccess: '刚刚',
      message: '解析成功'
    })
    success('已导入预设: 中国大陆 IPv4 订阅源')
  } else {
    const exists = subscriptions.value.find(s => s.id === 'sub_cn_v6')
    if (exists) {
      warn('中国大陆 IPv6 订阅源已存在')
      return
    }
    subscriptions.value.unshift({
      id: 'sub_cn_v6',
      name: '中国大陆 IPv6 地址段 (chnroute-v6)',
      url: 'https://raw.githubusercontent.com/mayaxcn/china-ip-list/master/chnroute-v6.txt',
      enabled: true,
      interval: 86400,
      count: 2140,
      usedCount: 0,
      ready: true,
      running: false,
      lastSuccess: '刚刚',
      message: '解析成功'
    })
    success('已导入预设: 中国大陆 IPv6 订阅源')
  }
}

const openCreateModal = () => {
  isEdit.value = false
  form.value = {
    id: `sub_${Date.now()}`,
    name: '',
    url: '',
    interval: 86400,
    enabled: true
  }
  isModalOpen.value = true
}

const openEditModal = (sub) => {
  isEdit.value = true
  form.value = { ...sub }
  isModalOpen.value = true
}

const saveSubscription = () => {
  if (isEdit.value) {
    const idx = subscriptions.value.findIndex(s => s.id === form.value.id)
    if (idx !== -1) {
      subscriptions.value[idx] = { ...subscriptions.value[idx], ...form.value }
      success(`订阅 "${form.value.name}" 已更新`)
    }
  } else {
    subscriptions.value.unshift({
      ...form.value,
      count: Math.floor(Math.random() * 5000 + 1000),
      usedCount: 0,
      ready: true,
      running: false,
      lastSuccess: '刚刚',
      message: '首次拉取完成'
    })
    success(`成功添加 IP 规则订阅 "${form.value.name}"`)
  }
  isModalOpen.value = false
}

const deleteSub = (sub) => {
  // Check if referenced
  const referenced = firewalls.value.filter(f => 
    f.groups.some(g => g.subscriptions.includes(sub.id))
  )
  if (referenced.length > 0) {
    alert(`无法删除！请先从以下防火墙策略中解除此订阅的引用：\n${referenced.map(r => '· ' + r.name).join('\n')}`)
    return
  }
  if (confirm(`确定要移除 IP 规则订阅 "${sub.name}" 吗？`)) {
    subscriptions.value = subscriptions.value.filter(s => s.id !== sub.id)
    info(`已删除订阅: ${sub.name}`)
  }
}
</script>

<style scoped>
.subscriptions-page { display: flex; flex-direction: column; gap: 20px; }
.page-topbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 16px; }
.page-title { font-size: 22px; font-weight: 700; color: var(--ink); }
.page-subtitle { font-size: 13px; color: var(--muted); margin-top: 3px; }
.header-actions { display: flex; gap: 10px; }
.spin-icon { animation: spin 0.8s linear infinite; }
@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }

/* Preset Hub Panel */
.preset-panel {
  padding: 16px 20px; display: flex; align-items: center; justify-content: space-between; gap: 16px; flex-wrap: wrap;
}
.preset-left { display: flex; align-items: center; gap: 14px; }
.preset-icon-box {
  width: 40px; height: 40px; border-radius: var(--radius-sm);
  background: rgba(99, 102, 241, 0.12); color: var(--brand-light);
  display: flex; align-items: center; justify-content: center; flex-shrink: 0;
}
.preset-info { display: flex; flex-direction: column; gap: 2px; }
.preset-title { font-size: 13.5px; font-weight: 700; color: var(--ink); }
.preset-desc { font-size: 12px; color: var(--muted); }
.preset-actions { display: flex; align-items: center; gap: 8px; }

/* Subscriptions Grid */
.subs-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: 16px; }
.sub-card { padding: 18px; display: flex; flex-direction: column; gap: 14px; }
.sub-card.inactive { opacity: 0.65; }

.card-head { display: flex; align-items: center; gap: 12px; }
.sub-icon-box {
  width: 36px; height: 36px; border-radius: var(--radius-sm);
  background: rgba(99, 102, 241, 0.12); color: var(--brand-light);
  display: flex; align-items: center; justify-content: center; flex-shrink: 0;
}
.sub-head-info { flex: 1; display: flex; flex-direction: column; gap: 4px; }
.sub-title { font-size: 14px; font-weight: 700; color: var(--ink); }

.url-source-box {
  background: var(--strip-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); padding: 8px 12px;
  display: flex; align-items: center; justify-content: space-between;
  cursor: pointer; transition: var(--transition-smooth);
}
.url-source-box:hover { border-color: var(--card-border-hover); }
.url-source-box:hover .url-text { color: var(--brand-light); }
.url-text { font-size: 11.5px; color: var(--ink-secondary); flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.copy-icon { color: var(--faint); margin-left: 8px; }

.meta-stats-box {
  background: var(--strip-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); padding: 10px 14px;
  display: flex; flex-direction: column; gap: 6px; font-size: 12px;
}
.meta-row { display: flex; align-items: center; justify-content: space-between; }
.meta-row .label { color: var(--faint); }
.meta-row .val { color: var(--ink); font-weight: 500; }
.count-highlight { color: var(--brand-cyan); font-weight: 700; }

.card-footer { display: flex; align-items: center; justify-content: space-between; border-top: 1px solid var(--line); padding-top: 10px; }
.actions-right { display: flex; gap: 4px; }
.text-bad { color: var(--bad) !important; }

/* Modal */
.modal-backdrop {
  position: fixed; inset: 0; background: rgba(0,0,0,0.65);
  backdrop-filter: blur(8px); -webkit-backdrop-filter: blur(8px);
  z-index: 1050; display: flex; align-items: center; justify-content: center; padding: 20px;
}
.modal-dialog {
  width: 100%; max-width: 500px; background: var(--card-bg);
  border: 1px solid var(--card-border-hover); border-radius: var(--radius-card);
  backdrop-filter: blur(30px); -webkit-backdrop-filter: blur(30px);
  box-shadow: 0 25px 60px rgba(0, 0, 0, 0.6), var(--inner-highlight);
  overflow: hidden;
}
.modal-header { padding: 18px 20px; border-bottom: 1px solid var(--line); display: flex; align-items: center; justify-content: space-between; }
.modal-title { font-size: 16px; font-weight: 700; color: var(--ink); }
.close-modal-btn { color: var(--muted); }
.modal-body { padding: 20px; display: flex; flex-direction: column; gap: 14px; }
.form-group { display: flex; flex-direction: column; gap: 6px; }
.form-label { font-size: 12px; font-weight: 600; color: var(--ink-secondary); }
.form-hint { font-size: 11px; color: var(--faint); }
.checkbox-row { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--ink-secondary); cursor: pointer; }
.modal-footer { display: flex; justify-content: flex-end; gap: 10px; padding-top: 8px; }

@media (max-width: 900px) {
  .subs-grid { grid-template-columns: 1fr; }
  .preset-panel { flex-direction: column; align-items: flex-start; }
}
</style>
