<template>
  <section class="home report-page">
    <Teleport defer to="#home-breadcrumb"><nav class="crumb" aria-label="当前位置">
      <RouterLink to="/overview">GateHome</RouterLink><span>/</span><button @click="chooser = true">远程回家</button>
      <template v-if="managing"><span>/</span><strong>WireGuard</strong></template>
    </nav></Teleport>
    <header class="report-heading">
      <div>
        <div class="title-line"><h1>{{ managing ? 'WireGuard' : '远程回家' }}</h1>
          <span v-if="managing" class="badge" :class="{ good: connectionOK && status.running }">{{ !connectionOK ? '状态不可用' : status.running ? '运行中' : '待检查' }}</span>
        </div>
        <p>{{ managing ? '通过加密隧道访问家庭网络，管理设备和访问范围。' : '从外网安全访问家中的服务和设备。' }}</p>
      </div>
      <div v-if="managing" class="report-actions">
        <button class="btn btn-secondary" @click="chooser = true"><Layers :size="15"/>接入方式</button>
        <button class="btn btn-secondary" @click="openSettings"><Settings :size="15"/>设置</button>
        <button class="btn btn-primary" :disabled="!connectionOK || !status.running" @click="openPeer()"><Plus :size="16"/>添加设备</button>
      </div>
    </header>
    <p v-if="loadError || (status?.settings && !status.available)" role="alert" class="report-error">
      {{ loadError || status.error || '暂时无法连接 WireGuard 宿主机服务' }}
      <button class="btn btn-secondary" @click="refresh">重试</button>
    </p>
    <p v-if="!status && !loadError" class="report-note">正在读取宿主机状态…</p>
    <template v-if="status && !managing">
      <h2>选择接入方式</h2>
      <div class="panel method">
        <span class="symbol"><Network/></span>
        <div><h3>WireGuard <span class="badge">{{ status.settings ? '已配置' : '未配置' }}</span></h3>
          <p>为手机和电脑建立加密隧道，按家庭网段或服务授权。</p></div>
        <button class="btn btn-primary" @click="status.settings ? chooser = false : openSettings()">
          {{ status.settings ? '管理 WireGuard' : '配置 WireGuard' }}
        </button>
      </div>
      <div class="panel diag"><h2>接入前准备</h2>
        <p><Monitor/>宿主机服务 <b>{{ status.available ? '已连接' : '待安装或连接' }}</b></p>
        <p><Globe/>公网入口 <b>可从外网访问的 IPv4 或 IPv6</b></p>
        <p><Network/>家庭网络 <b>选择网段或填写设备地址</b></p>
      </div>
    </template>
    <template v-if="managing">
      <div class="panel summary">
        <div v-for="item in metrics" :key="item.label"><span class="metric-icon"><component :is="item.icon"/></span>
          <div><small>{{ item.label }}</small><strong>{{ item.value }}</strong></div>
        </div>
      </div>
      <section class="peers"><div class="report-heading"><h2>设备</h2>
        <label class="search"><Search :size="16"/><input v-model.trim="search" type="search" aria-label="搜索设备" placeholder="搜索设备名称或隧道地址…"></label>
      </div>
      <div class="panel peer-list">
        <table><thead><tr><th>设备名称</th><th>隧道地址</th><th>最近握手</th><th>接收 / 发送</th><th>操作</th></tr></thead>
          <tbody><tr v-for="p in filtered" :key="p.id">
            <td><button class="device-name" @click="detail(p)"><span class="device-icon"><component :is="deviceIcon(p.name)" :size="17"/></span>{{ p.name }}</button></td>
            <td data-label="隧道地址">{{ p.address }}</td><td data-label="最近握手">{{ handshake(p) }}</td>
            <td data-label="网关接收 / 发送">{{ connectionOK ? bytes(p.rx) + ' / ' + bytes(p.tx) : '—' }}</td>
            <td class="actions"><button :aria-label="'编辑 '+p.name" title="编辑设备" :disabled="!connectionOK" @click="openPeer(p)"><Pencil :size="16"/></button>
              <button class="danger-text" :aria-label="'撤销 '+p.name" title="撤销授权" :disabled="!connectionOK" @click="show('remove', p)"><Trash2 :size="16"/></button></td>
          </tr></tbody>
        </table>
        <p v-if="!filtered.length" class="report-empty">{{ search ? '没有匹配的设备' : '尚未授权设备，添加第一台手机或电脑即可开始连接。' }}</p>
      </div></section>
      <section class="panel diag"><h2>连接诊断</h2>
        <p><Monitor/>宿主机服务 <b :class="{ good: connectionOK }"><CircleCheck v-if="connectionOK" :size="15"/>{{ connectionOK ? '已连接' : '状态不可用' }}</b></p>
        <p><Radio/>UDP 监听 <b>{{ connectionOK ? (status.listen_port || '未监听') : '—' }}</b></p>
        <p v-if="connectionOK && status.dns_ready != null"><Network/>隧道 DNS <b>{{ status.dns_ready ? '正常' : '需要检查' }}</b></p>
        <p v-if="connectionOK && status.fault" class="report-error">{{ status.fault }}</p>
        <p><Globe/>设备握手 <b :class="{good: hasHandshake}">{{ !connectionOK ? '—' : hasHandshake ? '已收到握手' : '等待设备握手' }}</b></p>
        <p v-if="connectionOK && (!status.forwarding || !hasHandshake)" class="report-note">{{ !status.forwarding ? '请开启宿主机 IPv4 转发。' : '手机关闭 Wi-Fi，使用移动网络开启隧道并打开一个家庭服务，即可验证外网访问。' }}</p>
      </section>
    </template>
    <dialog ref="dialog" class="modal-dialog" aria-labelledby="home-dialog-title" @close="clearDialog" @cancel="busy && $event.preventDefault()">
      <div class="modal-header"><h2 id="home-dialog-title">{{ dialogTitle }}</h2>
        <button class="close-modal-btn" aria-label="关闭" :disabled="busy" @click="dialog.close()"><X :size="18"/></button></div>
      <form class="modal-body" @submit.prevent="save">
        <p v-if="error" class="report-error" role="alert">{{ error }}</p>
        <template v-if="screen === 'settings'">
          <template v-if="!connectionOK || status.missing?.length || !status.forwarding">
            <strong>准备 Linux 宿主机</strong>
            <p class="report-note">{{ status?.error || loadError || (status.missing?.length ? '缺少工具：'+status.missing.join('、') : '需要开启 IPv4 转发。') }}</p>
            <details open><summary>部署步骤</summary><p class="report-note">在宿主机安装 Python 3、wireguard-tools、iproute2 和 nftables。一键安装的文件位于 /etc/gatehome/wireguard。按部署说明启用 gatehome-wireguard.service，开启 IPv4 转发。Docker 部署还需挂载控制套接字目录并添加对应用户组。</p><a :href="deploymentGuide" download="GateHome-WireGuard.md">下载完整部署说明</a></details>
            <button class="btn btn-secondary" type="button" @click="refresh">重新检测</button>
          </template>
          <label>公网域名或 IP<input v-model.trim="form.endpoint" list="home-domains" placeholder="vpn.example.net" required maxlength="253"></label>
          <datalist id="home-domains"><option v-for="host in status?.ddns" :key="host" :value="host"/></datalist>
          <div class="pair"><label>UDP 端口<input v-model.number="form.port" type="number" min="1" max="65535" required></label>
          <label>隧道网段<input v-model.trim="form.tunnel" :readonly="devices.length > 0" required placeholder="10.77.0.0/24"></label></div>
          <label>家庭局域网段<input v-model.trim="form.lan" :readonly="devices.length > 0" list="home-lans" required placeholder="192.168.2.0/24"></label>
          <datalist id="home-lans"><option v-for="net in status?.lans" :key="net" :value="net"/></datalist>
          <p class="report-note">可选择检测到的网段，也可手动填写。公网 IPv4 需要路由器映射 UDP 端口，IPv6 需要放行入站 UDP。</p>
          <label class="check"><input v-model="form.snat" type="checkbox">对家庭网络启用地址转换（SNAT）</label>
          <p class="report-note">{{ form.snat ? '家中设备可直接回复给网关，通常无需修改路由器。' : '请在家庭路由器添加返回隧道网段的静态路由。' }}</p>
          <p v-if="devices.length" class="report-note">已有设备时网段锁定。修改公网入口或端口后，请同步更新客户端 Endpoint。</p>
        </template>
        <template v-else-if="screen === 'peer'">
          <label>设备名称<input v-model.trim="form.name" maxlength="80" required autofocus placeholder="例如，我的手机"></label>
          <label>访问范围<select v-model="form.mode"><option value="lan">整个家庭网段</option><option value="host">指定内网设备或端口</option><option v-if="status.routes?.length || form.mode === 'route'" value="route">指定反代服务</option></select></label>
          <p v-if="form.mode === 'lan'" class="report-note">此设备可访问 {{ status.settings.lan }} 内的主机。其他网络流量继续使用当前网络。</p>
          <template v-if="form.mode === 'host'"><label>家庭设备 IP<input v-model.trim="form.target" placeholder="192.168.2.60" required></label>
            <label>允许的端口（留空允许所有端口）<input v-model.number="form.port" type="number" min="1" max="65535" placeholder="7777"></label></template>
          <template v-if="form.mode === 'route'"><label>反代服务<select v-model="form.route" required><option disabled value="">选择服务</option><option v-for="r in status.routes" :key="r.id" :value="r.id">{{ r.name }} · {{ r.host }}</option></select></label><p class="report-note">客户端使用隧道 DNS 解析所选域名。宿主机需安装 dnsmasq。</p></template>
          <label v-else class="check"><input v-model="form.admin" type="checkbox">允许范围内的 GateHome 管理端口</label>
          <label v-if="!form.id">已有客户端公钥（可选）<input v-model.trim="form.public_key" autocomplete="off" placeholder="留空生成一次性导入配置"></label>
          <p v-if="form.id" class="report-note">更改访问范围会立即更新宿主机规则。客户端请按设备详情同步 AllowedIPs 与 DNS。</p>
        </template>
        <template v-else-if="screen === 'profile'">
          <p>用手机 WireGuard 扫码，或下载配置到这台设备。</p>
          <img v-if="qr" :src="qr" class="qr" alt="此设备的 WireGuard 配置二维码">
          <p class="report-note">配置包含设备私钥，仅在本窗口提供。关闭后如需重新签发，请先撤销设备。</p>
          <button type="button" class="btn btn-primary" @click="download"><Download :size="16"/>下载 .conf</button>
        </template>
        <template v-else-if="screen === 'detail' && selected">
          <div class="detail-identity"><span class="metric-icon"><component :is="deviceIcon(selected.name)"/></span><span class="badge good">已授权设备</span></div>
          <dl class="facts"><template v-for="[k,v] in facts" :key="k"><dt>{{ k }}</dt><dd>{{ v }}</dd></template></dl>
          <button type="button" class="btn btn-secondary" @click="openPeer(selected)"><Pencil :size="15"/>编辑设备</button>
        </template>
        <p v-else-if="screen === 'remove'">撤销「{{ selected?.name }}」后，现有隧道将失效。重新接入需要再次授权。</p>
        <div v-if="['settings','peer','remove'].includes(screen)" class="modal-footer">
          <button type="button" class="btn btn-secondary" :disabled="busy" @click="dialog.close()">取消</button>
          <button class="btn" :class="screen === 'remove' ? 'btn-danger' : 'btn-primary'" :disabled="busy || !connectionOK || (screen === 'settings' && (status.missing?.length || !status.forwarding))">
            {{ busy ? '正在应用…' : screen === 'remove' ? '撤销授权' : '保存并应用' }}</button>
        </div>
      </form>
    </dialog>
  </section>
</template>

<script setup>
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import deploymentGuide from '../../../docs/wireguard.md?url'
import { CircleCheck, Download, Globe, Laptop, Layers, Monitor, Network, Pencil, Plus, Radio, Search, Settings, Smartphone, Tablet, Trash2, Users, X } from 'lucide-vue-next'
import { request } from '@/api/client'
import { startPolling } from '@/api/polling'
import { formatBytes } from '@/api/reports'
import '@/styles/reports.css'

const status = ref(null), loadError = ref(''), search = ref(''), chooser = ref(false)
const dialog = ref(null), screen = ref(''), form = ref({}), selected = ref(null)
const error = ref(''), busy = ref(false), profile = ref(''), qr = ref('')
const controller = new AbortController()
let stop, version = 0
const devices = computed(() => status.value?.devices || [])
const managing = computed(() => status.value?.settings && !chooser.value)
const filtered = computed(() => devices.value.filter(p => (p.name + ' ' + p.address).toLowerCase().includes(search.value.toLowerCase())))
const connectionOK = computed(() => Boolean(status.value?.available && !loadError.value))
const hasHandshake = computed(() => connectionOK.value && devices.value.some(p => p.handshake > 0))
const endpoint = computed(() => {
  const s = status.value?.settings
  return s ? (s.endpoint.includes(':') ? '['+s.endpoint+']' : s.endpoint) + ':' + s.port : ''
})
const metrics = computed(() => [
  { label:'公网入口', value:endpoint.value, icon:Globe }, { label:'隧道网段', value:status.value?.settings?.tunnel, icon:Layers },
  { label:'已授权设备', value:devices.value.length, icon:Users }
])
const dialogTitle = computed(() => ({settings:'WireGuard 设置', peer:form.value.id?'编辑设备':'添加设备', profile:'导入设备配置', remove:'撤销设备授权', detail:selected.value?.name})[screen.value])
const bytes = n => n == null ? '—' : formatBytes(n)
const deviceIcon = name => /book|电脑|laptop|桌面/i.test(name) ? Laptop : /pad|平板/i.test(name) ? Tablet : Smartphone
function handshake(p) {
  if (!connectionOK.value) return '—'
  if (!p.handshake) return status.value?.running ? '尚未握手' : '—'
  const sec = Math.max(0, Math.floor(Date.now()/1000) - p.handshake)
  if (sec < 60) return sec + ' 秒前'
  if (sec < 3600) return Math.floor(sec/60) + ' 分钟前'
  return new Date(p.handshake*1000).toLocaleString()
}
const facts = computed(() => {
  const s = status.value, p = devices.value.find(peer => peer.id === selected.value?.id)
  if (!s?.settings || !p) return []
  const isRoute = p.mode === 'route'
  return [['隧道地址',p.address+'/32'],['最近握手',handshake(p)],['网关接收 / 发送',connectionOK.value ? bytes(p.rx)+' / '+bytes(p.tx) : '—'],
    ['访问范围',p.mode === 'lan' ? s.settings.lan : isRoute ? p.host+':'+p.port : p.target+(p.port ? ':'+p.port : '')],
    ['管理端口',p.admin ? '范围内允许' : '未授权'],['Endpoint',endpoint.value],
    ['AllowedIPs',p.mode === 'lan' ? s.settings.lan : (isRoute ? '10.254.254.254' : p.target)+'/32'],
    ['DNS',isRoute ? '10.254.254.254' : '沿用设备网络'],['服务器公钥',s.server_public_key]]
})
async function refresh() {
  const seq = ++version
  try {
    const data = await request('/api/remote-home', {signal:controller.signal})
    if (seq !== version) return
    loadError.value = ''
    if (!data.available && status.value?.settings) {
      status.value = { ...status.value, available: false, error: data.error }
      return
    }
    status.value = data
  } catch (e) { if (e.name !== 'AbortError' && seq === version) loadError.value = e.message }
}
onMounted(() => { refresh(); stop = startPolling(refresh, 10000) })
onBeforeUnmount(() => { stop?.(); controller.abort(); clearDialog() })
function show(view, peer=null) {
  screen.value = view; selected.value = peer; error.value = ''
  if (!dialog.value.open) dialog.value.showModal()
}
function detail(peer) { show('detail',peer) }
function openSettings() {
  const s = status.value
  form.value = {...(s.settings || {endpoint:s.ddns?.[0] || '',port:51820,lan:s.lans?.find(n => n.startsWith('192.168.')) || s.lans?.[0] || '192.168.2.0/24',tunnel:'10.77.0.0/24',snat:true})}
  delete form.value.admin_port
  show('settings')
}
function openPeer(peer) {
  form.value = peer ? {id:peer.id,name:peer.name,mode:peer.mode,target:peer.target,port:peer.port || '',admin:peer.admin,route:peer.route} :
    {name:'',mode:'lan',target:'',port:'',admin:false,route:'',public_key:''}
  show('peer',peer)
}
function clearDialog() { profile.value = ''; qr.value = ''; screen.value = ''; selected.value = null; form.value = {}; error.value = '' }
async function save() {
  if (busy.value || !['settings','peer','remove'].includes(screen.value)) return
  busy.value = true; error.value = ''
  try {
    const op = screen.value === 'peer' ? (form.value.id ? 'edit' : 'add') : screen.value
    const body = op === 'remove' ? {id:selected.value.id} : {...form.value}
    if (op === 'edit' || op === 'add') body.port = body.mode === 'host' ? (Number(body.port) || 0) : 0
    const data = await request('/api/remote-home/'+op,{method:'POST',body,signal:controller.signal})
    chooser.value = false
    if (data.profile) {
      profile.value = data.profile; show('profile')
      try { const {default: QRCode} = await import('qrcode'); qr.value = await QRCode.toDataURL(data.profile,{width:320,margin:4}) }
      catch { error.value = '二维码生成失败，可以下载配置文件。' }
    } else if (op === 'add' || op === 'edit') {
      await refresh()
      const peer = devices.value.find(p => p.id === (data.id || body.id))
      if (peer) detail(peer)
      else dialog.value.close()
    } else dialog.value.close()
    await refresh()
  } catch (e) { if (e.name !== 'AbortError') error.value = e.message }
  finally { busy.value = false }
}
function download() {
  const link = document.createElement('a')
  const url = URL.createObjectURL(new Blob([profile.value],{type:'text/plain'}))
  link.href = url; link.download = 'gatehome.conf'; link.click()
  setTimeout(() => URL.revokeObjectURL(url),1000)
}
</script>

<style scoped>
.home { gap:24px; }
.crumb,.detail-identity,.diag b { display:flex; align-items:center; gap:8px; }
.crumb { color:var(--muted); font-size:13px; white-space:nowrap; }
.crumb button,.crumb a { color:inherit; }
.crumb strong { color:var(--ink); }
.detail-identity { padding-bottom:12px; border-bottom:1px solid var(--line); }
.home h1 { font-size:25px; }
.home h2 { font-size:17px; }
.home h3 { font-size:16px; }
.title-line,.method,.method h3,.search,.device-name,.actions,.diag p,.check { display:flex; align-items:center; gap:12px; }
.panel { background:var(--card-bg); border:1px solid var(--card-border); border-radius:var(--radius-md); min-width:0; }
.badge { font-size:11px; padding:4px 10px; border-radius:20px; border:1px solid var(--line); color:var(--muted); }
.good { color:var(--ok); }
.method,.diag { padding:22px; }
.method p,.home small { color:var(--muted); font-size:13px; }
.method > button { margin-left:auto; }
.symbol,.metric-icon,.device-icon { display:grid; place-items:center; flex:none; border-radius:12px; color:var(--brand-light); background:var(--strip-bg); width:44px; height:44px; }
.summary { display:grid; grid-template-columns:1.4fr 1fr 1fr; padding:20px 0; }
.summary > div { display:flex; align-items:center; gap:14px; padding:0 22px; min-width:0; }
.summary > div + div { border-left:1px solid var(--line); }
.summary strong { display:block; overflow-wrap:anywhere; margin-top:4px; font-size:17px; }
.metric-icon { border-radius:50%; background:color-mix(in srgb,var(--brand) 12%,var(--strip-bg)); }
.peers .report-heading { margin-bottom:12px; }
.search { border:1px solid var(--line); border-radius:var(--radius-sm); padding:0 12px; background:var(--input-bg); color:var(--muted); gap:8px; }
.search input { width:200px; max-width:100%; background:transparent; border:0; min-height:36px; color:var(--ink); }
.peer-list { overflow:hidden; }
table { border-collapse:collapse; width:100%; text-align:left; }
th,td { padding:13px 18px; font-size:13px; overflow-wrap:anywhere; }
th { font-size:12px; color:var(--muted); background:var(--strip-bg); }
td { border-top:1px solid var(--line); }
.device-name { max-width:100%; overflow-wrap:anywhere; color:var(--ink); font-weight:600; text-align:left; }
.device-icon { width:33px; height:33px; border:1px solid var(--line); border-radius:9px; }
.actions button { color:var(--ink-secondary); display:inline-grid; place-items:center; width:32px; height:32px; border-radius:6px; }
td.actions { display:table-cell; white-space:nowrap; }
.actions button:hover { background:var(--strip-bg); }
.actions .danger-text { color:var(--bad); }
.diag h2 { margin-bottom:12px; }
.diag p { border-top:1px solid var(--line); padding:12px 0; font-size:13px; }
.diag p:last-child { padding-bottom:0; }
.diag svg { width:18px; height:18px; color:var(--muted); flex:none; }
.diag b { margin-left:auto; text-align:right; font-weight:500; }
.modal-body label { display:grid; gap:6px; font-size:13px; }
.modal-body .check { display:flex; align-items:center; }
.check input { width:16px; height:16px; flex:none; }
.pair,.facts { display:grid; grid-template-columns:1fr 1fr; gap:12px; }
.facts { grid-template-columns:auto 1fr; font-size:13px; }
.facts dt { color:var(--muted); }
.facts dd { overflow-wrap:anywhere; margin:0; }
.qr { width:min(260px,100%); height:auto; align-self:center; margin:auto; }
@media(max-width:760px) {
  .home { gap:16px; }
  .summary { grid-template-columns:1fr; padding:2px 16px; }
  .summary > div { padding:10px 0; }
  .summary > div + div { border-left:0; border-top:1px solid var(--line); }
  .summary > div > div { display:flex; align-items:center; justify-content:space-between; gap:10px; flex:1; }
  .summary strong { font-size:14px; margin:0; text-align:right; }
  .metric-icon { width:32px; height:32px; }
  .metric-icon svg { width:17px; }
  .method { flex-wrap:wrap; }
  .method > div { flex:1; }
  .method > button,.search { width:100%; }
  .search input { width:100%; }
  table,tbody,tr,td { display:block; }
  thead { display:none; }
  tr { padding:12px 16px; }
  tr + tr { border-top:1px solid var(--line); }
  td { border:0; padding:4px 0; }
  td[data-label] { display:flex; justify-content:space-between; gap:10px; }
  td[data-label]::before { content:attr(data-label); color:var(--muted); }
  td.actions { display:flex; justify-content:flex-end; }
  .actions button { width:44px; height:44px; }
  .pair { grid-template-columns:1fr; }
}
</style>
