function sunPanelLaunchURL(c=config.sunpanel||{}) {
  const fallback=`http://${location.hostname}:${Number(c.port)||16680}/`, value=(c.launch_url||'').trim();
  if(!value)return fallback;
  try {
    const localPath=value.startsWith('/')&&!value.startsWith('//'), u=new URL(value,localPath?location.origin:undefined);
    return !/[\s\\]/.test(value)&&['http:','https:'].includes(u.protocol)&&u.hostname&&!u.username&&!u.password?u.href:fallback;
  } catch {return fallback;}
}
function sunPanelHTML() {
  const c=config.sunpanel||{}, enabled=!!c.enabled, port=Number(c.port)||16680;
  const launchURL=sunPanelLaunchURL(c), defaultURL=sunPanelLaunchURL({port});
  const storageSupported=!!status.sunpanel_storage_supported;
  const logo='<span class="sunpanel-logo" aria-hidden="true"><span class="sunpanel-logo-sun"></span><span class="sunpanel-logo-panel"></span></span>';
  return heading('START PAGE','浏览器首页','把常用服务、搜索和服务器信息放进一个清晰的新标签页。')+
    `<div class="sunpanel-page">
      <section class="sunpanel-hero" aria-labelledby="sunpanel-title">
        <div class="sunpanel-identity">${logo}<div><span class="eyebrow">SUN-PANEL · V1.3.0</span><h2 id="sunpanel-title">Sun-Panel 浏览器首页</h2><p>桌面、账号、分组和导入导出都在 Sun-Panel 内管理。</p><div class="sunpanel-runtime ${enabled?'is-ready':'is-off'}"><i class="status-dot"></i><span data-sunpanel-status>${enabled?'服务已启用':'服务未启用'}</span></div></div></div>
        <button class="sunpanel-status-toggle ${enabled?'is-enabled':''}" type="button" data-sunpanel-toggle aria-pressed="${enabled}" aria-controls="sunpanel-enabled"><span class="sunpanel-toggle-dot"></span><span data-sunpanel-toggle-label>${enabled?'已启用':'启用 Sun-Panel'}</span><small data-sunpanel-toggle-hint>${enabled?'点击关闭服务':'点击启用服务'}</small></button>
      </section>
      <div class="sunpanel-launch-row">
        <a class="sunpanel-launch ${enabled?'':'is-disabled'}" href="${enabled?esc(launchURL):'#'}" ${enabled?'target="_blank" rel="noopener"':'aria-disabled="true" tabindex="-1"'} data-sunpanel-launch data-sunpanel-open>${logo}<span><b>打开 Sun-Panel</b><small data-sunpanel-launch-note>${enabled?'在新标签页打开 · 使用下方访问地址':'启用后可打开浏览器首页'}</small></span>${icon('external-link')}</a>
        <div class="sunpanel-url-card"><span class="eyebrow">访问地址</span><code data-sunpanel-url>${esc(launchURL)}</code><small>可在下方配置；留空时使用当前主机和独立端口。</small></div>
      </div>
      ${storageSupported?'':'<div class="sunpanel-compatibility" role="status"><b>当前启动器尚未支持 Sun-Panel 完整回滚</b><span>普通重启仍可用；要让整站备份、恢复和失败回滚包含 Sun-Panel，请重新运行最新 Linux 安装脚本，或用最新 Dockerfile 重建镜像。</span></div>'}
      <div class="sunpanel-columns">
        ${panel('运行设置','访问地址保存后立即生效；端口和启用状态修改后需要重启 Gatehouse。',`<div class="panel-body"><form id="sunpanel-form"><input id="sunpanel-enabled" name="enabled" type="checkbox" class="sr-only" ${enabled?'checked':''}><label class="sr-only" for="sunpanel-enabled">启用 Sun-Panel</label><div class="form-grid"><label class="sunpanel-port-field span-2">独立访问端口<input name="port" type="number" min="1" max="65535" required value="${port}"><small>浏览器或新标签页访问服务器 IP 加此端口。Docker bridge 需要同步发布端口。</small></label><label class="span-2">新标签页访问地址（可选）<input name="launch_url" inputmode="url" maxlength="2048" value="${esc(c.launch_url||'')}" placeholder="${esc(defaultURL)}"><small>支持完整 HTTP(S) 地址或以 / 开头的本站路径，例如 /sunpanel/。留空默认使用当前地址栏主机和上方端口。</small></label></div><p class="form-note">Sun-Panel 使用独立账号登录，和 Gatehouse 管理账号分开。</p><p class="error" role="alert"></p><div class="form-actions"><button class="primary" type="submit">${icon('save')}保存运行设置</button></div></form></div>`)}
        ${panel('使用说明','把它当作浏览器新标签页入口，而不是 Gatehouse 的普通配置项。',`<div class="panel-body"><ul class="sunpanel-guide"><li><span>${icon('external-link')}</span><div><b>打开首页</b><small>点击上方“打开 Sun-Panel”，或访问服务器 IP 加独立端口。</small></div></li><li><span>${icon('login')}</span><div><b>独立登录</b><small>首次账号和密码遵循 Sun-Panel 部署说明，登录后可在其中修改。</small></div></li><li><span>${icon('download')}</span><div><b>数据与备份</b><small>Gatehouse 整站备份包含 Sun-Panel 数据，自带导入导出也继续可用。</small></div></li></ul></div>`)}
      </div>
      <section class="sunpanel-preview panel" aria-labelledby="sunpanel-preview-title"><div class="panel-heading"><div><h2 id="sunpanel-preview-title">页面预览</h2><p>${enabled?'在管理端直接查看 Sun-Panel，完整使用可点击上方按钮打开新标签页。':'启用 Sun-Panel 后，这里会显示页面预览。'}</p></div>${enabled?`<a class="secondary" href="${esc(launchURL)}" target="_blank" rel="noopener" data-sunpanel-open>新标签页打开</a>`:''}</div>${enabled?'<iframe class="sunpanel-frame" src="/sunpanel/" title="Sun-Panel 浏览器首页预览" referrerpolicy="no-referrer"></iframe>':'<div class="sunpanel-preview-empty"><span class="sunpanel-preview-icon">'+logo+'</span><b>首页尚未启用</b><p>点击上方状态按钮启用后，再保存运行设置并重启服务。</p></div>'}</section>
    </div>`;
}
function syncSunPanelPreview(form) {
  const enabled=form.elements.enabled.checked, toggle=document.querySelector('[data-sunpanel-toggle]'), status=document.querySelector('[data-sunpanel-status]'), runtime=document.querySelector('.sunpanel-runtime'), label=document.querySelector('[data-sunpanel-toggle-label]'), hint=document.querySelector('[data-sunpanel-toggle-hint]'), launch=document.querySelector('[data-sunpanel-launch]'), note=document.querySelector('[data-sunpanel-launch-note]'), url=document.querySelector('[data-sunpanel-url]'), port=Number(form.elements.port.value)||16680;
  const launchURL=sunPanelLaunchURL({port,launch_url:form.elements.launch_url.value});
  if(toggle){toggle.classList.toggle('is-enabled',enabled);toggle.setAttribute('aria-pressed',String(enabled));}
  if(runtime){runtime.classList.toggle('is-ready',enabled);runtime.classList.toggle('is-off',!enabled);}
  if(status) status.textContent=enabled?'服务已启用':'服务未启用';
  if(label) label.textContent=enabled?'已启用':'启用 Sun-Panel';
  if(hint) hint.textContent=enabled?'点击关闭服务':'点击启用服务';
  for(const link of document.querySelectorAll('[data-sunpanel-open]')){link.setAttribute('aria-disabled',String(!enabled));if(enabled){link.href=launchURL;link.target='_blank';link.rel='noopener';link.removeAttribute('tabindex');}else{link.href='#';link.removeAttribute('target');link.removeAttribute('rel');link.tabIndex=-1;}}
  if(launch)launch.classList.toggle('is-disabled',!enabled);
  if(note) note.textContent=enabled?'在新标签页打开 · 使用下方访问地址':'启用后可打开浏览器首页';
  if(url) url.textContent=launchURL;
  form.elements.launch_url.placeholder=sunPanelLaunchURL({port});
}
document.addEventListener('input',event=>{const form=event.target.closest('#sunpanel-form');if(form&&['port','launch_url'].includes(event.target.name))syncSunPanelPreview(form);});
document.addEventListener('click',event=>{const toggle=event.target.closest('[data-sunpanel-toggle]');if(!toggle)return;const form=document.querySelector('#sunpanel-form');if(!form)return;form.elements.enabled.checked=!form.elements.enabled.checked;syncSunPanelPreview(form);});
document.addEventListener('submit',async event=>{
  const form=event.target;
  if(form.id!=='sunpanel-form')return;
  event.preventDefault();
  const button=form.querySelector('button[type="submit"]');
  button.disabled=true;
  try {
    const next=structuredClone(config);
    next.sunpanel={enabled:form.elements.enabled.checked,port:Number(form.elements.port.value),launch_url:form.elements.launch_url.value.trim()};
    await save(next);
    render();
  } catch(error) {form.querySelector('.error').textContent=error.message;}
  finally {button.disabled=false;}
});
