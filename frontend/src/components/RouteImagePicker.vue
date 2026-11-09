<template>
  <div class="image-picker-panel" :class="{ 'is-busy': busy }">
    <div class="picker-header">
      <span class="picker-title">服务图片与自定义图标</span>
      <span class="picker-hint">用于反代卡片、SunPanel 与导航快捷方式</span>
    </div>

    <div class="picker-body">
      <!-- 预览区 -->
      <div class="picker-preview-col">
        <div class="preview-box">
          <img
            v-if="modelValue"
            :src="`/api/route-images/${modelValue}`"
            class="route-icon-img"
            width="48"
            height="48"
            alt="所选服务图片"
          />
          <div v-else class="preview-fallback">
            <ImageIcon :size="24" class="fallback-icon" />
          </div>
        </div>
        <button
          v-if="modelValue"
          type="button"
          class="btn btn-ghost btn-xs text-bad"
          :disabled="busy"
          @click="emit('update:modelValue', '')"
        >
          恢复默认
        </button>
      </div>

      <!-- 操作区 -->
      <div class="picker-controls-col">
        <!-- 本地上传 -->
        <div class="file-upload-row">
          <label class="custom-file-btn" :class="{ disabled: busy }">
            <Upload :size="14" />
            <span>{{ busy ? '正在上传…' : '选择本地图片' }}</span>
            <input
              type="file"
              class="hidden-file-input"
              accept="image/png,image/jpeg,image/gif,image/webp,image/x-icon,image/svg+xml"
              :disabled="busy"
              @change="upload"
            />
          </label>
          <span class="file-format-tip">支持 PNG / JPG / SVG / WebP / ICO</span>
        </div>

        <!-- 在线导入 -->
        <div class="image-url-row">
          <div class="url-input-wrap">
            <Link2 :size="13" class="url-icon" />
            <input
              v-model="url"
              class="form-input url-input"
              type="url"
              placeholder="或输入在线图片地址 (https://...)"
              aria-label="图片地址"
              :disabled="busy"
              @keydown.enter.prevent="importImage"
            />
          </div>
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="!url.trim() || busy"
            @click="importImage"
          >
            导入
          </button>
        </div>
      </div>
    </div>

    <!-- 状态反馈 -->
    <div v-if="message" class="picker-status-msg" role="status">
      <Check :size="13" class="text-ok" />
      <span>{{ message }}</span>
    </div>
    <div v-if="error" class="picker-error-msg" role="alert">
      <AlertCircle :size="13" class="text-bad" />
      <span>{{ error }}</span>
    </div>
  </div>
</template>

<script setup>
import { onUnmounted, ref } from 'vue'
import {
  AlertCircle,
  Check,
  Image as ImageIcon,
  Link2,
  Upload
} from 'lucide-vue-next'
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
.image-picker-panel {
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
  background: rgba(255, 255, 255, 0.02);
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-width: 0;
  transition: var(--transition-smooth);
}
.image-picker-panel:hover {
  border-color: var(--card-border-hover);
}

.picker-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
  flex-wrap: wrap;
}
.picker-title {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--ink);
}
.picker-hint {
  font-size: 11px;
  color: var(--faint);
}

.picker-body {
  display: flex;
  align-items: flex-start;
  gap: 16px;
}

.picker-preview-col {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}
.preview-box {
  width: 54px;
  height: 54px;
  border-radius: var(--radius-md);
  background: var(--strip-bg);
  border: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  box-shadow: inset 0 1px 3px rgba(0, 0, 0, 0.2);
}
.route-icon-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.fallback-icon {
  color: var(--faint);
}

.picker-controls-col {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
}

.file-upload-row {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.custom-file-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 14px;
  background: var(--brand-gradient);
  color: #ffffff;
  border-radius: var(--radius-sm);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  box-shadow: 0 2px 8px rgba(99, 102, 241, 0.35);
  transition: all 0.2s cubic-bezier(0.16, 1, 0.3, 1);
  position: relative;
  user-select: none;
}
.custom-file-btn:hover:not(.disabled) {
  opacity: 0.92;
  transform: translateY(-1px);
  box-shadow: 0 4px 12px rgba(99, 102, 241, 0.5);
}
.custom-file-btn.disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.hidden-file-input {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  border: 0;
}

.file-format-tip {
  font-size: 11px;
  color: var(--faint);
}

.image-url-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}
.url-input-wrap {
  position: relative;
  flex: 1;
  display: flex;
  align-items: center;
}
.url-icon {
  position: absolute;
  left: 10px;
  color: var(--muted);
  pointer-events: none;
}
.url-input {
  padding-left: 30px !important;
  height: 34px !important;
  font-size: 12px !important;
  width: 100%;
}

.btn-xs {
  font-size: 11px !important;
  padding: 2px 6px !important;
}

.picker-status-msg,
.picker-error-msg {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  padding: 6px 10px;
  border-radius: var(--radius-sm);
}
.picker-status-msg {
  background: rgba(34, 197, 94, 0.1);
  color: var(--ok);
}
.picker-error-msg {
  background: rgba(239, 68, 68, 0.1);
  color: var(--bad);
}

@media (max-width: 520px) {
  .picker-body {
    flex-direction: column;
    align-items: stretch;
  }
  .picker-preview-col {
    flex-direction: row;
    gap: 12px;
  }
}
</style>
