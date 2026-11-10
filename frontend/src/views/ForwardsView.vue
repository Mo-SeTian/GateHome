<template>
  <div class="forwards-page animate-fade-in">
    <div class="page-topbar">
      <div>
        <h1 class="page-title">端口转发</h1>
        <p class="page-subtitle">管理 TCP / UDP 四层转发、来源 IP 防火墙及当前监听状态</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-secondary" :disabled="loading || isSavingConfig" @click="reload">刷新</button
        ><button class="btn btn-primary" :disabled="!gatewayConfig || isSavingConfig" @click="openForward()">
          <Plus :size="15" /> 添加转发规则
        </button>
      </div>
    </div>
    <RestartNotice />
    <p v-if="loading" role="status">正在读取转发配置…</p>
    <p v-if="loadError" class="page-error" role="alert">{{ loadError }}</p>
    <div class="protocol-filters" role="group" aria-label="筛选协议">
      <button
        v-for="item in protocols"
        :key="item.value"
        class="btn btn-secondary btn-sm"
        :aria-pressed="protocolFilter === item.value"
        @click="protocolFilter = item.value"
      >
        {{ item.label }}
      </button>
    </div>
    <details class="live-listeners ref-details">
      <summary>
        <span>当前实际系统监听（共 {{ runningListeners.length }} 条）</span>
      </summary>
      <div class="listener-list">
        <p v-for="listener in runningListeners" :key="listener.id" class="listener-item">
          <strong>{{ listener.name }}</strong>
          <span>{{ listener.protocol.toUpperCase() }} :{{ listener.listen_port }} → {{ listener.upstream }}</span>
        </p>
        <p v-if="!runningListeners.length" class="report-note">暂无端口转发监听。</p>
      </div>
    </details>
    <div class="forwards-grid">
      <SpotlightCard
        v-for="forward in filteredForwards"
        :key="forward.id"
        class="forward-card"
        :class="{ inactive: !forward.enabled }"
      >
        <div class="card-head">
          <div class="protocol-badge" :class="forward.protocol === 'tcp+udp' ? 'dual' : forward.protocol">
            {{ forward.protocol.toUpperCase() }}
          </div>
          <div class="forward-info">
            <span class="forward-name">{{ forward.name }}</span
            ><span class="traffic-tag">{{ firewallName(forward.firewall_id) }}</span>
          </div>
          <label class="switch"
            ><input
              type="checkbox"
              :checked="forward.enabled"
              :disabled="isSavingConfig"
              :aria-label="`启用 ${forward.name}`"
              @change="toggleForward(forward, $event)" /><span class="switch-slider"
          /></label>
        </div>
        <div class="pipeline-box">
          <div class="pipeline-node">
            <span class="node-label">所有网卡监听</span
            ><span class="node-val mono-text">:{{ forward.listen_port }}</span>
          </div>
          <ArrowRight :size="15" />
          <div class="pipeline-node">
            <span class="node-label">转发目标</span
            ><span class="node-val mono-text">{{ forward.upstream }}</span>
          </div>
        </div>
        <p v-if="forward.protocol !== 'tcp'" class="traffic-tag">
          UDP 空闲超时 {{ forward.udp_idle_seconds || 300 }} 秒
        </p>
        <div class="card-footer">
          <span class="status-indicator"
            ><span
              class="status-dot"
              :class="forwardState(forward, gatewayStatus?.forward_listeners).tone"
            />{{ forwardState(forward, gatewayStatus?.forward_listeners).label }}</span
          >
          <div class="header-actions">
            <button class="btn btn-ghost btn-sm" :disabled="isSavingConfig" @click="openForward(forward)">
              <Edit3 :size="13" /> 编辑</button
            ><button
              class="btn btn-ghost btn-sm text-bad"
              :disabled="isSavingConfig"
              :aria-label="`删除 ${forward.name}`"
              @click="deleteForward(forward)"
            >
              <Trash2 :size="13" />
            </button>
          </div>
        </div>
      </SpotlightCard>
    </div>
    <p v-if="gatewayConfig && !filteredForwards.length" class="empty-message">暂无匹配的端口转发规则。</p>

    <dialog ref="dialog" class="modal-dialog" aria-labelledby="forward-title" @cancel="blockBusyClose">
      <div class="modal-header">
        <h2 id="forward-title" class="modal-title">{{ originalId == null ? '添加' : '编辑' }}端口转发规则</h2>
        <button class="close-modal-btn" aria-label="关闭" :disabled="isSavingConfig" @click="dialog.close()">
          <X :size="18" />
        </button>
      </div>
      <form class="modal-body" @submit.prevent="saveForward">
        <p v-if="formError" ref="errorElement" class="page-error" role="alert" tabindex="-1">
          {{ formError }}
        </p>
        <label class="form-group"
          >规则名称<input
            v-model="form.name"
            name="name"
            class="form-input"
            required
            maxlength="100"
        /></label>
        <div class="form-row-2">
          <label class="form-group"
            >传输协议<select v-model="form.protocol" name="protocol" class="form-select">
              <option value="tcp">TCP</option>
              <option value="udp">UDP</option>
              <option value="tcp+udp">TCP + UDP</option>
            </select></label
          >
          <label class="form-group"
            >本地监听端口<input
              v-model.number="form.listen_port"
              name="listen_port"
              type="number"
              min="1024"
              max="65535"
              class="form-input"
              required
          /></label>
        </div>
        <label class="form-group"
          >目标主机与端口<input
            v-model="form.upstream"
            name="upstream"
            class="form-input mono-text"
            placeholder="192.168.1.10:9000 或 [::1]:9000"
            required
        /></label>
        <label class="form-group"
          >来源 IP 防火墙<select v-model="form.firewall_id" name="firewall_id" class="form-select">
            <option value="">不限制来源 IP</option>
            <option v-for="firewall in snapshot?.config.firewalls" :key="firewall.id" :value="firewall.id">
              {{ firewall.name }}
            </option>
          </select></label
        >
        <label v-if="form.protocol !== 'tcp'" class="form-group"
          >UDP 空闲超时 秒<input
            v-model.number="form.udp_idle_seconds"
            name="udp_idle_seconds"
            class="form-input"
            type="number"
            min="30"
            max="86400"
            required
        /></label>
        <label class="toggle-row">
          <span>启用此转发规则</span>
          <input v-model="form.enabled" name="enabled" type="checkbox" />
        </label>
        <p class="form-note">保存后重启服务使监听配置生效。Docker bridge 部署需发布对应协议的端口。</p>
        <div class="modal-footer">
          <button type="button" class="btn btn-secondary" :disabled="isSavingConfig" @click="dialog.close()">
            取消</button
          ><button type="submit" class="btn btn-primary" :disabled="isSavingConfig">
            {{ isSavingConfig ? '保存中…' : '保存转发规则' }}
          </button>
        </div>
      </form>
    </dialog>
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, shallowRef } from 'vue'
import { ArrowRight, Edit3, Plus, Trash2, X } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import RestartNotice from '@/components/RestartNotice.vue'
import { forwardState, replaceConfigItem } from '@/api/configuration'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useToast } from '@/composables/useToast'

const {
  gatewayConfig,
  gatewayConfigSnapshot,
  gatewayStatus,
  isSavingConfig,
  refreshConfig,
  refreshStatus,
  saveConfig
} = useGatewayStatus()
const toast = useToast()
const forwards = computed(() => gatewayConfig.value?.forwards ?? [])
const runningListeners = computed(() =>
  (gatewayStatus.value?.forward_listeners ?? []).filter((item) => item.enabled)
)
const protocols = [
  { value: '', label: '全部协议' },
  { value: 'tcp', label: 'TCP' },
  { value: 'udp', label: 'UDP' },
  { value: 'tcp+udp', label: 'TCP + UDP' }
]
const protocolFilter = ref('')
const filteredForwards = computed(() =>
  forwards.value.filter(
    (item) =>
      !protocolFilter.value ||
      item.protocol === protocolFilter.value ||
      (item.protocol === 'tcp+udp' && protocolFilter.value !== 'tcp+udp')
  )
)
const loading = ref(false)
const loadError = ref('')
const dialog = ref(null)
const snapshot = shallowRef(null)
const originalId = ref(null)
const form = ref({})
const formError = ref('')
const errorElement = ref(null)
const controller = new AbortController()

function firewallName(id) {
  return gatewayConfig.value?.firewalls.find((item) => item.id === id)?.name || '不限制来源 IP'
}
async function reload() {
  if (loading.value || isSavingConfig.value) return
  loading.value = true
  loadError.value = ''
  try {
    await Promise.all([refreshConfig(controller.signal), refreshStatus(controller.signal)])
  } catch (err) {
    if (!controller.signal.aborted) loadError.value = err.message
  } finally {
    loading.value = false
  }
}
function openForward(forward) {
  snapshot.value = structuredClone(gatewayConfigSnapshot.value)
  originalId.value = forward?.id ?? null
  form.value = forward
    ? structuredClone(forward)
    : {
        id: crypto.randomUUID(),
        name: '',
        protocol: 'tcp',
        listen_port: '',
        upstream: '',
        firewall_id: '',
        enabled: true
      }
  form.value.udp_idle_seconds ||= 300
  formError.value = ''
  nextTick(() => dialog.value.showModal())
}
function blockBusyClose(event) {
  if (isSavingConfig.value) event.preventDefault()
}
async function saved() {
  toast.success('转发配置已保存，监听变化在重启后生效')
  try {
    await refreshStatus(controller.signal)
  } catch (err) {
    if (!controller.signal.aborted && err.status !== 401)
      toast.warn('配置已保存，监听状态刷新失败，请稍后刷新')
  }
}
async function saveForward() {
  if (isSavingConfig.value) return
  formError.value = ''
  try {
    const record = { ...form.value, name: form.value.name.trim(), upstream: form.value.upstream.trim() }
    if (record.protocol === 'tcp') delete record.udp_idle_seconds
    await saveConfig(snapshot.value, replaceConfigItem(snapshot.value, 'forwards', record, originalId.value))
    dialog.value.close()
    await saved()
  } catch (err) {
    formError.value = err.message
    await nextTick()
    errorElement.value?.focus()
  }
}
async function toggleForward(forward, event) {
  event.target.checked = forward.enabled
  if (isSavingConfig.value) return
  const current = structuredClone(gatewayConfigSnapshot.value)
  try {
    await saveConfig(
      current,
      replaceConfigItem(current, 'forwards', { ...forward, enabled: !forward.enabled }, forward.id)
    )
    await saved()
  } catch (err) {
    toast.error(err.message)
  }
}
async function deleteForward(forward) {
  if (isSavingConfig.value || !confirm(`删除端口转发「${forward.name}」？`)) return
  const current = structuredClone(gatewayConfigSnapshot.value)
  try {
    await saveConfig(current, {
      ...current.config,
      forwards: current.config.forwards.filter((item) => item.id !== forward.id)
    })
    await saved()
  } catch (err) {
    toast.error(err.message)
  }
}
onMounted(reload)
onUnmounted(() => controller.abort())
</script>

<style scoped>
.header-actions, .protocol-filters { display: flex; flex-wrap: wrap; gap: 8px; }
.protocol-filters [aria-pressed="true"] { border-color: var(--brand-light); color: var(--ink); }
.live-listeners { color: var(--muted); font-size: 13px; overflow-wrap: anywhere; }
.live-listeners summary { cursor: pointer; }
.page-error { color: var(--bad); font-size: 13px; overflow-wrap: anywhere; }
.empty-message, .form-note { color: var(--muted); font-size: 13px; }
.toggle-row { display: flex; justify-content: space-between; color: var(--ink); font-size: 13px; }
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
  grid-template-columns: repeat(auto-fill, minmax(360px, 1fr));
  gap: 16px;
}

.forward-card {
  padding: 18px;
  display: flex;
  flex-direction: column;
  min-height: 220px;
}
.forward-card, .pipeline-node { min-width: 0; }
.forward-card :deep(.card-inner) {
  display: flex;
  flex-direction: column;
  gap: 14px;
  height: 100%;
}
.node-val { overflow-wrap: anywhere; }
.protocol-badge.dual { background: var(--strip-bg); color: var(--brand-light); border: 1px solid var(--brand); }
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
  margin-top: auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-top: 1px solid var(--line);
  padding-top: 12px;
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
.modal-dialog {
  width: 100%; max-width: 460px; background: var(--card-hover-bg);
  border: 1px solid var(--card-border-hover); border-radius: var(--radius-card);
  backdrop-filter: blur(30px); -webkit-backdrop-filter: blur(30px);
  box-shadow: 0 25px 60px rgba(0, 0, 0, 0.6), var(--inner-highlight);
  margin: auto; max-height: calc(100dvh - 40px); overflow: auto; color: var(--ink);
}
.modal-dialog::backdrop { background: rgba(0,0,0,0.65); backdrop-filter: blur(8px); }
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
@media (max-width: 640px) {
  .page-topbar { flex-direction: column; align-items: stretch; gap: 12px; }
  .header-actions { width: 100%; }
  .header-actions .btn { flex: 1; justify-content: center; }
  .protocol-filters { width: 100%; display: flex; }
  .protocol-filters .btn { flex: 1; text-align: center; padding-inline: 6px; }
}
</style>
