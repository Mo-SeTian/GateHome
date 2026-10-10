<script setup lang="ts">
import { ref } from 'vue'
import type { UploadFileInfo } from 'naive-ui'
import { NAlert, NButton, NModal, NSpace, NUpload, useMessage } from 'naive-ui'
import { useAuthStore } from '@/store'

interface Preview { groups: number; items: number; images: number; externalImages: number }
const auth = useAuthStore()
const ms = useMessage()
const busy = ref(false)
const show = ref(false)
const preview = ref<Preview | null>(null)
const file = ref<File | null>(null)
const error = ref('')

async function request(action: string, data?: FormData) {
  const response = await fetch(`/sunpanel/api/panel/itemIcon/portable/${action}`, { method: 'POST', headers: { token: auth.token || '' }, body: data })
  if (response.ok && response.headers.get('Content-Type')?.startsWith('application/zip'))
    return response.blob()
  const result = await response.json()
  if (!response.ok || result.code !== 0)
    throw new Error(result.msg || '首页 ZIP 操作失败，请重试')
  return result.data
}

async function exportZIP() {
  busy.value = true
  error.value = ''
  try {
    const blob = await request('export') as Blob
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `SunPanel-${new Date().toISOString().slice(0, 10)}.zip`
    link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
    ms.success('首页 ZIP 已导出')
  }
  catch (err) { error.value = err instanceof Error ? err.message : '导出失败' }
  finally { busy.value = false }
}

function uploadData() {
  const data = new FormData()
  if (file.value)
    data.append('file', file.value)
  return data
}

async function inspectZIP(options: { file: UploadFileInfo }) {
  if (!options.file.file)
    return
  file.value = options.file.file
  preview.value = null
  busy.value = true
  error.value = ''
  try {
    preview.value = await request('inspect', uploadData())
    show.value = true
  }
  catch (err) { error.value = err instanceof Error ? err.message : 'ZIP 检查失败' }
  finally { busy.value = false }
}

async function importZIP() {
  if (!preview.value || !file.value)
    return
  busy.value = true
  error.value = ''
  try {
    await request('import', uploadData())
    show.value = false
    file.value = null
    preview.value = null
    ms.success('首页 ZIP 已导入，刷新首页即可查看')
  }
  catch (err) { error.value = err instanceof Error ? err.message : '导入失败' }
  finally { busy.value = false }
}
</script>

<template>
  <NSpace vertical :size="16" class="p-4">
    <strong>完整首页 ZIP</strong>
    <span>导出当前账号的分组、网站和使用中的本地图片；外部图片和在线图标保留原链接。</span>
    <NAlert v-if="error" type="error" :show-icon="false">
      {{ error }}
    </NAlert>
    <NSpace>
      <NButton type="primary" :loading="busy" @click="exportZIP">
        导出 ZIP（含图片）
      </NButton>
      <NUpload accept=".zip,application/zip" :default-upload="false" :show-file-list="false" :disabled="busy" @change="inspectZIP">
        <NButton :disabled="busy">
          导入 ZIP（含图片）
        </NButton>
      </NUpload>
    </NSpace>
    <span>ZIP 内容上限 64 MiB，单张图片最多 10 MiB。原 JSON 导入导出继续可用。</span>
    <NModal v-model:show="show" preset="card" title="预览首页 ZIP" style="width: min(560px, calc(100vw - 24px)); border-radius: 1rem" :mask-closable="!busy" :closable="!busy">
      <NSpace vertical :size="16">
        <NAlert v-if="error" type="error" :show-icon="false">
          {{ error }}
        </NAlert>
        <p v-if="preview">
          {{ preview.groups }} 个分组 · {{ preview.items }} 个网站 · {{ preview.images }} 张本地图片
        </p>
        <p v-if="preview?.externalImages">
          {{ preview.externalImages }} 个图标仍依赖外部链接或在线服务。
        </p>
        <p>导入会追加分组和网站，保留当前桌面。保存失败时撤回本次导入的分组与图片。</p>
        <NSpace justify="end">
          <NButton :disabled="busy" @click="show = false">
            取消
          </NButton>
          <NButton type="primary" :loading="busy" @click="importZIP">
            确认导入
          </NButton>
        </NSpace>
      </NSpace>
    </NModal>
  </NSpace>
</template>
