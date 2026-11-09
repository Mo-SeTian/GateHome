<template>
  <div class="report-page animate-fade-in">
    <header class="report-heading">
      <div>
        <h1>系统与网关设置</h1>
        <p>管理访问来源、出站代理、日志保留和管理员账户</p>
      </div>
      <button class="btn btn-secondary" :disabled="loading || isSavingConfig" @click="reload">
        重新读取
      </button>
    </header>
    <p v-if="error" class="report-error" role="alert">{{ error }}</p>
    <RestartNotice />
    <form v-if="draft" class="settings-layout" @submit.prevent="save">
      <fieldset :disabled="loading || isSavingConfig" class="settings-fields">
        <SpotlightCard class="report-panel">
          <h2>管理界面反代访问</h2>
          <label class="toggle-row">
            <span>允许指定公网来源访问</span>
            <input
              v-model="draft.admin_access.enabled"
              name="admin_access_enabled"
              type="checkbox"
            />
          </label>
          <label
            >允许的公网来源（每行一个）<textarea
              v-model="origins"
              name="origins"
              class="form-input"
              rows="3"
              placeholder="https://gate.example.com"
            />
          </label>
          <p class="report-note">
            填写 HTTP(S) 协议、主机和可选端口，不包含路径、账号或密码。启用时至少填写一个，最多 20 个。
          </p>
        </SpotlightCard>
        <SpotlightCard class="report-panel">
          <h2>出站网络代理</h2>
          <label class="toggle-row">
            <span>启用 HTTP / HTTPS / SOCKS5 代理</span>
            <input v-model="draft.outbound_proxy.enabled" name="proxy_enabled" type="checkbox" />
          </label>
          <label
            >代理地址<input
              v-model.trim="draft.outbound_proxy.url"
              name="proxy_url"
              class="form-input"
              placeholder="http://127.0.0.1:7890"
          /></label>
          <label
            >认证用户名<input
              v-model.trim="draft.outbound_proxy.username"
              name="proxy_username"
              class="form-input"
              autocomplete="off"
          /></label>
          <label
            >代理密码（{{ snapshot.proxy_password_configured ? '已配置' : '未配置' }}）<select
              v-model="passwordAction"
              name="proxy_password_action"
              class="form-select"
            >
              <option value="keep">保留已存密码</option>
              <option value="replace">设置新密码</option>
              <option value="clear">清除已存密码</option>
            </select></label
          >
          <label v-if="passwordAction === 'replace'"
            >新代理密码<input
              v-model="proxyPassword"
              name="proxy_password"
              type="password"
              class="form-input"
              autocomplete="new-password"
              required
          /></label>
          <p class="report-note">
            用于
            DDNS、订阅与版本检测等出站请求。修改代理地址或用户名时，后端会清除旧密码，请按需填写新密码；连接失败不会回退直连。
          </p>
        </SpotlightCard>
        <SpotlightCard class="report-panel">
          <h2>日志保留与存储配额</h2>
          <label
            >最大占用空间（MiB）<input
              v-model.number="draft.log_retention.max_size_mb"
              name="max_size_mb"
              class="form-input"
              type="number"
              min="1"
              max="1024"
              required
          /></label>
          <label
            >保留天数<input
              v-model.number="draft.log_retention.keep_days"
              name="keep_days"
              class="form-input"
              type="number"
              min="1"
              max="3650"
              required
          /></label>
          <p class="report-note">
            已使用
            {{
              formatBytes(gatewayStatus?.log_storage?.used_bytes)
            }}。保存后会按配额清理旧日志，请确认保留范围。
          </p>
          <p v-if="gatewayStatus?.log_storage?.write_error" class="report-error">
            日志写入失败，请检查存储目录和可用空间。
          </p>
        </SpotlightCard>
        <div class="report-actions">
          <button type="submit" class="btn btn-primary">
            {{ isSavingConfig ? '保存中…' : '保存全局配置' }}
          </button>
        </div>
      </fieldset>
      <div class="settings-fields">
        <SpotlightCard class="report-panel"
          ><h2>管理员账户</h2>
          <p class="report-note">当前账号：{{ username }}。修改账号或密码将退出全部管理会话。</p>
          <button type="button" class="btn btn-secondary" @click="openAccountModal">
            修改账号与密码
          </button></SpotlightCard
        >
        <SpotlightCard class="report-panel"
          ><h2>界面外观</h2>
          <div class="theme-options">
            <button
              v-for="option in themes"
              :key="option.value"
              type="button"
              class="btn btn-secondary"
              :aria-pressed="theme === option.value"
              @click="setTheme(option.value)"
            >
              {{ option.label }}{{ theme === option.value ? ' · 已选择' : '' }}
            </button>
          </div></SpotlightCard
        >
        <SpotlightCard class="report-panel"
          ><h2>版本与运行环境</h2>
          <dl class="system-meta">
            <dt>当前版本</dt>
            <dd>{{ gatewayVersion }}</dd>
            <dt>运行平台</dt>
            <dd>{{ maintenance?.platform || '读取中…' }}</dd>
            <dt>维护状态</dt>
            <dd>
              {{
                maintenance?.busy
                  ? '维护进行中'
                  : maintenance?.can_apply
                    ? '支持服务内维护'
                    : '请由启动器管理服务'
              }}
            </dd>
          </dl>
          <div class="report-actions">
            <RouterLink class="btn btn-secondary" to="/updates">版本与更新</RouterLink
            ><a class="btn btn-secondary" href="/legacy/">旧版控制台</a><button
              type="button"
              class="btn btn-secondary"
              :disabled="!maintenance?.can_apply || maintenance?.busy || restarting || isSavingConfig"
              @click="restart"
            >
              {{ restarting ? '正在安排重启…' : '重启网关服务' }}
            </button>
          </div></SpotlightCard
        >
        <SpotlightCard class="report-panel"
          ><h2>反代监听端口</h2>
          <div v-for="group in snapshot?.config.groups || []" :key="group.id" class="listener-row">
            <strong>{{ group.name }}</strong
            ><span
              >{{ group.enabled ? '启用' : '停用' }} · HTTP {{ group.http_port || '关闭' }} / HTTPS
              {{ group.https_port || '关闭' }}</span
            >
          </div>
          <RouterLink class="btn btn-secondary" to="/routes">管理反代组与监听</RouterLink></SpotlightCard
        >
      </div>
    </form>
  </div>
</template>
<script setup>
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { useRouter } from 'vue-router'
import SpotlightCard from '@/components/SpotlightCard.vue'
import RestartNotice from '@/components/RestartNotice.vue'
import { adminApi } from '@/api/admin'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useAppStore } from '@/composables/useAppStore'
import { useSession } from '@/composables/useSession'
import { useToast } from '@/composables/useToast'
import '@/styles/reports.css'
const { gatewayStatus, gatewayVersion, isSavingConfig, refreshConfig, refreshStatus, saveConfig } =
  useGatewayStatus()
const { theme, setTheme } = useAppStore(),
  { username, openAccountModal, endSession } = useSession()
const toast = useToast(),
  router = useRouter(),
  controller = new AbortController()
const themes = [
  { value: 'dark', label: 'Obsidian Tech 黑曜石深色' },
  { value: 'light', label: 'VisionOS 磨砂浅色' }
]
const snapshot = ref(null),
  draft = ref(null),
  maintenance = ref(null),
  origins = ref(''),
  loading = ref(false),
  error = ref(''),
  restarting = ref(false),
  passwordAction = ref('keep'),
  proxyPassword = ref('')
function apply(config) {
  snapshot.value = config
  draft.value = structuredClone(config.config)
  origins.value = (config.config.admin_access?.origins || []).join('\n')
  passwordAction.value = 'keep'
  proxyPassword.value = ''
}
function formatBytes(value) {
  return value == null ? '—' : `${(value / 1024 / 1024).toFixed(2)} MiB`
}
async function reload() {
  if (loading.value || isSavingConfig.value) return
  loading.value = true
  error.value = ''
  try {
    const [config, state] = await Promise.all([
      refreshConfig(controller.signal),
      adminApi.maintenance(controller.signal),
      refreshStatus(controller.signal)
    ])
    if (!controller.signal.aborted) {
      apply(config)
      maintenance.value = state
    }
  } catch (err) {
    if (!controller.signal.aborted) error.value = err.message
  } finally {
    loading.value = false
  }
}
async function save() {
  error.value = ''
  const config = {
    ...snapshot.value.config,
    admin_access: {
      ...draft.value.admin_access,
      origins: origins.value
        .split('\n')
        .map((value) => value.trim())
        .filter(Boolean)
    },
    outbound_proxy: { ...draft.value.outbound_proxy },
    log_retention: { ...draft.value.log_retention }
  }
  const credentials =
    passwordAction.value === 'keep'
      ? {}
      : { proxy_password: passwordAction.value === 'clear' ? '' : proxyPassword.value }
  try {
    const result = await saveConfig(snapshot.value, config, credentials)
    apply(result)
    toast.success('全局配置已保存')
    await refreshStatus(controller.signal)
  } catch (err) {
    if (!controller.signal.aborted) error.value = err.message
  } finally {
    proxyPassword.value = ''
  }
}
async function restart() {
  if (!confirm('重启将中断当前连接并使管理会话失效，确定继续？')) return
  restarting.value = true
  try {
    const result = await adminApi.restart()
    endSession()
    await router.replace({ name: 'Login', query: { redirect: '/settings' } })
    toast.info(result.message)
  } catch (err) {
    toast.error(err.message)
  } finally {
    restarting.value = false
  }
}
onMounted(reload)
onBeforeUnmount(() => {
  controller.abort()
  proxyPassword.value = ''
})
</script>
<style scoped>
.settings-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 20px;
  align-items: start;
}
.settings-fields {
  display: grid;
  gap: 18px;
  border: 0;
  padding: 0;
  margin: 0;
  min-width: 0;
}
.report-panel :deep(.card-inner) {
  display: grid;
  gap: 14px;
}
.report-panel h2 {
  margin-bottom: 0;
}
label:not(.toggle-row):not(.check-row) {
  display: grid;
  gap: 7px;
  font-size: 12px;
  color: var(--ink-secondary);
}
.check-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.theme-options {
  display: grid;
  gap: 12px;
}
.theme-options [aria-pressed='true'] {
  border-color: var(--brand);
}
.system-meta {
  display: grid;
  grid-template-columns: 90px minmax(0, 1fr);
  gap: 12px;
  font-size: 13px;
}
dt {
  color: var(--muted);
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.listener-row {
  display: grid;
  gap: 6px;
  color: var(--ink-secondary);
  font-size: 12px;
}
@media (max-width: 900px) {
  .settings-layout {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
