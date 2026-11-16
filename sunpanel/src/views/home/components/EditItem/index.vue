<script setup lang="ts">
import { computed, defineEmits, defineProps, ref, watch } from 'vue'
import type { FormInst, FormRules } from 'naive-ui'
import { NAlert, NButton, NCollapse, NCollapseItem, NForm, NFormItem, NGrid, NGridItem, NInput, NInputGroup, NModal, NSelect, NSpace, useMessage } from 'naive-ui'
import IconEditor from './IconEditor.vue'
import { edit, getSiteFavicon } from '@/api/panel/itemIcon'
import { getList as getGroupList } from '@/api/panel/itemIconGroup'
import { t } from '@/locales'
import type { GateHomeRoute } from '@/utils/gateHome'
import { routeLink } from '@/utils/gateHome'
import GateHomeConnection from '@/components/common/GateHomeConnection.vue'
import { localizeGateHomeIcon } from '@/utils/gateHomeImages'

interface Props {
  visible: boolean
  itemInfo: Panel.Info | null
  itemGroupId?: number
}

const props = defineProps<Props>()
const emit = defineEmits<Emit>()
const ms = useMessage()
const submitLoading = ref(false)
const savingLocalIcon = ref(false)
const savingGateHomeIcon = ref(false)
const getIconLoading = ref([false, false])
const itemIconGroupOptions = ref<{
  label: string
  value: number
}[]>([])

const restoreDefault: Panel.Info = {
  icon: null,
  title: '',
  url: '',
  lanUrl: '',
  description: '',
  openMethod: 2,
}

interface Emit {
  (e: 'update:visible', visible: boolean): void
  (e: 'done', item: Panel.Info): void// 创建完成
}

const model = ref<Panel.Info>(props.itemInfo ? { ...props.itemInfo } : { ...restoreDefault })
const formRef = ref<FormInst | null>(null)

const gateHomeRoutes = ref<GateHomeRoute[]>([])
const gateHomeLoading = ref(false)
const gateHomeError = ref('')
const gateHomeReady = ref(false)
const gateHomeExpanded = ref(false)
const gateHomeSelection = ref<number | null>(null)
const gateHomeOptions = computed(() => gateHomeRoutes.value.map((route, value) => ({
  label: `${route.group} / ${route.title} · ${route.url}`,
  value,
})))
const selectedRoute = computed(() => gateHomeSelection.value === null ? null : gateHomeRoutes.value[gateHomeSelection.value])

function expandGateHome(names: Array<string | number> | string | number | null) {
  gateHomeExpanded.value = Array.isArray(names) ? names.includes('gatehome') : names === 'gatehome'
}

function setGateHomeRoutes(data: GateHomeRoute[]) {
  gateHomeRoutes.value = data
  gateHomeSelection.value = null
}

async function importGateHomeRoute() {
  const route = selectedRoute.value
  if (!route || savingGateHomeIcon.value)
    return
  const draft = model.value
  savingGateHomeIcon.value = true
  try {
    const icon = await localizeGateHomeIcon(route)
    if (!props.visible || model.value !== draft || selectedRoute.value?.id !== route.id) return
    model.value.gateHome = routeLink(route, icon)
    model.value.title = model.value.gateHome.title
    model.value.url = route.url
    model.value.lanUrl = route.lanUrl
    if (icon) model.value.icon = icon
    formRef.value?.restoreValidation()
    ms.success('已填入名称、网址及可用图标，请检查后保存。')
  } catch (err) { ms.error(err instanceof Error ? err.message : '图标保存失败，请重试') }
  finally { savingGateHomeIcon.value = false }
}

const rules: FormRules = {
  title: {
    required: true,
    trigger: 'blur',
    message: t('form.required'),
  },
  url: {
    required: true,
    trigger: 'blur',
    type: 'string',
    message: t('form.required'),
  },
  // itemIconGroupId: {
  //   required: true,
  //   trigger: ['blur', 'change'],
  //   message: t('form.required'),
  // },
}

const options = [
  {
    default: true,
    label: t('iconItem.currentPageOpen'),
    value: 1,
  },
  {
    label: t('iconItem.newWindowOpen'),
    value: 2,
  },
  {
    label: t('iconItem.currentPageLayerOpen'),
    value: 3,
  },
]

// 更新值父组件传来的值
const show = computed({
  get: () => props.visible,
  set: (visible: boolean) => {
    emit('update:visible', visible)
  },
})

async function editApi() {
  submitLoading.value = true
  try {
    const { code, data, msg } = await edit<Panel.ItemInfo>(model.value)
    if (code === 0) {
      show.value = false
      model.value = { ...restoreDefault }

      emit('done', data)
    }
    else {
      ms.error(`${t('common.saveFail')}:${msg}`)
    }
  }
  catch (error) {
    ms.error(t('common.saveFail'))
  }
  submitLoading.value = false
}

const handleValidateButtonClick = (e: MouseEvent) => {
  e.preventDefault()
  formRef.value?.validate((errors) => {
    if (!errors)
      editApi()
  })
}

async function getIconByUrl(url: string, loadingIndex: number) {
  getIconLoading.value[loadingIndex] = true
  try {
    const { code, data } = await getSiteFavicon<{ iconUrl: string }>(url)
    if (code === 0) {
      model.value.icon = {
        itemType: 2,
        src: data.iconUrl,
      }
    }
    else {
      ms.error(t('iconItem.geticonFail'))
    }
  }
  catch (error) {
    ms.error(t('iconItem.geticonFail'))
  }
  getIconLoading.value[loadingIndex] = false
}

watch(() => props.visible, (newValue) => {
  savingLocalIcon.value = false
  gateHomeLoading.value = false
  gateHomeRoutes.value = []
  gateHomeSelection.value = null
  gateHomeError.value = ''
  if (newValue === true) {
    model.value = props.itemInfo ? { ...props.itemInfo } : { ...restoreDefault }
    if (props.itemGroupId)
      model.value.itemIconGroupId = props.itemGroupId
  }

  getGroupListOptions()
})

function getGroupListOptions() {
  getGroupList<Common.ListResponse<Panel.ItemIconGroup[]>>().then(({ data, code, msg }) => {
    if (code === 0) {
      itemIconGroupOptions.value = []

      for (let i = 0; i < data.list.length; i++) {
        const element = data.list[i]
        if (i === 0 && !model.value.itemIconGroupId) {
          model.value.itemIconGroupId = element.id
          restoreDefault.itemIconGroupId = element.id
        }

        itemIconGroupOptions.value.push({
          value: element.id as number,
          label: element.title as string,
        })
      }
    }
    else {
      ms.error(`${t('iconItem.getGroupFail')}:${msg}`)
    }
  })
}
</script>

<template>
  <NModal v-model:show="show" preset="card" size="small" style="width: min(600px, calc(100vw - 24px)); max-height: calc(100vh - 24px); max-height: calc(100dvh - 24px); overflow: hidden; border-radius: 1rem;" content-style="overflow: auto; min-height: 0" :title="itemInfo ? t('iconItem.edit') : t('iconItem.add')">
    <div class="p-[5px]">
      <NCollapse class="mb-5" @update:expanded-names="expandGateHome">
        <NCollapseItem title="从 GateHome 导入" name="gatehome">
          <NSpace vertical :size="12">
            <GateHomeConnection v-if="show && gateHomeExpanded" :disabled="savingGateHomeIcon || savingLocalIcon || submitLoading" @routes="setGateHomeRoutes" @loading="gateHomeLoading = $event" @error="gateHomeError = $event" @ready="gateHomeReady = $event" />
            <NSelect v-model:value="gateHomeSelection" aria-label="选择 GateHome 反代项" filterable clearable :loading="gateHomeLoading" :disabled="!gateHomeReady || savingGateHomeIcon || gateHomeLoading || !!gateHomeError" :options="gateHomeOptions" placeholder="搜索反代名称、分组或域名">
              <template #empty>
                {{ gateHomeLoading ? '正在读取反代配置…' : gateHomeReady ? '暂无可导入的已启用反代项' : '请先连接 GateHome' }}
              </template>
            </NSelect>
            <NAlert v-if="selectedRoute" type="info" :show-icon="false">
              <img v-if="selectedRoute.imageData" :src="selectedRoute.imageData" width="40" height="40" style="object-fit: contain" alt="" />
              <div v-if="selectedRoute.imageUnavailable">图片暂不可读取，保留当前图标。</div>
              <div class="break-all">
                默认网址：{{ selectedRoute.url }}
              </div>
              <div class="break-all">
                内网地址：{{ selectedRoute.lanUrl }}
              </div>
            </NAlert>
            <div class="text-sm opacity-70">
              默认网址按反代监听端口生成；外网端口映射不同时请修改。填入会替换名称、网址及可用图标，保存后生效。
            </div>
            <NSpace justify="end">
              <NButton type="primary" :loading="savingGateHomeIcon" :disabled="!selectedRoute || gateHomeLoading || !!gateHomeError || savingLocalIcon || submitLoading" @click="importGateHomeRoute">
                填入网站信息
              </NButton>
            </NSpace>
          </NSpace>
        </NCollapseItem>
      </NCollapse>
      <NAlert v-if="model.gateHome" class="mb-4" type="info" :show-icon="false">已关联 GateHome 反代。首页的“GateHome 联动”可预览配置变化。<NButton text type="primary" @click="model.gateHome = null">解除关联</NButton></NAlert>
      <NForm ref="formRef" :model="model" :rules="rules" :disabled="savingGateHomeIcon">
        <NGrid cols="2" :x-gap="10" item-responsive>
          <NGridItem span="2 500:1">
            <NFormItem path="itemIconGroupId" :label="t('iconItem.iconGroup')">
              <NSelect v-model:value="model.itemIconGroupId" :options="itemIconGroupOptions" />
            </NFormItem>
          </NGridItem>
          <NGridItem span="2 500:1">
            <NFormItem path="title" :label="$t('common.title')">
              <NInput v-model:value="model.title" type="text" show-count :maxlength="20" />
            </NFormItem>
          </NGridItem>
        </NGrid>

        <NFormItem path="icon" :label="$t('common.icon')">
          <IconEditor v-model:item-icon="model.icon" v-model:saving="savingLocalIcon" :disabled="savingGateHomeIcon" />
        </NFormItem>
        <NFormItem path="url" :label="$t('iconItem.url')">
          <!-- <NSelect :style="{ width: '100px' }" :options="urlProtocolOptions" /> -->
          <NInputGroup>
            <NInput v-model:value="model.url" type="text" :maxlength="1000" placeholder="http(s)://" />
            <NButton :disabled="!model.url" :loading="getIconLoading[0]" @click="getIconByUrl(model.url, 0)">
              {{ $t('iconItem.getIcon') }}
            </NButton>
          </NInputGroup>
        </NFormItem>
        <NFormItem path="lanUrl" :label="$t('iconItem.lanUrl')">
          <NInputGroup>
            <NInput v-model:value="model.lanUrl" type="text" :maxlength="1000" :placeholder="$t('iconItem.lanUrlInputPlaceholder')" />
            <NButton :disabled="!model.lanUrl" :loading="getIconLoading[1]" @click="getIconByUrl(model.lanUrl || '', 1)">
              {{ $t('iconItem.getIcon') }}
            </NButton>
          </NInputGroup>
        </NFormItem>
        <NFormItem path="description" :label="$t('common.description')">
          <NInput v-model:value="model.description" type="text" show-count :maxlength="100" />
        </NFormItem>
        <NFormItem path="openMethod" :label="$t('iconItem.openMethod')">
          <NSelect v-model:value="model.openMethod" :options="options" />
        </NFormItem>
      </NForm>
    </div>

    <template #footer>
      <div class="flex justify-end">
        <NButton type="success" :loading="submitLoading" :disabled="savingLocalIcon || savingGateHomeIcon" @click="handleValidateButtonClick">
          {{ $t('common.save') }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>
