import { ref, reactive } from 'vue'

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

  const backups = ref([
    { id: 'bk_1', name: 'gatehouse_backup_20261109_0300.zip', size: '14.2 MB', type: 'AUTO', storage: 'Local + WebDAV', createdAt: '2026-11-09 03:00' },
    { id: 'bk_2', name: 'gatehouse_pre_upgrade_v0.0.67.zip', size: '13.9 MB', type: 'MANUAL', storage: 'Local', createdAt: '2026-11-08 19:40' },
    { id: 'bk_3', name: 'gatehouse_backup_20261102_0300.zip', size: '13.6 MB', type: 'AUTO', storage: 'Local + WebDAV', createdAt: '2026-11-02 03:00' }
  ])

  const settings = reactive({
    autoBackupEnabled: true,
    webdavUrl: 'https://dav.jianguoyun.com/dav/gatehome/',
    currentVersion: `v${__GATEHOME_VERSION__}`,
    channel: 'stable' // 'stable' | 'beta'
  })

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
    closeCmdk,
    backups,
    settings
  }
}
