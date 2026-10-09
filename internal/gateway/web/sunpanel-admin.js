function sunPanelHTML() {
  const c=config.sunpanel;
  return panel('Sun-Panel 浏览器首页','基于 Sun-Panel v1.3.0；桌面、账号与导入导出在下方页面中管理。',`<div class="panel-body"><form id="sunpanel-form"><label class="check"><input name="enabled" type="checkbox" ${c.enabled?'checked':''}>启用 Sun-Panel</label><label>独立访问端口<input name="port" type="number" min="1" max="65535" required value="${Number(c.port)||16680}"></label><p class="form-note">修改开关或端口后需重启服务。Docker bridge 还需映射同一端口。Sun-Panel 使用独立账号登录。</p><p class="error" role="alert"></p><div class="form-actions"><button class="primary" type="submit">保存设置</button>${c.enabled?'<a class="button secondary" href="/sunpanel/" target="_blank" rel="noopener">新标签页打开</a>':''}</div></form></div>`) + (c.enabled?'<iframe class="sunpanel-frame" src="/sunpanel/" title="Sun-Panel 浏览器首页" referrerpolicy="no-referrer"></iframe>':'');
}
document.addEventListener('submit',async event=>{
  const form=event.target;
  if(form.id!=='sunpanel-form')return;
  event.preventDefault();
  const button=form.querySelector('button[type="submit"]');
  button.disabled=true;
  try {
    const next=structuredClone(config);
    next.sunpanel={enabled:form.elements.enabled.checked,port:Number(form.elements.port.value)};
    await save(next);
    render();
  } catch(error) {form.querySelector('.error').textContent=error.message;}
  finally {button.disabled=false;}
});
