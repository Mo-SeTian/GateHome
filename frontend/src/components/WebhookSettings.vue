<template>
  <section aria-label="故障推送设置">
    <h3>Webhook 故障推送</h3>
    <form v-if="saved" @submit.prevent="save">
      <label class="toggle-row">
        <span>启用推送</span><input v-model="enabled" type="checkbox" :disabled="busy" />
      </label>
      <SecretInput :key="saved.revision" id="alert-webhook-url" v-model.trim="url" name="webhook_url"
        label="Webhook 地址" kind="alert-webhook" secret-name="Webhook 地址" :configured="saved.configured"
        :placeholder="saved.configured ? '已保存，可直接输入新地址' : 'https://…'" :maxlength="2048" :disabled="busy" />
      <p class="form-note">反代上游连续两次检测失败、WebDAV 备份失败、正式证书将在 7 天内到期时发送 JSON POST，恢复后再通知。</p>
      <p v-if="saved.pending" class="form-note">待发送 {{ saved.pending }} 条。关闭推送或更换地址会清空待发通知。</p>
      <p v-if="saved.last_error" class="report-error">{{ saved.last_error }}，稍后自动重试。</p>
      <p v-if="saved.dropped" class="report-error">队列已满时丢弃了 {{ saved.dropped }} 条较早的通知。</p>
      <div class="actions">
        <button type="submit" class="btn btn-primary" :disabled="busy || !changed">保存</button>
        <button type="button" class="btn btn-secondary" :disabled="busy || changed || !saved.configured" @click="test">发送测试通知</button>
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

const saved = ref(null), enabled = ref(false), url = ref()
const busy = ref(false), retryLoad = ref(false), notice = ref(''), error = ref('')
const controller = new AbortController()
const changed = computed(() => saved.value && (saved.value.enabled !== enabled.value || url.value !== undefined))

function populate(data) {
  saved.value = data
  enabled.value = data.enabled
  url.value = undefined
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

function load() { return run(async () => populate(await adminApi.webhook(controller.signal))) }
function save() {
  return run(async () => {
    populate(await adminApi.saveWebhook({ enabled: enabled.value, url: url.value, revision: saved.value.revision }, controller.signal))
    notice.value = '推送设置已保存'
  })
}
function test() {
  return run(async () => {
    await adminApi.testWebhook(controller.signal)
    notice.value = '接收端已接受测试通知'
  })
}
onMounted(load)
onBeforeUnmount(() => controller.abort())
</script>

<style scoped>
form { display: grid; gap: 14px; margin-block: 14px; }
.actions { display: flex; flex-wrap: wrap; gap: 8px; }
h3 { font-size: 15px; color: var(--ink); }
p { font-size: 13px; overflow-wrap: anywhere; }
</style>
