<template>
  <div class="image-picker-panel" :class="{ 'is-busy': busy }">
    <div class="picker-header">
      <span class="picker-title">服务图片与自定义图标</span>
      <span class="picker-hint">用于反代卡片、SunPanel 与导航快捷方式</span>
    </div>

    <!-- 上半部分：预览图标 + 本地上传按钮与格式说明 -->
    <div class="picker-top-row">
      <div class="preview-box" :title="modelValue ? '当前已设置自定义图片' : '默认图标占位'">
        <img
          v-if="modelValue"
          :src="`/api/route-images/${modelValue}`"
          class="route-icon-img"
          width="44"
          height="44"
          alt="所选服务图片"
        />
        <div v-else class="preview-fallback">
          <ImageIcon :size="22" class="fallback-icon" />
        </div>
      </div>
      <div class="upload-btn-group">
        <div class="btn-with-clear">
          <label class="custom-file-btn" :class="{ disabled: busy }" title="从本地上传 PNG、JPG、SVG、WebP 或 ICO 格式图片">
            <Upload :size="13" class="btn-icon" />
            <span>{{ busy ? '正在上传…' : '选择本地图片' }}</span>
            <input
              type="file"
              class="hidden-file-input"
              accept="image/png,image/jpeg,image/gif,image/webp,image/x-icon,image/svg+xml"
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
        <span class="file-format-tip">支持 PNG / JPG / SVG / WebP / ICO</span>
      </div>
    </div>

    <!-- 下半部分：在线图片地址输入 + 导入按钮 -->
    <div class="picker-url-row">
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

/* 上半部分：头像与本地上传行 */
.picker-top-row {
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 10px;
  width: 100%;
  min-width: 0;
}

.preview-box {
  width: 44px;
  height: 44px;
  border-radius: 10px;
  background: var(--strip-bg);
  border: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  overflow: hidden;
  box-shadow: inset 0 1px 3px rgba(0, 0, 0, 0.15);
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

.upload-btn-group {
  display: flex;
  flex-direction: column;
  gap: 4px;
  flex: 1;
  min-width: 0;
}

.btn-with-clear {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

/* 自定义上传按钮：强制锁死为水平单行结构，杜绝任何外部 label 样式污染为纵向方块 */
.custom-file-btn {
  display: inline-flex !important;
  flex-direction: row !important;
  align-items: center !important;
  justify-content: center !important;
  gap: 6px !important;
  height: 32px !important;
  min-height: 32px !important;
  padding: 0 12px !important;
  line-height: 32px !important;
  white-space: nowrap !important;
  word-break: keep-all !important;
  background: var(--brand-gradient) !important;
  color: #ffffff !important;
  border-radius: var(--radius-sm) !important;
  font-size: 12px !important;
  font-weight: 600 !important;
  cursor: pointer !important;
  box-shadow: 0 2px 6px rgba(99, 102, 241, 0.35) !important;
  transition: all 0.2s cubic-bezier(0.16, 1, 0.3, 1) !important;
  position: relative !important;
  user-select: none !important;
  margin: 0 !important;
  flex-shrink: 0 !important;
  align-self: flex-start;
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
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  color: var(--bad);
  background: rgba(244, 63, 94, 0.1);
  border: 1px solid rgba(244, 63, 94, 0.2);
  padding: 0 8px;
  height: 32px;
  border-radius: var(--radius-sm);
  cursor: pointer;
  transition: var(--transition-smooth);
  white-space: nowrap;
  flex-shrink: 0;
}
.clear-img-btn:hover:not(:disabled) {
  background: rgba(244, 63, 94, 0.18);
  border-color: rgba(244, 63, 94, 0.35);
}

.file-format-tip {
  font-size: 10.5px;
  color: var(--faint);
  line-height: 1.3;
  white-space: nowrap;
}

/* 下半部分：在线导入整行自适应 */
.picker-url-row {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  min-width: 0;
}

.url-input-wrap {
  position: relative;
  flex: 1 1 0%;
  min-width: 0;
  display: flex;
  align-items: center;
}
.url-icon {
  position: absolute;
  left: 9px;
  color: var(--muted);
  pointer-events: none;
  z-index: 2;
}
.url-input {
  box-sizing: border-box !important;
  width: 100% !important;
  min-width: 0 !important;
  padding-left: 28px !important;
  padding-right: 8px !important;
  height: 32px !important;
  min-height: 32px !important;
  line-height: 30px !important;
  font-size: 11.5px !important;
  border-radius: var(--radius-sm) !important;
}

.import-btn {
  height: 32px !important;
  min-height: 32px !important;
  padding: 0 12px !important;
  font-size: 12px !important;
  font-weight: 500 !important;
  white-space: nowrap !important;
  flex: 0 0 auto !important;
  border-radius: var(--radius-sm) !important;
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
    font-size: 10px;
  }
}
</style>
