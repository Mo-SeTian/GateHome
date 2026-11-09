<template>
  <section class="report-page log-explorer" :aria-label="title">
    <h2 v-if="title" class="explorer-title">{{ title }}</h2>
    <p class="report-note">
      翻页保留当前日志快照；筛选或刷新读取最新记录。查询来自当前保留的最多 5000
      条日志，历史清理后的记录不在列表中。
    </p>
    <form class="report-form" @submit.prevent="applyFilters">
      <label v-if="scope === 'project'"
        >项目分类<select v-model="form.category" name="category" class="form-select">
          <option value="">全部分类</option>
          <option v-for="(label, key) in categories" :key="key" :value="key">{{ label }}</option>
        </select></label
      >
      <label v-else
        >反代规则<select v-model="form.rule" name="rule" class="form-select">
          <option value="">全部反代规则</option>
          <option v-for="route in gatewayConfig?.routes" :key="routeKey(route)" :value="routeKey(route)">
            {{ routeLabel(route) }}
          </option>
          <option v-if="form.rule && !hasRoute(form.rule)" :value="form.rule">
            {{ form.rule }}（历史规则）
          </option>
        </select></label
      >
      <label
        >执行结果<select v-model="form.result" name="result" class="form-select">
          <option value="">全部结果</option>
          <option value="success">成功</option>
          <option value="error">失败</option>
        </select></label
      >
      <template v-if="scope !== 'project'">
        <label
          >来源 IP<input
            v-model="form.ip"
            name="ip"
            class="form-input mono-text"
            placeholder="精确 IPv4 / IPv6"
            maxlength="64"
        /></label>
        <label
          >请求方法<select v-model="form.method" name="method" class="form-select">
            <option value="">全部方法</option>
            <option v-for="method in methods" :key="method">{{ method }}</option>
          </select></label
        >
        <label
          >HTTP 状态码<input
            v-model="form.status"
            name="status"
            class="form-input"
            type="number"
            min="100"
            max="599"
            placeholder="例如 403"
        /></label>
      </template>
      <template v-if="securityScope">
        <label
          >防火墙<select v-model="form.firewall" name="firewall" class="form-select">
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
          </select></label
        >
        <label
          >检测引擎<select v-model="form.engine" name="engine" class="form-select">
            <option value="">全部引擎</option>
            <option v-for="engine in engines" :key="engine" :value="engine">{{ engineLabel(engine) }}</option>
          </select></label
        >
        <label
          >处理动作<select v-model="form.decision" name="decision" class="form-select">
            <option value="">全部动作</option>
            <option value="block">拦截</option>
            <option value="detect">仅记录</option>
          </select></label
        >
        <label
          >检测规则编号<input
            v-model="form.rule_id"
            name="rule_id"
            class="form-input"
            type="number"
            min="1"
            max="999999999"
        /></label>
      </template>
      <label
        >开始时间<input v-model="form.from" name="from" class="form-input" type="datetime-local"
      /></label>
      <label>结束时间<input v-model="form.to" name="to" class="form-input" type="datetime-local" /></label>
      <label
        >关键词<input
          v-model="form.search"
          name="search"
          class="form-input"
          maxlength="200"
          placeholder="域名、IP、路径、操作或规则名称"
      /></label>
      <div class="report-actions wide">
        <button type="submit" class="btn btn-primary" :disabled="loading">筛选</button
        ><button type="button" class="btn btn-secondary" :disabled="loading" @click="reset">重置</button
        ><button type="button" class="btn btn-secondary" :disabled="loading" @click="refresh">
          {{ loading ? '读取中…' : '刷新日志' }}
        </button>
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
        <button class="btn btn-ghost" aria-label="关闭" :disabled="freezing" @click="detailDialog.close()">
          <X :size="18" />
        </button>
      </header>
      <template v-if="selected">
        <dl>
          <div v-for="([label, value], index) in detailRows" :key="index">
            <dt>{{ label }}</dt>
            <dd>{{ value || '—' }}</dd>
          </div>
        </dl>
        <section v-if="selected.firewall" class="detail-section">
          <h3>来源 IP 策略</h3>
          <p>
            {{ selected.firewall.name }} · {{ selected.firewall.group || selected.firewall.kind }} ·
            {{ selected.firewall.reason }}
          </p>
          <button v-if="securityScope" class="btn btn-secondary btn-sm" @click="filterFirewall">
            按此策略查询
          </button>
        </section>
        <section v-if="selected.security?.length" class="detail-section">
          <h3>安全规则命中</h3>
          <p v-for="(hit, index) in selected.security" :key="index">
            {{ engineLabel(hit.engine) }} #{{ hit.rule_id }} · {{ hit.name }} ·
            {{ hit.action === 'block' ? '拦截' : '仅记录' }}{{ hit.score ? ' · 分数 ' + hit.score : ''
            }}{{ hit.severity ? ' · ' + hit.severity : '' }}
          </p>
        </section>
        <div class="report-actions detail-section">
          <button v-if="selected.rule" class="btn btn-secondary btn-sm" @click="filterRule">
            按此反代规则查询</button
          ><router-link to="/ip-blocks" class="btn btn-secondary btn-sm">IP 拦截名单</router-link>
        </div>
        <form
          v-if="securityScope && hasRoute(selected.rule) && selected.remote && selected.remote !== '未知'"
          class="detail-section"
          @submit.prevent="freeze"
        >
          <h3>在此反代规则手动冻结 {{ selected.remote }}</h3>
          <p v-if="freezeError" ref="freezeErrorElement" class="report-error" role="alert" tabindex="-1">
            {{ freezeError }}
          </p>
          <fieldset :disabled="freezing">
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
      </template>
    </dialog>
  </section>
</template>

<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, shallowRef } from 'vue'
import { X } from 'lucide-vue-next'
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
.detail-section {
  display: grid;
  gap: 12px;
  margin-top: 20px;
  font-size: 13px;
  overflow-wrap: anywhere;
}
.detail-section h3 {
  font-size: 14px;
}
</style>
