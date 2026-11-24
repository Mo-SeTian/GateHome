<template>
  <div class="rulesets-wrap">
    <SpotlightCard class="report-panel rulesets-panel">
      <div class="panel-header-row">
        <div class="rulesets-title">
          <h2>规则集 (rule-providers)</h2>
          <span class="badge badge-neutral">共 {{ ruleSets.length }} 个</span>
          <span v-if="ruleStatsSummary.known > 0" class="badge badge-ok mono-text">
            合计 {{ ruleStatsSummary.total.toLocaleString() }} 条规则
          </span>
          <span v-if="statsLive" class="badge badge-brand">内核实时统计</span>
        </div>
        <span class="meta-pill">支持远端订阅 / 内置精选 / 手动编写（YAML · TXT · MRS）</span>
      </div>

      <div v-if="ruleSets.length" class="rulesets-list">
        <div v-for="rs in ruleSets" :key="rs.id" class="ruleset-card" :class="{ disabled: !rs.enabled }">
          <div class="ruleset-info">
            <div class="ruleset-title-line">
              <strong class="mono-text">{{ rs.name }}</strong>
              <span class="badge" :class="sourceBadgeClass(rs.source)">{{ sourceLabel(rs.source) }}</span>
              <span class="badge badge-neutral">{{ behaviorLabel(rs.behavior) }}</span>
              <span class="badge badge-neutral mono-text">{{ formatLabel(rs.format) }}</span>
              <span class="badge mono-text" :class="ruleCountOf(rs) != null ? 'badge-ok' : 'badge-neutral'">
                {{ ruleCountLabel(rs) }}
              </span>
              <span v-if="rs.comment" class="rule-comment">{{ rs.comment }}</span>
            </div>

            <div class="ruleset-meta mono-text">
              <span>路径: {{ rs.path }}</span>
              <span v-if="rs.source !== 'manual'"> · 刷新: {{ formatInterval(rs.interval) }}</span>
              <span v-if="statsMap[rs.name]?.updated_at">
                · 更新于: {{ formatUpdatedAt(statsMap[rs.name].updated_at) }}
              </span>
            </div>

            <div v-if="rs.url" class="ruleset-url mono-text">{{ rs.url }}</div>
          </div>

          <div class="rule-actions">
            <button
              v-if="rs.source !== 'manual'"
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="busyId === rs.id || updatingAll || updatingName === rs.name || !rs.enabled"
              :title="rs.enabled ? '立即拉取最新规则集内容' : '请先启用后再更新'"
              :aria-label="`更新规则集 ${rs.name}`"
              @click="updateOne(rs)"
            >
              <RefreshCw :size="13" :class="{ spinning: updatingName === rs.name }" />
              {{ updatingName === rs.name ? '更新中' : '更新' }}
            </button>
            <label class="switch" :aria-label="`启用规则集 ${rs.name}`">
              <input
                type="checkbox"
                :checked="rs.enabled"
                :disabled="busyId === rs.id"
                @change="toggle(rs.id)"
              />
              <span class="switch-slider" />
            </label>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="busyId === rs.id"
              :aria-label="`编辑规则集 ${rs.name}`"
              @click="openModal(rs)"
            >
              <Pencil :size="13" /> 编辑
            </button>
            <button
              type="button"
              class="btn btn-ghost btn-sm text-bad"
              :disabled="busyId === rs.id"
              :aria-label="`删除规则集 ${rs.name}`"
              @click="remove(rs)"
            >
              <Trash2 :size="14" />
            </button>
          </div>
        </div>
      </div>

      <div v-else class="report-empty rulesets-empty">
        <p>暂未配置规则集，可导入远端 URL、选用内置 MetaCubeX 规则集或手动编写 TXT / YAML 规则。</p>
        <button type="button" class="btn btn-secondary btn-sm" @click="openModal()">
          <Plus :size="14" /> 添加第一个规则集
        </button>
      </div>
    </SpotlightCard>

    <dialog ref="rsDialog" class="modal-dialog" aria-labelledby="mihomo-ruleset-dialog-title">
      <div class="modal-header">
        <h2 id="mihomo-ruleset-dialog-title" class="modal-title">{{ form.id ? '编辑规则集' : '新建规则集' }}</h2>
        <button type="button" class="close-modal-btn" aria-label="关闭" @click="rsDialog?.close()">
          <X :size="18" />
        </button>
      </div>

      <form id="mihomo-ruleset-form" class="modal-body mihomo-modal-form" @submit.prevent="submit">
        <p v-if="modalError" class="report-error" role="alert">{{ modalError }}</p>

        <div class="source-tabs" role="radiogroup" aria-label="规则集来源">
          <button
            v-for="opt in sourceOptions"
            :key="opt.id"
            type="button"
            class="source-tab-btn"
            :class="{ active: form.source === opt.id }"
            @click="switchSource(opt.id)"
          >
            {{ opt.label }}
          </button>
        </div>

        <label v-if="form.source === 'builtin'" class="form-field">
          <span>选择内置精选规则集 (MetaCubeX)</span>
          <select v-model="form.preset" name="ruleset_preset" class="form-input" @change="applyPreset(form.preset)">
            <option v-for="p in presets" :key="p.id" :value="p.id">
              {{ p.label }} · {{ p.behavior }} · {{ p.format.toUpperCase() }}
            </option>
          </select>
        </label>

        <div class="ports-grid">
          <label class="form-field">
            <span>规则集名称</span>
            <input
              v-model.trim="form.name"
              name="ruleset_name"
              class="form-input mono-text"
              required
              maxlength="64"
              placeholder="例如：openai 或 local-lan"
              @input="syncDefaultPath"
            />
          </label>

          <label class="form-field">
            <span>匹配模式 (behavior)</span>
            <select v-model="form.behavior" name="ruleset_behavior" class="form-input">
              <option value="domain">域名 (domain)</option>
              <option value="ipcidr">IP 网段 (ipcidr)</option>
              <option value="classical">混合规则 (classical)</option>
            </select>
          </label>
        </div>

        <div class="ports-grid">
          <label class="form-field">
            <span>文件格式 (format)</span>
            <select v-model="form.format" name="ruleset_format" class="form-input" @change="onFormatChange">
              <option value="yaml">YAML (.yaml)</option>
              <option value="text">TXT 纯文本 (.txt)</option>
              <option value="mrs">MRS 二进制 (.mrs)</option>
            </select>
          </label>

          <label class="form-field">
            <span>规则集路径 (相对工作目录)</span>
            <input
              v-model.trim="form.path"
              name="ruleset_path"
              class="form-input mono-text"
              placeholder="./ruleset/example.yaml"
              @input="pathCustom = true"
            />
          </label>
        </div>

        <div v-if="form.source !== 'manual'" class="ports-grid">
          <label class="form-field url-field">
            <span>规则集 URL</span>
            <input
              v-model.trim="form.url"
              name="ruleset_url"
              type="url"
              class="form-input mono-text"
              required
              placeholder="https://cdn.jsdelivr.net/..."
            />
          </label>

          <label class="form-field">
            <span>刷新间隔 (秒)</span>
            <input
              v-model.number="form.interval"
              name="ruleset_interval"
              type="number"
              min="0"
              max="2592000"
              class="form-input mono-text"
            />
          </label>
        </div>

        <template v-else>
          <label v-if="form.format !== 'mrs'" class="form-field">
            <span>规则内容（每行一条{{ form.format === 'yaml' ? '，未写 payload: 时将自动包装' : '' }}）</span>
            <textarea
              v-model="form.content"
              name="ruleset_content"
              class="form-input mono-text ruleset-textarea"
              rows="6"
              required
              :placeholder="manualPlaceholder"
            />
          </label>
          <p v-else class="ruleset-hint">
            MRS 为 Mihomo 预编译二进制格式，请确保上方「规则集路径」对应的 `.mrs` 文件已放置于 Mihomo 工作目录下，或改用「远端 / 内置」自动下载及 `TXT / YAML` 手动编写。
          </p>
        </template>

        <p v-if="form.format === 'mrs' && form.behavior === 'classical'" class="report-warning" role="status">
          注意：MRS 二进制格式仅支持 `domain` 或 `ipcidr` 匹配模式，不支持 `classical` 混合模式。
        </p>

        <label class="form-field">
          <span>备注 (可选)</span>
          <input v-model.trim="form.comment" name="ruleset_comment" class="form-input" maxlength="256" />
        </label>

        <template v-if="!form.id">
          <label class="toggle-card">
            <span>保存时同时在分流规则顶部添加 `RULE-SET` 引用</span>
            <input v-model="form.create_rule" name="ruleset_create_rule" type="checkbox" />
          </label>

          <div v-if="form.create_rule" class="ports-grid">
            <label class="form-field">
              <span>命中后出站目标</span>
              <select v-model="form.rule_target" name="ruleset_rule_target" class="form-input">
                <option v-for="t in targetOptions" :key="t" :value="t">{{ t }}</option>
              </select>
            </label>

            <label class="toggle-card">
              <span>跳过 DNS 解析 (no-resolve)</span>
              <input v-model="form.rule_no_resolve" type="checkbox" />
            </label>
          </div>
        </template>
      </form>

      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" @click="rsDialog?.close()">取消</button>
        <button type="submit" form="mihomo-ruleset-form" class="btn btn-primary" :disabled="modalBusy">
          {{ modalBusy ? '保存中…' : '保存规则集' }}
        </button>
      </div>
    </dialog>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { Pencil, Plus, RefreshCw, Trash2, X } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { adminApi } from '@/api/admin'
import { useToast } from '@/composables/useToast'

const props = defineProps({
  ruleSets: { type: Array, default: () => [] },
  presets: { type: Array, default: () => [] },
  targetOptions: { type: Array, default: () => ['DIRECT', 'REJECT'] },
  coreRunning: { type: Boolean, default: false }
})
const emit = defineEmits(['updated', 'toolbar-state'])

const toast = useToast()
const rsDialog = ref(null)
const modalError = ref('')
const modalBusy = ref(false)
const busyId = ref('')
const pathCustom = ref(false)
const statsMap = ref({})
const statsLive = ref(false)
const updatingAll = ref(false)
const updatingName = ref('')
const controller = new AbortController()

const updatableSets = computed(() =>
  props.ruleSets.filter((rs) => rs.enabled !== false && rs.source !== 'manual' && !!rs.url)
)
const hasUpdatable = computed(() => updatableSets.value.length > 0)

function ruleCountOf(rs) {
  const liveCount = statsMap.value?.[rs.name]?.rule_count
  if (typeof liveCount === 'number' && Number.isFinite(liveCount)) return liveCount
  if (rs.source === 'manual' && rs.format !== 'mrs') return countContentLines(rs.content)
  return null
}

function ruleCountLabel(rs) {
  const cnt = ruleCountOf(rs)
  if (cnt != null) return `${cnt.toLocaleString()} 条规则`
  if (!rs.enabled) return '已停用'
  if (rs.format === 'mrs') return '启动后统计'
  return '待更新'
}

const ruleStatsSummary = computed(() => {
  let total = 0
  let known = 0
  for (const rs of props.ruleSets) {
    const cnt = ruleCountOf(rs)
    if (cnt != null) {
      total += cnt
      known++
    }
  }
  return { total, known }
})

async function loadStats() {
  try {
    const res = await adminApi.mihomoRuleSetStats(controller.signal)
    if (controller.signal.aborted) return
    statsLive.value = !!res?.live
    statsMap.value = res?.providers || {}
  } catch (err) {
    if (err?.name !== 'AbortError') {
      // Non-fatal: fallback manual line counts remain available
    }
  }
}

async function runUpdate(names = []) {
  const isSingle = names.length === 1
  if (isSingle) updatingName.value = names[0]
  else updatingAll.value = true
  try {
    const res = await adminApi.updateMihomoRuleSets(names)
    if (res?.providers) {
      statsLive.value = !!res.live
      statsMap.value = res.providers
    }
    if (isSingle) {
      const item = res?.results?.[0]
      if (item && !item.ok) {
        toast.error(`规则集「${names[0]}」更新失败: ${item.error || '未知错误'}`)
      } else {
        toast.success(`规则集「${names[0]}」已更新`)
      }
    } else if (res?.failed > 0) {
      const firstErr = res.results?.find((r) => !r.ok)?.error
      toast.warning(`更新完成：${res.updated} 个成功，${res.failed} 个失败${firstErr ? `（${firstErr}）` : ''}`)
    } else {
      toast.success(`已完成 ${res?.updated || 0} 个规则集的批量更新`)
    }
  } catch (err) {
    toast.error(err.message)
  } finally {
    if (isSingle) updatingName.value = ''
    else updatingAll.value = false
  }
}

function updateAll() {
  if (updatingAll.value || !hasUpdatable.value) return
  return runUpdate([])
}

function updateOne(rs) {
  if (!rs?.name || updatingAll.value || updatingName.value) return
  return runUpdate([rs.name])
}

watch(
  [updatingAll, hasUpdatable],
  () => {
    emit('toolbar-state', { updatingAll: updatingAll.value, hasUpdatable: hasUpdatable.value })
  },
  { immediate: true }
)

watch(
  () => [props.ruleSets, props.coreRunning],
  () => loadStats()
)

onMounted(loadStats)
onBeforeUnmount(() => controller.abort())

const sourceOptions = [
  { id: 'http', label: '远端规则集' },
  { id: 'builtin', label: '内置规则集' },
  { id: 'manual', label: '手动编写' }
]

const form = reactive({
  id: '',
  name: '',
  source: 'http',
  preset: '',
  behavior: 'domain',
  format: 'yaml',
  url: '',
  path: './ruleset/custom.yaml',
  interval: 86400,
  content: '',
  enabled: true,
  comment: '',
  create_rule: true,
  rule_target: 'DIRECT',
  rule_no_resolve: false
})

const manualPlaceholder = computed(() => {
  if (form.behavior === 'ipcidr') return '192.168.0.0/16\n10.0.0.0/8'
  if (form.behavior === 'classical') return 'DOMAIN-SUFFIX,example.com\nIP-CIDR,172.16.0.0/12'
  return '+.internal.lan\n.example.com'
})

function formatExt(fmt) {
  if (fmt === 'text') return 'txt'
  if (fmt === 'mrs') return 'mrs'
  return 'yaml'
}

function defaultPathFor(name, fmt) {
  const safe = (name || 'custom').replace(/[/\\:*?"<>|\s]+/g, '-').replace(/^-+|-+$/g, '') || 'custom'
  return `./ruleset/${safe}.${formatExt(fmt)}`
}

function syncDefaultPath() {
  if (pathCustom.value) return
  form.path = defaultPathFor(form.name, form.format)
}

function onFormatChange() {
  if (form.format === 'mrs' && form.behavior === 'classical') {
    form.behavior = 'domain'
  }
  if (!pathCustom.value) {
    form.path = defaultPathFor(form.name, form.format)
  } else if (form.path) {
    form.path = form.path.replace(/\.(yaml|yml|txt|mrs)$/i, `.${formatExt(form.format)}`)
  }
}

function applyPreset(presetId) {
  const p = props.presets.find((item) => item.id === presetId)
  if (!p) return
  form.preset = p.id
  form.name = p.name
  form.behavior = p.behavior
  form.format = p.format
  form.url = p.url
  form.path = p.path
  form.interval = p.interval || 86400
  if (!form.comment) form.comment = p.description || p.label
  form.rule_no_resolve = p.behavior === 'ipcidr'
  pathCustom.value = false
}

function switchSource(src) {
  form.source = src
  modalError.value = ''
  if (src === 'builtin' && props.presets.length) {
    const id = form.preset || props.presets[0].id
    applyPreset(id)
  } else if (src === 'manual') {
    if (form.format === 'mrs') form.format = 'text'
    syncDefaultPath()
  } else {
    if (!form.interval) form.interval = 86400
    syncDefaultPath()
  }
}

function openModal(rs = null) {
  modalError.value = ''
  if (rs) {
    form.id = rs.id
    form.name = rs.name || ''
    form.source = rs.source || 'http'
    form.preset = rs.preset || ''
    form.behavior = rs.behavior || 'domain'
    form.format = rs.format || 'yaml'
    form.url = rs.url || ''
    form.path = rs.path || defaultPathFor(rs.name, rs.format)
    form.interval = rs.interval ?? 86400
    form.content = rs.content || ''
    form.enabled = rs.enabled !== false
    form.comment = rs.comment || ''
    form.create_rule = false
    pathCustom.value = true
  } else {
    form.id = ''
    form.name = ''
    form.source = 'http'
    form.preset = props.presets[0]?.id || ''
    form.behavior = 'domain'
    form.format = 'yaml'
    form.url = ''
    form.path = './ruleset/custom.yaml'
    form.interval = 86400
    form.content = ''
    form.enabled = true
    form.comment = ''
    form.create_rule = true
    form.rule_target = props.targetOptions[0] || 'DIRECT'
    form.rule_no_resolve = false
    pathCustom.value = false
  }
  rsDialog.value?.showModal()
}

async function submit() {
  modalError.value = ''
  modalBusy.value = true
  try {
    const payload = {
      id: form.id,
      name: form.name,
      source: form.source,
      preset: form.preset,
      behavior: form.behavior,
      format: form.format,
      url: form.url,
      path: form.path,
      interval: Number(form.interval) || 0,
      content: form.content,
      enabled: form.enabled,
      comment: form.comment,
      create_rule: !form.id && form.create_rule,
      rule_target: form.rule_target,
      rule_no_resolve: form.rule_no_resolve
    }
    const status = await adminApi.saveMihomoRuleSet(payload)
    emit('updated', status)
    rsDialog.value?.close()
    toast.success(form.id ? '规则集已更新' : '规则集已保存')
    await loadStats()
  } catch (err) {
    modalError.value = err.message
  } finally {
    modalBusy.value = false
  }
}

async function toggle(id) {
  busyId.value = id
  try {
    const status = await adminApi.toggleMihomoRuleSet(id)
    emit('updated', status)
    await loadStats()
  } catch (err) {
    toast.error(err.message)
  } finally {
    busyId.value = ''
  }
}

async function remove(rs) {
  busyId.value = rs.id
  try {
    const status = await adminApi.deleteMihomoRuleSet(rs.id)
    emit('updated', status)
    toast.success(`规则集「${rs.name}」已删除`)
    await loadStats()
  } catch (err) {
    toast.error(err.message)
  } finally {
    busyId.value = ''
  }
}

function sourceLabel(src) {
  if (src === 'builtin') return '内置'
  if (src === 'manual') return '手动'
  return '远端'
}

function sourceBadgeClass(src) {
  if (src === 'builtin') return 'badge-brand'
  if (src === 'manual') return 'badge-ok'
  return 'badge-info'
}

function behaviorLabel(b) {
  if (b === 'ipcidr') return 'IP (ipcidr)'
  if (b === 'classical') return '混合 (classical)'
  return '域名 (domain)'
}

function formatLabel(fmt) {
  if (fmt === 'text') return 'TXT'
  return String(fmt || 'yaml').toUpperCase()
}

function formatInterval(sec) {
  const n = Number(sec) || 0
  if (n <= 0) return '不自动刷新'
  if (n % 86400 === 0) return `${n / 86400} 天 (${n}s)`
  if (n % 3600 === 0) return `${n / 3600} 小时 (${n}s)`
  return `${n}s`
}

function formatUpdatedAt(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString()
}

function countContentLines(content) {
  if (!content) return 0
  return content
    .split(/\r?\n/)
    .map((l) => l.trim())
    .filter((l) => l && !l.startsWith('#') && l !== 'payload:').length
}

defineExpose({ openModal, updateAll })
</script>

<style scoped>
.rulesets-wrap {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  min-width: 0;
  max-width: 100%;
}

.rulesets-panel {
  padding: 16px;
  min-width: 0;
  max-width: 100%;
}

.panel-header-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 14px;
}

.rulesets-title {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  min-width: 0;
}

.rulesets-title h2 {
  margin: 0;
  font-size: 16px;
  font-weight: 700;
}

.meta-pill {
  font-size: 12px;
  color: var(--muted);
}

.rulesets-list {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 10px;
  min-width: 0;
}

.ruleset-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
  padding: 12px 14px;
  border-radius: var(--radius-sm);
  background: var(--strip-bg);
  border: 1px solid var(--line);
  min-width: 0;
  max-width: 100%;
}

.ruleset-card.disabled {
  opacity: 0.55;
}

.ruleset-info {
  display: grid;
  gap: 4px;
  min-width: 0;
  flex: 1;
}

.ruleset-title-line {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  min-width: 0;
}

.badge-info {
  color: var(--brand);
  background: rgba(99, 102, 241, 0.12);
  border-color: rgba(99, 102, 241, 0.28);
}

.rule-comment {
  font-size: 11.5px;
  color: var(--muted);
}

.ruleset-meta,
.ruleset-url {
  font-size: 11.5px;
  color: var(--muted);
  overflow-wrap: anywhere;
}

.ruleset-url {
  color: var(--faint);
}

.rule-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}

.rulesets-empty {
  display: grid;
  justify-items: center;
  gap: 10px;
  padding: 24px 12px;
}

.rulesets-empty p {
  margin: 0;
}

.source-tabs {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 4px;
  padding: 3px;
  border-radius: var(--radius-sm);
  background: var(--strip-bg);
  border: 1px solid var(--line);
}

.source-tab-btn {
  min-height: 36px;
  padding: 6px 8px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--muted);
  font-size: 12.5px;
  font-weight: 600;
  cursor: pointer;
  transition: var(--transition-smooth);
}

.source-tab-btn.active {
  background: var(--card-bg);
  color: var(--ink);
  box-shadow: 0 1px 3px rgba(15, 23, 42, 0.08);
}

.ports-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
}

.form-field {
  display: grid;
  gap: 6px;
  font-size: 12px;
  color: var(--ink-secondary);
  min-width: 0;
}

.toggle-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 10px 12px;
  border-radius: var(--radius-sm);
  background: var(--strip-bg);
  border: 1px solid var(--line);
  font-size: 12.5px;
  color: var(--ink-secondary);
  cursor: pointer;
  min-width: 0;
}

.ruleset-textarea {
  min-height: 140px !important;
  font-size: 12px;
  line-height: 1.55;
  tab-size: 2;
  white-space: pre;
}

.ruleset-hint {
  margin: 0;
  padding: 10px 12px;
  border-radius: var(--radius-sm);
  background: var(--strip-bg);
  border: 1px solid var(--line);
  font-size: 12px;
  color: var(--muted);
  line-height: 1.5;
}

.spinning {
  animation: ruleset-spin 0.85s linear infinite;
}

@keyframes ruleset-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
