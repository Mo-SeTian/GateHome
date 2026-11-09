<template>
  <transition name="fade">
    <div v-if="isAccountModalOpen" class="modal-backdrop" @click="closeAccountModal">
      <div class="account-dialog-card" @click.stop>
        <div class="dialog-header">
          <div class="dialog-title-group">
            <span class="eyebrow">ADMIN SECURITY</span>
            <h2 class="dialog-title">管理员账户设置</h2>
            <p class="dialog-desc">用于登录 GateHome 管理界面，与反代服务的独立访问账号分开。</p>
          </div>
          <button class="dialog-close-btn" @click="closeAccountModal">
            <X :size="16" />
          </button>
        </div>

        <!-- Inline Error Banner -->
        <transition name="slide-down">
          <div v-if="errorMessage" class="error-banner">
            <AlertCircle :size="15" class="error-icon" />
            <span>{{ errorMessage }}</span>
          </div>
        </transition>

        <form class="dialog-form" @submit.prevent="handleSave">
          <div class="form-group">
            <label class="form-label">
              <span>管理员账号</span>
              <span class="required-star">*</span>
            </label>
            <input 
              v-model="form.username" 
              type="text" 
              class="form-input" 
              maxlength="64" 
              required 
            />
            <small class="form-hint">1–64 字节，区分大小写；支持中文，不含控制字符或首尾空格。</small>
          </div>

          <div class="form-group">
            <label class="form-label">
              <span>当前管理密码</span>
              <span class="required-star">*</span>
            </label>
            <input 
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
            <label class="form-label">
              <span>新管理密码（可选）</span>
            </label>
            <input 
              v-model="form.newPassword" 
              type="password" 
              class="form-input" 
              maxlength="72" 
              placeholder="留空时保持原管理密码" 
            />
            <small class="form-hint">12–72 字节；留空时只修改账号，保持原密码。</small>
          </div>

          <div class="form-group" v-if="form.newPassword">
            <label class="form-label">
              <span>再次输入新管理密码</span>
              <span class="required-star">*</span>
            </label>
            <input 
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
            <button type="button" class="btn btn-secondary" @click="closeAccountModal">
              取消
            </button>
            <button type="submit" class="btn btn-primary" :disabled="isSaving">
              <span v-if="isSaving" class="btn-spinner" />
              <Save v-else :size="15" />
              <span>{{ isSaving ? '保存中...' : '保存管理账户' }}</span>
            </button>
          </div>
        </form>
      </div>
    </div>
  </transition>
</template>

<script setup>
import { reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { X, Save, AlertCircle, AlertTriangle } from 'lucide-vue-next'
import { useAppStore } from '@/composables/useAppStore'
import { useToast } from '@/composables/useToast'

const router = useRouter()
const { isAccountModalOpen, closeAccountModal, adminUser, updateAdminPassword } = useAppStore()
const { success, error: toastError } = useToast()

const form = reactive({
  username: adminUser.username,
  currentPassword: '',
  newPassword: '',
  confirmPassword: ''
})

const isSaving = ref(false)
const errorMessage = ref('')

watch(isAccountModalOpen, (open) => {
  if (open) {
    form.username = adminUser.username
    form.currentPassword = ''
    form.newPassword = ''
    form.confirmPassword = ''
    errorMessage.value = ''
  }
})

const handleSave = async () => {
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
    router.push('/login')
  } catch (err) {
    errorMessage.value = err.message
    toastError(errorMessage.value)
  } finally {
    isSaving.value = false
  }
}
</script>

<style scoped>
.modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.65);
  backdrop-filter: blur(10px);
  z-index: 120;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
}

.account-dialog-card {
  width: 100%;
  max-width: 480px;
  background: var(--modal-bg);
  border: 1px solid var(--modal-border);
  border-radius: 20px;
  padding: 26px 28px;
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
  display: flex;
  flex-direction: column;
  gap: 14px;
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
