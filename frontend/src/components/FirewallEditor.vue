<template>
  <dialog
    ref="dialog"
    class="firewall-dialog"
    aria-labelledby="firewall-title"
    @close="emit('close')"
    @cancel="blockBusyClose"
  >
    <div class="modal-header">
      <h2 id="firewall-title">{{ firewall ? '编辑' : '添加' }}防火墙策略</h2>
      <button
        type="button"
        class="btn btn-ghost"
        aria-label="关闭"
        :disabled="isSavingConfig"
        @click="dialog.close()"
      >
        <X :size="18" />
      </button>
    </div>
    <form @submit.prevent="save">
      <p v-if="error" ref="errorElement" class="error" role="alert" tabindex="-1">{{ error }}</p>
      <fieldset class="editor-fields" :disabled="isSavingConfig">
        <div class="form-row-2">
          <label
            >策略名称<input
              v-model="draft.name"
              name="name"
              class="form-input"
              maxlength="100"
              required
          /></label>
          <label
            >IP 未命中时<select v-model="draft.default_action" name="default_action" class="form-select">
              <option value="deny">禁止访问</option>
              <option value="allow">允许访问</option>
            </select></label
          >
        </div>
        <section class="editor-section">
          <h3>来源 IP 规则组</h3>
          <p>从上到下匹配，第一条命中决定允许或禁止。手动 IP 与勾选订阅合并；“排除”匹配组外 IP。</p>
          <fieldset v-for="(group, index) in draft.groups" :key="index" class="rule-fields ip-group">
            <legend>IP 组 #{{ index + 1 }}</legend>
            <div class="rule-actions">
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="index === 0"
                @click="move(draft.groups, index, -1)"
              >
                上移
              </button>
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="index === draft.groups.length - 1"
                @click="move(draft.groups, index, 1)"
              >
                下移
              </button>
              <button
                type="button"
                class="btn btn-ghost btn-sm text-bad"
                @click="draft.groups.splice(index, 1)"
              >
                移除组
              </button>
            </div>
            <label
              >组名称<input
                v-model="group.name"
                name="group_name"
                class="form-input"
                maxlength="100"
                required
            /></label>
            <div class="form-row-2">
              <label
                >匹配方式<select v-model="group.match" name="match" class="form-select">
                  <option value="include">包含：匹配组内 IP</option>
                  <option value="exclude">排除：匹配组外 IP</option>
                </select></label
              >
              <label
                >命中动作<select v-model="group.action" name="action" class="form-select">
                  <option value="allow">允许访问</option>
                  <option value="deny">禁止访问</option>
                </select></label
              >
            </div>
            <label
              >手动 IPv4 / IPv6 / CIDR<textarea
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
                {{ subscription.name }}{{ !subscription.enabled ? '（已停用）' : '' }}
              </label>
              <p v-if="!snapshot.config.subscriptions?.length">暂无订阅，可先填写手动网段。</p>
            </div>
          </fieldset>
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="draft.groups.length >= 100"
            @click="addGroup"
          >
            <Plus :size="13" /> 添加 IP 组
          </button>
          <p>任一引用的订阅没有有效缓存时，整个策略暂拒访问。仅使用 Web 防护时，将 IP 未命中动作设为允许。</p>
        </section>
        <section class="editor-section">
          <h3>Web 攻击防护 · Coraza / OWASP CRS</h3>
          <div class="form-row-2">
            <label
              >WAF 模式<select v-model="draft.protection.waf.mode" name="waf_mode" class="form-select">
                <option value="off">关闭</option>
                <option value="detect">仅记录</option>
                <option value="block">拦截攻击</option>
              </select></label
            >
            <label
              >检测等级<select
                v-model.number="draft.protection.waf.level"
                name="waf_level"
                class="form-select"
              >
                <option v-for="level in 4" :key="level" :value="level">
                  PL{{ level }}{{ level === 1 ? ' · 基础（推荐）' : '' }}
                </option>
              </select></label
            >
          </div>
          <label
            >请求体检查上限 KiB<input
              v-model.number="draft.bodyKiB"
              name="waf_body_kib"
              class="form-input"
              type="number"
              min="64"
              max="32768"
              step="any"
              required
          /></label>
          <p>
            建议先仅记录，再按误报调整例外。拦截模式超限返回 413；仅记录模式超限记录未完成检查并转发。响应及
            WebSocket 消息不检查。
          </p>
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
            >
              移除例外
            </button>
            <div class="form-row-2">
              <label
                >CRS 规则编号<input
                  v-model.number="exception.rule_id"
                  name="exception_id"
                  type="number"
                  min="911000"
                  max="948999"
                  class="form-input"
                  required
              /></label>
              <label
                >服务域名（可选）<input
                  v-model="exception.host"
                  name="exception_host"
                  class="form-input"
                  placeholder="留空适用于本策略所有域名"
              /></label>
              <label
                >路径前缀<input
                  v-model="exception.path"
                  name="exception_path"
                  class="form-input mono-text"
                  maxlength="256"
                  required
              /></label>
              <label
                >参数名（可选）<input
                  v-model="exception.parameter"
                  name="exception_parameter"
                  class="form-input"
                  maxlength="100"
              /></label>
            </div>
          </fieldset>
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="draft.protection.waf.exceptions.length >= 50"
            @click="addException"
          >
            添加规则例外
          </button>
          <p>
            仅豁免指定 CRS
            规则；路径按前缀匹配，参数留空豁免该请求的整条规则。路径只支持字母、数字、/、_、.、~、-。
          </p>
        </section>
        <section class="editor-section">
          <h3>爬虫与自定义 HTTP 规则</h3>
          <p>
            按顺序匹配正则表达式，支持 (?i) 忽略大小写。拦截规则立即拒绝；仅记录规则继续检查。UA
            规则建议结合限速。
          </p>
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
              >
                上移
              </button>
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="index === draft.protection.rules.length - 1"
                @click="move(draft.protection.rules, index, 1)"
              >
                下移
              </button>
              <button
                type="button"
                class="btn btn-ghost btn-sm text-bad"
                @click="draft.protection.rules.splice(index, 1)"
              >
                移除规则
              </button>
            </div>
            <label
              >规则名称<input
                v-model="rule.name"
                name="http_name"
                class="form-input"
                maxlength="100"
                required
            /></label>
            <div class="form-row-2">
              <label
                >检查目标<select v-model="rule.target" name="http_target" class="form-select">
                  <option value="user_agent">User-Agent</option>
                  <option value="path">请求路径</option>
                  <option value="method">请求方法</option>
                </select></label
              >
              <label
                >动作<select v-model="rule.action" name="http_action" class="form-select">
                  <option value="block">拦截</option>
                  <option value="detect">仅记录</option>
                </select></label
              >
            </div>
            <label
              >正则表达式<input
                v-model="rule.pattern"
                name="http_pattern"
                class="form-input mono-text"
                maxlength="256"
                required
            /></label>
          </fieldset>
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="draft.protection.rules.length >= 50"
            @click="
              draft.protection.rules.push({ name: '', target: 'user_agent', pattern: '', action: 'block' })
            "
          >
            添加 HTTP 规则
          </button>
        </section>
        <section class="editor-section">
          <h3>访问频率限制</h3>
          <label class="check-label"
            ><input
              v-model="draft.protection.rate.enabled"
              name="rate_enabled"
              type="checkbox"
            />启用每个服务、每个来源 IP 的限速</label
          >
          <div class="form-row-2">
            <label
              >时间窗口 秒<input
                v-model.number="draft.protection.rate.window_seconds"
                name="rate_window"
                type="number"
                min="1"
                max="3600"
                class="form-input"
                required
            /></label>
            <label
              >窗口内允许次数<input
                v-model.number="draft.protection.rate.requests"
                name="rate_requests"
                type="number"
                min="1"
                max="100000"
                class="form-input"
                required
            /></label>
          </div>
          <label
            >路径前缀<input
              v-model="draft.protection.rate.path"
              name="rate_path"
              class="form-input"
              maxlength="256"
              required
          /></label>
          <p>超限返回 429 和重试时间。按实际连接 IP 计算；前置代理会影响服务器看到的来源地址。</p>
        </section>
        <section class="editor-section">
          <h3>防护触发自动冻结</h3>
          <label class="check-label"
            ><input
              v-model="draft.protection.freeze.enabled"
              name="security_freeze_enabled"
              type="checkbox"
            />多次被防护拦截后冻结来源 IP</label
          >
          <div class="form-row-2">
            <label
              >累计窗口 秒<input
                v-model.number="draft.protection.freeze.window_seconds"
                name="security_freeze_window"
                type="number"
                min="1"
                max="86400"
                class="form-input"
                required
            /></label>
            <label
              >触发次数<input
                v-model.number="draft.protection.freeze.failures"
                name="security_freeze_failures"
                type="number"
                min="2"
                max="10000"
                class="form-input"
                required
            /></label>
            <label
              >冻结时长 分钟<input
                v-model.number="draft.freezeMinutes"
                name="security_freeze_minutes"
                type="number"
                min="1"
                max="10080"
                step="any"
                class="form-input"
                required
            /></label>
          </div>
          <p>
            WAF、HTTP 规则、限速分别累计。仅记录命中和请求体超限不会自动冻结；与独立账号验证的失败计数分开。
          </p>
        </section>
        <div class="modal-footer">
          <button type="button" class="btn btn-secondary" @click="dialog.close()">取消</button>
          <button type="submit" class="btn btn-primary">
            {{ isSavingConfig ? '保存中…' : '保存防火墙配置' }}
          </button>
        </div>
      </fieldset>
    </form>
  </dialog>
</template>

<script setup>
import { nextTick, onMounted, ref, toRaw } from 'vue'
import { Plus, X } from 'lucide-vue-next'
import { firewallDraft, firewallRecord, replaceConfigItem } from '@/api/configuration'
import { useGatewayStatus } from '@/composables/useGatewayStatus'

const props = defineProps({ snapshot: { type: Object, required: true }, firewall: Object })
const emit = defineEmits(['close', 'saved'])
const { saveConfig, isSavingConfig } = useGatewayStatus()
const dialog = ref(null)
const draft = ref(
  firewallDraft(props.firewall || { id: crypto.randomUUID(), name: '', default_action: 'deny', groups: [] })
)
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
.modal-header,
.modal-footer,
.rule-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}
h2 {
  font-size: 18px;
}
h3 {
  font-size: 15px;
}
form,
.editor-fields,
.editor-section,
.rule-fields,
.subscription-choices {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.editor-fields {
  border: 0;
  margin: 0;
  padding: 0;
}
.editor-section {
  padding: 16px 0;
  border-top: 1px solid var(--line);
}
.form-row-2 {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
label,
.subscription-choices {
  display: flex;
  flex-direction: column;
  gap: 6px;
  color: var(--ink-secondary);
  font-size: 12.5px;
  font-weight: 600;
}
p {
  color: var(--muted);
  font-size: 12px;
  line-height: 1.6;
  margin: 0;
  overflow-wrap: anywhere;
}
.rule-fields {
  padding: 16px;
  background: var(--strip-bg);
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
}
legend {
  font-size: 12px;
  font-weight: 700;
  color: var(--brand-light);
  padding: 0 4px;
}
.modal-footer {
  justify-content: flex-end;
  padding-top: 16px;
}
.error,
.text-bad {
  color: var(--bad);
}
input,
textarea,
select {
  min-width: 0;
}
@media (max-width: 600px) {
  .form-row-2 {
    grid-template-columns: 1fr;
  }
}
</style>
