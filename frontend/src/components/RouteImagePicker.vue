<template>
  <div class="image-picker-panel" :class="{ 'is-busy': busy }">
    <div class="picker-header">
      <span class="picker-title">服务图片与自定义图标</span>
      <span class="picker-hint">用于反代卡片、SunPanel 与导航快捷方式</span>
    </div>

    <!-- 第一通道：本地上传通道（预览槽 + 满宽上传按钮 + 可选恢复默认） -->
    <div class="picker-channel-row">
      <div class="preview-box" :title="modelValue ? '当前已设置自定义图片' : '默认图标占位'">
        <img
          v-if="modelValue"
          :src="`/api/route-images/${modelValue}`"
          class="route-icon-img"
          width="40"
          height="40"
          alt="所选服务图片"
        />
        <div v-else class="preview-fallback">
          <ImageIcon :size="20" class="fallback-icon" />
        </div>
      </div>

      <div class="upload-action-wrap">
        <label
          class="custom-file-btn"
          :class="{ disabled: busy }"
          title="从本地上传 PNG、JPG、GIF、WebP 或 ICO 格式图片"
        >
          <Upload :size="13" class="btn-icon" />
          <span>{{ busy ? '正在上传…' : '选择本地图片' }}</span>
          <input
            type="file"
            class="hidden-file-input"
            accept="image/png,image/jpeg,image/gif,image/webp,image/x-icon"
            :disabled="busy"
            @change="upload"
          />
        </label>
        <button
          v-if="modelValue"
          type="button"
          class="clear-img-btn"
          :disabled="busy"
          title="移除自定义图片，恢复系统默认分类图标"
          @click="emit('update:modelValue', '')"
        >
          <RotateCcw :size="11" />
          <span>恢复默认</span>
        </button>
      </div>
    </div>

    <!-- 第二通道：在线图片导入通道（满宽输入框 + 导入按钮） -->
    <div class="picker-channel-row">
      <div class="url-input-wrap">
        <Link2 :size="13" class="url-icon" />
        <input
          v-model="url"
          class="form-input url-input"
          type="url"
          placeholder="输入在线图片 URL (https://...)"
          aria-label="图片地址"
          :disabled="busy"
          @keydown.enter.prevent="importImage"
        />
      </div>
      <button
        type="button"
        class="btn btn-secondary import-btn"
        :disabled="!url.trim() || busy"
        @click="importImage"
      >
        导入
      </button>
    </div>

    <!-- 底部格式提示 -->
    <div class="picker-footer">
      <span class="file-format-tip">支持 PNG / JPG / GIF / WebP / ICO 格式（建议 1:1 比例）</span>
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
  RotateCcw,
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
  box-sizing: border-box;
  width: 100%;
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
  background: rgba(255, 255, 255, 0.02);
  padding: 12px 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
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
  gap: 6px;
  flex-wrap: wrap;
  min-width: 0;
}
.picker-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--ink);
}
.picker-hint {
  font-size: 11px;
  color: var(--faint);
  line-height: 1.4;
}

/* 核心通道行：保证每一行都是 100% 充满，两端严格对齐 */
.picker-channel-row {
  display: flex !important;
  flex-direction: row !important;
  align-items: center !important;
  gap: 8px !important;
  width: 100% !important;
  min-width: 0 !important;
  box-sizing: border-box !important;
}

.preview-box {
  width: 40px !important;
  height: 40px !important;
  min-width: 40px !important;
  min-height: 40px !important;
  border-radius: var(--radius-sm) !important;
  background: var(--strip-bg) !important;
  border: 1px solid var(--line) !important;
  display: flex !important;
  align-items: center !important;
  justify-content: center !important;
  flex-shrink: 0 !important;
  overflow: hidden !important;
  box-shadow: inset 0 1px 3px rgba(0, 0, 0, 0.15) !important;
  box-sizing: border-box !important;
}
.route-icon-img {
  width: 100%;
  height: 100%;
  object-fit: contain;
  padding: 2px;
}
.fallback-icon {
  color: var(--faint);
  opacity: 0.8;
}

.upload-action-wrap {
  display: flex !important;
  flex-direction: row !important;
  align-items: center !important;
  gap: 6px !important;
  flex: 1 1 0% !important;
  min-width: 0 !important;
}

/* 自定义上传按钮：充满剩余区域，两端对齐 */
.custom-file-btn {
  display: flex !important;
  flex-direction: row !important;
  align-items: center !important;
  justify-content: center !important;
  gap: 6px !important;
  flex: 1 1 0% !important;
  min-width: 0 !important;
  height: 40px !important;
  min-height: 40px !important;
  padding: 0 12px !important;
  line-height: 40px !important;
  white-space: nowrap !important;
  word-break: keep-all !important;
  background: var(--brand-gradient) !important;
  color: #ffffff !important;
  border-radius: var(--radius-sm) !important;
  font-size: 12.5px !important;
  font-weight: 600 !important;
  cursor: pointer !important;
  box-shadow: 0 2px 6px rgba(99, 102, 241, 0.35) !important;
  transition: all 0.2s cubic-bezier(0.16, 1, 0.3, 1) !important;
  position: relative !important;
  user-select: none !important;
  margin: 0 !important;
  box-sizing: border-box !important;
}
.custom-file-btn:hover:not(.disabled) {
  opacity: 0.92;
  transform: translateY(-1px);
  box-shadow: 0 4px 10px rgba(99, 102, 241, 0.45);
}
.custom-file-btn.disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn-icon {
  flex-shrink: 0;
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

.clear-img-btn {
  display: inline-flex !important;
  align-items: center !important;
  justify-content: center !important;
  gap: 4px !important;
  font-size: 11.5px !important;
  color: var(--bad) !important;
  background: rgba(244, 63, 94, 0.1) !important;
  border: 1px solid rgba(244, 63, 94, 0.25) !important;
  padding: 0 10px !important;
  height: 40px !important;
  min-height: 40px !important;
  border-radius: var(--radius-sm) !important;
  cursor: pointer !important;
  transition: var(--transition-smooth) !important;
  white-space: nowrap !important;
  flex: 0 0 auto !important;
  box-sizing: border-box !important;
}
.clear-img-btn:hover:not(:disabled) {
  background: rgba(244, 63, 94, 0.18);
  border-color: rgba(244, 63, 94, 0.4);
}

/* 下半部分：在线导入整行两端完全对齐 */
.url-input-wrap {
  position: relative !important;
  flex: 1 1 0% !important;
  min-width: 0 !important;
  display: flex !important;
  align-items: center !important;
}
.url-icon {
  position: absolute !important;
  left: 10px !important;
  color: var(--muted) !important;
  pointer-events: none !important;
  z-index: 2 !important;
}
.url-input {
  box-sizing: border-box !important;
  width: 100% !important;
  min-width: 0 !important;
  padding-left: 30px !important;
  padding-right: 8px !important;
  height: 38px !important;
  min-height: 38px !important;
  line-height: 36px !important;
  font-size: 12px !important;
  border-radius: var(--radius-sm) !important;
}

.import-btn {
  height: 38px !important;
  min-height: 38px !important;
  padding: 0 14px !important;
  font-size: 12px !important;
  font-weight: 500 !important;
  white-space: nowrap !important;
  flex: 0 0 auto !important;
  border-radius: var(--radius-sm) !important;
  box-sizing: border-box !important;
}

.picker-footer {
  display: flex;
  justify-content: flex-start;
  width: 100%;
}
.file-format-tip {
  font-size: 11px;
  color: var(--faint);
  line-height: 1.4;
}

/* 状态与错误提示 */
.picker-status-msg,
.picker-error-msg {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11.5px;
  padding: 5px 9px;
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

@media (max-width: 480px) {
  .image-picker-panel {
    padding: 10px 12px;
  }
  .picker-header {
    flex-direction: column;
    align-items: flex-start;
    gap: 2px;
  }
  .file-format-tip {
    font-size: 10.5px;
  }
}
</style>
