import { computed, ref, watchEffect } from 'vue'

// Global UI State
function safeGetTheme() {
  try {
    return localStorage.getItem('gh_theme')
  } catch {
    return null
  }
}

const media = window.matchMedia('(prefers-color-scheme: dark)')
const systemDark = ref(media.matches)
const savedTheme = safeGetTheme()
const themePreference = ref(['dark', 'light'].includes(savedTheme) ? savedTheme : 'system')
const theme = computed(() => themePreference.value === 'system'
  ? (systemDark.value ? 'dark' : 'light') : themePreference.value)
media.addEventListener('change', event => { systemDark.value = event.matches })
watchEffect(() => {
  document.body.classList.toggle('theme-dark', theme.value === 'dark')
  document.body.classList.toggle('theme-light', theme.value === 'light')
})
const isSidebarOpen = ref(false)
const isCmdkOpen = ref(false)

export function useAppStore() {
  const setTheme = value => {
    themePreference.value = value
    try {
      if (value === 'system') localStorage.removeItem('gh_theme')
      else localStorage.setItem('gh_theme', value)
    } catch {
      // Ignore storage failures in restricted browser environments.
    }
  }
  const toggleTheme = () => setTheme(theme.value === 'dark' ? 'light' : 'dark')

  const toggleSidebar = () => {
    isSidebarOpen.value = !isSidebarOpen.value
  }

  const closeSidebar = () => {
    isSidebarOpen.value = false
  }

  const toggleCmdk = () => {
    isCmdkOpen.value = !isCmdkOpen.value
  }

  const openCmdk = () => {
    isCmdkOpen.value = true
  }

  const closeCmdk = () => {
    isCmdkOpen.value = false
  }

  return {
    theme,
    themePreference,
    toggleTheme,
    setTheme,
    isSidebarOpen,
    toggleSidebar,
    closeSidebar,
    isCmdkOpen,
    toggleCmdk,
    openCmdk,
    closeCmdk
  }
}
