<template>
  <div class="icons-page">
    <div class="page-topbar icons-heading">
      <div>
        <h1 class="page-title">图标库</h1>
        <p class="page-subtitle">为反代服务查找和管理本地图标</p>
      </div>
      <div class="icons-actions">
        <button class="btn btn-secondary" type="button" @click="openImport"><Github :size="15" />从 GitHub 导入</button>
        <button class="btn btn-primary" type="button" @click="openUpload"><Upload :size="15" />上传本地图标</button>
      </div>
    </div>

    <dl class="icons-stats">
      <div>
        <dt>图标总数</dt>
        <dd>{{ stats ? stats.total.toLocaleString() : '—' }} <small v-if="stats">{{ formatBytes(stats.bytes) }}</small></dd>
      </div>
      <div v-for="[value, label] in sources" :key="value">
        <dt>{{ label }}</dt>
        <dd>{{ stats ? (stats.sources[value] || 0).toLocaleString() : '—' }}</dd>
      </div>
    </dl>
    <div v-if="statsError" class="icon-error" role="alert">{{ statsError }}</div>
    <div v-if="job.state !== 'idle'" class="icons-job" role="status">
      <span><strong>{{ job.message }}</strong><template v-if="job.state === 'completed'"> · {{ job.count }} 个</template></span>
      <progress v-if="job.state === 'running'" :value="job.progress" max="100" aria-label="GitHub 导入进度" />
    </div>

    <section class="icons-catalog" aria-label="查找图标">
      <div class="icons-toolbar">
        <label class="icon-search">
          <Search :size="16" aria-hidden="true" />
          <input
            v-model="filters.keyword" type="search" maxlength="80" aria-label="搜索图标"
            placeholder="搜索英文名、中文名或拼音" @keydown.esc="filters.keyword = ''"
          />
        </label>
        <button type="button" class="btn btn-secondary" :disabled="reindexing" @click="reindex">
          <RefreshCw :size="15" />{{ reindexing ? '重建中…' : '重建索引' }}
        </button>
      </div>
      <div class="icons-filter-row">
        <span>来源</span>
        <div class="icon-filters" role="group" aria-label="来源筛选">
          <button type="button" class="icon-chip" :aria-pressed="!filters.source" @click="filters.source = ''">全部</button>
          <button
            v-for="[value, label] in sources" :key="value" type="button" class="icon-chip"
            :aria-pressed="filters.source === value" @click="filters.source = value"
          >
            {{ label }}<span v-if="stats"> {{ stats.sources[value] || 0 }}</span>
          </button>
        </div>
      </div>
      <div class="icons-filter-row">
        <span>形态</span>
        <div class="icon-filters" role="group" aria-label="形态筛选">
          <button type="button" class="icon-chip" :aria-pressed="!filters.style" @click="filters.style = ''">全部</button>
          <button
            v-for="[value, label] in styles" :key="value" type="button" class="icon-chip"
            :aria-pressed="filters.style === value" @click="filters.style = value"
          >{{ label }}</button>
        </div>
      </div>

      <div class="icons-results-bar">
        <span role="status">{{ loading ? '正在读取图标…' : `共 ${total.toLocaleString()} 个图标` }}</span>
        <div class="icon-filters" role="group" aria-label="网格密度">
          <button type="button" class="icon-chip" :aria-pressed="!standard" @click="standard = false">紧凑</button>
          <button type="button" class="icon-chip" :aria-pressed="standard" @click="standard = true">标准</button>
        </div>
      </div>
      <p v-if="error" class="icon-error" role="alert">{{ error }}</p>
      <p v-else-if="!items.length && !loading" class="icon-empty">没有符合条件的图标</p>
      <div class="icons-grid" :class="{ standard }" :aria-busy="loading">
        <button
          v-for="icon in items" :key="icon.id" class="icon-tile" type="button" :disabled="loading"
          :title="[icon.name, icon.cnName, icon.format.toUpperCase(), '/icons/' + icon.relPath].filter(Boolean).join(' · ')"
          @click="showDetail(icon)"
        >
          <img :src="icon.url" width="40" height="40" alt="" loading="lazy" />
          <span>{{ icon.name }}</span>
        </button>
      </div>
      <div v-if="pageCount > 1" class="icons-pages">
        <button class="btn btn-secondary" type="button" :disabled="page <= 1 || loading" @click="page--">上一页</button>
        <span>{{ page }} / {{ pageCount }}</span>
        <button class="btn btn-secondary" type="button" :disabled="page >= pageCount || loading" @click="page++">下一页</button>
      </div>
      <div class="icons-storage"><span>存储路径</span><code class="icon-path">{{ stats?.path || '/icons/' }}</code></div>
    </section>

    <dialog ref="importDialog" class="icon-form-dialog" aria-labelledby="icon-import-title" @cancel="blockClose">
      <header class="icon-dialog-header">
        <h2 id="icon-import-title">从 GitHub 导入</h2>
        <button type="button" class="icon-close" aria-label="关闭导入" :disabled="formBusy" @click="importDialog.close()"><X :size="18" /></button>
      </header>
      <form class="icon-form" @submit.prevent="submitImport">
        <label class="form-field">
          <span>GitHub 仓库</span>
          <input v-model.trim="repository" class="form-input" type="url" required />
        </label>
        <label class="form-field">
          <span>下载方式</span>
          <select v-model="mirror" class="form-select">
            <option value="direct">GitHub 直连</option>
            <option value="ghproxy">GH-Proxy 加速</option>
            <option value="custom">自定义镜像</option>
          </select>
        </label>
        <p v-if="mirror === 'custom'" class="icon-form-hint">镜像地址由服务器的 <code>GATEHOME_ICON_IMPORT_MIRROR</code> 设置提供。</p>
        <p v-if="formError" class="icon-error" role="alert">{{ formError }}</p>
        <p v-if="job.state === 'running'" class="icon-form-hint">{{ job.message }} · {{ job.progress }}%</p>
        <div class="icon-form-actions">
          <button class="btn btn-secondary" type="button" :disabled="formBusy" @click="importDialog.close()">取消</button>
          <button class="btn btn-primary" type="submit" :disabled="formBusy || job.state === 'running'">开始导入</button>
        </div>
      </form>
    </dialog>

    <dialog ref="uploadDialog" class="icon-form-dialog" aria-labelledby="icon-upload-title" @cancel="blockClose">
      <header class="icon-dialog-header">
        <h2 id="icon-upload-title">上传本地图标</h2>
        <button type="button" class="icon-close" aria-label="关闭上传" :disabled="formBusy" @click="uploadDialog.close()"><X :size="18" /></button>
      </header>
      <form class="icon-form" @submit.prevent="submitUpload">
        <div
          class="icon-drop" :class="{ 'is-dragging': dragging }" @dragover.prevent="dragging = !formBusy"
          @dragleave="dragging = false" @drop.prevent="dropFiles"
        >
          <Upload :size="20" aria-hidden="true" />
          <span>将图片拖到这里</span>
          <label class="icon-file-label">
            <span>选择文件</span>
            <input
              ref="fileInput" type="file" aria-label="选择图标文件" multiple :disabled="formBusy"
              accept=".png,.jpg,.jpeg,.gif,.webp,.ico,.svg" @change="chooseFiles($event.target.files)"
            />
          </label>
        </div>
        <p class="icon-form-hint">每次最多 16 张，每张不超过 5 MiB。建议使用 512 × 512 或更大的图片。</p>
        <ul v-if="files.length" class="icon-upload-list" aria-label="待上传文件">
          <li v-for="(file, i) in files" :key="i"><span>{{ file.name }}</span><small>{{ formatBytes(file.size) }}</small></li>
        </ul>
        <p v-if="formError" class="icon-error" role="alert">{{ formError }}</p>
        <div class="icon-form-actions">
          <button class="btn btn-secondary" type="button" :disabled="formBusy" @click="uploadDialog.close()">取消</button>
          <button class="btn btn-primary" type="submit" :disabled="formBusy || !files.length">{{ formBusy ? '上传中…' : '上传图标' }}</button>
        </div>
      </form>
    </dialog>

    <dialog ref="detailDialog" class="icon-detail-dialog" aria-labelledby="icon-detail-title" @cancel="blockClose">
      <header class="icon-dialog-header">
        <h2 id="icon-detail-title">图标详情</h2>
        <button type="button" class="icon-close" aria-label="关闭详情" :disabled="formBusy" @click="detailDialog.close()"><X :size="18" /></button>
      </header>
      <div v-if="detail" class="icon-detail-content">
        <div class="icon-detail-preview">
          <img :src="detail.url" width="88" height="88" alt="" />
          <h3>{{ detail.name }}</h3>
          <p v-if="detail.cnName">{{ detail.cnName }}</p>
        </div>
        <dl class="icon-detail-data">
          <div><dt>来源</dt><dd>{{ detail.pack || iconSources[detail.source] }}</dd></div>
          <div><dt>形态</dt><dd>{{ iconStyles[detail.style] }}</dd></div>
          <div><dt>格式</dt><dd>{{ detail.format.toUpperCase() }} · {{ formatBytes(detail.bytes) }}</dd></div>
          <div v-if="detail.width && detail.height"><dt>分辨率</dt><dd>{{ detail.width }} × {{ detail.height }}</dd></div>
          <div><dt>存储路径</dt><dd><code class="icon-path">/icons/{{ detail.relPath }}</code></dd></div>
        </dl>
        <label class="form-field">
          <span>访问 URL</span>
          <input ref="detailURL" class="form-input" :value="absoluteURL(detail.url)" readonly @focus="$event.target.select()" />
        </label>
        <button class="btn btn-secondary" type="button" @click="copyURL"><Copy :size="14" />复制访问 URL</button>
        <p v-if="formError" class="icon-error" role="alert">{{ formError }}</p>
        <div v-if="detail.source === 'uploads'" class="icon-detail-delete">
          <p v-if="confirmDelete">永久删除此图标？</p>
          <button v-if="confirmDelete" class="btn btn-secondary" type="button" :disabled="formBusy" @click="confirmDelete = false">取消</button>
          <button class="btn btn-danger" type="button" :disabled="formBusy" @click="deleteIcon">
            <Trash2 :size="14" />{{ confirmDelete ? '确认删除' : '删除图标' }}
          </button>
        </div>
      </div>
    </dialog>
  </div>
</template>

<script setup>
import { onMounted, onUnmounted, ref } from 'vue'
import { Copy, Github, RefreshCw, Search, Trash2, Upload, X } from 'lucide-vue-next'
import { iconsApi, iconSources, iconStyles } from '@/api/icons'
import { formatBytes } from '@/api/reports'
import { useIconSearch } from '@/composables/useIconSearch'
import { useToast } from '@/composables/useToast'
import '@/styles/icons.css'

const { filters, items, total, page, pageCount, loading, error, refresh } = useIconSearch(72)
const sources = Object.entries(iconSources)
const styles = Object.entries(iconStyles)
const stats = ref(null)
const statsError = ref('')
const standard = ref(false)
const reindexing = ref(false)
const job = ref({ state: 'idle' })
const importDialog = ref(null)
const uploadDialog = ref(null)
const detailDialog = ref(null)
const fileInput = ref(null)
const detailURL = ref(null)
const repository = ref('https://github.com/xushier/HD-Icons')
const mirror = ref('direct')
const files = ref([])
const dragging = ref(false)
const formBusy = ref(false)
const formError = ref('')
const detail = ref(null)
const confirmDelete = ref(false)
const toast = useToast()
const lifetime = new AbortController()
let pollTimer
let statsRequest
let pollRequest

onMounted(() => {
  loadStats()
  pollImport()
})
onUnmounted(() => {
  lifetime.abort()
  statsRequest?.abort()
  pollRequest?.abort()
  clearTimeout(pollTimer)
})

async function loadStats() {
  if (lifetime.signal.aborted) return
  statsRequest?.abort()
  const controller = new AbortController()
  statsRequest = controller
  try {
    const result = await iconsApi.stats(controller.signal)
    if (controller.signal.aborted) return
    stats.value = result
    statsError.value = ''
  } catch (err) {
    if (!controller.signal.aborted) statsError.value = err.message
  }
}

async function pollImport() {
  if (lifetime.signal.aborted) return
  clearTimeout(pollTimer)
  pollRequest?.abort()
  const controller = new AbortController()
  pollRequest = controller
  try {
    const result = await iconsApi.importStatus(controller.signal)
    if (controller.signal.aborted) return
    const wasRunning = job.value.state === 'running'
    job.value = result
    if (wasRunning && result.state === 'completed') {
      refresh()
      loadStats()
    }
  } catch (err) {
    if (!controller.signal.aborted && job.value.state !== 'running') toast.error(err.message)
  }
  if (!controller.signal.aborted && job.value.state === 'running') pollTimer = setTimeout(pollImport, 1000)
}

function blockClose(event) {
  if (formBusy.value) event.preventDefault()
}

function openImport() {
  formError.value = ''
  importDialog.value.showModal()
}

function openUpload() {
  formError.value = ''
  files.value = []
  fileInput.value.value = ''
  dragging.value = false
  uploadDialog.value.showModal()
}

function showDetail(icon) {
  formError.value = ''
  confirmDelete.value = false
  detail.value = icon
  detailDialog.value.showModal()
}

async function submitImport() {
  formBusy.value = true
  formError.value = ''
  try {
    job.value = await iconsApi.importGitHub({ repository: repository.value, mirror: mirror.value })
    importDialog.value?.close()
    pollImport()
  } catch (err) {
    formError.value = err.message
  } finally {
    formBusy.value = false
  }
}

function chooseFiles(selection) {
  const selected = Array.from(selection)
  if (selected.length > 16 || selected.some(file => file.size > 5 * 1024 * 1024)) {
    formError.value = '每次最多上传 16 张图标，每张不超过 5 MiB。'
    files.value = []
    return
  }
  files.value = selected
  formError.value = ''
}

function dropFiles(event) {
  dragging.value = false
  if (!formBusy.value) {
    fileInput.value.value = ''
    chooseFiles(event.dataTransfer.files)
  }
}

async function submitUpload() {
  formBusy.value = true
  formError.value = ''
  try {
    const result = await iconsApi.upload(files.value)
    uploadDialog.value?.close()
    filters.keyword = ''
    filters.style = ''
    filters.source = 'uploads'
    page.value = 1
    refresh()
    loadStats()
    toast.success(`已上传 ${result.items.length} 张图标`)
  } catch (err) {
    formError.value = err.message
  } finally {
    formBusy.value = false
  }
}

async function reindex() {
  reindexing.value = true
  try {
    await iconsApi.reindex()
    refresh()
    loadStats()
    toast.success('图标索引已更新')
  } catch (err) {
    toast.error(err.message)
  } finally {
    reindexing.value = false
  }
}

async function deleteIcon() {
  if (!confirmDelete.value) {
    confirmDelete.value = true
    return
  }
  formBusy.value = true
  formError.value = ''
  try {
    await iconsApi.remove(detail.value.id)
    detailDialog.value?.close()
    refresh()
    loadStats()
    toast.success('图标已删除')
  } catch (err) {
    formError.value = err.message
  } finally {
    formBusy.value = false
  }
}

function absoluteURL(url) {
  return new URL(url, window.location.origin).href
}

async function copyURL() {
  try {
    if (navigator.clipboard) {
      await navigator.clipboard.writeText(detailURL.value.value)
    } else {
      detailURL.value.select()
      if (!document.execCommand('copy')) throw new Error('copy failed')
    }
    toast.success('访问 URL 已复制')
  } catch {
    detailURL.value.focus()
    detailURL.value.select()
    formError.value = '请使用系统复制快捷键复制已选中的地址。'
  }
}
</script>

<style scoped>
.icons-page { max-width: 1140px; margin: 0 auto; display: grid; gap: 14px; }
.icons-heading {
  display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between;
  gap: 10px; margin-bottom: 0;
}
.icons-actions, .icons-toolbar, .icons-pages { display: flex; align-items: center; gap: 8px; }
.icons-actions { flex-wrap: wrap; }
.icons-stats {
  display: grid; grid-template-columns: repeat(4, minmax(0, 1fr));
  background: var(--card-bg); border: 1px solid var(--line); border-radius: var(--radius-md);
}
.icons-stats > div { min-width: 0; padding: 10px 16px; }
.icons-stats > div + div { border-left: 1px solid var(--line); }
.icons-stats dt { font-size: 12px; color: var(--muted); }
.icons-stats dd { font-size: 24px; font-weight: 500; font-variant-numeric: tabular-nums; }
.icons-stats small { font-size: 11px; color: var(--muted); white-space: nowrap; }
.icons-job {
  display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px;
  padding: 8px 12px; border: 1px solid var(--line); border-radius: var(--radius-sm); color: var(--muted);
}
.icons-job progress { max-width: 100%; accent-color: var(--brand); }
.icons-catalog { display: grid; gap: 8px; }
.icons-toolbar .icon-search { flex: 1; }
.icons-filter-row { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 8px; align-items: start; }
.icons-filter-row .icon-chip > span { margin-inline-start: 4px; font-variant-numeric: tabular-nums; }
.icons-filter-row > span { color: var(--muted); font-size: 12px; padding-top: 5px; }
.icons-results-bar {
  display: flex; align-items: center; justify-content: space-between; gap: 8px;
  color: var(--muted); font-size: 12px; margin-top: 4px; border-top: 1px solid var(--line); padding-top: 8px;
}
.icons-grid { display: grid; grid-template-columns: repeat(12, minmax(0, 1fr)); gap: 6px; }
.icons-grid .icon-tile { min-height: 65px; padding: 5px; gap: 3px; transition: transform 120ms, border-color 120ms; }
.icons-grid .icon-tile:hover { transform: translateY(-2px); }
.icons-grid.standard { grid-template-columns: repeat(8, minmax(0, 1fr)); }
.icons-grid.standard .icon-tile { min-height: 82px; }
.icons-pages { justify-content: center; color: var(--muted); }
.icons-storage { display: flex; align-items: center; gap: 6px; color: var(--muted); font-size: 12px; padding-top: 4px; }
.icon-form-dialog, .icon-detail-dialog {
  color: var(--ink); background: var(--popover-bg); border: 1px solid var(--line);
  border-radius: var(--radius-md); padding: 0; overflow: auto;
}
.icon-form-dialog { width: min(470px, calc(100vw - 24px)); margin: auto; max-height: 88dvh; }
.icon-detail-dialog {
  inset: 0 0 0 auto; margin: 0; width: min(390px, 100vw);
  height: 100dvh; max-height: none; max-width: none; border-radius: 0;
}
.icon-form { display: grid; gap: 12px; }
.icon-form-hint { font-size: 12px; color: var(--muted); overflow-wrap: anywhere; }
.icon-form-actions { display: flex; justify-content: flex-end; gap: 8px; }
.icon-drop {
  padding: 16px; min-height: 100px; display: grid; justify-items: center; gap: 4px;
  border: 1px dashed var(--faint); border-radius: var(--radius-sm); color: var(--muted);
}
.icon-drop.is-dragging { border-color: var(--brand); background: var(--spotlight-color); }
.icon-file-label { position: relative; display: grid; place-items: center; min-height: 34px; cursor: pointer; color: var(--ink); text-decoration: underline; }
.icon-file-label:focus-within { outline: 2px solid var(--brand); outline-offset: 2px; }
.icon-file-label input { position: absolute; inset: 0; opacity: 0; cursor: pointer; width: 100%; }
.icon-upload-list { list-style: none; max-height: 132px; overflow: auto; }
.icon-upload-list li { display: flex; justify-content: space-between; gap: 12px; padding: 4px; }
.icon-upload-list li span { overflow-wrap: anywhere; }
.icon-upload-list small { color: var(--muted); white-space: nowrap; }
.icon-detail-content { padding: 16px; display: grid; gap: 12px; }
.icon-detail-content > .btn { justify-self: start; }
.icon-detail-preview { display: grid; justify-items: center; gap: 4px; padding: 12px; color: var(--muted); text-align: center; overflow-wrap: anywhere; }
.icon-detail-preview img { object-fit: contain; margin-bottom: 6px; }
.icon-detail-preview h3 { color: var(--ink); }
.icon-detail-data { display: grid; gap: 6px; }
.icon-detail-data > div { display: grid; grid-template-columns: 64px minmax(0, 1fr); gap: 8px; }
.icon-detail-data dt { color: var(--muted); }
.icon-detail-data dd { overflow-wrap: anywhere; }
.icon-detail-delete { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; padding-top: 12px; border-top: 1px solid var(--line); }

@media (max-width: 1240px) {
  .icons-grid { grid-template-columns: repeat(10, minmax(0, 1fr)); }
}
@media (max-width: 820px) {
  .icons-grid { grid-template-columns: repeat(8, minmax(0, 1fr)); }
  .icons-grid.standard { grid-template-columns: repeat(6, minmax(0, 1fr)); }
}
@media (max-width: 600px) {
  .icons-page { gap: 10px; }
  .icons-stats { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .icons-stats > div:nth-child(3) { border-left: 0; }
  .icons-stats > div:nth-child(n+3) { border-top: 1px solid var(--line); }
  .icons-stats > div { padding: 8px 10px; }
  .icons-stats dd { font-size: 20px; }
  .icons-grid { grid-template-columns: repeat(5, minmax(0, 1fr)); }
  .icons-grid.standard { grid-template-columns: repeat(4, minmax(0, 1fr)); }
  .icons-actions .btn, .icons-toolbar .btn, .icons-pages .btn, .icon-form-actions .btn { min-height: 44px; }
  .icon-detail-dialog { width: min(390px, 100vw) !important; max-height: 100dvh !important; border-radius: 0 !important; }
  .icon-detail-content { padding-bottom: max(16px, env(safe-area-inset-bottom)); }
}
@media (prefers-reduced-motion: reduce) {
  .icons-grid .icon-tile { transition: none; }
  .icons-grid .icon-tile:hover { transform: none; }
}
</style>
