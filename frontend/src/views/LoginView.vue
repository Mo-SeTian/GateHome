<template>
  <div class="login-wrapper">
    <!-- Ambient Aurora Glow Orbs -->
    <div class="glow-orb orb-1" />
    <div class="glow-orb orb-2" />
    <div class="glow-orb orb-3" />

    <!-- Top Floating Theme Switcher -->
    <div class="top-theme-bar">
      <button 
        class="theme-pill-btn" 
        :title="theme === 'dark' ? '切换为浅色模式' : '切换为深色模式'" 
        @click="toggleTheme"
      >
        <Sun v-if="theme === 'dark'" :size="15" />
        <Moon v-else :size="15" />
        <span>{{ theme === 'dark' ? '深色极客黑' : '清透晨曦白' }}</span>
      </button>
    </div>

    <!-- Login Centered Shell -->
    <div class="login-container">
      <!-- Brand Art Header -->
      <div class="login-art">
        <div class="brand-cube-icon">
          <svg viewBox="0 0 32 32" width="30" height="30">
            <defs>
              <linearGradient id="login-brand-g" x1="0%" y1="0%" x2="100%" y2="100%">
                <stop offset="0%" stop-color="#6366f1" />
                <stop offset="50%" stop-color="#3b82f6" />
                <stop offset="100%" stop-color="#06b6d4" />
              </linearGradient>
            </defs>
            <path d="M16 4L4 10.5v11L16 28l12-6.5v-11L16 4zm0 3.5l8.5 4.6-8.5 4.7-8.5-4.7L16 7.5zM6.5 19.8V12l8 4.4v7.8l-8-4.4zm10.5 4.4v-7.8l8-4.4v7.8l-8 4.4z" fill="url(#login-brand-g)" />
          </svg>
        </div>
        <h1 class="brand-name">GateHome</h1>
        <p class="brand-desc">家庭网络管理控制台</p>
      </div>

      <!-- Spotlight Frosted Login Card -->
      <SpotlightCard class="login-card">
        <div class="card-header">
          <div class="eyebrow-pill">GATEHOUSE CONSOLE</div>
          <h2 class="card-title">欢迎回来</h2>
          <p class="card-subtitle">使用 GateHome 管理员账号和管理密码登录</p>
        </div>

        <!-- Inline Error Banner -->
        <transition name="slide-down">
          <div v-if="errorMessage" class="error-banner" role="alert">
            <AlertCircle :size="15" class="error-icon" />
            <span>{{ errorMessage }}</span>
          </div>
        </transition>

        <!-- Login Form -->
        <form class="login-form" :aria-busy="isLoading" @submit.prevent="handleSubmit">
          <div class="form-group">
            <label class="form-label" for="admin-username">
              <span>管理员账号</span>
              <span class="required-star">*</span>
            </label>
            <div class="input-with-icon">
              <User :size="16" class="field-icon" />
              <input 
                id="admin-username"
                v-model="username" 
                type="text" 
                class="form-input" 
                placeholder="请输入管理员账号"
                maxlength="64"
                autocomplete="username"
                required 
                autofocus
              />
            </div>
          </div>

          <div class="form-group">
            <div class="label-with-action">
              <label class="form-label" for="admin-password">
                <span>管理密码</span>
                <span class="required-star">*</span>
              </label>
              <button 
                type="button" 
                class="field-help-btn"
                ref="forgotTrigger"
                @click="forgotDialog.showModal()"
              >
                忘记密码？
              </button>
            </div>
            <div class="input-with-icon">
              <Lock :size="16" class="field-icon" />
              <input 
                id="admin-password"
                v-model="password" 
                :type="showPassword ? 'text' : 'password'" 
                class="form-input" 
                placeholder="请输入管理密码" 
                maxlength="72"
                autocomplete="current-password"
                required 
              />
              <button 
                type="button" 
                class="eye-toggle-btn" 
                :aria-label="showPassword ? '隐藏密码' : '显示密码'"
                @click="showPassword = !showPassword"
              >
                <EyeOff v-if="showPassword" :size="15" />
                <Eye v-else :size="15" />
              </button>
            </div>
          </div>

          <button type="submit" class="btn btn-primary submit-btn" :disabled="isLoading">
            <span v-if="isLoading" class="btn-spinner" />
            <LogIn v-else :size="16" />
            <span>{{ isLoading ? '身份凭据校验中...' : '进入控制台' }}</span>
          </button>
        </form>

        <!-- Official GateHome Helper Note -->
        <div class="login-note-box">
          <p class="login-note">
            请使用网关管理账户。管理会话有效期为 12 小时。
          </p>
        </div>

        <div class="login-footer">
          <span>GateHome · {{ gatewayVersion }} · Aurora Next Console</span>
        </div>
      </SpotlightCard>
    </div>

    <dialog ref="forgotDialog" class="modal-box" aria-labelledby="forgot-title" @close="forgotTrigger?.focus()">
      <div class="modal-header">
        <div class="modal-title-row">
          <KeyRound :size="18" class="modal-icon" />
          <h3 id="forgot-title">如何重置管理员账户密码？</h3>
        </div>
        <button type="button" class="modal-close-btn" aria-label="关闭" autofocus @click="forgotDialog.close()">
          <X :size="16" />
        </button>
      </div>
      <div class="modal-body">
        <div class="step-card">
          <div class="step-num">1</div>
          <div class="step-text">登录服务器终端并停止 GateHome 服务。</div>
        </div>
        <div class="step-card">
          <div class="step-num">2</div>
          <div class="step-text">
            沿用现有启动参数，将 <code>init</code> 放在参数末尾。以下为独立部署的命令格式。
            <div class="code-terminal">
              <code>gatehouse -config &lt;配置目录&gt; -log &lt;日志目录&gt; -data &lt;数据目录&gt; init</code>
            </div>
            Docker Compose 部署请沿用原服务挂载，执行 <code>docker compose run --rm gatehouse init</code>。
          </div>
        </div>
        <div class="step-card">
          <div class="step-num">3</div>
          <div class="step-text">启动服务并使用新的管理员凭据登录。</div>
        </div>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-primary btn-sm" @click="forgotDialog.close()">我知道了</button>
      </div>
    </dialog>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { User, Lock, Eye, EyeOff, LogIn, AlertCircle, Sun, Moon, KeyRound, X } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { useAppStore } from '@/composables/useAppStore'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useSession } from '@/composables/useSession'
import { useToast } from '@/composables/useToast'
import { postLoginPath } from '@/router'

const router = useRouter()
const route = useRoute()
const { theme, toggleTheme } = useAppStore()
const { login } = useSession()
const { gatewayVersion } = useGatewayStatus()
const { success } = useToast()

const username = ref('')
const password = ref('')
const showPassword = ref(false)
const isLoading = ref(false)
const errorMessage = ref('')
const forgotDialog = ref(null)
const forgotTrigger = ref(null)

async function handleSubmit() {
  if (isLoading.value) return
  errorMessage.value = ''
  if (!username.value.trim()) {
    errorMessage.value = '请输入管理员账号'
    return
  }

  isLoading.value = true
  try {
    await login(username.value.trim(), password.value)
    password.value = ''
    success('已登录 GateHome')
    await router.replace(postLoginPath(route.query.redirect))
  } catch (err) {
    errorMessage.value = err.message
  } finally {
    isLoading.value = false
  }
}
</script>

<style scoped>
.login-wrapper {
  min-height: 100vh;
  width: 100vw;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  background: var(--canvas);
  position: relative;
  overflow-x: hidden;
  padding: 32px 16px;
  box-sizing: border-box;
}

/* Ambient Floating Glow Orbs */
.glow-orb {
  position: absolute;
  border-radius: 50%;
  filter: blur(140px);
  pointer-events: none;
  opacity: 0.22;
  transition: all 1s ease;
}
.orb-1 {
  width: 520px;
  height: 520px;
  background: #6366f1;
  top: -120px;
  left: 15%;
  animation: floatOrb 18s ease-in-out infinite alternate;
}
.orb-2 {
  width: 480px;
  height: 480px;
  background: #06b6d4;
  bottom: -100px;
  right: 15%;
  animation: floatOrb 22s ease-in-out infinite alternate-reverse;
}
.orb-3 {
  width: 340px;
  height: 340px;
  background: #3b82f6;
  top: 40%;
  left: 55%;
  opacity: 0.14;
}

@keyframes floatOrb {
  0% { transform: translate(0, 0) scale(1); }
  50% { transform: translate(30px, 40px) scale(1.08); }
  100% { transform: translate(-25px, 20px) scale(0.95); }
}

/* Top Theme Pill */
.top-theme-bar {
  position: absolute;
  top: 24px;
  right: 28px;
  z-index: 30;
}

.theme-pill-btn {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 14px;
  background: var(--card-bg);
  backdrop-filter: blur(20px);
  border: 1px solid var(--card-border);
  border-radius: var(--radius-pill);
  color: var(--muted);
  font-size: 12px;
  cursor: pointer;
  transition: var(--transition-smooth);
}
.theme-pill-btn:hover {
  background: var(--card-hover-bg);
  border-color: var(--card-border-hover);
  color: var(--ink);
}

/* Login Center Container */
.login-container {
  width: 100%;
  max-width: 420px;
  position: relative;
  z-index: 10;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

/* Brand Art */
.login-art {
  text-align: center;
  display: flex;
  flex-direction: column;
  align-items: center;
}

.brand-cube-icon {
  width: 52px;
  height: 52px;
  border-radius: 16px;
  background: rgba(99, 102, 241, 0.12);
  border: 1px solid rgba(99, 102, 241, 0.3);
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 12px;
  box-shadow: 0 8px 24px rgba(99, 102, 241, 0.15);
}

.brand-name {
  font-size: 24px;
  font-weight: 800;
  letter-spacing: -0.5px;
  color: var(--ink);
  margin: 0;
}

.brand-desc {
  font-size: 13px;
  color: var(--muted);
  margin-top: 4px;
  font-weight: 500;
}

/* Spotlight Card */
.login-card {
  padding: 30px 28px 24px;
  display: flex;
  flex-direction: column;
  gap: 20px;
  box-shadow: 0 20px 50px rgba(0, 0, 0, 0.35);
  border-radius: 20px;
}

.card-header {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.eyebrow-pill {
  font-size: 10px;
  font-weight: 800;
  letter-spacing: 1.2px;
  color: var(--brand-light);
  text-transform: uppercase;
}

.card-title {
  font-size: 21px;
  font-weight: 700;
  color: var(--ink);
  margin: 2px 0 0;
}

.card-subtitle {
  font-size: 12.5px;
  color: var(--muted);
  margin: 0;
}

/* Error Banner */
.error-banner {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 9px 12px;
  background: rgba(244, 63, 94, 0.12);
  border: 1px solid rgba(244, 63, 94, 0.3);
  border-radius: var(--radius-sm);
  color: var(--ink);
  font-size: 12.5px;
}
.error-icon {
  flex-shrink: 0;
}

/* Form */
.login-form {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.form-group {
  display: flex;
  flex-direction: column;
  gap: 6px;
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

.label-with-action {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.field-help-btn {
  font-size: 11.5px;
  color: var(--brand-light);
  background: transparent;
  border: none;
  cursor: pointer;
  padding: 0;
}
.field-help-btn:hover {
  text-decoration: underline;
}

.input-with-icon {
  position: relative;
  display: flex;
  align-items: center;
}

.field-icon {
  position: absolute;
  left: 12px;
  color: var(--faint);
  pointer-events: none;
}

.input-with-icon .form-input {
  width: 100% !important;
  padding-left: 38px !important;
  padding-right: 42px !important;
  height: 44px !important;
  line-height: 42px !important;
  box-sizing: border-box !important;
  font-size: 13.5px !important;
}

.eye-toggle-btn {
  position: absolute;
  right: 0;
  color: var(--faint);
  background: transparent;
  border: none;
  cursor: pointer;
  width: 44px;
  height: 44px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.eye-toggle-btn:hover {
  color: var(--ink);
}

.submit-btn {
  width: 100%;
  height: 42px;
  font-size: 14px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  border-radius: var(--radius-sm);
  box-shadow: 0 4px 18px rgba(99, 102, 241, 0.35);
}

.btn-spinner {
  width: 15px;
  height: 15px;
  border: 2px solid rgba(255, 255, 255, 0.3);
  border-top-color: #fff;
  border-radius: 50%;
  animation: spin 0.75s linear infinite;
}

/* Official Note */
.login-note-box {
  background: var(--strip-bg);
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  padding: 10px 12px;
}

.login-note {
  font-size: 11px;
  line-height: 1.55;
  color: var(--faint);
  margin: 0;
}

.login-footer {
  text-align: center;
  font-size: 11px;
  color: var(--faint);
  padding-top: 4px;
}

/* Modal */
.modal-title-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.modal-title-row h3 {
  font-size: 16px;
  font-weight: 700;
  color: var(--ink);
  margin: 0;
}

.modal-icon {
  color: var(--brand-light);
}

.modal-close-btn {
  background: transparent;
  border: none;
  color: var(--faint);
  cursor: pointer;
  padding: 4px;
}
.modal-close-btn:hover {
  color: var(--ink);
}

.modal-body {
  font-size: 13px;
  color: var(--muted);
  line-height: 1.6;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.modal-body p {
  margin: 0;
}

.step-card {
  display: flex;
  gap: 12px;
  padding: 10px 12px;
  background: var(--strip-bg);
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
}

.step-num {
  width: 20px;
  height: 20px;
  border-radius: 50%;
  background: var(--brand);
  color: #fff;
  font-size: 11px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.step-text {
  flex: 1;
}

.code-terminal {
  margin-top: 6px;
  padding: 8px 10px;
  background: #000;
  border-radius: 6px;
  color: #10b981;
  font-family: var(--mono);
  font-size: 12px;
}

.modal-footer {
  display: flex;
  justify-content: flex-end;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.slide-down-enter-active,
.slide-down-leave-active {
  transition: all 0.2s ease;
}
.slide-down-enter-from,
.slide-down-leave-to {
  opacity: 0;
  transform: translateY(-6px);
}
</style>
