<template>
  <section aria-label="故障推送设置">
    <h3>{{ wx ? 'WxPusher 故障推送' : 'Webhook 故障推送' }}</h3>
    <form v-if="saved" @submit.prevent="save">
      <label class="toggle-row">
        <span>启用推送</span><input v-model="enabled" type="checkbox" :disabled="busy" />
      </label>
      <div v-if="wx" class="push-target">
        <SecretInput :key="saved.revision" id="alert-wxpusher-token" v-model.trim="secret" name="app_token"
          label="AppToken" kind="alert-wxpusher" secret-name="AppToken" :configured="saved.token_configured"
          :placeholder="saved.token_configured ? '已保存，可直接输入新值' : 'AT_…'"
          :maxlength="256" :disabled="busy" />
        <div class="uid-field">
          <label for="wxpusher-uid">接收用户 UID</label>
          <input id="wxpusher-uid" v-model.trim="uid" class="form-input" placeholder="UID_…" maxlength="256"
            :disabled="busy" :required="enabled" autocomplete="off" spellcheck="false" />
        </div>
      </div>
      <SecretInput v-else :key="saved.revision" id="alert-webhook-url" v-model.trim="secret" name="webhook_url"
        label="Webhook 地址" kind="alert-webhook" secret-name="Webhook 地址" :configured="saved.configured"
        :placeholder="saved.configured ? '已保存，可直接输入新地址' : 'https://…'" :maxlength="2048" :disabled="busy" />
      <fieldset class="events">
        <legend>通知事件</legend>
        <label class="toggle-row" v-for="option in eventOptions" :key="option.id">
          <span>{{ option.label }}</span><input v-model="events" type="checkbox" :value="option.id" :disabled="busy" />
        </label>
      </fieldset>
      <p class="form-note">同一故障首次触发时推送一次，恢复后再通知。</p>
      <p v-if="saved.pending" class="form-note">待发送 {{ saved.pending }} 条。{{ wx ? '停用推送或更换接收配置会清空待发通知。' : '关闭推送或更换地址会清空待发通知。' }}</p>
      <p v-if="saved.last_error" class="report-error">{{ saved.last_error }}，稍后自动重试。</p>
      <p v-if="saved.dropped" class="report-error">队列已满时丢弃了 {{ saved.dropped }} 条较早的通知。</p>
      <div class="actions">
        <button type="submit" class="btn btn-primary" :disabled="busy || !changed">保存</button>
        <button type="button" class="btn btn-secondary" :disabled="busy || changed || !canTest" @click="test">发送测试通知</button>
      </div>
    </form>
    <p v-else-if="busy" role="status">正在读取推送设置…</p>
    <p v-if="error" class="report-error" role="alert">{{ error }}</p>
    <button v-if="retryLoad" type="button" class="btn btn-secondary" :disabled="busy" @click="load">重新读取设置</button>
    <p v-if="notice" role="status">{{ notice }}</p>
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
  { id: 'upstream', label: '反代上游连续失败' },
  { id: 'backup', label: 'WebDAV 备份失败' },
  { id: 'certificate', label: '证书即将到期' }
]
const saved = ref(null), enabled = ref(false), secret = ref(), uid = ref(''), events = ref([])
const busy = ref(false), retryLoad = ref(false), notice = ref(''), error = ref('')
const controller = new AbortController()
const changed = computed(() => saved.value && (
  saved.value.enabled !== enabled.value || secret.value !== undefined || (wx && uid.value !== saved.value.uid) || (
    events.value.length !== saved.value.events.length ||
    events.value.some(event => !saved.value.events.includes(event))
  )
))
const canTest = computed(() => wx ? saved.value?.token_configured && saved.value?.uid : saved.value?.configured)

function populate(data) {
  saved.value = data
  emit('loaded', data.enabled)
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
form { display: grid; gap: 14px; margin-block: 14px; }
.push-target { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); align-items: start; gap: 14px; }
@media (max-width: 650px) { .push-target { grid-template-columns: 1fr; } }
.actions { display: flex; flex-wrap: wrap; gap: 8px; }
.uid-field { display: grid; gap: 5px; }
.uid-field label, .events legend { font-size: 12px; font-weight: 600; color: var(--muted); }
.events { display: grid; gap: 5px; margin: 0; padding: 0; border: 0; }
.events legend { margin-bottom: 5px; }
.events label { display: flex; align-items: center; gap: 9px; min-height: 32px; font-size: 13px; }
h3 { font-size: 15px; color: var(--ink); }
p { font-size: 13px; overflow-wrap: anywhere; }
</style>
