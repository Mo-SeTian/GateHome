<template>
  <div class="form-field secret-field">
    <label :for="id">{{ label }}</label>
    <div class="secret-control">
      <input
        ref="input"
        :id="id" :name="name" class="form-input"
        :type="visible ? 'text' : 'password'"
        :value="modelValue ?? savedValue" :placeholder="configured && modelValue === '' ? '' : placeholder"
        :readonly="readonly" :disabled="disabled || loading" :maxlength="maxlength" :required="required"
        :autocomplete="autocomplete" spellcheck="false" @input="edit"
      />
      <button type="button" class="secret-eye"
        :disabled="disabled || loading || (!visible && !modelValue && !canReadSaved && !canShowEmpty)"
        :aria-label="visible ? `隐藏${eyeName}` : canReadSaved ? `显示已保存的${eyeName}` : `显示${eyeName}`"
        :aria-pressed="visible" :aria-controls="id" :aria-busy="loading" @click="toggle">
        <LoaderCircle v-if="loading" :size="16" class="secret-spinner" aria-hidden="true" />
        <EyeOff v-else-if="visible" :size="16" aria-hidden="true" />
        <Eye v-else :size="16" aria-hidden="true" />
      </button>
    </div>
    <p v-if="error" class="report-error" role="alert">{{ error }}</p>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Eye, EyeOff, LoaderCircle } from 'lucide-vue-next'
import { adminApi } from '@/api/admin'

const props = defineProps({
  id: { type: String, required: true }, name: { type: String, default: 'token' },
  label: { type: String, default: 'API Token' }, modelValue: String,
  kind: { type: String, default: '' }, credentialId: { type: String, default: '' },
  configured: Boolean, disabled: Boolean, readonly: Boolean, required: Boolean,
  secretName: { type: String, default: 'Token' },
  autocomplete: { type: String, default: 'new-password' },
  revealable: { type: Boolean, default: true },
  placeholder: { type: String, default: '留空保留已保存的 Token' },
  maxlength: { type: Number, default: 512 },
  modelModifiers: { type: Object, default: () => ({}) }
})
const emit = defineEmits(['update:modelValue'])
const input = ref(null)
defineExpose({ focus: () => input.value?.focus() })
const visible = ref(false), savedValue = ref(''), error = ref(''), loading = ref(false)
const canReadSaved = computed(() => props.modelValue === undefined && props.kind && props.configured && props.revealable)
const canShowEmpty = computed(() => props.secretName === '密码' && !props.readonly)
const eyeName = computed(() => props.secretName === '密码' ? '密码' : ' ' + props.secretName)
let controller, generation = 0
function reset() {
  generation++
  controller?.abort()
  visible.value = false
  savedValue.value = ''
  error.value = ''
  loading.value = false
}
function edit(event) {
  generation++
  controller?.abort()
  savedValue.value = ''
  error.value = ''
  emit('update:modelValue', props.modelModifiers.trim ? event.target.value.trim() : event.target.value)
}
async function toggle() {
  if (visible.value) return reset()
  error.value = ''
  if (props.modelValue) { visible.value = true; return }
  if (!canReadSaved.value) {
    if (canShowEmpty.value) visible.value = true
    return
  }
  const request = ++generation
  controller = new AbortController()
  loading.value = true
  try {
    const result = await adminApi.revealCredential({ kind: props.kind, id: props.credentialId }, controller.signal)
    if (request !== generation) return
    savedValue.value = result.value
    visible.value = true
  } catch (err) {
    if (request === generation && err.name !== 'AbortError') error.value = err.message
  } finally {
    if (request === generation) loading.value = false
  }
}
function hidden() { if (document.visibilityState === 'hidden') reset() }
watch(() => [props.kind, props.credentialId, props.configured, props.revealable], reset)
watch(() => props.modelValue, value => { if (!value || props.readonly) reset() })
onMounted(() => document.addEventListener('visibilitychange', hidden))
onBeforeUnmount(() => { reset(); document.removeEventListener('visibilitychange', hidden) })
</script>

<style scoped>
.secret-field { display: flex; flex-direction: column; gap: 5px; min-width: 0; }
.secret-field > label { font-size: 12px; font-weight: 600; color: var(--muted); }
.secret-control { position: relative; width: 100%; }
.secret-control .form-input { padding-inline-end: 48px !important; height: 44px !important; min-height: 44px !important; }
.secret-eye { position: absolute; inset: 0 0 auto auto; display: grid; place-items: center; width: 44px; height: 44px; border: 0; border-radius: var(--radius-sm); background: transparent; color: var(--muted); cursor: pointer; }
.secret-eye:hover:not(:disabled) { color: var(--ink); background: var(--strip-bg); }
.secret-eye:disabled { opacity: 0.5; cursor: default; }
.secret-spinner { animation: secret-spin 1s linear infinite; }
@keyframes secret-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { .secret-spinner { animation: none; } }
</style>
