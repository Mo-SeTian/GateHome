<template>
  <dialog
    ref="dialog"
    class="firewall-dialog"
    aria-labelledby="firewall-title"
    @close="emit('close')"
    @cancel="blockBusyClose"
  >
    <header class="firewall-header">
      <div class="heading-group">
        <Shield :size="28" class="heading-icon" aria-hidden="true" />
        <div>
          <h2 id="firewall-title">{{ firewall ? '编辑' : '添加' }}防火墙策略</h2>
          <p>统一管理访问来源与 Web 防护</p>
        </div>
      </div>
      <button
        type="button"
        class="btn btn-ghost firewall-close"
        aria-label="关闭"
        :disabled="isSavingConfig"
        @click="dialog.close()"
      >
        <X :size="18" />
      </button>
    </header>
    <form class="firewall-form" novalidate @submit.prevent="save">
      <fieldset class="editor-fields" :disabled="isSavingConfig">
        <div class="policy-basics form-row-2">
          <label class="form-field">
            策略名称
            <input
              v-model="draft.name"
              name="name"
              class="form-input"
              maxlength="100"
              placeholder="例如：家庭服务防护"
              required
              autofocus
            />
          </label>
          <label class="form-field">
            IP 未命中时
            <select v-model="draft.default_action" name="default_action" class="form-select">
              <option value="deny">禁止访问</option>
              <option value="allow">允许访问</option>
            </select>
          </label>
        </div>
        <div class="editor-layout">
          <nav class="section-nav" aria-label="防火墙配置分区">
            <button
              v-for="section in sections"
              :key="section.id"
              type="button"
              class="section-tab"
              :aria-pressed="activeSection === section.id"
              :aria-controls="'firewall-panel-' + section.id"
              @click="selectSection(section.id)"
            >
              <component :is="section.icon" :size="16" aria-hidden="true" />
              <span>{{ section.label }}</span>
            </button>
          </nav>
          <div ref="content" class="editor-content">
            <p v-if="error" ref="errorElement" class="editor-error" role="alert" tabindex="-1">{{ error }}</p>
            <section
              v-show="activeSection === 'ip'"
              id="firewall-panel-ip"
              class="editor-section"
              data-section="ip"
              aria-labelledby="firewall-section-ip"
            >
              <div class="section-heading">
                <h3 id="firewall-section-ip">来源 IP 规则</h3>
                <p>按顺序匹配，第一条命中决定允许或禁止。</p>
              </div>
              <p>从上到下匹配，第一条命中决定允许或禁止。手动 IP 与勾选订阅合并；“排除”匹配组外 IP。</p>
              <fieldset v-for="(group, index) in draft.groups" :key="index" class="rule-fields ip-group">
                <legend>IP 组 #{{ index + 1 }}</legend>
                <div class="rule-actions">
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm"
                    :disabled="index === 0"
                    @click="move(draft.groups, index, -1)"
                  >上移</button>
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm"
                    :disabled="index === draft.groups.length - 1"
                    @click="move(draft.groups, index, 1)"
                  >下移</button>
                  <button
                    type="button"
                    class="btn btn-ghost btn-sm text-bad"
                    @click="draft.groups.splice(index, 1)"
                  >移除组</button>
                </div>
                <label class="form-field">
                  组名称
                  <input
                    v-model="group.name"
                    name="group_name"
                    class="form-input"
                    maxlength="100"
                    required
                  />
                </label>
                <div class="form-row-2">
                  <label class="form-field">
                    匹配方式
                    <select v-model="group.match" name="match" class="form-select">
                      <option value="include">包含：匹配组内 IP</option>
                      <option value="exclude">排除：匹配组外 IP</option>
                    </select>
                  </label>
                  <label class="form-field">
                    命中动作
                    <select v-model="group.action" name="action" class="form-select">
                      <option value="allow">允许访问</option>
                      <option value="deny">禁止访问</option>
                    </select>
                  </label>
                </div>
                <label class="form-field">
                  手动 IPv4 / IPv6 / CIDR
                  <textarea
                    v-model="group.cidrsText"
                    name="cidrs"
                    class="form-input mono-text"
                    rows="3"
                    placeholder="每行一个，或用逗号分隔；最多 200 条"
                  />
                </label>
                <div class="subscription-choices">
                  <span>引用现有 IP 规则订阅</span>
                  <label
                    v-for="subscription in snapshot.config.subscriptions"
                    :key="subscription.id"
                    class="check-label"
                  >
                    <input
                      v-model="group.subscriptions"
                      name="subscription"
                      type="checkbox"
                      :value="subscription.id"
                      :disabled="!subscription.enabled"
                    />
                    {{ subscription.name }}
                    {{ !subscription.enabled ? '（已停用）' : '' }}
                  </label>
                  <p v-if="!snapshot.config.subscriptions?.length">暂无订阅，可先填写手动网段。</p>
                </div>
              </fieldset>
              <button
                type="button"
                class="btn btn-secondary btn-sm section-add"
                :disabled="draft.groups.length >= 100"
                @click="addGroup"
              >
                <Plus :size="13" />
                添加 IP 组
              </button>
              <p>任一引用的订阅没有有效缓存时，整个策略暂拒访问。仅使用 Web 防护时，将 IP 未命中动作设为允许。</p>
            </section>
            <section
              v-show="activeSection === 'waf'"
              id="firewall-panel-waf"
              class="editor-section"
              data-section="waf"
              aria-labelledby="firewall-section-waf"
            >
              <div class="section-heading">
                <h3 id="firewall-section-waf">Web 攻击防护</h3>
                <p>Coraza / OWASP CRS · 按需调整检测与规则例外。</p>
              </div>
              <div class="form-row-2">
                <label class="form-field">
                  WAF 模式
                  <select v-model="draft.protection.waf.mode" name="waf_mode" class="form-select">
                    <option value="off">关闭</option>
                    <option value="detect">仅记录</option>
                    <option value="block">拦截攻击</option>
                  </select>
                </label>
                <label class="form-field">
                  检测等级
                  <select
                    v-model.number="draft.protection.waf.level"
                    name="waf_level"
                    class="form-select"
                  >
                    <option v-for="level in 4" :key="level" :value="level">PL{{ level }}{{ level === 1 ? ' · 基础（推荐）' : '' }}</option>
                  </select>
                </label>
              </div>
              <label class="form-field">
                请求体检查上限 KiB
                <input
                  v-model.number="draft.bodyKiB"
                  name="waf_body_kib"
                  class="form-input"
                  type="number"
                  min="64"
                  max="32768"
                  step="any"
                  required
                />
              </label>
              <p>建议先仅记录，再按误报调整例外。拦截模式超限返回 413；仅记录模式检查可用前缀，提示未完成完整检查并转发全部正文。响应及
            WebSocket 消息不检查。</p>
              <fieldset
                v-for="(exception, index) in draft.protection.waf.exceptions"
                :key="index"
                class="rule-fields waf-exception"
              >
                <legend>规则例外 #{{ index + 1 }}</legend>
                <button
                  type="button"
                  class="btn btn-ghost btn-sm text-bad"
                  @click="draft.protection.waf.exceptions.splice(index, 1)"
                >移除例外</button>
                <div class="form-row-2">
                  <label class="form-field">
                    CRS 规则编号
                    <input
                      v-model.number="exception.rule_id"
                      name="exception_id"
                      type="number"
                      min="911000"
                      max="948999"
                      class="form-input"
                      required
                    />
                  </label>
                  <label class="form-field">
                    服务域名（可选）
                    <input
                      v-model="exception.host"
                      name="exception_host"
                      class="form-input"
                      placeholder="留空适用于本策略所有域名"
                    />
                  </label>
                  <label class="form-field">
                    路径前缀
                    <input
                      v-model="exception.path"
                      name="exception_path"
                      class="form-input mono-text"
                      maxlength="256"
                      required
                    />
                  </label>
                  <label class="form-field">
                    参数名（可选）
                    <input
                      v-model="exception.parameter"
                      name="exception_parameter"
                      class="form-input"
                      maxlength="100"
                    />
                  </label>
                </div>
              </fieldset>
              <button
                type="button"
                class="btn btn-secondary btn-sm section-add"
                :disabled="draft.protection.waf.exceptions.length >= 50"
                @click="addException"
              >添加规则例外</button>
              <p>仅豁免指定 CRS
            规则；路径按前缀匹配，参数留空豁免该请求的整条规则。路径只支持字母、数字、/、_、.、~、-。</p>
            </section>
            <section
              v-show="activeSection === 'http'"
              id="firewall-panel-http"
              class="editor-section"
              data-section="http"
              aria-labelledby="firewall-section-http"
            >
              <div class="section-heading">
                <h3 id="firewall-section-http">HTTP 规则</h3>
                <p>按顺序检查请求特征，匹配规则后执行对应动作。</p>
              </div>
              <p>按顺序匹配正则表达式，支持 (?i) 忽略大小写。拦截规则立即拒绝；仅记录规则继续检查。UA
            规则建议结合限速。</p>
              <fieldset
                v-for="(rule, index) in draft.protection.rules"
                :key="index"
                class="rule-fields http-rule"
              >
                <legend>HTTP 规则 #{{ index + 1 }}</legend>
                <div class="rule-actions">
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm"
                    :disabled="index === 0"
                    @click="move(draft.protection.rules, index, -1)"
                  >上移</button>
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm"
                    :disabled="index === draft.protection.rules.length - 1"
                    @click="move(draft.protection.rules, index, 1)"
                  >下移</button>
                  <button
                    type="button"
                    class="btn btn-ghost btn-sm text-bad"
                    @click="draft.protection.rules.splice(index, 1)"
                  >移除规则</button>
                </div>
                <label class="form-field">
                  规则名称
                  <input
                    v-model="rule.name"
                    name="http_name"
                    class="form-input"
                    maxlength="100"
                    required
                  />
                </label>
                <div class="form-row-2">
                  <label class="form-field">
                    检查目标
                    <select v-model="rule.target" name="http_target" class="form-select">
                      <option value="user_agent">User-Agent</option>
                      <option value="path">请求路径</option>
                      <option value="method">请求方法</option>
                    </select>
                  </label>
                  <label class="form-field">
                    动作
                    <select v-model="rule.action" name="http_action" class="form-select">
                      <option value="block">拦截</option>
                      <option value="detect">仅记录</option>
                    </select>
                  </label>
                </div>
                <label class="form-field">
                  正则表达式
                  <input
                    v-model="rule.pattern"
                    name="http_pattern"
                    class="form-input mono-text"
                    maxlength="256"
                    required
                  />
                </label>
              </fieldset>
              <button
                type="button"
                class="btn btn-secondary btn-sm section-add"
                :disabled="draft.protection.rules.length >= 50"
                @click="
              draft.protection.rules.push({ name: '', target: 'user_agent', pattern: '', action: 'block' })
            "
              >添加 HTTP 规则</button>
            </section>
            <section
              v-show="activeSection === 'rate'"
              id="firewall-panel-rate"
              class="editor-section"
              data-section="rate"
              aria-labelledby="firewall-section-rate"
            >
              <div class="section-heading">
                <h3 id="firewall-section-rate">限速与冻结</h3>
                <p>控制请求频率，自动处理重复拦截来源。</p>
              </div>
              <div class="protection-block">
                <div class="protection-heading">
                  <h4>访问频率限制</h4>
                  <label class="switch firewall-switch">
                    <input
                      v-model="draft.protection.rate.enabled"
                      name="rate_enabled"
                      type="checkbox"
                      role="switch"
                      aria-label="启用访问频率限制"
                    />
                    <span class="switch-slider" aria-hidden="true" />
                  </label>
                </div>
                <fieldset
                  class="control-grid"
                  :disabled="!draft.protection.rate.enabled"
                  aria-label="访问频率限制参数"
                >
                  <label class="form-field">
                    时间窗口
                    <span class="unit-control">
                      <input
                        v-model.number="draft.protection.rate.window_seconds"
                        name="rate_window"
                        type="number"
                        min="1"
                        max="3600"
                        class="form-input"
                        required
                      />
                      <span>秒</span>
                    </span>
                  </label>
                  <label class="form-field">
                    允许次数
                    <span class="unit-control">
                      <input
                        v-model.number="draft.protection.rate.requests"
                        name="rate_requests"
                        type="number"
                        min="1"
                        max="100000"
                        class="form-input"
                        required
                      />
                      <span>次</span>
                    </span>
                  </label>
                  <label class="form-field">
                    路径前缀
                    <input
                      v-model="draft.protection.rate.path"
                      name="rate_path"
                      class="form-input mono-text"
                      maxlength="256"
                      required
                    />
                  </label>
                </fieldset>
                <p>每个服务、每个来源 IP 独立计数；超限返回 429 和重试时间。前置代理会影响服务器看到的来源地址。</p>
              </div>
              <div class="protection-block">
                <div class="protection-heading">
                  <h4>自动冻结</h4>
                  <label class="switch firewall-switch">
                    <input
                      v-model="draft.protection.freeze.enabled"
                      name="security_freeze_enabled"
                      type="checkbox"
                      role="switch"
                      aria-label="启用防护自动冻结"
                    />
                    <span class="switch-slider" aria-hidden="true" />
                  </label>
                </div>
                <fieldset
                  class="control-grid"
                  :disabled="!draft.protection.freeze.enabled"
                  aria-label="自动冻结参数"
                >
                  <label class="form-field">
                    累计窗口
                    <span class="unit-control">
                      <input
                        v-model.number="draft.protection.freeze.window_seconds"
                        name="security_freeze_window"
                        type="number"
                        min="1"
                        max="86400"
                        class="form-input"
                        required
                      />
                      <span>秒</span>
                    </span>
                  </label>
                  <label class="form-field">
                    触发次数
                    <span class="unit-control">
                      <input
                        v-model.number="draft.protection.freeze.failures"
                        name="security_freeze_failures"
                        type="number"
                        min="2"
                        max="10000"
                        class="form-input"
                        required
                      />
                      <span>次</span>
                    </span>
                  </label>
                  <label class="form-field">
                    冻结时长
                    <span class="unit-control">
                      <input
                        v-model.number="draft.freezeMinutes"
                        name="security_freeze_minutes"
                        type="number"
                        min="1"
                        max="10080"
                        step="any"
                        class="form-input"
                        required
                      />
                      <span>分钟</span>
                    </span>
                  </label>
                </fieldset>
                <p>WAF、HTTP 规则与限速分别累计。仅记录命中和请求体超限不会自动冻结，与独立账号验证的失败计数分开。</p>
              </div>
            </section>
          </div>
        </div>
      </fieldset>
      <footer class="firewall-footer">
        <span class="footer-hint">保存后立即生效</span>
        <div class="footer-actions">
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="isSavingConfig"
            @click="dialog.close()"
          >取消</button>
          <button type="submit" class="btn btn-primary" :disabled="isSavingConfig">
            <Save :size="14" />
            {{ isSavingConfig ? '保存中…' : '保存策略' }}
          </button>
        </div>
      </footer>
    </form>
  </dialog>
</template>

<script setup>
import { newID } from '@/api/ids'
import { nextTick, onMounted, ref, toRaw } from 'vue'
import { Code, Gauge, Layers, Plus, Save, Shield, X } from 'lucide-vue-next'
import { firewallDraft, firewallRecord, replaceConfigItem } from '@/api/configuration'
import { useGatewayStatus } from '@/composables/useGatewayStatus'

const props = defineProps({ snapshot: { type: Object, required: true }, firewall: Object })
const emit = defineEmits(['close', 'saved'])
const { saveConfig, isSavingConfig } = useGatewayStatus()
const dialog = ref(null)
const draft = ref(
  firewallDraft(props.firewall || { id: newID(), name: '', default_action: 'deny', groups: [] })
)
const sections = [
  { id: 'ip', label: '来源 IP', icon: Layers },
  { id: 'waf', label: 'Web 防护', icon: Shield },
  { id: 'http', label: 'HTTP 规则', icon: Code },
  { id: 'rate', label: '限速与冻结', icon: Gauge }
]
const activeSection = ref('ip')
const content = ref(null)
function selectSection(id) {
  activeSection.value = id
  content.value?.scrollTo({ top: 0 })
}
const error = ref('')
const errorElement = ref(null)

function addGroup() {
  draft.value.groups.push({ name: '', match: 'include', action: 'allow', cidrsText: '', subscriptions: [] })
}
function addException() {
  draft.value.protection.waf.exceptions.push({ rule_id: '', host: '', path: '/', parameter: '' })
}
function move(items, index, delta) {
  items.splice(index + delta, 0, items.splice(index, 1)[0])
}
function blockBusyClose(event) {
  if (isSavingConfig.value) event.preventDefault()
}
async function save() {
  if (isSavingConfig.value) return
  error.value = ''
  const invalid = dialog.value.querySelector('input:invalid, select:invalid, textarea:invalid')
  if (invalid) {
    const section = invalid.closest('[data-section]')?.dataset.section
    if (section) selectSection(section)
    await nextTick()
    invalid.focus()
    invalid.reportValidity()
    return
  }
  try {
    const record = firewallRecord(toRaw(draft.value))
    await saveConfig(
      props.snapshot,
      replaceConfigItem(props.snapshot, 'firewalls', record, props.firewall?.id ?? null)
    )
    emit('saved', record.id)
  } catch (err) {
    error.value = err.message
    await nextTick()
    errorElement.value?.focus()
  }
}
onMounted(() => dialog.value.showModal())
</script>

<style scoped>
.firewall-dialog {
  --editor-accent: var(--brand-light);
  width: min(920px, calc(100vw - 48px)) !important;
  height: min(700px, calc(100dvh - 64px));
  max-height: calc(100dvh - 64px) !important;
  background: var(--modal-bg) !important;
  border: 1px solid var(--line) !important;
  border-radius: 16px !important;
  box-shadow: var(--shadow-card), 0 24px 72px rgb(0 0 0 / 0.18) !important;
}
.theme-light .firewall-dialog {
  --editor-accent: #4f46e5;
}
.theme-light .firewall-dialog::backdrop {
  background: rgb(15 23 42 / 0.22) !important;
}
.firewall-dialog::backdrop {
  background: rgb(4 7 13 / 0.5) !important;
  backdrop-filter: blur(6px) !important;
}
.firewall-header {
  padding: 18px 22px !important;
  gap: 12px;
}
.heading-group {
  display: flex;
  align-items: center;
  gap: 14px;
  min-width: 0;
}
.heading-icon {
  color: var(--editor-accent);
  flex-shrink: 0;
}
.firewall-header h2 {
  margin: 0;
  font-size: 18px;
  line-height: 1.4;
}
.firewall-header p {
  margin-top: 3px;
}
.firewall-header .firewall-close {
  width: 36px !important;
  height: 36px !important;
  flex-shrink: 0;
}
.firewall-dialog .firewall-form {
  padding: 0 !important;
  overflow: hidden !important;
  gap: 0 !important;
}
.editor-fields {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  min-width: 0;
  gap: 0;
  border: 0;
  padding: 0;
  margin: 0;
}
.policy-basics {
  padding: 18px 22px;
  border-bottom: 1px solid var(--line);
  flex-shrink: 0;
}
.form-row-2 {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.editor-layout {
  display: flex;
  flex: 1;
  min-height: 0;
}
.section-nav {
  width: 176px;
  flex-shrink: 0;
  padding: 12px;
  background: var(--strip-bg);
  border-right: 1px solid var(--line);
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.section-tab {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  min-height: 44px;
  padding: 10px;
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--muted);
  font: inherit;
  font-size: 13px;
  text-align: left;
  cursor: pointer;
  white-space: nowrap;
}
.section-tab svg {
  flex-shrink: 0;
}
.section-tab:hover {
  background: var(--card-bg);
  color: var(--ink);
}
.section-tab[aria-pressed='true'] {
  background: color-mix(in srgb, var(--brand) 12%, transparent);
  color: var(--editor-accent);
  font-weight: 600;
}
.editor-content {
  flex: 1;
  min-width: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: 20px 24px;
  scrollbar-width: thin;
  scrollbar-color: var(--line) transparent;
}
.editor-section {
  display: grid;
  gap: 14px;
  min-width: 0;
}
.section-heading {
  display: grid;
  gap: 5px;
  padding-bottom: 16px;
  border-bottom: 1px solid var(--line);
}
h3, h4 {
  margin: 0;
  font-size: 15px;
  line-height: 1.5;
  color: var(--ink);
}
h3 {
  font-size: 17px;
}
p {
  margin: 0;
  color: var(--muted);
  font-size: 12px;
  line-height: 1.6;
  overflow-wrap: anywhere;
}
.protection-block {
  display: grid;
  gap: 10px;
}
.protection-block + .protection-block {
  border-top: 1px solid var(--line);
  padding-top: 14px;
  margin-top: 4px;
}
.protection-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.firewall-switch {
  width: 44px;
  height: 44px;
  margin: -4px 0;
}
.firewall-switch input {
  width: 100%;
  height: 100%;
  left: 0;
  top: 0;
  cursor: pointer;
}
.firewall-switch .switch-slider {
  top: 11px;
  bottom: 11px;
  left: 3px;
  right: 3px;
  pointer-events: none;
}
.firewall-switch input:focus-visible + .switch-slider {
  outline: 2px solid var(--editor-accent);
  outline-offset: 3px;
}
.control-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  border: 0;
  padding: 0;
  margin: 0;
  min-width: 0;
}
.control-grid:disabled .form-field {
  color: var(--muted);
}
.control-grid:disabled .form-input {
  opacity: 0.65;
  cursor: not-allowed;
}
.unit-control {
  display: block;
  position: relative;
  min-width: 0;
}
.unit-control .form-input {
  padding-right: 42px !important;
  font-family: var(--mono);
}
.unit-control > span {
  position: absolute;
  right: 12px;
  top: 50%;
  transform: translateY(-50%);
  color: var(--muted);
  font-size: 12px;
  pointer-events: none;
}
.unit-control input {
  appearance: textfield;
}
.unit-control input::-webkit-inner-spin-button, .unit-control input::-webkit-outer-spin-button {
  appearance: none;
  margin: 0;
}
.rule-fields {
  display: grid;
  gap: 12px;
  min-width: 0;
  padding: 14px;
  background: var(--strip-bg);
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
}
.rule-actions {
  display: flex;
  justify-content: flex-end;
  gap: 6px;
  flex-wrap: wrap;
}
legend {
  padding: 0 4px;
  color: var(--editor-accent);
  font-size: 12px;
  font-weight: 600;
}
.subscription-choices {
  display: grid;
  gap: 8px;
  font-size: 12px;
  color: var(--ink-secondary);
}
.subscription-choices .check-label {
  flex-direction: row;
  min-height: 32px;
}
.section-add {
  justify-self: start;
}
.firewall-footer {
  justify-content: space-between !important;
  padding: 14px 22px !important;
  gap: 12px !important;
}
.footer-hint {
  color: var(--muted);
  font-size: 12px;
}
.footer-actions {
  display: flex;
  gap: 10px;
}
.editor-error {
  padding: 10px 12px;
  margin-bottom: 16px;
  border: 1px solid color-mix(in srgb, var(--bad) 30%, transparent);
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--bad) 6%, transparent);
  color: var(--bad);
}
.text-bad {
  color: var(--bad);
}
input, textarea, select {
  min-width: 0;
}
@media (max-width: 700px) {
  .firewall-dialog {
    inset: auto 0 0;
    width: 100% !important;
    max-width: none;
    height: min(90dvh, 820px);
    max-height: calc(100dvh - 12px) !important;
    margin: 0 !important;
    border-radius: 18px 18px 0 0 !important;
    border-bottom: 0 !important;
  }
  .firewall-header {
    padding: 14px 16px !important;
  }
  .heading-group {
    gap: 10px;
  }
  .heading-icon {
    width: 24px;
    height: 24px;
  }
  .firewall-header h2 {
    font-size: 16px;
  }
  .firewall-header .firewall-close {
    width: 44px !important;
    height: 44px !important;
  }
  .policy-basics {
    padding: 14px 16px;
    grid-template-columns: repeat(2, minmax(0, 1fr)) !important;
  }
  .editor-layout {
    flex-direction: column;
  }
  .section-nav {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    width: auto;
    padding: 8px 10px;
    border-right: 0;
    border-bottom: 1px solid var(--line);
  }
  .section-tab {
    justify-content: center;
    gap: 0;
    padding: 10px 2px;
    font-size: 12px;
    text-align: center;
  }
  .section-tab svg {
    display: none;
  }
  .editor-content {
    padding: 16px;
  }
  .section-heading p {
    display: none;
  }
  .control-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .control-grid > :last-child {
    grid-column: 1 / -1;
  }
  .rule-actions .btn, .section-add, .subscription-choices .check-label {
    min-height: 44px;
  }
  .firewall-footer {
    padding: 12px 16px max(12px, env(safe-area-inset-bottom, 0px)) !important;
  }
  .footer-actions {
    flex: 1;
  }
  .footer-actions .btn {
    min-height: 44px;
    flex: 1;
    justify-content: center;
  }
  .footer-hint {
    display: none;
  }
}
@media (max-width: 380px) {
  .rule-fields .form-row-2 {
    grid-template-columns: 1fr;
  }
}
@media (prefers-reduced-motion: reduce) {
  .firewall-dialog * {
    transition: none !important;
  }
}
</style>
