'use strict';
const mcpSettings={data:null,users:[],error:'',request:0,token:''};
function resetMCPSettings() {
  mcpSettings.request++;mcpSettings.data=null;mcpSettings.users=[];mcpSettings.error='';mcpSettings.token='';
  $('#mcp-token')?.remove();
}
function mcpClientConfig() {
  return JSON.stringify({mcpServers:{gatehome:{url:location.origin+'/mcp',headers:{Authorization:'Bearer YOUR_API_TOKEN'}}}},null,2);
}
function mcpHTML() {
  return heading('AI MANAGEMENT','AI 管理','通过 MCP 连接外部 AI，统一管理 GateHome 与 Sun-Panel。','<button class="secondary" data-action="refresh-mcp">'+icon('refresh')+'刷新设置</button>')+
    `<ol class="mcp-steps" aria-label="AI 接入步骤"><li><span aria-hidden="true">1</span><div><b>创建 API Token</b><p>生成并复制专用访问凭据。</p></div></li><li><span aria-hidden="true">2</span><div><b>开启服务并授权</b><p>选择 AI 可以查询、修改或维护的内容。</p></div></li><li><span aria-hidden="true">3</span><div><b>连接 AI 客户端</b><p>填写 MCP 地址与 API Token，即可调用工具。</p></div></li></ol>`+
    `<div id="mcp-settings">${mcpSettingsHTML()}</div>`;
}
function mcpSettingsHTML() {
  const data=mcpSettings.data,c=data?.config,endpoint=location.origin+'/mcp';
  const options=mcpSettings.users.map(user=>`<option value="${Number(user.id)}" ${user.id===c?.sunpanel_user_id?'selected':''}>${esc(user.name||user.username)} (${esc(user.username)})</option>`).join('');
  const missing=c?.sunpanel_user_id&&!mcpSettings.users.some(user=>user.id===c.sunpanel_user_id)?`<option value="${Number(c.sunpanel_user_id)}" selected>已绑定账号 #${Number(c.sunpanel_user_id)} · 刷新确认</option>`:'';
  const authorization=panel('服务与授权','保存后立即生效，可随时调整 AI 的管理范围。',!c?`<div class="panel-body"><p role="status">${esc(mcpSettings.error||'正在读取 MCP 设置…')}</p><button class="secondary" data-action="refresh-mcp">重新读取</button></div>`:`<form id="mcp-form" class="panel-body">
    <div class="toggle-row"><div><b>启用 MCP 服务</b><p>关闭后，API Token 无法调用管理工具。</p></div><label class="switch"><input name="enabled" type="checkbox" ${c.enabled?'checked':''}><span></span><span class="sr-only">启用 MCP 服务</span></label></div>
    <fieldset class="mcp-permissions"><legend>AI 管理权限</legend>
      <label class="check-label"><input name="read" type="checkbox" ${c.read?'checked':''}><span><b>查询</b><small>查看配置、运行状态、日志与首页内容。</small></span></label>
      <label class="check-label"><input name="write" type="checkbox" ${c.write?'checked':''}><span><b>配置修改</b><small>修改反代、网站、分组，执行联动与任务。</small></span></label>
      <label class="check-label"><input name="maintenance" type="checkbox" ${c.maintenance?'checked':''}><span><b>维护操作</b><small>重启服务、更新版本、恢复已检查的备份。</small></span></label>
    </fieldset>
    <label>Sun-Panel 管理账号<select name="sunpanel_user_id"><option value="0">不授权 Sun-Panel</option>${options}${missing}</select><small>绑定后仅管理该账号的桌面，不影响其他用户。</small></label>
    <button type="button" class="secondary mcp-refresh-users" data-action="mcp-refresh-users">${icon('refresh')}刷新 Sun-Panel 账号</button>
    <p class="form-note">默认仅勾选查询。AI 无法读取已保存的密码与私钥，也不能修改自己的授权。</p><p class="error" role="alert">${esc(mcpSettings.error)}</p>
    <div class="form-actions"><button type="submit" class="primary">${icon('save')}保存授权设置</button></div>
  </form>`,`<span class="badge ${c?.enabled?'':'gray'}">${c?c.enabled?'已启用':'未启用':'读取中'}</span>`);
  const token=panel('API Token','外部 AI 的专用凭据，与管理密码和 GitHub Token 分开。',`<div class="panel-body">
    <p data-visible-note class="form-note mcp-first-note">${data?.token_configured?'已创建 API Token。完整 Token 只在创建或重新生成后显示一次。':'创建 API Token 后，请开启服务、选择权限并保存。首次启用并保存也会自动创建 Token。'}</p>
    ${mcpSettings.token?`<label class="mcp-new-token">新 API Token（请立即保存）<input id="mcp-token" type="password" readonly autocomplete="off" spellcheck="false" value="${esc(mcpSettings.token)}"></label><div class="mcp-inline-actions"><button type="button" class="secondary" data-action="mcp-copy-token">复制 API Token</button><button type="button" class="secondary" data-action="mcp-toggle-token" aria-pressed="false">显示 Token</button></div><p class="form-note">离开此页或刷新浏览器后不再显示。如已丢失，请重新生成。</p>`:''}
    <div class="form-actions"><button type="button" class="${data?.token_configured?'secondary':'primary'}" data-action="mcp-rotate-token" ${!c?'disabled':''}>${icon(data?.token_configured?'refresh':'plus')}${data?.token_configured?'重新生成 API Token':'创建 API Token'}</button></div>
  </div>`,`<span class="badge ${data?.token_configured?'':'gray'}">${data?.token_configured?'已创建':'未创建'}</span>`);
  const guide=panel('AI 接入指引','在支持 Streamable HTTP 和 Bearer 认证的 AI 客户端中添加 MCP 服务。',`<div class="panel-body">
    <label for="mcp-endpoint">MCP 连接地址</label><div class="mcp-copy-row"><input id="mcp-endpoint" class="mono" readonly value="${esc(endpoint)}" spellcheck="false"><button type="button" class="secondary" data-action="mcp-copy-endpoint">复制地址</button></div>
    <ul class="info-list"><li><span>连接方式</span><b>Streamable HTTP</b></li><li><span>认证方式</span><b>Bearer / API Token</b></li></ul>
    <p class="form-note">客户端有 Token 输入框时填入 API Token；使用自定义请求头时，设置 <code>Authorization: Bearer YOUR_API_TOKEN</code>，替换占位符。</p>
    <details class="mcp-client-guide"><summary>查看客户端配置示例</summary><p class="form-note">将 YOUR_API_TOKEN 替换为刚创建的 Token。不同客户端的配置字段可能不同。</p><label class="sr-only" for="mcp-client-config">MCP 客户端配置示例</label><textarea id="mcp-client-config" class="mono" rows="10" readonly spellcheck="false">${esc(mcpClientConfig())}</textarea><button type="button" class="secondary" data-action="mcp-copy-config">复制配置示例</button></details>
    <p class="form-note">连接地址使用 GateHome 管理端口。远程 AI 需能访问此地址；通过公网接入时，换成可访问的 HTTPS 管理域名，并保留 /mcp。仅支持 OAuth 登录的客户端不能直接接入。</p>
  </div>`);
  const prompt=panel('AI 管理提示词','连接 MCP 后，将提示词粘贴给 AI，再说明你希望修改的内容。',`<div class="panel-body"><label for="mcp-management-prompt">编辑与联动参考指引</label><textarea id="mcp-management-prompt" class="mcp-management-prompt" rows="10" readonly spellcheck="false">${esc(data?.guide||'读取设置后可查看和复制提示词…')}</textarea><p class="form-note">涵盖完整分区保存、版本冲突、网站编辑、反代联动与维护验证。支持 MCP 提示词的客户端也可直接选择 <code>gatehome_management_guide</code>；初始化说明会提供同一份指引。</p><div class="form-actions"><button type="button" class="primary" data-action="mcp-copy-prompt" ${!data?.guide?'disabled':''}>复制 AI 管理提示词</button></div></div>`);
  return `<div class="mcp-layout"><div>${authorization}${prompt}</div><div>${token}${guide}</div></div>`;
}
function mcpDefaults(data) {
  const c=data.config;
  if(!c.enabled&&!c.read&&!c.write&&!c.maintenance) c.read=true;
  return data;
}
function mcpFormConfig(form) {
  const e=form.elements;
  return {enabled:e.enabled.checked,read:e.read.checked,write:e.write.checked,maintenance:e.maintenance.checked,sunpanel_user_id:Number(e.sunpanel_user_id.value)};
}
function renderMCPSettings(draft) {
  const target=$('#mcp-settings');
  if(!target||page!=='mcp'||!config) return;
  target.innerHTML=mcpSettingsHTML();hydrateIcons(target);
  if(draft) {
    const e=$('#mcp-form').elements;
    for(const name of ['enabled','read','write','maintenance']) e[name].checked=draft[name];
    e.sunpanel_user_id.value=String(draft.sunpanel_user_id);
  }
}
async function loadMCPSettings() {
  const request=++mcpSettings.request;
  try {
    const data=await api('mcp');
    if(request!==mcpSettings.request||!config||page!=='mcp') return;
    mcpSettings.data=mcpDefaults(data);mcpSettings.error='';
  } catch(e) {if(request===mcpSettings.request&&config) mcpSettings.error=e.message;}
  if(request===mcpSettings.request) renderMCPSettings();
}
async function refreshMCPUsers(button) {
  const request=mcpSettings.request;button.disabled=true;
  try {
    const users=await api('mcp/sunpanel-users');
    if(request!==mcpSettings.request||!config||page!=='mcp') return;
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
  const request=++mcpSettings.request,configured=mcpSettings.data.token_configured;
  const draft=rotate&&$('#mcp-form')?mcpFormConfig($('#mcp-form')):null;
  const payload=rotate?{revision:mcpSettings.data.revision}:{revision:mcpSettings.data.revision,config:mcpFormConfig(form)};
  const data=await api(rotate?'mcp/rotate-token':'mcp',rotate?'POST':'PUT',payload);
  if(request!==mcpSettings.request||!config||page!=='mcp') return;
  mcpSettings.token=data.token||mcpSettings.token;data.token='';mcpSettings.data=mcpDefaults(data);mcpSettings.error='';
  renderMCPSettings(draft);
  // MCP authorization shares the store revision with ordinary configuration.
  const fresh=await api('config');
  if(request!==mcpSettings.request||!config||page!=='mcp') return;
  applyConfig(fresh);
  renderMCPSettings(draft);toast(rotate?configured?'新 API Token 已生成，旧 Token 已失效':'API Token 已创建，请复制保存并开启服务':'MCP 授权设置已保存');
}
async function copyMCPText(value,field) {
  try {
    if(navigator.clipboard) await navigator.clipboard.writeText(value);
    else {
      const type=field.type;let copied=false;
      try {if(type==='password') field.type='text';field.focus();field.select();copied=document.execCommand('copy');}
      finally {if(type==='password') field.type=type;}
      if(!copied) throw new Error('copy denied');
    }
    toast('已复制');
  } catch {toast('浏览器未允许复制，请在对应输入框中手动复制');}
}
async function mcpSettingsAction(button) {
  if(button.dataset.action==='refresh-mcp') return loadMCPSettings();
  if(button.dataset.action==='mcp-refresh-users') return refreshMCPUsers(button);
  if(button.dataset.action==='mcp-toggle-token') {
    const field=$('#mcp-token'),show=field.type==='password';field.type=show?'text':'password';button.textContent=show?'隐藏 Token':'显示 Token';button.setAttribute('aria-pressed',String(show));return;
  }
  if(button.dataset.action==='mcp-copy-token') return copyMCPText(mcpSettings.token,$('#mcp-token'));
  if(button.dataset.action==='mcp-copy-endpoint') return copyMCPText(location.origin+'/mcp',$('#mcp-endpoint'));
  if(button.dataset.action==='mcp-copy-config') return copyMCPText(mcpClientConfig(),$('#mcp-client-config'));
  if(button.dataset.action==='mcp-copy-prompt') return copyMCPText(mcpSettings.data.guide,$('#mcp-management-prompt'));
  if(button.dataset.action==='mcp-rotate-token') {
    if(mcpSettings.data.token_configured&&!confirm('重新生成后，所有使用旧 API Token 的 AI 客户端都会失去访问权限。确认重新生成？')) return;
    button.disabled=true;button.setAttribute('aria-busy','true');
    try {await saveMCPSettings(null,true);}
    catch(e) {toast(e.message);}
    finally {button.disabled=false;button.removeAttribute('aria-busy');}
  }
}
