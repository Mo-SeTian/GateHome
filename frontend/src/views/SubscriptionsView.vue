<template>
  <div class="report-page subscriptions-page animate-fade-in">
    <div class="report-heading">
      <div>
        <h1>IP 规则订阅管理</h1>
        <p>自动获取 IPv4 / IPv6 / CIDR 规则，更新后应用到引用它的防火墙 IP 组</p>
      </div>
      <div class="report-actions">
        <button class="btn btn-secondary" :disabled="loading || isSavingConfig" @click="reload">
          刷新状态
        </button>
        <button
          class="btn btn-secondary"
          :disabled="queuingAll || !subscriptions.some((item) => item.enabled)"
          @click="refreshAll"
        >
          全部立即更新
        </button>
        <button class="btn btn-primary" :disabled="!gatewayConfig || isSavingConfig" @click="open()">
          <Plus :size="15" /> 添加 IP 订阅
        </button>
      </div>
    </div>
    <p v-if="loading && !gatewayConfig" role="status">正在读取订阅配置…</p>
    <p v-if="error || statusError" class="report-error" role="alert">{{ error || statusError }}</p>
    <SpotlightCard class="report-panel preset-panel">
      <div>
        <strong>快捷预设源 · 中国大陆 IP 列表</strong>
        <p class="report-note">mayaxcn/china-ip-list；选择后可检查配置再保存。</p>
      </div>
      <div class="report-actions">
        <button
          v-for="family in [4, 6]"
          :key="family"
          class="btn btn-secondary btn-sm"
          :disabled="!gatewayConfig || isSavingConfig"
          @click="preset(family)"
        >
          中国大陆 IPv{{ family }}</button
        ><a
          class="btn btn-ghost btn-sm"
          href="https://github.com/mayaxcn/china-ip-list"
          target="_blank"
          rel="noopener noreferrer"
          >查看来源 <ExternalLink :size="13"
        /></a>
      </div>
    </SpotlightCard>
    <p class="report-note">
      订阅为 HTTPS 文本，每行一个 IP / CIDR，支持注释；不跟随重定向。手动更新有 5
      秒冷却，接受任务不代表下载完成。停用或删除前须解除全部防火墙引用。
    </p>
    <div class="subs-grid">
      <SpotlightCard
        v-for="sub in subscriptions"
        :key="sub.id"
        class="sub-card"
        :class="{ inactive: !sub.enabled }"
      >
        <div class="sub-head">
          <div>
            <strong>{{ sub.name }}</strong>
            <p>
              <span class="badge" :class="state(sub).tone">{{ state(sub).label }}</span>
            </p>
          </div>
          <label class="switch"
            ><input
              type="checkbox"
              :checked="sub.enabled"
              :disabled="isSavingConfig || (sub.enabled && references(sub.id).length > 0)"
              :aria-label="`启用 ${sub.name}`"
              :title="references(sub.id).length ? '请先解除防火墙引用' : ''"
              @change="toggle(sub, $event)" /><span class="switch-slider"
          /></label>
        </div>
        <button
          class="source-url mono-text"
          type="button"
          :aria-label="`复制 ${sub.name} 的订阅地址`"
          @click="copy(sub.url)"
        >
          {{ sub.url }} <Copy :size="12" />
        </button>
        <p class="report-note">{{ status(sub.id)?.message || '等待服务器返回运行状态' }}</p>
        <dl>
          <div>
            <dt>有效规则</dt>
            <dd>{{ (status(sub.id)?.count ?? 0).toLocaleString() }} 条</dd>
          </div>
          <div>
            <dt>更新周期</dt>
            <dd>{{ sub.interval.toLocaleString() }} 秒</dd>
          </div>
          <div>
            <dt>最近尝试</dt>
            <dd>{{ reportDate(status(sub.id)?.last_run) }}</dd>
          </div>
          <div>
            <dt>最近成功</dt>
            <dd>{{ reportDate(status(sub.id)?.last_success) }}</dd>
          </div>
        </dl>
        <details v-if="references(sub.id).length" class="ref-details">
          <summary>
            <span>{{ references(sub.id).length }} 个防火墙 IP 组引用</span>
          </summary>
          <div class="ref-pills-wrap">
            <span v-for="name in references(sub.id)" :key="name" class="ref-pill">{{ name }}</span>
          </div>
        </details>
        <p v-else class="report-note">尚未被防火墙引用</p>
        <div class="report-actions sub-footer">
          <button
            class="btn btn-secondary btn-sm"
            :disabled="!sub.enabled || status(sub.id)?.running || queued.has(sub.id) || queuingAll"
            @click="refreshOne(sub)"
          >
            <RefreshCw :size="13" /> 立即拉取
          </button>
          <div class="report-actions">
            <button class="btn btn-ghost btn-sm" :disabled="isSavingConfig" @click="open(sub)">
              <Edit3 :size="13" /> 编辑</button
            ><button
              class="btn btn-ghost btn-sm text-bad"
              :disabled="isSavingConfig || references(sub.id).length > 0"
              :aria-label="`删除 ${sub.name}`"
              @click="remove(sub)"
            >
              <Trash2 :size="13" />
            </button>
          </div>
        </div>
      </SpotlightCard>
    </div>
    <p v-if="gatewayConfig && !subscriptions.length" class="report-empty">
      尚无 IP 规则订阅，请添加 HTTPS 文本规则源。
    </p>
    <dialog ref="dialog" class="report-dialog" aria-labelledby="subscription-title" @cancel="blockBusyClose">
      <header>
        <h2 id="subscription-title">{{ originalId ? '编辑' : '添加' }} IP 规则订阅</h2>
        <button class="btn btn-ghost" aria-label="关闭" :disabled="isSavingConfig" @click="dialog.close()">
          <X :size="18" />
        </button>
      </header>
      <form @submit.prevent="save">
        <p v-if="formError" ref="formErrorElement" class="report-error" role="alert" tabindex="-1">
          {{ formError }}
        </p>
        <fieldset :disabled="isSavingConfig">
          <label class="form-field"
            >名称<input v-model="form.name" name="name" class="form-input" required maxlength="100" autofocus
          /></label>
          <label class="form-field"
            >HTTPS 规则地址<input
              v-model="form.url"
              name="url"
              type="url"
              class="form-input mono-text"
              placeholder="https://example.com/cidrs.txt"
              maxlength="4096"
              required
          /></label>
          <p class="report-note">
            支持
            IPv4、IPv6、CIDR、空行和注释。新来源首次成功前，关联防火墙会暂拒访问；更新失败保留同一来源上次成功的缓存。
          </p>
          <label class="form-field"
            >更新周期 秒<input
              v-model.number="form.interval"
              name="interval"
              class="form-input"
              type="number"
              min="300"
              max="604800"
              required
          /></label>
          <label class="toggle-row">
            <span>启用后台自动更新</span>
            <input v-model="form.enabled" name="enabled" type="checkbox" />
          </label>
          <footer>
            <button type="button" class="btn btn-secondary" @click="dialog.close()">取消</button
            ><button type="submit" class="btn btn-primary">
              {{ isSavingConfig ? '保存中…' : '保存订阅' }}
            </button>
          </footer>
        </fieldset>
      </form>
    </dialog>
  </div>
</template>

<script setup>
import { newID } from '@/api/ids'
import { computed, nextTick, onMounted, onUnmounted, ref, shallowRef } from 'vue'
import { Copy, Edit3, ExternalLink, Plus, RefreshCw, Trash2, X } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { adminApi } from '@/api/admin'
import { replaceConfigItem } from '@/api/configuration'
import { reportDate, subscriptionState } from '@/api/reports'
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
const toast = useToast()
const subscriptions = computed(() => gatewayConfig.value?.subscriptions ?? [])
const loading = ref(false),
  error = ref(''),
  queuingAll = ref(false),
  queued = ref(new Set())
const dialog = ref(null),
  snapshot = shallowRef(null),
  originalId = ref(null),
  form = ref({})
const formError = ref(''),
  formErrorElement = ref(null)
const controller = new AbortController()
function status(id) {
  return gatewayStatus.value?.subscriptions?.find((item) => item.id === id)
}
function state(sub) {
  return subscriptionState(sub, status(sub.id))
}
function references(id) {
  return (gatewayConfig.value?.firewalls ?? []).flatMap((firewall) =>
    (firewall.groups ?? [])
      .filter((group) => group.subscriptions?.includes(id))
      .map((group) => `${firewall.name} / ${group.name}`)
  )
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
function open(sub, defaults = {}) {
  snapshot.value = structuredClone(gatewayConfigSnapshot.value)
  originalId.value = sub?.id ?? null
  form.value = sub
    ? structuredClone(sub)
    : { id: newID(), name: '', url: '', enabled: true, interval: 86400, ...defaults }
  formError.value = ''
  dialog.value.showModal()
}
function preset(family) {
  const url = `https://raw.githubusercontent.com/mayaxcn/china-ip-list/master/${family === 6 ? 'chnroute_v6.txt' : 'chnroute.txt'}`
  if (subscriptions.value.some((item) => item.url === url)) {
    toast.info('此预设来源已存在')
    return
  }
  open(undefined, { name: `中国大陆 IPv${family}（mayaxcn）`, url, interval: 28800 })
}
function blockBusyClose(event) {
  if (isSavingConfig.value) event.preventDefault()
}
async function saved() {
  toast.success('订阅配置已保存')
  try {
    await refreshStatus(controller.signal)
  } catch (err) {
    if (!controller.signal.aborted && err.status !== 401) toast.warn('配置已保存，运行状态刷新失败')
  }
}
async function save() {
  if (isSavingConfig.value) return
  formError.value = ''
  try {
    const record = { ...form.value, name: form.value.name.trim(), url: form.value.url.trim() }
    await saveConfig(
      snapshot.value,
      replaceConfigItem(snapshot.value, 'subscriptions', record, originalId.value)
    )
    dialog.value.close()
    await saved()
  } catch (err) {
    formError.value = err.message
    await nextTick()
    formErrorElement.value?.focus()
  }
}
async function toggle(sub, event) {
  event.target.checked = sub.enabled
  if (isSavingConfig.value || (sub.enabled && references(sub.id).length)) return
  const current = structuredClone(gatewayConfigSnapshot.value)
  try {
    await saveConfig(
      current,
      replaceConfigItem(current, 'subscriptions', { ...sub, enabled: !sub.enabled }, sub.id)
    )
    await saved()
  } catch (err) {
    toast.error(err.message)
  }
}
async function remove(sub) {
  if (isSavingConfig.value || references(sub.id).length || !confirm(`删除 IP 订阅「${sub.name}」？`)) return
  const current = structuredClone(gatewayConfigSnapshot.value)
  try {
    await saveConfig(current, {
      ...current.config,
      subscriptions: current.config.subscriptions.filter((item) => item.id !== sub.id)
    })
    await saved()
  } catch (err) {
    toast.error(err.message)
  }
}
async function queue(sub) {
  queued.value.add(sub.id)
  try {
    await adminApi.refreshSubscription(sub.id)
    return true
  } finally {
    queued.value.delete(sub.id)
  }
}
async function refreshOne(sub) {
  try {
    await queue(sub)
    toast.info('更新任务已加入队列，请等待服务器返回结果')
    await refreshStatus(controller.signal)
  } catch (err) {
    if (!controller.signal.aborted) toast.error(err.message)
  }
}
async function refreshAll() {
  queuingAll.value = true
  let accepted = 0,
    failed = 0
  for (const sub of subscriptions.value.filter((item) => item.enabled)) {
    if (controller.signal.aborted) break
    try {
      await queue(sub)
      accepted++
    } catch {
      failed++
    }
  }
  queuingAll.value = false
  if (controller.signal.aborted) return
  toast.info(`${accepted} 个更新任务已加入队列${failed ? `，${failed} 个请求失败，请单独重试` : ''}`)
  try {
    await refreshStatus(controller.signal)
  } catch (err) {
    if (!controller.signal.aborted) error.value = err.message
  }
}
async function copy(url) {
  try {
    await navigator.clipboard.writeText(url)
    toast.success('订阅地址已复制')
  } catch {
    toast.error('复制失败，请手动复制地址')
  }
}
useStatusPolling()
onMounted(reload)
onUnmounted(() => controller.abort())
</script>

<style scoped>
.subs-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(360px, 1fr));
  gap: 16px;
}
.sub-card {
  padding: 20px;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.sub-card :deep(.card-inner) {
  display: flex;
  flex-direction: column;
  gap: 14px;
  height: 100%;
}
.sub-head {
  display: flex;
  align-items: start;
  justify-content: space-between;
  gap: 12px;
}
.sub-head strong {
  color: var(--ink);
  font-size: 14px;
  overflow-wrap: anywhere;
}
.sub-head p {
  margin-top: 6px;
}
.source-url {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 11px;
  background: var(--strip-bg);
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  color: var(--ink-secondary);
  font-size: 11.5px;
  text-align: left;
  overflow-wrap: anywhere;
  cursor: pointer;
  transition: var(--transition-smooth);
}
.source-url:hover {
  border-color: var(--card-border-hover);
  color: var(--brand-light);
}
.sub-footer {
  margin-top: auto;
  padding-top: 10px;
  border-top: 1px solid var(--line);
}
dl {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  font-size: 12px;
  background: var(--strip-bg);
  padding: 10px 12px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--line);
}
dt {
  color: var(--muted);
  font-size: 11px;
}
dd {
  margin: 2px 0 0;
  color: var(--ink);
  font-weight: 600;
  font-family: var(--mono);
}
@media (max-width: 640px) {
  .subs-grid {
    grid-template-columns: 1fr;
  }
}
</style>
