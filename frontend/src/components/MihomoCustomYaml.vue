<template>
  <SpotlightCard class="report-panel custom-yaml-panel">
    <div class="panel-header-row">
      <div class="custom-yaml-title">
        <strong>{{ title }}</strong>
        <span class="badge" :class="dirty ? 'badge-warn' : 'badge-ok'">{{ dirty ? '未保存' : '与页面同步' }}</span>
      </div>
      <div class="report-actions">
        <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="load">
          <RefreshCw :size="13" /> 重新载入
        </button>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="validate">
          <CheckCircle2 :size="13" /> 校验
        </button>
        <button type="button" class="btn btn-primary btn-sm" :disabled="busy || !dirty" @click="save">
          <Save :size="13" /> {{ busy ? '处理中…' : '保存并生效' }}
        </button>
      </div>
    </div>
    <p v-if="message" class="custom-yaml-msg" :class="messageOk ? 'ok' : 'bad'" role="status">{{ message }}</p>
    <textarea
      v-model="content"
      class="form-input custom-yaml-editor mono-text"
      :aria-label="`${title} YAML`"
      spellcheck="false"
      wrap="off"
      @input="onInput"
      @keydown.tab.prevent="insertTab"
    />
  </SpotlightCard>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { CheckCircle2, RefreshCw, Save } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { adminApi } from '@/api/admin'
import { useToast } from '@/composables/useToast'

const props = defineProps({
  kind: { type: String, required: true }
})
const emit = defineEmits(['saved'])

const toast = useToast()
const content = ref('')
const original = ref('')
const busy = ref(false)
const message = ref('')
const messageOk = ref(true)

const title = computed(() => (props.kind === 'groups' ? '自定义策略组 (proxy-groups)' : '自定义分流规则 (rules)'))
const dirty = computed(() => content.value !== original.value)

function onInput() {
  message.value = ''
}

function insertTab(event) {
  const el = event.target
  const start = el.selectionStart
  const end = el.selectionEnd
  content.value = content.value.slice(0, start) + '  ' + content.value.slice(end)
  onInput()
  requestAnimationFrame(() => {
    el.selectionStart = el.selectionEnd = start + 2
  })
}

const controller = new AbortController()

async function load() {
  busy.value = true
  try {
    const res = await adminApi.mihomoCustomYaml(props.kind, controller.signal)
    if (controller.signal.aborted) return
    content.value = res.content || ''
    original.value = content.value
    message.value = ''
  } catch (err) {
    if (!controller.signal.aborted && err?.name !== 'AbortError') {
      toast.error(err.message)
    }
  } finally {
    if (!controller.signal.aborted) busy.value = false
  }
}

async function validate() {
  busy.value = true
  try {
    await adminApi.saveMihomoCustomYaml(props.kind, content.value, true, controller.signal)
    if (controller.signal.aborted) return
    message.value = '校验通过，可保存'
    messageOk.value = true
  } catch (err) {
    if (!controller.signal.aborted && err?.name !== 'AbortError') {
      message.value = err.message
      messageOk.value = false
    }
  } finally {
    if (!controller.signal.aborted) busy.value = false
  }
}

async function save() {
  busy.value = true
  try {
    const res = await adminApi.saveMihomoCustomYaml(props.kind, content.value, false, controller.signal)
    if (controller.signal.aborted) return
    content.value = res.content || content.value
    original.value = content.value
    message.value = ''
    toast.success('已保存并合并到运行配置')
    emit('saved', res.status)
  } catch (err) {
    if (!controller.signal.aborted && err?.name !== 'AbortError') {
      message.value = err.message
      messageOk.value = false
    }
  } finally {
    if (!controller.signal.aborted) busy.value = false
  }
}

onMounted(load)
onBeforeUnmount(() => controller.abort())
</script>

<style scoped>
.custom-yaml-panel {
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
  margin-bottom: 12px;
}

.panel-header-row .report-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  margin-left: auto;
}

.custom-yaml-title {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  min-width: 0;
}

.custom-yaml-msg {
  margin: 0 0 10px;
  padding: 8px 12px;
  border-radius: var(--radius-sm);
  font-size: 12px;
  line-height: 1.5;
  border: 1px solid var(--line);
  overflow-wrap: anywhere;
}

.custom-yaml-msg.ok {
  color: var(--ok);
  background: rgba(16, 185, 129, 0.1);
  border-color: rgba(16, 185, 129, 0.35);
}

.custom-yaml-msg.bad {
  color: var(--bad);
  background: rgba(244, 63, 94, 0.1);
  border-color: rgba(244, 63, 94, 0.35);
}

textarea.form-input.custom-yaml-editor {
  width: 100%;
  height: clamp(360px, 56dvh, 680px) !important;
  min-height: 320px !important;
  padding: 12px 14px;
  font-size: 12.5px;
  line-height: 1.6;
  resize: vertical;
  tab-size: 2;
  white-space: pre;
  overflow: auto;
}
</style>
