<script setup lang="ts">
import { computed, defineEmits, defineProps, ref, watch } from 'vue'
import type { FormInst, FormRules } from 'naive-ui'
import { NAlert, NButton, NCollapse, NCollapseItem, NForm, NFormItem, NGrid, NGridItem, NInput, NInputGroup, NModal, NSelect, NSpace, useMessage } from 'naive-ui'
import IconEditor from './IconEditor.vue'
import { edit, getSiteFavicon } from '@/api/panel/itemIcon'
import { getList as getGroupList } from '@/api/panel/itemIconGroup'
import { t } from '@/locales'
import { routeLink } from '@/utils/gateHome'

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

interface GateHomeRoute { id: string; title: string; group: string; url: string; lanUrl: string }
const gateHomeRoutes = ref<GateHomeRoute[]>([])
const gateHomeLoading = ref(false)
const gateHomeError = ref('')
const gateHomeLoaded = ref(false)
const gateHomeSelection = ref<number | null>(null)
let gateHomeRequest = 0
const gateHomeOptions = computed(() => gateHomeRoutes.value.map((route, value) => ({
  label: `${route.group} / ${route.title} · ${route.url}`,
  value,
})))
const selectedRoute = computed(() => gateHomeSelection.value === null ? null : gateHomeRoutes.value[gateHomeSelection.value])

function expandGateHome(names: Array<string | number> | string | number | null) {
  if (Array.isArray(names) && names.length && !gateHomeLoaded.value && !gateHomeLoading.value)
    loadGateHomeRoutes()
}

async function loadGateHomeRoutes() {
  const request = ++gateHomeRequest
  gateHomeLoading.value = true
  gateHomeError.value = ''
  gateHomeSelection.value = null
  try {
    const response = await fetch('/api/sunpanel/routes', { credentials: 'same-origin' })
    if (!response.ok)
      throw new Error(response.status === 401 ? '请先在当前浏览器登录 GateHome 管理页，再点击刷新。' : '无法读取反代配置，请稍后重试。')
    const data = await response.json()
    if (request !== gateHomeRequest)
      return
    gateHomeRoutes.value = data
    gateHomeLoaded.value = true
  }
  catch (error) {
    if (request === gateHomeRequest)
      gateHomeError.value = error instanceof Error ? error.message : '读取失败，请重试。'
  }
  finally {
    if (request === gateHomeRequest)
      gateHomeLoading.value = false
  }
}

function importGateHomeRoute() {
  const route = selectedRoute.value
  if (!route)
    return
  model.value.gateHome = routeLink(route)
  model.value.title = model.value.gateHome.title
  model.value.url = route.url
  model.value.lanUrl = route.lanUrl
  formRef.value?.restoreValidation()
  ms.success('已填入名称、默认网址和内网地址，请检查后保存。')
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
  ++gateHomeRequest
  gateHomeLoading.value = false
  gateHomeLoaded.value = false
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
  <NModal v-model:show="show" preset="card" size="small" style="width: 600px;border-radius: 1rem;" :title="itemInfo ? t('iconItem.edit') : t('iconItem.add')">
    <div class="h-[600px] overflow-auto p-[5px]">
      <NCollapse class="mb-5" @update:expanded-names="expandGateHome">
        <NCollapseItem title="从 GateHome 导入" name="gatehome">
          <NSpace vertical :size="12">
            <NAlert v-if="gateHomeError" type="warning" :show-icon="false">
              {{ gateHomeError }}
            </NAlert>
            <NSelect v-model:value="gateHomeSelection" aria-label="选择 GateHome 反代项" filterable clearable :loading="gateHomeLoading" :disabled="gateHomeLoading || !!gateHomeError" :options="gateHomeOptions" placeholder="搜索反代名称、分组或域名">
              <template #empty>
                {{ gateHomeLoading ? '正在读取反代配置…' : '暂无可导入的已启用反代项' }}
              </template>
            </NSelect>
            <NAlert v-if="selectedRoute" type="info" :show-icon="false">
              <div class="break-all">
                默认网址：{{ selectedRoute.url }}
              </div>
              <div class="break-all">
                内网地址：{{ selectedRoute.lanUrl }}
              </div>
            </NAlert>
            <div class="text-sm opacity-70">
              默认网址按反代监听端口生成；外网端口映射不同时请修改。填入会替换当前名称和两个网址，保存后生效。
            </div>
            <NSpace justify="end">
              <NButton :loading="gateHomeLoading" @click="loadGateHomeRoutes">
                刷新
              </NButton>
              <NButton type="primary" :disabled="!selectedRoute || gateHomeLoading || !!gateHomeError" @click="importGateHomeRoute">
                填入网站信息
              </NButton>
            </NSpace>
          </NSpace>
        </NCollapseItem>
      </NCollapse>
      <NAlert v-if="model.gateHome" class="mb-4" type="info" :show-icon="false">已关联 GateHome 反代。首页的“GateHome 联动”可预览配置变化。<NButton text type="primary" @click="model.gateHome = null">解除关联</NButton></NAlert>
      <NForm ref="formRef" :model="model" :rules="rules">
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
          <IconEditor v-model:item-icon="model.icon" v-model:saving="savingLocalIcon" />
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
      <NButton type="success" :loading="submitLoading" :disabled="savingLocalIcon" style="float: right;" @click="handleValidateButtonClick">
        {{ $t('common.save') }}
      </NButton>
    </template>
  </NModal>
</template>
