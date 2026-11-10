<template>
  <div class="report-page ddns-page animate-fade-in">
    <div class="report-heading">
      <div>
        <h1>动态域名解析 DDNS</h1>
        <p>每组独立配置 Cloudflare 账户与域名，自动同步 IPv4 / IPv6，仅修改 DNS 灰云记录</p>
      </div>
      <div class="report-actions">
        <button class="btn btn-secondary" :disabled="loading || isSavingConfig" @click="reload(true)">
          刷新配置与解析</button
        ><button
          class="btn btn-secondary"
          :disabled="queuing || !groups.some((item) => item.enabled)"
          @click="sync()"
        >
          同步全部启用组</button
        ><button
          class="btn btn-primary"
          :disabled="!gatewayConfig || loading || isSavingConfig"
          @click="edit()"
        >
          <Plus :size="15" /> 添加 DDNS 组
        </button>
      </div>
    </div>
    <p v-if="error" class="report-error" role="alert">{{ error }}</p>
    <p v-if="pollError || statusError" class="report-error" role="alert">{{ pollError || statusError }}</p>
    <PublicIPMonitor v-if="gatewayConfig" @network="network = $event" />
    <p class="report-note">
      公网 IP 查询保持直连。保存后立即生效；同一 A / AAAA 记录不可跨组重复。删除组或域名不会删除托管商已有的
      DNS 记录。
    </p>
    <div class="ddns-grid">
      <SpotlightCard
        v-for="group in groups"
        :key="group.id"
        class="report-panel ddns-card"
        :data-group-id="group.id"
      >
        <div class="report-heading">
          <strong>{{ group.name }}</strong
          ><span class="badge" :class="state(group).tone">{{ state(group).label }}</span>
        </div>
        <p class="mono-text domain-title">{{ group.zone }} · {{ modeLabel(group.mode) }}</p>
        <p class="report-note">
          Cloudflare · 每 {{ group.interval.toLocaleString() }} 秒 ·
          {{
            gatewayConfigSnapshot.dns_credentials_configured?.[group.id]
              ? '独立 Token 已配置'
              : '尚未配置 Token'
          }}
        </p>
        <p class="report-note">{{ job(group.id)?.message || '此组尚未运行' }}</p>
        <p class="report-note">
          检查 {{ reportDate(job(group.id)?.last_run) }} · 成功 {{ reportDate(job(group.id)?.last_success) }}
        </p>
        <div v-if="network?.groups[group.id]" class="group-ips">
          <p v-for="family in families(group.mode)" :key="family" class="report-note">
            {{ family === 'ipv4' ? 'IPv4' : 'IPv6' }} 检测：<span class="mono-text">{{
              network.groups[group.id][family].address || '未获取'
            }}</span
            ><span v-if="network.groups[group.id][family].error" class="report-error">
              · {{ network.groups[group.id][family].error }}</span
            >
          </p>
        </div>
        <div class="report-table-wrap" tabindex="0" role="region" :aria-label="`${group.name} DNS 记录，可横向滚动`">
          <table class="report-table dns-records">
            <thead>
              <tr>
                <th>域名</th>
                <th>公网解析 A / AAAA</th>
                <th>最近查询</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="host in group.hosts" :key="host">
                <td class="mono-text">{{ host }}</td>
                <td>
                  <div v-for="family in ['ipv4', 'ipv6']" :key="family">
                    <span>{{ family === 'ipv4' ? 'A' : 'AAAA' }}: </span
                    ><span :class="{ 'report-error': record(group.id, host)?.[family + '_error'] }">{{
                      address(group.id, host, family)
                    }}</span>
                  </div>
                </td>
                <td>{{ reportDate(record(group.id, host)?.checked_at) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <details v-if="references(group.id).length" class="ref-details">
          <summary>
            <span>关联 {{ references(group.id).length }} 个反代组</span>
          </summary>
          <div class="ref-pills-wrap">
            <span v-for="name in references(group.id)" :key="name" class="ref-pill">{{ name }}</span>
          </div>
        </details>
        <div class="report-actions card-actions">
          <button
            class="btn btn-secondary btn-sm"
            :disabled="!group.enabled || queuing || job(group.id)?.running"
            @click="sync(group.id)"
          >
            单独同步</button
          ><button class="btn btn-secondary btn-sm" :disabled="isSavingConfig" @click="edit(group, true)">
            编辑域名</button
          ><button class="btn btn-ghost btn-sm" :disabled="isSavingConfig" @click="edit(group)">编辑组</button
          ><button class="btn btn-ghost btn-sm" :disabled="isSavingConfig" @click="toggle(group)">
            {{ group.enabled ? '停用' : '启用' }}</button
          ><button
            class="btn btn-ghost btn-sm text-bad"
            :disabled="
              isSavingConfig ||
              references(group.id).length > 0 ||
              gatewayConfig.ddns.ip_monitor.group_id === group.id
            "
            :title="'先解除反代组和公网检测引用，再删除'"
            @click="remove(group)"
          >
            删除
          </button>
        </div>
      </SpotlightCard>
    </div>
    <p v-if="gatewayConfig && !groups.length" class="report-empty">
      尚无 DDNS 任务组，可先停用保存账户配置，再验证并启用。
    </p>
    <DdnsEditor
      v-if="editor"
      :snapshot="editor.snapshot"
      :group="editor.group"
      :hosts-only="editor.hostsOnly"
      :interfaces="network?.interfaces"
      :defaults="gatewayStatus?.ip_query_defaults"
      @close="editor = null"
      @saved="saved"
    />
  </div>
</template>
<script setup>
import { computed, onMounted, onUnmounted, ref, shallowRef } from 'vue'
import { Plus } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import DdnsEditor from '@/components/DdnsEditor.vue'
import PublicIPMonitor from '@/components/PublicIPMonitor.vue'
import { adminApi } from '@/api/admin'
import { startPolling } from '@/api/polling'
import { jobState, replaceDDNS } from '@/api/dns'
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
  groups = computed(() => gatewayConfig.value?.ddns.groups ?? [])
const loading = ref(false),
  error = ref(''),
  pollError = ref(''),
  queuing = ref(false),
  records = shallowRef({}),
  network = shallowRef(null),
  editor = shallowRef(null)
const controller = new AbortController()
let recordsVersion = 0,
  timer
function job(id) {
  return gatewayStatus.value?.jobs?.['ddns:' + id]
}
function state(group) {
  return jobState(group.enabled, job(group.id))
}
function modeLabel(mode) {
  return { ipv4: 'IPv4 · A', ipv6: 'IPv6 · AAAA', dual: 'IPv4 + IPv6' }[mode]
}
function families(mode) {
  return mode === 'dual' ? ['ipv4', 'ipv6'] : [mode]
}
function references(id) {
  return (gatewayConfig.value?.groups ?? [])
    .filter((item) => item.ddns_group_id === id)
    .map((item) => item.name)
}
function record(id, host) {
  return records.value[id]?.find((item) => item.host === host)
}
function address(id, host, family) {
  const row = record(id, host)
  return (
    row?.[family + '_error'] ||
    row?.[family]?.join(', ') ||
    (row?.running ? '查询中…' : !row?.checked_at || row.checked_at.startsWith('0001') ? '等待查询' : '无记录')
  )
}
async function readRecords(force = false) {
  const version = ++recordsVersion,
    result = await adminApi.ddnsRecords(force, controller.signal)
  if (!controller.signal.aborted && version === recordsVersion) records.value = result.groups
}
async function reload(force = false) {
  if (loading.value || isSavingConfig.value) return
  loading.value = true
  error.value = ''
  try {
    await refreshConfig(controller.signal)
    await Promise.all([refreshStatus(controller.signal), readRecords(force)])
  } catch (err) {
    if (!controller.signal.aborted) error.value = err.message
  } finally {
    loading.value = false
  }
}
function edit(group, hostsOnly = false) {
  const snapshot = structuredClone(gatewayConfigSnapshot.value)
  editor.value = {
    snapshot,
    group: group ? snapshot.config.ddns.groups.find((item) => item.id === group.id) : undefined,
    hostsOnly
  }
}
async function saved() {
  editor.value = null
  toast.success('DDNS 配置已保存，后台将按配置检查')
  await reload()
}
async function toggle(group) {
  const snapshot = structuredClone(gatewayConfigSnapshot.value)
  try {
    await saveConfig(snapshot, replaceDDNS(snapshot, { ...group, enabled: !group.enabled }, group.id))
    await saved()
  } catch (err) {
    toast.error(err.message)
  }
}
async function remove(group) {
  if (!confirm(`删除 DDNS 组「${group.name}」？托管商已有记录会保留。`)) return
  const snapshot = structuredClone(gatewayConfigSnapshot.value)
  try {
    await saveConfig(snapshot, {
      ...snapshot.config,
      ddns: {
        ...snapshot.config.ddns,
        groups: snapshot.config.ddns.groups.filter((item) => item.id !== group.id)
      }
    })
    await saved()
  } catch (err) {
    toast.error(err.message)
  }
}
async function sync(id) {
  if (queuing.value) return
  queuing.value = true
  try {
    const result = id ? await adminApi.syncDDNS(id) : await adminApi.runJobs('ddns')
    toast.info(result.message || '同步任务已加入队列，请等待执行结果')
    await refreshStatus(controller.signal)
  } catch (err) {
    if (!controller.signal.aborted) toast.error(err.message)
  } finally {
    queuing.value = false
  }
}
useStatusPolling()
onMounted(() => {
  reload()
  timer = startPolling(async () => {
    try {
      await readRecords()
      if (!controller.signal.aborted) pollError.value = ''
    } catch (err) {
      if (!controller.signal.aborted) pollError.value = err.message
    }
  }, 5000)
})
onUnmounted(() => {
  timer?.()
  controller.abort()
})
</script>
<style scoped>
.ddns-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  align-items: start;
  gap: 16px;
}
.ddns-card :deep(.card-inner) {
  display: grid;
  gap: 14px;
}
.domain-title {
  overflow-wrap: anywhere;
  font-size: 14px;
}
.dns-records {
  min-width: 520px;
}
.card-actions {
  border-top: 1px solid var(--line);
  padding-top: 14px;
}
.text-bad {
  color: var(--bad);
}
summary {
  color: var(--muted);
  font-size: 12px;
  cursor: pointer;
}
@media (max-width: 1100px) {
  .ddns-grid {
    grid-template-columns: 1fr;
  }
}
</style>
