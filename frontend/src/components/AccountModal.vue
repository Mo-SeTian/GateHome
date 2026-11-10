<template>
  <dialog ref="dialogRef" class="account-dialog-card" aria-labelledby="account-dialog-title"
    @click="onDialogClick" @cancel.prevent="requestClose" @close="closeAccountModal">
        <div class="dialog-header">
          <div class="dialog-title-group">
            <span class="eyebrow">ADMIN SECURITY</span>
            <h2 id="account-dialog-title" class="dialog-title">管理员账户设置</h2>
            <p class="dialog-desc">用于登录 GateHome 管理界面，与反代服务的独立访问账号分开。</p>
          </div>
          <button type="button" class="dialog-close-btn" aria-label="关闭" :disabled="isSaving" @click="requestClose">
            <X :size="16" />
          </button>
        </div>

        <!-- Inline Error Banner -->
        <transition name="slide-down">
          <div v-if="errorMessage" class="error-banner" role="alert">
            <AlertCircle :size="15" class="error-icon" />
            <span>{{ errorMessage }}</span>
          </div>
        </transition>

        <form class="dialog-form" @submit.prevent="handleSave">
          <div class="form-group">
            <label class="form-label" for="account-username">
              <span>管理员账号</span>
              <span class="required-star">*</span>
            </label>
            <input 
              id="account-username"
              autocomplete="username"
              v-model="form.username" 
              type="text" 
              class="form-input" 
              maxlength="64" 
              required 
            />
            <small class="form-hint">1–64 字节，区分大小写；支持中文，不含控制字符或首尾空格。</small>
          </div>

          <div class="form-group">
            <label class="form-label" for="account-current-password">
              <span>当前管理密码</span>
              <span class="required-star">*</span>
            </label>
            <input 
              id="account-current-password"
              ref="currentPasswordInput"
              autocomplete="current-password"
              v-model="form.currentPassword" 
              type="password" 
              class="form-input" 
              maxlength="72" 
              placeholder="验证当前管理密码" 
              required 
            />
            <small class="form-hint">修改账号或密码前，须先验证当前管理员密码。</small>
          </div>

          <div class="form-group">
            <label class="form-label" for="account-new-password">
              <span>新管理密码（可选）</span>
            </label>
            <input 
              id="account-new-password"
              autocomplete="new-password"
              v-model="form.newPassword" 
              type="password" 
              class="form-input" 
              maxlength="72" 
              placeholder="留空时保持原管理密码" 
            />
            <small class="form-hint">12–72 字节；留空时只修改账号，保持原密码。</small>
          </div>

          <div class="form-group" v-if="form.newPassword">
            <label class="form-label" for="account-confirm-password">
              <span>再次输入新管理密码</span>
              <span class="required-star">*</span>
            </label>
            <input 
              id="account-confirm-password"
              autocomplete="new-password"
              v-model="form.confirmPassword" 
              type="password" 
              class="form-input" 
              maxlength="72" 
              placeholder="请重复新密码" 
              required 
            />
          </div>

          <div class="warning-callout">
            <AlertTriangle :size="15" class="callout-icon" />
            <span>保存后所有管理员会话都会退出，请使用新账号和管理密码重新登录。各反代服务的访问账号密码保持独立。</span>
          </div>

          <div class="dialog-actions">
            <button type="button" class="btn btn-secondary" :disabled="isSaving" @click="requestClose">
              取消
            </button>
            <button type="submit" class="btn btn-primary" :disabled="isSaving">
              <span v-if="isSaving" class="btn-spinner" />
              <Save v-else :size="15" />
              <span>{{ isSaving ? '保存中...' : '保存管理账户' }}</span>
            </button>
          </div>
        </form>
  </dialog>
</template>

<script setup>
import { nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { X, Save, AlertCircle, AlertTriangle } from 'lucide-vue-next'
import { useSession } from '@/composables/useSession'
import { useToast } from '@/composables/useToast'

const router = useRouter()
const { isAccountModalOpen, closeAccountModal, username, updateAdminPassword } = useSession()
const { success, error: toastError } = useToast()

const form = reactive({
  username: username.value,
  currentPassword: '',
  newPassword: '',
  confirmPassword: ''
})

const isSaving = ref(false)
const errorMessage = ref('')
const currentPasswordInput = ref(null)
const dialogRef = ref(null)
let returnFocus

function requestClose() {
  if (!isSaving.value) closeAccountModal()
}

function onDialogClick(event) {
  if (event.target !== event.currentTarget) return
  const bounds = dialogRef.value.getBoundingClientRect()
  if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) requestClose()
}

watch(isAccountModalOpen, async (open) => {
  form.username = username.value
  form.currentPassword = ''
  form.newPassword = ''
  form.confirmPassword = ''
  errorMessage.value = ''
  if (open) {
    const active = document.activeElement
    returnFocus = active.closest('.user-menu-wrapper')?.querySelector('.user-profile-badge') || active
    await nextTick()
    if (!isAccountModalOpen.value) return
    dialogRef.value.showModal()
    currentPasswordInput.value?.focus()
  } else {
    dialogRef.value?.close()
    const target = returnFocus?.isConnected && returnFocus !== document.body && !returnFocus.closest('dialog')
      ? returnFocus : document.querySelector('.user-profile-badge')
    target?.focus()
    returnFocus = undefined
  }
}, { flush: 'post' })

onBeforeUnmount(() => dialogRef.value?.close())

const handleSave = async () => {
  if (isSaving.value) return
  errorMessage.value = ''
  isSaving.value = true
  try {
    const res = await updateAdminPassword({
      username: form.username,
      currentPassword: form.currentPassword,
      newPassword: form.newPassword,
      confirmPassword: form.confirmPassword
    })
    closeAccountModal()
    success(res.message)
    await router.replace({ name: 'Login' })
  } catch (err) {
    errorMessage.value = err.message
    toastError(errorMessage.value)
  } finally {
    isSaving.value = false
  }
}
</script>

<style scoped>
.account-dialog-card {
  width: min(480px, calc(100vw - 40px)) !important;
  max-width: 480px;
  max-height: calc(100dvh - 40px) !important;
  overflow: auto !important;
  background: var(--card-hover-bg);
  border: 1px solid var(--card-border);
  border-radius: 20px;
  padding: 26px 28px !important;
  box-shadow: 0 25px 60px rgba(0, 0, 0, 0.55);
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.dialog-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
}

.eyebrow {
  font-size: 10px;
  font-weight: 800;
  letter-spacing: 1.2px;
  color: var(--brand-light);
  text-transform: uppercase;
}

.dialog-title {
  font-size: 18px;
  font-weight: 700;
  color: var(--ink);
  margin: 3px 0 0;
}

.dialog-desc {
  font-size: 12px;
  color: var(--muted);
  margin: 4px 0 0;
}

.dialog-close-btn {
  background: transparent;
  border: none;
  color: var(--faint);
  cursor: pointer;
  padding: 4px;
}
.dialog-close-btn:hover {
  color: var(--ink);
}

.error-banner {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  background: rgba(244, 63, 94, 0.12);
  border: 1px solid rgba(244, 63, 94, 0.3);
  border-radius: var(--radius-sm);
  color: var(--danger);
  font-size: 12.5px;
}

.dialog-form {
  padding: 0 !important;
  overflow: visible !important;
  flex: 0 0 auto !important;
  display: flex;
  flex-direction: column;
  gap: 14px !important;
}

.form-group {
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.form-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--ink-secondary);
  display: flex;
  align-items: center;
  gap: 4px;
}

.required-star {
  color: var(--danger);
}

.form-hint {
  font-size: 11px;
  color: var(--faint);
}

.warning-callout {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 9px 12px;
  background: rgba(245, 158, 11, 0.1);
  border: 1px solid rgba(245, 158, 11, 0.25);
  border-radius: var(--radius-sm);
  color: var(--warn);
  font-size: 11.5px;
  line-height: 1.5;
}
.callout-icon {
  flex-shrink: 0;
  margin-top: 1px;
}

.dialog-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 6px;
}

.btn-spinner {
  width: 14px;
  height: 14px;
  border: 2px solid rgba(255, 255, 255, 0.3);
  border-top-color: #fff;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
</style>
