<template>
  <div class="report-page">
    <header v-if="!embedded" class="report-heading">
      <h1>模型接入点</h1>
      <RouterLink class="btn btn-secondary" to="/assistant">打开网关助手</RouterLink>
    </header>
    <section class="report-panel endpoint-panel">
      <div class="endpoint-toolbar">
        <h2>供应商</h2>
        <button v-if="settings" class="btn btn-secondary" type="button" :disabled="!!busy" @click="addProvider"><Plus :size="16" /> 添加供应商</button>
      </div>
      <div v-if="settings?.providers?.some(row => row.base_url)" class="endpoint-list" aria-label="已保存的接入点">
        <button v-for="row in settings.providers.filter(row => row.base_url)" :key="row.id" type="button"
          class="btn btn-secondary" :aria-pressed="draft?.id === row.id" :disabled="!!busy" @click="setDraft(row)">{{ row.name }}</button>
      </div>
      <p class="report-note">聊天内容、反代地址、监听端口、WireGuard 设备摘要和运行状态会发送到对话选中的模型接入点（不含密钥与密码）。</p>
      <form v-if="draft" @submit.prevent="save">
        <fieldset class="endpoint-form" :disabled="!!busy">
          <label>接入点名称<input v-model.trim="draft.name" class="form-input" required maxlength="128" placeholder="家庭模型服务" /></label>
          <label>请求方式
            <select v-model="draft.protocol" class="form-input" @change="resetCatalog">
              <option value="chat">OpenAI Chat Completions</option>
              <option value="responses">OpenAI Responses</option>
              <option value="anthropic">Anthropic Messages</option>
            </select>
          </label>
          <label>接口地址（Base URL）
            <input v-model.trim="draft.base_url" class="form-input mono" type="url" required maxlength="2048"
              :placeholder="draft.protocol === 'anthropic' ? 'https://api.anthropic.com/v1' : 'https://api.openai.com/v1'" @change="resetCatalog" />
          </label>
          <label>API Key
            <input v-model="draft.api_key" class="form-input" type="password" autocomplete="new-password" maxlength="4096"
              :disabled="draft.clear_key" :placeholder="savedKeyApplies ? '已保存，留空沿用' : '无需认证时可留空'" />
          </label>
          <label v-if="savedKeyApplies" class="endpoint-check"><input v-model="draft.clear_key" type="checkbox" /> 清除已保存的 API Key</label>
          <label>模型 ID
            <input v-model.trim="draft.model" class="form-input mono" required maxlength="200" placeholder="手动输入，或从获取结果中选择" />
          </label>
          <div class="model-catalog">
            <button class="btn btn-secondary" type="button" :disabled="!!busy || !draft.base_url" @click="fetchModels">
              <RefreshCw :size="15" /> {{ busy === 'models' ? '获取中…' : '获取模型' }}
            </button>
            <label v-if="catalog.length">可用模型
              <select v-model="draft.model" class="form-input" aria-label="选择获取到的模型">
                <option value="" disabled>选择默认模型</option>
                <option v-if="draft.model && !catalog.includes(draft.model)" :value="draft.model">{{ draft.model }}</option>
                <option v-for="id in catalog" :key="id" :value="id">{{ id }}</option>
              </select>
            </label>
          </div>
          <label>默认思考强度
            <select v-model="draft.effort" class="form-input">
              <option value="">模型默认</option><option value="low">低</option><option value="medium">中</option><option value="high">高</option>
            </select>
          </label>
          <p v-if="draft.effort" class="report-note">请为支持此设置的模型使用思考强度。</p>
          <p v-if="error" class="report-error" role="alert">{{ error }}</p>
          <p v-if="feedback" class="report-note" role="status">{{ feedback }}</p>
          <div class="form-actions">
            <button class="btn btn-secondary" type="button" :disabled="!!busy" @click="test">{{ busy === 'test' ? '测试中…' : '测试连接' }}</button>
            <button class="btn btn-primary" type="submit" :disabled="!!busy">{{ busy === 'save' ? '保存中…' : '保存接入点' }}</button>
          </div>
        </fieldset>
      </form>
      <p v-else-if="error" class="report-error" role="alert">{{ error }} <button class="btn btn-secondary" @click="load">重试</button></p>
      <p v-else class="report-note" role="status">正在读取接入点…</p>
    </section>
  </div>
</template>

<script setup>
import '@/styles/reports.css'
import { computed, onMounted, ref } from 'vue'
import { Plus, RefreshCw } from 'lucide-vue-next'
import { useAssistant } from '@/composables/useAssistant'

defineProps({
  embedded: { type: Boolean, default: false }
})

const { settings, loadSettings, saveSettings, testSettings, listModels } = useAssistant()
const draft = ref(null)
const catalog = ref([])
const error = ref('')
const feedback = ref('')
const busy = ref('')
const selected = computed(() => settings.value?.providers?.find(row => row.id === draft.value?.id))
const savedKeyApplies = computed(() => selected.value?.key_configured &&
  selected.value.base_url === draft.value?.base_url.replace(/\/+$/, '') && selected.value.protocol === draft.value?.protocol)

function setDraft(data) {
  const { id, name, base_url, model, protocol = 'chat', effort = '', models } = data
  draft.value = { id, name, base_url, model, protocol, effort, api_key: '', clear_key: false }
  catalog.value = models || []
  error.value = feedback.value = ''
}

function addProvider() {
  const id = settings.value.base_url ? 'new' : 'default'
  setDraft({ id, name: '', base_url: '', model: '' })
}

function resetCatalog() {
  catalog.value = []
  draft.value.clear_key = false
  feedback.value = ''
}

async function load() {
  try { setDraft(await loadSettings()) } catch (err) { error.value = err.message }
}

async function run(operation, action) {
  if (busy.value) return
  busy.value = operation
  error.value = feedback.value = ''
  try { await action() } catch (err) { error.value = err.message } finally { busy.value = '' }
}

function payload() {
  return { ...draft.value, models: catalog.value.filter(id => id !== draft.value.model).slice(0, 299), revision: settings.value.revision }
}

function fetchModels() {
  return run('models', async () => {
    const target = { ...draft.value }
    const { models } = await listModels({ ...payload(), models: [] })
    if (draft.value.base_url !== target.base_url || draft.value.protocol !== target.protocol ||
        draft.value.api_key !== target.api_key || draft.value.clear_key !== target.clear_key) return
    catalog.value = models
    if (!draft.value.model && models.length) draft.value.model = models[0]
    feedback.value = models.length ? `已找到 ${models.length} 个模型。` : '接口未返回模型，可手动输入模型 ID。'
  })
}

function test() {
  return run('test', async () => {
    await testSettings(payload())
    feedback.value = '连接成功，模型已回复。'
  })
}

function save() {
  return run('save', async () => {
    const body = payload()
    const data = await saveSettings(body)
    setDraft(body.id === 'new' ? data.providers.at(-1) : data.providers.find(row => row.id === body.id))
    feedback.value = '接入点已保存。'
  })
}

onMounted(load)
</script>

<style scoped>
.endpoint-panel { max-width: 820px; }
.endpoint-toolbar, .endpoint-list, .model-catalog, .form-actions { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.endpoint-toolbar { justify-content: space-between; margin-bottom: 12px; }
.endpoint-list { margin-bottom: 14px; }
.endpoint-list [aria-pressed="true"] { border-color: var(--brand); color: var(--ink); }
.endpoint-form { display: grid; gap: 14px; margin: 18px 0 0; border: 0; padding: 0; min-inline-size: 0; }
.endpoint-form label { display: grid; gap: 6px; font-size: 13px; font-weight: 600; min-width: 0; }
.endpoint-form .endpoint-check { display: flex; align-items: center; font-weight: 400; }
.model-catalog { align-items: end; }
.model-catalog label { flex: 1 1 220px; }
.form-actions { justify-content: flex-end; }
</style>
