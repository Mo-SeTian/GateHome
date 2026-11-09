<template>
  <fieldset class="image-picker" :disabled="busy">
    <legend>服务图片</legend>
    <img
      v-if="modelValue"
      :src="`/api/route-images/${modelValue}`"
      width="48"
      height="48"
      alt="所选服务图片"
    />
    <label
      >上传图片
      <input
        type="file"
        accept="image/png,image/jpeg,image/gif,image/webp,image/x-icon,image/svg+xml"
        @change="upload"
    /></label>
    <div class="image-url">
      <input
        v-model="url"
        class="form-input"
        type="url"
        placeholder="https://example.com/icon.png"
        aria-label="图片地址"
      />
      <button type="button" class="btn btn-secondary btn-sm" :disabled="!url.trim()" @click="importImage">
        导入
      </button>
    </div>
    <button
      v-if="modelValue"
      type="button"
      class="btn btn-ghost btn-sm"
      @click="emit('update:modelValue', '')"
    >
      使用默认图标
    </button>
    <p v-if="message" role="status">{{ message }}</p>
    <p v-if="error" role="alert">{{ error }}</p>
  </fieldset>
</template>

<script setup>
import { onUnmounted, ref } from 'vue'
import { adminApi } from '@/api/admin'

defineProps({ modelValue: String })
const emit = defineEmits(['update:modelValue', 'busy'])
const url = ref('')
const busy = ref(false)
const error = ref('')
const message = ref('')
let active = true
onUnmounted(() => {
  active = false
})

async function applyImage(operation) {
  busy.value = true
  emit('busy', true)
  error.value = ''
  message.value = '正在处理图片…'
  try {
    const result = await operation()
    if (!active) return
    emit('update:modelValue', result.id)
    url.value = ''
    message.value = '图片已选择，保存路由后生效'
  } catch (err) {
    if (active) {
      message.value = ''
      error.value = err.message
    }
  } finally {
    busy.value = false
    if (active) emit('busy', false)
  }
}

function upload(event) {
  const file = event.target.files[0]
  if (file) applyImage(() => adminApi.uploadRouteImage(file))
  event.target.value = ''
}

function importImage() {
  return applyImage(() => adminApi.importRouteImage({ url: url.value.trim() }))
}
</script>

<style scoped>
.image-picker {
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  padding: 12px;
  display: grid;
  gap: 10px;
  min-width: 0;
}
.image-picker legend,
.image-picker label,
.image-picker p {
  font-size: 12px;
  color: var(--muted);
}
.image-picker p {
  margin: 0;
}
.image-picker input[type='file'] {
  max-width: 100%;
}
.image-url {
  display: flex;
  gap: 8px;
}
.image-url input {
  min-width: 0;
  flex: 1;
}
.image-picker [role='alert'] {
  color: var(--bad);
}
</style>
