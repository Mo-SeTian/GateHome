<template>
  <div class="report-page animate-fade-in">
    <header class="report-heading">
      <div>
        <h1>AI 管理 · MCP</h1>
        <p>通过 Streamable HTTP 连接 AI，为各客户端配置独立权限、有效期与首页账号</p>
      </div>
      <div class="report-actions">
        <button class="btn btn-secondary" :disabled="busy || loading" @click="load">刷新设置</button
        ><button class="btn btn-secondary" :disabled="busy || loadingUsers" @click="loadUsers">
          {{ loadingUsers ? '读取中…' : '刷新 SunPanel 账号' }}
        </button>
      </div>
    </header>
    <p v-if="error" class="report-error" role="alert">{{ error }}</p>
    <p v-if="userError" class="report-warning" role="status">{{ userError }}</p>
    <div v-if="data" class="mcp-layout">
      <SpotlightCard class="report-panel">
        <div class="report-heading">
          <h2>服务与兼容 Token 授权</h2>
          <span class="badge" :class="data.config.enabled ? 'badge-ok' : 'badge-neutral'">{{
            data.config.enabled ? '已启用' : '未启用'
          }}</span>
        </div>
        <form @submit.prevent="saveGlobal">
          <fieldset :disabled="busy || loading">
            <McpPermissions v-model="draft" :users="users" title="服务总开关与兼容 Token 权限" />
            <p class="report-note">
              关闭服务总开关后全部客户端停止访问；每个专用客户端使用自身权限。首次启用会生成兼容 Token。
            </p>
            <button class="btn btn-primary" type="submit">保存授权设置</button>
          </fieldset>
        </form>
        <p class="report-note">
          兼容 Token：{{ data.token_configured ? '已配置，不能读取原值' : '尚未配置' }}
        </p>
        <button class="btn btn-secondary" :disabled="busy || loading" @click="rotateLegacy">
          {{ data.token_configured ? '重新生成兼容 Token' : '创建兼容 Token' }}
        </button>
      </SpotlightCard>
      <SpotlightCard class="report-panel">
        <h2>客户端接入</h2>
        <label>MCP 连接地址<input class="form-input mono-text" :value="endpoint" readonly /></label>
        <p class="report-note">
          认证方式：Authorization: Bearer YOUR_API_TOKEN。远程接入请使用可访问的 HTTPS 管理域名并保留 /mcp。
        </p>
        <textarea
          class="form-input mono-text"
          aria-label="MCP 客户端配置示例"
          :value="clientConfig"
          rows="10"
          readonly
          spellcheck="false"
        />
        <button class="btn btn-secondary" @click="copy(clientConfig)">复制配置示例</button>
      </SpotlightCard>
    </div>
    <SpotlightCard v-if="newToken" class="report-panel token-panel">
      <h2>新 Token · 仅显示一次</h2>
      <p class="report-note">{{ tokenLabel }}。请保存到客户端；关闭、刷新设置或离开本页后无法再次读取。</p>
      <input
        class="form-input mono-text"
        aria-label="新 API Token"
        :type="showToken ? 'text' : 'password'"
        :value="newToken"
        readonly
        autocomplete="off"
        spellcheck="false"
      />
      <div class="report-actions">
        <button class="btn btn-secondary" @click="copy(newToken)">复制 Token</button
        ><button class="btn btn-secondary" :aria-pressed="showToken" @click="showToken = !showToken">
          {{ showToken ? '隐藏 Token' : '显示 Token' }}</button
        ><button class="btn btn-secondary" @click="testToken = newToken">用于本页调试</button
        ><button class="btn btn-secondary" @click="clearToken">关闭凭据</button>
      </div>
    </SpotlightCard>
    <SpotlightCard v-if="data" class="report-panel">
      <div class="report-heading">
        <h2>客户端 Token（{{ data.clients.length }}）</h2>
        <button class="btn btn-primary" :disabled="busy || loading" @click="editClient()">创建客户端</button>
      </div>
      <p class="report-note">
        这是已授权客户端列表。每次重新生成会使该客户端旧 Token 失效；撤销后不能重新启用。
      </p>
      <div v-if="data.clients.length" class="report-table-wrap">
        <table class="report-table">
          <thead>
            <tr>
              <th>客户端 / 状态</th>
              <th>权限 / 首页账号</th>
              <th>有效期</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="client in data.clients" :key="client.id" :data-client-id="client.id">
              <td>
                {{ client.name }}<small>{{ clientState(client) }}</small>
              </td>
              <td>
                {{ permissionText(client.config)
                }}<small>{{
                  client.config.sunpanel_user_id
                    ? `首页账号 #${client.config.sunpanel_user_id}`
                    : '未授权首页'
                }}</small>
              </td>
              <td>
                {{ client.expires_at ? new Date(client.expires_at).toLocaleString() : '长期有效'
                }}<small>创建于 {{ new Date(client.created_at).toLocaleString() }}</small>
              </td>
              <td>
                <div class="report-actions">
                  <button
                    class="btn btn-secondary btn-sm"
                    :disabled="busy || loading || !!client.revoked_at"
                    @click="editClient(client)"
                  >
                    编辑</button
                  ><button
                    class="btn btn-secondary btn-sm"
                    :disabled="busy || loading || !!client.revoked_at"
                    @click="clientOperation(client, 'rotate')"
                  >
                    重新生成</button
                  ><button
                    class="btn btn-secondary btn-sm"
                    :disabled="busy || loading || !!client.revoked_at"
                    @click="clientOperation(client, 'revoke')"
                  >
                    撤销
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else class="report-empty">暂无客户端，创建后获取专用 Token。</p>
    </SpotlightCard>
    <SpotlightCard class="report-panel">
      <h2>实际工具调试</h2>
      <p class="report-note">
        输入具有所需权限的 Token 后读取真实工具目录。Token
        仅在本页内存中使用；调用修改或维护类工具前会要求确认。
      </p>
      <div class="report-form">
        <label class="wide"
          >调试 Token<input
            v-model.trim="testToken"
            name="test_token"
            class="form-input"
            type="password"
            autocomplete="off"
            spellcheck="false"
            @input="resetTools"
        /></label>
        <div class="report-actions wide">
          <button class="btn btn-secondary" :disabled="invoking || !testToken" @click="connect">
            {{ invoking ? '请求中…' : '连接并读取工具' }}</button
          ><button class="btn btn-secondary" :disabled="invoking" @click="disconnect">清除调试凭据</button>
        </div>
        <template v-if="tools.length"
          ><label class="wide"
            >工具（{{ tools.length }}）<select
              v-model="toolName"
              class="form-select"
              name="tool"
              @change="resetToolArgs"
            >
              <option v-for="tool in tools" :key="tool.name" :value="tool.name">{{ tool.name }}</option>
            </select></label
          >
          <p class="report-note wide">{{ selectedTool?.description }}</p>
          <details class="wide">
            <summary>参数 JSON Schema</summary>
            <pre>{{ JSON.stringify(selectedTool?.inputSchema, null, 2) }}</pre>
          </details>
          <label class="wide"
            >调用参数（JSON 对象）<textarea
              v-model="toolArgs"
              name="tool_args"
              class="form-input mono-text"
              rows="5"
              spellcheck="false"
            /></label
          ><button class="btn btn-primary" :disabled="invoking || !testToken" @click="invoke">
            调用选中工具
          </button></template
        >
      </div>
      <p v-if="rpcError" class="report-error" role="alert">{{ rpcError }}</p>
      <pre v-if="result" class="rpc-result" aria-label="MCP 工具返回结果">{{ result }}</pre>
    </SpotlightCard>
    <SpotlightCard v-if="data?.guide" class="report-panel"
      ><details>
        <summary>AI 管理提示词与联动说明</summary>
        <textarea
          class="form-input"
          aria-label="AI 管理提示词"
          :value="data.guide"
          rows="12"
          readonly
        /><button class="btn btn-secondary" @click="copy(data.guide)">复制提示词</button>
      </details></SpotlightCard
    >
    <dialog
      ref="clientDialog"
      class="report-dialog"
      aria-labelledby="client-title"
      @close="editor = null"
      @cancel="busy && $event.preventDefault()"
    >
      <header>
        <h2 id="client-title">{{ editor?.client.id ? '编辑客户端' : '创建客户端 Token' }}</h2>
        <button class="btn btn-secondary btn-sm" :disabled="busy" @click="clientDialog.close()">关闭</button>
      </header>
      <form v-if="editor" @submit.prevent="saveClient">
        <fieldset :disabled="busy">
          <label
            >客户端名称<input
              v-model.trim="editor.client.name"
              name="name"
              class="form-input"
              maxlength="40"
              required /></label
          ><label
            >到期时间（留空长期有效）<input
              v-model="editor.expires"
              name="expires_at"
              class="form-input"
              type="datetime-local" /></label
          ><McpPermissions v-model="editor.client.config" :users="users" />
          <p v-if="clientError" class="report-error" role="alert">{{ clientError }}</p>
          <footer>
            <button class="btn btn-primary" type="submit">{{ busy ? '保存中…' : '保存客户端' }}</button>
          </footer>
        </fieldset>
      </form>
    </dialog>
  </div>
</template>
<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import SpotlightCard from '@/components/SpotlightCard.vue'
import McpPermissions from '@/components/McpPermissions.vue'
import { adminApi } from '@/api/admin'
import { mcpRequest } from '@/api/mcp'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
import { useToast } from '@/composables/useToast'
import '@/styles/reports.css'
const toast = useToast(),
  controller = new AbortController(),
  { refreshConfig } = useGatewayStatus()
const data = ref(null),
  draft = ref(null),
  users = ref([]),
  error = ref(''),
  userError = ref(''),
  loading = ref(false),
  loadingUsers = ref(false),
  busy = ref(false)
const newToken = ref(''),
  tokenLabel = ref(''),
  showToken = ref(false),
  editor = ref(null),
  clientDialog = ref(null),
  clientError = ref('')
const testToken = ref(''),
  tools = ref([]),
  toolName = ref(''),
  toolArgs = ref('{}'),
  result = ref(''),
  rpcError = ref(''),
  invoking = ref(false)
const endpoint = location.origin + '/mcp'
const clientConfig = JSON.stringify(
  { mcpServers: { gatehome: { url: endpoint, headers: { Authorization: 'Bearer YOUR_API_TOKEN' } } } },
  null,
  2
)
const selectedTool = computed(() => tools.value.find((tool) => tool.name === toolName.value))
function clearToken() {
  newToken.value = ''
  showToken.value = false
  tokenLabel.value = ''
}
function resetTools() {
  tools.value = []
  toolName.value = ''
  result.value = ''
  rpcError.value = ''
}
function resetToolArgs() {
  toolArgs.value = '{}'
  result.value = ''
}
function disconnect() {
  testToken.value = ''
  resetTools()
}
function apply(response, label = '') {
  if (controller.signal.aborted) return
  clearToken()
  newToken.value = response.token || ''
  tokenLabel.value = label
  const safe = { ...response }
  delete safe.token
  data.value = { ...safe, clients: safe.clients || [] }
}
function defaults(config) {
  return { ...config, read: config.read || (!config.enabled && !config.write && !config.maintenance) }
}
async function load() {
  if (loading.value || busy.value) return
  loading.value = true
  error.value = ''
  clearToken()
  try {
    const response = await adminApi.mcp(controller.signal)
    if (!controller.signal.aborted) {
      apply(response)
      draft.value = defaults(response.config)
    }
  } catch (err) {
    if (!controller.signal.aborted) error.value = err.message
  } finally {
    loading.value = false
  }
}
async function loadUsers() {
  if (loadingUsers.value) return
  loadingUsers.value = true
  userError.value = ''
  try {
    const response = await adminApi.mcpUsers(controller.signal)
    if (!controller.signal.aborted) users.value = response
  } catch (err) {
    if (!controller.signal.aborted)
      userError.value = `无法读取首页账号：${err.message}。请确认 SunPanel 已启用并重启。`
  } finally {
    loadingUsers.value = false
  }
}
async function mutation(action, label) {
  if (busy.value || loading.value) return false
  busy.value = true
  error.value = ''
  clientError.value = ''
  clearToken()
  try {
    const response = await action()
    if (controller.signal.aborted) return false
    apply(response, label)
    toast.success('MCP 设置已保存')
    await refreshConfig(controller.signal)
    return true
  } catch (err) {
    if (!controller.signal.aborted) {
      error.value = err.message
      clientError.value = err.message
    }
    return false
  } finally {
    busy.value = false
  }
}
async function saveGlobal() {
  const config = { ...draft.value }
  if (await mutation(() => adminApi.saveMCP({ revision: data.value.revision, config }), '兼容 Token'))
    draft.value = defaults(data.value.config)
}
async function rotateLegacy() {
  if (data.value.token_configured && !confirm('重新生成后，兼容入口的旧 Token 将立即失效，确定继续？')) return
  await mutation(() => adminApi.rotateMCPToken(data.value.revision), '兼容 Token')
}
function clientState(client) {
  return client.revoked_at
    ? '已撤销'
    : client.expires_at && new Date(client.expires_at) <= new Date()
      ? '已过期'
      : client.config.enabled
        ? '已启用'
        : '已暂停'
}
function permissionText(config) {
  return (
    [
      ['read', '查询'],
      ['write', '修改'],
      ['maintenance', '维护']
    ]
      .filter(([key]) => config[key])
      .map(([, label]) => label)
      .join(' / ') || '无权限'
  )
}
async function editClient(client) {
  const expires = client?.expires_at
    ? new Date(
        new Date(client.expires_at).getTime() - new Date(client.expires_at).getTimezoneOffset() * 60000
      )
        .toISOString()
        .slice(0, 16)
    : ''
  editor.value = {
    revision: data.value.revision,
    expires,
    client: client
      ? JSON.parse(JSON.stringify(client))
      : {
          name: '',
          config: { enabled: true, read: true, write: false, maintenance: false, sunpanel_user_id: 0 }
        }
  }
  clientError.value = ''
  await nextTick()
  clientDialog.value.showModal()
}
async function saveClient() {
  const client = {
    ...editor.value.client,
    config: { ...editor.value.client.config },
    expires_at: editor.value.expires ? new Date(editor.value.expires).toISOString() : null
  }
  const revision = editor.value.revision
  if (await mutation(() => adminApi.saveMCPClient({ revision, client }), `客户端「${client.name}」`))
    clientDialog.value.close()
}
async function clientOperation(client, operation) {
  if (
    !confirm(
      operation === 'revoke'
        ? `撤销「${client.name}」？此客户端 Token 将立即失效。`
        : `重新生成「${client.name}」的 Token？旧 Token 将立即失效。`
    )
  )
    return
  await mutation(
    () => adminApi.saveMCPClient({ revision: data.value.revision, client, [operation]: true }),
    `客户端「${client.name}」`
  )
}
async function copy(value) {
  try {
    await navigator.clipboard.writeText(value)
    toast.success('已复制')
  } catch {
    toast.error('浏览器未允许复制，请在输入框中手动复制')
  }
}
async function connect() {
  if (invoking.value) return
  invoking.value = true
  resetTools()
  try {
    await mcpRequest(
      testToken.value,
      'initialize',
      {
        protocolVersion: '2025-03-26',
        capabilities: {},
        clientInfo: { name: 'GateHome-console', version: __GATEHOME_VERSION__ }
      },
      controller.signal
    )
    const response = await mcpRequest(testToken.value, 'tools/list', {}, controller.signal)
    if (!controller.signal.aborted) {
      tools.value = response.tools || []
      toolName.value = tools.value[0]?.name || ''
      toolArgs.value = '{}'
    }
  } catch (err) {
    if (!controller.signal.aborted) rpcError.value = err.message
  } finally {
    invoking.value = false
  }
}
async function invoke() {
  if (invoking.value) return
  let args
  try {
    args = JSON.parse(toolArgs.value)
    if (!args || typeof args !== 'object' || Array.isArray(args)) throw new Error()
  } catch {
    rpcError.value = '调用参数须为合法 JSON 对象'
    return
  }
  if (
    selectedTool.value?.annotations?.readOnlyHint !== true &&
    !confirm('此工具可能修改配置或执行维护操作，确认调用？')
  )
    return
  invoking.value = true
  rpcError.value = ''
  result.value = ''
  try {
    const response = await mcpRequest(
      testToken.value,
      'tools/call',
      { name: toolName.value, arguments: args },
      controller.signal
    )
    if (!controller.signal.aborted) {
      result.value = JSON.stringify(response, null, 2)
      if (response.isError) rpcError.value = '工具执行失败，详情见返回结果'
      else toast.success('MCP 工具调用完成')
    }
  } catch (err) {
    if (!controller.signal.aborted) rpcError.value = err.message
  } finally {
    invoking.value = false
  }
}
onMounted(load)
onBeforeUnmount(() => {
  controller.abort()
  clearToken()
  disconnect()
  toolArgs.value = '{}'
})
</script>
<style scoped>
.mcp-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 20px;
  align-items: start;
}
.report-panel :deep(.card-inner),
form,
form > fieldset {
  display: grid;
  gap: 16px;
  min-width: 0;
}
form > fieldset {
  margin: 0;
  padding: 0;
  border: 0;
}
.report-panel h2 {
  margin-bottom: 0;
}
label:not(.toggle-row):not(.check-row) {
  display: grid;
  gap: 7px;
  font-size: 12px;
  color: var(--ink-secondary);
  min-width: 0;
}
.token-panel {
  border-color: var(--warn);
}
pre {
  font-size: 12px;
  line-height: 1.65;
  overflow: auto;
  max-height: 440px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.rpc-result {
  background: var(--strip-bg);
  border: 1px solid var(--line);
  padding: 16px;
  border-radius: var(--radius-sm);
}
details > textarea,
details > button {
  margin-top: 14px;
}
summary {
  cursor: pointer;
  font-size: 13px;
  color: var(--ink-secondary);
}
@media (max-width: 900px) {
  .mcp-layout {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
