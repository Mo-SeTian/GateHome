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
          <div v-if="errorMessage" class="error-banner">
            <AlertCircle :size="15" class="error-icon" />
            <span>{{ errorMessage }}</span>
          </div>
        </transition>

        <!-- Login Form -->
        <form class="login-form" @submit.prevent="handleSubmit">
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
                placeholder="admin" 
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
                @click="showForgotModal = true"
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
                :title="showPassword ? '隐藏密码' : '显示密码'"
                @click="showPassword = !showPassword"
              >
                <EyeOff v-if="showPassword" :size="15" />
                <Eye v-else :size="15" />
              </button>
            </div>
          </div>

          <div class="form-options">
            <label class="checkbox-label">
              <input type="checkbox" v-model="rememberMe" />
              <span class="custom-checkbox">
                <Check v-if="rememberMe" :size="12" />
              </span>
              <span class="remember-text">保持 30 天管理会话有效</span>
            </label>
          </div>

          <!-- Submit Buttons -->
          <div class="actions-group">
            <button 
              type="submit" 
              class="btn btn-primary submit-btn" 
              :disabled="isLoading"
            >
              <span v-if="isLoading" class="btn-spinner" />
              <LogIn v-else :size="16" />
              <span>{{ isLoading ? '身份凭据校验中...' : '进入控制台' }}</span>
            </button>

            <!-- Quick Demo Bypass Button -->
            <button 
              type="button" 
              class="btn demo-quick-btn" 
              :disabled="isLoading"
              @click="handleDemoLogin"
            >
              <Zap :size="14" class="zap-icon" />
              <span>一键快捷免密进入 (演示模式)</span>
            </button>
          </div>
        </form>

        <!-- Official GateHome Helper Note -->
        <div class="login-note-box">
          <p class="login-note">
            这里使用管理账户，与反代服务的访问账号分开。忘记管理账户？在服务器终端运行 
            <code class="code-pill">gatehouse init</code>，使用原配置目录重置并重启服务。
          </p>
        </div>

        <div class="login-footer">
          <span>GateHome · {{ settings.currentVersion }} · Aurora Next Console</span>
        </div>
      </SpotlightCard>
    </div>

    <!-- Forgot Password Modal -->
    <transition name="fade">
      <div v-if="showForgotModal" class="modal-backdrop" @click="showForgotModal = false">
        <div class="modal-box" @click.stop>
          <div class="modal-header">
            <div class="modal-title-row">
              <KeyRound :size="18" class="modal-icon" />
              <h3>如何重置管理员账户密码？</h3>
            </div>
            <button class="modal-close-btn" @click="showForgotModal = false">
              <X :size="16" />
            </button>
          </div>
          <div class="modal-body">
            <p>为保障家庭网关的最高安全级别，GateHome 默认不提供未经授权的公网重置通道。</p>
            <div class="step-card">
              <div class="step-num">1</div>
              <div class="step-text">通过 SSH 或终端登入运行 GateHome 的物理宿主机 / NAS。</div>
            </div>
            <div class="step-card">
              <div class="step-num">2</div>
              <div class="step-text">
                执行以下初始化命令，沿用原配置目录重置账号密码并重启服务：
                <div class="code-terminal">
                  <code>gatehouse init --config /etc/gatehouse</code>
                </div>
              </div>
            </div>
            <div class="step-card">
              <div class="step-num">3</div>
              <div class="step-text">重置完成后，在此页面使用新设定的管理员凭据登录。</div>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-primary btn-sm" @click="showForgotModal = false">
              我知道了
            </button>
          </div>
        </div>
      </div>
    </transition>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { 
  User, Lock, Eye, EyeOff, LogIn, AlertCircle, 
  Zap, Check, Sun, Moon, KeyRound, X 
} from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { useAppStore } from '@/composables/useAppStore'
import { useToast } from '@/composables/useToast'

const router = useRouter()
const route = useRoute()
const { theme, toggleTheme, login, settings } = useAppStore()
const { success, error: toastError } = useToast()

const username = ref('admin')
const password = ref('')
const showPassword = ref(false)
const rememberMe = ref(true)
const isLoading = ref(false)
const errorMessage = ref('')
const showForgotModal = ref(false)

const finishLogin = (loggedUser) => {
  success(`欢迎回来，${loggedUser}！已进入控制中心`)
  const redirect = route.query.redirect || '/overview'
  router.push(redirect)
}

const handleSubmit = async () => {
  errorMessage.value = ''
  isLoading.value = true
  try {
    const res = await login(username.value, password.value)
    finishLogin(res.username)
  } catch (err) {
    errorMessage.value = err.message || '账号或管理密码错误'
    toastError(errorMessage.value)
  } finally {
    isLoading.value = false
  }
}

const handleDemoLogin = async () => {
  username.value = 'admin'
  errorMessage.value = ''
  isLoading.value = true
  try {
    const res = await login('admin', 'DEMO_PASSWORD')
    finishLogin(res.username)
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
  color: var(--danger);
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
  width: 100%;
  padding-left: 36px;
  padding-right: 36px;
  height: 40px;
  box-sizing: border-box;
  font-size: 13.5px;
}

.eye-toggle-btn {
  position: absolute;
  right: 12px;
  color: var(--faint);
  background: transparent;
  border: none;
  cursor: pointer;
  padding: 4px;
  display: flex;
  align-items: center;
}
.eye-toggle-btn:hover {
  color: var(--ink);
}

/* Checkbox */
.form-options {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: -2px;
}

.checkbox-label {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  font-size: 12px;
  color: var(--muted);
  user-select: none;
}

.checkbox-label input[type="checkbox"] {
  display: none;
}

.custom-checkbox {
  width: 16px;
  height: 16px;
  border-radius: 4px;
  border: 1px solid var(--line-bold);
  background: var(--input-bg);
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  transition: var(--transition-smooth);
}

.checkbox-label input[type="checkbox"]:checked + .custom-checkbox {
  background: var(--brand);
  border-color: var(--brand);
}

/* Buttons */
.actions-group {
  display: flex;
  flex-direction: column;
  gap: 9px;
  margin-top: 6px;
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

.demo-quick-btn {
  width: 100%;
  height: 38px;
  background: var(--strip-bg);
  border: 1px dashed var(--line-bold);
  color: var(--muted);
  font-size: 12.5px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border-radius: var(--radius-sm);
  transition: var(--transition-smooth);
}
.demo-quick-btn:hover {
  background: var(--card-hover-bg);
  border-color: var(--brand-light);
  color: var(--ink);
}

.zap-icon {
  color: var(--warn);
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

.code-pill {
  font-family: var(--mono);
  background: var(--input-bg);
  border: 1px solid var(--line);
  padding: 1px 4px;
  border-radius: 3px;
  color: var(--ink);
}

.login-footer {
  text-align: center;
  font-size: 11px;
  color: var(--faint);
  padding-top: 4px;
}

/* Modal */
.modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.65);
  backdrop-filter: blur(8px);
  z-index: 100;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
}

.modal-box {
  width: 100%;
  max-width: 480px;
  background: var(--modal-bg);
  border: 1px solid var(--modal-border);
  border-radius: 18px;
  padding: 24px;
  box-shadow: 0 25px 60px rgba(0, 0, 0, 0.5);
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.modal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

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
