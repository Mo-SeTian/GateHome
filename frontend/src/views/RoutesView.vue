<template>
  <div class="routes-page animate-fade-in">
    <div class="page-topbar">
      <div>
        <h1 class="page-title">反向代理与路由组</h1>
        <p class="page-subtitle">管理反代组、域名路由、来源 IP 防火墙与独立访问账户</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-secondary" :disabled="loading || isSavingConfig" @click="reload">刷新</button>
        <button
          class="btn btn-secondary"
          :disabled="!gatewayConfig || isSavingConfig"
          @click="openHealthSettings"
        >
          健康检查
        </button>
        <button
          class="btn btn-secondary"
          :disabled="!proxyGroups.length || isSavingConfig"
          @click="openDiscovery"
        >
          发现服务
        </button>
        <button class="btn btn-secondary" :disabled="!gatewayConfig || isSavingConfig" @click="openGroup()">
          新建反代组
        </button>
        <button
          class="btn btn-primary"
          :disabled="!proxyGroups.length || isSavingConfig"
          @click="openRoute()"
        >
          <Plus :size="15" /> 新建反代服务
        </button>
      </div>
    </div>

    <RestartNotice />
    <p v-if="loadError" class="page-error" role="alert">{{ loadError }}</p>
    <p v-if="loading" role="status">正在读取反代配置…</p>

    <div class="filter-bar">
      <div class="chips-group">
        <button class="chip-btn" :class="{ active: selectedGroup === 'ALL' }" @click="selectedGroup = 'ALL'">
          全部路由 <span class="chip-count">{{ routes.length }}</span>
        </button>
        <button
          v-for="group in proxyGroups"
          :key="group.id"
          class="chip-btn"
          :class="{ active: selectedGroup === group.id }"
          @click="selectedGroup = group.id"
        >
          {{ group.name }} <span class="chip-count">{{ getGroupCount(group.id) }}</span>
        </button>
        <button
          class="chip-btn"
          :class="{ active: selectedGroup === 'INACTIVE' }"
          @click="selectedGroup = 'INACTIVE'"
        >
          已停用 <span class="chip-count">{{ routes.filter((route) => !route.enabled).length }}</span>
        </button>
      </div>
      <div class="search-box">
        <Search :size="14" /><input
          v-model="searchQuery"
          class="search-input"
          aria-label="搜索反代服务"
          placeholder="按名称、域名或上游搜索"
        />
      </div>
    </div>

    <section
      v-for="{ group, items } in groupedRoutes"
      :key="group.id"
      class="route-group"
      :class="{ collapsed: isGroupCollapsed(group.id) }"
      :data-group-id="group.id"
      :aria-labelledby="`route-group-title-${group.id}`"
    >
      <header class="route-group-header">
        <button
          type="button"
          class="route-group-toggle"
          :aria-label="`${isGroupCollapsed(group.id) ? '展开' : '收起'} ${group.name} 的路由`"
          :aria-expanded="!isGroupCollapsed(group.id)"
          :aria-controls="`route-group-body-${group.id}`"
          @click="toggleGroupCollapse(group.id)"
        >
          <ChevronDown :size="16" aria-hidden="true" :class="{ folded: isGroupCollapsed(group.id) }" />
          <span class="route-group-info">
            <span class="route-group-title">
              <strong :id="`route-group-title-${group.id}`">{{ group.name }}</strong>
              <span class="chip-count">{{ items.length }} 个服务</span>
            </span>
            <span class="route-group-meta">
              HTTP {{ group.http_port || '关闭' }} / HTTPS {{ group.https_port || '关闭' }} · {{ groupState(group) }}
              <span v-if="group.domain_suffix"> · {{ group.domain_suffix }}</span>
            </span>
          </span>
        </button>
        <div class="action-btn-group group-actions">
          <button class="btn btn-secondary btn-sm" :disabled="isSavingConfig" @click="openGroup(group)">编辑组</button>
          <button class="btn btn-ghost btn-sm" :disabled="isSavingConfig" @click="toggleGroup(group)">
            {{ group.enabled ? '停用组' : '启用组' }}
          </button>
          <button
            class="btn btn-ghost btn-sm text-bad"
            :disabled="isSavingConfig || getGroupCount(group.id) > 0"
            :aria-label="`删除反代组 ${group.name}`"
            title="先移走或删除组内路由，再删除此组"
            @click="deleteGroup(group)"
          >删除组</button>
        </div>
      </header>
      <div :id="`route-group-body-${group.id}`" v-show="!isGroupCollapsed(group.id)" class="route-group-body">
        <div class="routes-grid">
          <SpotlightCard
            v-for="route in items"
            :key="route.id || routeKey(route)"
            class="route-card"
            :class="{ inactive: !route.enabled }"
          >
            <div class="card-head">
              <div class="app-icon-wrap" :class="routeCategoryClass(route)">
                <img
                  v-if="route.image"
                  :src="routeImageUrl(route.image)"
                  width="32"
                  height="32"
                  alt=""
                /><Network v-else :size="18" />
              </div>
              <div class="card-title-group">
                <div class="card-title-row">
                  <span class="service-name">{{ route.name || route.host }}</span>
                </div>
                <div class="service-category">所属组 {{ groupName(route.group_id) }}</div>
              </div>
              <label class="switch"
                ><input
                  type="checkbox"
                  :checked="route.enabled"
                  :disabled="isSavingConfig"
                  :aria-label="`启用 ${route.name || route.host}`"
                  @change="toggleRoute(route, $event)" /><span class="switch-slider"
              /></label>
            </div>
            <div class="endpoint-info">
              <button
                type="button"
                class="info-row domain-row"
                :disabled="!accessURL(route)"
                :title="accessURL(route) || '此组尚未配置对应的访问端口'"
                :aria-label="accessURL(route) ? `复制访问地址 ${accessURL(route)}` : '访问端口未配置'"
                @click="copyURL(accessURL(route))"
              >
                <Globe :size="14" aria-hidden="true" /><span class="info-text mono-text domain-text">{{ accessURL(route) || route.host }}</span
                ><Copy :size="12" aria-hidden="true" />
              </button>
              <div class="info-row target-row">
                <ArrowRight :size="14" /><span class="info-text mono-text">{{ route.upstream }}</span>
              </div>
            </div>
            <div class="feature-badges">
              <span v-if="route.tls" class="feature-badge ssl"><Lock :size="10" /> HTTPS</span>
              <span v-if="route.firewall_id" class="feature-badge fw">{{ firewallName(route.firewall_id) }}</span>
              <span v-if="route.auth.enabled" class="feature-badge auth"><Key :size="10" /> 账号保护</span>
              <span class="feature-badge hits">{{ route.enabled ? '已启用' : '已停用' }}</span>
            </div>
            <button type="button" class="health-detail" @click="showHealth(route)">
              {{ routeHealth(route) }}
            </button>
            <div class="card-actions">
              <span class="latency-label">{{ route.tls ? 'HTTPS' : 'HTTP' }}</span>
              <div class="action-btn-group">
                <RouterLink class="btn btn-ghost btn-sm" :to="{ path: '/traffic', query: { rule: route.id } }">查看流量</RouterLink>
                <button class="btn btn-ghost btn-sm" :disabled="isSavingConfig" @click="openRoute(route)">
                  <Edit3 :size="13" /> 编辑
                </button>
                <button
                  class="btn btn-ghost btn-sm text-bad"
                  :disabled="isSavingConfig"
                  :aria-label="`删除 ${route.name || route.host}`"
                  @click="deleteRoute(route)"
                >
                  <Trash2 :size="13" />
                </button>
              </div>
            </div>
          </SpotlightCard>
        </div>
        <p v-if="!items.length" class="group-empty">此组暂无反代服务，可通过“新建反代服务”添加。</p>
      </div>
    </section>
    <div v-if="gatewayConfig && !groupedRoutes.length" class="empty-state">
      <AlertCircle :size="30" />
      <p>暂无匹配的反代服务</p>
    </div>

    <dialog
      ref="routeDialog"
      class="modal-dialog"
      aria-labelledby="route-title"
      @close="closeRoute"
      @cancel="blockBusyClose"
    >
      <div class="modal-header">
        <h2 id="route-title" class="modal-title">
          {{ originalRouteId == null ? '新建' : '编辑' }}反向代理路由
        </h2>
        <button
          class="close-modal-btn"
          aria-label="关闭"
          :disabled="isSavingConfig || imageBusy"
          @click="routeDialog.close()"
        >
          <X :size="18" />
        </button>
      </div>
      <form class="modal-body" @submit.prevent="saveRoute">
        <p v-if="formError" ref="routeError" class="page-error" role="alert" tabindex="-1">{{ formError }}</p>
        <label class="form-group"
          >服务名称<input v-model="routeDraft.name" name="name" class="form-input" maxlength="100"
        /></label>
        <label class="form-group"
          >所属反代组<select v-model="routeDraft.group_id" name="group_id" class="form-select" required @change="changeRouteGroup">
            <option v-for="group in editSnapshot.config.groups" :key="group.id" :value="group.id">
              {{ group.name }}
            </option>
          </select></label
        >
        <label class="form-group"
          >{{ fullHost || !editSnapshot.config.groups.find(group => group.id === routeDraft.group_id)?.domain_suffix ? '访问域名' : '子域名前缀' }}<input
            v-model="routeDraft.host"
            name="host"
            class="form-input mono-text"
            required
            placeholder="完整域名，或本组的子域名前缀"
        /></label>
        <label v-if="editSnapshot.config.groups.find(group => group.id === routeDraft.group_id)?.domain_suffix" class="check-label">
          <input v-model="fullHost" name="full_host" type="checkbox" />填写完整域名（不自动补全后缀）
        </label>
        <p class="form-note">保存为 {{ resolvedHost || '—' }}</p>
        <label class="form-group"
          >上游地址<input
            v-model="routeDraft.upstream"
            name="upstream"
            class="form-input mono-text"
            required
            placeholder="http://192.168.1.10:8080"
        /></label>
        <label class="form-group"
          >防火墙<select v-model="routeDraft.firewall_id" name="firewall_id" class="form-select">
            <option value="">不限制来源 IP</option>
            <option v-for="firewall in editSnapshot.config.firewalls" :key="firewall.id" :value="firewall.id">
              {{ firewall.name }}
            </option>
          </select></label
        >
        <div class="toggles-group">
          <label class="toggle-row">
            <span>启用此反代路由</span>
            <input v-model="routeDraft.enabled" name="enabled" type="checkbox" />
          </label>
          <label class="toggle-row">
            <span>使用 HTTPS 访问（需要匹配证书）</span>
            <input v-model="routeDraft.tls" name="tls" type="checkbox" />
          </label>
          <label class="toggle-row">
            <span>启用独立访问账户保护</span>
            <input v-model="routeDraft.auth.enabled" name="auth_enabled" type="checkbox" />
          </label>
        </div>
        <label class="check-label">
          <input v-model="clearRoutePassword" name="clear_route_password" type="checkbox" />清除已保存的访问密码（须关闭独立访问验证）
        </label>
        <fieldset v-if="routeDraft.auth.enabled" class="auth-fields">
          <label class="form-group"
            >访问用户名<input
              v-model="routeDraft.auth.username"
              name="auth_username"
              class="form-input"
              required
              autocomplete="off"
              maxlength="64"
          /></label>
          <label class="form-group"
            >{{ passwordRequired ? '设置访问密码' : '修改访问密码'
            }}<input
              v-model="routePassword"
              name="route_password"
              class="form-input"
              type="password"
              autocomplete="new-password"
              :required="passwordRequired"
              :placeholder="passwordRequired ? '12–72 字节' : '留空保留已配置密码'"
          /></label>
          <label class="form-group"
            >验证失败上限<input
              v-model.number="routeDraft.auth.failure_limit"
              name="failure_limit"
              class="form-input"
              type="number"
              min="1"
              max="100"
              required
          /></label>
          <label class="form-group"
            >冻结时长 分钟<input
              v-model.number="freezeMinutes"
              name="freeze_minutes"
              class="form-input"
              type="number"
              min="1"
              max="10080"
              required
          /></label>
        </fieldset>
        <RouteImagePicker v-if="routeOpen" v-model="routeDraft.image" @busy="imageBusy = $event" />
        <div class="modal-footer">
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="isSavingConfig || imageBusy"
            @click="routeDialog.close()"
          >
            取消</button
          ><button type="submit" class="btn btn-primary" :disabled="isSavingConfig || imageBusy">
            {{ isSavingConfig ? '保存中…' : '保存路由' }}
          </button>
        </div>
      </form>
    </dialog>

    <dialog ref="groupDialog" class="modal-dialog" aria-labelledby="group-title" @cancel="blockBusyClose">
      <div class="modal-header">
        <h2 id="group-title" class="modal-title">{{ originalGroupId == null ? '新建' : '编辑' }}反代组</h2>
        <button
          class="close-modal-btn"
          aria-label="关闭"
          :disabled="isSavingConfig"
          @click="groupDialog.close()"
        >
          <X :size="18" />
        </button>
      </div>
      <form class="modal-body" @submit.prevent="saveGroup">
        <p v-if="formError" class="page-error" role="alert">{{ formError }}</p>
        <label class="form-group"
          >组名称<input
            v-model="groupDraft.name"
            name="name"
            class="form-input"
            required
            maxlength="100"
        /></label>
        <div class="form-row-2">
          <label class="form-group"
            >HTTP 端口<input
              v-model.number="groupDraft.http_port"
              name="http_port"
              type="number"
              class="form-input"
              min="0"
              max="65535"
              required /></label
          ><label class="form-group"
            >HTTPS 端口<input
              v-model.number="groupDraft.https_port"
              name="https_port"
              type="number"
              class="form-input"
              min="0"
              max="65535"
              required
          /></label>
        </div>
        <p class="form-note">端口为 0 时关闭对应协议。监听变更在重启后生效。</p>
        <label class="form-group"
          >域名后缀<input
            v-model="groupDraft.domain_suffix"
            name="domain_suffix"
            class="form-input"
            placeholder="example.com"
        /></label>
        <label class="form-group"
          >绑定 DDNS 组<select v-model="groupDraft.ddns_group_id" name="ddns_group_id" class="form-select">
            <option value="">不绑定</option>
            <option v-for="group in editSnapshot.config.ddns.groups" :key="group.id" :value="group.id">
              {{ group.name }} · {{ group.zone }}
            </option>
          </select></label
        >
        <label class="toggle-row"
          >启用此组<input v-model="groupDraft.enabled" name="enabled" type="checkbox"
        /></label>
        <div class="modal-footer">
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="isSavingConfig"
            @click="groupDialog.close()"
          >
            取消</button
          ><button type="submit" class="btn btn-primary" :disabled="isSavingConfig">保存反代组</button>
        </div>
      </form>
    </dialog>

    <dialog ref="healthDialog" class="modal-dialog" aria-labelledby="health-title" @cancel="blockBusyClose">
      <div class="modal-header">
        <h2 id="health-title" class="modal-title">服务健康检查</h2>
        <button
          class="close-modal-btn"
          aria-label="关闭"
          :disabled="isSavingConfig"
          @click="healthDialog.close()"
        >
          <X :size="18" />
        </button>
      </div>
      <form class="modal-body" @submit.prevent="saveHealthSettings">
        <p v-if="formError" class="page-error" role="alert">{{ formError }}</p>
        <label class="toggle-row">
          <span>启用服务器上游检查</span>
          <input v-model="healthDraft.enabled" type="checkbox" name="enabled" />
        </label>
        <label class="form-group"
          >检查间隔 秒<input
            v-model.number="healthDraft.interval"
            name="interval"
            type="number"
            min="30"
            max="86400"
            class="form-input"
            required
        /></label>
        <div class="modal-footer">
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="isSavingConfig"
            @click="healthDialog.close()"
          >
            取消
          </button>
          <button type="submit" class="btn btn-primary" :disabled="isSavingConfig">
            {{ isSavingConfig ? '保存中…' : '保存检查设置' }}
          </button>
        </div>
      </form>
    </dialog>
    <dialog ref="healthDetailDialog" class="modal-dialog" aria-labelledby="health-detail-title">
      <div class="modal-header">
        <h2 id="health-detail-title" class="modal-title">上游健康诊断</h2>
        <button aria-label="关闭" class="close-modal-btn" @click="healthDetailDialog.close()">
          <X :size="18" />
        </button>
      </div>
      <div class="modal-body">
        <div class="health-diagnostic-card">
          <div class="diagnostic-header">
            <span class="diagnostic-badge" :class="healthDetail?.status >= 200 && healthDetail?.status < 400 ? 'status-ok' : 'status-bad'">
              {{ healthDetail?.label || '等待检测' }}
            </span>
            <span v-if="healthDetail?.duration_ms" class="diagnostic-latency mono-text">
              {{ healthDetail.duration_ms }} ms
            </span>
          </div>
          <p v-if="healthDetail?.message" class="diagnostic-msg">{{ healthDetail.message }}</p>
          <div v-if="healthDetail?.checked_at" class="diagnostic-meta">
            <span>最近检查：{{ new Date(healthDetail.checked_at).toLocaleString() }}</span>
            <span v-if="healthDetail.status">HTTP {{ healthDetail.status }}</span>
          </div>
        </div>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" @click="healthDetailDialog.close()">关闭</button>
      </div>
    </dialog>
    <dialog
      ref="discoveryDialog"
      class="modal-dialog"
      aria-labelledby="discovery-title"
      @close="discoveryOpen = false"
      @cancel="blockBusyClose"
    >
      <div class="modal-header">
        <h2 id="discovery-title" class="modal-title">发现内网服务</h2>
        <button
          aria-label="关闭"
          class="close-modal-btn"
          :disabled="isSavingConfig"
          @click="discoveryDialog.close()"
        >
          <X :size="18" />
        </button>
      </div>
      <div class="modal-body">
        <ServiceDiscovery v-if="discoveryOpen" :snapshot="editSnapshot" @saved="discoverySaved" />
      </div>
    </dialog>
  </div>
</template>

<script setup>
import { newID } from '@/api/ids'
import { computed, nextTick, onMounted, onUnmounted, ref, shallowRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  AlertCircle,
  ArrowRight,
  ChevronDown,
  Copy,
  Edit3,
  Globe,
  Key,
  Lock,
  Network,
  Plus,
  Search,
  Trash2,
  X
} from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import RestartNotice from '@/components/RestartNotice.vue'
import RouteImagePicker from '@/components/RouteImagePicker.vue'
import { routeImageUrl } from '@/api/icons'
import ServiceDiscovery from '@/components/ServiceDiscovery.vue'
import { adminApi } from '@/api/admin'
import { defaultRouteTLS, replaceConfigItem, routeAccessURL, routeHost, routeKey, routeUsesFullHost } from '@/api/configuration'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useToast } from '@/composables/useToast'

const {
  gatewayConfig,
  gatewayConfigSnapshot,
  gatewayStatus,
  isSavingConfig,
  refreshConfig,
  refreshStatus,
  saveConfig
} = useGatewayStatus()
const toast = useToast()
const currentRoute = useRoute()
const router = useRouter()
const routes = computed(() => gatewayConfig.value?.routes ?? [])
const proxyGroups = computed(() => gatewayConfig.value?.groups ?? [])
const selectedGroup = ref('ALL')
const searchQuery = ref('')
const loading = ref(false)
const loadError = ref('')
const health = ref(null)
const healthDetail = ref(null)
const controller = new AbortController()
const editSnapshot = shallowRef({ config: { groups: [], firewalls: [], ddns: { groups: [] } } })
const formError = ref('')
const routeDialog = ref(null)
const groupDialog = ref(null)
const healthDialog = ref(null)
const healthDetailDialog = ref(null)
const discoveryDialog = ref(null)
const routeOpen = ref(false)
const discoveryOpen = ref(false)
const originalRouteId = ref(null)
const originalGroupId = ref(null)
const originalRoute = ref(null)
const routeDraft = ref({ auth: {} })
const groupDraft = ref({})
const healthDraft = ref({})
const routePassword = ref('')
const clearRoutePassword = ref(false)
const fullHost = ref(false)
const freezeMinutes = ref(60)
const imageBusy = ref(false)
const routeError = ref(null)
const filteredRoutes = computed(() =>
  routes.value.filter((route) => {
    if (selectedGroup.value === 'INACTIVE' && route.enabled) return false
    if (!['ALL', 'INACTIVE'].includes(selectedGroup.value) && route.group_id !== selectedGroup.value)
      return false
    const query = searchQuery.value.trim().toLowerCase()
    return [route.name, route.host, route.upstream, accessURL(route)].some((value) =>
      (value || '').toLowerCase().includes(query)
    )
  })
)
const groupedRoutes = computed(() => proxyGroups.value
  .filter(group => ['ALL', 'INACTIVE'].includes(selectedGroup.value) || group.id === selectedGroup.value)
  .map(group => ({ group, items: filteredRoutes.value.filter(route => route.group_id === group.id) }))
  .filter(section => section.items.length || (!searchQuery.value.trim() && selectedGroup.value !== 'INACTIVE')))
const GROUP_COLLAPSE_KEY = 'gatehome_route_collapsed_groups'
const collapsedGroups = ref(loadCollapsedGroups())
function loadCollapsedGroups() {
  try {
    const saved = JSON.parse(localStorage.getItem(GROUP_COLLAPSE_KEY) || '[]')
    return new Set(Array.isArray(saved) ? saved : [])
  } catch { return new Set() }
}
function saveCollapsedGroups() {
  try { localStorage.setItem(GROUP_COLLAPSE_KEY, JSON.stringify([...collapsedGroups.value])) } catch {}
}
function isGroupCollapsed(id) {
  return collapsedGroups.value.has(id)
}
function toggleGroupCollapse(id) {
  if (collapsedGroups.value.has(id)) collapsedGroups.value.delete(id)
  else collapsedGroups.value.add(id)
  saveCollapsedGroups()
}
watch([searchQuery, selectedGroup], () => {
  for (const { group } of groupedRoutes.value) collapsedGroups.value.delete(group.id)
  saveCollapsedGroups()
})
const resolvedHost = computed(() =>
  routeHost(
    routeDraft.value.host || '',
    editSnapshot.value.config.groups.find((group) => group.id === routeDraft.value.group_id),
    fullHost.value
  )
)
const passwordRequired = computed(() => {
  const previous = originalRoute.value
  return (
    !previous ||
    !editSnapshot.value.route_passwords_configured?.[routeKey(previous)] ||
    routeKey(previous) !== `${routeDraft.value.group_id}/${resolvedHost.value}` ||
    previous.auth.username !== routeDraft.value.auth.username
  )
})

function getGroupCount(id) {
  return routes.value.filter((route) => route.group_id === id).length
}
function groupName(id) {
  return proxyGroups.value.find((group) => group.id === id)?.name || id
}
function accessURL(route) {
  return routeAccessURL(route, proxyGroups.value.find(group => group.id === route.group_id))
}
function firewallName(id) {
  return gatewayConfig.value?.firewalls.find((firewall) => firewall.id === id)?.name || id
}
function groupState(group) {
  if (!gatewayStatus.value) return '正在读取监听状态'
  const running = gatewayStatus.value?.listeners?.find((item) => item.id === group.id && item.enabled)
  if (!group.enabled) return running ? '停用待重启' : '已停用'
  if (!running) return '启用待重启'
  return running.http_port === group.http_port && running.https_port === group.https_port
    ? '正在监听'
    : '端口修改待重启'
}
function routeHealth(route) {
  if (!route.enabled) return '路由已停用'
  if (!proxyGroups.value.find((group) => group.id === route.group_id)?.enabled) return '反代组已停用'
  const result = health.value?.routes?.[route.id]
  return result ? `${result.label}${result.healthy ? ` · ${result.duration_ms} ms` : ''}` : '等待上游探测'
}
async function refreshHealth() {
  try {
    const value = await adminApi.serviceHealth(controller.signal)
    if (!controller.signal.aborted) health.value = value
  } catch (err) {
    if (!controller.signal.aborted && err.status !== 401) loadError.value = err.message
  }
}
async function reload() {
  if (loading.value || isSavingConfig.value) return
  loading.value = true
  loadError.value = ''
  try {
    await Promise.all([refreshConfig(controller.signal), refreshStatus(controller.signal), refreshHealth()])
  } catch (err) {
    if (!controller.signal.aborted) loadError.value = err.message
  } finally {
    loading.value = false
  }
}
function beginEdit() {
  editSnapshot.value = structuredClone(gatewayConfigSnapshot.value)
  formError.value = ''
}
function openRoute(route) {
  beginEdit()
  originalRouteId.value = route ? route.id || routeKey(route) : null
  originalRoute.value = route ? structuredClone(route) : null
  routeDraft.value = route
    ? structuredClone(route)
    : {
        id: newID(),
        name: '',
        group_id: proxyGroups.value.some((group) => group.id === selectedGroup.value)
          ? selectedGroup.value
          : proxyGroups.value[0].id,
        host: '',
        upstream: '',
        tls: false,
        enabled: true,
        firewall_id: '',
        image: '',
        auth: { enabled: false, username: '', failure_limit: 5, freeze_seconds: 3600 }
      }
  routeDraft.value.auth = {
    ...routeDraft.value.auth,
    failure_limit: routeDraft.value.auth.failure_limit || 5
  }
  fullHost.value = routeUsesFullHost(route?.host, proxyGroups.value.find((group) => group.id === routeDraft.value.group_id))
  if (!route) routeDraft.value.tls = defaultRouteTLS(proxyGroups.value.find((group) => group.id === routeDraft.value.group_id))
  clearRoutePassword.value = false
  freezeMinutes.value = (routeDraft.value.auth.freeze_seconds || 3600) / 60
  routePassword.value = ''
  imageBusy.value = false
  routeOpen.value = true
  nextTick(() => routeDialog.value.showModal())
}
function closeRoute() {
  routeOpen.value = false
  routePassword.value = ''
  clearRoutePassword.value = false
  imageBusy.value = false
}
function blockBusyClose(event) {
  if (isSavingConfig.value || imageBusy.value) event.preventDefault()
}
async function saved(message) {
  toast.success(message)
  try {
    await refreshStatus(controller.signal)
  } catch (err) {
    if (!controller.signal.aborted && err.status !== 401)
      toast.warn('配置已保存，监听状态刷新失败，请稍后刷新')
  }
  refreshHealth()
}
async function saveRoute() {
  if (isSavingConfig.value || imageBusy.value) return
  formError.value = ''
  try {
    const record = {
      ...routeDraft.value,
      host: resolvedHost.value,
      name: routeDraft.value.name.trim(),
      upstream: routeDraft.value.upstream.trim(),
      auth: { ...routeDraft.value.auth, freeze_seconds: Number(freezeMinutes.value) * 60 }
    }
    const credentials = {}
    if (clearRoutePassword.value) {
      if (record.auth.enabled) throw new Error('请先关闭独立访问验证，再清除已保存密码')
      if (routePassword.value) throw new Error('填写新密码和清除密码不能同时选择')
      credentials.route_passwords = { [routeKey(record)]: '' }
    }
    if (record.auth.enabled) {
      if (passwordRequired.value && !routePassword.value)
        throw new Error('域名、所属组或访问账号变化后，请重新设置独立访问密码')
      if (routePassword.value) credentials.route_passwords = { [routeKey(record)]: routePassword.value }
    }
    await saveConfig(
      editSnapshot.value,
      replaceConfigItem(editSnapshot.value, 'routes', record, originalRouteId.value),
      credentials
    )
    routeDialog.value.close()
    await saved('反代路由已保存')
  } catch (err) {
    formError.value = err.message
    await nextTick()
    routeError.value?.focus()
  }
}
function openGroup(group) {
  beginEdit()
  originalGroupId.value = group?.id ?? null
  groupDraft.value = group
    ? structuredClone(group)
    : {
        id: newID(),
        name: '',
        http_port: 0,
        https_port: 0,
        enabled: true,
        domain_suffix: '',
        ddns_group_id: ''
      }
  nextTick(() => groupDialog.value.showModal())
}
async function saveGroup() {
  if (isSavingConfig.value) return
  try {
    const record = {
      ...groupDraft.value,
      name: groupDraft.value.name.trim(),
      domain_suffix: (groupDraft.value.domain_suffix || '').trim().toLowerCase()
    }
    await saveConfig(
      editSnapshot.value,
      replaceConfigItem(editSnapshot.value, 'groups', record, originalGroupId.value)
    )
    groupDialog.value.close()
    await saved('反代组已保存，监听变化在重启后生效')
  } catch (err) {
    formError.value = err.message
  }
}
async function changeItem(collection, record, remove = false) {
  if (isSavingConfig.value) return
  const snapshot = structuredClone(gatewayConfigSnapshot.value)
  try {
    const id = record.id || routeKey(record)
    const config = remove
      ? {
          ...snapshot.config,
          [collection]: snapshot.config[collection].filter((item) => (item.id || routeKey(item)) !== id)
        }
      : replaceConfigItem(snapshot, collection, { ...record, enabled: !record.enabled }, id)
    await saveConfig(snapshot, config)
    if (remove && selectedGroup.value === id) selectedGroup.value = 'ALL'
    await saved('配置已保存')
  } catch (err) {
    toast.error(err.message)
  }
}
function toggleRoute(route, event) {
  event.target.checked = route.enabled
  return changeItem('routes', route)
}
function toggleGroup(group) {
  return changeItem('groups', group)
}
function deleteRoute(route) {
  if (confirm(`删除反代路由「${route.name || route.host}」？`)) return changeItem('routes', route, true)
}
function deleteGroup(group) {
  if (confirm(`删除反代组「${group.name}」？`)) return changeItem('groups', group, true)
}
async function copyURL(url) {
  try {
    try {
      await navigator.clipboard.writeText(url)
    } catch {
      // LAN HTTP pages have no Clipboard API; copy during the original click.
      if (document.visibilityState === 'hidden' || !document.hasFocus()) throw new Error('Page inactive')
      const active = document.activeElement
      const input = document.createElement('textarea')
      input.value = url
      input.readOnly = true
      input.style.cssText = 'position:fixed;left:-9999px;top:0;font-size:16px'
      document.body.append(input)
      try {
        input.select()
        if (!document.execCommand('copy')) throw new Error('Copy rejected')
      } finally {
        input.remove()
        if (document.visibilityState === 'visible' && document.hasFocus()) active?.focus({ preventScroll: true })
      }
    }
    toast.success('访问地址已复制')
  } catch {
    toast.error('复制失败，请手动复制访问地址')
  }
}
function showHealth(route) {
  healthDetail.value = health.value?.routes?.[route.id] ?? null
  healthDetailDialog.value.showModal()
}
function openHealthSettings() {
  beginEdit()
  healthDraft.value = { ...editSnapshot.value.config.service_health }
  if (!healthDraft.value.interval) healthDraft.value = { enabled: true, interval: 60 }
  healthDialog.value.showModal()
}
async function saveHealthSettings() {
  if (isSavingConfig.value) return
  try {
    await saveConfig(editSnapshot.value, {
      ...editSnapshot.value.config,
      service_health: { ...healthDraft.value }
    })
    healthDialog.value.close()
    await saved('健康检查设置已保存')
  } catch (err) {
    formError.value = err.message
  }
}
function openDiscovery() {
  beginEdit()
  discoveryOpen.value = true
  nextTick(() => discoveryDialog.value.showModal())
}
function discoverySaved() {
  discoveryDialog.value.close()
  saved('已创建所选服务的反代路由')
}
function routeCategoryClass(route) {
  const name = (route.name || route.host || '').toLowerCase()
  if (name.includes('cloud') || name.includes('nas') || name.includes('storage') || name.includes('pan') || name.includes('drive')) return 'storage'
  if (name.includes('vault') || name.includes('pass') || name.includes('auth') || name.includes('key') || name.includes('safe')) return 'security'
  if (name.includes('home') || name.includes('iot') || name.includes('hass') || name.includes('zigbee')) return 'iot'
  if (name.includes('media') || name.includes('plex') || name.includes('jelly') || name.includes('emby') || name.includes('video') || name.includes('music')) return 'media'
  if (name.includes('git') || name.includes('code') || name.includes('dev') || name.includes('gitea')) return 'dev'
  return 'system'
}

function changeRouteGroup() {
  const group = proxyGroups.value.find((item) => item.id === routeDraft.value.group_id)
  routeDraft.value.tls = defaultRouteTLS(group)
}

watch(
  [() => currentRoute.query.new, gatewayConfig],
  ([requested, config]) => {
    if (requested !== '1' || !config || routeOpen.value) return
    router.replace({ query: { ...currentRoute.query, new: undefined } })
    if (config.groups.length) openRoute()
    else openGroup()
  },
  { immediate: true }
)
let healthTimer
onMounted(() => {
  reload()
  healthTimer = setInterval(refreshHealth, 30000)
})
onUnmounted(() => {
  clearInterval(healthTimer)
  controller.abort()
  routePassword.value = ''
})
</script>

<style scoped>
.header-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.page-error { color: var(--bad); font-size: 13px; overflow-wrap: anywhere; }
.form-note { color: var(--muted); font-size: 12px; margin: 0; overflow-wrap: anywhere; }
.route-group {
  min-width: 0;
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
}
.route-group-header {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
}
.route-group-toggle {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 1;
  min-width: 0;
  min-height: 44px;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--ink);
  text-align: left;
  cursor: pointer;
}
.route-group-toggle > svg { flex-shrink: 0; color: var(--muted); transition: transform 0.15s ease; }
.route-group-toggle > svg.folded { transform: rotate(-90deg); }
.route-group-toggle:hover strong { color: var(--brand-light); }
.route-group-info { display: grid; gap: 5px; min-width: 0; }
.route-group-title { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; font-size: 14px; }
.route-group-title strong { overflow-wrap: anywhere; }
.route-group-meta { color: var(--muted); font-size: 12px; overflow-wrap: anywhere; }
.route-group-body { padding: 0 16px 16px; }
.group-actions { flex-wrap: wrap; flex-shrink: 0; }
.group-empty { padding: 12px 0; font-size: 12px; color: var(--muted); }
@media (prefers-reduced-motion: reduce) {
  .route-group-toggle > svg { transition: none; }
}
.health-detail { color: var(--muted); text-align: left; font-size: 12px; margin-bottom: 12px; }
.auth-fields {
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
  background: rgba(255, 255, 255, 0.02);
  display: grid;
  gap: 14px;
  padding: 16px;
  min-width: 0;
}
.health-diagnostic-card {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 16px;
  border-radius: var(--radius-md);
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid var(--line);
}
.diagnostic-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.diagnostic-badge {
  font-size: 12px;
  font-weight: 600;
  padding: 3px 10px;
  border-radius: 999px;
}
.diagnostic-badge.status-ok {
  background: rgba(16, 185, 129, 0.15);
  color: var(--ok);
  border: 1px solid rgba(16, 185, 129, 0.3);
}
.diagnostic-badge.status-bad {
  background: rgba(244, 63, 94, 0.15);
  color: var(--bad);
  border: 1px solid rgba(244, 63, 94, 0.3);
}
.diagnostic-latency {
  font-size: 13px;
  color: var(--brand-cyan);
  font-weight: 600;
}
.diagnostic-msg {
  font-size: 13px;
  color: var(--ink);
  line-height: 1.5;
  margin: 0;
}
.diagnostic-meta {
  display: flex;
  gap: 12px;
  font-size: 11.5px;
  color: var(--faint);
}
.routes-page { display: flex; flex-direction: column; gap: 20px; }
.page-topbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 16px; }
.page-title { font-size: 22px; font-weight: 700; color: var(--ink); }
.page-subtitle { font-size: 13px; color: var(--muted); margin-top: 3px; }

.filter-bar { display: flex; align-items: center; justify-content: space-between; gap: 16px; flex-wrap: wrap; }
.chips-group { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.chip-btn {
  display: inline-flex; align-items: center; gap: 6px; padding: 5px 12px;
  border-radius: var(--radius-pill); background: var(--strip-bg);
  border: 1px solid var(--line); color: var(--muted); font-size: 12px; font-weight: 500;
}
.chip-btn.active {
  background: rgba(99, 102, 241, 0.18); border-color: rgba(99, 102, 241, 0.4);
  color: #fff; font-weight: 600;
}
.theme-light .chip-btn.active { color: var(--brand); }
.chip-count {
  font-family: var(--mono); font-size: 10px; background: rgba(148, 163, 184, 0.15);
  padding: 1px 5px; border-radius: 999px;
}

.search-box {
  display: flex; align-items: center; gap: 8px; padding: 6px 12px;
  background: var(--input-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); width: 260px;
}
.search-box-icon { color: var(--faint); }
.search-input { border: none; background: transparent; color: var(--ink); font-size: 12.5px; flex: 1; }
.clear-btn { color: var(--faint); padding: 2px; }

/* Grid */
.routes-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 16px;
}
.route-card {
  padding: 18px;
  display: flex;
  flex-direction: column;
  min-height: 240px;
}
.route-card :deep(.card-inner) {
  display: flex;
  flex-direction: column;
  height: 100%;
}
.route-card.inactive { opacity: 0.65; filter: grayscale(0.2); }

.card-head { display: flex; align-items: center; gap: 12px; margin-bottom: 14px; }
.app-icon-wrap {
  width: 36px; height: 36px; border-radius: var(--radius-sm);
  display: flex; align-items: center; justify-content: center;
  border: 1px solid var(--line); flex-shrink: 0;
}
.app-icon-wrap img { display: block; object-fit: contain; flex-shrink: 0; }
.app-icon-wrap.system { background: rgba(99, 102, 241, 0.12); color: var(--brand-light); }
.app-icon-wrap.storage { background: rgba(6, 182, 212, 0.12); color: var(--brand-cyan); }
.app-icon-wrap.security { background: rgba(245, 158, 11, 0.12); color: var(--warn); }
.app-icon-wrap.iot { background: rgba(16, 185, 129, 0.12); color: var(--ok); }
.app-icon-wrap.media { background: rgba(244, 63, 94, 0.12); color: var(--bad); }
.app-icon-wrap.dev { background: rgba(168, 85, 247, 0.12); color: #c084fc; }
.app-icon-wrap.ops { background: rgba(56, 189, 248, 0.12); color: #38bdf8; }

.card-title-group { flex: 1; min-width: 0; }
.card-title-row { display: flex; align-items: center; gap: 6px; }
.service-name { font-size: 13.5px; font-weight: 600; color: var(--ink); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.service-category { font-size: 11px; color: var(--faint); margin-top: 1px; }

.endpoint-info {
  background: var(--strip-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); padding: 9px 11px; margin-bottom: 12px;
  display: flex; flex-direction: column; gap: 6px;
}
.info-row { display: flex; align-items: center; gap: 7px; font-size: 12px; }
.domain-row { cursor: pointer; color: var(--ink); }
.domain-row:hover .domain-text { color: var(--brand-light); }
.domain-row:hover .copy-icon { opacity: 1; }
.domain-text { font-weight: 500; flex: 1; overflow-wrap: anywhere; }
.copy-icon { opacity: 0; color: var(--faint); transition: opacity 0.2s ease; }
.target-row { color: var(--muted); }
.target-arrow { color: var(--brand-cyan); }

.feature-badges { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; margin-bottom: 10px; }
.feature-badge {
  display: inline-flex; align-items: center; gap: 4px; font-size: 10.5px; font-weight: 600;
  padding: 2px 7px; border-radius: 4px;
}
.feature-badge.ssl { background: rgba(16, 185, 129, 0.12); color: var(--ok); border: 1px solid rgba(16, 185, 129, 0.2); }
.feature-badge.fw { background: rgba(99, 102, 241, 0.12); color: var(--brand-light); border: 1px solid rgba(99, 102, 241, 0.2); }
.feature-badge.auth { background: rgba(245, 158, 11, 0.12); color: var(--warn); border: 1px solid rgba(245, 158, 11, 0.2); }
.feature-badge.ws { background: rgba(6, 182, 212, 0.12); color: var(--brand-cyan); border: 1px solid rgba(6, 182, 212, 0.2); }
.feature-badge.hits { background: var(--strip-bg); color: var(--faint); border: 1px solid var(--line); font-family: var(--mono); }

.health-detail {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 3px 9px;
  background: var(--strip-bg);
  border: 1px solid var(--line);
  border-radius: var(--radius-pill);
  color: var(--muted);
  font-size: 11px;
  margin-bottom: 12px;
  width: fit-content;
  cursor: pointer;
  transition: var(--transition-smooth);
  text-align: left;
}
.health-detail:hover {
  border-color: var(--card-border-hover);
  color: var(--ink);
  background: var(--card-hover-bg);
}

.card-actions {
  margin-top: auto;
  display: flex; align-items: center; justify-content: space-between; border-top: 1px solid var(--line); padding-top: 12px;
}
.latency-indicator { display: flex; align-items: center; gap: 4px; font-size: 11.5px; }
.latency-label { color: var(--faint); }
.latency-val { color: var(--ok); font-family: var(--mono); font-weight: 600; }
.latency-val.zero { color: var(--faint); }
.action-btn-group { display: flex; gap: 4px; }
.text-bad { color: var(--bad) !important; }

.empty-state {
  padding: 60px 20px; text-align: center; display: flex; flex-direction: column; align-items: center; gap: 10px;
}
.empty-icon { color: var(--faint); }
.empty-title { font-size: 15px; font-weight: 600; color: var(--ink); }
.empty-desc { font-size: 12.5px; color: var(--muted); }

.route-card { min-width: 0; }
.info-text { min-width: 0; overflow-wrap: anywhere; }
.domain-row { width: 100%; background: transparent; border: 0; text-align: left; }
.domain-row > svg { flex-shrink: 0; }
.domain-row:disabled { cursor: default; color: var(--muted); }

.toggles-panel {
  background: var(--strip-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); padding: 12px 14px; display: flex; flex-direction: column; gap: 10px;
}
.toggle-info { display: flex; flex-direction: column; }
.toggle-title { font-size: 12.5px; font-weight: 600; color: var(--ink); }
.toggle-desc { font-size: 11px; color: var(--faint); }
.auth-sub-fields { padding: 8px 0; border-top: 1px dashed var(--line); margin-top: 4px; }

@media (max-width: 640px) {
  .route-group-header { flex-wrap: wrap; padding: 10px 12px; }
  .route-group-toggle { flex-basis: 100%; }
  .group-actions { width: 100%; }
  .group-actions .btn { min-height: 44px; flex: 1; justify-content: center; }
  .route-group-body { padding: 0 12px 12px; }
  .domain-row { min-height: 44px; }
  .routes-grid { grid-template-columns: 1fr; }
  .filter-bar { flex-direction: column; align-items: stretch; }
  .search-box { width: 100%; }
  .header-actions { width: 100%; }
  .header-actions .btn { flex: 1 1 calc(50% - 6px); justify-content: center; }
}
</style>
