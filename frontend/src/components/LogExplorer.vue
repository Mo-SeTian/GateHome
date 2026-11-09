<template>
  <section class="report-page log-explorer" :aria-label="title">
    <h2 v-if="title" class="explorer-title">{{ title }}</h2>

    <form class="report-form report-filter-bar" @submit.prevent="applyFilters">
      <!-- 常用核心筛选项（一行铺开并自适应，右侧为操作按钮组） -->
      <div class="filter-primary-row">
        <div class="filter-primary-fields">
          <!-- 规则 / 分类 -->
          <div v-if="scope === 'project'" class="filter-col">
            <label class="col-label">项目分类</label>
            <select v-model="form.category" name="category" class="form-select">
              <option value="">全部分类</option>
              <option v-for="(label, key) in categories" :key="key" :value="key">{{ label }}</option>
            </select>
          </div>
          <div v-else class="filter-col">
            <label class="col-label">反代规则</label>
            <select v-model="form.rule" name="rule" class="form-select">
              <option value="">全部反代规则</option>
              <option v-for="route in gatewayConfig?.routes" :key="routeKey(route)" :value="routeKey(route)">
                {{ routeLabel(route) }}
              </option>
              <option v-if="form.rule && !hasRoute(form.rule)" :value="form.rule">
                {{ form.rule }}（历史规则）
              </option>
            </select>
          </div>

          <!-- 执行结果 -->
          <div class="filter-col filter-col-sm">
            <label class="col-label">执行结果</label>
            <select v-model="form.result" name="result" class="form-select">
              <option value="">全部结果</option>
              <option value="success">成功</option>
              <option value="error">失败</option>
            </select>
          </div>

          <!-- 关键词搜索 -->
          <div class="filter-col filter-col-search">
            <label class="col-label">关键词搜索</label>
            <div class="search-box">
              <Search :size="14" class="search-icon" />
              <input
                v-model="form.search"
                name="search"
                class="form-input"
                maxlength="200"
                placeholder="搜索域名、IP、路径、操作或规则…"
              />
            </div>
          </div>
        </div>

        <!-- 按钮组 -->
        <div class="filter-actions-group">
          <button type="submit" class="btn btn-primary" :disabled="loading">
            <Search :size="14" />
            <span>筛选</span>
          </button>
          <button
            type="button"
            class="btn btn-secondary filter-toggle-btn"
            :class="{ active: showAdvanced }"
            @click="showAdvanced = !showAdvanced"
          >
            <SlidersHorizontal :size="14" />
            <span>{{ showAdvanced ? '收起' : '更多筛选' }}</span>
            <span v-if="!showAdvanced && activeAdvancedCount > 0" class="filter-badge">
              {{ activeAdvancedCount }}
            </span>
            <ChevronDown :size="13" class="toggle-arrow" :class="{ rotated: showAdvanced }" />
          </button>
          <button
            type="button"
            class="btn btn-secondary btn-icon-btn"
            :disabled="loading"
            title="重置全部筛选"
            aria-label="重置全部筛选"
            @click="reset"
          >
            <RotateCcw :size="14" />
          </button>
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="loading"
            @click="refresh"
          >
            <RefreshCw :size="14" :class="{ 'spin-anim': loading }" />
            <span>{{ loading ? '读取中…' : '刷新' }}</span>
          </button>
        </div>
      </div>

      <!-- 高级筛选项抽屉 -->
      <transition name="drawer-fade">
        <div v-if="showAdvanced" class="filter-advanced-drawer">
          <div class="advanced-grid">
            <template v-if="scope !== 'project'">
              <div class="filter-col">
                <label class="col-label">来源 IP</label>
                <input
                  v-model="form.ip"
                  name="ip"
                  class="form-input mono-text"
                  placeholder="精确 IPv4 / IPv6"
                  maxlength="64"
                />
              </div>
              <div class="filter-col">
                <label class="col-label">请求方法</label>
                <select v-model="form.method" name="method" class="form-select">
                  <option value="">全部方法</option>
                  <option v-for="method in methods" :key="method">{{ method }}</option>
                </select>
              </div>
              <div class="filter-col">
                <label class="col-label">HTTP 状态码</label>
                <input
                  v-model="form.status"
                  name="status"
                  class="form-input"
                  type="number"
                  min="100"
                  max="599"
                  placeholder="例如 403"
                />
              </div>
            </template>

            <template v-if="securityScope">
              <div class="filter-col">
                <label class="col-label">防火墙策略</label>
                <select v-model="form.firewall" name="firewall" class="form-select">
                  <option value="">全部策略</option>
                  <option v-for="firewall in gatewayConfig?.firewalls" :key="firewall.id" :value="firewall.id">
                    {{ firewall.name }}
                  </option>
                  <option
                    v-if="form.firewall && !gatewayConfig?.firewalls.some((item) => item.id === form.firewall)"
                    :value="form.firewall"
                  >
                    {{ form.firewall }}（历史策略）
                  </option>
                </select>
              </div>
              <div class="filter-col">
                <label class="col-label">检测引擎</label>
                <select v-model="form.engine" name="engine" class="form-select">
                  <option value="">全部引擎</option>
                  <option v-for="engine in engines" :key="engine" :value="engine">{{ engineLabel(engine) }}</option>
                </select>
              </div>
              <div class="filter-col">
                <label class="col-label">处理动作</label>
                <select v-model="form.decision" name="decision" class="form-select">
                  <option value="">全部动作</option>
                  <option value="block">拦截</option>
                  <option value="detect">仅记录</option>
                </select>
              </div>
              <div class="filter-col">
                <label class="col-label">检测规则编号</label>
                <input
                  v-model="form.rule_id"
                  name="rule_id"
                  class="form-input"
                  type="number"
                  min="1"
                  max="999999999"
                  placeholder="规则 ID"
                />
              </div>
            </template>

            <div class="filter-col">
              <label class="col-label">开始时间</label>
              <input v-model="form.from" name="from" class="form-input" type="datetime-local" />
            </div>
            <div class="filter-col">
              <label class="col-label">结束时间</label>
              <input v-model="form.to" name="to" class="form-input" type="datetime-local" />
            </div>
          </div>
        </div>
      </transition>

      <div class="filter-quick-info">
        <span class="quick-info-text">保留最多 5000 条实时日志 · 翻页保留快照 · 筛选或刷新读取最新记录</span>
        <span v-if="dirty && data" class="dirty-badge">筛选条件已变更，点击「筛选」生效</span>
      </div>
    </form>
    <p v-if="error" ref="errorElement" class="report-error" role="alert" tabindex="-1">
      {{ error }}{{ data ? '。当前显示上次成功查询的记录。' : '' }}
    </p>
    <p v-else-if="loading && !data" role="status">正在读取日志…</p>
    <p v-if="data?.write_error" class="report-warning" role="alert">
      日志写入或清理失败，当前记录可能仅存在于内存，请检查日志目录权限与磁盘空间。
    </p>
    <p v-if="dirty && data" class="report-note">筛选条件尚未应用，当前仍显示上次查询结果。</p>
    <div v-if="data" class="report-pagination">
      <span ref="resultElement" class="log-result" role="status" tabindex="-1"
        >共 {{ data.total }} 条 · 第 {{ data.page }} / {{ data.pages }} 页</span
      >
      <label
        >每页条数<select
          v-model.number="size"
          name="size"
          class="form-select"
          :disabled="loading"
          @change="changeSize"
        >
          <option v-for="value in [20, 50, 100, 200]" :key="value" :value="value">{{ value }} 条</option>
        </select></label
      >
      <div class="report-actions">
        <button
          class="btn btn-secondary btn-sm"
          :disabled="loading || data.page <= 1"
          @click="goPage(data.page - 1)"
        >
          上一页</button
        ><button
          class="btn btn-secondary btn-sm"
          :disabled="loading || data.page >= data.pages"
          @click="goPage(data.page + 1)"
        >
          下一页
        </button>
      </div>
    </div>
    <div v-if="data?.entries.length" class="report-table-wrap">
      <table class="report-table log-table">
        <thead>
          <tr>
            <th>时间 / 分类</th>
            <th>调用 / 目标</th>
            <th v-if="scope !== 'project'">来源 IP / 归属地</th>
            <th>结果 / 耗时</th>
            <th>说明与命中规则</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="entry in data.entries" :key="entry.id" :data-log-id="entry.id">
            <td>
              {{ reportDate(entry.time)
              }}<small>#{{ entry.id }} · {{ categories[entry.category] || entry.category }}</small>
            </td>
            <td>
              <strong>{{ entry.action }}</strong
              ><small class="mono-text">{{ entry.target }}</small
              ><small class="mono-text">{{ entry.method }} {{ entry.path }}</small
              ><small v-if="entry.rule">{{ ruleName(entry.rule) }}</small>
            </td>
            <td v-if="scope !== 'project'" class="mono-text">
              {{ entry.remote || '—' }}<small>{{ entry.remote_region || '归属地未知' }}</small>
            </td>
            <td>
              <span class="badge" :class="entry.ok ? 'badge-ok' : 'badge-bad'">{{ outcome(entry) }}</span
              ><small>{{ entry.status ? `HTTP ${entry.status} · ` : '' }}{{ entry.duration_ms }} ms</small>
            </td>
            <td>
              {{ entry.message
              }}<small v-if="entry.firewall">{{ entry.firewall.name }} · {{ entry.firewall.reason }}</small
              ><small v-for="(hit, index) in entry.security" :key="index"
                >{{ engineLabel(hit.engine) }} #{{ hit.rule_id }} · {{ hit.name }} ·
                {{ hit.action === 'block' ? '拦截' : '仅记录' }}</small
              >
            </td>
            <td>
              <button
                class="btn btn-secondary btn-sm"
                :aria-label="`查看日志 ${entry.id}`"
                @click="show(entry)"
              >
                详情
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-else-if="data" class="report-empty">暂无匹配记录，请调整筛选条件或产生新的调用后刷新。</p>
    <dialog
      ref="detailDialog"
      class="report-dialog"
      aria-labelledby="log-detail-title"
      @cancel="blockBusyClose"
    >
      <header>
        <h2 id="log-detail-title">调用详情 #{{ selected?.id }}</h2>
        <button class="close-modal-btn" aria-label="关闭" :disabled="freezing" @click="detailDialog.close()">
          <X :size="18" />
        </button>
      </header>
      <div v-if="selected" class="modal-body">
        <dl class="detail-dl">
          <div v-for="([label, value], index) in detailRows" :key="index" class="detail-row">
            <dt>{{ label }}</dt>
            <dd>{{ value || '—' }}</dd>
          </div>
        </dl>
        <section v-if="selected.firewall" class="detail-section glass-section">
          <h3>来源 IP 策略</h3>
          <p>
            {{ selected.firewall.name }} · {{ selected.firewall.group || selected.firewall.kind }} ·
            {{ selected.firewall.reason }}
          </p>
          <button v-if="securityScope" class="btn btn-secondary btn-sm" @click="filterFirewall">
            按此策略查询
          </button>
        </section>
        <section v-if="selected.security?.length" class="detail-section glass-section">
          <h3>安全规则命中</h3>
          <p v-for="(hit, index) in selected.security" :key="index">
            {{ engineLabel(hit.engine) }} #{{ hit.rule_id }} · {{ hit.name }} ·
            {{ hit.action === 'block' ? '拦截' : '仅记录' }}{{ hit.score ? ' · 分数 ' + hit.score : ''
            }}{{ hit.severity ? ' · ' + hit.severity : '' }}
          </p>
        </section>
        <div class="report-actions">
          <button v-if="selected.rule" class="btn btn-secondary btn-sm" @click="filterRule">
            按此反代规则查询</button
          ><router-link to="/ip-blocks" class="btn btn-secondary btn-sm">IP 拦截名单</router-link>
        </div>
        <form
          v-if="securityScope && hasRoute(selected.rule) && selected.remote && selected.remote !== '未知'"
          class="detail-freeze-form glass-section"
          @submit.prevent="freeze"
        >
          <h3>在此反代规则手动冻结 {{ selected.remote }}</h3>
          <p v-if="freezeError" ref="freezeErrorElement" class="report-error" role="alert" tabindex="-1">
            {{ freezeError }}
          </p>
          <fieldset :disabled="freezing" class="freeze-fieldset">
            <label
              >冻结时长 分钟<input
                v-model.number="freezeMinutes"
                name="freeze_minutes"
                type="number"
                min="1"
                max="10080"
                class="form-input"
                required /></label
            ><label
              >备注原因<input
                v-model="freezeReason"
                name="freeze_reason"
                maxlength="500"
                class="form-input" /></label
            ><button type="submit" class="btn btn-danger">{{ freezing ? '正在冻结…' : '冻结此 IP' }}</button>
          </fieldset>
        </form>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" :disabled="freezing" @click="detailDialog.close()">关闭</button>
      </div>
    </dialog>
  </section>
</template>

<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, shallowRef } from 'vue'
import {
  ChevronDown,
  RefreshCw,
  RotateCcw,
  Search,
  SlidersHorizontal,
  X
} from 'lucide-vue-next'
import { adminApi } from '@/api/admin'
import { routeKey } from '@/api/configuration'
import { engineLabel, logQuery, reportDate } from '@/api/reports'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useToast } from '@/composables/useToast'
import '@/styles/reports.css'

const props = defineProps({ scope: { type: String, required: true }, title: String })
const { gatewayConfig, refreshConfig } = useGatewayStatus()
const toast = useToast()
const categories = {
  access: '反代访问',
  admin: '管理操作',
  ddns: '动态域名',
  subscriptions: 'IP 订阅',
  certificates: 'SSL 证书',
  forward: '端口转发'
}
const methods = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS', 'CONNECT']
const engines = ['ip', 'waf', 'custom', 'rate', 'system', 'auth', 'freeze']
const securityScope = computed(() => props.scope === 'firewall' || props.scope === 'security')
const showAdvanced = ref(false)
const emptyForm = () => ({
  category: '',
  result: '',
  rule: '',
  ip: '',
  method: '',
  status: '',
  firewall: '',
  engine: '',
  decision: '',
  rule_id: '',
  from: '',
  to: '',
  search: ''
})
const form = ref(emptyForm()),
  applied = shallowRef(emptyForm()),
  data = shallowRef(null),
  size = ref(20)
const loading = ref(false),
  error = ref(''),
  errorElement = ref(null),
  resultElement = ref(null)
const detailDialog = ref(null),
  selected = shallowRef(null),
  freezing = ref(false)
const freezeMinutes = ref(60),
  freezeReason = ref('管理员根据日志手动冻结'),
  freezeError = ref(''),
  freezeErrorElement = ref(null)
const dirty = computed(() => JSON.stringify(form.value) !== JSON.stringify(applied.value))

const activeAdvancedCount = computed(() => {
  let count = 0
  if (props.scope !== 'project') {
    if (form.value.ip) count++
    if (form.value.method) count++
    if (form.value.status) count++
  }
  if (securityScope.value) {
    if (form.value.firewall) count++
    if (form.value.engine) count++
    if (form.value.decision) count++
    if (form.value.rule_id) count++
  }
  if (form.value.from) count++
  if (form.value.to) count++
  return count
})

const controller = new AbortController()
let requestVersion = 0
function hasRoute(key) {
  return gatewayConfig.value?.routes.some((route) => routeKey(route) === key)
}
function routeLabel(route) {
  const group = gatewayConfig.value?.groups.find((item) => item.id === route.group_id)
  return `${group?.name || route.group_id} / ${route.name || route.host} · ${route.host}`
}
function ruleName(key) {
  const route = gatewayConfig.value?.routes.find((item) => routeKey(item) === key)
  return route ? routeLabel(route) : key || '未匹配反代规则'
}
function outcome(entry) {
  if (entry.freeze_created) return '达到阈值 · 冻结'
  if (entry.outcome === 'ip_frozen') return '冻结 IP 拦截'
  if (entry.outcome === 'forwarded' && entry.security?.length) return '仅记录 · 已放行'
  return (
    {
      blocked: '防火墙拦截',
      auth_failed: '账号验证失败',
      auth_rate_limited: '验证速率限制',
      auth_rejected: '验证请求被拒绝'
    }[entry.outcome] || (entry.ok ? '成功' : '失败')
  )
}
const detailRows = computed(() =>
  selected.value
    ? [
        ['时间', reportDate(selected.value.time)],
        ['关联规则', ruleName(selected.value.rule)],
        ['规则标识', selected.value.rule],
        ['调用', selected.value.action],
        ['目标', selected.value.target],
        ['请求', `${selected.value.method} ${selected.value.path}`],
        ['来源 IP', selected.value.remote],
        ['归属地', selected.value.remote_region],
        [
          '结果',
          `${outcome(selected.value)} · HTTP ${selected.value.status || '—'} · ${selected.value.duration_ms} ms`
        ],
        ['账号验证结果', selected.value.auth_result],
        ['说明', selected.value.message]
      ]
    : []
)
async function load(page = 1, keepSnapshot = false, filters = applied.value) {
  const version = ++requestVersion
  loading.value = true
  error.value = ''
  try {
    const query = logQuery(
      props.scope,
      filters,
      page,
      size.value,
      keepSnapshot ? data.value?.through : undefined
    )
    const result = await adminApi.logs(query, controller.signal)
    if (controller.signal.aborted || version !== requestVersion) return
    data.value = result
    applied.value = { ...filters }
  } catch (err) {
    if (!controller.signal.aborted && version === requestVersion) {
      error.value = err.message
      await nextTick()
      errorElement.value?.focus()
    }
  } finally {
    if (version === requestVersion) loading.value = false
  }
}
function applyFilters() {
  return load(1, false, { ...form.value })
}
function refresh() {
  return load()
}
function changeSize() {
  return load()
}
function reset() {
  form.value = emptyForm()
  return applyFilters()
}
async function goPage(page) {
  await load(page, true)
  if (!error.value) {
    await nextTick()
    resultElement.value?.focus()
  }
}
function show(entry) {
  selected.value = entry
  freezeError.value = ''
  freezeMinutes.value = 60
  freezeReason.value = '管理员根据日志手动冻结'
  nextTick(() => detailDialog.value.showModal())
}
function filterRule() {
  form.value.rule = selected.value.rule
  detailDialog.value.close()
  applyFilters()
}
function filterFirewall() {
  form.value.firewall = selected.value.firewall.id
  showAdvanced.value = true
  detailDialog.value.close()
  applyFilters()
}
function blockBusyClose(event) {
  if (freezing.value) event.preventDefault()
}
async function freeze() {
  if (freezing.value) return
  freezing.value = true
  freezeError.value = ''
  try {
    const result = await adminApi.freezeIP({
      rule: selected.value.rule,
      ip: selected.value.remote,
      minutes: freezeMinutes.value,
      reason: freezeReason.value.trim()
    })
    if (controller.signal.aborted) return
    if (result.write_error) toast.warn('已冻结，但持久化失败，请检查磁盘与权限')
    else toast.success('已冻结此 IP 在所选反代规则的访问')
    detailDialog.value.close()
  } catch (err) {
    if (!controller.signal.aborted) {
      freezeError.value = err.message
      await nextTick()
      freezeErrorElement.value?.focus()
    }
  } finally {
    freezing.value = false
  }
}
onMounted(() => {
  load()
  refreshConfig(controller.signal).catch((err) => {
    if (!controller.signal.aborted) error.value = err.message
  })
})
onUnmounted(() => controller.abort())
</script>

<style scoped>
.explorer-title {
  font-size: 16px;
  color: var(--ink);
}

/* 现代化紧凑筛选栏容器 */
.report-filter-bar {
  display: flex !important;
  flex-direction: column;
  gap: 12px;
  padding: 14px 16px !important;
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
  background: var(--surface);
  box-shadow: var(--shadow-sm);
  min-width: 0;
}

.filter-primary-row {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  width: 100%;
}

.filter-primary-fields {
  display: flex;
  align-items: flex-end;
  flex-wrap: wrap;
  gap: 12px;
  flex: 1;
  min-width: 0;
}

.filter-col {
  display: flex;
  flex-direction: column;
  gap: 5px;
  min-width: 140px;
  flex: 1;
}

.filter-col-sm {
  max-width: 130px;
  min-width: 100px;
  flex: 0 0 auto;
}

.filter-col-search {
  min-width: 220px;
  flex: 2;
}

.col-label {
  font-size: 11.5px;
  font-weight: 600;
  color: var(--muted);
  user-select: none;
}

.search-box {
  position: relative;
  display: flex;
  align-items: center;
  width: 100%;
}

.search-icon {
  position: absolute;
  left: 11px;
  color: var(--muted);
  pointer-events: none;
}

.search-box .form-input {
  padding-left: 32px !important;
  width: 100%;
}

.filter-actions-group {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
  margin-left: auto;
}

.filter-actions-group .btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
}

.filter-toggle-btn {
  position: relative;
  transition: var(--transition-smooth);
}

.filter-toggle-btn.active {
  background: var(--surface-hover);
  color: var(--ink);
  border-color: var(--brand);
}

.filter-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: var(--brand);
  color: #ffffff;
  font-size: 11px;
  font-weight: 700;
  height: 18px;
  padding: 0 5px;
  border-radius: 9px;
  line-height: 1;
}

.toggle-arrow {
  transition: transform 0.2s ease;
}

.toggle-arrow.rotated {
  transform: rotate(180deg);
}

.btn-icon-btn {
  padding: 0 10px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}

.spin-anim {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}

/* 高级筛选抽屉 */
.filter-advanced-drawer {
  padding-top: 14px;
  border-top: 1px dashed var(--line);
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.advanced-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
  gap: 12px;
  align-items: flex-end;
}

/* 抽屉过渡动画 */
.drawer-fade-enter-active,
.drawer-fade-leave-active {
  transition: all 0.25s cubic-bezier(0.16, 1, 0.3, 1);
  overflow: hidden;
}

.drawer-fade-enter-from,
.drawer-fade-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}

.filter-quick-info {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  font-size: 11.5px;
  color: var(--muted);
  padding: 2px 2px 0;
  flex-wrap: wrap;
}

.dirty-badge {
  color: var(--brand);
  font-weight: 600;
  background: rgba(99, 102, 241, 0.1);
  padding: 2px 8px;
  border-radius: var(--radius-sm);
}

/* 详情弹窗样式 */
.detail-dl {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin: 0;
}

.detail-row {
  display: grid;
  grid-template-columns: 110px minmax(0, 1fr);
  gap: 12px;
  font-size: 13px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--line);
}

.detail-row dt {
  color: var(--muted);
  font-weight: 500;
}

.detail-row dd {
  margin: 0;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
  font-family: var(--mono);
  color: var(--ink);
}

.glass-section {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 14px 16px;
  border-radius: var(--radius-md);
  background: rgba(255, 255, 255, 0.02);
  border: 1px solid var(--line);
}

.glass-section h3 {
  font-size: 13.5px;
  font-weight: 600;
  color: var(--ink);
  margin: 0;
}

.detail-freeze-form {
  margin-top: 6px;
}

.freeze-fieldset {
  display: flex;
  flex-direction: column;
  gap: 12px;
  border: none;
  padding: 0;
  margin: 0;
}

/* ================= 响应式适配 (Mobile & Tablet) ================= */
@media (max-width: 768px) {
  .report-filter-bar {
    padding: 12px !important;
    gap: 10px;
  }

  .filter-primary-row {
    flex-direction: column;
    align-items: stretch;
    gap: 10px;
  }

  .filter-primary-fields {
    display: grid !important;
    grid-template-columns: 1fr 1fr;
    gap: 8px;
    width: 100%;
  }

  .filter-col {
    min-width: 0 !important;
    width: 100% !important;
  }

  .filter-col-sm {
    max-width: none !important;
    min-width: 0 !important;
    width: 100% !important;
  }

  .filter-col-search {
    grid-column: 1 / -1;
    min-width: 0 !important;
    width: 100% !important;
  }

  .filter-actions-group {
    display: grid !important;
    grid-template-columns: 1fr 1fr auto auto;
    gap: 8px;
    width: 100%;
    margin-left: 0;
  }

  .filter-actions-group .btn {
    justify-content: center;
    padding-inline: 8px;
  }

  .filter-advanced-drawer {
    padding-top: 10px;
  }

  .advanced-grid {
    grid-template-columns: 1fr !important;
    gap: 10px;
  }

  .filter-quick-info {
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
  }

  .quick-info-text {
    font-size: 11px;
    line-height: 1.4;
  }

  .report-pagination {
    display: flex !important;
    flex-direction: column !important;
    align-items: stretch !important;
    gap: 10px !important;
  }

  .report-pagination .log-result {
    text-align: center !important;
    font-size: 12px !important;
  }

  .report-pagination label {
    justify-content: center !important;
  }

  .report-pagination .report-actions {
    width: 100% !important;
    display: grid !important;
    grid-template-columns: 1fr 1fr !important;
    gap: 8px !important;
  }

  .report-pagination .report-actions .btn {
    width: 100% !important;
    justify-content: center !important;
  }
}

@media (min-width: 480px) and (max-width: 768px) {
  .advanced-grid {
    grid-template-columns: 1fr 1fr !important;
  }
}

@media (max-width: 400px) {
  .filter-actions-group {
    grid-template-columns: 1fr 1fr 36px 36px;
  }
  .filter-actions-group .btn span {
    font-size: 11.5px;
  }
}
</style>
