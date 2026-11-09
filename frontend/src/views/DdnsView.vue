<template>
  <div class="ddns-page animate-fade-in">
    <div class="page-topbar">
      <div>
        <h1 class="page-title">动态域名解析 (DDNS)</h1>
        <p class="page-subtitle">自动检测公网 IPv4 / IPv6 地址变动并同步至 DNS 托管商</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-secondary" @click="syncAllTasks">
          <RefreshCw :size="14" :class="{ 'spin-icon': isSyncing }" />
          立即强制同步
        </button>
        <button class="btn btn-primary" @click="openCreateModal">
          <Plus :size="15" />
          添加 DDNS 任务
        </button>
      </div>
    </div>

    <!-- DDNS Task Cards -->
    <div class="ddns-grid">
      <SpotlightCard 
        v-for="task in ddnsTasks" 
        :key="task.id"
        class="ddns-card"
      >
        <div class="card-head">
          <div class="provider-badge">
            <Globe :size="16" class="provider-icon" />
            <span class="provider-name">{{ task.provider }}</span>
          </div>
          <span class="badge" :class="task.status === 'synced' ? 'badge-ok' : 'badge-warn'">
            <span class="status-dot" :class="task.status === 'synced' ? 'ok' : 'warn'" />
            {{ task.status === 'synced' ? '已同步' : '同步中' }}
          </span>
        </div>

        <div class="domain-title mono-text">{{ task.domain }}</div>

        <div class="task-details-box">
          <div class="detail-row">
            <span class="detail-label">解析记录类型:</span>
            <span class="badge badge-neutral mono-text">{{ task.recordType }}</span>
          </div>
          <div class="detail-row">
            <span class="detail-label">当前解析 IP:</span>
            <span class="ip-text mono-text">{{ task.currentIp }}</span>
          </div>
          <div class="detail-row">
            <span class="detail-label">TTL 设置:</span>
            <span class="detail-val">{{ task.ttl }}</span>
          </div>
          <div class="detail-row">
            <span class="detail-label">上次同步:</span>
            <span class="detail-val">{{ task.lastSync }}</span>
          </div>
        </div>

        <div class="card-footer">
          <button class="btn btn-secondary btn-sm" @click="syncSingle(task)">
            <RefreshCw :size="12" /> 单独同步
          </button>
          <button class="btn btn-ghost btn-sm text-bad" @click="deleteTask(task)">
            <Trash2 :size="13" />
          </button>
        </div>
      </SpotlightCard>
    </div>

    <!-- Create Modal -->
    <transition name="modal-fade">
      <div v-if="isModalOpen" class="modal-backdrop" @click.self="isModalOpen = false">
        <div class="modal-dialog">
          <div class="modal-header">
            <h3 class="modal-title">添加 DDNS 动态域名任务</h3>
            <button class="close-modal-btn" @click="isModalOpen = false"><X :size="18" /></button>
          </div>
          <form class="modal-body" @submit.prevent="createTask">
            <div class="form-group">
              <label class="form-label">DNS 托管商</label>
              <select v-model="form.provider" class="form-select">
                <option value="Cloudflare">Cloudflare (推荐)</option>
                <option value="Aliyun 阿里云 DNS">Aliyun 阿里云 DNS</option>
                <option value="DNSPod (腾讯云)">DNSPod (腾讯云)</option>
                <option value="HuaweiCloud 华为云">HuaweiCloud 华为云</option>
              </select>
            </div>
            <div class="form-group">
              <label class="form-label">解析域名</label>
              <input v-model="form.domain" type="text" placeholder="nas.example.com" class="form-input mono-text" required />
            </div>
            <div class="form-row-2">
              <div class="form-group">
                <label class="form-label">记录类型</label>
                <select v-model="form.recordType" class="form-select">
                  <option value="A (IPv4)">A (IPv4)</option>
                  <option value="AAAA (IPv6)">AAAA (IPv6)</option>
                </select>
              </div>
              <div class="form-group">
                <label class="form-label">TTL</label>
                <input v-model="form.ttl" type="text" placeholder="自动 / 600s" class="form-input" />
              </div>
            </div>
            <div class="form-group">
              <label class="form-label">API 凭据密钥 / Token</label>
              <input type="password" placeholder="••••••••••••••••••••" class="form-input" required />
            </div>
            <div class="modal-footer">
              <button type="button" class="btn btn-secondary" @click="isModalOpen = false">取消</button>
              <button type="submit" class="btn btn-primary">保存任务</button>
            </div>
          </form>
        </div>
      </div>
    </transition>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { RefreshCw, Plus, Globe, Trash2, X } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { useAppStore } from '@/composables/useAppStore'
import { useToast } from '@/composables/useToast'

const { ddnsTasks } = useAppStore()
const { success, info } = useToast()

const isSyncing = ref(false)
const isModalOpen = ref(false)
const form = ref({
  provider: 'Cloudflare',
  domain: '',
  recordType: 'A (IPv4)',
  ttl: '自动'
})

const syncAllTasks = () => {
  isSyncing.value = true
  setTimeout(() => {
    isSyncing.value = false
    ddnsTasks.value.forEach(t => {
      t.lastSync = '刚刚'
      t.status = 'synced'
    })
    success('所有 DDNS 动态域名已强制更新成功')
  }, 700)
}

const syncSingle = (task) => {
  task.lastSync = '刚刚'
  success(`域名 "${task.domain}" 同步成功 (当前IP: ${task.currentIp})`)
}

const openCreateModal = () => {
  form.value = {
    provider: 'Cloudflare',
    domain: '',
    recordType: 'A (IPv4)',
    ttl: '自动'
  }
  isModalOpen.value = true
}

const createTask = () => {
  ddnsTasks.value.push({
    id: `ddns_${Date.now()}`,
    provider: form.value.provider,
    domain: form.value.domain,
    recordType: form.value.recordType,
    currentIp: form.value.recordType.includes('IPv6') ? '2408:8207:1800::4a91' : '114.88.23.109',
    lastSync: '刚刚',
    status: 'synced',
    ttl: form.value.ttl || '自动'
  })
  success(`DDNS 任务 "${form.value.domain}" 已建立`)
  isModalOpen.value = false
}

const deleteTask = (task) => {
  if (confirm(`确定要移除 DDNS 任务 "${task.domain}" 吗？`)) {
    ddnsTasks.value = ddnsTasks.value.filter(t => t.id !== task.id)
    info(`已删除任务: ${task.domain}`)
  }
}
</script>

<style scoped>
.ddns-page { display: flex; flex-direction: column; gap: 20px; }
.page-topbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 16px; }
.page-title { font-size: 22px; font-weight: 700; color: var(--ink); }
.page-subtitle { font-size: 13px; color: var(--muted); margin-top: 3px; }
.header-actions { display: flex; gap: 10px; }
.spin-icon { animation: spin 0.8s linear infinite; }
@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }

.ddns-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: 16px; }
.ddns-card { padding: 18px; display: flex; flex-direction: column; gap: 12px; }

.card-head { display: flex; align-items: center; justify-content: space-between; }
.provider-badge { display: flex; align-items: center; gap: 6px; font-weight: 600; font-size: 12.5px; color: var(--ink); }
.provider-icon { color: var(--brand-light); }

.domain-title { font-size: 15px; font-weight: 700; color: var(--ink); }

.task-details-box {
  background: var(--strip-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); padding: 10px 14px;
  display: flex; flex-direction: column; gap: 6px; font-size: 12px;
}
.detail-row { display: flex; align-items: center; justify-content: space-between; }
.detail-label { color: var(--faint); }
.detail-val { color: var(--ink); }
.ip-text { color: var(--brand-cyan); font-weight: 600; }

.card-footer { display: flex; align-items: center; justify-content: space-between; border-top: 1px solid var(--line); padding-top: 10px; }
.text-bad { color: var(--bad) !important; }

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
  box-shadow: 0 25px 60px rgba(0, 0, 0, 0.6), var(--inner-highlight);
  overflow: hidden;
}
.modal-header { padding: 18px 20px; border-bottom: 1px solid var(--line); display: flex; align-items: center; justify-content: space-between; }
.modal-title { font-size: 16px; font-weight: 700; color: var(--ink); }
.close-modal-btn { color: var(--muted); }
.modal-body { padding: 20px; display: flex; flex-direction: column; gap: 14px; }
.form-group { display: flex; flex-direction: column; gap: 6px; }
.form-label { font-size: 12px; font-weight: 600; color: var(--ink-secondary); }
.form-row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.modal-footer { display: flex; justify-content: flex-end; gap: 10px; padding-top: 8px; }

@media (max-width: 800px) {
  .ddns-grid { grid-template-columns: 1fr; }
}
</style>
