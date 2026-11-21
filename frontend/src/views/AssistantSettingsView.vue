<template>
  <div class="report-page">
    <header class="report-heading">
      <h1>模型接入点</h1>
      <RouterLink class="btn btn-secondary" to="/assistant">打开网关助手</RouterLink>
    </header>
    <section class="report-panel endpoint-panel">
      <h2>OpenAI 兼容接口</h2>
      <p class="report-note">聊天内容、反代地址和运行摘要会发送到此接入点。连接测试仅发送一条测试消息。</p>
      <form v-if="draft" class="endpoint-form" @submit.prevent="save">
        <label>接入点名称<input v-model.trim="draft.name" class="form-input" required maxlength="128" placeholder="家庭模型服务" /></label>
        <label>接口地址（Base URL）<input v-model.trim="draft.base_url" class="form-input mono" type="url" required maxlength="2048" placeholder="https://ai.example.com/v1" /></label>
        <label>模型 ID<input v-model.trim="draft.model" class="form-input mono" required maxlength="200" placeholder="模型服务提供的 ID" /></label>
        <label>API Key<input v-model="draft.api_key" class="form-input" type="password" autocomplete="new-password" maxlength="4096"
          :disabled="draft.clear_key" :placeholder="savedKeyApplies ? '已保存，留空沿用' : '无需认证时可留空'" /></label>
        <label v-if="savedKeyApplies" class="endpoint-check"><input v-model="draft.clear_key" type="checkbox" /> 清除已保存的 API Key</label>
        <p v-if="settings?.key_configured && !savedKeyApplies" class="report-note">修改接口地址后，旧 API Key 不会用于新地址。</p>
        <p v-if="error" class="report-error" role="alert">{{ error }}</p>
        <p v-if="feedback" class="report-note" role="status">{{ feedback }}</p>
        <div class="form-actions">
          <button class="btn btn-secondary" type="button" :disabled="!!busy" @click="test">{{ busy === 'test' ? '测试中…' : '测试连接' }}</button>
          <button class="btn btn-primary" type="submit" :disabled="!!busy">{{ busy === 'save' ? '保存中…' : '保存接入点' }}</button>
        </div>
      </form>
      <p v-else-if="error" class="report-error" role="alert">{{ error }} <button class="btn btn-secondary" @click="load">重试</button></p>
      <p v-else class="report-note" role="status">正在读取接入点…</p>
    </section>
  </div>
</template>

<script setup>
import '@/styles/reports.css'
import { computed, onMounted, ref } from 'vue'
import { useAssistant } from '@/composables/useAssistant'

const { settings, loadSettings, saveSettings, testSettings } = useAssistant()
const draft = ref(null)
const error = ref('')
const feedback = ref('')
const busy = ref('')
const savedKeyApplies = computed(() => settings.value?.key_configured &&
  settings.value.base_url === draft.value?.base_url.replace(/\/+$/, ''))

function setDraft(data) {
  const { name, base_url, model, revision } = data
  draft.value = { name, base_url, model, revision, api_key: '', clear_key: false }
}

async function load() {
  error.value = ''
  try { setDraft(await loadSettings()) } catch (err) { error.value = err.message }
}

async function run(kind) {
  busy.value = kind
  feedback.value = ''
  error.value = ''
  try {
    if (kind === 'test') {
      await testSettings(draft.value)
      feedback.value = '连接成功，模型已回复。'
    } else {
      setDraft(await saveSettings(draft.value))
      feedback.value = '接入点已保存。'
    }
  } catch (err) {
    error.value = err.message
    if (kind === 'save' && err.status === 409) {
      const latest = await loadSettings().catch(() => null)
      if (latest) {
        setDraft(latest)
        error.value = '配置已变化，已载入最新接入点。请重新检查并保存。'
      }
    }
  } finally {
    busy.value = ''
  }
}

function save() { return run('save') }
function test(event) {
  if (event.target.form.reportValidity()) return run('test')
}
onMounted(load)
</script>

<style scoped>
.endpoint-panel { max-width: 720px; background: var(--card-bg); border: 1px solid var(--line); border-radius: var(--radius-card); }
.endpoint-form { display: grid; gap: 15px; margin-top: 18px; }
.endpoint-form label { display: grid; gap: 6px; }
.endpoint-form .endpoint-check { display: flex; align-items: center; }
.endpoint-form .form-actions { display: flex; gap: 8px; flex-wrap: wrap; }
</style>
