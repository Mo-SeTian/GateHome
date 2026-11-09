<template>
  <div class="report-page animate-fade-in">
    <div class="report-heading">
      <div>
        <h1>SSL / TLS 证书管理</h1>
        <p>按任务管理普通域名与泛域名证书，Cloudflare DNS-01 自动验证</p>
      </div>
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
    </div>
    <p v-if="error || pollError" class="report-error" role="alert">{{ error || pollError }}</p>
    <p v-if="acme?.staging" class="report-warning" role="status">
      当前为测试环境，签发的证书不被浏览器信任。正式环境切换会加载各自独立的证书。
    </p>
    <div v-if="acme" class="job-summary">
      <span class="badge" :class="jobState(acme.enabled, job).tone">{{
        jobState(acme.enabled, job).label
      }}</span>
      <p>{{ job?.message || '尚未运行证书检查' }}</p>
      <p class="report-note">
        最近检查 {{ reportDate(job?.last_run) }} · 最近成功 {{ reportDate(job?.last_success) }} · 自动签发{{
          acme.enabled ? '已启用' : '已停用'
        }}
      </p>
    </div>
    <p class="report-note">
      全局检查只处理已启用且需要申请或续期的任务，每小时自动检查。停用任务只暂停申请与续期，已有有效证书继续加载；移除任务会停止加载该任务证书。
    </p>
    <div class="certs-grid">
      <SpotlightCard
        v-for="request in acme?.requests"
        :key="request.id"
        class="report-panel cert-card"
        :data-request-id="request.id"
      >
        <div class="report-heading">
          <Lock :size="18" /><span
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
        </div>
        <p class="domains mono-text">{{ request.domains.join('\n') }}</p>
        <dl>
          <div>
            <dt>DNS 验证</dt>
            <dd>Cloudflare · DNS-01</dd>
          </div>
          <div>
            <dt>申请算法</dt>
            <dd>ECDSA P-256</dd>
          </div>
          <div>
            <dt>独立凭据</dt>
            <dd>
              {{
                gatewayConfigSnapshot.certificate_credentials_configured?.[request.id] ? '已配置' : '尚未配置'
              }}
            </dd>
          </div>
          <div>
            <dt>签发环境</dt>
            <dd>{{ (certificate(request.id)?.staging ?? acme.staging) ? '测试环境' : '正式环境' }}</dd>
          </div>
          <div>
            <dt>到期时间</dt>
            <dd>{{ reportDate(certificate(request.id)?.not_after) }}</dd>
          </div>
          <div>
            <dt>剩余时间</dt>
            <dd>{{ remaining(request.id) }}</dd>
          </div>
          <div>
            <dt>申请与续期</dt>
            <dd>{{ request.enabled ? '任务已启用' : '任务已暂停' }}</dd>
          </div>
        </dl>
        <div class="report-actions">
          <button class="btn btn-secondary btn-sm" :disabled="isSavingConfig" @click="edit(request)">
            编辑任务</button
          ><button class="btn btn-ghost btn-sm" :disabled="isSavingConfig" @click="toggle(request)">
            {{ request.enabled ? '暂停续期' : '启用续期' }}</button
          ><button class="btn btn-ghost btn-sm text-bad" :disabled="isSavingConfig" @click="remove(request)">
            移除任务
          </button>
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
import { computed, onMounted, onUnmounted, ref, shallowRef } from 'vue'
import { Lock, Plus } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import CertificateEditor from '@/components/CertificateEditor.vue'
import { adminApi } from '@/api/admin'
import { jobState, replaceCertificate } from '@/api/dns'
import { reportDate } from '@/api/reports'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useToast } from '@/composables/useToast'
import '@/styles/reports.css'
const {
  gatewayConfig,
  gatewayConfigSnapshot,
  gatewayStatus,
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
  pollError = ref(''),
  queuing = ref(false),
  editor = shallowRef(null),
  now = ref(Date.now())
const controller = new AbortController()
let timer
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
onMounted(() => {
  reload()
  timer = setInterval(async () => {
    now.value = Date.now()
    try {
      await refreshStatus(controller.signal)
      if (!controller.signal.aborted) pollError.value = ''
    } catch (err) {
      if (!controller.signal.aborted) pollError.value = err.message
    }
  }, 5000)
})
onUnmounted(() => {
  clearInterval(timer)
  controller.abort()
})
</script>
<style scoped>
.certs-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  align-items: start;
  gap: 16px;
}
.cert-card :deep(.card-inner) {
  display: grid;
  gap: 14px;
}
.domains {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-size: 14px;
}
.cert-card dl {
  display: grid;
  gap: 10px;
  font-size: 12px;
}
.cert-card dl > div {
  display: flex;
  justify-content: space-between;
  gap: 14px;
  flex-wrap: wrap;
}
dt,
.job-summary {
  color: var(--muted);
}
dd {
  margin: 0;
  color: var(--ink-secondary);
}
.job-summary {
  padding: 16px;
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  display: grid;
  gap: 10px;
}
.text-bad {
  color: var(--bad);
}
.job-summary > .badge {
  justify-self: start;
}
@media (max-width: 900px) {
  .certs-grid {
    grid-template-columns: 1fr;
  }
}
</style>
