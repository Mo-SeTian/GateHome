<template>
  <div class="discovery">
    <form class="scan-fields" @submit.prevent="start">
      <label
        >内网 IP<input
          v-model="ip"
          class="form-input"
          required
          placeholder="192.168.1.10 或 ::1"
          :disabled="scanning || pending"
      /></label>
      <label
        >端口<input
          v-model="ports"
          class="form-input"
          placeholder="留空扫描常用端口，或填写 8080,9000-9010"
          :disabled="scanning || pending"
      /></label>
      <button type="submit" class="btn btn-secondary" :disabled="scanning || pending">开始扫描</button>
      <button v-if="scanning" type="button" class="btn btn-secondary" :disabled="pending" @click="stop">
        停止扫描
      </button>
    </form>
    <p v-if="scan" role="status">{{ scanLabel }} · 已检查 {{ scan.completed }} / {{ scan.total }} 个端口</p>
    <p v-if="error" role="alert">{{ error }}</p>
    <form v-if="rows.length" @submit.prevent="createRoutes">
      <label
        >导入到反代组
        <select v-model="groupId" name="group_id" class="form-select" :disabled="pending">
          <option v-for="group in snapshot.config.groups" :key="group.id" :value="group.id">
            {{ group.name }}
          </option>
        </select>
      </label>
      <label>来源 IP 防火墙
        <select v-model="firewallID" name="firewall_id" class="form-select" :disabled="pending">
          <option value="">不限制来源 IP</option>
          <option v-for="firewall in snapshot.config.firewalls" :key="firewall.id" :value="firewall.id">{{ firewall.name }}</option>
        </select>
      </label>
      <p class="report-note">此策略应用到本次导入的全部路由；不选择时允许所有来源。</p>
      <div v-for="row in rows" :key="row.port" class="scan-result">
        <label
          ><input v-model="row.selected" type="checkbox" :disabled="scanning || pending" />
          {{ row.name || row.upstream }}</label
        >
        <p>{{ row.upstream }} · HTTP {{ row.status }}</p>
        <p v-if="row.tls_untrusted">上游证书不受信任，实际反代连接仍须使用可信证书。</p>
        <label v-if="row.selected"
          >服务名称<input v-model="row.name" name="service_name" class="form-input" required maxlength="100" :disabled="pending" />
        </label>
        <label v-if="row.selected"
          >访问域名
          <input
            v-model="row.host"
            class="form-input"
            required
            :disabled="pending"
            :placeholder="group?.domain_suffix && !row.fullHost ? '子域名前缀，如 app.home' : '完整域名'"
          />
        </label>
        <label v-if="row.selected && group?.domain_suffix"><input v-model="row.fullHost" type="checkbox" :disabled="pending" />填写完整域名</label>
        <p v-if="row.selected" class="report-note">保存为 {{ routeHost(row.host, group, row.fullHost) || '—' }}</p>
        <label v-if="row.selected && row.icon"><input v-model="row.useImage" type="checkbox" :disabled="pending" />使用服务图片</label>
      </div>
      <label
        ><input v-model="tls" name="tls" type="checkbox" :disabled="pending" /> 使用 HTTPS 访问，需已有匹配证书</label
      >
      <button
        type="submit"
        class="btn btn-primary"
        :disabled="scanning || pending || isSavingConfig || !rows.some((row) => row.selected)"
      >
        {{ pending ? '正在导入…' : '创建所选反代路由' }}
      </button>
    </form>
    <p v-else-if="scan && !scanning">未发现 HTTP 服务，可调整 IP 或端口后重新扫描。</p>
  </div>
</template>

<script setup>
import { newID } from '@/api/ids'
import { computed, onUnmounted, ref, watch } from 'vue'
import { adminApi } from '@/api/admin'
import { defaultRouteTLS, routeHost } from '@/api/configuration'
import { useGatewayStatus } from '@/composables/useGatewayStatus'

const props = defineProps({ snapshot: { type: Object, required: true } })
const emit = defineEmits(['saved'])
const { saveConfig, isSavingConfig } = useGatewayStatus()
const ip = ref('')
const ports = ref('')
const scan = ref(null)
const rows = ref([])
const error = ref('')
const pending = ref(false)
const firewallID = ref('')
const groupId = ref(props.snapshot.config.groups[0]?.id ?? '')
const group = computed(() => props.snapshot.config.groups.find((item) => item.id === groupId.value))
const tls = ref(defaultRouteTLS(group.value))
watch(groupId, () => { tls.value = defaultRouteTLS(group.value) })
const scanning = computed(() => ['running', 'stopping'].includes(scan.value?.state))
const scanLabel = computed(() => ({
  running: '扫描中', stopping: '正在停止', completed: '已完成', cancelled: '已取消',
  timed_out: '扫描超时', expired: '结果已失效，请重新扫描'
})[scan.value?.state] || scan.value?.state)
const controller = new AbortController()
let timer

function applyScan(value) {
  scan.value = value
  const previous = new Map(rows.value.map((row) => [row.port, row]))
  rows.value = value.services.map((row) => ({
    ...row,
    selected: previous.get(row.port)?.selected ?? false,
    name: previous.get(row.port)?.name ?? (row.name || row.upstream),
    host: previous.get(row.port)?.host ?? '',
    fullHost: previous.get(row.port)?.fullHost ?? false,
    useImage: previous.get(row.port)?.useImage ?? true
  }))
  if (scanning.value) timer = setTimeout(poll, 750)
}

async function poll() {
  try {
    const value = await adminApi.discoveryStatus(scan.value.id, controller.signal)
    if (!controller.signal.aborted) applyScan(value)
  } catch (err) {
    if (!controller.signal.aborted) {
      error.value = err.message
      scan.value = { ...scan.value, state: 'expired' }
    }
  }
}

async function start() {
  clearTimeout(timer)
  pending.value = true
  error.value = ''
  rows.value = []
  try {
    const result = await adminApi.discoverServices({ ip: ip.value.trim(), ports: ports.value.trim() })
    if (controller.signal.aborted) {
      adminApi.cancelDiscovery(result.id).catch(() => {})
      return
    }
    applyScan(result)
  } catch (err) {
    error.value = err.message
  } finally {
    pending.value = false
  }
}

async function stop() {
  pending.value = true
  clearTimeout(timer)
  try {
    const result = await adminApi.cancelDiscovery(scan.value.id)
    if (!controller.signal.aborted) applyScan(result)
  } catch (err) {
    error.value = err.message
  } finally {
    pending.value = false
  }
}

async function createRoutes() {
  if (pending.value || scanning.value || isSavingConfig.value) return
  pending.value = true
  error.value = ''
  try {
    const config = structuredClone(props.snapshot.config)
    if (rows.value.some((row) => row.selected && row.tls_untrusted))
      throw new Error('所选 HTTPS 上游证书不受信任，请配置可信证书或改用 HTTP 端口后重新扫描')
    for (const row of rows.value.filter((row) => row.selected)) {
      let image = ''
      if (row.useImage && row.icon) image = (await adminApi.importRouteImage({ scan_id: scan.value.id, port: row.port })).id
      config.routes.push({
        id: newID(),
        group_id: groupId.value,
        name: row.name.trim(),
        host: routeHost(row.host, group.value, row.fullHost),
        upstream: row.upstream,
        image,
        tls: tls.value,
        enabled: true,
        firewall_id: firewallID.value,
        auth: { enabled: false, username: '', failure_limit: 5, freeze_seconds: 3600 }
      })
    }
    if (controller.signal.aborted) return
    await saveConfig(props.snapshot, config)
    emit('saved')
  } catch (err) {
    error.value = err.message
  } finally {
    pending.value = false
  }
}

onUnmounted(() => {
  clearTimeout(timer)
  controller.abort()
  if (scanning.value) adminApi.cancelDiscovery(scan.value.id).catch(() => {})
})
</script>

<style scoped>
.discovery,
.discovery form,
.scan-fields label {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.discovery label,
.discovery p {
  color: var(--ink-secondary);
  font-size: 13px;
}
.discovery p {
  margin: 0;
  overflow-wrap: anywhere;
}
.scan-result {
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  display: grid;
  gap: 8px;
}
.discovery [role='alert'] {
  color: var(--bad);
}
</style>
