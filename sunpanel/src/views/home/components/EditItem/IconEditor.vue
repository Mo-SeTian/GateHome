<script setup lang="ts">
import { NButton, NColorPicker, NInput, NRadio, NUpload, useMessage } from 'naive-ui'
import type { UploadFileInfo } from 'naive-ui'
import { computed, defineProps, onUnmounted, ref } from 'vue'
import { uploadImage } from '@/api/system/file'
import { ItemIcon } from '@/components/common'
import { t } from '@/locales'
import { useAuthStore } from '@/store'
import { createLocalIconPng } from '@/utils/localIcon'
import { apiRespErrMsg } from '@/utils/request/apiMessage'

const props = defineProps<{
  itemIcon: Panel.ItemIcon | null
}>()
const emit = defineEmits<{
  (e: 'update:itemIcon', visible: Panel.ItemIcon): void // 定义修改父组件（prop内）的值的事件
  (e: 'update:saving', saving: boolean): void
}>()
const authStore = useAuthStore()
const ms = useMessage()
const savingLocalIcon = ref(false)
const iconPreview = ref<HTMLElement | null>(null)
let unmounted = false
onUnmounted(() => {
  unmounted = true
})

// 默认图标背景色
const defautSwatchesBackground = [
  '#00000000',
  '#000000',
  '#ffffff',
  '#18A058',
  '#2080F0',
  '#F0A020',
  'rgba(208, 48, 80, 1)',
  '#C418D1FF',
]

const initData: Panel.ItemIcon = {
  itemType: 2,
  backgroundColor: '#2a2a2a6b',
}

const itemIconInfo = computed({
  get() {
    const v = {
      ...initData,
      ...props.itemIcon,
      backgroundColor: props.itemIcon?.backgroundColor || initData.backgroundColor,
    }
    return v
  },
  set() {
    handleChange()
  },
})

function handleIconTypeRadioChange(type: number) {
  // checkedValueRef.value = type
  itemIconInfo.value.itemType = type
  handleChange()
}

function handleChange() {
  emit('update:itemIcon', itemIconInfo.value || null)
}

function handleResetBackgroundColor() {
  itemIconInfo.value.backgroundColor = initData.backgroundColor
  handleChange()
}

async function handleSaveLocalIcon() {
  if (savingLocalIcon.value || !itemIconInfo.value.text?.trim())
    return
  const original = props.itemIcon
  savingLocalIcon.value = true
  emit('update:saving', true)
  try {
    const preview = iconPreview.value?.querySelector('svg')
    const color = preview ? getComputedStyle(preview).color : '#ffffff'
    const file = await createLocalIconPng(itemIconInfo.value.text, color)
    const { code, data } = await uploadImage(file)
    if (code !== 0)
      return
    if (!unmounted && props.itemIcon === original) {
      emit('update:itemIcon', { ...itemIconInfo.value, itemType: 2, src: data.imageUrl })
      ms.success(t('iconItem.localIconSaved'))
    }
  }
  catch {
    if (!unmounted)
      ms.error(t('iconItem.localIconSaveFail'))
  }
  finally {
    savingLocalIcon.value = false
    if (!unmounted)
      emit('update:saving', false)
  }
}

const handleUploadFinish = ({
  file,
  event,
}: {
  file: UploadFileInfo
  event?: ProgressEvent
}) => {
  const res = JSON.parse((event?.target as XMLHttpRequest).response)
  if (res.code === 0) {
    const imageUrl = res.data.imageUrl
    itemIconInfo.value.src = imageUrl
    emit('update:itemIcon', itemIconInfo.value || null)
  }
  else {
    apiRespErrMsg(res)
    // ms.error(`${t('common.uploadFail')}:${res.msg}`)
  }

  return file
}
</script>

<template>
  <div>
    <div class="mb-[10px]">
      <NRadio
        :disabled="savingLocalIcon"
        :checked="itemIconInfo.itemType === 1 "
        :value="1"
        name="iconType"
        @change="handleIconTypeRadioChange(1)"
      >
        {{ $t('common.text') }}
      </NRadio>

      <NRadio
        :disabled="savingLocalIcon"
        :checked="itemIconInfo.itemType === 2"
        :value="2"
        name="iconType"
        @change="handleIconTypeRadioChange(2)"
      >
        {{ $t('common.image') }}
      </NRadio>

      <NRadio
        :disabled="savingLocalIcon"
        :checked="itemIconInfo.itemType === 3"
        :value="3"
        name="iconType"
        @change="handleIconTypeRadioChange(3)"
      >
        {{ $t('iconItem.onlineIcon') }}
      </NRadio>
    </div>

    <div class="min-h-[100px]">
      <div class="flex">
        <div>
          <div ref="iconPreview" class="border rounded-2xl bg-slate-200 overflow-hidden rounded-2xl transparent-grid">
            <ItemIcon :item-icon="itemIconInfo" />
          </div>
        </div>
        <!-- 文字 -->
        <div class="ml-[20px] min-w-0 flex-1">
          <!-- <NImage :src="model.icon" preview-disabled /> -->
          <div v-if="itemIconInfo.itemType === 1">
            <NInput v-model:value="itemIconInfo.text" class="mb-[5px]" size="small" type="text" @input="handleChange" />
          </div>

          <div v-if="itemIconInfo.itemType === 3">
            <div>
              <NInput v-model:value="itemIconInfo.text" :disabled="savingLocalIcon" class="mb-[5px]" size="small" type="text" :placeholder="$t('iconItem.inputIconName')" @input="handleChange" />
              <div class="flex flex-wrap gap-2">
                <NButton size="small" type="primary" secondary :loading="savingLocalIcon" :disabled="savingLocalIcon || !itemIconInfo.text?.trim()" @click="handleSaveLocalIcon">
                  {{ $t('iconItem.saveLocalIcon') }}
                </NButton>
                <NButton tag="a" href="https://icon-sets.iconify.design/" target="_blank" rel="noopener" size="small" quaternary type="info">
                  {{ $t('iconItem.onlineIconLibrary') }}
                </NButton>
              </div>
              <div class="mt-2 text-xs opacity-70">
                {{ $t('iconItem.localIconHint') }}
              </div>
            </div>
          </div>

          <!-- 图片 -->
          <div v-if="itemIconInfo.itemType === 2">
            <NInput v-model:value="itemIconInfo.src" class="mb-[5px] w-full" size="small" type="text" :placeholder="$t('iconItem.inputIconUrlOrUpload')" @input="handleChange" />
            <NUpload
              action="/sunpanel/api/file/uploadImg"
              :show-file-list="false"
              name="imgfile"
              :headers="{
                token: authStore.token as string,
              }"
              @finish="handleUploadFinish"
            >
              <NButton size="small">
                {{ $t('iconItem.selectUpload') }}
              </NButton>
            </NUpload>
          </div>
        </div>
      </div>

      <div class="flex items-center mt-[10px]">
        <div class="w-auto text-slate-500 mr-[10px]">
          {{ $t('common.backgroundColor') }}
        </div>
        <div class="w-[150px] flex items-center mr-[10px]">
          <NColorPicker
            v-model:value="itemIconInfo.backgroundColor"
            :disabled="savingLocalIcon"
            size="small"
            :modes="['hex']"
            :swatches="defautSwatchesBackground"
            @complete="handleChange"
            @update-value="handleChange"
          />
        </div>
        <div v-if="itemIconInfo.backgroundColor !== initData.backgroundColor" class="w-auto text-slate-500 mr-[10px] cursor-pointer">
          <NButton :disabled="savingLocalIcon" quaternary type="info" @click="handleResetBackgroundColor">
            {{ $t('common.reset') }}
          </NButton>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.transparent-grid {
    background-image: linear-gradient(45deg, #fff 25%, transparent 25%, transparent 75%, #fff 75%),
                      linear-gradient(45deg, #fff 25%, transparent 25%, transparent 75%, #fff 75%);
    background-size: 16px 16px;
    background-position: 0 0, 8px 8px;
}
</style>
