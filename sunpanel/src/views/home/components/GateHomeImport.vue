<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { NAlert, NButton, NCheckbox, NEmpty, NModal, NSelect, NSpace, NTabPane, NTabs, useMessage } from 'naive-ui'
import { post } from '@/utils/request'
import type { GateHomeRoute } from '@/utils/gateHome'
import { isImported, loadGateHomeRoutes, routeChanges, routeLink } from '@/utils/gateHome'

const props = defineProps<{ visible: boolean; groups: (Panel.ItemIconGroup & { items?: Panel.ItemInfo[] })[] }>()
const emit = defineEmits<{ (e: 'update:visible', visible: boolean): void; (e: 'done'): void }>()
const show = computed({ get: () => props.visible, set: value => emit('update:visible', value) })
const ms = useMessage()
const routes = ref<GateHomeRoute[]>([])
const selected = ref<string[]>([])
const syncSelected = ref<number[]>([])
const group = ref<number | null>(null)
const loading = ref(false)
const saving = ref(false)
const error = ref('')
let request = 0
const existing = computed(() => props.groups.flatMap(group => group.items || []))
const groupOptions = computed(() => props.groups.map(group => ({ label: group.title, value: group.id as number })))
const routeOptions = computed(() => routes.value.map(route => ({
  label: `${route.group} / ${route.title}${isImported(route, existing.value) ? ' · 已存在' : ''}`,
  value: route.id,
  disabled: isImported(route, existing.value),
})))
const preview = computed(() => routes.value.filter(route => selected.value.includes(route.id)))
const syncRows = computed(() => existing.value.filter(item => item.gateHome).map((item) => {
  const route = routes.value.find(route => route.id === item.gateHome?.routeId)
  return { item, route, changes: route ? routeChanges(item, route) : [] }
}).filter(row => !row.route || row.changes.length))

function toggleSync(id: number | undefined, checked: boolean) {
  if (id !== undefined)
    syncSelected.value = checked ? [...syncSelected.value, id] : syncSelected.value.filter(selected => selected !== id)
}

async function refresh() {
  const current = ++request
  loading.value = true
  error.value = ''
  selected.value = []
  syncSelected.value = []
  try {
    const data = await loadGateHomeRoutes()
    if (current === request)
      routes.value = data
  }
  catch (err) {
    if (current === request)
      error.value = err instanceof Error ? err.message : '读取失败，请重试。'
  }
  finally {
    if (current === request)
      loading.value = false
  }
}

async function importSelected() {
  saving.value = true
  try {
    const items = preview.value.map(route => ({ title: routeLink(route).title, url: route.url, lanUrl: route.lanUrl, icon: null, openMethod: 2, itemIconGroupId: group.value, gateHome: routeLink(route) }))
    const { code, data, msg } = await post<{ created: number; skipped: number }>({ url: '/panel/itemIcon/gateHome/import', data: { items } })
    if (code !== 0) {
      ms.error(msg)
      return
    }
    ms.success(`已导入 ${data.created} 个项目，跳过 ${data.skipped} 个重复项`)
    emit('done')
    show.value = false
  }
  catch { ms.error('导入失败，请重试') }
  finally { saving.value = false }
}

async function syncSelectedItems() {
  saving.value = true
  try {
    const items = syncRows.value.filter(row => row.route && syncSelected.value.includes(row.item.id as number)).map(row => ({ id: row.item.id, updatedAt: row.item.updateTime, route: routeLink(row.route!) }))
    const { code, data, msg } = await post<{ updated: number }>({ url: '/panel/itemIcon/gateHome/sync', data: { items } })
    if (code !== 0) {
      ms.error(msg)
      return
    }
    ms.success(`已更新 ${data.updated} 个关联项目，自定义字段已保留`)
    emit('done')
    show.value = false
  }
  catch { ms.error('关联更新失败，请重新预览') }
  finally { saving.value = false }
}

watch(() => props.visible, (visible) => {
  if (visible) {
    group.value = props.groups[0]?.id as number || null
    refresh()
  }
  else { ++request }
})
</script>

<template>
  <NModal v-model:show="show" preset="card" title="GateHome 联动" style="width: min(760px, calc(100vw - 24px)); border-radius: 1rem" :mask-closable="!saving" :closable="!saving">
    <NSpace vertical :size="16">
      <NAlert v-if="error" type="warning" :show-icon="false">
        {{ error }}
      </NAlert>
      <NSpace justify="space-between" align="center">
        <span>读取已启用的反代项，导入后按需预览并更新。</span>
        <NButton :loading="loading" :disabled="saving" @click="refresh">
          刷新反代列表
        </NButton>
      </NSpace>
      <NTabs type="line" animated>
        <NTabPane name="import" tab="批量导入">
          <NSpace vertical :size="16">
            <label>导入到分组</label>
            <NSelect v-model:value="group" aria-label="导入到分组" :options="groupOptions" :disabled="saving" />
            <label>选择反代项</label>
            <NSelect v-model:value="selected" multiple filterable clearable aria-label="批量选择反代项" placeholder="搜索名称或分组，已存在的项目自动跳过" :options="routeOptions" :loading="loading" :disabled="loading || saving || !!error" />
            <div class="gatehome-preview">
              <NEmpty v-if="!preview.length" description="选择后在这里预览内外网地址" />
              <article v-for="route in preview" :key="route.id" class="gatehome-row">
                <strong>{{ routeLink(route).title }}</strong>
                <div>默认网址：{{ route.url }}</div>
                <div>内网地址：{{ route.lanUrl }}</div>
              </article>
            </div>
            <NSpace justify="end">
              <NButton type="primary" :disabled="!preview.length || !group || loading || !!error" :loading="saving" @click="importSelected">
                导入 {{ preview.length }} 个项目
              </NButton>
            </NSpace>
          </NSpace>
        </NTabPane>
        <NTabPane name="sync" tab="关联更新">
          <NSpace vertical :size="16">
            <NAlert type="info" :show-icon="false">
              只更新仍与上次导入值相同的名称和地址；自定义字段、图标和分组保留。
            </NAlert>
            <div class="gatehome-preview">
              <NEmpty v-if="!syncRows.length && !loading" description="关联项目暂无变化" />
              <article v-for="row in syncRows" :key="row.item.id" class="gatehome-row">
                <NCheckbox v-if="row.route" :checked="syncSelected.includes(row.item.id as number)" :disabled="saving" @update:checked="toggleSync(row.item.id, $event)">
                  {{ row.item.title }}
                </NCheckbox>
                <strong v-else>{{ row.item.title }}</strong>
                <div v-if="!row.route">
                  来源已停用或删除，保留此项目。
                </div>
                <div v-for="change in row.changes" :key="change.field" class="gatehome-change">
                  <strong>{{ change.label }} · {{ change.preserved ? '保留自定义值' : '将更新' }}</strong>
                  <div>当前：{{ change.current || '未填写' }}</div>
                  <div>反代：{{ change.next || '未填写' }}</div>
                </div>
              </article>
            </div>
            <NSpace justify="end">
              <NButton type="primary" :disabled="!syncSelected.length || loading || !!error" :loading="saving" @click="syncSelectedItems">
                应用 {{ syncSelected.length }} 个项目的更新
              </NButton>
            </NSpace>
          </NSpace>
        </NTabPane>
      </NTabs>
    </NSpace>
  </NModal>
</template>

<style scoped>
.gatehome-preview { max-height: 42vh; overflow: auto; }
.gatehome-row { padding: 14px 0; border-bottom: 1px solid var(--n-border-color); overflow-wrap: anywhere; }
.gatehome-row > div { margin-top: 6px; }
.gatehome-change { padding: 10px 12px; border-radius: 8px; background: var(--n-color-modal); }
</style>
