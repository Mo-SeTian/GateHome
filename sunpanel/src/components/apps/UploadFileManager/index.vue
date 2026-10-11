<script setup lang="ts">
import { NAlert, NButton, NButtonGroup, NCard, NEllipsis, NGrid, NGridItem, NImage, NImageGroup, NInput, NModal, NPagination, NSelect, NSpin, useDialog, useMessage } from 'naive-ui'
import { computed, onMounted, ref, watch } from 'vue'
import { deletePreview, deletes, getList } from '@/api/system/file'
import { set as savePanelConfig } from '@/api/panel/userConfig'
import { RoundCardModal, SvgIcon } from '@/components/common'
import { copyToClipboard, timeFormat } from '@/utils/cmn'
import { t } from '@/locales'
import { usePanelState } from '@/store'

interface InfoModalState {
  title: string
  show: boolean
  fileInfo: File.Info | null
}
const ms = useMessage()
const loading = ref(false)
const imageList = ref<File.Info[]>([])
const keyword = ref('')
const usage = ref('all')
const page = ref(1)
const cleaning = ref(false)
const previewShow = ref(false)
const preview = ref<{ list: File.Info[]; bytes: number }>({ list: [], bytes: 0 })
const errorText = ref('')
const usageOptions = [{ label: '全部图片', value: 'all' }, { label: '正在使用', value: 'used' }, { label: '未使用', value: 'unused' }]
const filtered = computed(() => imageList.value.filter(file => (!keyword.value.trim() || file.fileName.toLocaleLowerCase().includes(keyword.value.trim().toLocaleLowerCase())) && (usage.value === 'all' || (usage.value === 'used' ? !!file.references?.length : !file.references?.length))))
const visibleImages = computed(() => filtered.value.slice((page.value - 1) * 24, page.value * 24))
watch([keyword, usage], () => {
  page.value = 1
})
async function previewCleanup() {
  loading.value = true
  errorText.value = ''
  try {
    const result = await deletePreview()
    if (result.code !== 0)
      throw new Error(result.msg || '清理预览失败')
    preview.value = result.data
    previewShow.value = true
  }
  catch (error) { errorText.value = error instanceof Error ? error.message : '清理预览失败' }
  finally { loading.value = false }
}
async function cleanUnused() {
  if (cleaning.value)
    return
  cleaning.value = true
  errorText.value = ''
  try {
    const ids = preview.value.list.map(file => file.id as number)
    for (let index = 0; index < ids.length; index += 100) {
      const result = await deletes(ids.slice(index, index + 100))
      if (result.code !== 0)
        throw new Error(result.msg || '清理失败')
    }
    previewShow.value = false
    ms.success('未使用图片已清理')
  }
  catch (error) { errorText.value = error instanceof Error ? error.message : '清理失败，请重新预览' }
  finally {
    cleaning.value = false
    await getFileList()
    if (previewShow.value) {
      try {
        const refreshed = await deletePreview()
        if (refreshed.code === 0) {
          const requested = new Set(preview.value.list.map(file => file.id))
          const remaining = refreshed.data.list.filter(file => requested.has(file.id))
          preview.value = { list: remaining, bytes: remaining.reduce((sum, file) => sum + (file.size || 0), 0) }
        }
      }
      catch { /* Keep the original preview and error until the user refreshes. */ }
    }
  }
}
const dialog = useDialog()
const panelStore = usePanelState()
const infoModalState = ref<InfoModalState>({
  show: false,
  title: '',
  fileInfo: null,
})

async function getFileList() {
  loading.value = true
  try {
    const result = await getList<Common.ListResponse<File.Info[]>>()
    if (result.code !== 0)
      throw new Error(result.msg || '读取文件失败')
    imageList.value = result.data.list
    page.value = Math.min(page.value, Math.max(1, Math.ceil(filtered.value.length / 24)))
  }
  catch (error) { errorText.value = error instanceof Error ? error.message : '读取文件失败' }
  finally { loading.value = false }
}

async function copyImageUrl(text: string) {
  const res = await copyToClipboard(text)
  if (res)
    ms.success(t('apps.uploadsFileManager.copySuccess'))

  else
    ms.error(t('apps.uploadsFileManager.copyFailed'))
}

function handleDelete(id: number) {
  if (cleaning.value || loading.value)
    return
  dialog.warning({
    title: t('common.warning'),
    content: t('apps.uploadsFileManager.deleteWarningText'),
    positiveText: t('common.confirm'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => {
      deletesImges(id)
    },
  })
}

async function deletesImges(id: number) {
  try {
    const { code, msg } = await deletes([id])
    if (code === 0) {
      getFileList()
      ms.success(t('common.success'))
    }
    else {
      ms.error(`${t('common.failed')}:${msg}`)
    }
  }
  catch (error) {
    ms.error(t('common.failed'))
  }
}

function handleInfoClick(fileInfo: File.Info) {
  infoModalState.value.fileInfo = fileInfo
  infoModalState.value.show = true
}

function handleSetWallpaper(imgSrc: string) {
  panelStore.panelConfig.backgroundImageSrc = imgSrc
  savePanelConfig({ panel: panelStore.panelConfig })
}

onMounted(() => {
  getFileList()
})
</script>

<template>
  <div class="asset-manager bg-slate-200 dark:bg-zinc-900 p-3 h-full overflow-auto">
    <div class="asset-toolbar mb-3">
      <NInput v-model:value="keyword" clearable placeholder="搜索文件名称" aria-label="搜索文件名称" />
      <NSelect v-model:value="usage" :options="usageOptions" aria-label="筛选图片引用" />
      <NButton :disabled="loading || cleaning" @click="previewCleanup">
        清理未使用图片
      </NButton>
    </div>
    <NAlert v-if="errorText" type="error" class="mb-3">
      {{ errorText }}
    </NAlert>
    <NSpin v-show="loading" size="small" />
    <NAlert type="info" :bordered="false">
      {{ $t('apps.uploadsFileManager.alertText') }}
    </NAlert>
    <div class="flex justify-center mt-2">
      <div v-if="filtered.length === 0 && !loading" class="flex">
        {{ $t('apps.uploadsFileManager.nothingText') }}
      </div>
      <NImageGroup v-else>
        <NGrid cols="1 420:2 720:3 1080:4" :x-gap="5" :y-gap="5">
          <NGridItem v-for="item in visibleImages" :key="item.id">
            <NCard size="small" style="border-radius: 5px;" :bordered="true">
              <template #cover>
                <div class="card transparent-grid">
                  <NImage :lazy="true" style="object-fit: contain;height: 100%;" :src="item.src" />
                </div>
              </template>
              <template #footer>
                <span class="text-xs">
                  <NEllipsis>
                    {{ item.fileName }}
                  </NEllipsis>
                </span>
                <div class="text-xs mt-1">
                  {{ item.references?.length ? `使用中 · ${item.references.length} 处` : '未使用' }} · {{ ((item.size || 0) / 1024).toFixed(1) }} KiB
                </div>
                <div class="flex justify-center mt-[10px]">
                  <NButtonGroup>
                    <NButton size="tiny" tertiary style="cursor: pointer;" :title="$t('apps.uploadsFileManager.copyLink')" @click="copyImageUrl(item.src)">
                      <template #icon>
                        <SvgIcon icon="ion-copy" />
                      </template>
                    </NButton>
                    <NButton size="tiny" tertiary style="cursor: pointer;" :title="timeFormat(item.createTime)" @click="handleInfoClick(item)">
                      <template #icon>
                        <SvgIcon icon="mdi-information-box-outline" />
                      </template>
                    </NButton>
                    <NButton size="tiny" tertiary style="cursor: pointer;" :title="$t('apps.uploadsFileManager.setWallpaper')" @click="handleSetWallpaper(item.src)">
                      <template #icon>
                        <SvgIcon icon="lucide:wallpaper" />
                      </template>
                    </NButton>
                    <NButton :disabled="!!item.references?.length || cleaning || loading" size="tiny" tertiary type="error" style="cursor: pointer;" :title="$t('common.delete')" @click="handleDelete(item.id as number)">
                      <template #icon>
                        <SvgIcon icon="material-symbols-delete" />
                      </template>
                    </NButton>
                  </NButtonGroup>
                </div>
              </template>
            </NCard>
          </NGridItem>
        </NGrid>
      </NImageGroup>
    </div>

    <NPagination v-if="filtered.length > 24" v-model:page="page" class="mt-3" :item-count="filtered.length" :page-size="24" :page-slot="5" />
    <NModal v-model:show="previewShow" preset="card" title="清理未使用图片" class="asset-cleanup-modal" style="width:min(600px, calc(100vw - 24px));border-radius:1rem" :closable="!cleaning" :mask-closable="!cleaning" :close-on-esc="!cleaning">
      <NAlert v-if="errorText" type="error" class="mb-3">
        {{ errorText }}
      </NAlert>
      <p>本次将清理 {{ preview.list.length }} 个未使用文件，共 {{ (preview.bytes / 1048576).toFixed(2) }} MiB。确认时会再次检查引用，正在使用的图片不会删除。</p>
      <div class="max-h-[45vh] overflow-auto mt-3">
        <div v-for="file in preview.list" :key="file.id" class="py-2 border-b border-neutral-500/20 break-all">
          {{ file.fileName }}
        </div>
      </div>
      <template #footer>
        <div class="flex justify-end gap-2">
          <NButton :disabled="cleaning" @click="previewShow = false">
            取消
          </NButton><NButton type="error" :loading="cleaning" :disabled="cleaning || !preview.list.length" @click="cleanUnused">
            确认清理
          </NButton>
        </div>
      </template>
    </NModal>
    <RoundCardModal v-model:show="infoModalState.show" style="max-width: 300px;" size="small" :title="$t('apps.uploadsFileManager.infoTitle')">
      <div>
        <div>
          <div class="mb-2">
            <span class="text-slate-500">使用位置</span><div v-for="(reference, index) in infoModalState.fileInfo?.references || []" :key="index">
              {{ reference.kind }} · {{ reference.title }}
            </div><div v-if="!infoModalState.fileInfo?.references?.length">
              未使用
            </div>
          </div>
          <div class="mb-2">
            <span class="text-slate-500">
              {{ $t('apps.uploadsFileManager.fileName') }}
            </span>
            <div class="text-xs">
              {{ infoModalState.fileInfo?.fileName }}
            </div>
          </div>
          <div class="mb-2">
            <span class="text-slate-500">
              {{ $t('apps.uploadsFileManager.path') }}
            </span>
            <div class="text-xs">
              {{ infoModalState.fileInfo?.src }}
            </div>
          </div>
          <div class="mb-2">
            <span class="text-slate-500">
              {{ $t('apps.uploadsFileManager.uploadTime') }}
            </span>
            <div class="text-xs">
              {{ timeFormat(infoModalState.fileInfo?.createTime) }}
            </div>
          </div>
        </div>
      </div>
    </RoundCardModal>
  </div>
</template>

<style scoped>
.asset-toolbar {display:grid;grid-template-columns:minmax(0,1fr) 130px auto;gap:8px}
.asset-manager :deep(.n-button) {min-height:40px;min-width:40px}
@media(max-width:639px){.asset-toolbar{grid-template-columns:1fr auto}.asset-toolbar>:first-child{grid-column:1/-1}.asset-manager :deep(.n-button){min-height:44px;min-width:44px}.asset-manager :deep(input){font-size:16px}}

.card {
  display: flex;
  justify-content: center;
  align-items: center;
  height: 80px;
}

.transparent-grid {
  background-image: linear-gradient(45deg, #f0f0f0 25%, transparent 25%, transparent 75%, #f0f0f0 75%),
    linear-gradient(45deg, #f0f0f0 25%, transparent 25%, transparent 75%, #f0f0f0 75%);
  background-size: 16px 16px;
  background-position: 0 0, 8px 8px;
}
</style>

<style>
.asset-cleanup-modal{max-height:calc(100vh - 24px)}.asset-cleanup-modal>.n-card__content{overflow:auto;min-height:0}
@media(max-width:639px){.asset-cleanup-modal .n-button{min-height:44px}}
</style>
