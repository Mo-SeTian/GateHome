<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { NAlert, NButton, NCard, NCheckbox, NEmpty, NInput, NModal, NPagination, NSelect, NSpin, NTag, NTooltip, useMessage } from 'naive-ui'
import { editIcon, getIconList } from '@/api/panel/itemIcon'
import { getList as getGroups } from '@/api/panel/itemIconGroup'
import { ItemIcon, SvgIcon } from '@/components/common'
import IconEditor from '@/views/home/components/EditItem/IconEditor.vue'
import { createLocalIconPng, isImageURL } from '@/utils/localIcon'
import { importImage, uploadImage } from '@/api/system/file'
import { t } from '@/locales'

const emit = defineEmits<{ (e: 'iconUpdated', item: Panel.ItemInfo): void }>()
const ms = useMessage()
const items = ref<Panel.ItemInfo[]>([])
const groups = ref<Panel.ItemIconGroup[]>([])
const loading = ref(false)
const loadError = ref('')
const keyword = ref('')
const groupId = ref<number | null>(null)
const page = ref(1)
const selected = ref<Panel.ItemInfo | null>(null)
const draft = ref<Panel.ItemIcon | null>(null)
const editorShow = ref(false)
const saving = ref(false)
const savingImage = ref(false)
const saveError = ref('')
let active = true
const selectedIds = ref<number[]>([])
const batchShow = ref(false)
const batchRunning = ref(false)
const batchRows = ref<{ item: Panel.ItemInfo; state: string; error: string }[]>([])
const busy = computed(() => saving.value || savingImage.value || batchRunning.value)
function toggleSelection(item: Panel.ItemInfo, checked: boolean) {
  if (!item.id)
    return
  selectedIds.value = checked ? [...selectedIds.value, item.id] : selectedIds.value.filter(id => id !== item.id)
}
function previewBatch() {
  batchRows.value = items.value.filter(item => item.id && selectedIds.value.includes(item.id) && ['remote', 'online'].includes(iconKind(item.icon))).map(item => ({ item: { ...item, icon: item.icon ? { ...item.icon } : null }, state: '等待处理', error: '' }))
  batchShow.value = true
}
async function saveBatch(retry = false) {
  if (batchRunning.value)
    return
  batchRunning.value = true
  try {
    for (const row of batchRows.value) {
      if (!active)
        break
      if (row.state === '已保存' || (retry && row.state !== '失败'))
        continue
      row.state = '处理中'
      row.error = ''
      try {
        const source = (row.item.icon?.itemType === 2 ? row.item.icon.src : row.item.icon?.text)?.trim() || ''
        const image = isImageURL(source) ? await importImage(source) : await uploadImage(await createLocalIconPng(source, '#ffffff'))
        if (image.code !== 0)
          throw new Error(image.msg || '图片下载失败')
        if (!active)
          break
        const result = await editIcon(row.item, { ...row.item.icon, itemType: 2, src: image.data.imageUrl })
        if (result.code !== 0)
          throw new Error(result.msg || '图标保存失败')
        if (active) {
          items.value = items.value.map(item => item.id === result.data.id ? result.data : item)
          row.item = result.data
          row.state = '已保存'
          selectedIds.value = selectedIds.value.filter(id => id !== result.data.id)
          emit('iconUpdated', result.data)
        }
      }
      catch (error) {
        row.state = '失败'
        row.error = error instanceof Error ? error.message : '图标保存失败'
      }
    }
  }
  finally {
    batchRunning.value = false
  }
}
const groupOptions = computed(() => groups.value.map(group => ({ label: group.title || '', value: group.id as number })))
const filtered = computed(() => {
  const query = keyword.value.trim().toLocaleLowerCase()
  return items.value.filter(item => (groupId.value === null || item.itemIconGroupId === groupId.value)
    && (!query || `${item.title} ${item.url}`.toLocaleLowerCase().includes(query)))
})
const visibleItems = computed(() => filtered.value.slice((page.value - 1) * 24, page.value * 24))
watch([keyword, groupId], () => {
  page.value = 1
})

function groupTitle(item: Panel.ItemInfo) {
  return groups.value.find(group => group.id === item.itemIconGroupId)?.title || t('apps.iconManage.noGroup')
}

function iconKind(icon: Panel.ItemIcon | null) {
  if (!icon || !icon.itemType || (icon.itemType === 2 && !icon.src))
    return 'unset'
  if (icon.itemType === 1)
    return 'text'
  const source = (icon.itemType === 2 ? icon.src : icon.text)?.trim() || ''
  if (icon.itemType === 3 && !isImageURL(source))
    return source ? 'online' : 'unset'
  return source.startsWith('/sunpanel/uploads/') ? 'local' : 'remote'
}

async function refresh() {
  if (loading.value || batchRunning.value)
    return
  loading.value = true
  loadError.value = ''
  try {
    const [icons, categories] = await Promise.all([getIconList(), getGroups<Common.ListResponse<Panel.ItemIconGroup[]>>()])
    if (icons.code !== 0 || categories.code !== 0)
      throw new Error(icons.msg || categories.msg || t('apps.iconManage.loadFailed'))
    if (active) {
      items.value = icons.data.list
      groups.value = categories.data.list
      if (groupId.value !== null && !groups.value.some(group => group.id === groupId.value))
        groupId.value = null
      page.value = Math.min(page.value, Math.max(1, Math.ceil(filtered.value.length / 24)))
    }
  }
  catch (error) {
    if (active)
      loadError.value = error instanceof Error ? error.message : t('apps.iconManage.loadFailed')
  }
  finally {
    if (active)
      loading.value = false
  }
}

function openEditor(item: Panel.ItemInfo) {
  selected.value = item
  draft.value = item.icon ? { ...item.icon } : null
  saveError.value = ''
  savingImage.value = false
  editorShow.value = true
}

async function save() {
  if (!selected.value || busy.value || !draft.value)
    return
  saving.value = true
  saveError.value = ''
  try {
    const result = await editIcon(selected.value, draft.value)
    if (result.code !== 0)
      throw new Error(result.msg || t('apps.iconManage.saveFailed'))
    if (active) {
      items.value = items.value.map(item => item.id === result.data.id ? result.data : item)
      emit('iconUpdated', result.data)
      editorShow.value = false
      ms.success(t('common.saveSuccess'))
    }
  }
  catch (error) {
    if (active)
      saveError.value = error instanceof Error ? error.message : t('apps.iconManage.saveFailed')
  }
  finally {
    if (active)
      saving.value = false
  }
}

onMounted(refresh)
onUnmounted(() => {
  active = false
})
</script>

<template>
  <div class="icon-manager p-3 h-full flex flex-col gap-3">
    <div class="flex items-center gap-2">
      <h2 class="font-bold text-base">
        {{ $t('apps.iconManage.appName') }}
      </h2>
      <NTooltip>
        <template #trigger>
          <NButton quaternary circle :aria-label="$t('apps.iconManage.helpLabel')">
            <template #icon>
              <SvgIcon icon="lucide:info" />
            </template>
          </NButton>
        </template>
        {{ $t('apps.iconManage.help') }}
      </NTooltip>
      <span class="ml-auto text-sm" role="status">{{ $t('apps.iconManage.count', { count: filtered.length, total: items.length }) }}</span>
    </div>
    <div class="icon-toolbar">
      <NInput v-model:value="keyword" clearable :aria-label="$t('apps.iconManage.search')" :placeholder="$t('apps.iconManage.search')" />
      <NSelect v-model:value="groupId" clearable :options="groupOptions" :aria-label="$t('iconItem.iconGroup')" :placeholder="$t('apps.iconManage.allGroups')" />
      <NButton :disabled="!selectedIds.length || busy || loading" @click="previewBatch">
        保存选中图标为本地
      </NButton>
      <NButton :disabled="batchRunning" :loading="loading" @click="refresh">
        {{ $t('apps.iconManage.refresh') }}
      </NButton>
    </div>
    <NAlert v-if="loadError" type="error" :show-icon="false">
      {{ loadError }}
    </NAlert>
    <div class="icon-list flex-1 min-h-0 overflow-auto" :aria-busy="loading">
      <NSpin v-if="loading && !items.length" class="flex justify-center p-6" />
      <NEmpty v-else-if="!visibleItems.length && !loadError" class="p-6" :description="$t(items.length ? 'apps.iconManage.noMatch' : 'apps.iconManage.empty')" />
      <div v-else class="flex flex-col gap-2">
        <NCard v-for="item in visibleItems" :key="item.id" size="small" class="rounded-xl">
          <div class="icon-row">
            <NCheckbox v-if="['remote', 'online'].includes(iconKind(item.icon))" class="icon-batch-check" :checked="!!item.id && selectedIds.includes(item.id)" :disabled="busy || loading" :aria-label="`选择 ${item.title} 保存为本地`" @update:checked="(checked: boolean) => toggleSelection(item, checked)" />
            <div class="rounded-lg overflow-hidden" role="img" :aria-label="$t('apps.iconManage.preview', { title: item.title })">
              <ItemIcon :item-icon="item.icon" :size="56" />
            </div>
            <div class="min-w-0">
              <div class="font-bold truncate" :title="item.title">
                {{ item.title }}
              </div>
              <div class="flex flex-wrap items-center gap-2 mt-1">
                <span class="text-sm">{{ groupTitle(item) }}</span>
                <NTag size="small" :bordered="false">
                  {{ $t(`apps.iconManage.kinds.${iconKind(item.icon)}`) }}
                </NTag>
              </div>
              <div class="text-xs mt-1 truncate opacity-70" :title="item.icon?.itemType === 2 ? item.icon.src : item.icon?.text">
                {{ item.icon?.itemType === 2 ? item.icon.src : item.icon?.text }}
              </div>
            </div>
            <NButton secondary type="primary" :disabled="loading || batchRunning" :aria-label="$t('apps.iconManage.editLabel', { title: item.title })" @click="openEditor(item)">
              {{ $t('common.edit') }}
            </NButton>
          </div>
        </NCard>
      </div>
    </div>
    <NPagination v-if="filtered.length > 24" v-model:page="page" :item-count="filtered.length" :page-size="24" :page-slot="5" />
    <NModal v-model:show="batchShow" preset="card" title="批量保存本地图标" class="icon-edit-modal" style="width:min(640px, calc(100vw - 24px));border-radius:1rem" :closable="!batchRunning" :mask-closable="!batchRunning" :close-on-esc="!batchRunning">
      <p role="status" aria-live="polite">
        已保存 {{ batchRows.filter(row => row.state === '已保存').length }} / {{ batchRows.length }} 个；仅修改图标。
      </p>
      <div class="max-h-[50vh] overflow-auto mt-3">
        <div v-for="row in batchRows" :key="row.item.id" class="py-3 border-b border-neutral-500/20">
          <div class="flex justify-between gap-2">
            <strong>{{ row.item.title }}</strong><span>{{ row.state }}</span>
          </div><p v-if="row.error" class="text-red-600 dark:text-red-400 mt-1">
            {{ row.error }}
          </p>
        </div>
      </div>
      <template #footer>
        <div class="flex justify-end gap-2">
          <NButton :disabled="batchRunning" @click="batchShow = false">
            关闭
          </NButton><NButton v-if="batchRows.some(row => row.state === '失败')" :disabled="batchRunning" @click="saveBatch(true)">
            重试失败项
          </NButton><NButton type="success" :loading="batchRunning" :disabled="batchRunning || !batchRows.some(row => row.state === '等待处理')" @click="saveBatch()">
            开始保存
          </NButton>
        </div>
      </template>
    </NModal>
    <NModal v-model:show="editorShow" preset="card" size="small" class="icon-edit-modal" style="width:min(560px, calc(100vw - 24px));border-radius:1rem" :title="$t('apps.iconManage.editTitle', { title: selected?.title })" :closable="!busy" :close-on-esc="!busy" :mask-closable="!busy">
      <NAlert v-if="saveError" class="mb-3" type="error" :show-icon="false">
        {{ saveError }}
      </NAlert>
      <IconEditor v-if="editorShow" :key="selected?.id" v-model:item-icon="draft" v-model:saving="savingImage" :disabled="saving" />
      <template #footer>
        <div class="flex justify-end gap-2">
          <NButton :disabled="busy" @click="editorShow = false">
            {{ $t('common.cancel') }}
          </NButton>
          <NButton type="success" :loading="saving" :disabled="busy || !draft" @click="save">
            {{ $t('common.save') }}
          </NButton>
        </div>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.icon-toolbar { display: grid; grid-template-columns: minmax(0, 1fr) 150px auto auto; gap: 8px; }
.icon-batch-check:before{content:'';position:absolute;inset:-13px}.icon-batch-check {position:absolute;left:8px;top:6px;z-index:1}.icon-row {position:relative; display: grid; grid-template-columns: 56px minmax(0, 1fr) auto; align-items: center; gap: 12px; }
.icon-manager :deep(.n-button--circle) {min-width:40px}
.icon-manager :deep(.n-button), .icon-toolbar :deep(.n-input), .icon-toolbar :deep(.n-base-selection) { min-height: 40px; }
@media (max-width: 639px) {
  .icon-manager :deep(.n-button--circle) {min-width:44px}
  .icon-toolbar { grid-template-columns: minmax(0, 1fr) auto; }
  .icon-toolbar > :first-child { grid-column: 1 / -1; }
  .icon-toolbar > :nth-child(2) {grid-column: 1 / -1;}
  .icon-manager :deep(.n-button), .icon-toolbar :deep(.n-input), .icon-toolbar :deep(.n-base-selection) { min-height: 44px; }
  .icon-row { gap: 8px; }
}
</style>

<style>
.icon-edit-modal { max-height: calc(100vh - 24px); }
.icon-edit-modal > .n-card__content { min-height: 0; overflow: auto; }
@media (max-width: 639px) {
  .icon-edit-modal .n-button, .icon-edit-modal .n-input, .icon-edit-modal .n-radio { min-height: 44px; }
  .icon-edit-modal .n-radio { display: inline-flex; align-items: center; }
  .icon-edit-modal .n-input input { font-size: 16px; }
}
@media (prefers-reduced-motion: reduce) {
  .icon-edit-modal { animation: none !important; transition: none !important; }
}
</style>
