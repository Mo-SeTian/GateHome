<template>
  <div class="report-page animate-fade-in">
    <header class="report-heading">
      <div>
        <h1>版本更新与回滚</h1>
        <p>检查正式版或 Beta，校验更新包，并查看更新前的真实备份</p>
      </div>
      <button class="btn btn-secondary" :disabled="!!busy || loading" @click="reload">刷新维护状态</button>
    </header>
    <p v-if="error" class="report-error" role="alert">{{ error }}</p>
    <p v-if="maintenance && !maintenance.can_apply" class="report-warning">
      当前启动器支持检查更新包，执行更新需要支持维护的 Linux 或 Docker 启动方式。
    </p>
    <div class="updates-grid">
      <SpotlightCard class="report-panel"
        ><h2>在线更新</h2>
        <p class="version-label">
          当前版本 <strong>{{ gatewayVersion }}</strong> · {{ maintenance?.platform || '读取中…' }}
        </p>
        <label
          >发布渠道<select
            v-model="channel"
            name="channel"
            class="form-select"
            :disabled="!!busy || active"
            @change="resetRelease"
          >
            <option value="stable">正式稳定版</option>
            <option value="beta">Beta 公测版</option>
          </select></label
        >
        <p v-if="release" class="report-note" data-release>
          <template v-if="release.version">
            最新发行：v{{ release.version }} · {{ formatBytes(release.size) }}<br />发布于
            {{ reportDate(release.published_at) }}<br />{{
              release.update_available ? '有可用更新' : '当前版本无需更新'
            }}
            · 检查于 {{ reportDate(release.checked_at) }}
          </template>
          <template v-else>该渠道暂无有效发行版本</template>
        </p>
        <p v-else class="report-note">尚未检查当前渠道的发行版本。</p>
        <div class="report-actions">
          <button class="btn btn-secondary" :disabled="!!busy || active" @click="checkUpdate">
            {{ busy === 'check' ? '检查中…' : '检查更新' }}</button
          ><button
            class="btn btn-primary"
            :disabled="
              !!busy ||
              active ||
              !release?.update_available ||
              !maintenance?.can_apply ||
              !capacity?.can_proceed ||
              gatewayStatus?.maintenance_busy
            "
            @click="updateOnline"
          >
            {{ gatewayStatus?.update_history_available ? '备份并在线更新' : '在线更新并重启' }}</button
          ><a
            v-if="releaseURL"
            class="btn btn-secondary"
            :href="releaseURL"
            target="_blank"
            rel="noopener noreferrer"
            >发行说明</a
          >
        </div>
        <p class="report-note">
          确认后将下载、校验并安排安装重启。新程序启动检查失败时由启动器回滚；不把下载入队显示为更新成功。
        </p>
        <div v-if="job?.phase" class="online-job" role="status" aria-live="polite">
          <strong
            >{{ maintenancePhase(job.phase) }} · {{ job.channel === 'beta' ? 'Beta' : '正式版' }} v{{
              job.version
            }}</strong
          >
          <p v-if="job.total" class="report-note">
            {{ formatBytes(job.downloaded) }} / {{ formatBytes(job.total) }}
          </p>
          <progress v-if="job.total" :value="job.downloaded" :max="job.total" aria-label="更新包下载进度" />
          <p v-if="job.error" class="report-error">{{ job.error }}</p>
          <p v-if="job.retry_at" class="report-note">可在 {{ reportDate(job.retry_at) }} 后重试</p>
        </div>
        <p v-if="pollError" class="report-error" role="alert">更新进度读取失败，正在重试：{{ pollError }}</p>
      </SpotlightCard>
      <BackupCapacity
        :capacity="capacity"
        :error="capacityError"
        :loading="capacityLoading"
        @refresh="readCapacity"
      />
      <SpotlightCard class="report-panel"
        ><h2>上传更新包</h2>
        <form @submit.prevent="inspect">
          <fieldset :disabled="!!busy || active">
            <label
              >项目更新 ZIP<input
                name="update_file"
                type="file"
                accept=".zip,application/zip"
                required
                @change="selectFile"
            /></label>
            <div v-if="file" class="selected-file-pill">
              <span>已选择更新包：{{ file.name }}</span>
            </div>
            <p class="report-note">
              只接受项目格式、当前平台且版本更高的可信更新包。先预检，再确认应用；预检不会替换正在运行的程序。
            </p>
            <button class="btn btn-secondary" type="submit" :disabled="!maintenance">
              {{ busy === 'inspect' ? '正在校验…' : '检查更新包' }}
            </button>
          </fieldset>
        </form>
        <MaintenancePreview
          v-if="preview"
          :preview="preview"
          kind="update"
          :disabled="!!busy || active || !capacity?.can_proceed || gatewayStatus?.maintenance_busy"
          :busy="busy === 'apply'"
          @apply="apply"
      /></SpotlightCard>
      <SpotlightCard class="report-panel"
        ><h2>GitHub API Token（可选）</h2>
        <p class="report-note">
          {{
            configSnapshot?.github_token_configured ? '已配置，仅返回配置状态。' : '尚未配置。'
          }}
          用于读取公开发行版本，提高 API 访问额度。
        </p>
        <form @submit.prevent="saveToken">
          <fieldset :disabled="!!busy || active || isSavingConfig">
            <label
              >新 Token<input
                v-model="githubToken"
                name="github_token"
                class="form-input"
                type="password"
                autocomplete="new-password"
                placeholder="留空保留已存 Token"
                maxlength="512" /></label
            ><label class="check-row"
              ><input v-model="clearToken" name="clear_github_token" type="checkbox" />明确清除已存
              Token</label
            ><button
              class="btn btn-secondary"
              type="submit"
              :disabled="!configSnapshot || (!githubToken && !clearToken)"
            >
              保存 GitHub Token
            </button>
          </fieldset>
        </form></SpotlightCard
      >
    </div>
    <SpotlightCard class="report-panel"
      ><div class="report-heading">
        <h2>更新前备份与历史回滚</h2>
        <button class="btn btn-secondary btn-sm" :disabled="historyLoading" @click="readHistory">
          刷新历史
        </button>
      </div>
      <p class="report-note">
        保留最近 {{ history?.retention ?? '—' }} 份更新前程序与数据快照。回滚会恢复备份时的程序、配置及账号。
      </p>
      <p v-if="historyError" class="report-error" role="alert">
        {{ historyError }}{{ history?.backups.length ? '，仍显示上次历史' : '' }}
      </p>
      <p
        v-if="history?.last_result"
        :class="history.last_result.ok ? 'report-note' : 'report-warning'"
        data-update-result
      >
        {{ reportDate(history.last_result.at) }} · {{ history.last_result.message }}
      </p>
      <div v-if="history?.backups.length" class="report-table-wrap">
        <table class="report-table">
          <thead>
            <tr>
              <th>备份版本</th>
              <th>目标版本</th>
              <th>时间</th>
              <th>大小</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="backup in history.backups" :key="backup.id" :data-update-backup="backup.id">
              <td>v{{ backup.version }}</td>
              <td>v{{ backup.target_version }}</td>
              <td>{{ reportDate(backup.created_at) }}</td>
              <td>{{ formatBytes(backup.bytes) }}</td>
              <td>
                <button
                  class="btn btn-secondary btn-sm"
                  :disabled="
                    !!busy ||
                    active ||
                    !gatewayStatus?.update_history_available ||
                    gatewayStatus?.maintenance_busy ||
                    !capacity?.can_proceed
                  "
                  @click="rollback(backup)"
                >
                  恢复此版本
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else class="report-empty">{{ historyLoading ? '正在读取更新历史…' : '暂无更新前备份' }}</p>
    </SpotlightCard>
  </div>
</template>
<script setup>
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { useRouter } from 'vue-router'
import SpotlightCard from '@/components/SpotlightCard.vue'
import BackupCapacity from '@/components/BackupCapacity.vue'
import MaintenancePreview from '@/components/MaintenancePreview.vue'
import { adminApi } from '@/api/admin'
import { maintenanceApi, maintenancePhase, onlineUpdateActive } from '@/api/maintenance'
import { formatBytes, reportDate } from '@/api/reports'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useSession } from '@/composables/useSession'
import { useToast } from '@/composables/useToast'
import '@/styles/reports.css'
const { gatewayStatus, gatewayVersion, refreshStatus, refreshConfig, saveConfig, isSavingConfig } =
    useGatewayStatus(),
  { endSession } = useSession()
const router = useRouter(),
  toast = useToast(),
  controller = new AbortController()
const maintenance = ref(null),
  capacity = ref(null),
  capacityLoading = ref(false),
  capacityError = ref(''),
  channel = ref('stable'),
  release = ref(null),
  job = ref(null),
  busy = ref(''),
  loading = ref(false),
  error = ref(''),
  pollError = ref('')
const history = ref(null),
  historyLoading = ref(false),
  historyError = ref(''),
  configSnapshot = ref(null),
  githubToken = ref(''),
  clearToken = ref(false),
  file = ref(null),
  preview = ref(null)
const active = computed(() => onlineUpdateActive(job.value?.phase))
const releaseURL = computed(() => {
  try {
    const url = new URL(release.value?.release_url)
    return url.protocol === 'https:' && url.hostname === 'github.com' ? url.href : ''
  } catch {
    return ''
  }
})
let timer,
  pollGeneration = 0,
  leaving = false
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
async function readHistory() {
  if (historyLoading.value) return
  historyLoading.value = true
  historyError.value = ''
  try {
    const result = await maintenanceApi.history(controller.signal)
    if (!controller.signal.aborted) history.value = result
  } catch (err) {
    if (!controller.signal.aborted) historyError.value = err.message
  } finally {
    historyLoading.value = false
  }
}
async function loginAgain(message) {
  if (leaving || controller.signal.aborted) return
  leaving = true
  preview.value = null
  githubToken.value = ''
  endSession()
  await router.replace({ name: 'Login', query: { redirect: '/updates' } })
  toast.info(message)
}
async function poll() {
  const generation = ++pollGeneration
  clearTimeout(timer)
  try {
    const result = await maintenanceApi.onlineStatus(controller.signal)
    if (controller.signal.aborted || generation !== pollGeneration) return
    job.value = result
    pollError.value = ''
    if (onlineUpdateActive(result.phase) && ['stable', 'beta'].includes(result.channel))
      channel.value = result.channel
    if (result.phase === 'restarting')
      await loginAgain('更新包已校验，服务正在重启；恢复后将自动载入新界面，请重新登录查看维护结果')
  } catch (err) {
    if (!controller.signal.aborted && generation === pollGeneration) pollError.value = err.message
  } finally {
    if (!controller.signal.aborted && generation === pollGeneration && !leaving)
      timer = setTimeout(poll, active.value ? 1200 : 5000)
  }
}
async function reload() {
  if (loading.value || busy.value) return
  loading.value = true
  error.value = ''
  preview.value = null
  try {
    const [state, config] = await Promise.all([
      adminApi.maintenance(controller.signal),
      refreshConfig(controller.signal),
      readCapacity(),
      readHistory(),
      refreshStatus(controller.signal),
      poll()
    ])
    if (!controller.signal.aborted) {
      maintenance.value = state
      configSnapshot.value = config
    }
  } catch (err) {
    if (!controller.signal.aborted) error.value = err.message
  } finally {
    loading.value = false
  }
}
function resetRelease() {
  release.value = null
  error.value = ''
}
async function checkUpdate() {
  resetRelease()
  await action('check', async () => {
    const result = await maintenanceApi.checkRelease(channel.value, controller.signal)
    if (!controller.signal.aborted) release.value = result
  })
}
async function updateOnline() {
  if (
    !release.value?.update_available ||
    !confirm(
      `更新到 ${channel.value === 'beta' ? 'Beta ' : ''}v${release.value.version}？确认后将下载、校验并安装，服务会重启。请确认已保存当前数据。`
    )
  )
    return
  await action('update', async () => {
    const result = await maintenanceApi.updateOnline(channel.value, release.value.version)
    if (!controller.signal.aborted) {
      job.value = result
      toast.info('在线更新已开始，请等待实际维护结果')
      await poll()
    }
  })
}
function selectFile(event) {
  file.value = event.target.files[0] || null
  preview.value = null
}
async function inspect() {
  preview.value = null
  await action('inspect', async () => {
    if (!file.value) throw new Error('请选择更新 ZIP')
    if (file.value.size > maintenance.value.max_update_bytes) throw new Error('更新包超过后端大小限制')
    const result = await maintenanceApi.inspect('update', file.value, '', controller.signal)
    if (!controller.signal.aborted) preview.value = result
  })
}
async function apply() {
  if (
    !preview.value?.can_apply ||
    !confirm(`安装已检查的 v${preview.value.version} 并重启？启动器会按能力保存更新前备份。`)
  )
    return
  await action('apply', async () => {
    const result = await maintenanceApi.apply('update', preview.value.id)
    await loginAgain(result.message)
  })
}
async function rollback(backup) {
  if (
    !confirm(
      `恢复至 v${backup.version}（${reportDate(backup.created_at)}）？当前程序和数据将被覆盖，之后使用备份时的管理员账号和密码登录。`
    )
  )
    return
  await action('rollback', async () => {
    const result = await maintenanceApi.rollback(backup.id)
    await loginAgain(result.message)
  })
}
async function saveToken() {
  await action('token', async () => {
    const result = await saveConfig(configSnapshot.value, configSnapshot.value.config, {
      github_token: clearToken.value ? '' : githubToken.value
    })
    if (!controller.signal.aborted) {
      configSnapshot.value = result
      clearToken.value = false
      toast.success('GitHub Token 设置已保存')
    }
  })
  githubToken.value = ''
}
onMounted(reload)
onBeforeUnmount(() => {
  controller.abort()
  clearTimeout(timer)
  pollGeneration++
  githubToken.value = ''
  file.value = null
  preview.value = null
})
</script>
<style scoped>
.updates-grid {
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
  border: 0;
  padding: 0;
  margin: 0;
}
h2 {
  margin-bottom: 0;
}
label:not(.toggle-row):not(.check-row) {
  display: grid;
  gap: 7px;
  color: var(--ink-secondary);
  font-size: 12px;
  min-width: 0;
}
.check-row {
  display: flex;
  gap: 8px;
  align-items: center;
}
.version-label {
  color: var(--muted);
  font-size: 13px;
}
.version-label strong {
  color: var(--ink);
}
.online-job {
  display: grid;
  gap: 10px;
  font-size: 13px;
  padding: 14px;
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
}
progress {
  width: 100%;
  accent-color: var(--brand);
}
.report-page :deep(button:disabled) {
  opacity: 0.5;
  cursor: not-allowed;
}
@media (max-width: 850px) {
  .updates-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
