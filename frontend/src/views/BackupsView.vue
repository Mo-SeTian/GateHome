<template>
  <div class="report-page animate-fade-in">
    <header class="report-heading">
      <h1>备份与恢复</h1>
      <button class="btn btn-secondary" :disabled="!!busy || loading" @click="reload">重新读取</button>
    </header>
    <p v-if="error" class="report-error" role="alert">{{ error }}</p>
    <p v-if="pollError" class="report-error" role="alert">
      WebDAV 状态读取失败{{ remote ? '，保留上次结果' : '' }}：{{ pollError }}
    </p>
    <p v-if="maintenance && !maintenance.can_apply" class="report-warning">
      当前部署可导出和检查备份，执行恢复需要支持维护的 Linux 或 Docker 启动器。
    </p>
    <BackupCapacity
      :capacity="capacity"
      :error="capacityError"
      :loading="capacityLoading"
      @refresh="readCapacity"
    />
    <div class="backup-grid">
      <SpotlightCard class="report-panel"
        ><h2>下载本地备份</h2>
        <p class="report-note">
          包含配置与凭据、证书、图片、SunPanel 数据、Mihomo 代理分流配置、日志和订阅缓存。登录会话及历史流量统计不在 ZIP
          内；完整迁机请另存四个持久化目录。
        </p>
        <form @submit.prevent="exportBackup">
          <fieldset :disabled="!!busy">
            <SecretInput id="local-backup-password" name="backup_password" label="备份密码" :maxlength="null"
              secret-name="密码" v-model="backupPassword" placeholder="留空使用默认加密" />
            <SecretInput id="local-backup-confirm-password" name="confirm_password" label="再次输入密码" :maxlength="null"
              secret-name="密码" v-model="confirmPassword" placeholder="" />
            <p class="report-note">
              ZIP 内容使用 AES-256-GCM
              加密。密码可留空，恢复时须使用相同密码；文件直接下载到浏览器，请自行保存。
            </p>
            <button class="btn btn-primary" :disabled="!capacity?.can_proceed" type="submit">
              {{ busy === 'export' ? '正在打包…' : '下载备份 ZIP' }}
            </button>
          </fieldset>
        </form></SpotlightCard
      >
      <SpotlightCard class="report-panel"
        ><h2>检查本地备份</h2>
        <form @submit.prevent="inspectLocal">
          <fieldset :disabled="!!busy">
            <label
              >备份 ZIP<input
                name="backup_file"
                type="file"
                accept=".zip,application/zip"
                required
                @change="selectFile" /></label
            >
            <div v-if="file" class="selected-file-pill">
              <span>已选择备份文件：{{ file.name }}</span>
            </div>
            <SecretInput id="local-restore-password" name="restore_password" label="导出时的备份密码" :maxlength="null"
              secret-name="密码" v-model="restorePassword" autocomplete="off" placeholder="" @update:model-value="preview = null" />
            <p class="report-note">先验证密码、格式和端口，再确认恢复；预检不会替换运行数据。</p>
            <button class="btn btn-secondary" type="submit">
              {{ busy === 'inspect' ? '正在检查…' : '检查备份' }}
            </button>
          </fieldset>
        </form></SpotlightCard
      >
    </div>
    <MaintenancePreview
      v-if="preview"
      :preview="preview"
      kind="restore"
      :disabled="!!busy || !maintenance?.can_apply || !capacity?.can_proceed || gatewayStatus?.maintenance_busy"
      :busy="busy === 'restore'"
      @apply="restore"
    />
    <SpotlightCard class="report-panel" v-if="draft">
      <div class="report-heading">
        <h2>WebDAV 与定时备份</h2>
        <span class="badge" :class="remote?.busy ? 'badge-brand' : 'badge-neutral'">{{
          maintenancePhase(remote?.job?.phase)
        }}</span>
      </div>
      <form @submit.prevent="saveRemote">
        <fieldset :disabled="!!busy || remote?.busy">
          <div class="report-form webdav-form">
            <div class="wide webdav-address">
              <label>
                WebDAV 目录地址
                <input
                  v-model.trim="draft.url"
                  name="webdav_url"
                  type="url"
                  class="form-input"
                  placeholder="https://storage.example.com/dav/GateHome/"
                  maxlength="2048"
                />
              </label>
              <button ref="browseButton" class="btn btn-secondary" type="button" :disabled="!draft.url" @click="openDirectories">
                选择目录
              </button>
            </div>
            <label class="credential-field"
              >账号<input
                v-model="draft.username"
                name="webdav_username"
                class="form-input"
                maxlength="255"
                autocomplete="off"
            /></label>
            <SecretInput :key="`storage-${snapshot.revision}`" id="webdav-storage-password"
              name="webdav_password" class="credential-field" :maxlength="null"
              label="存储密码" secret-name="密码" kind="webdav" v-model="secrets.password"
              :configured="!!remote.password_configured"
              :placeholder="remote.password_configured ? '已保存，点击小眼睛查看' : '填写 WebDAV 密码'" />
            <p class="report-note wide">
              同一账号可在当前目录及其子目录使用已保存密码。更换位置或账号时，请按需重新填写。
            </p>
            <SecretInput :key="`encryption-${snapshot.revision}`" id="webdav-encryption-password"
              name="webdav_backup_password" :maxlength="null"
              label="备份加密密码" secret-name="密码" kind="webdav-backup" v-model="secrets.backupPassword"
              :configured="!!remote.backup_password_configured"
              :placeholder="remote.backup_password_configured ? '已保存，点击小眼睛查看' : '远程备份必填'" />
            <SecretInput id="webdav-encryption-confirm-password" name="webdav_confirm_password" :maxlength="null"
              label="再次输入加密密码" secret-name="密码" v-model="secrets.confirmPassword" placeholder="" />
            <label
              >保留份数<input
                v-model.number="draft.keep"
                name="keep"
                class="form-input"
                type="number"
                min="1"
                max="100"
                required
            /></label>
            <label class="toggle-row wide">
              <span>启用定时备份</span>
              <input
                v-model="draft.schedule_enabled"
                name="schedule_enabled"
                type="checkbox"
              />
            </label>
            <div class="wide schedule-fields">
              <label>
                频率<select v-model="draft.schedule_mode" name="schedule_mode" class="form-select">
                  <option value="daily">每天</option>
                  <option value="weekly">每周</option>
                </select>
              </label>
              <label v-if="draft.schedule_mode === 'weekly'">
                每周日期<select v-model.number="draft.weekday" name="weekday" class="form-select">
                  <option v-for="(day, index) in weekdays" :key="index" :value="index">{{ day }}</option>
                </select>
              </label>
              <label>
                时间<input v-model="draft.time" name="time" class="form-input" type="time" required />
              </label>
              <label>
                时区<input
                  v-model.trim="draft.timezone"
                  name="timezone"
                  class="form-input"
                  required
                  placeholder="Asia/Shanghai"
                />
              </label>
            </div>
            <p class="report-note wide">
              仅在新文件上传并校验成功后，清理本实例超出保留份数的备份。旧备份仍需原加密密码。
            </p>
            <div class="report-actions wide">
              <button class="btn btn-secondary" type="button" :disabled="!draft.url" @click="testRemote">
                {{ busy === 'test' ? '测试中…' : '测试连接' }}
              </button>
              <button class="btn btn-primary" type="submit">
                {{ busy === 'save' ? '保存中…' : '保存 WebDAV 设置' }}
              </button>
            </div>
          </div>
        </fieldset>
      </form>
      <div class="report-actions">
        <button
          class="btn btn-secondary"
          :disabled="!!busy || remote?.busy || !remote?.config.url || !capacity?.can_proceed"
          @click="runRemote"
        >
          立即远程备份
        </button>
      </div>
      <p class="report-note" role="status">
        最近尝试：{{ reportDate(remote.last_attempt) }} · 最近成功：{{ reportDate(remote.last_success)
        }}<br />下次计划：{{ reportDate(remote.next_run) }} · {{ remote.config.timezone }}<br />最近文件：{{
          remote.last_file || '—'
        }}
      </p>
      <p v-if="remote.last_error" class="report-error" role="alert">{{ remote.last_error }}</p>
    </SpotlightCard>
    <dialog ref="directoryDialog" class="modal-dialog webdav-directory-dialog" aria-labelledby="webdav-directory-title" @close="closeDirectories">
      <header class="modal-header">
        <h2 id="webdav-directory-title">选择 WebDAV 目录</h2>
        <button class="btn btn-secondary btn-sm" type="button" autofocus @click="directoryDialog.close()">取消</button>
      </header>
      <div class="modal-body webdav-directories" :aria-busy="directoryLoading">
        <div class="report-actions">
          <button type="button" class="btn btn-secondary btn-sm" :disabled="directoryLoading || directoryHistory.length < 2" @click="parentDirectory">
            上一级
          </button>
          <code ref="directoryLocation" tabindex="-1">{{ currentDirectory?.url || draft?.url }}</code>
        </div>
        <p v-if="directoryError" class="report-error" role="alert">{{ directoryError }}</p>
        <p v-if="directoryLoading" role="status">正在读取目录…</p>
        <ul v-else-if="currentDirectory?.directories.length">
          <li v-for="directory in currentDirectory.directories" :key="directory.url">
            <button type="button" class="btn btn-secondary directory-entry" @click="readDirectories(directory.url)">
              <FolderClosed :size="16" aria-hidden="true" />
              <span>{{ directory.name }}</span>
              <ChevronRight :size="15" aria-hidden="true" />
            </button>
          </li>
        </ul>
        <p v-else-if="currentDirectory" class="report-note">此目录没有子目录</p>
      </div>
      <footer class="modal-footer">
        <button type="button" class="btn btn-primary" :disabled="directoryLoading || !currentDirectory" @click="selectDirectory">
          使用此目录
        </button>
      </footer>
    </dialog>
    <SpotlightCard class="report-panel"
      ><div class="report-heading">
        <h2>远程备份</h2>
        <button
          class="btn btn-secondary"
          :disabled="listing || !!busy || !remote?.config.url"
          @click="readFiles"
        >
          {{ listing ? '读取中…' : '读取远程目录' }}
        </button>
      </div>
      <p v-if="listError" class="report-error" role="alert">
        {{ listError }}{{ files.length ? '，仍显示上次目录结果' : '' }}
      </p>
      <div class="report-form">
        <label
          >检查恢复时的密码<select
            v-model="remotePasswordMode"
            name="remote_password_mode"
            class="form-select"
            @change="preview = null"
          >
            <option value="stored">使用已保存的备份加密密码</option>
            <option value="custom">使用该备份的其他密码</option>
          </select></label
        >
        <SecretInput v-if="remotePasswordMode === 'custom'" id="webdav-restore-password" :maxlength="null"
          name="remote_restore_password" label="备份密码（支持空密码）" secret-name="密码" class="wide"
          v-model="remotePassword" autocomplete="off" placeholder="" @update:model-value="preview = null" />
      </div>
      <ReportTableScroll v-if="files.length" label="WebDAV 备份列表">
        <table class="report-table">
          <thead>
            <tr>
              <th>备份文件</th>
              <th>时间</th>
              <th>大小</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="file in files" :key="file.name" :data-backup-name="file.name">
              <td>{{ file.name }}</td>
              <td>{{ reportDate(file.created_at) }}</td>
              <td>{{ formatBytes(file.size) }}</td>
              <td>
                <div class="report-actions">
                  <button class="btn btn-secondary btn-sm" :disabled="!!busy" @click="downloadRemote(file)">
                    下载</button
                  ><button class="btn btn-secondary btn-sm" :disabled="!!busy" @click="inspectRemote(file)">
                    检查恢复
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </ReportTableScroll>
      <p v-else class="report-empty">{{ listing ? '正在读取目录…' : '尚未读取到远程 GateHome 备份' }}</p>
    </SpotlightCard>
  </div>
</template>
<script setup>
import ReportTableScroll from '@/components/ReportTableScroll.vue'
import { computed, nextTick, onMounted, onBeforeUnmount, reactive, ref } from 'vue'
import { ChevronRight, FolderClosed } from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import SpotlightCard from '@/components/SpotlightCard.vue'
import SecretInput from '@/components/SecretInput.vue'
import BackupCapacity from '@/components/BackupCapacity.vue'
import MaintenancePreview from '@/components/MaintenancePreview.vue'
import { adminApi } from '@/api/admin'
import { maintenanceApi, webDAVConnectionPayload, webDAVPayload, maintenancePhase, downloadBackup } from '@/api/maintenance'
import { formatBytes, reportDate } from '@/api/reports'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useSession } from '@/composables/useSession'
import { useToast } from '@/composables/useToast'
import '@/styles/reports.css'
const { gatewayStatus, refreshStatus, refreshConfig } = useGatewayStatus(),
  { endSession } = useSession()
const router = useRouter(),
  toast = useToast(),
  controller = new AbortController()
const maintenance = ref(null),
  capacity = ref(null),
  capacityError = ref(''),
  capacityLoading = ref(false),
  loading = ref(false),
  busy = ref(''),
  error = ref('')
const backupPassword = ref(''),
  confirmPassword = ref(''),
  restorePassword = ref(''),
  file = ref(null),
  preview = ref(null)
const remote = ref(null),
  snapshot = ref(null),
  draft = ref(null),
  files = ref([]),
  listing = ref(false),
  listError = ref(''),
  pollError = ref(''),
  remotePasswordMode = ref('stored'),
  remotePassword = ref('')
const secrets = reactive({ password: undefined, backupPassword: undefined, confirmPassword: '' })
const browseButton = ref(null)
const directoryDialog = ref(null)
const directoryLocation = ref(null)
const directoryLoading = ref(false)
const directoryError = ref('')
const directoryHistory = ref([])
const currentDirectory = computed(() => directoryHistory.value.at(-1))
let directoryRequest
const weekdays = ['星期日', '星期一', '星期二', '星期三', '星期四', '星期五', '星期六']
let timer,
  remoteGeneration = 0,
  listGeneration = 0
function clearSecrets() {
  Object.assign(secrets, { password: undefined, backupPassword: undefined, confirmPassword: '' })
}
async function action(name, fn) {
  if (busy.value) return
  busy.value = name
  error.value = ''
  try {
    await fn()
  } catch (err) {
    if (!controller.signal.aborted) error.value = err.message
  } finally {
    busy.value = ''
  }
}
async function readCapacity() {
  if (capacityLoading.value) return
  capacityLoading.value = true
  capacityError.value = ''
  try {
    const result = await maintenanceApi.capacity(controller.signal)
    if (!controller.signal.aborted) capacity.value = result
  } catch (err) {
    if (!controller.signal.aborted) {
      capacity.value = null
      capacityError.value = err.message
    }
  } finally {
    capacityLoading.value = false
  }
}
async function readRemote(reset = false) {
  const generation = ++remoteGeneration
  clearTimeout(timer)
  try {
    const result = await maintenanceApi.webdav(controller.signal)
    if (controller.signal.aborted || generation !== remoteGeneration) return
    const completed = remote.value?.busy && !result.busy
    remote.value = result
    pollError.value = ''
    if (reset) {
      snapshot.value = result
      draft.value = { ...result.config }
      clearSecrets()
    }
    if (completed && result.config.url) readFiles()
  } catch (err) {
    if (!controller.signal.aborted && generation === remoteGeneration) pollError.value = err.message
  } finally {
    if (!controller.signal.aborted && generation === remoteGeneration && !document.hidden)
      timer = setTimeout(() => readRemote(), remote.value?.busy ? 1500 : 5000)
  }
}
async function readFiles() {
  const generation = ++listGeneration
  listing.value = true
  listError.value = ''
  try {
    const result = await maintenanceApi.files(controller.signal)
    if (!controller.signal.aborted && generation === listGeneration) files.value = result
  } catch (err) {
    if (!controller.signal.aborted && generation === listGeneration) listError.value = err.message
  } finally {
    if (generation === listGeneration) listing.value = false
  }
}
async function reload() {
  if (busy.value || loading.value) return
  loading.value = true
  error.value = ''
  try {
    const [state] = await Promise.all([
      adminApi.maintenance(controller.signal),
      readRemote(true),
      readCapacity(),
      refreshStatus(controller.signal)
    ])
    if (!controller.signal.aborted) maintenance.value = state
  } catch (err) {
    if (!controller.signal.aborted) error.value = err.message
  } finally {
    loading.value = false
  }
}
async function exportBackup() {
  await action('export', async () => {
    if (backupPassword.value !== confirmPassword.value) throw new Error('两次输入的备份密码不一致')
    const blob = await maintenanceApi.backup(backupPassword.value, controller.signal)
    if (!controller.signal.aborted) {
      downloadBackup(blob, `gatehome-backup-${new Date().toISOString().replace(/[:.]/g, '-')}.zip`)
      toast.success('备份已生成并开始下载')
    }
  })
  backupPassword.value = ''
  confirmPassword.value = ''
}
function selectFile(event) {
  file.value = event.target.files[0] || null
  preview.value = null
}
async function inspectLocal() {
  preview.value = null
  await action('inspect', async () => {
    if (!file.value) throw new Error('请选择备份 ZIP')
    if (!maintenance.value) throw new Error('请先读取维护状态')
    if (file.value.size > maintenance.value.max_backup_bytes) throw new Error('备份文件超过后端大小限制')
    const result = await maintenanceApi.inspect(
      'backup',
      file.value,
      restorePassword.value,
      controller.signal
    )
    if (!controller.signal.aborted) preview.value = result
  })
  restorePassword.value = ''
}
async function saveRemote() {
  await action('save', async () => {
    const body = webDAVPayload(snapshot.value, draft.value, secrets)
    await maintenanceApi.saveWebDAV(body)
    if (controller.signal.aborted) return
    clearSecrets()
    listGeneration++
    files.value = []
    preview.value = null
    await Promise.all([readRemote(true), refreshConfig(controller.signal)])
    toast.success('WebDAV 设置已保存')
  })
}
async function testRemote() {
  await action('test', async () => {
    const result = await maintenanceApi.testWebDAV(webDAVConnectionPayload(draft.value, secrets), controller.signal)
    if (!controller.signal.aborted) toast.info(result.message)
  })
}
function openDirectories() {
  directoryDialog.value.showModal()
  readDirectories(draft.value.url)
}
async function readDirectories(url) {
  if (directoryLoading.value) return
  const request = new AbortController()
  directoryRequest = request
  directoryLoading.value = true
  directoryError.value = ''
  try {
    const result = await maintenanceApi.webDAVDirectories(webDAVConnectionPayload(draft.value, secrets, url), request.signal)
    if (!request.signal.aborted) directoryHistory.value.push(result)
  } catch (err) {
    if (!request.signal.aborted) directoryError.value = err.message
  } finally {
    if (directoryRequest === request) directoryLoading.value = false
  }
  await nextTick()
  if (!request.signal.aborted && directoryDialog.value?.open) directoryLocation.value?.focus()
}
function parentDirectory() {
  directoryError.value = ''
  directoryHistory.value.pop()
  directoryLocation.value?.focus()
}
function closeDirectories() {
  directoryRequest?.abort()
  directoryRequest = undefined
  directoryLoading.value = false
  directoryError.value = ''
  directoryHistory.value = []
  browseButton.value?.focus()
}
function selectDirectory() {
  draft.value.url = currentDirectory.value.url
  directoryDialog.value.close()
}
async function runRemote() {
  await action('run', async () => {
    const result = await maintenanceApi.runWebDAV()
    if (!controller.signal.aborted) {
      toast.info(result.message)
      await readRemote()
    }
  })
}
async function downloadRemote(item) {
  await action('download', async () => {
    const blob = await maintenanceApi.downloadRemote(item.name, controller.signal)
    if (!controller.signal.aborted) {
      downloadBackup(blob, item.name)
      toast.success('远程备份已开始下载')
    }
  })
}
async function inspectRemote(item) {
  preview.value = null
  await action('inspect', async () => {
    const body = { name: item.name }
    if (remotePasswordMode.value === 'custom') body.password = remotePassword.value
    const result = await maintenanceApi.inspectRemote(body, controller.signal)
    if (!controller.signal.aborted) {
      preview.value = result
      toast.info('远程备份预检完成，请核对后确认恢复')
    }
  })
  remotePassword.value = ''
}
async function restore() {
  if (
    !preview.value?.can_apply ||
    !confirm('恢复将覆盖当前数据并重启服务，之后使用备份时的管理员账号和密码登录。确定恢复已检查的备份？')
  )
    return
  await action('restore', async () => {
    const result = await maintenanceApi.apply('restore', preview.value.id)
    endSession()
    await router.replace({ name: 'Login', query: { redirect: '/backups' } })
    toast.info(result.message)
  })
}
const onBackupsVisibility = () => {
  if (!document.hidden && !controller.signal.aborted) readRemote()
}
onMounted(() => {
  document.addEventListener('visibilitychange', onBackupsVisibility)
  reload()
})
onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onBackupsVisibility)
  controller.abort()
  directoryRequest?.abort()
  clearTimeout(timer)
  remoteGeneration++
  listGeneration++
  clearSecrets()
  backupPassword.value = ''
  confirmPassword.value = ''
  restorePassword.value = ''
  remotePassword.value = ''
  file.value = null
  preview.value = null
})
</script>
<style scoped>
.backup-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 20px;
  align-items: start;
}
.report-panel :deep(.card-inner),
form,
fieldset {
  display: grid;
  gap: 15px;
  min-width: 0;
}
fieldset {
  padding: 0;
  margin: 0;
  border: 0;
}
h2 {
  margin-bottom: 0;
}
label:not(.toggle-row) {
  display: grid;
  gap: 7px;
  font-size: 12px;
  color: var(--ink-secondary);
  min-width: 0;
}
.report-form.webdav-form {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  align-items: start;
}
.webdav-form > :is(label, .secret-field) { grid-column: span 2; }
.webdav-form > .credential-field { grid-column: span 3; }
.webdav-form > .wide { grid-column: 1 / -1; }
.webdav-form label { min-width: 0; }
.webdav-address, .schedule-fields { display: grid; gap: 12px; }
.webdav-address { grid-template-columns: minmax(0, 1fr) auto; align-items: end; }
.schedule-fields { grid-template-columns: repeat(auto-fit, minmax(min(100%, 130px), 1fr)); }
.webdav-directories code, .directory-entry span { min-width: 0; overflow-wrap: anywhere; }
.webdav-directories ul { display: grid; gap: 6px; padding: 0; margin: 0; list-style: none; }
.directory-entry { width: 100%; min-height: 44px; justify-content: flex-start; text-align: left; }
.directory-entry svg { flex-shrink: 0; }
.directory-entry svg:last-child { margin-left: auto; }
.report-page :deep(button:disabled) {
  opacity: 0.5;
  cursor: not-allowed;
}
@media (max-width: 640px) {
  .report-form.webdav-form, .webdav-address, .schedule-fields { grid-template-columns: minmax(0, 1fr); }
  .webdav-form > :is(label, .secret-field, .credential-field) { grid-column: 1 / -1; }
  .webdav-directory-dialog .btn { min-height: 44px; }
}
@media (max-width: 850px) {
  .backup-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
