<template>
  <section aria-label="故障推送设置" class="push-editor">
    <div class="push-header">
      <div>
        <h3>{{ wx ? 'WxPusher 故障推送' : 'Webhook 故障推送' }}</h3>
        <p class="form-note">{{ wx ? '微信实时告警卡片推送' : currentFormat.desc }}</p>
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

      <template v-else>
        <div class="uid-field">
          <label for="alert-webhook-format">推送渠道模板</label>
          <select id="alert-webhook-format" v-model="format" name="webhook_format" class="form-select" :disabled="busy">
            <option v-for="item in webhookFormats" :key="item.id" :value="item.id">{{ item.label }}</option>
          </select>
        </div>

        <SecretInput
          :key="saved.revision"
          id="alert-webhook-url"
          v-model.trim="secret"
          name="webhook_url"
          label="Webhook 地址"
          kind="alert-webhook"
          secret-name="Webhook 地址"
          :configured="saved.configured"
          :placeholder="saved.configured ? '已保存，可直接输入新地址' : currentFormat.placeholder"
          :maxlength="2048"
          :disabled="busy"
        />

        <div v-if="format === 'telegram'" class="uid-field">
          <label for="alert-webhook-chat-id">Telegram Chat ID</label>
          <input
            id="alert-webhook-chat-id"
            v-model.trim="chatId"
            name="webhook_chat_id"
            class="form-input mono"
            placeholder="例如：-1001234567890 或用户数字 ID"
            maxlength="128"
            :disabled="busy"
            :required="enabled && format === 'telegram'"
            autocomplete="off"
            spellcheck="false"
          />
        </div>

        <SecretInput
          v-if="currentFormat.supportsSecret"
          :key="saved.revision + '-' + format"
          id="alert-webhook-secret"
          v-model.trim="secretToken"
          name="webhook_secret"
          :label="currentFormat.secretLabel"
          kind="alert-webhook-secret"
          :secret-name="currentFormat.secretLabel"
          :configured="saved.secret_configured"
          :placeholder="saved.secret_configured ? '已保存，留空保留；输入新值覆盖' : currentFormat.secretPlaceholder"
          :maxlength="512"
          :disabled="busy"
        />
      </template>

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
            <div class="event-title-row">
              <label :for="'alert-event-' + channel + '-' + option.id">{{ option.label }}</label>
              <router-link
                v-if="option.id === 'upstream'"
                to="/routes?health=:settings"
                class="event-config-link"
              >
                设置通知延迟 →
              </router-link>
            </div>
            <span>{{ option.desc }}</span>
          </div>
        </div>
      </fieldset>

      <p class="form-note">同一状态类故障首次触发时推送一次，恢复后再通知；安全冻结/限流告警带 5 分钟合并去重。</p>
      <p v-if="saved.pending" class="form-note">
        待发送 {{ saved.pending }} 条。{{ wx ? '停用推送或更换接收配置会清空待发通知。' : '关闭推送或更换地址/模板会清空待发通知。' }}
      </p>

      <details v-if="!wx && format === 'generic'" class="payload-details">
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
import { useGatewayStatus } from '@/composables/useGatewayStatus'

const props = defineProps({ channel: { type: String, default: 'webhook' } })
const emit = defineEmits(['loaded'])
const wx = props.channel === 'wxpusher'
const { gatewayConfig, refreshConfig } = useGatewayStatus()

function formatAlertDelay(seconds) {
  const s = Math.max(0, Math.floor(Number(seconds) || 0))
  if (s >= 3600) {
    const hours = Math.floor(s / 3600)
    const mins = Math.floor((s % 3600) / 60)
    const rem = s % 60
    let text = `${hours} 小时`
    if (mins > 0) text += ` ${mins} 分钟`
    if (rem > 0) text += ` ${rem} 秒`
    return text
  }
  if (s >= 60) {
    const mins = Math.floor(s / 60)
    const rem = s % 60
    return rem === 0 ? `${mins} 分钟` : `${mins} 分 ${rem} 秒`
  }
  return `${s} 秒`
}

const webhookFormats = [
  {
    id: 'generic',
    label: '通用 JSON POST',
    desc: '发送标准 JSON 结构并附带 Idempotency-Key，支持可选 HMAC-SHA256 签名请求头',
    placeholder: 'https://…',
    supportsSecret: true,
    secretLabel: 'HMAC 签名密钥（可选）',
    secretPlaceholder: '留空则不附加 X-GateHome-Signature 请求头'
  },
  {
    id: 'wecom',
    label: '企业微信群机器人',
    desc: '直接推送企业微信 Markdown 卡片消息（填写群机器人 Webhook 地址）',
    placeholder: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=…',
    supportsSecret: false
  },
  {
    id: 'feishu',
    label: '飞书 / Lark 群机器人',
    desc: '推送飞书富文本消息；如机器人开启了签名校验可填写签名密钥',
    placeholder: 'https://open.feishu.cn/open-apis/bot/v2/hook/…',
    supportsSecret: true,
    secretLabel: '签名校验密钥（可选）',
    secretPlaceholder: '填写飞书机器人安全设置中的签名校验密钥'
  },
  {
    id: 'dingtalk',
    label: '钉钉群机器人',
    desc: '推送钉钉 Markdown 消息（消息标题含 GateHome 关键字；支持加签 SEC 密钥）',
    placeholder: 'https://oapi.dingtalk.com/robot/send?access_token=…',
    supportsSecret: true,
    secretLabel: '加签密钥 Secret（可选）',
    secretPlaceholder: 'SEC 开头的钉钉机器人加签密钥'
  },
  {
    id: 'telegram',
    label: 'Telegram Bot',
    desc: '通过 Bot API sendMessage 推送到指定群组或个人会话',
    placeholder: 'https://api.telegram.org/bot<TOKEN>/sendMessage',
    supportsSecret: false
  },
  {
    id: 'bark',
    label: 'Bark (iOS 推送)',
    desc: '推送分组通知到 Bark 服务端或自建 Bark 实例',
    placeholder: 'https://api.day.app/<YOUR_KEY>',
    supportsSecret: false
  },
  {
    id: 'gotify',
    label: 'Gotify',
    desc: '推送分级消息到自建 Gotify Server（URL 填写 /message，令牌填于下方或 URL token 参数）',
    placeholder: 'https://gotify.example.com/message',
    supportsSecret: true,
    secretLabel: 'App Token（可选，X-Gotify-Key）',
    secretPlaceholder: '如 URL 已带 ?token=... 可留空'
  }
]

const eventOptions = computed(() => {
  const delay = Number(gatewayConfig.value?.service_health?.alert_delay_seconds) || 0
  const upstreamDesc = delay >= 30
    ? `反代上游持续不可达满 ${formatAlertDelay(delay)}（${delay} 秒）后推送一次，恢复后再通知`
    : '连续 2 次健康探测超时或失败时推送一次，恢复后再通知'
  return [
    { id: 'upstream', label: '反代上游连续失败', desc: upstreamDesc },
    { id: 'backup', label: 'WebDAV 备份失败', desc: '定时或手动上传加密备份至远程目录失败时立即告警' },
    { id: 'certificate', label: '证书即将到期', desc: '正式签发证书剩余有效期不足 7 天时推送续期提醒' },
    { id: 'ddns', label: 'DDNS 同步失败', desc: '动态域名解析检查或更新失败时推送告警，恢复后通知' },
    { id: 'mihomo', label: 'Mihomo 订阅与核心异常', desc: '代理核心异常退出、订阅自动更新失败，以及订阅到期或剩余流量触发预警阈值时提醒' },
    { id: 'security', label: '安全防护与登录冻结', desc: '反代独立账号连续验证失败触发自动冻结或管理登录触发限流时实时提醒' }
  ]
})

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
const format = ref('generic')
const chatId = ref('')
const secretToken = ref()
const events = ref([])
const busy = ref(false)
const retryLoad = ref(false)
const notice = ref('')
const error = ref('')
const controller = new AbortController()

const currentFormat = computed(() => webhookFormats.find(item => item.id === format.value) || webhookFormats[0])

const changed = computed(() => saved.value && (
  saved.value.enabled !== enabled.value ||
  secret.value !== undefined ||
  (wx ? uid.value !== saved.value.uid : (
    format.value !== (saved.value.format || 'generic') ||
    (format.value === 'telegram' && chatId.value !== (saved.value.chat_id || '')) ||
    secretToken.value !== undefined
  )) ||
  (events.value.length !== saved.value.events.length || events.value.some(event => !saved.value.events.includes(event)))
))
const canTest = computed(() => {
  if (wx) return !!(saved.value?.token_configured && saved.value?.uid)
  if (!saved.value?.configured) return false
  return (saved.value?.format || 'generic') !== 'telegram' || !!saved.value?.chat_id
})

function onCardClick(id, event) {
  if (busy.value || event.target.closest('input, label, a, button')) return
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
  format.value = data.format || 'generic'
  chatId.value = data.chat_id ?? ''
  secretToken.value = undefined
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
    let result
    if (wx) {
      result = await adminApi.saveWxPusher({ ...shared, app_token: secret.value, uid: uid.value }, controller.signal)
    } else {
      const payload = { ...shared, url: secret.value, format: format.value }
      if (format.value === 'telegram') payload.chat_id = chatId.value
      else if (saved.value.chat_id) payload.chat_id = ''
      if (currentFormat.value.supportsSecret && secretToken.value !== undefined) payload.secret = secretToken.value
      else if (!currentFormat.value.supportsSecret && saved.value.secret_configured) payload.secret = ''
      result = await adminApi.saveWebhook(payload, controller.signal)
    }
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

onMounted(() => {
  load()
  if (!gatewayConfig.value) refreshConfig(controller.signal).catch(() => {})
})
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
.uid-field .form-input, .uid-field .form-select { height: 44px !important; min-height: 44px !important; }
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
.event-title-row { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 6px; }
.event-config-link { font-size: 12px; color: var(--brand); text-decoration: none; font-weight: 500; white-space: nowrap; }
.event-config-link:hover { text-decoration: underline; }
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
