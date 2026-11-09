import { ref } from 'vue'

// Global UI State
const theme = ref(localStorage.getItem('gh_theme') || 'dark')
const isSidebarOpen = ref(false)
const isCmdkOpen = ref(false)

export function useAppStore() {
  const toggleTheme = () => {
    theme.value = theme.value === 'dark' ? 'light' : 'dark'
    localStorage.setItem('gh_theme', theme.value)
    document.body.className = `theme-${theme.value}`
  }

  const setTheme = (val) => {
    theme.value = val
    localStorage.setItem('gh_theme', val)
    document.body.className = `theme-${val}`
  }

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
