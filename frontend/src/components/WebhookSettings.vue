<template>
  <section aria-label="故障推送设置" class="push-editor">
    <div class="push-header">
      <div>
        <h3>{{ wx ? 'WxPusher 故障推送' : 'Webhook 故障推送' }}</h3>
        <p class="form-note">{{ wx ? '微信实时告警卡片推送' : '通用 JSON POST 回调（支持企业微信 / 飞书 / 自建脚本）' }}</p>
      </div>
    </div>

    <form v-if="saved" class="push-form" @submit.prevent="save">
      <label class="toggle-row">
        <span>启用推送</span><input v-model="enabled" type="checkbox" :disabled="busy" />
      </label>

      <div v-if="saved.last_error" class="push-error-banner" role="status">
        最近投递异常：{{ saved.last_error }}，系统将在约一分钟后自动重试。
      </div>
      <div v-if="saved.dropped" class="push-error-banner" role="status">
        队列已满时丢弃了 {{ saved.dropped }} 条较早的通知。
      </div>

      <div v-if="wx" class="push-target">
        <SecretInput
          :key="saved.revision"
          id="alert-wxpusher-token"
          v-model.trim="secret"
          name="app_token"
          label="AppToken"
          kind="alert-wxpusher"
          secret-name="AppToken"
          :configured="saved.token_configured"
          :placeholder="saved.token_configured ? '已保存，可直接输入新值' : 'AT_…'"
          :maxlength="256"
          :disabled="busy"
        />
        <div class="uid-field">
          <label for="wxpusher-uid">接收用户 UID</label>
          <input
            id="wxpusher-uid"
            v-model.trim="uid"
            class="form-input mono"
            placeholder="UID_…"
            maxlength="256"
            :disabled="busy"
            :required="enabled"
            autocomplete="off"
            spellcheck="false"
          />
        </div>
      </div>

      <SecretInput
        v-else
        :key="saved.revision"
        id="alert-webhook-url"
        v-model.trim="secret"
        name="webhook_url"
        label="Webhook 地址"
        kind="alert-webhook"
        secret-name="Webhook 地址"
        :configured="saved.configured"
        :placeholder="saved.configured ? '已保存，可直接输入新地址' : 'https://…'"
        :maxlength="2048"
        :disabled="busy"
      />

      <fieldset class="events">
        <legend>通知事件</legend>
        <div
          v-for="option in eventOptions"
          :key="option.id"
          class="event-option"
          :class="{ checked: events.includes(option.id), disabled: busy }"
          @click="onCardClick(option.id, $event)"
        >
          <input
            :id="'alert-event-' + channel + '-' + option.id"
            v-model="events"
            type="checkbox"
            :value="option.id"
            :disabled="busy"
          />
          <div class="event-copy">
            <label :for="'alert-event-' + channel + '-' + option.id">{{ option.label }}</label>
            <span>{{ option.desc }}</span>
          </div>
        </div>
      </fieldset>

      <p class="form-note">同一故障首次触发时推送一次，恢复后再通知。</p>
      <p v-if="saved.pending" class="form-note">
        待发送 {{ saved.pending }} 条。{{ wx ? '停用推送或更换接收配置会清空待发通知。' : '关闭推送或更换地址会清空待发通知。' }}
      </p>

      <details v-if="!wx" class="payload-details">
        <summary>Webhook 请求体参考（支持 Idempotency-Key 去重）</summary>
        <pre class="payload-code">{{ webhookSample }}</pre>
      </details>

      <div class="actions">
        <button type="submit" class="btn btn-primary" :disabled="busy || !changed">保存</button>
        <button type="button" class="btn btn-secondary" :disabled="busy || changed || !canTest" @click="test">发送测试通知</button>
      </div>
    </form>

    <p v-else-if="busy" role="status">正在读取推送设置…</p>
    <p v-if="error" class="report-error" role="alert">{{ error }}</p>
    <button v-if="retryLoad" type="button" class="btn btn-secondary" :disabled="busy" @click="load">重新读取设置</button>
    <p v-if="notice" class="push-notice" role="status">{{ notice }}</p>
  </section>
</template>

<script setup>
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import SecretInput from './SecretInput.vue'
import { adminApi } from '@/api/admin'

const props = defineProps({ channel: { type: String, default: 'webhook' } })
const emit = defineEmits(['loaded'])
const wx = props.channel === 'wxpusher'

const eventOptions = [
  { id: 'upstream', label: '反代上游连续失败', desc: '连续 2 次健康探测超时或失败时推送一次，恢复后再通知' },
  { id: 'backup', label: 'WebDAV 备份失败', desc: '定时或手动上传加密备份至远程目录失败时立即告警' },
  { id: 'certificate', label: '证书即将到期', desc: '正式签发证书剩余有效期不足 7 天时推送续期提醒' }
]

const webhookSample = `{
  "id": "evt_01J...",
  "type": "upstream",
  "status": "firing",
  "resource": "jellyfin.home.arpa",
  "message": "反代上游连续检测超时",
  "time": "2026-11-21T11:14:12Z"
}`

const saved = ref(null)
const enabled = ref(false)
const secret = ref()
const uid = ref('')
const events = ref([])
const busy = ref(false)
const retryLoad = ref(false)
const notice = ref('')
const error = ref('')
const controller = new AbortController()

const changed = computed(() => saved.value && (
  saved.value.enabled !== enabled.value ||
  secret.value !== undefined ||
  (wx && uid.value !== saved.value.uid) ||
  (events.value.length !== saved.value.events.length || events.value.some(event => !saved.value.events.includes(event)))
))
const canTest = computed(() => wx ? saved.value?.token_configured && saved.value?.uid : saved.value?.configured)

function onCardClick(id, event) {
  if (busy.value || event.target.closest('input, label')) return
  const index = events.value.indexOf(id)
  if (index >= 0) events.value = events.value.filter(item => item !== id)
  else events.value = [...events.value, id]
}

function populate(data) {
  saved.value = data
  emit('loaded', {
    enabled: data.enabled,
    pending: data.pending || 0,
    last_error: data.last_error || '',
    events: data.events ?? []
  })
  enabled.value = data.enabled
  secret.value = undefined
  uid.value = data.uid ?? ''
  events.value = data.events ?? []
  retryLoad.value = false
}

async function run(action) {
  if (busy.value) return
  busy.value = true
  error.value = notice.value = ''
  try { await action() }
  catch (err) {
    if (err.name !== 'AbortError') {
      error.value = err.message
      retryLoad.value = !saved.value || err.status === 409
    }
  } finally { busy.value = false }
}

function load() { return run(async () => populate(await (wx ? adminApi.wxPusher : adminApi.webhook)(controller.signal))) }
function save() {
  return run(async () => {
    const shared = { enabled: enabled.value, revision: saved.value.revision, events: events.value }
    const result = wx
      ? await adminApi.saveWxPusher({ ...shared, app_token: secret.value, uid: uid.value }, controller.signal)
      : await adminApi.saveWebhook({ ...shared, url: secret.value }, controller.signal)
    populate(result)
    notice.value = '推送设置已保存'
  })
}
function test() {
  return run(async () => {
    await (wx ? adminApi.testWxPusher : adminApi.testWebhook)(controller.signal)
    notice.value = wx ? 'WxPusher 已受理测试通知' : '接收端已接受测试通知'
  })
}

onMounted(load)
onBeforeUnmount(() => controller.abort())
</script>

<style scoped>
.push-editor { display: grid; grid-template-columns: minmax(0, 1fr); gap: 14px; min-width: 0; }
.push-header { padding-bottom: 12px; border-bottom: 1px solid var(--line); min-width: 0; }
.push-header h3 { font-size: 15.5px; font-weight: 700; color: var(--ink); margin: 0 0 4px; }
.push-form { display: grid; grid-template-columns: minmax(0, 1fr); gap: 14px; min-width: 0; }
.push-target { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); align-items: start; gap: 14px; min-width: 0; }
.uid-field { display: flex; flex-direction: column; gap: 5px; min-width: 0; }
.uid-field label, .events legend { font-size: 12px; font-weight: 600; color: var(--muted); }
.uid-field .form-input { height: 44px !important; min-height: 44px !important; }
.mono { font-family: var(--mono); }

.push-error-banner {
  padding: 10px 12px; border-radius: var(--radius-sm); font-size: 12.5px;
  background: rgba(244, 63, 94, 0.1); border: 1px solid rgba(244, 63, 94, 0.3);
  color: var(--bad); overflow-wrap: anywhere;
}

.events { display: grid; grid-template-columns: minmax(0, 1fr); gap: 8px; margin: 0; padding: 0; border: 0; min-width: 0; }
.events legend { margin-bottom: 4px; }
.event-option {
  display: flex; align-items: flex-start; gap: 10px; padding: 10px 12px; min-width: 0;
  border-radius: var(--radius-sm); border: 1px solid var(--line); background: var(--strip-bg);
  cursor: pointer; transition: var(--transition-smooth);
}
.event-option:hover:not(.disabled) { border-color: var(--card-border-hover); background: var(--card-hover-bg); }
.event-option.checked { border-color: rgba(99, 102, 241, 0.38); background: rgba(99, 102, 241, 0.06); }
.event-option.disabled { opacity: 0.6; cursor: not-allowed; }
.event-option input { margin-top: 2px !important; }
.event-copy { display: grid; gap: 2px; min-width: 0; flex: 1; }
.event-copy label { font-size: 13px; font-weight: 600; color: var(--ink); cursor: pointer; overflow-wrap: anywhere; }
.event-copy span { font-size: 12px; color: var(--muted); line-height: 1.45; overflow-wrap: anywhere; }

.payload-details { min-width: 0; max-width: 100%; }
.payload-details summary { gap: 8px; overflow-wrap: anywhere; }
.payload-code {
  margin: 0; font-family: var(--mono); font-size: 11.5px; line-height: 1.5;
  color: var(--ink-secondary); overflow-x: auto; white-space: pre-wrap; overflow-wrap: anywhere;
}
.actions { display: flex; flex-wrap: wrap; gap: 8px; padding-top: 8px; border-top: 1px solid var(--line); }
.push-notice { font-size: 13px; color: var(--ok); font-weight: 500; }
p { font-size: 13px; overflow-wrap: anywhere; }

@media (max-width: 650px) {
  .push-target { grid-template-columns: minmax(0, 1fr); }
}
</style>
