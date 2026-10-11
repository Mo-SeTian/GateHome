<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { NButton, NLayout, NLayoutContent, NLayoutSider, NSpace } from 'naive-ui'
import { useAuthStore } from '@/store'
import { AppLoader, RoundCardModal, SvgIcon } from '@/components/common'
import { t } from '@/locales'

interface App {
  name: string
  componentName: string
  icon: string
  auth?: number
}
const props = defineProps<{
  visible: boolean
}>()

const emit = defineEmits<{
  (e: 'update:visible', visible: boolean): void
  (e: 'iconUpdated', item: Panel.ItemInfo): void
}>()

const componentName = ref('UserInfo')
const collapsed = ref(false)
const screenWidth = ref(0)
const isSmallScreen = ref(false)
const defaultTitle = t('appLauncher.title')
const title = ref('')
const height = ref('500px')

const apps = ref<App[]>([
  {
    name: t('apps.userInfo.appName'),
    componentName: 'UserInfo',
    icon: 'material-symbols-person-edit-outline-rounded',
  },
  {
    name: t('apps.baseSettings.appName'),
    componentName: 'Style',
    icon: 'ion-color-palette-outline',
  },
  {
    name: t('apps.itemGroupManage.appName'),
    componentName: 'ItemGroupManage',
    icon: 'material-symbols-ad-group-outline-rounded',
  },
  {
    name: t('apps.iconManage.appName'),
    componentName: 'IconManage',
    icon: 'lucide:wallpaper',
  },
  {
    name: t('apps.uploadsFileManager.appName'),
    componentName: 'UploadFileManager',
    icon: 'tabler:file-upload',
  },
  {
    name: t('apps.exportImport.appName'),
    componentName: 'ImportExport',
    icon: 'icon-park-outline-import-and-export',
  },
  {
    name: t('apps.about.appName'),
    componentName: 'About',
    icon: 'lucide-info',
  },
])

const authStore = useAuthStore()

const show = computed({
  get: () => props.visible,
  set: (visible: boolean) => {
    emit('update:visible', visible)
  },
})

function handleClickApp(item: App) {
  componentName.value = item.componentName
  if (isSmallScreen.value)
    collapsed.value = true
}

function getScreenWidth() {
  return window.innerWidth
}

function handleResize() {
  screenWidth.value = getScreenWidth()
  height.value = `${Math.min(500, Math.max(120, window.innerHeight - 112))}px`
  if (screenWidth.value < 640) {
    collapsed.value = true
    isSmallScreen.value = true
  }
  else {
    collapsed.value = false
    isSmallScreen.value = false
  }
}

onMounted(() => {
  const adminApp: App = {
    name: t('adminSettingUsers.appName'),
    componentName: 'Users',
    icon: 'lucide-users',
    auth: 1,
  }
  // 初始化
  if (authStore.userInfo?.role === 1)
    apps.value.push(adminApp)

  window.addEventListener('resize', handleResize)
  handleResize()
})

onUnmounted(() => {
  window.removeEventListener('resize', handleResize)
})
</script>

<template>
  <div>
    <RoundCardModal
      v-model:show="show"
      style="max-width: 900px;"
      size="small"
    >
      <template #header>
        <div class="flex items-center select-none">
          <NButton quaternary circle :aria-label="collapsed ? '展开应用菜单' : '收起应用菜单'" :aria-expanded="!collapsed" @click="collapsed = !collapsed">
            <SvgIcon class="text-2xl transition-all duration-500" :icon="collapsed ? 'tabler-layout-sidebar-right-collapse-filled' : 'tabler-layout-sidebar-left-collapse-filled'" />
          </NButton>
          <div class="ml-1">
            {{ title === '' ? defaultTitle : title }}
          </div>
        </div>
      </template>
      <div class="w-full h-full app-starter-modal-content">
        <NSpace vertical size="large" style="height: 100%;width: 100%;">
          <NLayout has-sider style="border-radius:0.75rem;">
            <NLayoutSider
              v-model:collapsed="collapsed"
              collapse-mode="width"
              :collapsed-width="0"
              :width="isSmallScreen ? '100%' : 240"
              style="height: 100%;"
              content-style="overflow: hidden"
            >
              <div class="w-full h-full dark:bg-[#2c2c32]">
                <div
                  class="p-[5px] bg-slate-200 dark:bg-zinc-900 rounded-xl overflow-auto"
                  :style="{
                    width: isSmallScreen ? '100%' : '220px',
                    minWidth: '200px',
                    height,
                  }"
                >
                  <div
                    v-for=" (item, index) in apps"
                    :key="index"
                    role="button"
                    tabindex="0"
                    :aria-pressed="componentName === item.componentName"
                    :style="{ color: componentName === item.componentName ? 'var(--n-color-target)' : '' }"
                    @click="handleClickApp(item)"
                    @keydown.enter="handleClickApp(item)"
                    @keydown.space.prevent="handleClickApp(item)"
                  >
                    <div
                      class="bg-white dark:bg-zinc-800 p-[10px] rounded-lg mb-[5px] font-bold cursor-pointer flex items-center hover:bg-slate-50 focus:bg-slate-50"
                    >
                      <div class="flex items-center justify-center">
                        <div class="text-lg">
                          <SvgIcon :icon="item.icon" />
                        </div>
                        <span class="ml-2">{{ item.name }}</span>
                      </div>
                    <!-- 更多按钮 -->
                    <!-- <div class="ml-auto">
                      <SvgIcon icon="mingcute-more-1-fill" />
                    </div> -->
                    </div>
                  </div>
                </div>
              </div>
            </NLayoutSider>
            <NLayoutContent v-show="!isSmallScreen || collapsed" :content-style="{ height }">
              <div class="rounded-2xl h-full overflow-auto transition-all duration-500 min-w-0">
                <AppLoader :component-name="componentName" class="h-full" @icon-updated="emit('iconUpdated', $event)" />
              </div>
            </NLayoutContent>
          </NLayout>
        </NSpace>
      </div>
    </RoundCardModal>
  </div>
</template>

<style scoped>
.text-shadow {
  text-shadow: 0px 0px 5px gray;
}
</style>

<style>
.dark .app-starter-modal-content .n-layout{
    background-color: #2c2c32;
}
</style>
