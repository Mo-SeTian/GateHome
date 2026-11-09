<template>
  <div class="forwards-page animate-fade-in">
    <div class="page-topbar">
      <div>
        <h1 class="page-title">端口转发</h1>
        <p class="page-subtitle">配置 TCP / UDP 四层流量透明代理与端口映射规则</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-primary" @click="openCreateModal">
          <Plus :size="15" />
          添加转发规则
        </button>
      </div>
    </div>

    <!-- Forwards Grid -->
    <div class="forwards-grid">
      <SpotlightCard 
        v-for="fwd in forwards" 
        :key="fwd.id" 
        class="forward-card"
        :class="{ inactive: !fwd.enabled }"
      >
        <div class="card-head">
          <div class="protocol-badge" :class="fwd.protocol.toLowerCase()">
            {{ fwd.protocol }}
          </div>
          <div class="forward-info">
            <span class="forward-name">{{ fwd.name }}</span>
            <span class="traffic-tag">已穿透 {{ fwd.traffic }}</span>
          </div>
          <label class="switch">
            <input 
              type="checkbox" 
              :checked="fwd.enabled" 
              @change="toggleForward(fwd)" 
            />
            <span class="switch-slider" />
          </label>
        </div>

        <!-- Connection Pipeline Diagram -->
        <div class="pipeline-box">
          <div class="pipeline-node">
            <span class="node-label">本地监听</span>
            <span class="node-val mono-text">:{{ fwd.listenPort }}</span>
          </div>
          <div class="pipeline-arrow">
            <ArrowRight :size="15" />
          </div>
          <div class="pipeline-node">
            <span class="node-label">转发目标</span>
            <span class="node-val mono-text">{{ fwd.target }}</span>
          </div>
        </div>

        <div class="card-footer">
          <span class="status-indicator">
            <span class="status-dot" :class="fwd.enabled ? 'ok' : 'neutral'" />
            {{ fwd.enabled ? '转发监听中' : '已暂停' }}
          </span>
          <button class="btn btn-ghost btn-sm text-bad" @click="deleteForward(fwd)">
            <Trash2 :size="13" />
          </button>
        </div>
      </SpotlightCard>
    </div>

    <!-- Create Forward Modal -->
    <transition name="modal-fade">
      <div v-if="isModalOpen" class="modal-backdrop" @click.self="isModalOpen = false">
        <div class="modal-dialog">
          <div class="modal-header">
            <h3 class="modal-title">添加端口转发规则</h3>
            <button class="close-modal-btn" @click="isModalOpen = false">
              <X :size="18" />
            </button>
          </div>

          <form class="modal-body" @submit.prevent="createForward">
            <div class="form-group">
              <label class="form-label">规则名称</label>
              <input v-model="form.name" type="text" placeholder="例如: 幻兽帕鲁服务器" class="form-input" required />
            </div>

            <div class="form-row-2">
              <div class="form-group">
                <label class="form-label">传输协议</label>
                <select v-model="form.protocol" class="form-select">
                  <option value="TCP">TCP</option>
                  <option value="UDP">UDP</option>
                  <option value="TCP+UDP">TCP + UDP (双栈)</option>
                </select>
              </div>
              <div class="form-group">
                <label class="form-label">本地监听端口</label>
                <input v-model="form.listenPort" type="number" placeholder="8211" class="form-input mono-text" required />
              </div>
            </div>

            <div class="form-group">
              <label class="form-label">目标主机与端口</label>
              <input v-model="form.target" type="text" placeholder="192.168.2.160:8211" class="form-input mono-text" required />
            </div>

            <div class="modal-footer">
              <button type="button" class="btn btn-secondary" @click="isModalOpen = false">取消</button>
              <button type="submit" class="btn btn-primary">立即添加</button>
            </div>
          </form>
        </div>
      </div>
    </transition>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { Plus, ArrowRight, Trash2, X } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { useAppStore } from '@/composables/useAppStore'
import { useToast } from '@/composables/useToast'

const { forwards } = useAppStore()
const { success, info } = useToast()

const isModalOpen = ref(false)
const form = ref({
  name: '',
  protocol: 'TCP',
  listenPort: '',
  target: ''
})

const openCreateModal = () => {
  form.value = {
    name: '',
    protocol: 'TCP',
    listenPort: '',
    target: '192.168.2.'
  }
  isModalOpen.value = true
}

const createForward = () => {
  forwards.value.push({
    id: `fwd_${Date.now()}`,
    name: form.value.name,
    protocol: form.value.protocol,
    listenPort: Number(form.value.listenPort),
    target: form.value.target,
    traffic: '0 MB',
    enabled: true
  })
  success(`端口转发规则 "${form.value.name}" 已启动`)
  isModalOpen.value = false
}

const toggleForward = (fwd) => {
  fwd.enabled = !fwd.enabled
  if (fwd.enabled) {
    success(`已开启端口转发: :${fwd.listenPort} -> ${fwd.target}`)
  } else {
    info(`已关闭端口转发: :${fwd.listenPort}`)
  }
}

const deleteForward = (fwd) => {
  if (confirm(`确定要移除 "${fwd.name}" 端口转发规则吗？`)) {
    forwards.value = forwards.value.filter(f => f.id !== fwd.id)
    info(`已移除端口转发规则: ${fwd.name}`)
  }
}
</script>

<style scoped>
.forwards-page {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.page-topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 16px;
}
.page-title { font-size: 22px; font-weight: 700; color: var(--ink); }
.page-subtitle { font-size: 13px; color: var(--muted); margin-top: 3px; }

.forwards-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 16px;
}

.forward-card {
  padding: 18px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.forward-card.inactive {
  opacity: 0.65;
}

.card-head {
  display: flex;
  align-items: center;
  gap: 12px;
}

.protocol-badge {
  padding: 4px 8px;
  border-radius: 6px;
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.5px;
}
.protocol-badge.tcp { background: rgba(99, 102, 241, 0.15); color: var(--brand-light); border: 1px solid rgba(99, 102, 241, 0.3); }
.protocol-badge.udp { background: rgba(6, 182, 212, 0.15); color: var(--brand-cyan); border: 1px solid rgba(6, 182, 212, 0.3); }

.forward-info {
  flex: 1;
  display: flex;
  flex-direction: column;
}
.forward-name { font-size: 13.5px; font-weight: 600; color: var(--ink); }
.traffic-tag { font-size: 11px; color: var(--faint); font-family: var(--mono); }

.pipeline-box {
  background: var(--strip-bg);
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  padding: 12px 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.pipeline-node {
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.node-label { font-size: 10.5px; color: var(--faint); }
.node-val { font-size: 13px; font-weight: 600; color: var(--ink); }

.pipeline-arrow {
  color: var(--brand-light);
  display: flex;
  align-items: center;
}

.card-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-top: 1px solid var(--line);
  padding-top: 10px;
}

.status-indicator {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11.5px;
  color: var(--muted);
}

.text-bad { color: var(--bad) !important; }

/* Modal */
.modal-backdrop {
  position: fixed; inset: 0; background: rgba(0,0,0,0.65);
  backdrop-filter: blur(8px); -webkit-backdrop-filter: blur(8px);
  z-index: 1050; display: flex; align-items: center; justify-content: center; padding: 20px;
}
.modal-dialog {
  width: 100%; max-width: 460px; background: var(--card-bg);
  border: 1px solid var(--card-border-hover); border-radius: var(--radius-card);
  backdrop-filter: blur(30px); -webkit-backdrop-filter: blur(30px);
  box-shadow: 0 25px 60px rgba(0, 0, 0, 0.6), var(--inner-highlight);
  overflow: hidden;
}
.modal-header {
  padding: 18px 20px; border-bottom: 1px solid var(--line);
  display: flex; align-items: center; justify-content: space-between;
}
.modal-title { font-size: 16px; font-weight: 700; color: var(--ink); }
.close-modal-btn { color: var(--muted); }
.modal-body { padding: 20px; display: flex; flex-direction: column; gap: 14px; }
.form-group { display: flex; flex-direction: column; gap: 6px; }
.form-label { font-size: 12px; font-weight: 600; color: var(--ink-secondary); }
.form-row-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.modal-footer { display: flex; justify-content: flex-end; gap: 10px; padding-top: 8px; }

@media (max-width: 900px) {
  .forwards-grid { grid-template-columns: 1fr; }
}
</style>
