import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { usePasskey } from '@/api/passkeys'
import { adminApi } from '@/api/admin'
import { markSessionChanged } from '@/api/client'
import { useAssistant } from './useAssistant'
import { useToast } from './useToast'

try {
  localStorage.removeItem('gh_auth')
  localStorage.removeItem('gh_user')
} catch {
  // Ignore storage restrictions.
}

const username = ref('')
const isAuthenticated = computed(() => Boolean(username.value))
const isAccountModalOpen = ref(false)
const isLoggingOut = ref(false)
let sessionChecked = false
let sessionCheck

function setSession(value) {
  markSessionChanged()
  sessionChecked = true
  username.value = value
}

function endSession() {
  setSession('')
  isAccountModalOpen.value = false
}

function restoreSession() {
  if (sessionChecked) return
  sessionCheck ??= adminApi.account().then(account => setSession(account.username)).catch(err => {
    if (err.status !== 401) throw err
    endSession()
  }).finally(() => { sessionCheck = undefined })
  return sessionCheck
}

async function login(name, password) {
  await adminApi.login(name, password)
  await adoptLogin()
}

async function loginWithPasskey(signal) {
  await usePasskey('login', signal)
  await adoptLogin()
}

async function adoptLogin() {
  const account = await adminApi.account()
  setSession(account.username)
}

async function updateAdminPassword({ username, currentPassword, newPassword, confirmPassword }) {
  const result = await adminApi.updateAccount({
    username: username.trim(), current_password: currentPassword,
    new_password: newPassword, confirm_password: confirmPassword
  })
  endSession()
  return result
}

export function useSession() {
  return {
    username, isAuthenticated, isAccountModalOpen, isLoggingOut,
    restoreSession, login, loginWithPasskey, endSession, updateAdminPassword,
    openAccountModal: () => { isAccountModalOpen.value = true },
    closeAccountModal: () => { isAccountModalOpen.value = false }
  }
}

export function useLogout() {
  const router = useRouter()
  const toast = useToast()
  const { resetAssistant } = useAssistant()

  return async () => {
    if (isLoggingOut.value) return
    isLoggingOut.value = true
    try {
      await adminApi.logout().catch(err => {
        if (err.status !== 401) throw err
      })
      resetAssistant({ clearStorage: true })
      endSession()
      await router.replace({ name: 'Login' })
      toast.info('已退出管理会话')
    } catch (err) {
      toast.error(err.message)
    } finally {
      isLoggingOut.value = false
    }
  }
}
