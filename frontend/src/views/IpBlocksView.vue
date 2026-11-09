<template>
  <div class="ip-blocks-page animate-fade-in">
    <div class="page-topbar">
      <div>
        <h1 class="page-title">IP 拦截名单</h1>
        <p class="page-subtitle">查看账号验证、安全防护及管理员冻结记录，按反代规则独立生效，到期自动解除</p>
      </div>
      <div class="header-actions">
        <button
          class="btn btn-secondary"
          :disabled="loading || mutating || isSavingConfig"
          @click="refresh(true)"
        >
          刷新名单
        </button>
        <button class="btn btn-primary" :disabled="!routes.length || mutating" @click="openCreate">
          <ShieldAlert :size="15" /> 手动冻结 IP
        </button>
      </div>
    </div>
    <p v-if="loading && !view" role="status">正在读取冻结记录…</p>
    <p v-if="loadError" class="error" role="alert">
      {{ loadError }}{{ view ? '。当前显示上次获取的记录。' : '' }}
    </p>
    <p v-if="view?.write_error" class="write-warning" role="alert">
      冻结记录保存失败。当前进程仍执行拦截，但重启可能丢失最新变更，请检查数据目录权限与磁盘空间。
    </p>
    <div class="filter-bar">
      <label class="rule-select-wrap"
        >关联反代规则<select v-model="selectedRule" class="form-select" aria-label="筛选反代规则">
          <option value="">全部反代规则</option>
          <option v-for="item in ruleOptions" :key="item.key" :value="item.key">{{ item.label }}</option>
        </select></label
      >
      <label class="search-box"
        ><Search :size="14" /><input
          v-model="searchQuery"
          class="search-input"
          aria-label="搜索 IP 或冻结原因"
          placeholder="搜索 IPv4 / IPv6、规则或冻结原因"
      /></label>
    </div>
    <p v-if="view" class="result-count" role="status">
      共 {{ filteredList.length }} 条匹配记录 · 第 {{ page }} / {{ totalPages }} 页 · 每 5 秒刷新
    </p>
    <div class="freeze-cards-grid">
      <SpotlightCard v-for="item in paginatedList" :key="item.rule + '|' + item.ip" class="freeze-card">
        <div class="freeze-head">
          <div class="ip-title-group">
            <strong class="mono-text freeze-ip">{{ item.ip }}</strong
            ><span class="badge" :class="item.source === 'manual' ? 'badge-warn' : 'badge-bad'">{{
              sourceLabel(item.source)
            }}</span>
          </div>
          <button
            class="btn btn-secondary btn-sm"
            :disabled="mutating"
            :aria-label="`解除 ${item.ip} 在 ${item.rule} 的冻结`"
            @click="unblock(item)"
          >
            <Unlock :size="13" /> 解除冻结
          </button>
        </div>
        <dl class="freeze-details-box">
          <div>
            <dt>关联业务规则</dt>
            <dd>
              {{ ruleName(item) }}<span class="rule-key mono-text">{{ item.rule }}</span>
            </dd>
          </div>
          <div>
            <dt>冻结原因</dt>
            <dd class="reason-text">{{ item.reason }}</dd>
          </div>
          <div v-if="item.failures">
            <dt>触发时累计</dt>
            <dd>{{ item.failures }} 次</dd>
          </div>
          <div>
            <dt>冻结时间</dt>
            <dd>{{ date(item.started_at) }}</dd>
          </div>
          <div>
            <dt>到期时间</dt>
            <dd>{{ date(item.until) }}</dd>
          </div>
        </dl>
        <div class="freeze-footer">
          <span><Clock :size="12" /> 剩余 {{ remaining(item.until) }}</span
          ><span>到期自动解除</span>
        </div>
      </SpotlightCard>
    </div>
    <div v-if="view && !filteredList.length" class="empty-state">
      <CheckCircle :size="32" /><strong>{{
        activeEntries.length ? '没有匹配的冻结记录' : '当前没有被冻结的 IP'
      }}</strong>
      <p>
        {{
          activeEntries.length
            ? '请调整反代规则或搜索条件。'
            : '达到账号或防护阈值、或手动冻结后，记录会显示在这里。'
        }}
      </p>
    </div>
    <nav v-if="totalPages > 1" class="pagination" aria-label="冻结记录分页">
      <button class="btn btn-secondary" :disabled="page <= 1" @click="page--">上一页</button
      ><span>{{ page }} / {{ totalPages }}</span
      ><button class="btn btn-secondary" :disabled="page >= totalPages" @click="page++">下一页</button>
    </nav>
    <dialog ref="dialog" class="modal-dialog" aria-labelledby="freeze-title" @cancel="blockBusyClose">
      <div class="modal-header">
        <h2 id="freeze-title">手动冻结 IP 来源</h2>
        <button class="btn btn-ghost" aria-label="关闭" :disabled="mutating" @click="dialog.close()">
          <X :size="18" />
        </button>
      </div>
      <form class="modal-body" @submit.prevent="createFreeze">
        <p v-if="formError" ref="errorElement" class="error" role="alert" tabindex="-1">{{ formError }}</p>
        <fieldset :disabled="mutating" class="freeze-fields">
          <label
            >来源 IP 地址<input
              v-model="form.ip"
              name="ip"
              class="form-input mono-text"
              placeholder="203.0.113.10 或 2001:db8::10"
              required
              autofocus
          /></label>
          <label
            >生效的反代规则<select v-model="form.rule" name="rule" class="form-select" required>
              <option v-for="route in routes" :key="routeKey(route)" :value="routeKey(route)">
                {{ routeLabel(route) }}
              </option>
            </select></label
          >
          <label
            >冻结持续时长 分钟<input
              v-model.number="form.minutes"
              name="minutes"
              class="form-input"
              type="number"
              min="1"
              max="10080"
              required
          /></label>
          <p class="form-note">范围 1–10080 分钟，最长 7 天。仅对所选反代规则生效，到期自动解除。</p>
          <label
            >备注原因（可选，最多 500 字节）<input
              v-model="form.reason"
              name="reason"
              class="form-input"
              type="text"
              maxlength="500"
              placeholder="留空使用管理员手动冻结"
            />
          </label>
        </fieldset>
        <div class="modal-footer">
          <button type="button" class="btn btn-secondary" @click="dialog.close()">取消</button>
          <button type="submit" class="btn btn-danger">{{ mutating ? '正在冻结…' : '执行冻结' }}</button>
        </div>
      </form>
    </dialog>
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, shallowRef, watch } from 'vue'
import { CheckCircle, Clock, Search, ShieldAlert, Unlock, X } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { adminApi } from '@/api/admin'
import { routeKey } from '@/api/configuration'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useToast } from '@/composables/useToast'

const { gatewayConfig, isSavingConfig, refreshConfig } = useGatewayStatus()
const toast = useToast()
const routes = computed(() => gatewayConfig.value?.routes ?? [])
const view = shallowRef(null)
const loading = ref(false)
const mutating = ref(false)
const loadError = ref('')
const selectedRule = ref('')
const searchQuery = ref('')
const page = ref(1)
const dialog = ref(null)
const form = ref({})
const formError = ref('')
const errorElement = ref(null)
const serverTime = ref(Date.now())
const tick = ref(performance.now())
let receivedAt = tick.value
let requestVersion = 0
const controller = new AbortController()
const now = computed(() => serverTime.value + tick.value - receivedAt)
const activeEntries = computed(() =>
  (view.value?.entries ?? []).filter((item) => Date.parse(item.until) > now.value)
)
const ruleOptions = computed(() => {
  const options = new Map(
    activeEntries.value.map((item) => [
      item.rule,
      { key: item.rule, label: `${item.rule_name || '已删除规则'} · ${item.rule}` }
    ])
  )
  for (const route of routes.value)
    options.set(routeKey(route), {
      key: routeKey(route),
      label: routeLabel(route)
    })
  return [...options.values()]
})
const filteredList = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  return activeEntries.value.filter(
    (item) =>
      (!selectedRule.value || item.rule === selectedRule.value) &&
      [item.ip, item.reason, item.rule, ruleName(item)].some((value) => value.toLowerCase().includes(query))
  )
})
const totalPages = computed(() => Math.max(1, Math.ceil(filteredList.value.length / 20)))
const paginatedList = computed(() => filteredList.value.slice((page.value - 1) * 20, page.value * 20))
watch([selectedRule, searchQuery], () => {
  page.value = 1
})
watch(totalPages, (count) => {
  page.value = Math.min(page.value, count)
})

function routeLabel(route) {
  const group = gatewayConfig.value.groups.find((item) => item.id === route.group_id)
  return `${group?.name || route.group_id} · ${route.name || route.host} · ${route.host}`
}
function ruleName(item) {
  return routes.value.find((route) => routeKey(route) === item.rule)?.name || item.rule_name || item.rule
}
function sourceLabel(source) {
  return { manual: '管理员手动', automatic: '账号验证失败', security: '安全防护触发' }[source] || source
}
function date(value) {
  return new Date(value).toLocaleString()
}
function remaining(until) {
  const seconds = Math.max(0, Math.ceil((Date.parse(until) - now.value) / 1000))
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  return `${days ? days + ' 天 ' : ''}${hours ? hours + ' 小时 ' : ''}${minutes} 分 ${seconds % 60} 秒`
}
function apply(result) {
  receivedAt = performance.now()
  serverTime.value = Date.parse(result.now)
  tick.value = receivedAt
  view.value = result
  loadError.value = ''
}
async function readBlocks(version) {
  const result = await adminApi.ipBlocks(controller.signal)
  if (!controller.signal.aborted && version === requestVersion) apply(result)
}
async function refresh(loadConfig = false) {
  if (loading.value || mutating.value || isSavingConfig.value) return
  loading.value = true
  const version = ++requestVersion
  try {
    await Promise.all([readBlocks(version), ...(loadConfig ? [refreshConfig(controller.signal)] : [])])
  } catch (err) {
    if (!controller.signal.aborted && version === requestVersion) loadError.value = err.message
  } finally {
    loading.value = false
  }
}
function openCreate() {
  form.value = {
    rule: routes.value.some((route) => routeKey(route) === selectedRule.value)
      ? selectedRule.value
      : routeKey(routes.value[0]),
    ip: '',
    minutes: 1440,
    reason: ''
  }
  formError.value = ''
  dialog.value.showModal()
}
function blockBusyClose(event) {
  if (mutating.value) event.preventDefault()
}
async function createFreeze() {
  if (mutating.value) return
  mutating.value = true
  ++requestVersion
  formError.value = ''
  try {
    const result = await adminApi.freezeIP({
      ...form.value,
      ip: form.value.ip.trim(),
      reason: form.value.reason.trim()
    })
    if (controller.signal.aborted) return
    apply(result)
    page.value = 1
    selectedRule.value = form.value.rule
    searchQuery.value = ''
    dialog.value.close()
    if (result.write_error) toast.warn('已冻结，但记录保存失败，请检查磁盘与权限')
    else toast.success('IP 已加入所选规则的拦截名单')
  } catch (err) {
    if (!controller.signal.aborted) {
      formError.value = err.message
      await nextTick()
      errorElement.value?.focus()
    }
  } finally {
    mutating.value = false
  }
}
async function unblock(item) {
  if (mutating.value || !confirm(`解除 IP ${item.ip} 在「${ruleName(item)}」的冻结？`)) return
  mutating.value = true
  ++requestVersion
  let expired = false
  try {
    const result = await adminApi.releaseIP({ rule: item.rule, ip: item.ip })
    if (controller.signal.aborted) return
    apply(result)
    if (result.write_error) toast.warn('当前冻结已解除，但记录保存失败，请检查磁盘与权限')
    else toast.success('已解除所选规则的 IP 冻结')
  } catch (err) {
    if (!controller.signal.aborted) {
      expired = err.status === 404
      if (expired) toast.info(err.message)
      else toast.error(err.message)
    }
  } finally {
    mutating.value = false
  }
  if (expired) refresh()
}
let refreshTimer, clockTimer
onMounted(() => {
  refresh(true)
  refreshTimer = setInterval(() => refresh(), 5000)
  clockTimer = setInterval(() => {
    tick.value = performance.now()
  }, 1000)
})
onUnmounted(() => {
  clearInterval(refreshTimer)
  clearInterval(clockTimer)
  controller.abort()
})
</script>

<style scoped>
.ip-blocks-page {
  display: flex;
  flex-direction: column;
  gap: 20px;
}
.page-topbar,
.filter-bar,
.freeze-head,
.freeze-footer,
.modal-header,
.modal-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
}
.page-title {
  font-size: 22px;
  font-weight: 700;
  color: var(--ink);
}
.page-subtitle {
  font-size: 13px;
  color: var(--muted);
  margin-top: 3px;
}
.header-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.rule-select-wrap {
  display: grid;
  gap: 6px;
  max-width: 100%;
  color: var(--muted);
  font-size: 12px;
}
.rule-select-wrap select {
  max-width: 420px;
}
.search-box {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border: 1px solid var(--line);
  background: var(--input-bg);
  border-radius: var(--radius-sm);
  width: 300px;
  max-width: 100%;
  color: var(--faint);
}
.search-input {
  border: 0;
  background: transparent;
  color: var(--ink);
  font-size: 12px;
  flex: 1;
  min-width: 0;
}
.result-count,
.note {
  color: var(--muted);
  font-size: 12px;
}
.freeze-cards-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.freeze-card {
  padding: 18px;
  min-width: 0;
}
.freeze-card :deep(.card-inner) {
  display: grid;
  gap: 14px;
}
.ip-title-group {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  min-width: 0;
}
.freeze-ip {
  font-size: 16px;
  overflow-wrap: anywhere;
}
.freeze-details-box {
  padding: 12px;
  margin: 0;
  display: grid;
  gap: 12px;
  background: var(--strip-bg);
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  font-size: 12px;
}
.freeze-details-box div {
  display: grid;
  grid-template-columns: 86px minmax(0, 1fr);
  gap: 10px;
}
dt {
  color: var(--muted);
}
dd {
  margin: 0;
  color: var(--ink-secondary);
  overflow-wrap: anywhere;
}
.rule-key {
  display: block;
  margin-top: 4px;
  color: var(--muted);
}
.reason-text,
.error {
  color: var(--bad);
}
.freeze-footer {
  color: var(--muted);
  font-size: 12px;
}
.freeze-footer span:first-child {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--brand-light);
}
.empty-state {
  display: grid;
  justify-items: center;
  text-align: center;
  gap: 12px;
  padding: 48px 16px;
  color: var(--muted);
}
.empty-state svg {
  color: var(--ok);
}
.pagination {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 14px;
  font-size: 13px;
  color: var(--muted);
}
.write-warning {
  padding: 14px;
  border: 1px solid var(--warn);
  color: var(--warn);
  border-radius: var(--radius-sm);
  font-size: 13px;
}
p {
  margin: 0;
  overflow-wrap: anywhere;
}
form,
.freeze-fields,
.freeze-fields label {
  display: grid;
  gap: 14px;
  min-width: 0;
}
.freeze-fields {
  border: 0;
  padding: 0;
  margin: 0;
}
.freeze-fields label {
  font-size: 13px;
  color: var(--ink-secondary);
  gap: 6px;
}
.form-input,
.form-select {
  min-width: 0;
}
.modal-footer {
  justify-content: flex-end;
}
@media (max-width: 900px) {
  .freeze-cards-grid {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 600px) {
  .filter-bar {
    align-items: stretch;
    flex-direction: column;
  }
  .search-box {
    width: 100%;
    box-sizing: border-box;
  }
  .rule-select-wrap select {
    width: 100%;
  }
}
</style>
