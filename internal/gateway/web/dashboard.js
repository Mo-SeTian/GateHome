'use strict';
const widgetCatalog=[['server','服务器状态','server'],['cpu','CPU 占用','activity'],['memory','内存占用','layers'],['storage','存储空间','server'],['traffic','上传 / 下载速率','activity'],['transfer','上传 / 下载数据量','download'],['requests','访问数量','chart'],['blocked','拦截数量','shield'],['trend','访问趋势','chart'],['provinces','省份 / IP 分析','globe'],['map','中国访问地图','globe'],['services','代理服务','routes'],['listeners','监听入口','gateway'],['events','最近活动','logs']];
const provincePoints={'北京':[116.4,39.9],'天津':[117.2,39.1],'河北':[114.5,38.0],'山西':[112.5,37.9],'内蒙古':[111.7,40.8],'辽宁':[123.4,41.8],'吉林':[125.3,43.9],'黑龙江':[126.6,45.8],'上海':[121.5,31.2],'江苏':[118.8,32.1],'浙江':[120.2,30.3],'安徽':[117.3,31.9],'福建':[119.3,26.1],'江西':[115.9,28.7],'山东':[117.0,36.7],'河南':[113.6,34.8],'湖北':[114.3,30.6],'湖南':[113.0,28.2],'广东':[113.3,23.1],'广西':[108.3,22.8],'海南':[110.3,20.0],'重庆':[106.5,29.6],'四川':[104.1,30.7],'贵州':[106.7,26.6],'云南':[102.7,25.0],'西藏':[91.1,29.7],'陕西':[108.9,34.3],'甘肃':[103.8,36.1],'青海':[101.8,36.6],'宁夏':[106.3,38.5],'新疆':[87.6,43.8],'台湾':[121.5,25.0],'香港':[114.2,22.3],'澳门':[113.5,22.2]};
let dashboardView=null,dashboardError='',dashboardRequest=0,dashboardDrag=null,dashboardSaving=false,dashboardEditing=false;

function resetDashboard() { cancelWidgetDrag();dashboardRequest++;dashboardView=null;dashboardError='';dashboardEditing=false; }
function toggleDashboardEditing() {
  if(dashboardSaving||busy) return;
  cancelWidgetDrag();dashboardEditing=!dashboardEditing;render();
  $('[data-action="edit-dashboard"]')?.focus({preventScroll:true});
}
function widgetIDs(current=config) { return current.dashboard?.widgets??widgetCatalog.map(w=>w[0]); }
function bytesLabel(value) {
  if(!Number.isFinite(Number(value))||value===null||value===undefined) return '—';
  const units=['B','KiB','MiB','GiB','TiB'];let size=Math.max(0,Number(value)),i=0;
  while(size>=1024&&i<units.length-1) {size/=1024;i++;}
  return size.toLocaleString('zh-CN',{maximumFractionDigits:i?1:0})+' '+units[i];
}
function safeServiceURL(value) {
  try {const u=new URL(value);return ['http:','https:'].includes(u.protocol)&&u.hostname&&!u.username&&!u.password?u.href:'';} catch {return '';}
}
function publicServiceURL(route,current=config) {
  const group=current.groups.find(g=>g.id===route.group_id);if(!group) return '';
  const secure=route.tls||!group.http_port,port=secure?group.https_port:group.http_port;
  return port?safeServiceURL(`${secure?'https':'http'}://${route.host}:${port}/`):'';
}
function serviceAddressHTML(route) {
  const internal=safeServiceURL(route.upstream),external=publicServiceURL(route);
  const line=(url,label,kind)=>url?`<a class="service-address-link mono" href="${esc(url)}" target="_blank" rel="noopener noreferrer" aria-label="${esc(kind+'：'+label+'，在新标签页打开')}">${icon('external-link')}<span>${esc(label)}</span></a>`:`<span class="form-note">${esc(label)}</span>`;
  return line(internal,route.upstream,'内网服务地址')+line(external,external?new URL(external).host:route.host+'（监听端口已关闭）','域名访问地址');
}
function metricHTML(value,note='',unit='') {return `<div class="widget-metric">${esc(value)}${unit?`<small>${esc(unit)}</small>`:''}</div><p class="widget-note">${esc(note)}</p>`;}
function meterHTML(used,total) {
  if(!total) return '';
  const percent=Math.min(100,Math.max(0,100*used/total));
  return `<meter min="0" max="100" value="${percent}" aria-label="已使用 ${percent.toFixed(1)}%"></meter>`;
}
function widgetBody(id) {
  const resources=dashboardView?.resources||{},access=dashboardView?.access||{},regions=dashboardView?.regions||{};
  if(id==='server') return metricHTML('运行中',`${resources.platform||'正在读取'} · ${resources.cpus||'—'} 核`)+`<div class="widget-pair"><span>GateHome <b>v${esc(status.version||'—')}</b></span><span>服务运行 <b>${Math.floor((status.uptime_seconds||0)/3600)} 小时 ${Math.floor((status.uptime_seconds||0)%3600/60)} 分钟</b></span></div><p class="widget-note">CPU、内存和网卡来自运行环境；Docker 的 /proc 可能反映宿主机资源。</p>`;
  if(id==='cpu') return metricHTML(resources.cpu_percent===null||resources.cpu_percent===undefined?'—':resources.cpu_percent.toFixed(1),'',resources.cpu_percent===null||resources.cpu_percent===undefined?'':'%')+(resources.cpu_percent===null||resources.cpu_percent===undefined?'':meterHTML(resources.cpu_percent,100))+`<p class="widget-note">${esc(resources.cpu_error||'整体 CPU 使用率，两次采样差值；首次采样后显示。')}</p>`;
  if(id==='memory') return metricHTML(resources.memory_total?(100*resources.memory_used/resources.memory_total).toFixed(1):'—','',resources.memory_total?'%':'')+meterHTML(resources.memory_used,resources.memory_total)+`<p class="widget-note">${esc(resources.memory_error||bytesLabel(resources.memory_used)+' / '+bytesLabel(resources.memory_total)+' · 扣除可用缓存')}</p>`;
  if(id==='storage') return `<div class="storage-rows">${(resources.storage||[]).map(d=>`<div><b>${esc(d.name)}文件系统</b><span>${d.total?(100*d.used/d.total).toFixed(1)+'%':'—'}</span>${meterHTML(d.used,d.total)}<small>${esc(d.error||'已用 '+bytesLabel(d.used)+' · 可用 '+bytesLabel(d.available))}</small></div>`).join('')||'<p class="widget-note">正在读取存储空间…</p>'}</div>`;
  if(id==='traffic'||id==='transfer') {
    const rate=id==='traffic';
    return `<div class="traffic-values"><div><span>${icon('upload')}上传</span><b>${bytesLabel(resources.network_error?null:(rate?resources.tx_rate:resources.tx_bytes))}${rate?'<small>/s</small>':''}</b></div><div><span>${icon('download')}下载</span><b>${bytesLabel(resources.network_error?null:(rate?resources.rx_rate:resources.rx_bytes))}${rate?'<small>/s</small>':''}</b></div></div><p class="widget-note">${esc(resources.network_error||'网卡 '+(resources.interface||'自动选择')+' · '+(rate?'每 5 秒采样，首次采样后显示。':'自网卡启动累计，包含此网卡全部流量。'))}</p>`;
  }
  if(id==='requests') return metricHTML((status.requests||0).toLocaleString(),'本次启动后的业务请求','次')+`<p class="widget-note">最近 24 小时保留记录：${access.total||0} 次 · ${regions.unique_ips||0} 个来源 IP</p>`;
  if(id==='blocked') return metricHTML((status.blocked||0).toLocaleString(),'本次启动后的访问拦截','次')+`<p class="widget-note">保留记录：防火墙 ${access.firewall_blocked||0} · 冻结 ${access.frozen_blocked||0} · 账号失败 ${access.auth_failed||0}</p>`;
  if(id==='trend') return dashboardView?`<div class="widget-chart">${trendChart(access,compactCharts.matches)}</div><div class="chart-legend"><span>成功 ${access.normal||0}</span><span>拦截 ${access.blocked||0}</span><span>失败 ${access.failed||0}</span></div><p class="widget-note">最近 24 小时，基于尚未清理且进入查询缓存的访问记录。</p>`:'<p class="widget-note">正在读取访问趋势…</p>';
  if(id==='provinces') return `<div class="province-summary"><b>${regions.unique_ips||0}</b> 个来源 IP <span>· ${regions.sample||0} 次访问</span></div><div class="province-list">${(regions.provinces||[]).map(row=>`<div><span>${esc(row.name)}</span><meter min="0" max="${Math.max(1,regions.provinces[0]?.requests||1)}" value="${row.requests}" aria-label="${esc(row.name)} ${row.requests} 次访问"></meter><b>${row.requests} 次 <small>${row.ips} IP</small></b></div>`).join('')||'<p class="widget-note">还没有访问记录，产生访问后显示省份分析。</p>'}</div><p class="widget-note">最近 24 小时 · IP 离线估算 · 未定位 ${regions.unmapped||0} 次，包含内网及境外地址。</p>`;
  if(id==='map') return dashboardMapHTML(regions);
  if(id==='services') return routeTable(false,true);
  if(id==='listeners') return `<div class="listener-summary">${config.groups.map(g=>`<div><b>${esc(g.name)}</b>${groupBadge(g)}<small>${esc(portsText(g))}</small></div>`).join('')||'<p>尚未创建反代组。</p>'}</div>`;
  if(id==='events') return eventsHTML();
  return '';
}
function dashboardMapHTML(regions) {
  const target=config.dashboard?.map_province,hub=provincePoints[target]||[104,35],point=p=>[(p[0]-73)*8,(54-p[1])*8];
  const [hx,hy]=point(hub),visits=regions.visits||[];
  const paths=visits.filter(v=>provincePoints[v.province]).map(v=>{
    const [x,y]=point(provincePoints[v.province]),distance=Math.hypot(x-hx,y-hy),curve=(y+hy)/2-Math.min(65,distance/3);
    return `<g class="visit-${v.blocked?'blocked':'allowed'}"><path class="visit-line" d="M${x},${y} Q${(x+hx)/2},${curve} ${hx},${hy}"/><circle cx="${x}" cy="${y}" r="3.5" class="visit-point"/><title>${esc(v.ip+' · '+v.province+' · '+date(v.time)+(v.blocked?' · 拦截':''))}</title></g>`;
  }).join('');
  return `<div class="access-map"><svg viewBox="0 0 520 320" role="img" aria-label="最近访问按 IP 归属省份汇聚的中国示意地图"><title>来源省份到${esc(target||'示意汇聚点')}的访问，非精确位置或实际网络线路</title><image href="/china-outline.svg" width="520" height="320"/>${paths}<circle cx="${hx}" cy="${hy}" r="6" class="map-hub"/><text x="${hx+10}" y="${hy-10}" class="map-label">${esc(target||'汇聚点（示意）')}</text></svg></div><div class="map-legend"><span>允许 / 普通访问</span><span>拦截</span><span>${visits.filter(v=>provincePoints[v.province]).length} 条已定位记录</span></div><div class="map-visits">${visits.slice(0,5).map(v=>`<div><span class="mono">${esc(v.ip)}</span><span>${esc(v.province||v.region)}</span><small>${esc(date(v.time))}</small></div>`).join('')||'<p class="widget-note">等待访问记录。省份可定位后显示动态访问线条。</p>'}</div><p class="widget-note">最近 20 次保留访问 · 每 5 秒刷新 · 线条表示访问方向，省份点为示意位置。</p>`;
}
function dashboardHTML() {
  const ids=widgetIDs();
  return heading('WORKSPACE / OVERVIEW','网络概览','服务器、流量与安全事件，自定义你的工作空间。',`<button class="${dashboardEditing?'primary':'secondary'}" data-action="edit-dashboard" aria-pressed="${dashboardEditing}" ${dashboardSaving?'disabled':''}>${icon(dashboardEditing?'save':'edit')}${dashboardEditing?'完成':'编辑布局'}</button>${dashboardEditing?'<button class="secondary" data-action="configure-dashboard">'+icon('settings')+'配置组件</button>':''}`+addRouteButton())+
    `<div class="dashboard-meta"><span>${dashboardView?'最近采样：'+esc(date(dashboardView.sampled_at)):'正在读取实时数据…'} · 每 5 秒刷新</span><span>${dashboardEditing?'编辑模式：拖动手柄或按钮排序，修改自动保存，完成后退出。':'点击“编辑布局”配置组件和调整顺序'}</span></div><p id="dashboard-error" class="error" role="alert">${esc(dashboardError)}</p><div class="dashboard-grid" role="list" aria-label="概览小组件">${ids.map((id,i)=>{
      const definition=widgetCatalog.find(w=>w[0]===id);if(!definition) return '';
      return `<section class="dashboard-widget ${['map','trend','provinces','services','listeners','events'].includes(id)?'widget-wide':''}" data-widget="${id}" role="listitem"><div class="widget-heading"><h2>${icon(definition[2])}${definition[1]}</h2>${dashboardEditing?`<div class="widget-controls"><button class="icon-button widget-handle" type="button" data-widget-handle="${id}" aria-label="拖动${definition[1]}排序，方向键可移动">${icon('menu')}</button><button class="icon-button" type="button" data-action="move-widget" data-widget="${id}" data-direction="-1" aria-label="上移${definition[1]}" ${i===0?'disabled':''}>${icon('chevron-up')}</button><button class="icon-button" type="button" data-action="move-widget" data-widget="${id}" data-direction="1" aria-label="下移${definition[1]}" ${i===ids.length-1?'disabled':''}>${icon('chevron-down')}</button></div>`:''}</div><div class="widget-body">${widgetBody(id)}</div></section>`;
    }).join('')}</div>${ids.length?'':'<div class="empty"><h3>尚未显示组件</h3><p>点击“编辑布局”，通过“配置组件”选择要显示的内容。</p></div>'}<p id="dashboard-announcement" class="sr-only" role="status" aria-live="polite"></p>`;
}
async function loadDashboard() {
  if(!config||page!=='overview'||dashboardDrag||dashboardSaving) return;
  const request=++dashboardRequest;
  try {
    const value=await api('dashboard');
    if(request!==dashboardRequest||!config||page!=='overview'||dashboardDrag||dashboardSaving) return;
    dashboardView=value;dashboardError='';
    document.querySelectorAll('.dashboard-widget').forEach(card=>{const body=card.querySelector('.widget-body');body.innerHTML=widgetBody(card.dataset.widget);prepareTables(body);hydrateIcons(body);});
    const sample=$('.dashboard-meta span');if(sample) sample.textContent='最近采样：'+date(value.sampled_at)+' · 每 5 秒刷新';
    const error=$('#dashboard-error');if(error) error.textContent='';
  } catch(error) {
    if(request===dashboardRequest&&config&&page==='overview') {dashboardError=error.message;const el=$('#dashboard-error');if(el) el.textContent='刷新失败，保留上次数据：'+error.message;}
  }
}
function openDashboardSettings() {
  if(!dashboardEditing||dashboardSaving) return;
  const form=$('#dashboard-form'),selected=widgetIDs();
  $('#dashboard-choices').innerHTML=widgetCatalog.map(([id,title])=>`<label class="check-label"><input type="checkbox" name="widgets" value="${id}" ${selected.includes(id)?'checked':''}>${title}</label>`).join('');
  const interfaces=dashboardView?.resources?.networks||[],saved=config.dashboard?.network_interface||'';
  form.elements.network_interface.innerHTML='<option value="">自动选择活跃网卡</option>'+[...new Set([...interfaces.map(n=>n.name),...saved?[saved]:[]])].map(name=>`<option value="${esc(name)}">${esc(name)}</option>`).join('');
  form.elements.network_interface.value=saved;
  form.elements.map_province.innerHTML='<option value="">示意汇聚点</option>'+Object.keys(provincePoints).map(name=>`<option>${name}</option>`).join('');
  form.elements.map_province.value=config.dashboard?.map_province||'';$('.error',form).textContent='';openDialog($('#dashboard-dialog'));
}
async function saveDashboardSettings() {
  if(!dashboardEditing) return;
  const form=$('#dashboard-form'),selected=new FormData(form).getAll('widgets'),next=clone(config),current=widgetIDs();
  next.dashboard={widgets:[...current.filter(id=>selected.includes(id)),...selected.filter(id=>!current.includes(id))],network_interface:form.elements.network_interface.value,map_province:form.elements.map_province.value};
  await save(next);closeDialog($('#dashboard-dialog'));render();await loadDashboard();
}
function reorderedWidgets(ids,id,target,after=false) {
  if(id===target||!ids.includes(id)||!ids.includes(target)) return [...ids];
  const next=ids.filter(value=>value!==id),index=next.indexOf(target)+(after?1:0);next.splice(index,0,id);return next;
}
async function persistWidgetOrder(ids,focusID='') {
  if(!dashboardEditing||dashboardSaving||!config) return;
  const old=widgetIDs();if(JSON.stringify(old)===JSON.stringify(ids)) return;
  dashboardSaving=true;
  const editButton=$('[data-action="edit-dashboard"]');if(editButton) editButton.disabled=true;
  const grid=$('.dashboard-grid');grid?.setAttribute('aria-busy','true');
  try {
    const next=clone(config);next.dashboard={...(next.dashboard||{}),widgets:ids};
    await save(next);render();
    $(`[data-widget-handle="${focusID}"]`)?.focus({preventScroll:true});
    const announcement=$('#dashboard-announcement');if(announcement) announcement.textContent='组件顺序已保存';
  } catch(error) {toast('排序未保存，原顺序保留：'+error.message);render();}
  finally {dashboardSaving=false;const editButton=$('[data-action="edit-dashboard"]');if(editButton) editButton.disabled=false;}
}
async function moveDashboardWidget(id,direction) {
  if(!dashboardEditing) return;
  const ids=widgetIDs(),index=ids.indexOf(id),target=index+direction;
  if(target<0||target>=ids.length) return;
  const next=[...ids];[next[index],next[target]]=[next[target],next[index]];await persistWidgetOrder(next,id);
}
function cancelWidgetDrag() {
  const drag=dashboardDrag;if(!drag) return;
  dashboardDrag=null;
  drag.card.classList.remove('widget-dragging');drag.card.style.removeProperty('transform');
  drag.handle.setAttribute('aria-pressed','false');
  document.querySelectorAll('.widget-drop-target').forEach(el=>el.classList.remove('widget-drop-target'));
  if(drag.handle.hasPointerCapture(drag.pointer)) drag.handle.releasePointerCapture(drag.pointer);
}
document.addEventListener('pointerdown',event=>{
  const handle=event.target.closest('[data-widget-handle]');
  if(!dashboardEditing||!handle||event.button!==0||!config||busy||dashboardSaving||dashboardDrag) return;
  event.preventDefault();handle.focus();handle.setPointerCapture(event.pointerId);
  dashboardDrag={handle,card:handle.closest('.dashboard-widget'),id:handle.dataset.widgetHandle,pointer:event.pointerId,x:event.clientX,y:event.clientY,active:false,target:null};
});
document.addEventListener('pointermove',event=>{
  const drag=dashboardDrag;if(!drag||event.pointerId!==drag.pointer) return;
  const dx=event.clientX-drag.x,dy=event.clientY-drag.y;
  if(!drag.active&&Math.hypot(dx,dy)<6) return;
  drag.active=true;drag.card.classList.add('widget-dragging');drag.handle.setAttribute('aria-pressed','true');
  drag.card.style.transform=`translate(${dx}px,${dy}px)`;
  const target=document.elementFromPoint(event.clientX,event.clientY)?.closest('.dashboard-widget');
  document.querySelectorAll('.widget-drop-target').forEach(el=>el.classList.remove('widget-drop-target'));
  drag.target=target&&target!==drag.card?{id:target.dataset.widget,after:event.clientY>target.getBoundingClientRect().top+target.getBoundingClientRect().height/2}:null;
  if(drag.target) target.classList.add('widget-drop-target');
  if(event.clientY>innerHeight-55) window.scrollBy(0,12);
  if(event.clientY<80) window.scrollBy(0,-12);
});
document.addEventListener('pointerup',event=>{
  const drag=dashboardDrag;if(!drag||event.pointerId!==drag.pointer) return;
  const target=drag.target,id=drag.id,active=drag.active;cancelWidgetDrag();
  if(active&&target) persistWidgetOrder(reorderedWidgets(widgetIDs(),id,target.id,target.after),id);
});
document.addEventListener('pointercancel',cancelWidgetDrag);
document.addEventListener('lostpointercapture',event=>{if(dashboardDrag?.pointer===event.pointerId) cancelWidgetDrag();});
document.addEventListener('keydown',event=>{
  if(event.key==='Escape'&&dashboardDrag) {event.preventDefault();cancelWidgetDrag();return;}
  const handle=event.target.closest('[data-widget-handle]');
  if(dashboardEditing&&handle&&['ArrowUp','ArrowDown','ArrowLeft','ArrowRight'].includes(event.key)&&!dashboardSaving&&!busy) {event.preventDefault();moveDashboardWidget(handle.dataset.widgetHandle,['ArrowUp','ArrowLeft'].includes(event.key)?-1:1);}
});
window.addEventListener('blur',cancelWidgetDrag);
window.addEventListener('hashchange',()=>{cancelWidgetDrag();dashboardEditing=false;});
