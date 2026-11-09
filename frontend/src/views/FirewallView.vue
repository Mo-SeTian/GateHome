<template>
  <div class="firewall-page animate-fade-in">
    <div class="page-topbar">
      <div>
        <h1 class="page-title">防火墙与 WAF 攻击防护</h1>
        <p class="page-subtitle">管理来源 IP / CIDR、规则订阅与 Coraza · OWASP CRS 防护，保存后立即生效</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-secondary" :disabled="loading || isSavingConfig" @click="reload">刷新</button>
        <button class="btn btn-secondary" @click="$router.push('/firewall-logs')">
          <FileSearch :size="14" /> 拦截审计记录
        </button>
        <button class="btn btn-primary" :disabled="!gatewayConfig || isSavingConfig" @click="edit()">
          <Plus :size="15" /> 添加防火墙策略
        </button>
      </div>
    </div>
    <p v-if="loading" role="status">正在读取防火墙配置…</p>
    <p v-if="loadError" class="error" role="alert">{{ loadError }}</p>
    <div class="strategy-tabs" role="group" aria-label="选择防火墙策略">
      <button
        v-for="firewall in firewalls"
        :key="firewall.id"
        class="strategy-tab-btn"
        :class="{ active: currentFwId === firewall.id }"
        :aria-pressed="currentFwId === firewall.id"
        @click="currentFwId = firewall.id"
      >
        <Shield :size="15" /> {{ firewall.name }}
        <span class="badge" :class="firewall.default_action === 'deny' ? 'badge-bad' : 'badge-ok'"
          >未命中：{{ firewall.default_action === 'deny' ? '禁止' : '允许' }}</span
        >
      </button>
    </div>
    <template v-if="activeFw">
      <div class="policy-actions">
        <div>
          <strong>{{ activeFw.name }}</strong>
          <p class="note">关联 {{ references.length }} 项服务；端口转发仅应用来源 IP 规则。</p>
        </div>
        <div class="header-actions">
          <button class="btn btn-secondary" :disabled="isSavingConfig" @click="edit(activeFw)">
            <Edit3 :size="14" /> 编辑策略
          </button>
          <button
            class="btn btn-ghost text-bad"
            :disabled="isSavingConfig || references.length > 0"
            :title="references.length ? '先解除服务引用，再删除此策略' : '删除此策略'"
            @click="remove"
          >
            删除策略
          </button>
        </div>
      </div>
      <p v-if="waiting" class="waiting-note" role="status">
        引用的订阅尚无有效缓存，整个防火墙暂拒访问。更新失败时会保留上次成功列表。
      </p>
      <details v-if="references.length" class="references ref-details">
        <summary>
          <span>查看已关联服务（共 {{ references.length }} 项）</span>
        </summary>
        <div class="ref-pills-wrap">
          <span v-for="item in references" :key="item.key" class="ref-pill">{{ item.label }}</span>
        </div>
      </details>
      <div class="firewall-main-grid">
        <SpotlightCard class="panel-card">
          <div class="panel-head">
            <Layers :size="18" class="brand-text" />
            <div>
              <h2 class="panel-title">来源 IP 规则组</h2>
              <p class="note">从上到下匹配，第一条命中决定允许或禁止</p>
            </div>
          </div>
          <div v-for="(group, index) in activeFw.groups" :key="index" class="group-card-item">
            <div class="group-head">
              <strong
                ><span class="brand-text mono-text">#{{ index + 1 }}</span> {{ group.name }}</strong
              >
              <div class="header-actions">
                <span class="badge badge-brand">{{
                  group.match === 'include' ? '包含组内' : '排除组外'
                }}</span
                ><span class="badge" :class="group.action === 'allow' ? 'badge-ok' : 'badge-bad'">{{
                  group.action === 'allow' ? '允许' : '禁止'
                }}</span>
              </div>
            </div>
            <p v-if="group.cidrs?.length" class="mono-text cidrs-text">{{ group.cidrs.join(', ') }}</p>
            <div v-if="group.subscriptions?.length" class="subscription-tags">
              <span v-for="id in group.subscriptions" :key="id" class="badge badge-brand"
                ><Download :size="11" /> {{ subscriptionLabel(id) }}</span
              >
            </div>
          </div>
          <p v-if="!activeFw.groups?.length" class="note">暂无 IP 组，所有来源 IP 执行默认动作。</p>
          <p class="note">
            未命中任何规则时：<strong>{{
              activeFw.default_action === 'allow' ? '允许访问' : '禁止访问'
            }}</strong>
          </p>
        </SpotlightCard>
        <SpotlightCard class="panel-card">
          <div class="panel-head">
            <ShieldAlert :size="18" class="brand-text" />
            <h2 class="panel-title">Web 攻击防护</h2>
            <span
              class="badge"
              :class="
                activeFw.protection.waf.mode === 'block'
                  ? 'badge-ok'
                  : activeFw.protection.waf.mode === 'detect'
                    ? 'badge-warn'
                    : 'badge-neutral'
              "
              >{{ wafLabel(activeFw.protection.waf.mode) }}</span
            >
          </div>
          <div class="summary-row">
            <span>检测等级</span><strong>PL{{ activeFw.protection.waf.level || 1 }}</strong>
          </div>
          <div class="summary-row">
            <span>请求体检查上限</span
            ><strong>{{ (activeFw.protection.waf.body_limit || 1048576) / 1024 }} KiB</strong>
          </div>
          <h3 class="panel-title">规则例外与豁免</h3>
          <div
            v-for="(exception, index) in activeFw.protection.waf.exceptions"
            :key="index"
            class="rule-summary"
          >
            <span class="badge badge-neutral">CRS #{{ exception.rule_id }}</span>
            <p class="mono-text">
              {{ exception.host || '全部域名' }} · {{ exception.path
              }}{{ exception.parameter ? ' · ' + exception.parameter : '' }}
            </p>
          </div>
          <p v-if="!activeFw.protection.waf.exceptions?.length" class="note">暂无规则例外。</p>
          <h3 class="panel-title">自定义 HTTP 规则</h3>
          <div v-for="(rule, index) in activeFw.protection.rules" :key="index" class="rule-summary">
            <strong
              >{{ index + 1 }}. {{ rule.name }} · {{ rule.action === 'block' ? '拦截' : '仅记录' }}</strong
            >
            <p class="mono-text">{{ targetLabel(rule.target) }} · {{ rule.pattern }}</p>
          </div>
          <p v-if="!activeFw.protection.rules?.length" class="note">暂无自定义规则。</p>
          <div class="summary-row">
            <span>访问限速</span
            ><strong>{{
              activeFw.protection.rate.enabled
                ? `${activeFw.protection.rate.window_seconds} 秒 / ${activeFw.protection.rate.requests} 次`
                : '未启用'
            }}</strong>
          </div>
          <p v-if="activeFw.protection.rate.enabled" class="mono-text note">
            路径前缀 {{ activeFw.protection.rate.path }}
          </p>
          <div class="summary-row">
            <span>防护触发自动冻结</span
            ><strong>{{
              activeFw.protection.freeze.enabled
                ? `${activeFw.protection.freeze.window_seconds} 秒内 ${activeFw.protection.freeze.failures} 次`
                : '未启用'
            }}</strong>
          </div>
          <p v-if="activeFw.protection.freeze.enabled" class="note">
            冻结 {{ activeFw.protection.freeze.seconds / 60 }} 分钟；<router-link to="/ip-blocks"
              >管理 IP 拦截名单</router-link
            >
          </p>
        </SpotlightCard>
      </div>
    </template>
    <div v-if="gatewayConfig && !firewalls.length" class="empty-state">
      <Shield :size="32" />
      <p>暂无防火墙策略，添加后可在反代或端口转发中关联。</p>
    </div>
    <FirewallEditor
      v-if="editor"
      :snapshot="editor.snapshot"
      :firewall="editor.firewall"
      @close="editor = null"
      @saved="saved"
    />
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, shallowRef, watch } from 'vue'
import { Download, Edit3, FileSearch, Layers, Plus, Shield, ShieldAlert } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import FirewallEditor from '@/components/FirewallEditor.vue'
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
const firewalls = computed(() => gatewayConfig.value?.firewalls ?? [])
const currentFwId = ref('')
const activeFw = computed(() => firewalls.value.find((item) => item.id === currentFwId.value))
const editor = shallowRef(null)
const loading = ref(false)
const loadError = ref('')
const controller = new AbortController()
const references = computed(() => [
  ...(gatewayConfig.value?.routes ?? [])
    .filter((item) => item.firewall_id === currentFwId.value)
    .map((item) => ({
      key: 'route/' + (item.id || item.group_id + '/' + item.host),
      label: `反代 · ${item.name || item.host} · ${item.host}`
    })),
  ...(gatewayConfig.value?.forwards ?? [])
    .filter((item) => item.firewall_id === currentFwId.value)
    .map((item) => ({
      key: 'forward/' + item.id,
      label: `转发 · ${item.name} · ${item.protocol.toUpperCase()} :${item.listen_port}`
    }))
])
const waiting = computed(
  () =>
    gatewayStatus.value &&
    activeFw.value?.groups?.some((group) =>
      group.subscriptions?.some(
        (id) => !gatewayStatus.value.subscriptions?.find((status) => status.id === id)?.ready
      )
    )
)
watch(
  firewalls,
  (items) => {
    if (!items.some((item) => item.id === currentFwId.value)) currentFwId.value = items[0]?.id ?? ''
  },
  { immediate: true }
)

function subscriptionLabel(id) {
  const subscription = gatewayConfig.value.subscriptions.find((item) => item.id === id)
  const status = gatewayStatus.value?.subscriptions?.find((item) => item.id === id)
  return `${subscription?.name || id} · ${status?.ready ? status.count.toLocaleString() + ' 条' : '等待有效缓存'}`
}
function wafLabel(mode) {
  return { block: '拦截攻击', detect: '仅记录', off: '已关闭' }[mode] || '已关闭'
}
function targetLabel(target) {
  return { user_agent: 'User-Agent', path: '请求路径', method: '请求方法' }[target] || target
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
function edit(firewall) {
  const snapshot = structuredClone(gatewayConfigSnapshot.value)
  editor.value = {
    snapshot,
    firewall: firewall ? snapshot.config.firewalls.find((item) => item.id === firewall.id) : undefined
  }
}
async function saved(id) {
  editor.value = null
  currentFwId.value = id
  toast.success('防火墙策略已保存并立即生效')
  try {
    await refreshStatus(controller.signal)
  } catch (err) {
    if (!controller.signal.aborted && err.status !== 401)
      toast.warn('策略已保存，订阅状态刷新失败，请稍后刷新')
  }
}
async function remove() {
  if (
    isSavingConfig.value ||
    references.value.length ||
    !confirm(`删除防火墙策略「${activeFw.value.name}」？`)
  )
    return
  const snapshot = structuredClone(gatewayConfigSnapshot.value)
  const id = currentFwId.value
  try {
    await saveConfig(snapshot, {
      ...snapshot.config,
      firewalls: snapshot.config.firewalls.filter((item) => item.id !== id)
    })
    toast.success('防火墙策略已删除')
  } catch (err) {
    toast.error(err.message)
  }
}
onMounted(reload)
onUnmounted(() => controller.abort())
</script>

<style scoped>
.firewall-page {
  display: flex;
  flex-direction: column;
  gap: 20px;
}
.page-topbar,
.policy-actions,
.group-head,
.summary-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
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
.header-actions,
.strategy-tabs,
.subscription-tags {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.strategy-tab-btn {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 9px 14px;
  border-radius: var(--radius-sm);
  background: var(--strip-bg);
  border: 1px solid var(--line);
  color: var(--muted);
  font-size: 13px;
}
.strategy-tab-btn.active {
  background: rgba(99, 102, 241, 0.18);
  border-color: var(--brand);
  color: var(--ink);
}
.firewall-main-grid {
  display: grid;
  grid-template-columns: 1.1fr 1fr;
  gap: 16px;
  align-items: start;
}
.panel-card {
  padding: 20px;
  min-width: 0;
}
.panel-card :deep(.card-inner) {
  display: grid;
  gap: 16px;
}
.panel-head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.panel-title {
  font-size: 14px;
  font-weight: 700;
  color: var(--ink);
}
.brand-text {
  color: var(--brand-light);
}
.group-card-item,
.rule-summary {
  display: grid;
  gap: 10px;
  padding: 14px;
  border: 1px solid var(--line);
  background: var(--strip-bg);
  border-radius: var(--radius-sm);
}
.group-head,
.summary-row,
.rule-summary {
  color: var(--ink-secondary);
  font-size: 13px;
}
.summary-row {
  border-top: 1px solid var(--line);
  padding-top: 12px;
}
p {
  margin: 0;
  overflow-wrap: anywhere;
}
.note,
.references {
  color: var(--muted);
  font-size: 12px;
  line-height: 1.6;
}
.references summary {
  cursor: pointer;
}
.references p {
  margin-top: 6px;
}
.cidrs-text {
  font-size: 12px;
  color: var(--ink-secondary);
}
.subscription-tags .badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  white-space: normal;
}
.waiting-note {
  padding: 14px;
  border: 1px solid var(--warn);
  border-radius: var(--radius-sm);
  color: var(--warn);
  font-size: 13px;
}
.error,
.text-bad {
  color: var(--bad);
}
.empty-state {
  display: grid;
  justify-items: center;
  gap: 12px;
  padding: 48px 16px;
  color: var(--muted);
}
@media (max-width: 1100px) {
  .firewall-main-grid {
    grid-template-columns: 1fr;
  }
}
</style>
