'use strict';
const mcpSettings={data:null,users:[],error:'',request:0,token:''};
function resetMCPSettings() {
  mcpSettings.request++;mcpSettings.data=null;mcpSettings.users=[];mcpSettings.error='';mcpSettings.token='';
  $('#mcp-token')?.remove();
}
function mcpSettingsHTML() {
  const data=mcpSettings.data,c=data?.config;
  const endpoint=location.origin+'/mcp';
  const options=mcpSettings.users.map(user=>`<option value="${Number(user.id)}" ${user.id===c?.sunpanel_user_id?'selected':''}>${esc(user.name||user.username)} (${esc(user.username)})</option>`).join('');
  const missing=c?.sunpanel_user_id&&!mcpSettings.users.some(user=>user.id===c.sunpanel_user_id)?`<option value="${Number(c.sunpanel_user_id)}" selected>已绑定账号 #${Number(c.sunpanel_user_id)} · 刷新确认</option>`:'';
  return panel('AI 管理 · MCP','让支持 HTTP MCP 的 AI 客户端管理 GateHome 和指定账号的 Sun-Panel。',`<div class="panel-body"><label>连接地址<input readonly value="${esc(endpoint)}" spellcheck="false"></label><p class="form-note">使用 Streamable HTTP 和专用 Bearer 令牌，权限保存后立即生效。首次启用默认仅授权查询；修改与维护可分别开启。</p>${!c?'<p role="status">'+esc(mcpSettings.error||'正在读取 MCP 设置…')+'</p><button class="secondary" data-action="refresh-mcp">重新读取</button>':`<form id="mcp-form"><div class="toggle-row"><div><b>启用 MCP 服务</b><p>关闭后 AI 客户端无法继续调用工具。</p></div><label class="switch"><input name="enabled" type="checkbox" ${c.enabled?'checked':''}><span></span><span class="sr-only">启用 MCP 服务</span></label></div><div class="form-grid"><label class="check-label"><input name="read" type="checkbox" ${c.read?'checked':''}>查询：配置、状态、日志与首页</label><label class="check-label"><input name="write" type="checkbox" ${c.write?'checked':''}>修改：配置、网站、分组与任务</label><label class="check-label span-2"><input name="maintenance" type="checkbox" ${c.maintenance?'checked':''}>维护：重启、更新与恢复</label><label class="span-2">Sun-Panel 管理账号<select name="sunpanel_user_id"><option value="0">不授权 Sun-Panel</option>${options}${missing}</select><small>只可访问绑定账号的桌面。Sun-Panel 启动后可刷新账号列表。</small></label></div><button type="button" class="secondary" data-action="mcp-refresh-users">${icon('refresh')}刷新 Sun-Panel 账号</button><p class="form-note">AI 不会读取已保存的密码、Token 或私钥，不能修改自己的授权。网站内容和日志可能包含外部文本，客户端应将它们作为数据处理。</p><p class="error" role="alert">${esc(mcpSettings.error)}</p><div class="form-actions"><button type="submit" class="primary">保存 MCP 设置</button></div></form><div class="mcp-token-area"><p class="form-note">专用令牌：${data.token_configured?'已配置':'未配置'}。完整令牌只在创建或重置后显示一次，请复制到客户端保存。重置后旧令牌立即失效。</p>${mcpSettings.token?`<label>新令牌（仅本次可复制）<input id="mcp-token" type="password" readonly autocomplete="off" spellcheck="false" value="${esc(mcpSettings.token)}"></label><div class="form-actions"><button type="button" class="secondary" data-action="mcp-copy-token">复制令牌</button><button type="button" class="secondary" data-action="mcp-toggle-token" aria-pressed="false">显示令牌</button></div>`:''}<div class="form-actions"><button type="button" class="secondary" data-action="mcp-rotate-token" ${!data.token_configured?'disabled':''}>重置令牌</button></div></div>`}</div>`,`<button class="secondary" data-action="refresh-mcp">${icon('refresh')}刷新授权设置</button>`);
}
function renderMCPSettings() {
  const target=$('#mcp-settings');
  if(!target||page!=='settings'||!config) return;
  target.innerHTML=mcpSettingsHTML();hydrateIcons(target);
}
async function loadMCPSettings() {
  const request=++mcpSettings.request;
  try {
    const data=await api('mcp');
    if(request!==mcpSettings.request||!config||page!=='settings') return;
    if(!data.token_configured&&!data.config.enabled) data.config.read=true;
    mcpSettings.data=data;mcpSettings.error='';
  } catch(e) {if(request===mcpSettings.request&&config) mcpSettings.error=e.message;}
  if(request===mcpSettings.request) renderMCPSettings();
}
async function refreshMCPUsers(button) {
  const request=mcpSettings.request;button.disabled=true;
  try {
    const users=await api('mcp/sunpanel-users');
    if(request!==mcpSettings.request||!config||page!=='settings') return;
    const select=$('#mcp-form').elements.sunpanel_user_id,selected=select.value;
    mcpSettings.users=users;
    select.innerHTML='<option value="0">不授权 Sun-Panel</option>'+users.map(user=>`<option value="${Number(user.id)}">${esc(user.name||user.username)} (${esc(user.username)})</option>`).join('');
    select.value=selected;
    if(!select.value) select.value='0';
    mcpSettings.error='';$('.error',$('#mcp-form')).textContent='';
  } catch(e) {if(request===mcpSettings.request&&$('#mcp-form')) $('.error',$('#mcp-form')).textContent=e.message;}
  finally {button.disabled=false;}
}
async function saveMCPSettings(form,rotate=false) {
  const request=mcpSettings.request;
  const e=form?.elements;
  const payload=rotate?{revision:mcpSettings.data.revision}:{revision:mcpSettings.data.revision,config:{enabled:e.enabled.checked,read:e.read.checked,write:e.write.checked,maintenance:e.maintenance.checked,sunpanel_user_id:Number(e.sunpanel_user_id.value)}};
  const data=await api(rotate?'mcp/rotate-token':'mcp',rotate?'POST':'PUT',payload);
  if(request!==mcpSettings.request||!config||page!=='settings') return;
  mcpSettings.token=data.token||mcpSettings.token;data.token='';mcpSettings.data=data;mcpSettings.error='';
  renderMCPSettings();
  // MCP authorization shares the store revision with ordinary configuration.
  const fresh=await api('config');
  if(request!==mcpSettings.request||!config||page!=='settings') return;
  applyConfig(fresh);
  renderMCPSettings();toast(rotate?'新令牌已生成，旧令牌已失效':'MCP 设置已保存');
}
async function mcpSettingsAction(button) {
  if(button.dataset.action==='refresh-mcp') return loadMCPSettings();
  if(button.dataset.action==='mcp-refresh-users') return refreshMCPUsers(button);
  if(button.dataset.action==='mcp-toggle-token') {
    const field=$('#mcp-token'),show=field.type==='password';field.type=show?'text':'password';button.textContent=show?'隐藏令牌':'显示令牌';button.setAttribute('aria-pressed',String(show));return;
  }
  if(button.dataset.action==='mcp-copy-token') {
    try {
      if(navigator.clipboard) await navigator.clipboard.writeText(mcpSettings.token);
      else {
        const field=$('#mcp-token'),type=field.type;let copied=false;
        try {field.type='text';field.focus();field.select();copied=document.execCommand('copy');}
        finally {field.type=type;}
        if(!copied) throw new Error('copy denied');
      }
      toast('令牌已复制');
    } catch {toast('浏览器未允许复制，请点击显示令牌后手动复制');}
    return;
  }
  if(button.dataset.action==='mcp-rotate-token') {
    if(!confirm('重置后所有使用旧令牌的 AI 客户端都会失去访问权限。确认重置？')) return;
    button.disabled=true;
    try {await saveMCPSettings(null,true);}
    catch(e) {toast(e.message);}
    finally {button.disabled=false;}
  }
}
