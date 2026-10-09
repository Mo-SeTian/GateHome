'use strict';
const $ = (selector, scope = document) => scope.querySelector(selector);
const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const clone = value => structuredClone(value);
function newID() {
  const bytes=new Uint8Array(16);crypto.getRandomValues(bytes);bytes[6]=(bytes[6]&15)|64;bytes[8]=(bytes[8]&63)|128;
  const hex=[...bytes].map(b=>b.toString(16).padStart(2,'0')).join('');return hex.slice(0,8)+'-'+hex.slice(8,12)+'-'+hex.slice(12,16)+'-'+hex.slice(16,20)+'-'+hex.slice(20);
}
const pages = {overview:'概览',routes:'反向代理',ddns:'动态域名',certificates:'SSL 证书',access:'防火墙管理',subscriptions:'订阅管理',settings:'设置',logs:'日志',statistics:'统计','firewall-logs':'防火墙拦截记录','ip-blocks':'IP 拦截名单'};
const iconNames = new Set(['overview','routes','globe','certificate','shield','download','logs','chart','settings','server','gateway','router','layers','activity','refresh','plus','close','logout','menu','more','back','edit','chevron-down','chevron-up','chevron-left','chevron-right','external-link','trash','play','pause','login','filter','unlock','save','upload']);
function icon(name) { return `<svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><use href="/icons.svg#${iconNames.has(name)?name:'layers'}"></use></svg>`; }
function hydrateIcons(scope=document) {
  scope.querySelectorAll('[data-icon]').forEach(el=>{el.innerHTML=icon(el.dataset.icon);});
  scope.querySelectorAll('.field-help>summary,.ip-monitor-settings>summary').forEach(el=>{if(!el.querySelector('svg')) el.insertAdjacentHTML('beforeend',icon('chevron-down'));});
  const actions={'add-ip-block':'shield','remove-firewall-group':'trash','move-firewall-group-up':'chevron-up','move-firewall-group-down':'chevron-down','configure-certificates':'settings','filter-ip-blocks':'filter','apply-log-filters':'filter','reset-log-filters':'refresh','release-ip-block':'unlock','export-statistics':'download','preset-subscription':'download','restart-service':'refresh','run-ddns':'refresh','go-firewalls':'shield','apply-maintenance':'upload','check-online-update':'refresh','install-online-update':'download'};
  scope.querySelectorAll('button[data-action],button[data-job],button[type="submit"]').forEach(el=>{
    if(el.querySelector('svg')) return;
    const action=el.dataset.action||'',name=actions[action]||(action.startsWith('refresh-')||el.dataset.job?'refresh':action.startsWith('add-')?'plus':action.startsWith('edit-')?'edit':action.startsWith('delete-')?'trash':action.startsWith('toggle-')?(el.textContent.trim().startsWith('启用')?'play':'pause'):el.type==='submit'?({'backup-form':'download','restore-form':'upload','update-form':'upload','ip-block-form':'shield'}[el.form?.id]||'save'):'');
    if(name) el.insertAdjacentHTML('afterbegin',icon(name));
  });
}
const reducedMotion=window.matchMedia('(prefers-reduced-motion: reduce)');
const mobileNavigation=window.matchMedia('(max-width: 900px)');
const compactCharts=window.matchMedia('(max-width: 680px)');
const collapsedGroups=new Set();
let renderedPage='',pageMotion;
const dialogMotions=new WeakMap(),dialogOpeners=new WeakMap();
function focusSelector(el) {
  if(!el) return '';
  if(el.id) return '#'+CSS.escape(el.id);
  if(el.dataset.action||el.dataset.page) return 'button'+['action','page','index','group','panel','kind','pageNumber'].filter(k=>el.dataset[k]!==undefined).map(k=>`[data-${k.replace(/[A-Z]/g,c=>'-'+c.toLowerCase())}="${CSS.escape(el.dataset[k])}"]`).join('');
  return '';
}
function openDialog(dialog) {
  const from={opacity:getComputedStyle(dialog).opacity,transform:getComputedStyle(dialog).transform};
  dialogMotions.get(dialog)?.cancel();
  if(!dialog.open) {dialogOpeners.set(dialog,focusSelector(document.activeElement));dialog.showModal();from.opacity=0;from.transform='translateY(12px) scale(.98)';}
  if(!reducedMotion.matches) dialogMotions.set(dialog,dialog.animate([from,{opacity:1,transform:'none'}],{duration:240,easing:'cubic-bezier(.2,.8,.2,1)'}));
}
function closeDialog(dialog) {
  if(!dialog?.open) return;
  if(dialog.id==='route-dialog') {++routeImageRequest;routeImagePending=false;}
  if(dialog.id==='discovery-dialog') abandonDiscovery();
  const from={opacity:getComputedStyle(dialog).opacity,transform:getComputedStyle(dialog).transform};
  dialogMotions.get(dialog)?.cancel();
  if(reducedMotion.matches) {dialog.close();return;}
  const motion=dialog.animate([from,{opacity:0,transform:'translateY(8px) scale(.99)'}],{duration:140,easing:'cubic-bezier(.4,0,1,1)',fill:'forwards'});
  dialogMotions.set(dialog,motion);
  motion.finished.then(()=>{if(dialogMotions.get(dialog)===motion) {dialog.close();motion.cancel();dialogMotions.delete(dialog);}}).catch(()=>{});
}
function setNavigation(open,restoreFocus=true) {
  open=open&&mobileNavigation.matches;
  document.body.classList.toggle('nav-open',open);
  $('#nav-toggle').setAttribute('aria-expanded',String(open));
  $('#nav-scrim').hidden=!open;
  $('#main').inert=open;
  $('#sidebar').inert=mobileNavigation.matches&&!open;
  if(open) requestAnimationFrame(()=>{if(document.body.classList.contains('nav-open')) $('#sidebar nav .active')?.focus();});
  else if(restoreFocus&&mobileNavigation.matches) $('#nav-toggle').focus();
}
let ipBlockView={entries:[]},ipBlockRule='',ipBlockSearch='',ipBlockPage=1,ipBlockRequest=0,ipBlockError='',ipBlockReceived=0;
let maintenancePreview=null;
const onlineUpdate={release:null,phase:'',error:'',job:null,timer:null,request:0};
let logRequest=0;
const discovery={request:0,timer:null,scan:null,rows:new Map(),pending:false};
let routeImageRequest=0,routeImagePending=false;
const securityEvents=Object.fromEntries(['firewall','security'].map(kind=>[kind,{filters:emptyEventFilters(),applied:emptyEventFilters(),view:null,size:20,request:0,loading:false,error:''}]));
let logPage=1,logSize=50;
let statisticsView=null,statisticsHours='24',statisticsRule='',statisticsRequest=0,statisticsLoading=false,statisticsError='';
let logView={entries:[],pages:1,total:0,through:0}, logCategory='',logResult='',logScope='project',logRule='',logSearch='',logIP='',logFrom='',logTo='',logMethod='',logStatus='',proxyPasswordConfigured=false;
let dnsCredentialsConfigured={};
let certificateCredentialsConfigured={};
let routePasswordsConfigured={};
let dnsRecords={},dnsRecordsRequest=0,dnsRecordsError='';
let networkInfo=null,ipGroup='',ipInterface='',ipIPv4Source='url',ipIPv6Source='url',ipRequest=0,ipViewKey='';
let config, revision, status = {}, page = 'overview', toastTimer, busy = false;

async function api(path, method = 'GET', body) {
  const multipart=typeof FormData!=='undefined'&&body instanceof FormData;
  const response = await fetch('/api/' + path, {method, credentials:'same-origin', headers:multipart?{'X-Gatehouse-Request':'1'}:{'Content-Type':'application/json','X-Gatehouse-Request':'1'}, body:multipart?body:body === undefined ? undefined : JSON.stringify(body)});
  const data = await response.json();
  if (!response.ok) {
    if (response.status === 401 && path !== 'login') showLogin();
    throw new Error(data.error || '请求失败');
  }
  return data;
}
function toast(message) {
  $('#toast').textContent = message; $('#toast').hidden = false; if(!reducedMotion.matches) {$('#toast').getAnimations().forEach(a=>a.cancel());$('#toast').animate([{opacity:0,translate:'0 8px'},{opacity:1,translate:'0 0'}],{duration:180,easing:'ease-out'});}
  clearTimeout(toastTimer); toastTimer = setTimeout(() => { $('#toast').hidden = true; }, 4500);
}
function showLogin() { resetDashboard();setNavigation(false,false); $('#login').hidden = false; $('#app').hidden = true; document.querySelectorAll('dialog').forEach(d=>d.close()); config = undefined; renderedPage='';clearTimeout(onlineUpdate.timer);onlineUpdate.request++;onlineUpdate.phase='';onlineUpdate.job=null;onlineUpdate.release=null;onlineUpdate.error='';abandonDiscovery();for(const state of Object.values(securityEvents)) {state.request++;state.view=null;state.filters=emptyEventFilters();state.applied=emptyEventFilters();state.loading=false;state.error='';} }
function applyConfig(data) { config = data.config; revision = data.revision; dnsCredentialsConfigured=data.dns_credentials_configured||{}; certificateCredentialsConfigured=data.certificate_credentials_configured||{};routePasswordsConfigured=data.route_passwords_configured||{}; proxyPasswordConfigured=data.proxy_password_configured; ipRequest++;dnsRecordsRequest++;dnsRecords={};dnsRecordsError=''; if(networkInfo) networkInfo.groups={}; }
async function save(next, dnsTokens, proxyPassword, certificateTokens, routePasswords) {
  const payload = {config:next, revision};
  if (dnsTokens !== undefined) payload.dns_tokens = dnsTokens;
  if (proxyPassword !== undefined) payload.proxy_password=proxyPassword;
  if (certificateTokens !== undefined) payload.certificate_tokens=certificateTokens;
  if (routePasswords !== undefined) payload.route_passwords=routePasswords;
  applyConfig(await api('config', 'PUT', payload));
  await refreshStatus();
  toast('配置已保存' + (status.restart_required ? '，监听端口重启后生效' : ''));
}
function badge(text, kind = '') { return `<span class="badge ${kind}">${esc(text)}</span>`; }
function empty(title, message, action = '') { return `<div class="empty"><div class="empty-icon">${icon('layers')}</div><h3>${esc(title)}</h3><p>${esc(message)}</p>${action}</div>`; }
function heading(kicker, title, description, actions = '') { return `<div class="page-heading"><div><span class="eyebrow">${kicker}</span><h1 tabindex="-1">${title}</h1><p>${description}</p></div><div class="heading-actions">${actions}</div></div>`; }
function discoveryButton(groupID='') {return `<button class="secondary" data-action="discover-services" data-group="${esc(groupID)}" ${!config.groups.length?'disabled':''}>${icon('activity')}发现内网服务</button>`;}
function addRouteButton(groupID = '') { return `<button class="primary" data-action="add-route" data-group="${esc(groupID)}"><span data-icon="plus"></span>添加代理服务</button>`; }
function panel(title, description, body, action = '') { return `<section class="panel"><div class="panel-heading"><div><h2>${title}</h2>${description ? `<p>${description}</p>` : ''}</div>${action}</div>${body}</section>`; }
function groupPanel(kind, id, title, description, body, actions, state) {
  const key=kind+':'+id,bodyID='group-panel-'+kind+'-'+id,collapsed=collapsedGroups.has(key);
  return `<section class="panel group-panel ${collapsed?'is-collapsed':''}"><div class="panel-heading"><div class="group-heading"><h2 class="title-status"><span class="group-kind-icon">${icon({proxy:'router',ddns:'globe',firewall:'shield',certificate:'certificate'}[kind])}</span><button class="group-disclosure" data-action="toggle-group-panel" data-panel="${esc(key)}" aria-expanded="${!collapsed}" aria-controls="${esc(bodyID)}" aria-label="${collapsed?'展开':'收起'}${esc(title)}"><span>${esc(title)}</span>${icon('chevron-down')}</button>${state}</h2><p>${description}</p></div><div class="group-actions">${actions}</div></div><div id="${esc(bodyID)}" class="group-panel-body" ${collapsed?'hidden':''}>${body}</div></section>`;
}
function toggleGroupPanel(button) {
  const key=button.dataset.panel,body=document.getElementById(button.getAttribute('aria-controls')),collapsed=button.getAttribute('aria-expanded')==='true';
  if(collapsed) collapsedGroups.add(key);else collapsedGroups.delete(key);
  button.setAttribute('aria-expanded',String(!collapsed));button.setAttribute('aria-label',(collapsed?'展开':'收起')+button.querySelector('span').textContent);
  button.closest('.group-panel').classList.toggle('is-collapsed',collapsed);
  body.getAnimations().forEach(motion=>motion.cancel());body.hidden=collapsed;
  if(!collapsed&&!reducedMotion.matches) body.animate([{opacity:0,transform:'translateY(-4px)'},{opacity:1,transform:'none'}],{duration:160,easing:'ease-out'});
}
function actionLabel(action) { return action==='allow'?'允许访问':'禁止访问'; }
function firewallName(id) { return config.firewalls.find(f=>f.id===id)?.name || '无防火墙'; }
function firewallSubscriptions(f) { return [...new Set((f.groups||[]).flatMap(g=>g.subscriptions||[]))]; }
function firewallWaiting(f) { return firewallSubscriptions(f).some(id=>!subscriptionStatus(id).ready); }
function subscriptionChoices(refs = []) {
  return config.subscriptions.length?config.subscriptions.map(d=>{const v=subscriptionStatus(d.id);return `<label class="check-label"><input type="checkbox" name="subscription" value="${esc(d.id)}" ${!d.enabled?'disabled':''} ${refs.includes(d.id)?'checked':''}><span>${esc(d.name)}<small>${!d.enabled?'已停用':v.ready?v.count.toLocaleString()+' 条 IP / CIDR':'等待首次成功更新'}</small></span></label>`;}).join(''):'<p class="form-note">暂无订阅，可先填写手动 IP，或在订阅管理中添加来源。</p>';
}
function date(value) { return !value || value.startsWith('0001') ? '尚未运行' : new Date(value).toLocaleString('sv-SE', {hour12:false}); }
function jobBadge(kind, enabled) {
  if (!enabled) return badge('未启用','gray');
  const job = status.jobs?.[kind];
  if (!job || !job.last_run || job.last_run.startsWith('0001')) return badge('等待检查','amber');
  if (job.running) return badge('运行中');
  return badge(job.last_success && job.last_success >= job.last_run ? '运行正常' : '需要关注', job.last_success && job.last_success >= job.last_run ? '' : 'amber');
}
function eventsHTML() {
  const events = status.events || [];
  if (!events.length) return empty('一切从这里开始', '启用 DDNS 或证书后，运行记录会显示在这里。');
  return `<div class="event-list">${events.slice(0,12).map(e => `<div class="event"><span class="event-dot ${e.ok?'':'bad'}"></span><div><p>${esc(e.message)}</p><small>${e.kind.startsWith('ddns') ? '动态域名' : 'SSL 证书'} · ${date(e.time)}</small></div></div>`).join('')}</div>`;
}
function groupName(id) { return config.groups.find(g=>g.id===id)?.name || '未分组'; }
function portsText(g) { return [g.http_port ? 'HTTP '+g.http_port : '',g.https_port ? 'HTTPS '+g.https_port : ''].filter(Boolean).join(' · ') || '没有监听端口'; }
function routeActive(r) { return r.enabled && config.groups.some(g=>g.id===r.group_id && g.enabled); }
function subscriptionStatus(id) { return (status.subscriptions || []).find(s=>s.id===id) || {ready:false,count:0,message:'等待首次更新'}; }
function objectMenu(label, buttons) {
  return `<details class="object-menu"><summary class="icon-button" aria-label="${esc(label)}操作">${icon('more')}</summary><div class="object-menu-items">${buttons}</div></details>`;
}
function routeTable(accessOnly = false, overview = false, groupID = '') {
  const rows=config.routes.map((r,i)=>({r,i})).filter(({r})=>!groupID||r.group_id===groupID).slice(0,overview?4:100);
  if (!rows.length) return empty('还没有代理服务', '添加一个子域名，将它连接到你的 NAS、相册或其他内网服务。', addRouteButton(groupID));
  const workspace=page==='routes',headers=accessOnly?['服务 / 域名','防火墙','策略状态','运行状态','操作']:['服务 / 域名','服务地址','协议','防火墙','访问账号','运行状态','操作'];
  return `<div class="table-wrap ${workspace?'proxy-table':'service-table'}"><table><thead><tr>${headers.map(h=>`<th>${h}</th>`).join('')}</tr></thead><tbody>${rows.map(({r,i}) => {
    const f=config.firewalls.find(f=>f.id===r.firewall_id),waiting=f&&firewallWaiting(f),key=r.group_id+'/'+r.host;
    const edit=`<button class="text-button" data-action="edit-route" data-index="${i}">编辑</button>`;
    const identity=`<div class="service-cell"><span class="service-icon">${icon('server')}${routeImageHTML(r.image)}</span><div><b>${esc(r.name||r.host)}</b><small class="mono">${esc(r.host)}</small>${groupID?'':`<small>${esc(groupName(r.group_id))}</small>`}</div></div>`;
    const status=badge(routeActive(r)?'已启用':r.enabled?'组已停用':'已停用',routeActive(r)?'':'gray');
    const controls=`<div class="table-actions">${edit}${overview?'':objectMenu(r.name||r.host,`<button class="text-button" data-action="toggle-route" data-index="${i}">${r.enabled?'停用':'启用'}</button><button class="text-button danger" data-action="delete-route" data-index="${i}">删除服务</button>`)}</div>`;
    return `<tr data-route-key="${esc(key)}"><td>${identity}</td>${accessOnly?`<td>${esc(f?.name||'未配置')}</td><td>${f?badge(waiting?'等待订阅，暂拒访问':'规则可用',waiting?'amber':'')+`<small>未命中${actionLabel(f.default_action)}</small>`:'不限制来源 IP'}</td>`:`<td class="service-address">${serviceAddressHTML(r)}</td><td><span class="protocol">${r.tls?'HTTPS':'HTTP'}</span></td><td>${esc(f?.name||'未配置')}</td><td>${r.auth?.enabled?'独立账号':'公开访问'}</td>`}<td>${status}</td><td>${controls}</td></tr>`;
  }).join('')}</tbody></table></div>`;
}
function firewallsHTML() {
  const action='<button class="primary" data-action="add-firewall"><span data-icon="plus"></span>添加防火墙</button>';
  const list=config.firewalls.map((f,i)=>{
    const used=config.routes.filter(r=>r.firewall_id===f.id).length, waiting=firewallWaiting(f);
    const groups=(f.groups||[]).map((g,j)=>{
      const sources=(g.subscriptions||[]).map(id=>config.subscriptions.find(d=>d.id===id)?.name||id).join('、');
      return `<tr><td>${j+1}</td><td><b>${esc(g.name)}</b></td><td>${badge(g.match==='exclude'?'排除（组外 IP）':'包含（组内 IP）','gray')}</td><td>${badge(actionLabel(g.action),g.action==='deny'?'red':'')}</td><td><span class="mono">${esc((g.cidrs||[]).slice(0,2).join(', '))||'无手动 IP'}${(g.cidrs||[]).length>2?' …':''}</span><small>${sources?'订阅：'+esc(sources):'无订阅'}</small></td></tr>`;
    }).join('');
    const actions=`<button class="secondary" data-action="edit-firewall" data-index="${i}">编辑防火墙</button>`+objectMenu(f.name,`<button class="text-button danger" data-action="delete-firewall" data-index="${i}">删除防火墙</button>`);
    return groupPanel('firewall',f.id,f.name,`${(f.groups||[]).length} 个 IP 组 · ${used} 个服务共用 · 未命中时${actionLabel(f.default_action)}`,protectionSummary(f)+(groups?`<div class="table-wrap"><table><thead><tr><th>顺序</th><th>IP 组</th><th>匹配条件</th><th>命中动作</th><th>IP / 订阅来源</th></tr></thead><tbody>${groups}</tbody></table></div>`:'<div class="panel-body form-note">暂无 IP 组，来源 IP 执行默认动作。</div>'),actions,badge(waiting?'等待有效订阅，暂拒访问':'规则可用',waiting?'amber':''));
  }).join('');
  return heading('FIREWALL POLICIES','防火墙管理','统一管理 IP、Web 攻击、爬虫与访问限速，供不同代理服务共用。',action) +
    '<div class="hint">IP 规则第一条命中生效，允许后继续检查 Web 防护。“包含”匹配组内 IP，“排除”匹配组外 IP；未命中执行默认动作。此处控制代理访问权限，按真实连接 IP 判断。来源 IP 被中间设备改写时，看到的是设备地址。</div>' +
    (list||panel('防火墙','创建后可在代理服务中选择。',empty('还没有防火墙','添加多个 IP 组，分别配置允许或禁止及包含或排除条件。',action))) +
    panel('代理选用的防火墙','编辑代理服务，可选择无防火墙或指定一个防火墙。',routeTable(true));
}
function groupBadge(g) {
  if(!g.enabled) return badge('已停用','gray');
  const running=(status.listeners||[]).find(r=>r.id===g.id&&r.enabled);
  return running&&running.http_port===g.http_port&&running.https_port===g.https_port ? badge('监听中') : badge('重启后生效','amber');
}
function groupsHTML() {
  const action=discoveryButton()+'<button class="primary" data-action="add-group"><span data-icon="plus"></span>添加反代组</button>';
  const groups=config.groups.map((g,i)=>{
    const dns=config.ddns.groups.find(d=>d.id===g.ddns_group_id),count=config.routes.filter(r=>r.group_id===g.id).length;
    const summary=`${count} 个服务 · ${esc(portsText(g))}${g.domain_suffix?' · '+esc(g.domain_suffix):''}${dns?' · DDNS：'+esc(dns.name):''}`;
    const actions=discoveryButton(g.id)+addRouteButton(g.id)+objectMenu(g.name,`<button class="text-button" data-action="edit-group" data-index="${i}">编辑组</button><button class="text-button" data-action="toggle-group" data-index="${i}">${g.enabled?'停用组':'启用组'}</button><button class="text-button danger" data-action="delete-group" data-index="${i}">删除组</button>`);
    return groupPanel('proxy',g.id,g.name,summary,routeTable(false,false,g.id),actions,groupBadge(g));
  }).join('');
  return heading('PROXY GROUPS','反向代理','按组管理服务；点击组名展开或收起，编辑配置使用弹窗。',action)+(groups||empty('还没有反代组','先创建一个监听入口。',action))+'<p class="proxy-note">组内按域名分流。监听端口修改后需重启；访问账号和防火墙保存后生效。</p>';
}
function prepareTables(scope) {
  scope.querySelectorAll('.table-wrap').forEach(el=>{
    el.tabIndex=0;el.setAttribute('role','region');el.setAttribute('aria-label',(el.closest('.panel')?.querySelector('h2')?.textContent||'数据')+'列表');
    const labels=[...el.querySelectorAll('thead th')].map(th=>th.textContent);
    el.querySelectorAll('tbody tr').forEach(row=>[...row.cells].forEach((cell,i)=>{cell.dataset.label=labels[i]||'';}));
  });
}
function leaveRouteEditor() {
  const dialog=$('#route-dialog');
  if(dialog.open&&$('#route-form').dataset.dirty==='true'&&!confirm('放弃尚未保存的服务修改？')) return false;
  dialogMotions.get(dialog)?.cancel();dialog.close();return true;
}
function subscriptionsHTML() {
  const actions='<button class="primary" data-action="add-subscription"><span data-icon="plus"></span>添加订阅</button>';
  const rows=config.subscriptions.map((d,i)=>{
    const v=subscriptionStatus(d.id), failed=v.last_run&&!v.last_run.startsWith('0001')&&(!v.last_success||new Date(v.last_success)<new Date(v.last_run))&&!v.running;
    const state=!d.enabled?badge('已停用','gray'):v.running?badge('更新中'):failed?badge(v.ready?'更新失败，使用缓存':'拉取失败','amber'):badge(v.ready?'可用':'待更新',v.ready?'':'gray');
    const used=config.firewalls.filter(f=>firewallSubscriptions(f).includes(d.id)).length;
    return `<tr><td><b>${esc(d.name)}</b><small class="subscription-source"><a href="${esc(d.url)}" target="_blank" rel="noopener noreferrer">${esc(d.url)}</a></small></td><td>${v.count.toLocaleString()} 条<small>${used} 个防火墙引用</small></td><td>${state}<small>${esc(v.message)}</small></td><td>${date(v.last_success)}<small>每 ${d.interval.toLocaleString()} 秒更新</small></td><td><div class="table-actions"><button class="text-button" data-action="refresh-subscription" data-index="${i}" ${!d.enabled||v.running?'disabled':''}>更新</button><button class="text-button" data-action="edit-subscription" data-index="${i}">编辑</button><button class="text-button" data-action="toggle-subscription" data-index="${i}">${d.enabled?'停用':'启用'}</button><button class="text-button danger" data-action="delete-subscription" data-index="${i}">删除</button></div></td></tr>`;
  }).join('');
  return heading('IP SUBSCRIPTIONS','订阅管理','逐条管理订阅，定时更新后自动应用到引用它的防火墙 IP 组。',actions) +
    '<div class="hint">每行一个 IP / CIDR，支持 IPv4、IPv6 和 # 注释。修改 URL 后重新下载；更新失败保留上次成功列表。删除或停用订阅前，需要先移除防火墙组中的引用。</div>' +
    panel('快捷添加','mayaxcn/china-ip-list 提供中国大陆地址分配列表；省份列表可通过自定义链接添加。','<div class="panel-body preset-actions"><button class="secondary" data-action="preset-subscription" data-family="4">中国大陆 IPv4</button><button class="secondary" data-action="preset-subscription" data-family="6">中国大陆 IPv6</button><a class="inline-icon-link" href="https://github.com/mayaxcn/china-ip-list" target="_blank" rel="noopener noreferrer">查看来源 <span data-icon="external-link"></span><span class="sr-only">（在新标签页打开）</span></a></div>') +
    panel('已配置订阅','更新频率、条目数量与运行结果。',rows?`<div class="table-wrap"><table><thead><tr><th>订阅名称 / 来源</th><th>条目数</th><th>状态</th><th>最近成功更新</th><th>操作</th></tr></thead><tbody>${rows}</tbody></table></div>`:empty('还没有 IP 订阅','添加一个通用 HTTPS 链接，或使用上方中国 IP 快捷入口。',actions));
}
function overviewHTML() { return dashboardHTML(); }

function ddnsMode(mode) { return {ipv4:'IPv4（A）',ipv6:'IPv6（AAAA）',dual:'IPv4 + IPv6'}[mode]; }
function ddnsGroupStatus(g) { return status.jobs?.['ddns:'+g.id]; }
function ddnsBadge(g) { const job=ddnsGroupStatus(g), failed=job&&!job.running&&new Date(job.last_success)<new Date(job.last_run);return !g.enabled?badge('已停用','gray'):!job?badge('等待运行','gray'):job.running?badge('同步中'):failed?badge('同步失败','amber'):badge('同步正常'); }
function ddnsSummary(g) { const job=ddnsGroupStatus(g);return (job?job.message:'尚未运行此任务组。')+'\n最近检查：'+date(job?.last_run)+' · 最近成功：'+date(job?.last_success); }
function ddnsPrefixes(g) { return g.hosts.map(host=>host===g.zone?'@':host.slice(0,-g.zone.length-1)).join('\n'); }
function ddnsHosts(zone,value) {
  zone=zone.trim().toLowerCase();if(!zone) throw new Error('请先填写组的根域名');
  const prefixes=value.split(/[\n,]+/).map(s=>s.trim().toLowerCase()).filter(Boolean);
  if(!prefixes.length||prefixes.length>100) throw new Error('域名列表须包含 1–100 个子域名前缀');
  const hosts=prefixes.map(prefix=>prefix==='@'||prefix===zone?zone:prefix.endsWith('.'+zone)?prefix:prefix+'.'+zone);
  if(new Set(hosts).size!==hosts.length) throw new Error('域名列表中存在重复域名');
  return hosts;
}
function ddnsHostsConfig(current,id,value) {
  const next=clone(current),g=next.ddns.groups.find(g=>g.id===id);
  if(!g) throw new Error('此 DDNS 组已不存在，请刷新页面');
  g.hosts=ddnsHosts(g.zone,value);return next;
}
function updateDomainPreview(form,zone) {
  $('.error',form).textContent='';
  $('[data-domain-zone]',form).textContent=zone.trim().toLowerCase()||'请先填写根域名';
  const preview=$('[data-domain-preview]',form);
  try {preview.textContent=ddnsHosts(zone,form.elements.hosts.value).join('\n');}catch(e){preview.textContent=e.message;}
}
function openDDNSHosts(index) {
  const g=config.ddns.groups[index],form=$('#ddns-hosts-form');form.reset();
  form.elements.group_id.value=g.id;form.elements.hosts.value=ddnsPrefixes(g);
  $('#ddns-hosts-title').textContent='编辑域名 · '+g.name;$('.error',form).textContent='';
  updateDomainPreview(form,g.zone);openDialog($('#ddns-hosts-dialog'));
}
function domainAddressHTML(record,key) {
  const values=record?.[key]||[];
  if(record?.[key+'_error']) return `<span class="error">${esc(record[key+'_error'])}</span>`;
  if(values.length) return values.map(esc).join('<br>');
  return `<span class="muted">${record?.running?'查询中…':!record?.checked_at||record.checked_at.startsWith('0001')?'等待查询':'无记录'}</span>`;
}
function ddnsRecordsTable(g) {
  const records=dnsRecords[g.id]||[];
  return `<div class="table-wrap ddns-records-table" tabindex="0" role="region" aria-label="${esc(g.name)}域名解析列表"><table><thead><tr><th>域名</th><th>IPv4 / A</th><th>IPv6 / AAAA</th></tr></thead><tbody>${g.hosts.map(host=>{
    const record=records.find(r=>r.host===host);
    return `<tr><td class="mono"><b>${esc(host)}</b><small>${record?.checked_at&&!record.checked_at.startsWith('0001')?'最近解析：'+date(record.checked_at):dnsRecordsError?esc(dnsRecordsError):'公网 DNS 解析（DoH）'}</small>${record?.running&&record?.checked_at&&!record.checked_at.startsWith('0001')?'<small>正在刷新，显示上次解析</small>':''}</td><td class="mono">${domainAddressHTML(record,'ipv4')}</td><td class="mono">${domainAddressHTML(record,'ipv6')}</td></tr>`;
  }).join('')}</tbody></table></div>`;
}
async function loadDNSRecords(force=false) {
  const request=++dnsRecordsRequest;
  try {
    const data=await api('ddns/records'+(force?'?refresh=1':''));
    if(request!==dnsRecordsRequest||!config||page!=='ddns') return;
    dnsRecords=data.groups;dnsRecordsError='';
  } catch(error) {
    if(request!==dnsRecordsRequest||!config||page!=='ddns') return;
    dnsRecordsError='解析查询不可用，请刷新重试';
    if(force) toast(error.message);
  }
  if(!config||page!=='ddns') return;
  config.ddns.groups.forEach(g=>{
    const target=$('[data-ddns-records="'+g.id+'"]');if(!target) return;
    const previous=$('.table-wrap',target),left=previous?.scrollLeft||0,focused=document.activeElement===previous;
    target.innerHTML=ddnsRecordsTable(g);prepareTables(target);
    const table=$('.table-wrap',target);table.scrollLeft=left;if(focused) table.focus({preventScroll:true});
  });
}
function ddnsGroupsTable() {
  return config.ddns.groups.map((g,i)=>{
    const job=ddnsGroupStatus(g);
    const actions=`<button class="secondary" data-action="edit-ddns-hosts" data-index="${i}">编辑域名</button><button class="text-button" data-ddns-run="${esc(g.id)}" data-action="run-ddns" data-index="${i}" ${!g.enabled||job?.running?'disabled':''}>立即同步</button>`+objectMenu(g.name,`<button class="text-button" data-action="edit-ddns" data-index="${i}">编辑组</button><button class="text-button" data-action="toggle-ddns" data-index="${i}">${g.enabled?'停用':'启用'}</button><button class="text-button danger" data-action="delete-ddns" data-index="${i}">删除组</button>`);
    return groupPanel('ddns',g.id,g.name,`${g.provider==='cloudflare'?'Cloudflare':esc(g.provider)} · ${esc(g.zone)} · ${ddnsMode(g.mode)} · 每 ${g.interval} 秒检查 · ${g.hosts.length} 个域名`,`<div class="panel-body ddns-records-toolbar"><span>公网 DNS 解析（DoH） · 每 60 秒查询</span></div><div data-ddns-records="${esc(g.id)}">${ddnsRecordsTable(g)}</div><div class="panel-body"><p class="form-note">同步 IP 来源：${esc(g.interface||'自动路由')} · IPv4：${ipSourceName(g.ipv4_source)} · IPv6：${ipSourceName(g.ipv6_source)}</p><p class="form-note ddns-summary" data-ddns-summary="${esc(g.id)}">${esc(ddnsSummary(g))}</p></div>`,actions,`<span data-ddns-status="${esc(g.id)}">${ddnsBadge(g)}</span>`);
  }).join('')||panel('DDNS 任务组','每组可以同步多个域名。',empty('还没有 DDNS 组','按需选择 IPv4、IPv6 或双栈同步。','<button class="primary" data-action="add-ddns">添加 DDNS 组</button>'));
}
function jobSummary(kind) { const job=status.jobs?.[kind]; return `<p class="form-note" data-job-summary="${kind}">${job ? `${esc(job.message)}<br>最近检查：${date(job.last_run)}` : '尚未运行任务。'}</p>`; }
function ipSourceName(value) { return value==='interface'?'读取网卡公网 IP':'通过接口获取'; }
function ipEndpoints(g,kind) { return g[kind+'_urls']||(g[kind+'_url']?[g[kind+'_url']]:status.ip_query_defaults?.[kind]||[]); }
function interfaceChoices(selected='') {
  const interfaces=networkInfo?.interfaces||[];
  return '<option value="">自动路由</option>'+interfaces.map(i=>`<option value="${esc(i.name)}" ${i.name===selected?'selected':''} ${!i.up||i.loopback?'disabled':''}>${esc(i.name)}${!i.up?' · 未连接':i.loopback?' · 环回':' · '+esc(i.addresses.slice(0,2).join(' / ')||'暂无地址')}</option>`).join('')+(selected&&!interfaces.some(i=>i.name===selected)?`<option value="${esc(selected)}" selected>${esc(selected)} · 当前不存在</option>`:'');
}
function ipSelectionKey() {
  const g=config.ddns.groups.find(g=>g.id===ipGroup);
  return JSON.stringify(g?[g.id,g.interface,g.mode,g.ipv4_source,g.ipv6_source,ipEndpoints(g,'ipv4'),ipEndpoints(g,'ipv6')]:['preview',ipInterface,ipIPv4Source,ipIPv6Source]);
}
function ipControlsHTML() {
  const g=config.ddns.groups.find(g=>g.id===ipGroup);
  if(g) return `<p class="form-note">网卡：${esc(g.interface||'自动路由')} · IPv4：${ipSourceName(g.ipv4_source)} · IPv6：${ipSourceName(g.ipv6_source)}</p><button class="text-button" data-action="edit-ddns" data-index="${config.ddns.groups.indexOf(g)}">修改组内 IP 设置</button>`;
  const choices=selected=>`<option value="url" ${selected==='url'?'selected':''}>通过接口获取（推荐）</option><option value="interface" ${selected==='interface'?'selected':''}>读取网卡公网 IP</option>`;
  return `<label>网卡<select id="preview-interface">${interfaceChoices(ipInterface)}</select></label><label>IPv4 获取方式<select id="preview-ipv4-source">${choices(ipIPv4Source)}</select></label><label>IPv6 获取方式<select id="preview-ipv6-source">${choices(ipIPv6Source)}</select></label>`;
}
function ipCardsHTML(g,ip={},compact=false) {
  const cards=[['IPv4','ipv4'],['IPv6','ipv6']].map(([label,key])=>{
    const v=ip?.[key]||{},enabled=!g||g.mode==='dual'||g.mode===key;
    const source=key==='ipv4'?(g?(g.ipv4_source||'url'):ipIPv4Source):(g?(g.ipv6_source||'url'):ipIPv6Source);
    if(compact) return `<div class="ip-card"><span>${v.error&&v.address?'上次有效公网 '+label:'公网 '+label}</span><b class="mono">${enabled?esc(v.address||(ip?.running?'检测中…':'未获取')):'未启用此类型'}</b>${enabled?`<small>${ipSourceName(source)} · ${esc(v.interface||'尚未识别网卡')} · ${date(v.checked_at)}</small>${v.error?`<p class="error">${esc(v.error)}</p>`:''}`:''}</div>`;
    return `<div class="ip-card"><span>${v.error&&v.address?'上次有效公网 '+label:'公网 '+label}</span><b class="mono">${enabled?esc(v.address||(ip?.running?'检测中…':'未获取')):'未启用此类型'}</b>${enabled?`<p>${ipSourceName(source)} · 来源网卡：<b>${esc(v.interface||'尚未识别')}</b></p>${v.endpoint?`<p class="mono">${v.error&&v.address?'上次获取接口':'获取接口'}：${esc(v.endpoint)}${v.attempts>1?' · 已尝试 '+v.attempts+' 个接口':''}</p>`:''}<p class="mono">本地源地址：${esc(v.local_address||'—')}</p><small>最近检测：${date(v.checked_at)}${v.error&&v.address?' · 最近成功：'+date(v.last_success):''}</small>${v.error?`<p class="error">${esc(v.error)}</p>`:''}`:''}</div>`;
  }).join('');
  return `<div class="ip-cards${compact?' ip-cards-compact':''}">${cards}</div>`;
}
function ipResultsHTML(compact=false) {
  const view=ipViewKey===ipSelectionKey()?networkInfo:null;
  const g=config.ddns.groups.find(g=>g.id===ipGroup);
  return ipCardsHTML(g,view?.ip,compact)+(compact?'':`<p class="form-note">${view?.ip.running?'正在检测，IPv4 / IPv6 结果分别更新。':'页面打开期间每 30 秒检测，显示结果每 5 秒刷新。'}${ipGroup?'检测使用该组保存的 IP 设置。':'临时检测的接口方式使用默认列表；请在 DDNS 组中保存用于同步的 IP 设置。'}</p>`);
}
function networkHTML() {
  if(ipGroup&&!config.ddns.groups.some(g=>g.id===ipGroup)) ipGroup='';
  return '<div class="ip-monitor">'+panel('实时公网 IP','',`<div class="panel-body"><div id="ip-results" aria-live="polite">${ipResultsHTML(true)}</div><details class="ip-monitor-settings"><summary>检测设置与详情</summary><div class="ip-controls"><label>检测配置<select id="ip-group"><option value="">本机临时检测</option>${config.ddns.groups.map(g=>`<option value="${esc(g.id)}" ${g.id===ipGroup?'selected':''}>${esc(g.name)}</option>`).join('')}</select></label><div id="ip-controls" class="ip-controls">${ipControlsHTML()}</div></div><div id="ip-details">${ipResultsHTML()}</div><p class="form-note">Docker bridge 仅能看到容器内网卡；使用 Linux host 网络可选择主机网卡。</p></details></div>`,`<button class="secondary" data-action="refresh-public-ip">刷新公网 IP</button>`)+'</div>';
}
async function loadIPInfo(force=false) {
  const request=++ipRequest,key=ipSelectionKey();
  const input={group_id:ipGroup,interface:ipInterface,ipv4_source:ipIPv4Source,ipv6_source:ipIPv6Source};
  const data=await api('ddns/network'+(force?'':'?'+new URLSearchParams(input)),force?'POST':'GET',force?input:undefined);
  if(request!==ipRequest||!config||page!=='ddns'||key!==ipSelectionKey()) return;
  networkInfo=data;ipViewKey=key;
  if($('#ip-results')) $('#ip-results').innerHTML=ipResultsHTML(true);
  if($('#ip-details')) $('#ip-details').innerHTML=ipResultsHTML();
  const select=$('#preview-interface');if(select&&document.activeElement!==select) select.innerHTML=interfaceChoices(ipInterface);
  const formSelect=$('#ddns-group-form [name="interface"]');if(formSelect&&document.activeElement!==formSelect) formSelect.innerHTML=interfaceChoices(formSelect.value);
}
function ddnsHTML() {
  return heading('DYNAMIC DNS','动态域名','每组独立选择 DNS 服务商、根域名、账户凭据和同步类型。','<button class="secondary" data-action="refresh-dns-records">刷新解析</button><button class="secondary" data-job="ddns">同步全部启用组</button><button class="primary" data-action="add-ddns"><span data-icon="plus"></span>添加 DDNS 组</button>') +
    '<div class="hint">每个组使用自己的 DNS 账户，不同组可以配置不同根域名和 Token。当前支持 Cloudflare（仅 DNS / 灰云）。IP 查询保持直连，IPv6 发布前确认主机地址可入站。</div>' +
    networkHTML()+`<div id="ddns-groups">${ddnsGroupsTable()}</div>`;
}
function openDDNS(index = -1) {
  const form=$('#ddns-group-form');form.reset();
  const g=index<0?{provider:'cloudflare',zone:'',name:'',mode:'dual',hosts:[],interval:300,enabled:false}:config.ddns.groups[index];
  form.elements.index.value=index;
  form.elements.provider.innerHTML=(status.dns_providers||[{id:'cloudflare',name:'Cloudflare'}]).map(p=>`<option value="${esc(p.id)}">${esc(p.name)}</option>`).join('');
  for(const key of ['provider','zone','name','mode','interval']) form.elements[key].value=g[key];
  for(const kind of ['ipv4','ipv6']) form.elements[kind+'_urls'].value=ipEndpoints(g,kind).join('\n');
  form.elements.interface.innerHTML=interfaceChoices(g.interface||'');
  form.elements.ipv4_source.value=g.ipv4_source||'url';form.elements.ipv6_source.value=g.ipv6_source||'url';
  form.elements.token.value='';form.elements.token.placeholder=dnsCredentialsConfigured[g.id]?'已配置，留空保留该组 Token':'填写此组的 API Token';form.elements.clear_token.checked=false;
  form.elements.hosts.value=ddnsPrefixes(g);form.elements.enabled.checked=g.enabled;updateDomainPreview(form,g.zone);
  $('.error',form).textContent='';$('#ddns-title').textContent=index<0?'添加 DDNS 组':'编辑 DDNS 组';openDialog($('#ddns-dialog'));
}
function certificateTable() {
  const certs = status.certificates || [];
  if (!config.acme.requests.length) return empty('还没有证书任务','手动填写普通域名或泛域名，并配置独立 DNS 验证凭据。','<button class="secondary" data-action="add-certificate">添加证书任务</button>');
  return config.acme.requests.map((request,i)=>{
    const c=certs.find(c=>c.id===request.id),notAfter=c?.not_after&&!c.not_after.startsWith('0001')?date(c.not_after):'';
    const actions=`<button class="secondary" data-action="edit-certificate" data-index="${i}">编辑任务</button>`+objectMenu(request.domains[0],`<button class="text-button" data-action="toggle-certificate" data-index="${i}">${request.enabled?'暂停续期':'启用续期'}</button><button class="text-button danger" data-action="delete-certificate" data-index="${i}">移除任务</button>`);
    const summary=`${request.domains.length} 个域名 · DNS-01 · ${request.enabled?'自动申请与续期':'已暂停申请与续期'}${notAfter?' · 到期 '+notAfter:''}`;
    const body=`<dl class="panel-body certificate-task-info"><div><dt>证书域名</dt><dd class="mono">${request.domains.map(esc).join('<br>')}</dd></div><div><dt>DNS 验证</dt><dd>Cloudflare<small>${certificateCredentialsConfigured[request.id]?'独立凭据已配置':'需配置独立凭据'}</small></dd></div><div><dt>签发环境</dt><dd>${config.acme.staging?'测试环境':'正式环境'}</dd></div><div><dt>到期时间</dt><dd>${notAfter||'尚未签发'}</dd></div></dl>`;
    return groupPanel('certificate',request.id,request.domains[0],summary,body,actions,badge(c?.ready?'有效':'等待有效证书',c?.ready?'':'gray'));
  }).join('');
}
function openCertificate(index=-1) {
  const form=$('#certificate-request-form');form.reset();form.elements.index.value=index;
  const request=index<0?{provider:'cloudflare',domains:[],enabled:true}:config.acme.requests[index];
  form.elements.domains.value=request.domains.join('\n');form.elements.provider.value=request.provider;form.elements.enabled.checked=request.enabled;
  form.elements.token.placeholder=certificateCredentialsConfigured[request.id]?'已配置，留空保留此任务 Token':'填写此证书任务的 Cloudflare Token';
  $('#certificate-request-title').textContent=index<0?'添加证书任务':'编辑证书任务';$('.error',form).textContent='';openDialog($('#certificate-request-dialog'));
}
function openCertificateSettings() {
  const form=$('#cert-form'),a=config.acme;form.reset();
  form.elements.enabled.checked=a.enabled;form.elements.email.value=a.email;form.elements.staging.value=String(a.staging);form.elements.terms.checked=a.accept_terms;
  const job=status.jobs?.acme;$('.error',form).textContent='';$('[data-job-summary]',form).textContent=job?job.message+' · 最近检查：'+date(job.last_run):'尚未运行任务。';openDialog($('#cert-settings-dialog'));
}
function certificatesHTML() {
  const a=config.acme;
  return heading('CERTIFICATE MANAGEMENT','SSL 证书','按任务管理域名证书；邮箱、签发环境与自动申请选项在证书配置中设置。','<button class="secondary" data-action="configure-certificates"><span data-icon="settings"></span>证书配置</button><button class="secondary" data-job="acme">检查 / 申请证书</button><button class="primary" data-action="add-certificate"><span data-icon="plus"></span>添加证书任务</button>') +
    (a.staging?'<div class="hint amber">当前使用测试环境，签发的证书不被浏览器信任。完成验证后，可在证书配置中切换到正式环境。</div>':'')+
    '<p class="form-note certificate-note">一项任务的多个域名申请为同一张证书。泛域名仅覆盖一级子域名；需要同时覆盖根域名时，请一并添加根域名。</p>'+`<div id="certificate-table">${certificateTable()}</div>`;
}
function onlineUpdateHTML() {
  const release=onlineUpdate.release,pending=!!onlineUpdate.phase,supported=status.maintenance_available;
  const available=release?.update_available&&release.version!==status.version;
  const message=onlineUpdate.job?.phase==='checking'?'正在确认本次更新版本…':({checking:'正在通过服务器检查 GitHub 最新正式版…',downloading:'1 / 3 · 正在后台下载，当前服务继续运行…',applying:'2 / 3 · 正在校验更新包并安排重启…'}[onlineUpdate.phase]||'');
  const progress=onlineUpdate.phase==='downloading'&&onlineUpdate.job?.total?`<div class="online-update-progress"><progress max="${onlineUpdate.job.total}" value="${onlineUpdate.job.downloaded}" aria-label="更新包下载进度"></progress><span>${Math.min(100,Math.floor(100*onlineUpdate.job.downloaded/onlineUpdate.job.total))}% · ${(onlineUpdate.job.downloaded/1048576).toFixed(1)} / ${(onlineUpdate.job.total/1048576).toFixed(1)} MiB</span></div>`:'';
  return panel('在线更新','从 GateHome 的 GitHub 正式发布获取更新，使用设置中已保存的出站代理。',`<div class="panel-body online-update" aria-busy="${pending}"><ul class="info-list"><li><span>当前版本</span><b>v${esc(status.version||'加载中')}</b></li><li><span>更新连接</span><b>${config.outbound_proxy?.enabled?'使用已保存的出站代理':'直接连接 GitHub'}</b></li><li><span>最新正式版</span><b>${release?'v'+esc(release.version):'尚未检查'}</b></li>${release?`<li><span>发布时间 / 更新包</span><b>${date(release.published_at)} · ${(release.size/1048576).toFixed(1)} MiB</b></li>`:''}${onlineUpdate.job?.version?`<li><span>本次更新版本</span><b>v${esc(onlineUpdate.job.version)}</b></li>`:''}</ul><p class="form-note">下载后检查 GitHub SHA-256、包内文件哈希、版本和主机架构。更新时短暂中断服务，新版本启动检查失败会回滚。下载任务在服务器后台执行，刷新页面可继续查看进度。</p>${!supported?'<p class="form-note">当前启动方式支持检查版本；在线安装及自动重启需要 Linux 安装脚本或新版 Docker。</p>':''}<p class="online-update-status" role="status" aria-live="polite">${esc(message||(release?(available?'有新版本可用。':'当前版本已是最新，或高于最新正式版。'):''))}</p>${progress}<p class="error" role="alert">${esc(onlineUpdate.error)}</p><div class="form-actions"><button type="button" class="secondary" data-action="check-online-update" ${pending?'disabled':''}>${onlineUpdate.phase==='checking'?'检查中…':'检查更新'}</button><button type="button" class="primary" data-action="install-online-update" ${pending||!available||!supported||status.maintenance_busy?'disabled':''}>${pending&&onlineUpdate.phase!=='checking'?'更新中…':'更新并重启'}</button>${release?`<a href="${esc(release.release_url)}" target="_blank" rel="noopener noreferrer">${icon('external-link')}发行说明</a>`:''}</div></div>`);
}
function renderOnlineUpdate() {
  const root=$('#online-update');if(!root||!config) return;
  root.innerHTML=onlineUpdateHTML();hydrateIcons(root);
}
async function checkOnlineUpdate() {
  if(onlineUpdate.phase) return;
  clearTimeout(onlineUpdate.timer);onlineUpdate.request++;onlineUpdate.job=null;
  onlineUpdate.phase='checking';onlineUpdate.error='';onlineUpdate.release=null;renderOnlineUpdate();
  try {onlineUpdate.release=await api('maintenance/check-online-update','POST',{});}
  catch(error) {onlineUpdate.error=error.message;}
  finally {onlineUpdate.phase='';renderOnlineUpdate();}
}
async function installOnlineUpdate() {
  const release=onlineUpdate.release;
  if(onlineUpdate.phase||!release?.update_available||release.version===status.version) return;
  if(!confirm('更新至 v'+release.version+' 并重启服务？下载和校验期间继续运行，重启会短暂中断连接。')) return;
  onlineUpdate.phase='downloading';onlineUpdate.error='';renderOnlineUpdate();
  try {
    onlineUpdate.job=await api('maintenance/download-online-update','POST',{version:release.version});
    maintenancePreview=null;await loadOnlineUpdateStatus();
  } catch(error) {onlineUpdate.phase='';onlineUpdate.error=error.message;renderOnlineUpdate();}
}
async function loadOnlineUpdateStatus() {
  clearTimeout(onlineUpdate.timer);
  if(!config) return;
  const request=++onlineUpdate.request;
  try {
    const job=await api('maintenance/online-update-status');
    if(request!==onlineUpdate.request||!config) return;
    onlineUpdate.job=job.phase?job:null;onlineUpdate.error=job.error||'';
    if(job.phase==='restarting') {showLogin();toast('3 / 3 · 更新已安排，服务正在重启，稍候重新登录查看版本。');return;}
    onlineUpdate.phase=job.phase==='verifying'?'applying':['checking','downloading'].includes(job.phase)?'downloading':'';
    if(onlineUpdate.phase) onlineUpdate.timer=setTimeout(loadOnlineUpdateStatus,1500);
    renderOnlineUpdate();
  } catch(error) {
    if(request!==onlineUpdate.request||!config) return;
    onlineUpdate.error='进度查询暂时失败，正在重试：'+error.message;
    if(onlineUpdate.phase) onlineUpdate.timer=setTimeout(loadOnlineUpdateStatus,2000);
    renderOnlineUpdate();
  }
}
function maintenanceHTML() {
  const supported=status.maintenance_available;
  const preview=maintenancePreview;
  const detail=!preview?'':`<div class="maintenance-preview"><b>${preview.kind==='update'?'更新包 v'+esc(preview.version):'备份 v'+esc(preview.version)}</b>${preview.kind==='restore'?`<p>${preview.groups} 个反代组 · ${preview.routes} 个服务 · ${preview.ddns_groups} 个 DDNS 组 · ${preview.firewalls} 个防火墙 · ${preview.subscriptions} 个订阅<br>${preview.images||0} 张服务图片 · ${preview.certificates||0} 个证书文件 · ${preview.log_entries||0} 条日志 · ${preview.subscription_caches||0} 个订阅缓存<br>${preview.includes_files?'':'此旧备份未包含日志、缓存与图片；恢复时保留服务器现有文件。<br>'}备份时间：${date(preview.created_at)} · ${preview.token_configured?'含 Cloudflare Token':'未配置 Cloudflare Token'}</p>`:''}<p>${esc(preview.message)}</p><button class="primary" data-action="apply-maintenance" ${!preview.can_apply?'disabled':''}>${preview.kind==='update'?'应用更新并重启':'恢复数据并重启'}</button></div>`;
  const backupDetail=preview?.kind==='restore'?detail:'',updateDetail=preview?.kind==='update'?detail:'';
  return panel('备份与恢复','备份配置与凭据、证书、服务图片、日志和订阅缓存。',`<div class="panel-body two-col maintenance-forms"><form id="backup-form"><h3>下载加密备份</h3><label>备份密码<input name="password" type="password" autocomplete="new-password"></label><label>再次输入备份密码<input name="confirm_password" type="password" autocomplete="new-password"></label><p class="form-note">密码不限长度，可以留空；留空时恢复也无需填写密码。请保存设置的密码，恢复时需保持一致。ZIP 内的数据使用 AES-256-GCM 加密，含反代访问密码、IP 冻结名单与全部已保存的业务数据。登录会话、未保存的扫描结果和维护临时文件不备份。</p><p class="error" role="alert"></p><div class="form-actions"><button class="primary" type="submit">下载备份 ZIP</button></div></form><form id="restore-form"><h3>上传备份</h3><label>备份 ZIP<input name="file" type="file" accept=".zip,application/zip" required></label><label>导出时的备份密码<input name="password" type="password" autocomplete="off"></label><p class="form-note">密码不限长度；导出时留空，这里也留空。先检查备份再确认恢复。恢复后使用备份时的管理员密码重新登录。</p><p class="error" role="alert"></p><div class="form-actions"><button class="secondary" type="submit">检查备份</button></div></form></div>${backupDetail}`) +
    `<div id="online-update">${onlineUpdateHTML()}</div>` +
    panel('上传版本更新','使用本项目“更新版本”目录生成的 ZIP；只接受更高版本。',`<form id="update-form" class="panel-body"><label>更新 ZIP<input name="file" type="file" accept=".zip,application/zip" required></label><p class="form-note">检查版本、文件哈希和主机架构后再应用。只使用你信任的项目更新包；哈希校验用于检查文件完整性。</p><p class="form-note">${supported?'维护时短暂停止服务，新程序启动检查失败会回滚。':'此部署可导出备份及检查上传包。应用更新或恢复需要 Linux 安装脚本或新版 Docker 启动方式。'}</p><p class="error" role="alert"></p><div class="form-actions"><button type="submit" class="secondary">检查更新包</button></div></form>${updateDetail}`) +
    `<div class="app-version"><b>Gatehouse</b><button class="secondary" data-action="restart-service" ${!supported?'disabled':''}>重启服务</button><span>版本 ${esc(status.version||'加载中')}</span></div>`;
}
async function inspectMaintenance(form,kind) {
  const data=new FormData(form);
  const response=await fetch('/api/maintenance/inspect-'+(kind==='restore'?'backup':'update'),{method:'POST',credentials:'same-origin',headers:{'X-Gatehouse-Request':'1'},body:data});
  if(form.elements.password) form.elements.password.value='';
  const result=await response.json();if(!response.ok) throw new Error(result.error||'文件检查失败');maintenancePreview={...result,kind};render();
}
async function downloadBackup(password) {
  const response=await fetch('/api/maintenance/backup',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json','X-Gatehouse-Request':'1'},body:JSON.stringify({password})});
  if(!response.ok) {const result=await response.json();throw new Error(result.error||'备份失败');}
  const blob=await response.blob(),url=URL.createObjectURL(blob),link=document.createElement('a');link.href=url;link.download='gatehouse-backup-'+new Date().toISOString().replace(/[:.]/g,'-')+'.zip';document.body.append(link);link.click();link.remove();setTimeout(()=>URL.revokeObjectURL(url),30000);
  toast('加密备份已生成，请妥善保存 ZIP 和备份密码');
}
function adminAccessHTML() {
  const access=config.admin_access||{enabled:false,origins:[]};
  return panel('管理界面反代访问','通过自己的公网域名登录和管理 Gatehouse。',`<form id="admin-access-form" class="panel-body"><div class="toggle-row"><div><b>允许指定公网地址反代访问</b><p>兼容公网 HTTPS 接入内部 HTTP 管理端口，保存后立即生效。</p></div><label class="switch"><input name="enabled" type="checkbox" ${access.enabled?'checked':''}><span></span><span class="sr-only">允许指定公网地址反代访问</span></label></div><label>公网访问地址<textarea name="origins" rows="3" spellcheck="false" placeholder="https://gate.example.com:18443">${esc((access.origins||[]).join('\n'))}</textarea><small>每行一个实际访问地址，包含 http:// 或 https:// 和非默认端口，不填写路径。最多 20 个地址。</small></label><p class="form-note">在反向代理中添加对应域名，内网服务选 HTTP，地址填写 127.0.0.1:16666（管理端口）。仍须使用管理密码登录；只允许填写的地址，不开放其他网站的跨站 API 调用。</p><p class="error" role="alert"></p><div class="form-actions"><button type="submit" class="primary">保存访问设置</button></div></form>`);
}
function logRetentionHTML() {
  const c=config.log_retention?.max_size_mb?config.log_retention:{max_size_mb:16,keep_days:30},s=status.log_storage||{};
  return panel('日志保留','项目、反代、防火墙与安全事件共用一个空间上限。',`<form id="log-retention-form" class="panel-body"><div class="form-grid"><label>最大占用空间（MiB）<input name="max_size_mb" type="number" min="1" max="1024" step="1" required value="${esc(c.max_size_mb)}"><small>1–1024 MiB，所有调用日志文件合计。</small></label><label>保留天数<input name="keep_days" type="number" min="1" max="3650" step="1" required value="${esc(c.keep_days)}"><small>1–3650 天，按记录时间自动清理。</small></label></div><p class="form-note">超过任一限制时删除最早的日志。保存后立即应用，此后每分钟检查；清理后的日志无法恢复。</p><p class="form-note">当前占用 ${(Number(s.used_bytes||0)/(1<<20)).toFixed(2)} MiB · 最近检查：${date(s.last_cleanup)}。页面查询和统计显示最近 5000 条保留记录。</p><p class="form-note">日志会包含在备份中；网页备份总内容上限为 64 MiB，较大的日志目录可通过文件备份保留。</p><p class="error" role="alert"></p>${s.write_error?'<p class="error" role="alert">日志写入或清理失败，请检查日志目录权限和磁盘空间。</p>':''}<div class="form-actions"><button type="submit" class="primary">保存日志设置</button></div></form>`);
}
function settingsHTML() {
  const rows=config.groups.map((g,i)=>`<tr><td><b>${esc(g.name)}</b></td><td class="mono">${g.http_port||'已关闭'}</td><td class="mono">${g.https_port||'已关闭'}</td><td>${groupBadge(g)}</td><td><button class="text-button" data-action="edit-group" data-index="${i}">编辑组</button></td></tr>`).join('');
  const p=config.outbound_proxy;
  return heading('SETTINGS','系统设置','集中配置各组监听端口和后台请求代理。','<button class="primary" data-action="add-group"><span data-icon="plus"></span>添加反代组</button>') +
    adminAccessHTML()+logRetentionHTML()+
    panel('出站代理服务器','用于在线更新、订阅、Cloudflare API、证书 HTTP 请求及公网 DNS 解析；保存后立即生效。',`<form id="outbound-form" class="panel-body"><div class="toggle-row"><div><b>启用出站代理</b><p>公网 IP 探测及内网反向代理连接保持直连。</p></div><label class="switch"><input name="enabled" type="checkbox" ${p.enabled?'checked':''}><span></span><span class="sr-only">启用出站代理</span></label></div><div class="form-grid"><label class="span-2">代理地址<input name="url" value="${esc(p.url)}" placeholder="http://127.0.0.1:7890"><small>支持 HTTP、HTTPS、SOCKS5 / SOCKS5H，需填写端口。地址中不要填写账号密码。Docker 内的 127.0.0.1 指容器自身。</small></label><label>代理用户名（可选）<input name="username" value="${esc(p.username)}" autocomplete="off"></label><label>代理密码（可选）<input name="password" type="password" autocomplete="new-password" placeholder="${proxyPasswordConfigured?'已配置，留空保留':'无需认证可留空'}"><small>密码不回显；变更服务器或用户名后需重新填写。</small></label><label class="check-label span-2"><input name="clear_password" type="checkbox">清除已保存的代理密码</label></div><p class="form-note">代理连接失败时会记录错误，不自动退回直连。证书 DNS 传播检查仍需直接访问 DNS 服务。</p><p class="error" role="alert"></p><div class="form-actions"><button type="submit" class="primary">保存代理设置</button></div></form>`) +
    '<div class="hint">爱快为每组分别配置 TCP 映射。例如外网 18443 映射到第一组 HTTPS 18443，外网 19443 映射到第二组 HTTPS 19443。修改监听或组启停状态后需重启服务。</div>' +
    panel('反代组监听','HTTP / HTTPS 填写 0 即关闭该协议。',rows?`<div class="table-wrap"><table><thead><tr><th>反代组</th><th>HTTP</th><th>HTTPS</th><th>状态</th><th>操作</th></tr></thead><tbody>${rows}</tbody></table></div>`:empty('还没有反代组','到反向代理页面创建组和域名规则。')) +
    panel('部署提示','根据运行方式确认网络连接。','<div class="panel-body"><ul class="info-list"><li><span>管理入口</span><b>默认 0.0.0.0:16666，可通过服务器 IP 访问</b></li><li><span>Docker bridge</span><b>为各组非零端口添加 compose.bridge.yaml 映射</b></li><li><span>IP 黑白名单</span><b>Linux Docker 推荐 host 网络，保留来源 IP</b></li><li><span>订阅更新</span><b>立即应用规则，无需重启</b></li><li><span>爱快映射</span><b>按组映射业务端口；管理可使用 SSH 隧道</b></li></ul></div>') + maintenanceHTML();
}
function readLogFilters() {logCategory=$('#log-category')?.value||'';logRule=$('#log-rule')?.value||'';logResult=$('#log-result').value;logMethod=$('#log-method')?.value||'';logStatus=$('#log-status')?.value||'';logSearch=$('#log-search').value.trim();logIP=$('#log-ip')?.value.trim()||'';logFrom=$('#log-from').value;logTo=$('#log-to').value;}
function logCategoryName(category) { return {access:'反向代理',admin:'管理操作',ddns:'DDNS',subscriptions:'IP 订阅',certificates:'SSL 证书'}[category]||category; }
function logsHTML() {
  const access=logScope==='access';
  const rows=logView.entries.map(e=>`<tr><td>${date(e.time)}<small>${esc(logCategoryName(e.category))} · #${e.id}</small></td><td><b>${esc(e.action)}</b><small class="mono">${esc(e.target)}${e.path?'<br>'+esc(e.path):''}</small><small>${esc(e.method)}${!access&&e.remote?' · 来源 '+esc(e.remote):''}</small></td>${access?'<td class="source-ip">'+sourceIPHTML(e)+'</td>':''}<td>${badge(e.ok?'成功':'失败',e.ok?'':'red')}<small>${e.status?'HTTP '+e.status:''} · ${e.duration_ms} ms</small></td><td class="log-message">${esc(e.message)}${securityHitsHTML(e)}</td></tr>`).join('');
  return heading('CALL LOGS',access?'反代日志':'项目日志',access?'按代理规则、访问结果筛选请求记录。':'查看订阅、DDNS、证书和管理操作的调用结果。','<button class="secondary" data-action="refresh-logs">刷新日志</button>') +
    '<div class="hint">日志持久化到本机日志目录，总空间与保留天数可在「设置 → 日志保留」修改，超限自动删除最早记录。忽略查询参数、请求正文及认证头，隐藏已配置凭据和敏感路径片段。WebSocket 记录在连接关闭后显示。</div>' +
    `<div class="log-tabs"><button class="${access?'secondary':'primary'}" data-action="project-logs">项目日志</button><button class="${access?'primary':'secondary'}" data-action="access-logs">反代日志</button></div><div class="log-filters">${access?`<label>反代规则<select id="log-rule"><option value="">全部反代规则</option>${config.routes.map(r=>{const key=r.group_id+'/'+r.host;return `<option value="${esc(key)}" ${logRule===key?'selected':''}>${esc(groupName(r.group_id))} / ${esc(r.name||r.host)} · ${esc(r.host)}</option>`;}).join('')}</select></label>`:`<label>项目分类<select id="log-category"><option value="">全部项目分类</option>${['admin','ddns','subscriptions','certificates'].map(c=>`<option value="${c}" ${logCategory===c?'selected':''}>${logCategoryName(c)}</option>`).join('')}</select></label>`}<label>结果<select id="log-result"><option value="">全部结果</option><option value="success" ${logResult==='success'?'selected':''}>成功</option><option value="error" ${logResult==='error'?'selected':''}>失败</option></select></label>${access?`<label>来源 IP<input id="log-ip" value="${esc(logIP)}" maxlength="45" placeholder="精确 IPv4 / IPv6" spellcheck="false"></label><label>请求方法<select id="log-method"><option value="">全部方法</option>${['GET','POST','PUT','PATCH','DELETE','HEAD','OPTIONS','CONNECT'].map(m=>`<option ${logMethod===m?'selected':''}>${m}</option>`).join('')}</select></label><label>HTTP 状态码<input id="log-status" type="number" min="100" max="599" placeholder="例如 403" value="${esc(logStatus)}"></label>`:''}<label>开始时间<input id="log-from" type="datetime-local" value="${esc(logFrom)}"></label><label>结束时间<input id="log-to" type="datetime-local" value="${esc(logTo)}"></label><label class="log-search">关键词<input id="log-search" placeholder="域名、IP、路径或操作结果" maxlength="200" value="${esc(logSearch)}"></label><button class="secondary" data-action="apply-log-filters">筛选</button><button class="text-button" data-action="reset-log-filters">重置</button></div>` +
    (logView.write_error?'<div class="hint amber">日志写入失败，当前只显示内存记录，请检查数据目录权限与磁盘空间。</div>':'') +
    logPaginationHTML()+panel('调用明细','时间、操作、来源、状态、耗时及结果。',rows?`<div class="table-wrap"><table><thead><tr><th>时间 / 分类</th><th>调用 / 目标</th>${access?'<th>来源 IP / 归属地</th>':''}<th>结果 / 耗时</th><th>详细说明</th></tr></thead><tbody>${rows}</tbody></table></div>`:empty('暂无匹配日志','执行订阅更新、DDNS 同步或代理访问后可在此查看。')) +
    logPaginationHTML(true);
}
function logPaginationHTML(bottom=false) {
  const pages=logView.pages||1,visible=[...new Set([1,pages,logPage-2,logPage-1,logPage,logPage+1,logPage+2].filter(n=>n>=1&&n<=pages))].sort((a,b)=>a-b);
  const buttons=visible.map((n,i)=>(i&&n-visible[i-1]>1?'<span>…</span>':'')+`<button type="button" class="${n===logPage?'primary':'secondary'}" data-action="log-page" data-page-number="${n}" ${n===logPage?'aria-current="page" disabled':''} aria-label="第 ${n} 页">${n}</button>`).join('');
  return `<div class="log-pagination"><span>共 ${logView.total||0} 条 · 第 ${logPage} / ${pages} 页</span>${bottom?'':`<label>每页条数<select id="log-size">${[20,50,100,200].map(n=>`<option value="${n}" ${logSize===n?'selected':''}>${n} 条</option>`).join('')}</select></label>`}<div class="pagination-buttons"><button type="button" class="secondary" data-action="log-page" data-page-number="${logPage-1}" ${logPage<=1?'disabled':''}>${icon('chevron-left')}上一页</button>${buttons}<button type="button" class="secondary" data-action="log-page" data-page-number="${logPage+1}" ${logPage>=pages?'disabled':''}>下一页${icon('chevron-right')}</button></div></div>`;
}
async function loadLogs(targetPage=1,keepSnapshot=false) {
  const request=++logRequest;
  const result=await api('logs?'+new URLSearchParams({scope:logScope,category:logScope==='access'?'access':logCategory,result:logResult,rule:logScope==='access'?logRule:'',search:logSearch,ip:logScope==='access'?logIP:'',from:logFrom?new Date(logFrom).toISOString():'',to:logTo?new Date(logTo).toISOString():'',method:logScope==='access'?logMethod:'',status:logScope==='access'?logStatus:'',page:String(targetPage),size:String(logSize),through:keepSnapshot?String(logView.through):'0'}));
  if(request!==logRequest) return;
  logView=result;logPage=result.page;if(page==='logs') render();
}

function freezeNow() { return ipBlockView.now?new Date(ipBlockView.now).getTime()+Date.now()-ipBlockReceived:Date.now(); }
function frozenRows() {return (ipBlockView.entries||[]).filter(b=>new Date(b.until).getTime()>freezeNow());}
function remainingFreeze(until) {
  const minutes=Math.max(1,Math.ceil((new Date(until).getTime()-freezeNow())/60000));
  return minutes>=60?Math.floor(minutes/60)+' 小时 '+(minutes%60)+' 分钟':minutes+' 分钟';
}
function ipBlocksHTML() {
  const all=frozenRows(),filtered=all.filter(b=>(!ipBlockRule||b.rule===ipBlockRule)&&(!ipBlockSearch||b.ip.toLowerCase().includes(ipBlockSearch.toLowerCase())));
  const count=Math.max(1,Math.ceil(filtered.length/20));ipBlockPage=Math.min(count,Math.max(1,ipBlockPage));
  const cards=filtered.slice((ipBlockPage-1)*20,ipBlockPage*20).map(b=>`<article class="freeze-row"><div class="freeze-identity"><b class="mono">${esc(b.ip)}</b><small>${esc(statisticsRuleName(b.rule))}</small><small class="mono">${esc(b.rule)}</small></div><div class="freeze-reason">${badge(b.source==='security'?'防护触发自动冻结':b.source==='automatic'?'验证失败自动冻结':'手动冻结',b.source!=='manual'?'amber':'gray')}<p>${esc(b.reason)}</p>${b.failures?`<small>触发时累计 ${b.failures} 次</small>`:''}</div><div class="freeze-time" data-until="${esc(b.until)}"><span>剩余 ${remainingFreeze(b.until)}</span><small>冻结：${date(b.started_at)}</small><small>解冻：${date(b.until)}</small></div><button class="secondary" data-action="release-ip-block" data-rule="${esc(b.rule)}" data-ip="${esc(b.ip)}">解除冻结</button></article>`).join('');
  const metrics=[['当前冻结',all.length,'每条规则单独拦截'],['自动冻结',all.filter(b=>b.source!=='manual').length,'账号验证 / 防护触发'],['手动冻结',all.filter(b=>b.source==='manual').length,'管理员加入名单']];
  return heading('IP ACCESS CONTROL','IP 拦截名单','查看冻结来源、触发规则和解冻时间，到期自动解除。',`<button class="secondary" data-action="refresh-ip-blocks">刷新名单</button><button class="primary" data-action="add-ip-block" ${!config.routes.length?'disabled':''}>${icon('plus')}手动冻结 IP</button>`)+
    '<div class="hint">每条反代规则按来源 IP 分别计数：默认连续验证失败 5 次，冻结 1 小时。账号失败次数和时长在代理服务编辑页设置；WAF、HTTP 规则和限速的冻结策略在防火墙中设置。成功验证清零账号连续失败，解除冻结清零该服务的防护计数。来源采用实际连接 IP；转发请求头不会改变拦截对象。</div>'+
    (ipBlockError?`<div class="hint amber" role="alert">刷新失败：${esc(ipBlockError)}。当前显示上次获取的名单。</div>`:'')+
    (ipBlockView.write_error?'<div class="hint amber" role="alert">冻结记录保存失败。当前进程仍执行拦截，但重启可能丢失最新变更；请检查数据目录权限与磁盘空间。</div>':'')+
    `<div class="security-metrics">${metrics.map(([label,value,note])=>`<div class="stat"><span class="stat-label">${label}</span><div class="stat-value">${value}<small>个</small></div><span class="stat-foot">${note}</span></div>`).join('')}</div>`+
    `<div class="statistics-filters"><label>反代规则<select id="ip-block-rule"><option value="">全部反代规则</option>${[...new Set([...config.routes.map(r=>r.group_id+'/'+r.host),...all.map(b=>b.rule)])].map(key=>`<option value="${esc(key)}" ${ipBlockRule===key?'selected':''}>${esc(statisticsRuleName(key))}</option>`).join('')}</select></label><label>来源 IP<input id="ip-block-search" type="text" value="${esc(ipBlockSearch)}" placeholder="搜索 IPv4 / IPv6" maxlength="64"></label><button class="secondary" data-action="filter-ip-blocks">筛选名单</button></div>`+
    panel('冻结记录',`共 ${filtered.length} 条匹配记录 · 第 ${ipBlockPage} / ${count} 页`,cards?`<div class="freeze-list">${cards}</div>`:empty('当前没有匹配的冻结 IP','达到账号或防护冻结阈值、或手动冻结后，记录显示在这里。'))+
    `<div class="log-pagination"><span>每页 20 条 · 到期自动解除</span><div class="pagination-buttons"><button class="secondary" data-action="ip-block-page" data-page-number="${ipBlockPage-1}" ${ipBlockPage<=1?'disabled':''}>${icon('chevron-left')}上一页</button><button class="secondary" data-action="ip-block-page" data-page-number="${ipBlockPage+1}" ${ipBlockPage>=count?'disabled':''}>下一页${icon('chevron-right')}</button></div></div>`;
}
async function loadIPBlocks(quiet=false) {
  const request=++ipBlockRequest;
  try {
    const result=await api('ip-blocks');
    if(request!==ipBlockRequest) return;
    const changed=JSON.stringify(ipBlockView.entries)!==JSON.stringify(result.entries)||ipBlockView.write_error!==result.write_error||!!ipBlockError;
    ipBlockView=result;ipBlockReceived=Date.now();ipBlockError='';
    if(page==='ip-blocks'&&(!quiet||changed)) render();
    else if(page==='ip-blocks') document.querySelectorAll('.freeze-time[data-until] span').forEach(el=>{el.textContent='剩余 '+remainingFreeze(el.parentElement.dataset.until);});
  } catch(error) {if(request===ipBlockRequest) {ipBlockError=error.message;if(page==='ip-blocks') render();}throw error;}
}
function openIPBlock() {
  const form=$('#ip-block-form');form.reset();
  form.elements.rule.innerHTML=config.routes.map(r=>{const key=r.group_id+'/'+r.host;return `<option value="${esc(key)}">${esc(statisticsRuleName(key))} · ${esc(r.host)}</option>`;}).join('');
  if(config.routes.some(r=>r.group_id+'/'+r.host===ipBlockRule)) form.elements.rule.value=ipBlockRule;
  $('.error',form).textContent='';openDialog($('#ip-block-dialog'));
}
function securityEngineName(engine) {return ({waf:'CRS',custom:'HTTP 规则',rate:'访问限速',system:'系统保护',ip:'IP 规则',auth:'账号验证',freeze:'IP 冻结'})[engine]||engine;}
function securityOutcome(e) {
  if(e.freeze_created) return '达到阈值 · 冻结';
  if(e.outcome==='forwarded'&&e.security?.length) return '仅记录 · 已放行';
  return ({blocked:'防火墙拦截',ip_frozen:'冻结 IP 拦截',auth_failed:'账号验证失败',auth_rate_limited:'验证速率限制',auth_rejected:'验证请求被拒绝'})[e.outcome]||'访问被拒绝';
}
function sourceIPHTML(e) {
  return `<span class="mono">${esc(e.remote||'—')}</span><small class="ip-region">${esc(e.remote_region||'归属地未知')}</small>`;
}
function securityHitsHTML(e) {
  return (e.security||[]).map(h=>`<small>${esc(securityEngineName(h.engine))} #${esc(h.rule_id)} · ${esc(h.name)} · ${h.action==='block'?'拦截':'仅记录'}${h.score?' · 分数 '+esc(h.score):''}${h.severity?' · '+esc(h.severity):''}</small>`).join('');
}
function emptyEventFilters() {return {rule:'',firewall:'',engine:'',decision:'',ip:'',rule_id:'',from:'',to:'',search:''};}
function eventSelect(kind,key,label,options) {
  const value=securityEvents[kind].filters[key];
  return `<label class="event-${key}">${label}<select id="${kind}-event-${key}" name="${key}">${options.map(([v,text])=>`<option value="${esc(v)}" ${v===value?'selected':''}>${esc(text)}</option>`).join('')}</select></label>`;
}
function eventInput(kind,key,label,type='text',extra='') {
  return `<label class="event-${key}">${label}<input id="${kind}-event-${key}" name="${key}" type="${type}" value="${esc(securityEvents[kind].filters[key])}" ${extra}></label>`;
}
function eventPaginationHTML(kind,bottom=false) {
  const state=securityEvents[kind],d=state.view||{page:1,pages:1,total:0},current=d.page,pages=d.pages;
  const numbers=[...new Set([1,pages,current-1,current,current+1].filter(n=>n>=1&&n<=pages))].sort((a,b)=>a-b);
  const button=(n,label,disabled=false,key=String(n))=>`<button type="button" id="${kind}-event-page-${bottom?'bottom':'top'}-${key}" class="${n===current?'primary':'secondary'}" data-action="event-page" data-kind="${kind}" data-page-number="${n}" ${disabled||state.loading?'disabled':''} ${n===current?'aria-current="page"':''}>${label}</button>`;
  return `<nav class="log-pagination event-pagination" aria-label="${kind==='firewall'?'防火墙记录':'安全事件'}${bottom?'底部':'顶部'}分页"><span id="${kind}-event-result${bottom?'-bottom':''}" tabindex="-1" role="status">共 ${d.total} 条 · 第 ${current} / ${pages} 页</span>${bottom?'':`<label>每页条数<select id="${kind}-event-size" data-event-size="${kind}" ${state.loading?'disabled':''}>${[20,50,100,200].map(n=>`<option value="${n}" ${n===state.size?'selected':''}>${n} 条</option>`).join('')}</select></label>`}<div class="pagination-buttons">${button(current-1,icon('chevron-left')+'上一页',current<=1,'previous')}${numbers.map((n,i)=>(i&&n-numbers[i-1]>1?'<span>…</span>':'')+button(n,String(n),n===current)).join('')}${button(current+1,'下一页'+icon('chevron-right'),current>=pages,'next')}</div></nav>`;
}
function securityEventsHTML(kind) {
  const state=securityEvents[kind],firewall=kind==='firewall';
  const services=[['','全部反代服务'],...config.routes.map(r=>[r.group_id+'/'+r.host,statisticsRuleName(r.group_id+'/'+r.host)+' · '+r.host])];
  const policies=[['','全部防火墙'],...config.firewalls.map(f=>[f.id,f.name])];
  // Preserve selected historical identities even after a service/policy is removed.
  if(state.filters.rule&&!services.some(([id])=>id===state.filters.rule)) services.push([state.filters.rule,state.filters.rule+'（历史服务）']);
  if(state.filters.firewall&&!policies.some(([id])=>id===state.filters.firewall)) policies.push([state.filters.firewall,state.filters.firewall+'（历史防火墙）']);
  const engines=[['','全部检测类型'],...['ip','waf','custom','rate','system','freeze',...(firewall?[]:['auth'])].map(v=>[v,securityEngineName(v)])];
  const form=`<form id="${kind}-event-filters" class="event-filters" data-event-filters="${kind}"><fieldset ${state.loading?'disabled':''}>`+
    eventSelect(kind,'rule','反代服务',services)+eventSelect(kind,'firewall','防火墙',policies)+eventSelect(kind,'engine','检测类型',engines)+eventSelect(kind,'decision','处理结果',[['','全部结果'],['block','拦截'],['detect','仅记录']])+
    eventInput(kind,'ip','来源 IP','text','maxlength="45" placeholder="精确 IPv4 / IPv6" spellcheck="false"')+eventInput(kind,'rule_id','检测规则编号','number','min="1" max="999999999" placeholder="例如 941100"')+
    eventInput(kind,'from','开始时间','datetime-local')+eventInput(kind,'to','结束时间','datetime-local')+eventInput(kind,'search','关键词','text','maxlength="200" placeholder="域名、路径、规则名称或原因"')+
    `<div class="event-filter-actions"><button id="${kind}-event-apply" class="primary" type="submit" ${state.loading?'aria-busy="true"':''}>${icon('filter')}筛选</button><button class="secondary" type="button" data-action="reset-events" data-kind="${kind}">${icon('refresh')}重置</button></div></fieldset><p class="form-note">点击筛选应用条件；翻页沿用已应用的条件和日志快照。</p></form>`;
  const rows=(state.view?.entries||[]).map(e=>`<tr data-event-id="${esc(e.id)}"><td>${date(e.time)}<small>#${esc(e.id)}</small></td><td class="source-ip">${sourceIPHTML(e)}</td><td><b>${esc(statisticsRuleName(e.rule))}</b><small class="mono">${esc(e.target||e.rule.split('/').slice(1).join('/')||e.rule)}</small><small>${esc(e.firewall?.name||'—')}</small></td><td>${badge(securityOutcome(e),e.outcome==='forwarded'?'gray':'amber')}<small>HTTP ${esc(e.status)} · ${esc(e.method)}</small><small class="mono">${esc(e.path)}</small></td><td>${esc(e.firewall?.reason||e.message)}${securityHitsHTML(e)}${e.firewall?.order?`<small>第 ${esc(e.firewall.order)} 个 IP 组 · ${e.firewall.match==='exclude'?'排除匹配':'包含匹配'}</small>`:''}</td></tr>`).join('');
  const table=rows?`<div class="table-wrap security-table"><table><thead><tr><th>时间</th><th>来源 IP / 归属地</th><th>反代服务 / 防火墙</th><th>处理结果 / 请求</th><th>规则与原因</th></tr></thead><tbody>${rows}</tbody></table></div>`:empty(state.loading?'正在加载记录':state.view?'暂无匹配记录':'尚未加载记录',state.loading?'请稍候。':'可调整筛选条件，或在产生安全事件后刷新。');
  const error=state.error?`<div id="${kind}-event-error" class="hint amber" role="alert" tabindex="-1">加载失败：${esc(state.error)}。${state.view?'保留上次成功结果，请重试。':'请检查筛选条件并重试。'}</div>`:'';
  return `<div id="${kind}-events">`+panel(firewall?'防火墙记录':'最近安全事件',firewall?'IP 规则、CRS、HTTP 规则、限速、系统保护及冻结 IP 拦截；包含仅记录命中。':'此列表按下方条件独立筛选；图表汇总使用页面顶部条件。',`<div class="panel-body">${form}${error}${state.view?.write_error?'<div class="hint amber" role="alert">日志保存失败，当前显示内存记录。</div>':''}${state.loading?'<p class="form-note" role="status">正在更新，保留当前结果…</p>':''}${eventPaginationHTML(kind)}${table}${eventPaginationHTML(kind,true)}</div>`,`<button class="secondary" data-action="refresh-events" data-kind="${kind}" ${state.loading?'disabled':''}>${icon('refresh')}刷新记录</button>`)+`</div>`;
}
function firewallLogsHTML() {
  return heading('FIREWALL RECORDS','防火墙拦截记录','按来源、服务和触发规则查看防火墙拦截及仅记录命中。')+
    '<div class="hint">翻页保持当前日志快照；筛选或刷新从第一页读取最新记录。日志最多保留 5000 条，轮转后的旧记录不在查询范围内。IP 归属地使用本地数据库估算，仅供参考。</div>'+securityEventsHTML('firewall');
}
async function loadSecurityEvents(kind,targetPage=1,keepSnapshot=false) {
  const state=securityEvents[kind],request=++state.request,filters={...(keepSnapshot?state.applied:state.filters)};
  const focused=focusSelector(document.activeElement),pageChange=document.activeElement?.dataset.action==='event-page';
  const visible=()=>page===(kind==='firewall'?'firewall-logs':'statistics');
  state.loading=true;state.error='';if(visible()) render();
  try {
    const query={...filters,scope:kind,page:String(targetPage),size:String(state.size),through:keepSnapshot?String(state.view?.through||0):'0'};
    for(const key of ['from','to']) query[key]=filters[key]?new Date(filters[key]).toISOString():'';
    const data=await api('logs?'+new URLSearchParams(query));
    if(request===state.request) {state.view=data;state.applied=filters;}
  } catch(error) {
    if(request===state.request) {state.error=error.message;throw error;}
  } finally {
    if(request===state.request) {state.loading=false;if(visible()) {render();if(state.error) $('#'+kind+'-event-error')?.focus();else if(pageChange) $('#'+kind+'-event-result')?.focus();else if(focused) $(focused)?.focus({preventScroll:true});}}
  }
}
function securityStatisticsHTML(d) {
  const metrics=[['WAF 命中请求',d.waf_matched||0,'一次请求只计一次'],['WAF 拦截',d.waf_blocked||0,'实际被 WAF 拦截'],['访问限速拦截',d.rate_blocked||0,'超出窗口允许次数'],['HTTP 规则拦截',d.custom_blocked||0,'自定义规则'],['验证成功',d.auth_success||0,'独立服务账号'],['验证失败',d.auth_failed||0,'错误账号或密码'],['未验证请求',d.auth_required||0,'尚无有效服务会话'],['验证速率限制',d.auth_limited||0,'短时过多验证尝试'],['防火墙拦截',d.firewall_blocked||0,'IP / WAF / HTTP 规则 / 限速'],['冻结 IP 拦截',d.frozen_blocked||0,'包含触发冻结的请求'],['新增自动冻结',d.freeze_created||0,'所选时间内的触发次数'],['当前冻结 IP',d.active_freezes||0,'实时值，不受时间范围限制']];
  return (d.freeze_write_error?'<div class="hint amber" role="alert">冻结记录保存失败，最新变更可能无法在重启后恢复。请检查磁盘与数据目录权限。</div>':'')+
    panel('账号验证与安全防护','验证指标仅计算 Gatehouse 独立服务账号；后端返回 401 不会算作账号验证失败。',`<div class="security-metrics panel-body">${metrics.map(([label,value,note])=>`<div class="stat"><span class="stat-label">${label}</span><div class="stat-value">${value.toLocaleString()}<small>${label==='当前冻结 IP'?'个':'次'}</small></div><span class="stat-foot">${note}</span></div>`).join('')}</div>`,`<a class="text-button" href="#ip-blocks">${icon('shield')}管理冻结 IP</a>`)+
    securityEventsHTML('security');
}

const statisticsColors={normal:'#22825b',blocked:'#b03939',failed:'#9d6f1e'};
function statisticsLegend() {return '<div class="chart-legend">'+[['normal','正常访问'],['blocked','安全拦截'],['failed','其他失败']].map(([key,name])=>`<span><i class="${key}"></i>${name}</span>`).join('')+'</div>';}
function trendChart(data, compact = false) {
  const rows=data.trend||[],maximum=Math.max(2,...rows.flatMap(r=>[r.normal,r.blocked,r.failed]));
  const left=compact?38:54,span=compact?342:720,width=compact?400:800,font=compact?14:11;
  const x=i=>left+i*span/Math.max(1,rows.length-1),y=n=>192-n*160/maximum;
  const grid=[0,.5,1].map(r=>`<line x1="${left}" x2="${left+span}" y1="${y(maximum*r)}" y2="${y(maximum*r)}" stroke="#e5eaf0"/><text x="${left-10}" y="${y(maximum*r)+4}" text-anchor="end" fill="#667085" font-size="${font}">${Math.round(maximum*r)}</text>`).join('');
  const labels=[...new Set(compact?[0,rows.length-1]:[0,Math.floor(rows.length/3),Math.floor(rows.length*2/3),rows.length-1])].filter(i=>i>=0&&rows[i]).map(i=>`<text x="${x(i)}" y="218" text-anchor="${i===0?'start':i===rows.length-1?'end':'middle'}" fill="#667085" font-size="${font}">${esc(new Date(rows[i].time).toLocaleString('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit'}))}</text>`).join('');
  const lines=Object.keys(statisticsColors).map(key=>`<polyline points="${rows.map((r,i)=>x(i)+','+y(r[key])).join(' ')}" fill="none" stroke="${statisticsColors[key]}" stroke-width="2.5"/>${rows.map((r,i)=>`<circle cx="${x(i)}" cy="${y(r[key])}" r="3" fill="${statisticsColors[key]}"><title>${esc(date(r.time))}：${r[key]} 次</title></circle>`).join('')}`).join('');
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} 240" role="img" aria-label="访问趋势"><title>访问趋势，${data.total} 次请求</title>${grid}${labels}${lines}</svg>`;
}
function statusChart(data, compact = false) {
  const rows=data.statuses||[],maximum=Math.max(1,...rows.map(r=>r.count));
  const width=compact?400:800,step=compact?60:120,bar=compact?32:66,font=compact?14:12;
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} 230" role="img" aria-label="HTTP 状态码分布"><title>HTTP 状态码分布</title><line x1="35" x2="${width-25}" y1="188" y2="188" stroke="#e5eaf0"/>${rows.map((r,i)=>{const x=(compact?30:60)+i*step,height=r.count*145/maximum;return `<rect x="${x}" y="${188-height}" width="${bar}" height="${height}" rx="4" fill="${r.label==='4xx'||r.label==='5xx'?statisticsColors.failed:statisticsColors.normal}"><title>${esc(r.label)}：${r.count} 次</title></rect><text x="${x+bar/2}" y="${178-height}" text-anchor="middle" fill="#53617e" font-size="${font}">${r.count}</text><text x="${x+bar/2}" y="212" text-anchor="middle" fill="#667085" font-size="${font}">${esc(r.label)}</text>`;}).join('')}</svg>`;
}
function statisticsRuleName(key) {
  const r=config.routes.find(r=>r.group_id+'/'+r.host===key);
  return r?groupName(r.group_id)+' / '+(r.name||r.host):key||'未匹配代理规则';
}
function rulesChart(data, compact = false) {
  const rows=(data.rules||[]).slice(0,8),rowHeight=compact?70:58,height=Math.max(130,rows.length*rowHeight+20),maximum=Math.max(1,...rows.map(r=>r.total));
  const canvas=compact?400:800,start=compact?12:260,span=compact?330:460,font=compact?14:12;
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${canvas} ${height}" role="img" aria-label="反代规则访问排行"><title>反代规则访问排行，前 8 条</title>${rows.map((r,i)=>{let x=start;const segments=Object.keys(statisticsColors).map(key=>{const width=r[key]*span/maximum,rect=`<rect x="${x}" y="${i*rowHeight+(compact?32:12)}" width="${width}" height="24" fill="${statisticsColors[key]}"><title>${r[key]} 次</title></rect>`;x+=width;return rect;}).join('');const name=statisticsRuleName(r.rule),limit=compact?24:18;return `<text x="12" y="${i*rowHeight+(compact?20:29)}" fill="#53617e" font-size="${font}"><title>${esc(name)}</title>${esc(name.length>limit?name.slice(0,limit-1)+'…':name)}</text>${segments}<text x="${x+8}" y="${i*rowHeight+(compact?49:29)}" fill="#53617e" font-size="${font}">${r.total}</text>`;}).join('')}${rows.length?'':`<text x="${canvas/2}" y="70" text-anchor="middle" fill="#667085" font-size="14">暂无访问记录</text>`}</svg>`;
}
function statisticsHTML() {
  const d=statisticsView;
  const filters=`<div class="statistics-filters"><label>统计时间<select id="statistics-hours">${[['24','最近 24 小时'],['168','最近 7 天'],['720','最近 30 天']].map(([value,name])=>`<option value="${value}" ${statisticsHours===value?'selected':''}>${name}</option>`).join('')}</select></label><label>反代规则<select id="statistics-rule"><option value="">全部反代规则</option>${config.routes.map(r=>{const key=r.group_id+'/'+r.host;return `<option value="${esc(key)}" ${statisticsRule===key?'selected':''}>${esc(statisticsRuleName(key))} · ${esc(r.host)}</option>`;}).join('')}</select></label></div>`;
  const headingHTML=heading('ACCESS STATISTICS','访问统计','查看反代流量、独立账号验证与安全拦截，按时间和规则分析。','<button class="secondary" data-action="refresh-statistics" '+(statisticsLoading?'aria-busy="true" disabled':'')+'>刷新统计</button><button class="primary" data-action="export-statistics" '+(!d||statisticsLoading||statisticsError?'disabled':'')+'>导出图表 SVG</button>');
  if(!d) return headingHTML+filters+panel('访问统计','',empty(statisticsError?'统计加载失败':'正在加载统计',statisticsError||'统计当前保留的反代日志。'));
  return headingHTML+filters+(statisticsError?`<div class="hint amber" role="alert">更新失败：${esc(statisticsError)}。当前显示上次统计，请刷新后重试。</div>`:'')+`<div class="hint">基于当前保留的日志（最多 ${5000} 条项目与反代日志合计），轮转或清理后的记录不包含在统计中。最早保留记录：${date(d.retained_from)}。<br>统计范围：${date(d.from)} — ${date(d.to)}；正常为 HTTP 1xx–3xx，安全拦截为防火墙拒绝与冻结 IP 拒绝；其他失败包含未验证请求、错误账号和后端错误。验证与冻结指标是请求总数的子集，不能相加。TLS 握手失败不产生 HTTP 访问记录，WebSocket 在连接结束后计入。</div>`+
    (d.write_error?'<div class="hint amber">日志写入失败，请检查数据目录；当前统计来自内存记录。</div>':'')+
    `<div class="stats">${[['请求总数',d.total,'全部访问'],['正常访问',d.normal,'HTTP 1xx–3xx'],['安全拦截',d.blocked,'拦截率 '+(d.total?(d.blocked*100/d.total).toFixed(1):'0.0')+'%'],['其他失败',d.failed,'验证失败、后端错误等']].map(([name,value,note])=>`<div class="stat"><span class="stat-label">${name}</span><div class="stat-value">${value.toLocaleString()}<small>次</small></div><span class="stat-foot">${note}</span></div>`).join('')}</div>`+
    panel('访问趋势',new Date(d.to)-new Date(d.from)<=86400000?'每小时请求次数。':'每天请求次数。',`<div class="chart-body">${statisticsLegend()}${trendChart(d,compactCharts.matches)}</div>`)+
    `<div class="two-col">`+panel('HTTP 状态码分布','显示 HTTP 返回状态码。',`<div class="chart-body">${statusChart(d,compactCharts.matches)}</div>`)+
    panel('反代规则排行','按请求次数排序，显示前 8 条；已删除规则的日志保留原标识。',`<div class="chart-body">${statisticsLegend()}${rulesChart(d,compactCharts.matches)}</div>`)+`</div>`+securityStatisticsHTML(d);
}
async function loadStatistics() {
  const request=++statisticsRequest;
  statisticsLoading=true;statisticsError='';if(page==='statistics') render();
  try {
    const data=await api('statistics?'+new URLSearchParams({hours:statisticsHours,rule:statisticsRule}));
    if(request===statisticsRequest) statisticsView=data;
  } catch(error) {
    if(request===statisticsRequest) {statisticsError=error.message;throw error;}
  } finally {
    if(request===statisticsRequest) {statisticsLoading=false;if(page==='statistics') render();}
  }
}
function downloadStatistics() {
  if(!statisticsView) return;
  const d=statisticsView,legend=[['正常访问',statisticsColors.normal],['安全拦截',statisticsColors.blocked],['其他失败',statisticsColors.failed]].map(([name,color],i)=>`<rect x="${40+i*180}" y="125" width="12" height="12" fill="${color}"/><text x="${60+i*180}" y="136" font-size="13" fill="#53617e">${name}</text>`).join('');
  const data=`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1000 1210"><rect width="1000" height="1210" fill="white"/><g font-family="sans-serif"><text x="40" y="40" font-size="24" fill="#1a283c">Gatehouse 访问统计</text><text x="40" y="68" font-size="13" fill="#667085">${esc(date(d.from)+' — '+date(d.to))} · ${esc(statisticsRule?statisticsRuleName(statisticsRule):'全部反代规则')}</text><text x="40" y="96" font-size="14" fill="#53617e">总请求 ${d.total} · 正常 ${d.normal} · 拦截 ${d.blocked} · 其他失败 ${d.failed}</text>${legend}<text x="40" y="176" font-size="16" fill="#1a283c">访问趋势</text><svg x="40" y="185" width="920" height="276">${trendChart(d)}</svg><text x="40" y="495" font-size="16" fill="#1a283c">HTTP 状态码分布</text><svg x="40" y="508" width="920" height="265">${statusChart(d)}</svg><text x="40" y="808" font-size="16" fill="#1a283c">反代规则排行（前 8 条）</text><svg x="40" y="820" width="920" height="250">${rulesChart(d)}</svg><text x="40" y="1100" font-size="12" fill="#667085">统计仅包含当前保留的日志；TLS 握手失败不计入 HTTP 请求。</text><text x="40" y="1132" font-size="13" fill="#53617e">独立验证：成功 ${d.auth_success||0} · 失败 ${d.auth_failed||0} · 未验证 ${d.auth_required||0} · 速率限制 ${d.auth_limited||0}</text><text x="40" y="1160" font-size="13" fill="#53617e">安全防护：防火墙 ${d.firewall_blocked||0} · 冻结拒绝 ${d.frozen_blocked||0} · 自动冻结 ${d.freeze_created||0} · 当前冻结 IP ${d.active_freezes||0}</text></g></svg>`;
  const url=URL.createObjectURL(new Blob([data],{type:'image/svg+xml;charset=utf-8'}));
  const link=document.createElement('a');link.href=url;link.download='gatehouse-statistics-'+new Date().toLocaleDateString('sv-SE')+'.svg';link.click();setTimeout(()=>URL.revokeObjectURL(url),1000);
}
function render() {
  if (!config) return;
  const content=$('#content'),changed=renderedPage!==page;
  if(changed&&page!=='routes') {dialogMotions.get($('#route-dialog'))?.cancel();$('#route-dialog').close();}
  const entryState=pageMotion?.playState==='running'?{opacity:getComputedStyle(content).opacity,transform:getComputedStyle(content).transform}:{opacity:.25,transform:'translateY(8px)'};
  const focused=content.contains(document.activeElement)?focusSelector(document.activeElement):'';
  const selection=document.activeElement instanceof HTMLInputElement&&document.activeElement.type==='text'?[document.activeElement.selectionStart,document.activeElement.selectionEnd]:null;
  const values=[...content.querySelectorAll('.stat-value')].map(el=>el.textContent);
  $('#breadcrumb').textContent='工作空间 / '+pages[page];
  $('#route-count').textContent=config.routes.length;
  document.querySelectorAll('nav [data-page]').forEach(b=>{b.classList.toggle('active',b.dataset.page===page);if(b.dataset.page===page) b.setAttribute('aria-current','page');else b.removeAttribute('aria-current');});
  let html;
  if (page==='overview') html=overviewHTML();
  if (page==='routes') html=groupsHTML();
  if (page==='ddns') html=ddnsHTML();
  if (page==='certificates') html=certificatesHTML();
  if (page==='access') html=firewallsHTML();
  if (page==='firewall-logs') html=firewallLogsHTML();
  if (page==='subscriptions') html=subscriptionsHTML();
  if (page==='settings') html=settingsHTML();
  if (page==='logs') html=logsHTML();
  if (page==='statistics') html=statisticsHTML();
  if (page==='ip-blocks') html=ipBlocksHTML();
  content.innerHTML=html;
  content.dataset.page=page;
  content.querySelectorAll('.panel-heading>.group-actions').forEach(actions=>{
    const state=actions.querySelector('.badge,[data-ddns-status]');if(state) {const title=actions.previousElementSibling.querySelector('h2');title.classList.add('title-status');title.append(state);}
  });
  prepareTables(content);
  hydrateIcons(content);
  content.querySelectorAll('.log-tabs button').forEach(button=>button.setAttribute('aria-pressed',String(button.classList.contains('primary'))));

  if(changed) {
    pageMotion?.cancel();
    setNavigation(false,false);
    window.scrollTo({top:0,behavior:'instant'});
    if(renderedPage) $('.page-heading h1',content)?.focus({preventScroll:true});
    if(!reducedMotion.matches) pageMotion=content.animate([entryState,{opacity:1,transform:'none'}],{duration:240,easing:'cubic-bezier(.2,.8,.2,1)'});
  } else {
    const target=focused?$(focused,content):null;
    target?.focus({preventScroll:true});
    if(selection&&target instanceof HTMLInputElement) target.setSelectionRange(...selection);
    if(!reducedMotion.matches) content.querySelectorAll('.stat-value').forEach((el,i)=>{if(values[i]!==el.textContent) el.animate([{opacity:.4},{opacity:1}],{duration:160});});
  }
  renderedPage=page;
  if(changed&&page==='settings') loadOnlineUpdateStatus();
  if(changed&&page==='overview') loadDashboard();
  if(page==='ddns') {loadIPInfo().catch(e=>{if($('#ip-results')) $('#ip-results').textContent=e.message;});loadDNSRecords();}
}
async function refreshStatus() {
  try {
    status=await api('status');
    $('#connection-label').textContent='已连接';
    $('.connection').classList.remove('offline');
    $('#restart-banner').hidden=!status.restart_required;
    $('#uptime').textContent='已运行 '+Math.floor(status.uptime_seconds/60)+' 分钟';
  } catch(error) { $('#connection-label').textContent='连接中断'; $('.connection').classList.add('offline'); throw error; }
}
async function load() {
  applyConfig(await api('config'));
  await refreshStatus();
  $('#login').hidden=true; $('#app').hidden=false;
  page=pages[location.hash.slice(1)]?location.hash.slice(1):'overview';
  render();
  if(page==='logs') await loadLogs();
  if(page==='statistics') await Promise.all([loadStatistics(),loadSecurityEvents('security')]);
  if(page==='firewall-logs') await loadSecurityEvents('firewall');
	if(page==='ip-blocks') await loadIPBlocks();
}
function proxyHost(group,value,full=false) {
  value=value.trim().toLowerCase();
  if(!value||!group?.domain_suffix||full) return value;
  const suffix=group.domain_suffix;
  return value==='@'||value===suffix?suffix:value.endsWith('.'+suffix)?value:value+'.'+suffix;
}
function proxyPrefix(group,host) {
  const suffix=group?.domain_suffix;
  return !suffix?host:host===suffix?'@':host.endsWith('.'+suffix)?host.slice(0,-suffix.length-1):host;
}
function validateRouteHost(form, showError=false) {
  const host=form.elements.host;
  const message=host.value.trim()?'':$('[data-route-host-label]',form).textContent==='子域名前缀'?'请填写子域名前缀，如 nas；@ 表示根域名。':'请填写访问域名，如 nas.example.com。';
  host.setCustomValidity(message);
  const error=$('#route-host-error',form);error.textContent=showError?message:'';error.hidden=!error.textContent;
  if(error.hidden) host.removeAttribute('aria-invalid');else host.setAttribute('aria-invalid','true');
  return !message;
}
function updateRouteDomain(form) {
  const group=config.groups.find(g=>g.id===form.elements.group_id.value),full=form.dataset.fullHost==='true';
  $('[data-route-host-label]',form).textContent=group?.domain_suffix&&!full?'子域名前缀':'访问域名';
  form.elements.host.placeholder=group?.domain_suffix&&!full?'例如：nas；@ 表示根域名':'nas.example.com';
  $('[data-route-host-preview]',form).textContent=proxyHost(group,form.elements.host.value,full)||'填写域名后显示完整地址';
  const dns=config.ddns.groups.find(g=>g.id===group?.ddns_group_id);
  $('[data-route-dns-note]',form).textContent=dns?'保存后加入 DDNS 组「'+dns.name+'」，'+(dns.enabled?'按 '+ddnsMode(dns.mode)+' 同步。':'该组已停用，启用后同步。'):(group?.domain_suffix?'自动补全后缀：'+group.domain_suffix:'此组填写完整域名。');
  validateRouteHost(form,form.elements.host.getAttribute('aria-invalid')==='true');
}
function upstreamParts(value) {
  const match=String(value||'').trim().match(/^(https?):\/\/(.*)$/i);
  return match?{scheme:match[1].toLowerCase(),address:match[2].replace(/\/$/,'')}:{scheme:'http',address:String(value||'').trim()};
}
function upstreamURL(scheme,address) {
  address=address.trim();
  if(!['http','https'].includes(scheme)||!address||address.includes('://')) throw new Error('请选择 HTTP 或 HTTPS，主机地址中只填写主机 / IP 与端口');
  return scheme+'://'+address;
}
function discoveredRoutesConfig(original,groupID,rows,tls,firewallID) {
  const group=original.groups.find(g=>g.id===groupID);
  if(!group) throw new Error('所属反代组已不存在，请重新选择');
  if(!rows.length) throw new Error('请至少选择一个服务');
  if(original.routes.length+rows.length>100) throw new Error('代理规则最多 100 条，请减少所选服务');
  const next=clone(original),seen=new Set(original.routes.filter(r=>r.group_id===groupID).map(r=>r.host));
  for(const row of rows) {
    const host=proxyHost(group,row.host),name=row.name.trim();
    if(!host||host.length>253||!host.includes('.')||!host.split('.').every(s=>/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(s))) throw new Error('请为每个所选服务填写有效的访问域名');
    if(seen.has(host)) throw new Error('访问域名重复：'+host);
    if(!name||new TextEncoder().encode(name).length>100) throw new Error('服务名称须为 1–100 字节');
    if(row.tls_untrusted) throw new Error('该 HTTPS 后端证书未受信任，请改用 HTTP 端口或配置可信证书后重新扫描');
    seen.add(host);
    next.routes.push({group_id:groupID,name,host,upstream:row.upstream,image:row.use_image?row.image_id||'':'',tls,enabled:true,firewall_id:firewallID,auth:{enabled:false,username:'',failure_limit:5,freeze_seconds:3600}});
  }
  return next;
}
function discoveryRowHTML(row) {
  const key=esc(row.port);
  const image=row.icon?.startsWith('/api/service-discovery/')?`<img src="${esc(row.icon)}" alt="" loading="lazy" data-discovery-icon>`:'';
  return `<article class="discovery-service" data-service-port="${key}"><div class="discovery-service-heading"><span class="discovery-service-icon">${icon('server')}${image}</span><div><b>${esc(row.name)}</b><small class="mono">${esc(row.upstream)}</small><small>${badge(row.scheme.toUpperCase(),'gray')} 响应 ${esc(row.status)}${row.redirect_other?' · 重定向到其他地址，未跟随':''}</small></div><label class="discovery-choice"><input id="discovery-select-${key}" type="checkbox" data-discovery-field="selected" data-port="${key}" ${row.tls_untrusted?'disabled':''}>选择<span class="sr-only">端口 ${key}</span></label></div>${row.tls_untrusted?'<p class="hint amber">后端 HTTPS 证书未受信任：已识别服务信息，创建前请改用 HTTP 端口或配置可信证书后重新扫描。</p>':''}<div class="form-grid"><label>服务名称<input id="discovery-name-${key}" data-discovery-field="name" data-port="${key}" value="${esc(row.name)}" maxlength="100" disabled></label><label><span data-discovery-host-label>访问域名</span><input id="discovery-host-${key}" data-discovery-field="host" data-port="${key}" maxlength="253" aria-describedby="discovery-host-error-${key}" disabled><small id="discovery-host-error-${key}" class="field-error" role="alert" hidden></small></label><small class="discovery-domain-preview mono span-2" data-discovery-domain>选择后填写域名</small>${image?`<label class="check-label span-2"><input type="checkbox" data-discovery-field="use_image" data-port="${key}" ${row.use_image?'checked':''} disabled>采用应用图标</label>`:''}</div></article>`;
}
function abandonDiscovery() {
  ++discovery.request;clearTimeout(discovery.timer);discovery.timer=null;
  const scan=discovery.scan;discovery.scan=null;discovery.pending=false;
  if(scan&&['running','stopping'].includes(scan.state)) api('service-discovery/'+scan.id+'/cancel','POST',{}).catch(()=>{});
}
function openDiscovery(groupID='') {
  if(busy||!config.groups.length) return;
  abandonDiscovery();discovery.rows.clear();
  const form=$('#discovery-form');form.reset();
  form.elements.group_id.innerHTML=config.groups.map(g=>`<option value="${esc(g.id)}">${esc(g.name)}</option>`).join('');form.elements.group_id.value=groupID||config.groups[0].id;
  form.elements.firewall_id.innerHTML='<option value="">无防火墙</option>'+config.firewalls.map(f=>`<option value="${esc(f.id)}">${esc(f.name)}</option>`).join('');
  $('#discovery-custom-ports').hidden=true;form.elements.ports.disabled=true;form.elements.ports.required=false;
  $('#discovery-results').innerHTML=empty('等待发现服务','输入设备 IP，开始扫描后在这里选择 HTTP / HTTPS 服务。');
  $('#discovery-scan-error').textContent='';$('#discovery-create-error').textContent='';
  updateDiscoveryGroup(true);renderDiscoveryProgress();openDialog($('#discovery-dialog'));
}
function updateDiscoveryGroup(resetProtocol=false) {
  const form=$('#discovery-form'),group=config.groups.find(g=>g.id===form.elements.group_id.value);
  if(resetProtocol) form.elements.tls.value=group?.https_port?'true':'false';
  const dns=config.ddns.groups.find(d=>d.id===group?.ddns_group_id);
  $('#discovery-dns-note').textContent=dns?'新域名随 DDNS 组「'+dns.name+'」同步。':group?.domain_suffix?'自动补全域名后缀：'+group.domain_suffix:'此组没有域名后缀，请为所选服务填写完整域名。';
  document.querySelectorAll('[data-service-port]').forEach(el=>{
    $('[data-discovery-host-label]',el).textContent=group?.domain_suffix?'子域名前缀':'访问域名';
    $('[data-discovery-field="host"]',el).placeholder=group?.domain_suffix?'例如：nas；@ 表示根域名':'nas.example.com';
  });
  validateDiscoveryRows(false);
}
function updateDiscoveryInput(input) {
  const row=discovery.rows.get(Number(input.dataset.port));if(!row) return;
  row[input.dataset.discoveryField]=input.type==='checkbox'?input.checked:input.value;
  const article=input.closest('[data-service-port]');article.classList.toggle('is-selected',!!row.selected);
  for(const field of article.querySelectorAll('input:not([type="checkbox"])')) {field.disabled=!row.selected;field.required=!!row.selected;}
  const imageChoice=$('[data-discovery-field="use_image"]',article);if(imageChoice) imageChoice.disabled=!row.selected;
  validateDiscoveryRows(false);renderDiscoveryProgress();
}
function validateDiscoveryRows(showErrors) {
  const form=$('#discovery-form'),group=config?.groups.find(g=>g.id===form.elements.group_id.value);
  const selected=[...discovery.rows.values()].filter(r=>r.selected),hosts=selected.map(r=>proxyHost(group,r.host));
  let valid=true;
  for(const row of discovery.rows.values()) {
    const host=proxyHost(group,row.host),field=$('#discovery-host-'+row.port),error=$('#discovery-host-error-'+row.port);
    let message='';
    if(row.selected) {
      if(!host) message=group?.domain_suffix?'请填写子域名前缀':'请填写完整访问域名';
      else if(host.length>253||!host.includes('.')||!host.split('.').every(s=>/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(s))) message='访问域名格式无效';
      else if(hosts.filter(h=>h===host).length>1||config.routes.some(r=>r.group_id===group?.id&&r.host===host)) message='此访问域名在组内重复';
    }
    field.setCustomValidity(message);
    const visible=!!message&&(showErrors||field.getAttribute('aria-invalid')==='true');
    error.textContent=visible?message:'';error.hidden=!visible;
    if(visible) field.setAttribute('aria-invalid','true');else field.removeAttribute('aria-invalid');
    $('[data-discovery-domain]',field.closest('article')).textContent=host?'完整域名：'+host:row.selected?'填写后显示完整域名':'选择后填写域名';
    if(message) valid=false;
  }
  if(showErrors) {
    if(!selected.length) {$('#discovery-create-error').textContent='请至少选择一个服务';$('#discovery-create-error').focus();return false;}
    if(!form.reportValidity()) return false;
  }
  return valid;
}
function renderDiscoveryProgress() {
  const scan=discovery.scan,active=scan&&['running','stopping'].includes(scan.state),count=[...discovery.rows.values()].filter(r=>r.selected).length;
  $('#discovery-start').disabled=!!active||discovery.pending;$('#discovery-stop').disabled=!active||scan?.state==='stopping';
  $('#discovery-form').elements.ip.disabled=!!active||discovery.pending;$('#discovery-port-mode').disabled=!!active||discovery.pending;
  $('#discovery-form').elements.ports.disabled=!!active||discovery.pending||$('#discovery-port-mode').value!=='custom';
  $('#discovery-progress').max=scan?.total||1;$('#discovery-progress').value=scan?.completed||0;
  const state={running:'扫描中',stopping:'正在停止',completed:'扫描完成',cancelled:'已停止，保留已发现服务',timed_out:'扫描超时，保留已发现服务',expired:'扫描结果已失效，请重新扫描'};
  $('#discovery-status').textContent=scan?scan.ip+' · '+(state[scan.state]||scan.state)+' · '+scan.completed+' / '+scan.total+' 个端口 · '+scan.open_tcp+' 个 TCP 端口开放 · '+scan.services.length+' 个 Web 服务'+(scan.truncated?'（已达到 100 个结果上限）':''):discovery.pending?'正在开始扫描…':'尚未扫描';
  $('#discovery-results').setAttribute('aria-busy',String(!!active));$('#discovery-create').disabled=!count||!!active||discovery.pending||busy||scan?.state==='expired';
  $('#discovery-create-label').textContent='创建所选规则'+(count?'（'+count+'）':'');
}
function receiveDiscovery(scan) {
  discovery.scan=scan;
  for(const service of scan.services) {
    if(discovery.rows.has(service.port)) continue;
    if(!discovery.rows.size) $('#discovery-results').innerHTML='<div class="discovery-service-list"></div>';
    const row={...service,selected:false,host:'',use_image:!!service.icon,image_id:''};discovery.rows.set(service.port,row);
    $('.discovery-service-list').insertAdjacentHTML('beforeend',discoveryRowHTML(row));
  }
  if(!discovery.rows.size&&!['running','stopping'].includes(scan.state)) $('#discovery-results').innerHTML=empty('没有识别到 Web 服务','确认服务器能连接此 IP，或填写自定义端口范围重试。开放的 TCP 端口不一定是 HTTP / HTTPS 服务。');
  updateDiscoveryGroup();renderDiscoveryProgress();
}
async function pollDiscovery(request) {
  if(request!==discovery.request||!$('#discovery-dialog').open||!discovery.scan) return;
  try {
    const scan=await api('service-discovery/'+discovery.scan.id);
    if(request!==discovery.request) return;
    receiveDiscovery(scan);$('#discovery-scan-error').textContent='';
  } catch(error) {
    if(request!==discovery.request) return;
    if(error.message==='扫描结果已失效，请重新扫描') {discovery.scan.state='expired';renderDiscoveryProgress();$('#discovery-scan-error').textContent=error.message;}
    else $('#discovery-scan-error').textContent=error.message+'。保留已发现的服务，正在重试。';
  }
  if(request===discovery.request&&['running','stopping'].includes(discovery.scan?.state)) discovery.timer=setTimeout(()=>pollDiscovery(request),1000);
}
async function startDiscovery() {
  if(discovery.pending||['running','stopping'].includes(discovery.scan?.state)) return;
  const form=$('#discovery-form');if(!form.elements.ip.reportValidity()||!form.elements.ports.reportValidity()) return;
  const request=++discovery.request;clearTimeout(discovery.timer);discovery.pending=true;renderDiscoveryProgress();$('#discovery-scan-error').textContent='';
  try {
    const scan=await api('service-discovery','POST',{ip:form.elements.ip.value.trim(),ports:form.elements.port_mode.value==='custom'?form.elements.ports.value.trim():''});
    if(request!==discovery.request) {api('service-discovery/'+scan.id+'/cancel','POST',{}).catch(()=>{});return;}
    discovery.rows.clear();$('#discovery-results').innerHTML=empty('正在发现服务','结果陆续显示，可停止扫描后创建已发现的服务。');$('#discovery-create-error').textContent='';
    receiveDiscovery(scan);discovery.timer=setTimeout(()=>pollDiscovery(request),500);
  } catch(error) {
    if(request===discovery.request) {$('#discovery-scan-error').textContent=error.message;$('#discovery-scan-error').focus();}
  } finally {
    if(request===discovery.request) {discovery.pending=false;renderDiscoveryProgress();}
  }
}
async function stopDiscovery() {
  if(!discovery.scan) return;
  const request=discovery.request;
  try {const scan=await api('service-discovery/'+discovery.scan.id+'/cancel','POST',{});if(request===discovery.request) receiveDiscovery(scan);}
  catch(error) {if(request===discovery.request) {$('#discovery-scan-error').textContent=error.message;$('#discovery-scan-error').focus();}}
}
function routeImageHTML(id) {
  return /^[a-f0-9]{64}$/.test(id||'')?`<img src="/api/route-images/${id}" alt="" loading="lazy" data-route-image>`:'';
}
function renderRouteImage() {
  const form=$('#route-form');
  $('#route-image-preview').innerHTML=icon('server')+routeImageHTML(form.elements.image.value);
  $('#route-image-status').textContent=routeImagePending?'正在处理图片…':form.elements.image.value?'已选择图片，保存规则后生效':'使用默认图标';
  for(const control of form.querySelectorAll('[data-image-control],button[type="submit"]')) control.disabled=routeImagePending||busy;
}
async function loadRouteImage(kind) {
  if(routeImagePending) return false;
  const form=$('#route-form'),request=++routeImageRequest,error=$('#route-image-error');
  let data,path;
  if(kind==='file') {
    const file=form.elements.image_file.files[0];if(!file) return false;
    if(file.size>5*1024*1024) {error.textContent='图片须小于等于 5 MiB';form.elements.image_file.value='';return false;}
    data=new FormData();data.append('file',file);path='route-images/upload';
  } else {
    const url=form.elements.image_url.value.trim();
    if(!url) {error.textContent='请填写图片 URL';return false;}
    data={url};path='route-images/import';
  }
  routeImagePending=true;error.textContent='';renderRouteImage();
  try {
    const result=await api(path,'POST',data);
    if(request!==routeImageRequest||!$('#route-dialog').open) return false;
    form.elements.image.value=result.id;form.elements.image_url.value='';form.elements.image_file.value='';form.dataset.dirty='true';
    return true;
  } catch(e) {
    if(request===routeImageRequest&&$('#route-dialog').open) {error.textContent=e.message;error.focus();}
    return false;
  } finally {
    if(request===routeImageRequest) {routeImagePending=false;renderRouteImage();}
  }
}
function clearRouteImage() {
  ++routeImageRequest;routeImagePending=false;
  const form=$('#route-form');form.elements.image.value='';form.elements.image_url.value='';form.elements.image_file.value='';form.dataset.dirty='true';
  $('#route-image-error').textContent='';renderRouteImage();
}
function openRoute(index = -1, groupID = '') {
  if(busy) return;
  if(!config.groups.length) return openGroup();
  const dialog=$('#route-dialog'),form=$('#route-form');
  if(dialog.open&&form.dataset.dirty==='true') {if(Number(form.elements.index.value)===index&&index>=0) return;if(!confirm('放弃尚未保存的服务修改？')) return;}
  form.reset();form.dataset.dirty='false';form.elements.host.removeAttribute('aria-invalid');
  const r=index<0?{group_id:groupID||config.groups[0]?.id,name:'',host:'',upstream:'',tls:!!config.groups.find(g=>g.id===(groupID||config.groups[0]?.id))?.https_port,firewall_id:''}:config.routes[index];
  for (const key of ['name','host']) form.elements[key].value=r[key];
  const upstream=upstreamParts(r.upstream);form.elements.upstream_scheme.value=upstream.scheme;form.elements.upstream.value=upstream.address;
  ++routeImageRequest;routeImagePending=false;form.elements.image.value=r.image||'';$('#route-image-error').textContent='';renderRouteImage();
  form.elements.index.value=index;
  form.elements.group_id.innerHTML=config.groups.map(g=>`<option value="${esc(g.id)}">${esc(g.name)} · ${esc(portsText(g))}</option>`).join('');
  form.elements.group_id.value=r.group_id;
  const group=config.groups.find(g=>g.id===r.group_id);
  form.dataset.fullHost=String(!!r.host&&!!group?.domain_suffix&&r.host!==group.domain_suffix&&!r.host.endsWith('.'+group.domain_suffix));
  form.elements.host.value=proxyPrefix(group,r.host);updateRouteDomain(form);
  form.elements.tls.checked=r.tls;
  form.elements.firewall_id.innerHTML='<option value="">无防火墙</option>'+config.firewalls.map(f=>`<option value="${esc(f.id)}">${esc(f.name)}${firewallWaiting(f)?'（等待有效订阅）':''}</option>`).join('');
  form.elements.firewall_id.value=r.firewall_id||'';
  form.elements.failure_limit.value=r.auth?.failure_limit||5;form.elements.freeze_minutes.value=(r.auth?.freeze_seconds||3600)/60;
  form.elements.auth_enabled.checked=!!r.auth?.enabled;form.elements.auth_username.value=r.auth?.username||'';
  form.elements.auth_password.placeholder=routePasswordsConfigured[r.group_id+'/'+r.host]?'已配置，留空保留访问密码':'首次开启时设置访问密码';
  $('#dialog-title').textContent=index<0?'添加代理服务':r.name||r.host;
  $('[data-editor-domain]',form).textContent=r.host||'基础配置与访问控制';
  $('#route-error').textContent=''; openDialog($('#route-dialog'));
}
function firewallGroupFields(g = {name:'',match:'include',action:'allow',cidrs:[],subscriptions:[]}) {
  return `<fieldset class="firewall-group"><legend>IP 组 <span class="rule-number"></span></legend><div class="rule-actions"><button type="button" class="text-button" data-action="move-firewall-group-up">上移</button><button type="button" class="text-button" data-action="move-firewall-group-down">下移</button><button type="button" class="text-button danger" data-action="remove-firewall-group">移除组</button></div><div class="form-grid"><label class="span-2">IP 组名称<input name="group_name" value="${esc(g.name)}" placeholder="例如：内网 / 中国 IP / 例外地址" maxlength="100" required></label><label>匹配条件<select name="match"><option value="include" ${g.match==='include'?'selected':''}>包含（IP 在组内）</option><option value="exclude" ${g.match==='exclude'?'selected':''}>排除（IP 不在组内）</option></select></label><label>命中时<select name="action"><option value="allow" ${g.action==='allow'?'selected':''}>允许访问</option><option value="deny" ${g.action==='deny'?'selected':''}>禁止访问</option></select></label><label class="span-2">手动 IP / CIDR<textarea name="cidrs" rows="2" placeholder="每行一个 IP 或 CIDR">${esc((g.cidrs||[]).join('\n'))}</textarea></label><div class="span-2"><b class="field-title">引用订阅（可多选）</b><div class="subscription-choices">${subscriptionChoices(g.subscriptions||[])}</div></div></div></fieldset>`;
}
function numberFirewallGroups() {
  const groups=[...document.querySelectorAll('#firewall-groups .firewall-group')];
  groups.forEach((g,i)=>{ $('.rule-number',g).textContent=i+1; $('[data-action="move-firewall-group-up"]',g).disabled=i===0; $('[data-action="move-firewall-group-down"]',g).disabled=i===groups.length-1; });
  hydrateIcons($('#firewall-groups'));
}
function httpRuleFields(r={name:'',target:'user_agent',pattern:'',action:'block'}) {
  return `<fieldset class="protection-rule" data-http-rule><legend>HTTP 规则</legend><button type="button" class="text-button danger" data-action="remove-protection-rule">${icon('trash')}移除</button><div class="form-grid"><label class="span-2">规则名称<input name="http_name" value="${esc(r.name)}" maxlength="100" required></label><label>检查目标<select name="http_target">${[['user_agent','User-Agent'],['path','请求路径'],['method','请求方法']].map(([v,n])=>`<option value="${v}" ${r.target===v?'selected':''}>${n}</option>`).join('')}</select></label><label>动作<select name="http_action"><option value="block" ${r.action==='block'?'selected':''}>拦截</option><option value="detect" ${r.action==='detect'?'selected':''}>仅记录</option></select></label><label class="span-2">正则表达式<input name="http_pattern" value="${esc(r.pattern)}" placeholder="例如：(?i)(crawler|scrapy)" maxlength="256" required></label></div></fieldset>`;
}
function wafExceptionFields(e={rule_id:'',host:'',path:'/',parameter:''}) {
  return `<fieldset class="protection-rule" data-waf-exception><legend>CRS 规则例外</legend><button type="button" class="text-button danger" data-action="remove-protection-rule">${icon('trash')}移除</button><div class="form-grid"><label>CRS 规则编号<input name="exception_id" type="number" min="911000" max="948999" value="${esc(e.rule_id)}" placeholder="例如：941100" required></label><label>服务域名（可选）<input name="exception_host" value="${esc(e.host)}" placeholder="app.example.com"></label><label>路径前缀<input name="exception_path" value="${esc(e.path)}" maxlength="256" required></label><label>参数名（可选）<input name="exception_parameter" value="${esc(e.parameter)}" maxlength="100" placeholder="例如：content 或 json.content"></label></div></fieldset>`;
}
function readProtection(form) {
  const e=form.elements;
  return {waf:{mode:e.waf_mode.value,level:Number(e.waf_level.value),body_limit:Number(e.waf_body_kib.value)*1024,exceptions:[...form.querySelectorAll('[data-waf-exception]')].map(row=>({rule_id:Number($('[name="exception_id"]',row).value),host:$('[name="exception_host"]',row).value.trim().toLowerCase(),path:$('[name="exception_path"]',row).value.trim(),parameter:$('[name="exception_parameter"]',row).value.trim()}))},rate:{enabled:e.rate_enabled.checked,requests:Number(e.rate_requests.value),window_seconds:Number(e.rate_window.value),path:e.rate_path.value.trim()},freeze:{enabled:e.security_freeze_enabled.checked,failures:Number(e.security_freeze_failures.value),window_seconds:Number(e.security_freeze_window.value),seconds:Number(e.security_freeze_minutes.value)*60},rules:[...form.querySelectorAll('[data-http-rule]')].map(row=>({name:$('[name="http_name"]',row).value.trim(),target:$('[name="http_target"]',row).value,pattern:$('[name="http_pattern"]',row).value,action:$('[name="http_action"]',row).value}))};
}
function fillProtection(form,p={}) {
  const e=form.elements,w=p.waf||{},r=p.rate||{},f=p.freeze||{};
  const values={waf_mode:w.mode||'off',waf_level:w.level||1,waf_body_kib:(w.body_limit||1048576)/1024,rate_requests:r.requests||120,rate_window:r.window_seconds||60,rate_path:r.path||'/',security_freeze_failures:f.failures||5,security_freeze_window:f.window_seconds||300,security_freeze_minutes:(f.seconds||3600)/60};
  Object.entries(values).forEach(([key,value])=>e[key].value=value);e.rate_enabled.checked=!!r.enabled;e.security_freeze_enabled.checked=!!f.enabled;
  $('#http-rules').innerHTML=(p.rules||[]).map(httpRuleFields).join('');$('#waf-exceptions').innerHTML=(w.exceptions||[]).map(wafExceptionFields).join('');
}
function protectionSummary(f) {
  const p=f.protection||{},mode=({detect:'仅记录',block:'拦截'})[p.waf?.mode]||'关闭';
  return `<div class="protection-summary">${badge('WAF · '+mode,p.waf?.mode==='block'?'':p.waf?.mode==='detect'?'amber':'gray')}${badge(p.rate?.enabled?'访问限速已开启':'访问限速关闭','gray')}${badge((p.rules||[]).length+' 条 HTTP 规则','gray')}${badge(p.freeze?.enabled?'防护自动冻结已开启':'防护自动冻结关闭','gray')}</div>`;
}
function openFirewall(index = -1) {
  const form=$('#firewall-form'); form.reset();
  const f=index<0?{name:'',default_action:'deny',groups:[]}:config.firewalls[index];
  form.elements.index.value=index; form.elements.name.value=f.name; form.elements.default_action.value=f.default_action;
  $('#firewall-groups').innerHTML=(f.groups||[]).map(firewallGroupFields).join(''); numberFirewallGroups();fillProtection(form,f.protection);
  $('.error',form).textContent=''; $('#firewall-title').textContent=index<0?'添加防火墙':'编辑防火墙'; openDialog($('#firewall-dialog'));
}
function openGroup(index = -1) {
  const form=$('#group-form'); form.reset();
  const used=new Set(config.groups.flatMap(g=>[g.http_port,g.https_port]));
  let http=18080; while(used.has(http)||used.has(http+363)||http===16666||http+363===16666) http+=1000;
  const g=index<0?{name:'',http_port:http,https_port:http+363,enabled:true}:config.groups[index];
  form.elements.index.value=index; form.elements.name.value=g.name;
  form.elements.domain_suffix.value=g.domain_suffix||'';
  form.elements.ddns_group_id.innerHTML='<option value="">不绑定 DDNS</option>'+config.ddns.groups.map(d=>`<option value="${esc(d.id)}">${esc(d.name)} · ${esc(d.zone)} · ${ddnsMode(d.mode)}${d.enabled?'':' · 已停用'}</option>`).join('');
  form.elements.ddns_group_id.value=g.ddns_group_id||'';
  form.elements.http_port.value=g.http_port; form.elements.https_port.value=g.https_port;
  form.elements.enabled.checked=g.enabled; $('.error',form).textContent='';
  $('#group-title').textContent=index<0?'添加反代组':'编辑反代组'; openDialog($('#group-dialog'));
}
function openSubscription(index = -1, family = '') {
  const form=$('#subscription-form'); form.reset();
  const preset=family?{name:'中国大陆 IPv'+family+'（mayaxcn）',url:'https://raw.githubusercontent.com/mayaxcn/china-ip-list/master/'+(family==='6'?'chnroute_v6.txt':'chnroute.txt'),interval:28800,enabled:true}:null;
  const d=index<0?(preset||{name:'',url:'',interval:86400,enabled:true}):config.subscriptions[index];
  form.elements.index.value=index;
  for(const key of ['name','url','interval']) form.elements[key].value=d[key];
  form.elements.enabled.checked=d.enabled; $('.error',form).textContent='';
  $('#subscription-title').textContent=index<0?'添加 IP 订阅':'编辑 IP 订阅'; openDialog($('#subscription-dialog'));
}
async function submitForm(form, work) {
  if (busy) return;
  busy=true; const buttons=form.querySelectorAll('button[type="submit"]'); buttons.forEach(b=>{b.disabled=true;b.setAttribute('aria-busy','true');});
  const error=$(form.id==='discovery-form'?'#discovery-create-error':'.error',form); if (error) error.textContent='';
  try { await work(); } catch(e) { if(error) {error.textContent=e.message;error.tabIndex=-1;error.focus();} else toast(e.message); }
  finally { busy=false; buttons.forEach(b=>{b.disabled=false;b.removeAttribute('aria-busy');}); if(form.id==='discovery-form') renderDiscoveryProgress();if(form.id==='route-form') renderRouteImage(); }
}
document.addEventListener('submit',event=>{
  event.preventDefault(); const form=event.target;
  if(form.id==='login-form') return submitForm(form,async()=>{ const password=form.elements.password.value; form.elements.password.value=''; await api('login','POST',{password}); await load(); });
  if(!config) return;
  if(form.id==='dashboard-form') return submitForm(form,saveDashboardSettings);
  if(form.dataset.eventFilters) {
    const kind=form.dataset.eventFilters;securityEvents[kind].filters={...emptyEventFilters(),...Object.fromEntries(new FormData(form))};
    return loadSecurityEvents(kind).catch(e=>toast(e.message));
  }
  submitForm(form,async()=>{
    const next=clone(config), e=form.elements;
    if(form.id==='discovery-form') {
      if(!validateDiscoveryRows(true)) return;
      const rows=[...discovery.rows.values()].filter(r=>r.selected);
      const request=discovery.request;
      discoveredRoutesConfig(config,e.group_id.value,rows,e.tls.value==='true',e.firewall_id.value);
      for(const row of rows) if(row.use_image&&row.icon&&!row.image_id) {
        const image=await api('route-images/import','POST',{scan_id:discovery.scan.id,port:row.port});
        if(request!==discovery.request||!$('#discovery-dialog').open) return;row.image_id=image.id;
      }
      if(request!==discovery.request||!$('#discovery-dialog').open) return;
      const next=discoveredRoutesConfig(config,e.group_id.value,rows,e.tls.value==='true',e.firewall_id.value);
      await save(next);collapsedGroups.delete('proxy:'+e.group_id.value);closeDialog($('#discovery-dialog'));render();toast('已创建 '+rows.length+' 条代理规则');
    } else if(form.id==='backup-form') {
      const password=e.password.value;if(password!==e.confirm_password.value) throw new Error('两次备份密码不一致');e.password.value='';e.confirm_password.value='';await downloadBackup(password);
    } else if(form.id==='restore-form'||form.id==='update-form') {
      await inspectMaintenance(form,form.id==='restore-form'?'restore':'update');
    } else if(form.id==='ip-block-form') {
	  ++ipBlockRequest;
      const result=await api('ip-blocks','POST',{rule:e.rule.value,ip:e.ip.value.trim(),minutes:Number(e.minutes.value),reason:e.reason.value.trim()});
      ipBlockView=result;ipBlockReceived=Date.now();ipBlockError='';ipBlockPage=1;closeDialog($('#ip-block-dialog'));render();toast(result.write_error?'已冻结，但记录保存失败，请检查磁盘和权限':'IP 已加入拦截名单');
    } else if(form.id==='route-form') {
      if(routeImagePending) throw new Error('请等待图片处理完成');
      if(e.image_url.value.trim()&&!await loadRouteImage('url')) return;
      if(!$('#route-dialog').open) return;
      const index=Number(e.index.value);
      const r={group_id:e.group_id.value,name:e.name.value.trim(),host:proxyHost(next.groups.find(g=>g.id===e.group_id.value),e.host.value,form.dataset.fullHost==='true'),upstream:upstreamURL(e.upstream_scheme.value,e.upstream.value),image:e.image.value,tls:e.tls.checked,enabled:index<0?true:next.routes[index].enabled,firewall_id:e.firewall_id.value,auth:{enabled:e.auth_enabled.checked,username:e.auth_username.value.trim(),failure_limit:Number(e.failure_limit.value),freeze_seconds:Number(e.freeze_minutes.value)*60}};
      if(index<0) next.routes.push(r); else next.routes[index]=r;
      const password=e.clear_auth_password.checked?'':e.auth_password.value||undefined;
      await save(next,undefined,undefined,undefined,password===undefined?undefined:{[r.group_id+'/'+r.host]:password});e.auth_password.value='';form.dataset.dirty='false';collapsedGroups.delete('proxy:'+r.group_id);closeDialog($('#route-dialog')); render();
    } else if(form.id==='firewall-form') {
      const index=Number(e.index.value), f={id:index<0?newID():next.firewalls[index].id,name:e.name.value.trim(),default_action:e.default_action.value,protection:readProtection(form),groups:[...form.querySelectorAll('.firewall-group')].map(g=>({name:$('[name="group_name"]',g).value.trim(),match:$('[name="match"]',g).value,action:$('[name="action"]',g).value,cidrs:$('[name="cidrs"]',g).value.split(/[\n,]+/).map(s=>s.trim()).filter(Boolean),subscriptions:[...g.querySelectorAll('[name="subscription"]:checked')].map(input=>input.value)}))};
      if(index<0) next.firewalls.push(f); else next.firewalls[index]=f;
      await save(next); closeDialog($('#firewall-dialog')); render();
    } else if(form.id==='ddns-group-form') {
      const index=Number(e.index.value),g={id:index<0?newID():next.ddns.groups[index].id,provider:e.provider.value,zone:e.zone.value.trim().toLowerCase(),name:e.name.value.trim(),mode:e.mode.value,hosts:ddnsHosts(e.zone.value,e.hosts.value),interval:Number(e.interval.value),enabled:e.enabled.checked,ipv4_urls:e.ipv4_urls.value.split('\n').map(s=>s.trim()).filter(Boolean),ipv6_urls:e.ipv6_urls.value.split('\n').map(s=>s.trim()).filter(Boolean),interface:e.interface.value,ipv4_source:e.ipv4_source.value,ipv6_source:e.ipv6_source.value};
      if(index<0) next.ddns.groups.push(g);else next.ddns.groups[index]=g;
      const token=e.clear_token.checked?'':e.token.value.trim()||undefined;const tokens=token===undefined?undefined:{[g.id]:token};
      await save(next,tokens);e.token.value='';closeDialog($('#ddns-dialog'));render();
    } else if(form.id==='ddns-hosts-form') {
      await save(ddnsHostsConfig(config,e.group_id.value,e.hosts.value));closeDialog($('#ddns-hosts-dialog'));render();
    } else if(form.id==='admin-access-form') {
      next.admin_access={enabled:e.enabled.checked,origins:e.origins.value.split('\n').map(s=>s.trim()).filter(Boolean)};
      await save(next);render();
    } else if(form.id==='outbound-form') {
      next.outbound_proxy={enabled:e.enabled.checked,url:e.url.value.trim(),username:e.username.value.trim()};
      const password=e.clear_password.checked?'':e.password.value||undefined;
      await save(next,undefined,password);e.password.value='';render();
    } else if(form.id==='log-retention-form') {
      next.log_retention={max_size_mb:Number(e.max_size_mb.value),keep_days:Number(e.keep_days.value)};
      await save(next);render();
    } else if(form.id==='cert-form') {
      next.acme={...next.acme,enabled:e.enabled.checked,email:e.email.value.trim(),staging:e.staging.value==='true',accept_terms:e.terms.checked};
      await save(next);closeDialog($('#cert-settings-dialog'));render();
    } else if(form.id==='certificate-request-form') {
      const index=Number(e.index.value),request={id:index<0?newID():next.acme.requests[index].id,provider:e.provider.value,domains:e.domains.value.split(/[\n,]+/).map(s=>s.trim().toLowerCase()).filter(Boolean),enabled:e.enabled.checked};
      if(index<0) next.acme.requests.push(request);else next.acme.requests[index]=request;
      const token=e.clear_token.checked?'':e.token.value.trim()||undefined;
      await save(next,undefined,undefined,token===undefined?undefined:{[request.id]:token});e.token.value='';closeDialog($('#certificate-request-dialog'));render();
    } else if(form.id==='group-form') {
      const index=Number(e.index.value), g={id:index<0?newID():next.groups[index].id,name:e.name.value.trim(),http_port:Number(e.http_port.value),https_port:Number(e.https_port.value),enabled:e.enabled.checked,domain_suffix:e.domain_suffix.value.trim().toLowerCase(),ddns_group_id:e.ddns_group_id.value};
      if(index<0) next.groups.push(g); else next.groups[index]=g;
      await save(next); closeDialog($('#group-dialog')); render();
    } else if(form.id==='subscription-form') {
      const index=Number(e.index.value), d={id:index<0?newID():next.subscriptions[index].id,name:e.name.value.trim(),url:e.url.value.trim(),interval:Number(e.interval.value),enabled:e.enabled.checked};
      if(index<0) next.subscriptions.push(d); else next.subscriptions[index]=d;
      await save(next); closeDialog($('#subscription-dialog')); render();
    }
  });
});
document.addEventListener('click',async event=>{
  const button=event.target.closest('button'); if(!button) return;
  if(button.id==='nav-toggle') {setNavigation(!document.body.classList.contains('nav-open'));return;}
  if(button.id==='nav-close'||button.id==='nav-scrim') {setNavigation(false);return;}
  if(button.dataset.page) {if(!leaveRouteEditor()) return;setNavigation(false,false);location.hash=button.dataset.page;return;}
  const action=button.dataset.action;
  if(action==='close-dialog') { closeDialog(button.closest('dialog')); return; }
  if(!config||busy) return;
  if(action==='edit-dashboard') return toggleDashboardEditing();
  if(action==='configure-dashboard') return openDashboardSettings();
  if(action==='move-widget') return moveDashboardWidget(button.dataset.widget,Number(button.dataset.direction));
  if(action==='toggle-group-panel') return toggleGroupPanel(button);
  if(action==='discover-services') return openDiscovery(button.dataset.group);
  if(action==='start-discovery') return startDiscovery();
  if(action==='stop-discovery') return stopDiscovery();
  if(action==='add-route') return openRoute(-1,button.dataset.group);
  if(action==='edit-route') return openRoute(Number(button.dataset.index));
  if(action==='import-route-image') return loadRouteImage('url');
  if(action==='clear-route-image') return clearRouteImage();
  if(['add-group','edit-group','toggle-group','delete-group','toggle-route','delete-route'].includes(action)&&!leaveRouteEditor()) return;
  if(action==='add-ddns') return openDDNS();
  if(action==='edit-ddns') return openDDNS(Number(button.dataset.index));
  if(action==='edit-ddns-hosts') return openDDNSHosts(Number(button.dataset.index));
  if(action==='add-certificate') return openCertificate();
  if(action==='configure-certificates') return openCertificateSettings();
  if(action==='edit-certificate') return openCertificate(Number(button.dataset.index));
  if(action==='add-firewall') return openFirewall();
  if(action==='edit-firewall') return openFirewall(Number(button.dataset.index));
  if(action==='add-group') return openGroup();
  if(action==='edit-group') return openGroup(Number(button.dataset.index));
  if(action==='add-subscription') return openSubscription();
  if(action==='edit-subscription') return openSubscription(Number(button.dataset.index));
  if(action==='preset-subscription') return openSubscription(-1,button.dataset.family);
  if(action==='go-firewalls') { if(leaveRouteEditor()) location.hash='access'; return; }
  if(action==='add-ip-block') return openIPBlock();
  if(action==='filter-ip-blocks') {ipBlockRule=$('#ip-block-rule').value;ipBlockSearch=$('#ip-block-search').value.trim();ipBlockPage=1;render();return;}
  if(action==='ip-block-page') {ipBlockPage=Number(button.dataset.pageNumber);render();return;}
  if(action==='add-http-rule') { $('#http-rules').insertAdjacentHTML('beforeend',httpRuleFields());$('#http-rules').lastElementChild.querySelector('input').focus();return; }
  if(action==='add-waf-exception') { $('#waf-exceptions').insertAdjacentHTML('beforeend',wafExceptionFields());$('#waf-exceptions').lastElementChild.querySelector('input').focus();return; }
  if(action==='remove-protection-rule') { button.closest('.protection-rule').remove();return; }
  if(action==='add-firewall-group') { $('#firewall-groups').insertAdjacentHTML('beforeend',firewallGroupFields()); numberFirewallGroups(); return; }
  if(action==='remove-firewall-group') { button.closest('.firewall-group').remove(); numberFirewallGroups(); return; }
  if(action==='move-firewall-group-up'||action==='move-firewall-group-down') {
    const group=button.closest('.firewall-group');
    if(action==='move-firewall-group-up'&&group.previousElementSibling) group.previousElementSibling.before(group);
    if(action==='move-firewall-group-down'&&group.nextElementSibling) group.nextElementSibling.after(group);
    numberFirewallGroups(); return;
  }
  busy=true; button.disabled=true; button.setAttribute('aria-busy','true');
  try {
    if(action==='toggle-certificate'||action==='delete-certificate') {
      const next=clone(config),index=Number(button.dataset.index);
      if(action==='delete-certificate') {if(!confirm('移除此证书任务？停止续期并不再选用其证书，证书文件保留。')) return;next.acme.requests.splice(index,1);}
      else next.acme.requests[index].enabled=!next.acme.requests[index].enabled;
      await save(next);render();
    }
    if(action==='refresh-public-ip') {await loadIPInfo(true);toast('公网 IP 检测已安排，DNS 记录按组同步任务更新');}
    if(action==='check-online-update') await checkOnlineUpdate();
    if(action==='install-online-update') await installOnlineUpdate();
    if(action==='restart-service') {if(!confirm('重启会短暂中断服务，监听端口变更将生效。确认重启？')) return;const result=await api('maintenance/restart','POST',{});toast(result.message);showLogin();}
    if(action==='apply-maintenance') {
      const preview=maintenancePreview;if(!preview?.can_apply) throw new Error('请先检查可用的维护包');
      const message=preview.kind==='restore'?'恢复会覆盖当前全部配置、Token 和管理员登录数据，并重启服务。确认恢复？':'确认更新至 '+preview.version+' 并重启服务？';
      if(!confirm(message)) return;
      const result=await api('maintenance/apply-'+preview.kind,'POST',{id:preview.id});maintenancePreview=null;toast(result.message);showLogin();
    }
    if(action==='apply-log-filters') {readLogFilters();await loadLogs();}
    if(action==='reset-log-filters') {logCategory='';logRule='';logResult='';logMethod='';logStatus='';logSearch='';logIP='';logFrom='';logTo='';await loadLogs();}
    if(action==='project-logs'||action==='access-logs') {logScope=action==='access-logs'?'access':'project';await loadLogs();}
    if(action==='refresh-dns-records') await loadDNSRecords(true);
    if(action==='refresh-logs') await loadLogs();
    if(action==='log-page') await loadLogs(Number(button.dataset.pageNumber),true);
    if(action==='event-page') await loadSecurityEvents(button.dataset.kind,Number(button.dataset.pageNumber),true);
    if(action==='refresh-events') await loadSecurityEvents(button.dataset.kind);
    if(action==='reset-events') {securityEvents[button.dataset.kind].filters=emptyEventFilters();await loadSecurityEvents(button.dataset.kind);}
    if(action==='refresh-ip-blocks') await loadIPBlocks();
    if(action==='release-ip-block') {
	  ++ipBlockRequest;
      const result=await api('ip-blocks','DELETE',{rule:button.dataset.rule,ip:button.dataset.ip});
      ipBlockView=result;ipBlockReceived=Date.now();ipBlockError='';render();toast(result.write_error?'已解除，但保存失败，请检查磁盘和权限':'已解除 IP 冻结，连续失败计数已清零');
    }
    if(action==='refresh-statistics') await Promise.all([loadStatistics(),loadSecurityEvents('security')]);
    if(action==='export-statistics') downloadStatistics();
    if(action==='run-ddns') {const g=config.ddns.groups[Number(button.dataset.index)];const result=await api('ddns/'+encodeURIComponent(g.id)+'/run','POST',{});toast(result.message);await refreshStatus();render();}
    if(action==='toggle-ddns'||action==='delete-ddns') {
      const next=clone(config),index=Number(button.dataset.index);
      if(action==='delete-ddns') {if(next.groups.some(g=>g.ddns_group_id===next.ddns.groups[index].id)) throw new Error('请先在反代组中解除对此 DDNS 组的绑定。');if(!confirm('删除此 DDNS 组及其凭据？DNS 中已有记录会保留。')) return;const id=next.ddns.groups[index].id;next.ddns.groups.splice(index,1);if(next.acme.dns_groups) Object.keys(next.acme.dns_groups).forEach(host=>{if(next.acme.dns_groups[host]===id) delete next.acme.dns_groups[host];});}else next.ddns.groups[index].enabled=!next.ddns.groups[index].enabled;
      await save(next);render();
    }
    if(button.id==='logout') { await api('logout','POST',{}); showLogin(); }
    if(button.dataset.job) { const result=await api('jobs/'+button.dataset.job,'POST',{}); toast(result.message); await refreshStatus(); }
    if(action==='refresh-subscription') { const d=config.subscriptions[Number(button.dataset.index)]; const result=await api('subscriptions/'+encodeURIComponent(d.id)+'/refresh','POST',{}); toast(result.message); await refreshStatus(); render(); }
    if(action==='toggle-group'||action==='delete-group') {
      const next=clone(config), index=Number(button.dataset.index), group=next.groups[index];
      if(action==='delete-group') { if(!confirm('删除此反代组及组内所有代理规则？DNS 记录和证书文件会保留。')) return; next.routes=next.routes.filter(r=>r.group_id!==group.id); next.groups.splice(index,1); }
      else group.enabled=!group.enabled;
      await save(next); render();
    }
    if(action==='delete-firewall') {
      const next=clone(config), index=Number(button.dataset.index), firewall=next.firewalls[index];
      if(next.routes.some(r=>r.firewall_id===firewall.id)) throw new Error('此防火墙正在被代理服务使用，请先更换或移除代理的防火墙选择。');
      if(!confirm('删除此防火墙及其中所有 IP 组？')) return;
      next.firewalls.splice(index,1); await save(next); render();
    }
    if(action==='toggle-subscription'||action==='delete-subscription') {
      const next=clone(config), index=Number(button.dataset.index), subscription=next.subscriptions[index];
      if(next.firewalls.some(f=>firewallSubscriptions(f).includes(subscription.id))) throw new Error('请先从防火墙的 IP 组中移除此订阅引用，再停用或删除。');
      if(action==='delete-subscription') { if(!confirm('删除此订阅配置？')) return; next.subscriptions.splice(index,1); }
      else subscription.enabled=!subscription.enabled;
      await save(next); render();
    }
    if(action==='toggle-route'||action==='delete-route') {
      const next=clone(config), index=Number(button.dataset.index);
      if(action==='delete-route') { if(!confirm('删除这条代理规则？对应 DNS 记录和已保存证书不会被删除。')) return; next.routes.splice(index,1); }
      else next.routes[index].enabled=!next.routes[index].enabled;
      await save(next); render();
    }
  } catch(e) { toast(e.message); }
  finally { busy=false; button.disabled=false; button.removeAttribute('aria-busy'); }
});
window.addEventListener('hashchange',()=>{ if(config) { const nextPage=pages[location.hash.slice(1)]?location.hash.slice(1):'overview';if(nextPage!==page&&!leaveRouteEditor()) {history.replaceState(null,'','#'+page);return;}page=nextPage; render(); if(page==='logs') loadLogs().catch(e=>toast(e.message)); if(page==='statistics') {loadStatistics().catch(e=>toast(e.message));loadSecurityEvents('security').catch(e=>toast(e.message));} if(page==='firewall-logs') loadSecurityEvents('firewall').catch(e=>toast(e.message)); if(page==='ip-blocks') loadIPBlocks().catch(e=>toast(e.message)); } });
$('#route-form').addEventListener('invalid',event=>{
  if(event.target.name==='host') validateRouteHost(event.currentTarget,true);
},true);
$('#route-form').elements.host.addEventListener('blur',event=>validateRouteHost(event.target.form,true));
document.addEventListener('input',event=>{
  const form=event.target.closest('form');
  if(form?.id==='route-form') form.dataset.dirty='true';
  if(form?.id==='route-form'&&event.target.name==='upstream'&&/^https?:\/\//i.test(event.target.value)) {const upstream=upstreamParts(event.target.value);form.elements.upstream_scheme.value=upstream.scheme;event.target.value=upstream.address;}
  if(form?.id==='route-form'&&event.target.name==='host') updateRouteDomain(form);
  if(form?.id==='ddns-group-form'&&['zone','hosts'].includes(event.target.name)) updateDomainPreview(form,form.elements.zone.value);
  if(form?.id==='ddns-hosts-form'&&event.target.name==='hosts') updateDomainPreview(form,config.ddns.groups.find(g=>g.id===form.elements.group_id.value)?.zone||'');
});
document.addEventListener('error',event=>{if(event.target.matches?.('img[data-discovery-icon],img[data-route-image]')) event.target.hidden=true;},true);
document.addEventListener('input',event=>{if(event.target.closest('#discovery-results')) updateDiscoveryInput(event.target);const form=event.target.closest('[data-event-filters]');if(form&&event.target.name) securityEvents[form.dataset.eventFilters].filters[event.target.name]=event.target.value;});
document.addEventListener('change',async event=>{
  if(event.target.closest('#discovery-results')) {updateDiscoveryInput(event.target);return;}
  if(event.target.id==='discovery-group') {updateDiscoveryGroup(true);return;}
  if(event.target.id==='discovery-port-mode') {const custom=event.target.value==='custom';$('#discovery-custom-ports').hidden=!custom;$('#discovery-form').elements.ports.disabled=!custom;$('#discovery-form').elements.ports.required=custom;return;}
  if(event.target.dataset.eventSize) {const kind=event.target.dataset.eventSize;securityEvents[kind].size=Number(event.target.value);try{await loadSecurityEvents(kind,1,true);}catch(e){toast(e.message);}return;}
  if(event.target.closest('#route-form')) $('#route-form').dataset.dirty='true';
  if(event.target.closest('#route-form')&&event.target.name==='image_file') {await loadRouteImage('file');return;}
  if(event.target.closest('#route-form')&&event.target.name==='group_id') {const form=event.target.closest('form');form.dataset.fullHost='false';updateRouteDomain(form);return;}
  if(event.target.closest('#group-form')&&event.target.name==='ddns_group_id') {const form=event.target.closest('form');if(!form.elements.domain_suffix.value) form.elements.domain_suffix.value=config.ddns.groups.find(g=>g.id===event.target.value)?.zone||'';return;}
  if(event.target.id==='log-size') {logSize=Number(event.target.value);try{await loadLogs();}catch(e){toast(e.message);}return;}
  if(event.target.id==='ip-block-rule') {ipBlockRule=event.target.value;ipBlockPage=1;render();return;}
  if(['statistics-hours','statistics-rule'].includes(event.target.id)) {statisticsHours=$('#statistics-hours').value;statisticsRule=$('#statistics-rule').value;try{await loadStatistics();}catch(e){toast(e.message);}return;}
  if(['ip-group','preview-interface','preview-ipv4-source','preview-ipv6-source'].includes(event.target.id)) {
    if(event.target.id==='ip-group') {ipGroup=event.target.value;$('#ip-controls').innerHTML=ipControlsHTML();}
    else {ipInterface=$('#preview-interface').value;ipIPv4Source=$('#preview-ipv4-source').value;ipIPv6Source=$('#preview-ipv6-source').value;}
    ipViewKey='';$('#ip-results').innerHTML=ipResultsHTML(true);$('#ip-details').innerHTML=ipResultsHTML();
    try {await loadIPInfo();}catch(e){$('#ip-results').textContent=e.message;}return;
  }
  if(['log-category','log-result','log-rule','log-method'].includes(event.target.id)) {readLogFilters();loadLogs().catch(e=>toast(e.message));}});
document.querySelectorAll('dialog').forEach(dialog=>{
  dialog.setAttribute('aria-labelledby',dialog.querySelector('h2').id);
  const form=dialog.querySelector('form'),heading=form.querySelector('.dialog-heading'),actions=form.querySelector('.dialog-actions'),body=document.createElement('div');body.className='dialog-body';
  while(heading.nextSibling&&heading.nextSibling!==actions) body.append(heading.nextSibling);
  form.insertBefore(body,actions);
  dialog.querySelector('[data-action="close-dialog"]').innerHTML=icon('close');
  dialog.addEventListener('cancel',event=>{event.preventDefault();closeDialog(dialog);});
  dialog.addEventListener('close',()=>{
    if(dialog.id==='discovery-dialog') abandonDiscovery();
    if(dialog.id==='route-dialog') {++routeImageRequest;routeImagePending=false;}
    dialogMotions.get(dialog)?.cancel();dialogMotions.delete(dialog);
    const opener=dialogOpeners.get(dialog);if(opener) $(opener)?.focus({preventScroll:true});
  });
});
$('#sidebar .brand').addEventListener('click',()=>setNavigation(false,false));
$('.skip-link').addEventListener('click',event=>{event.preventDefault();$('#content').focus();});
document.addEventListener('keydown',event=>{
  if(!document.body.classList.contains('nav-open')) return;
  if(event.key==='Escape') {event.preventDefault();setNavigation(false);return;}
  if(event.key==='Tab') {
    const targets=[...$('#sidebar').querySelectorAll('a,button')].filter(el=>!el.disabled&&el.getClientRects().length);
    const index=targets.indexOf(document.activeElement);
    if(event.shiftKey&&index<=0) {event.preventDefault();targets.at(-1)?.focus();}
    else if(!event.shiftKey&&(index===targets.length-1||index===-1)) {event.preventDefault();targets[0]?.focus();}
  }
});
mobileNavigation.addEventListener('change',()=>setNavigation(false,false));
compactCharts.addEventListener('change',()=>{if(page==='statistics'&&config) render();});
document.addEventListener('click',event=>document.querySelectorAll('.object-menu[open]').forEach(menu=>{if(!menu.contains(event.target)) menu.open=false;}));
document.addEventListener('keydown',event=>{
  if(event.key!=='Escape') return;
  const menu=document.querySelector('.object-menu[open]');if(menu) {menu.open=false;menu.querySelector('summary').focus();event.preventDefault();return;}
});
reducedMotion.addEventListener('change',()=>{
  if(!reducedMotion.matches) return;
  pageMotion?.cancel();
  document.querySelectorAll('dialog').forEach(dialog=>{const motion=dialogMotions.get(dialog);if(motion?.effect.getTiming().fill==='forwards') dialog.close();motion?.cancel();});
});
hydrateIcons();setNavigation(false,false);
load().catch(e=>{ showLogin(); if(e.message!=='请先登录') $('#login-error').textContent=e.message; });
setInterval(async()=>{
  if(!config||document.hidden||busy||dashboardDrag||dashboardSaving) return;
  try {
    await refreshStatus();
    if(page==='overview'&&!dashboardDrag&&!dashboardSaving&&!document.querySelector('dialog[open]')) await loadDashboard();
    if((page==='subscriptions'||page==='access')&&!document.querySelector('dialog[open]')) render();
    if(page==='ddns') config.ddns.groups.forEach(g=>{const badgeElement=$('[data-ddns-status="'+g.id+'"]');if(badgeElement) badgeElement.innerHTML=ddnsBadge(g);const summary=$('[data-ddns-summary="'+g.id+'"]');if(summary) summary.textContent=ddnsSummary(g);const button=$('[data-ddns-run="'+g.id+'"]');if(button) button.disabled=!g.enabled||!!ddnsGroupStatus(g)?.running;});
    if(page==='ddns') await Promise.all([loadIPInfo(),loadDNSRecords()]);
    if(page==='certificates') {const target=$('#certificate-table'),focused=target.contains(document.activeElement)?focusSelector(document.activeElement):'';target.innerHTML=certificateTable();hydrateIcons(target);if(focused) $(focused,target)?.focus({preventScroll:true});}
    if(page==='ip-blocks'&&!document.querySelector('dialog[open]')&&document.activeElement!==$('#ip-block-search')) await loadIPBlocks(true);
    document.querySelectorAll('[data-job-summary]').forEach(el=>{ const kind=el.dataset.jobSummary; const job=status.jobs?.[kind]; el.textContent=job?job.message+' · 最近检查：'+date(job.last_run):'尚未运行任务。'; });
  } catch { /* Connection status is visible; never replace a form with a polling error. */ }
},5000);
