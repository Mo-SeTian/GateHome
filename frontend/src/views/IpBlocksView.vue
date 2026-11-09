<template>
  <div class="ip-blocks-page animate-fade-in">
    <div class="page-topbar">
      <div>
        <h1 class="page-title">IP 冻结名单</h1>
        <p class="page-subtitle">达到账号密码验证失败阈值、触发 WAF 防护或管理员手动添加的高危来源 IP，到期自动解除阻断</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-primary" @click="openCreateModal">
          <ShieldAlert :size="15" />
          手动冻结 IP
        </button>
      </div>
    </div>

    <!-- Filter & Search Controls -->
    <div class="filter-bar">
      <div class="rule-select-wrap">
        <label class="filter-label">关联反代规则</label>
        <select v-model="selectedRule" class="form-select filter-select">
          <option value="ALL">全部反代规则</option>
          <option v-for="rt in routes" :key="rt.id" :value="rt.id">
            {{ rt.name }} ({{ rt.domain }})
          </option>
        </select>
      </div>

      <div class="search-box">
        <Search :size="14" class="search-icon" />
        <input v-model="searchQuery" type="text" placeholder="搜索 IPv4 / IPv6 或冻结原因..." class="search-input" />
      </div>
    </div>

    <!-- Frozen Records List (Cards & Table) -->
    <div class="freeze-cards-grid">
      <SpotlightCard 
        v-for="item in filteredList" 
        :key="item.id"
        class="freeze-card"
      >
        <div class="freeze-head">
          <div class="ip-title-group">
            <span class="mono-text freeze-ip">{{ item.ip }}</span>
            <span class="badge" :class="item.manual ? 'badge-warn' : 'badge-bad'">
              {{ item.manual ? '管理员手动' : '安全引擎自动' }}
            </span>
          </div>
          <button class="btn btn-secondary btn-sm" @click="unblock(item)">
            <Unlock :size="12" /> 解除冻结
          </button>
        </div>

        <div class="freeze-details-box">
          <div class="f-detail-row">
            <span class="f-label">关联业务规则:</span>
            <span class="f-val">{{ item.ruleName }}</span>
          </div>
          <div class="f-detail-row">
            <span class="f-label">冻结触发原因:</span>
            <span class="f-val reason-text">{{ item.reason }}</span>
          </div>
          <div class="f-detail-row">
            <span class="f-label">到期时间:</span>
            <span class="f-val mono-text">{{ item.until }}</span>
          </div>
          <div class="f-detail-row">
            <span class="f-label">累计阻断请求:</span>
            <span class="f-val mono-text hits-val">{{ item.hits }} 次</span>
          </div>
        </div>

        <div class="freeze-footer">
          <span class="remaining-text">
            <Clock :size="12" />
            剩余冻结时长: <b class="time-bold">{{ item.remaining }}</b>
          </span>
          <span class="auto-release-tag">到期自动解除</span>
        </div>
      </SpotlightCard>
    </div>

    <div v-if="filteredList.length === 0" class="empty-state">
      <CheckCircle :size="32" class="empty-icon ok-text" />
      <div class="empty-title">当前没有被冻结的 IP</div>
      <div class="empty-desc">达到账号验证失败或 WAF 阈值后，被阻断的来源 IP 将在此处展示与追踪</div>
    </div>

    <!-- Manual Freeze Modal -->
    <transition name="modal-fade">
      <div v-if="isModalOpen" class="modal-backdrop" @click.self="isModalOpen = false">
        <div class="modal-dialog">
          <div class="modal-header">
            <h3 class="modal-title">手动冻结 IP 来源</h3>
            <button class="close-modal-btn" @click="isModalOpen = false"><X :size="18" /></button>
          </div>
          <form class="modal-body" @submit.prevent="createFreeze">
            <div class="form-group">
              <label class="form-label">来源 IP 地址 (IPv4 / IPv6)</label>
              <input v-model="form.ip" type="text" placeholder="例如: 203.0.113.10" class="form-input mono-text" required />
            </div>

            <div class="form-group">
              <label class="form-label">关联生效的反代规则</label>
              <select v-model="form.ruleId" class="form-select">
                <option value="ALL">全部反代规则 (全局阻断)</option>
                <option v-for="rt in routes" :key="rt.id" :value="rt.id">
                  {{ rt.name }} ({{ rt.domain }})
                </option>
              </select>
            </div>

            <div class="form-group">
              <label class="form-label">冻结持续时长</label>
              <select v-model="form.duration" class="form-select">
                <option value="1800">30 分钟</option>
                <option value="7200">2 小时</option>
                <option value="86400">24 小时 (1天)</option>
                <option value="604800">7 天</option>
                <option value="forever">永久冻结</option>
              </select>
            </div>

            <div class="form-group">
              <label class="form-label">冻结备注原因</label>
              <input v-model="form.reason" type="text" placeholder="例如: 疑似恶意脚本探测，手动阻断" class="form-input" required />
            </div>

            <div class="modal-footer">
              <button type="button" class="btn btn-secondary" @click="isModalOpen = false">取消</button>
              <button type="submit" class="btn btn-danger">执行冻结</button>
            </div>
          </form>
        </div>
      </div>
    </transition>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ShieldAlert, Unlock, Search, CheckCircle, Clock, X } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { useAppStore } from '@/composables/useAppStore'
import { useToast } from '@/composables/useToast'

const { ipBlocks, routes } = useAppStore()
const { success, info } = useToast()

const selectedRule = ref('ALL')
const searchQuery = ref('')
const isModalOpen = ref(false)

const form = ref({
  ip: '',
  ruleId: 'ALL',
  duration: '86400',
  reason: ''
})

const filteredList = computed(() => {
  return ipBlocks.value.filter(item => {
    if (searchQuery.value.trim()) {
      const q = searchQuery.value.toLowerCase()
      return item.ip.toLowerCase().includes(q) || item.reason.toLowerCase().includes(q)
    }
    return true
  })
})

const openCreateModal = () => {
  form.value = {
    ip: '',
    ruleId: 'ALL',
    duration: '86400',
    reason: '管理员手动添加拦截'
  }
  isModalOpen.value = true
}

const createFreeze = () => {
  const selectedRt = routes.value.find(r => r.id === form.value.ruleId)
  ipBlocks.value.unshift({
    id: `blk_${Date.now()}`,
    ip: form.value.ip,
    rule: form.value.ruleId === 'ALL' ? '全局阻断' : selectedRt?.domain,
    ruleName: form.value.ruleId === 'ALL' ? '全部反代规则' : selectedRt?.name,
    reason: form.value.reason,
    until: form.value.duration === 'forever' ? '永久' : '24 小时后',
    remaining: form.value.duration === 'forever' ? '永久冻结' : '24 小时',
    hits: 0,
    manual: true
  })
  success(`已将 IP ${form.value.ip} 冻结，网关将阻断其访问`)
  isModalOpen.value = false
}

const unblock = (item) => {
  ipBlocks.value = ipBlocks.value.filter(i => i.id !== item.id)
  info(`已解除对 IP ${item.ip} 的冻结`)
}
</script>

<style scoped>
.ip-blocks-page { display: flex; flex-direction: column; gap: 20px; }
.page-topbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 16px; }
.page-title { font-size: 22px; font-weight: 700; color: var(--ink); }
.page-subtitle { font-size: 13px; color: var(--muted); margin-top: 3px; }

.filter-bar { display: flex; align-items: center; justify-content: space-between; gap: 16px; flex-wrap: wrap; }
.rule-select-wrap { display: flex; align-items: center; gap: 8px; }
.filter-label { font-size: 12px; color: var(--faint); font-weight: 500; }
.filter-select { width: 240px; padding: 6px 10px; font-size: 12.5px; }

.search-box {
  display: flex; align-items: center; gap: 8px; padding: 6px 12px;
  background: var(--input-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); width: 280px;
}
.search-icon { color: var(--faint); }
.search-input { border: none; background: transparent; color: var(--ink); font-size: 12.5px; flex: 1; }

.freeze-cards-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: 16px; }
.freeze-card { padding: 18px; display: flex; flex-direction: column; gap: 12px; }

.freeze-head { display: flex; align-items: center; justify-content: space-between; }
.ip-title-group { display: flex; align-items: center; gap: 10px; }
.freeze-ip { font-size: 16px; font-weight: 700; color: var(--ink); }

.freeze-details-box {
  background: var(--strip-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); padding: 10px 14px;
  display: flex; flex-direction: column; gap: 6px; font-size: 12px;
}
.f-detail-row { display: flex; align-items: center; justify-content: space-between; }
.f-label { color: var(--faint); }
.f-val { color: var(--ink); font-weight: 500; }
.reason-text { color: var(--bad); }
.hits-val { color: var(--brand-light); font-weight: 700; }

.freeze-footer {
  display: flex; align-items: center; justify-content: space-between;
  border-top: 1px solid var(--line); padding-top: 10px; font-size: 11.5px;
}
.remaining-text { display: flex; align-items: center; gap: 6px; color: var(--muted); }
.time-bold { color: var(--brand-cyan); }
.auto-release-tag { color: var(--faint); }

.empty-state {
  padding: 60px 20px; text-align: center; color: var(--muted);
  display: flex; flex-direction: column; align-items: center; gap: 10px;
}
.empty-icon { width: 36px; height: 36px; }
.ok-text { color: var(--ok); }
.empty-title { font-size: 15px; font-weight: 600; color: var(--ink); }
.empty-desc { font-size: 12.5px; color: var(--muted); }

/* Modal */
.modal-backdrop {
  position: fixed; inset: 0; background: rgba(0,0,0,0.65);
  backdrop-filter: blur(8px); -webkit-backdrop-filter: blur(8px);
  z-index: 1050; display: flex; align-items: center; justify-content: center; padding: 20px;
}
.modal-dialog {
  width: 100%; max-width: 480px; background: var(--card-bg);
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
.modal-footer { display: flex; justify-content: flex-end; gap: 10px; padding-top: 8px; }

@media (max-width: 900px) {
  .freeze-cards-grid { grid-template-columns: 1fr; }
  .filter-bar { flex-direction: column; align-items: stretch; }
  .filter-select, .search-box { width: 100%; }
}
</style>
