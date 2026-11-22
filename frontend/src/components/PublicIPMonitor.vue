<template>
  <SpotlightCard class="report-panel">
    <div class="report-heading monitor-heading">
      <div class="monitor-title-wrap">
        <h2>公网 IP 与网卡</h2>
        <span v-if="resultInput.group_id" class="monitor-chip">{{ groupName(resultInput.group_id) }}</span>
        <span v-if="network?.ip.running" class="monitor-chip">检测中…</span>
      </div>
      <div class="report-actions">
        <button class="btn btn-secondary btn-sm" :disabled="loading" @click="read(true)">检测公网 IP</button
        ><button class="btn btn-secondary btn-sm" :disabled="!gatewayConfig || isSavingConfig" @click="edit">
          检测设置
        </button>
      </div>
    </div>
    <p v-if="error" class="report-error" role="alert">{{ error }}</p>
    <div class="ip-results">
      <div v-for="family in ['ipv4', 'ipv6']" :key="family">
        <div class="ip-row-top">
          <strong>{{ family === 'ipv4' ? 'IPv4' : 'IPv6' }}</strong>
          <span class="ip-iface mono-text">
            {{ network?.ip[family]?.interface || '—' }} · {{ network?.ip[family]?.local_address || '—' }}
          </span>
        </div>
        <p class="ip-addr mono-text">
          {{
            network?.mode === (family === 'ipv4' ? 'ipv6' : 'ipv4')
              ? '未启用此类型'
              : network?.ip[family]?.address || '未获取'
          }}
        </p>
        <p v-if="network?.ip[family]?.error" class="report-error">
          {{ network.ip[family].address ? '保留上次有效地址 · ' : '' }}{{ network.ip[family].error }}
        </p>
        <small class="ip-meta">
          {{ reportDate(network?.ip[family]?.checked_at)
          }}<template v-if="network?.ip[family]?.endpoint"> · {{ network.ip[family].endpoint }}</template
          ><template v-if="network?.ip[family]?.attempts > 1"> · {{ network.ip[family].attempts }} 次</template>
        </small>
      </div>
    </div>
    <dialog
      ref="dialog"
      class="report-dialog"
      aria-labelledby="monitor-title"
      @cancel="blockBusyClose"
      @close="cancelPreview"
    >
      <header>
        <h2 id="monitor-title">公网 IP 检测设置</h2>
        <button class="close-modal-btn" aria-label="关闭" :disabled="isSavingConfig" @click="dialog.close()">
          <X :size="18" />
        </button>
      </header>
      <form class="modal-body" @submit.prevent="save">
        <p v-if="formError" ref="errorElement" class="report-error" role="alert" tabindex="-1">
          {{ formError }}
        </p>
        <fieldset :disabled="isSavingConfig" class="dialog-fieldset">
          <label class="form-field"
            >检测配置<select v-model="draft.group_id" name="monitor_group_id" class="form-select">
              <option value="">本机检测</option>
              <option v-for="group in gatewayConfig?.ddns.groups" :key="group.id" :value="group.id">
                {{ group.name }}
              </option>
            </select></label
          ><IPSourceFields v-if="!draft.group_id" :settings="draft" :interfaces="network?.interfaces" />
          <p class="report-note">
            本机检测使用默认 IP 查询接口；指定 DDNS 组时使用该组的网卡和接口。保存后刷新与重新登录会沿用。
          </p>
        </fieldset>
        <div class="modal-footer">
          <button type="button" class="btn btn-secondary" :disabled="loading" @click="preview">
            预览检测
          </button>
          <button type="submit" class="btn btn-primary" :disabled="isSavingConfig">
            {{ isSavingConfig ? '保存中…' : '保存检测设置' }}
          </button>
        </div>
      </form>
    </dialog>
  </SpotlightCard>
</template>
<script setup>
import { nextTick, onMounted, onUnmounted, ref, shallowRef, watch } from 'vue'
import { X } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import IPSourceFields from '@/components/IPSourceFields.vue'
import { adminApi } from '@/api/admin'
import { reportDate } from '@/api/reports'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useToast } from '@/composables/useToast'
const { gatewayConfig, gatewayConfigSnapshot, isSavingConfig, saveConfig } = useGatewayStatus()
const emit = defineEmits(['network'])
const resultInput = shallowRef({})
const toast = useToast(),
  network = shallowRef(null),
  input = shallowRef({}),
  draft = ref({}),
  snapshot = shallowRef(null),
  dialog = ref(null)
const loading = ref(false),
  error = ref(''),
  formError = ref(''),
  errorElement = ref(null)
const controller = new AbortController()
let version = 0,
  timer
function savedInput() {
  return {
    group_id: '',
    interface: '',
    ipv4_source: 'url',
    ipv6_source: 'url',
    ...gatewayConfig.value?.ddns.ip_monitor
  }
}
function groupName(id) {
  return gatewayConfig.value?.ddns.groups.find((item) => item.id === id)?.name || id
}
async function read(force = false) {
  const request = ++version,
    settings = { ...input.value }
  loading.value = true
  try {
    const result = await adminApi.ddnsNetwork(settings, force, controller.signal)
    if (!controller.signal.aborted && request === version) {
      network.value = result
      resultInput.value = settings
      error.value = ''
      emit('network', result)
    }
  } catch (err) {
    if (!controller.signal.aborted && request === version) error.value = err.message
  } finally {
    if (request === version) loading.value = false
  }
}
function edit() {
  snapshot.value = structuredClone(gatewayConfigSnapshot.value)
  draft.value = savedInput()
  formError.value = ''
  dialog.value.showModal()
}
function preview() {
  input.value = { ...draft.value }
  read(true)
}
function cancelPreview() {
  input.value = savedInput()
  read()
}
function blockBusyClose(event) {
  if (isSavingConfig.value) event.preventDefault()
}
async function save() {
  if (isSavingConfig.value) return
  formError.value = ''
  try {
    const config = structuredClone(snapshot.value.config)
    config.ddns.ip_monitor = { ...draft.value }
    await saveConfig(snapshot.value, config)
    dialog.value.close()
    toast.success('公网 IP 检测设置已保存')
  } catch (err) {
    formError.value = err.message
    await nextTick()
    errorElement.value?.focus()
  }
}
watch(
  gatewayConfig,
  () => {
    if (!dialog.value?.open) {
      input.value = savedInput()
      read()
    }
  },
  { immediate: true }
)
onMounted(() => {
  timer = setInterval(() => read(), 5000)
})
onUnmounted(() => {
  clearInterval(timer)
  controller.abort()
})
</script>
<style scoped>
.monitor-heading h2 {
  margin-bottom: 0;
}
.monitor-title-wrap {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.monitor-chip {
  font-size: 12px;
  padding: 2px 8px;
  border-radius: 999px;
  background: var(--strip-bg);
  border: 1px solid var(--line);
  color: var(--muted);
}
.ip-results {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
  margin-top: 14px;
}
.ip-results > div {
  padding: 12px 14px;
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  min-width: 0;
  display: grid;
  gap: 6px;
}
.ip-row-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}
.ip-iface {
  font-size: 12px;
  color: var(--muted);
  overflow-wrap: anywhere;
}
.ip-addr {
  font-size: 14px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  overflow-wrap: anywhere;
  margin: 0;
}
.ip-results .report-error {
  margin: 0;
}
.ip-meta {
  display: block;
  color: var(--muted);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  overflow-wrap: anywhere;
}
@media (max-width: 600px) {
  .ip-results {
    grid-template-columns: 1fr;
  }
}
</style>
