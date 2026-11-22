<template>
  <div class="report-page animate-fade-in">
    <header class="report-heading">
      <h1>SSL / TLS 证书</h1>
      <div class="report-actions">
        <button class="btn btn-secondary" :disabled="loading || isSavingConfig" @click="reload">
          刷新状态</button
        ><button
          class="btn btn-secondary"
          :disabled="!gatewayConfig || isSavingConfig"
          @click="edit(undefined, true)"
        >
          证书配置</button
        ><button class="btn btn-secondary" :disabled="!acme?.enabled || queuing || job?.running" @click="run">
          检查 / 申请证书</button
        ><button class="btn btn-primary" :disabled="!gatewayConfig || isSavingConfig" @click="edit()">
          <Plus :size="15" /> 添加证书任务
        </button>
      </div>
    </header>
    <p v-if="error || statusError" class="report-error" role="alert">{{ error || statusError }}</p>
    <p v-if="acme?.staging" class="report-warning" role="status">
      当前为测试环境，签发的证书不被浏览器信任。正式环境切换会加载各自独立的证书。
    </p>
    <div v-if="acme" class="job-summary">
      <span class="badge" :class="jobState(acme.enabled, job).tone">{{
        jobState(acme.enabled, job).label
      }}</span>
      <span v-if="job?.message" class="job-msg">{{ job.message }}</span>
      <span v-if="job?.last_success && !job.last_success.startsWith('0001')" class="job-time">{{
        reportDate(job.last_success)
      }}</span>
      <span v-if="!acme.enabled" class="cert-chip">自动签发已停用</span>
    </div>
    <div class="certs-grid">
      <SpotlightCard
        v-for="request in acme?.requests"
        :key="request.id"
        class="report-panel cert-card"
        :data-request-id="request.id"
      >
        <div class="cert-row-top">
          <div class="cert-primary">
            <Lock :size="16" class="cert-lock" aria-hidden="true" />
            <div class="cert-domains">
              <span v-for="domain in request.domains" :key="domain" class="mono-text domain-chip">{{
                domain
              }}</span>
            </div>
            <span
              class="badge"
              :class="
                certificate(request.id)?.ready ? 'badge-ok' : expired(request.id) ? 'badge-bad' : 'badge-warn'
              "
              >{{
                certificate(request.id)?.ready
                  ? '证书有效'
                  : expired(request.id)
                    ? '证书已过期'
                    : '等待有效证书'
              }}</span
            >
            <span class="cert-chip">{{ remaining(request.id) }}</span>
            <span v-if="certificate(request.id)?.staging ?? acme.staging" class="cert-chip warn">测试环境</span>
            <span v-if="!request.enabled" class="cert-chip">已暂停</span>
            <span
              v-if="!gatewayConfigSnapshot.certificate_credentials_configured?.[request.id]"
              class="cert-chip warn"
            >
              未配置 Token
            </span>
          </div>
          <div class="report-actions cert-actions">
            <button class="btn btn-secondary btn-sm" :disabled="isSavingConfig" @click="edit(request)">
              编辑任务</button
            ><button class="btn btn-ghost btn-sm" :disabled="isSavingConfig" @click="toggle(request)">
              {{ request.enabled ? '暂停续期' : '启用续期' }}</button
            ><button class="btn btn-ghost btn-sm text-bad" :disabled="isSavingConfig" @click="remove(request)">
              移除任务
            </button>
          </div>
        </div>
        <div class="cert-meta">
          <span>Cloudflare · DNS-01 · ECDSA P-256</span
          ><span v-if="certificate(request.id)?.not_after && !certificate(request.id).not_after.startsWith('0001')">
            · 到期 {{ reportDate(certificate(request.id).not_after) }}</span
          >
        </div>
      </SpotlightCard>
    </div>
    <p v-if="acme && !acme.requests.length" class="report-empty">
      尚无证书任务，可添加一个或多个普通域名与泛域名。
    </p>
    <CertificateEditor
      v-if="editor"
      :snapshot="editor.snapshot"
      :request="editor.request"
      :settings="editor.settings"
      @close="editor = null"
      @saved="saved"
    />
  </div>
</template>
<script setup>
import { computed, onMounted, watch, onUnmounted, ref, shallowRef } from 'vue'
import { Lock, Plus } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import CertificateEditor from '@/components/CertificateEditor.vue'
import { adminApi } from '@/api/admin'
import { jobState, replaceCertificate } from '@/api/dns'
import { reportDate } from '@/api/reports'
import { useGatewayStatus, useStatusPolling } from '@/composables/useGatewayStatus'
import { useToast } from '@/composables/useToast'
import '@/styles/reports.css'
const {
  gatewayConfig,
  gatewayConfigSnapshot,
  gatewayStatus,
  statusError,
  isSavingConfig,
  refreshConfig,
  refreshStatus,
  saveConfig
} = useGatewayStatus()
const toast = useToast(),
  acme = computed(() => gatewayConfig.value?.acme),
  job = computed(() => gatewayStatus.value?.jobs?.acme)
const loading = ref(false),
  error = ref(''),
  queuing = ref(false),
  editor = shallowRef(null),
  now = ref(Date.now())
const controller = new AbortController()
function certificate(id) {
  return gatewayStatus.value?.certificates.find((item) => item.id === id)
}
function remaining(id) {
  const value = certificate(id)?.not_after
  if (!value || value.startsWith('0001')) return '尚未签发'
  const days = Math.ceil((Date.parse(value) - now.value) / 86400000)
  return days <= 0 ? '已到期' : `${days} 天`
}
function expired(id) {
  const value = certificate(id)?.not_after
  return value && !value.startsWith('0001') && Date.parse(value) <= now.value
}
async function reload() {
  if (loading.value || isSavingConfig.value) return
  loading.value = true
  error.value = ''
  try {
    await Promise.all([refreshConfig(controller.signal), refreshStatus(controller.signal)])
  } catch (err) {
    if (!controller.signal.aborted) error.value = err.message
  } finally {
    loading.value = false
  }
}
function edit(request, settings = false) {
  const snapshot = structuredClone(gatewayConfigSnapshot.value)
  editor.value = {
    snapshot,
    request: request ? snapshot.config.acme.requests.find((item) => item.id === request.id) : undefined,
    settings
  }
}
async function saved() {
  editor.value = null
  toast.success('证书配置已保存')
  await reload()
}
async function toggle(request) {
  const snapshot = structuredClone(gatewayConfigSnapshot.value)
  try {
    await saveConfig(
      snapshot,
      replaceCertificate(snapshot, { ...request, enabled: !request.enabled }, request.id)
    )
    await saved()
  } catch (err) {
    toast.error(err.message)
  }
}
async function remove(request) {
  if (
    !confirm(
      `移除证书任务「${request.domains.join(', ')}」？此任务证书将不再加载，关联 HTTPS 服务可能不可用。`
    )
  )
    return
  const snapshot = structuredClone(gatewayConfigSnapshot.value)
  try {
    await saveConfig(snapshot, {
      ...snapshot.config,
      acme: {
        ...snapshot.config.acme,
        requests: snapshot.config.acme.requests.filter((item) => item.id !== request.id)
      }
    })
    await saved()
  } catch (err) {
    toast.error(err.message)
  }
}
async function run() {
  if (queuing.value) return
  queuing.value = true
  try {
    const result = await adminApi.runJobs('acme')
    toast.info(result.message || '检查任务已加入队列，请等待真实执行结果')
    await refreshStatus(controller.signal)
  } catch (err) {
    if (!controller.signal.aborted) toast.error(err.message)
  } finally {
    queuing.value = false
  }
}
useStatusPolling()
watch(gatewayStatus, () => { now.value = Date.now() })
onMounted(reload)
onUnmounted(() => controller.abort())
</script>
<style scoped>
.job-summary {
  padding: 10px 14px;
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px 12px;
  color: var(--muted);
  font-size: 12px;
}
.job-msg {
  color: var(--ink-secondary);
  overflow-wrap: anywhere;
}
.job-time {
  font-variant-numeric: tabular-nums;
}
.certs-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 14px;
}
.cert-card {
  padding: 14px 16px;
}
.cert-card :deep(.card-inner) {
  display: grid;
  gap: 10px;
}
.cert-row-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px 16px;
  min-width: 0;
}
.cert-primary {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  min-width: 0;
  flex: 1;
}
.cert-lock {
  color: var(--muted);
  flex-shrink: 0;
}
.cert-domains {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  min-width: 0;
}
.domain-chip {
  font-size: 13px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 6px;
  border: 1px solid var(--line);
  background: var(--strip-bg);
  color: var(--ink);
  overflow-wrap: anywhere;
  max-width: 100%;
}
.cert-chip {
  display: inline-flex;
  align-items: center;
  font-size: 12px;
  padding: 2px 8px;
  border-radius: 999px;
  border: 1px solid var(--line);
  background: var(--strip-bg);
  color: var(--muted);
  font-variant-numeric: tabular-nums;
}
.cert-chip.warn {
  color: var(--warn);
  border-color: var(--warn);
}
.cert-actions {
  flex-shrink: 0;
}
.cert-meta {
  font-size: 12px;
  color: var(--muted);
  font-variant-numeric: tabular-nums;
  overflow-wrap: anywhere;
}
.text-bad {
  color: var(--bad);
}
@media (max-width: 768px) {
  .cert-row-top {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
