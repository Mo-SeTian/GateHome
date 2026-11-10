<template>
  <div class="report-page animate-fade-in">
    <header class="report-heading">
      <div>
        <h1>SunPanel 浏览器首页</h1>
        <p>管理首页服务与访问入口，保留独立账号、分组、网站和上传资源</p>
      </div>
      <button class="btn btn-secondary" :disabled="loading || isSavingConfig" @click="reload">
        刷新配置
      </button>
    </header>
    <p v-if="error" class="report-error" role="alert">{{ error }}</p>
    <RestartNotice />
    <p v-if="gatewayStatus?.sunpanel_storage_supported === false" class="report-warning">
      当前启动器不支持 SunPanel 持久化，请先升级启动器。
    </p>
    <SpotlightCard v-if="draft" class="report-panel home-config">
      <h2>首页服务</h2>
      <form class="sunpanel-form" @submit.prevent="save">
        <label class="toggle-row">
          <span>启用首页服务</span>
          <input v-model="draft.enabled" name="enabled" type="checkbox" />
        </label>
        <div class="form-grid">
          <label class="form-group"
            >监听端口<input
              v-model.number="draft.port"
              name="port"
              class="form-input"
              type="number"
              min="1"
              max="65535"
              required
          /></label>
          <label class="form-group"
            >内网访问地址<input
              v-model.trim="draft.launch_url"
              name="launch_url"
              class="form-input"
              placeholder="留空使用当前主机和监听端口"
              maxlength="2048"
          /></label>
          <label class="form-group"
            >外网访问地址<input
              v-model.trim="draft.external_url"
              name="external_url"
              class="form-input"
              placeholder="https://home.example.com"
              maxlength="2048"
          /></label>
        </div>
        <p class="report-note">
          开关与端口修改需要重启；访问地址保存后即可使用。内网地址支持本站路径（如
          /sunpanel/），外网地址须为完整 HTTP(S) 地址。Docker 部署需发布首页端口。
        </p>
        <div class="form-actions">
          <button class="btn btn-primary" type="submit" :disabled="loading || isSavingConfig">
            {{ isSavingConfig ? '保存中…' : '保存首页设置' }}
          </button>
        </div>
      </form>
    </SpotlightCard>
    <SpotlightCard class="report-panel">
      <div class="report-heading">
        <h2>首页预览</h2>
        <div class="report-actions">
          <button class="btn btn-secondary btn-sm" :disabled="!enabled" @click="frameKey++">重载首页</button>
          <a
            v-if="enabled && internalURL"
            class="btn btn-primary btn-sm"
            :href="internalURL"
            target="_blank"
            rel="noopener noreferrer"
            >内网打开 <ExternalLink :size="13"
          /></a>
          <a
            v-if="enabled && externalURL"
            class="btn btn-secondary btn-sm"
            :href="externalURL"
            target="_blank"
            rel="noopener noreferrer"
            >外网打开 <ExternalLink :size="13"
          /></a>
        </div>
      </div>
      <p class="report-note">
        首页使用独立的 SunPanel 登录。登录后通过首页工具栏的「GateHome
        联动」批量导入已启用反代，也可预览关联更新；自定义名称、地址和分组会保留。
      </p>
      <p v-if="!enabled" class="report-empty">首页服务未启用，请保存设置并重启服务。</p>
      <p v-else-if="gatewayStatus?.restart_required" class="report-empty">
        监听配置待重启，重启后可加载首页。
      </p>
      <iframe
        v-else
        :key="frameKey"
        class="home-frame"
        title="SunPanel 浏览器首页"
        src="/sunpanel/"
        referrerpolicy="same-origin"
      />
    </SpotlightCard>
    <SpotlightCard class="report-panel">
      <h2>可导入反代（{{ routes.length }}）</h2>
      <p class="report-note">
        以下内容来自已启用的反代组和路由。在上方首页内选择导入目标分组并确认导入，重复项会自动跳过。
      </p>
      <div v-if="routes.length" class="report-table-wrap">
        <table class="report-table">
          <thead>
            <tr>
              <th>服务 / 分组</th>
              <th>默认网址</th>
              <th>内网地址</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="route in routes" :key="route.id">
              <td>
                {{ route.title }}<small>{{ route.group }}</small>
              </td>
              <td>{{ route.url }}</td>
              <td>{{ route.lanUrl }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else class="report-empty">{{ loading ? '正在读取反代…' : '暂无可导入的反代' }}</p>
    </SpotlightCard>
  </div>
</template>
<script setup>
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { ExternalLink } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import RestartNotice from '@/components/RestartNotice.vue'
import { adminApi } from '@/api/admin'
import { sunPanelLaunchURL } from '@/api/sunpanel'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useToast } from '@/composables/useToast'
import '@/styles/reports.css'
const { gatewayConfig, gatewayStatus, isSavingConfig, refreshConfig, refreshStatus, saveConfig } =
  useGatewayStatus()
const toast = useToast(),
  controller = new AbortController()
const snapshot = ref(null),
  draft = ref(null),
  routes = ref([]),
  loading = ref(false),
  error = ref(''),
  frameKey = ref(0)
const enabled = computed(() => Boolean(gatewayConfig.value?.sunpanel?.enabled))
const internalURL = computed(() => sunPanelLaunchURL(gatewayConfig.value?.sunpanel || {}, location.hostname))
const externalURL = computed(() =>
  sunPanelLaunchURL(gatewayConfig.value?.sunpanel || {}, location.hostname, true)
)
async function reload() {
  if (loading.value || isSavingConfig.value) return
  loading.value = true
  error.value = ''
  try {
    const [config, items] = await Promise.all([
      refreshConfig(controller.signal),
      adminApi.sunPanelRoutes(controller.signal),
      refreshStatus(controller.signal)
    ])
    if (controller.signal.aborted) return
    snapshot.value = config
    draft.value = { ...config.config.sunpanel }
    routes.value = items
  } catch (err) {
    if (!controller.signal.aborted) error.value = err.message
  } finally {
    loading.value = false
  }
}
async function save() {
  error.value = ''
  try {
    const result = await saveConfig(snapshot.value, {
      ...snapshot.value.config,
      sunpanel: { ...draft.value }
    })
    snapshot.value = result
    draft.value = { ...result.config.sunpanel }
    toast.success('首页设置已保存')
    await refreshStatus(controller.signal)
  } catch (err) {
    if (!controller.signal.aborted) error.value = err.message
  }
}
onMounted(reload)
onBeforeUnmount(() => controller.abort())
</script>
<style scoped>
.sunpanel-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
  width: 100%;
}
.form-grid {
  display: grid;
  grid-template-columns: 160px 1fr 1fr;
  gap: 12px;
  width: 100%;
}
.form-grid .form-group {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  color: var(--ink-secondary);
}
.form-actions {
  display: flex;
  align-items: center;
}
.home-frame {
  width: 100%;
  height: 680px;
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
  margin-top: 16px;
  background: var(--strip-bg);
}
.report-panel :deep(.card-inner) {
  display: grid;
  gap: 12px;
}
h2 {
  margin-bottom: 0;
}
@media (max-width: 768px) {
  .form-grid {
    grid-template-columns: 1fr;
  }
  .form-actions .btn {
    width: 100%;
    justify-content: center;
  }
}
@media (max-width: 600px) {
  .home-frame {
    height: 620px;
  }
}
</style>
