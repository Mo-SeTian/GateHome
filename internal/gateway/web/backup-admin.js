'use strict';
const remoteBackups={data:null,files:[],error:'',listError:'',loading:false,request:0,listRequest:0,generation:0,timer:null};
function resetRemoteBackups() {remoteBackups.request++;remoteBackups.listRequest++;remoteBackups.generation++;clearTimeout(remoteBackups.timer);remoteBackups.timer=null;remoteBackups.data=null;remoteBackups.files=[];remoteBackups.error='';remoteBackups.listError='';remoteBackups.loading=false;}
function backupsHTML() {
 const maintenance=maintenanceHTML();
 return heading('BACKUP & RESTORE','备份与恢复','整站备份覆盖 GateHome 与 Sun-Panel 配置、账户、图片及持久化数据。','<button class="secondary" data-action="refresh-remote-backups">'+icon('refresh')+'刷新备份</button>')+
  `<div class="backup-layout"><div>${maintenance.backups}</div><div id="webdav-settings">${webDAVSettingsHTML()}</div></div><div id="webdav-files">${webDAVFilesHTML()}</div>`;
}
function webDAVSettingsHTML() {
 const data=remoteBackups.data,c=data?.config;
 if(!c) return panel('WebDAV 备份','配置远程存储、加密密码、保留份数与备份计划。',`<div class="panel-body"><p role="status">${esc(remoteBackups.error||'正在读取设置…')}</p><button class="secondary" data-action="refresh-remote-backups">重新读取</button></div>`);
 const weekdays=['星期日','星期一','星期二','星期三','星期四','星期五','星期六'];
 return panel('WebDAV 与定时备份','连接设置与加密密码保存在本机受限配置中，只有保存后才用于备份。',`<form id="webdav-backup-form" data-revision="${Number(data.revision)}" class="panel-body"><div class="form-grid">
  <label class="span-2">WebDAV 目录地址<input name="url" type="url" value="${esc(c.url)}" placeholder="https://storage.example.com/dav/GateHome/" spellcheck="false"><small>填写最终备份目录；支持内网 HTTP 和公网 HTTPS，不在地址中填写账号密码。</small></label>
  <label>WebDAV 账号<input name="username" value="${esc(c.username)}" autocomplete="off" maxlength="255"></label><label>WebDAV 密码<input name="password" type="password" autocomplete="new-password" placeholder="${data.password_configured?'已配置，留空保留':'可选'}"><small>变更地址或账号后，需要重新填写存储密码。</small></label>
  <label class="span-2 check-label"><input name="clear_password" type="checkbox">清除 WebDAV 密码（匿名连接）</label>
  <label>备份加密密码<input name="backup_password" type="password" autocomplete="new-password" placeholder="${data.backup_password_configured?'已配置，留空保留':'远程备份需要加密密码'}"><small>包含账户与凭据，远程备份必须设置密码；修改后旧备份仍使用原密码。</small></label><label>再次输入加密密码<input name="confirm_backup_password" type="password" autocomplete="new-password"></label>
  <label>最多保留份数<input name="keep" type="number" min="1" max="100" step="1" value="${Number(c.keep)}" required><small>新备份上传并校验成功后清理，仅处理本实例的备份。</small></label>
  <label class="check-label"><input name="schedule_enabled" type="checkbox" ${c.schedule_enabled?'checked':''}>启用定时备份</label>
  <label>备份频率<select name="schedule_mode"><option value="daily" ${c.schedule_mode==='daily'?'selected':''}>每天</option><option value="weekly" ${c.schedule_mode==='weekly'?'selected':''}>每周</option></select></label><label>每周日期<select name="weekday">${weekdays.map((text,index)=>`<option value="${index}" ${index===c.weekday?'selected':''}>${text}</option>`).join('')}</select></label>
  <label>备份时间<input name="time" type="time" value="${esc(c.time)}" required></label><label>时区<input name="timezone" value="${esc(c.timezone)}" required placeholder="Asia/Shanghai"><small>按该时区运行，不依赖 Docker 或 Linux 系统时区。</small></label>
  </div><p class="error" role="alert">${esc(remoteBackups.error)}</p><div class="form-actions"><button class="primary" type="submit" ${data.busy?'disabled':''}>保存 WebDAV 设置</button><button class="secondary" type="button" data-action="test-webdav-backup" ${!c.url||data.busy?'disabled':''}>测试连接</button><button class="secondary" type="button" data-action="run-webdav-backup" ${!c.url||data.busy?'disabled':''}>立即备份</button></div></form><div class="panel-body remote-backup-status" role="status" aria-live="polite">${webDAVStatusHTML()}</div>`);
}
function webDAVStatusHTML() {
 const data=remoteBackups.data,c=data?.config;
 if(!data) return '';
 const phase={snapshot:'正在生成一致快照',encrypt:'正在加密备份',upload:'正在上传、校验及清理',done:'备份完成',failed:'备份失败'}[data.job?.phase]||'等待备份';
 return `<ul class="info-list"><li><span>运行状态</span><b>${esc(phase)}</b></li><li><span>最近成功</span><b>${date(data.last_success)}</b></li><li><span>下次计划</span><b>${data.next_run?esc(new Date(data.next_run).toLocaleString('zh-CN',{timeZone:c.timezone,hour12:false}))+' · '+esc(c.timezone):'未启用定时备份'}</b></li></ul>${data.last_error?`<p class="error" role="alert">${esc(data.last_error)}</p>`:''}`;
}
function webDAVFilesHTML() {
 const rows=remoteBackups.files;
 return panel('远程备份','列出该目录中的 GateHome 完整备份；重装后也能下载或检查恢复，旧备份仍需原加密密码。',`<div class="panel-body"><p class="error" role="alert">${esc(remoteBackups.listError)}</p>${remoteBackups.loading?'<p role="status">正在读取远程目录…</p>':''}${rows.length?`<div class="table-wrap"><table><thead><tr><th>备份时间</th><th>文件 / 大小</th><th>操作</th></tr></thead><tbody>${rows.map(file=>`<tr><td>${date(file.created_at)}</td><td><span class="mono remote-backup-name">${esc(file.name)}</span><small>${(Number(file.size)/1048576).toFixed(2)} MiB</small></td><td><div class="table-actions"><button type="button" class="text-button" data-action="download-webdav-backup" data-file="${esc(file.name)}">下载</button><button type="button" class="text-button" data-action="inspect-webdav-backup" data-file="${esc(file.name)}">检查恢复</button></div></td></tr>`).join('')}</tbody></table></div>`:'<p>尚未读取到远程 GateHome 备份。</p>'}<div class="form-actions"><button type="button" class="secondary" data-action="list-webdav-backups" ${remoteBackups.loading||!remoteBackups.data?.config.url?'disabled':''}>读取远程备份</button></div></div>`);
}
function renderRemoteBackups() {
 if(!config||page!=='backups') return;
 const settings=$('#webdav-settings'),files=$('#webdav-files');
 if(settings&&!$('#webdav-backup-form')?.contains(document.activeElement)&&$('#webdav-backup-form')?.dataset.dirty!=='true') {settings.innerHTML=webDAVSettingsHTML();hydrateIcons(settings);}
 const form=$('#webdav-backup-form');if(form&&remoteBackups.data) form.querySelectorAll('button').forEach(button=>{button.disabled=remoteBackups.data.busy||(['test-webdav-backup','run-webdav-backup'].includes(button.dataset.action)&&!remoteBackups.data.config.url);});
 const state=$('.remote-backup-status');if(state) state.innerHTML=webDAVStatusHTML();
 if(files) {files.innerHTML=webDAVFilesHTML();hydrateIcons(files);prepareTables(files);}
}
async function loadRemoteBackups(withFiles=false) {
 const request=++remoteBackups.request;
 try {
  const data=await api('backups/webdav');
  if(request!==remoteBackups.request||!config||page!=='backups') return;
  const completed=remoteBackups.data?.busy&&!data.busy;remoteBackups.data=data;remoteBackups.error='';renderRemoteBackups();
  clearTimeout(remoteBackups.timer);
  if(data.busy) remoteBackups.timer=setTimeout(()=>loadRemoteBackups(),1500);
  if((withFiles||completed)&&data.config.url) await listRemoteBackups();
 } catch(error) {if(request===remoteBackups.request&&config&&page==='backups'){remoteBackups.error=error.message;renderRemoteBackups();}}
}
async function listRemoteBackups() {
 const request=++remoteBackups.listRequest,generation=remoteBackups.generation;remoteBackups.loading=true;remoteBackups.listError='';renderRemoteBackups();
 try {const files=await api('backups/webdav/files');if(request!==remoteBackups.listRequest||generation!==remoteBackups.generation||!config||page!=='backups') return;remoteBackups.files=files;}
 catch(error){if(request===remoteBackups.listRequest&&generation===remoteBackups.generation)remoteBackups.listError=error.message;}
 finally {if(request===remoteBackups.listRequest&&generation===remoteBackups.generation){remoteBackups.loading=false;renderRemoteBackups();}}
}
async function saveWebDAVSettings(form) {
 const generation=remoteBackups.generation;
 const e=form.elements,password=e.password.value,backupPassword=e.backup_password.value;
 if(backupPassword!==e.confirm_backup_password.value) throw new Error('两次备份加密密码不一致');
 const payload={revision:Number(form.dataset.revision),config:{url:e.url.value.trim(),username:e.username.value,keep:Number(e.keep.value),schedule_enabled:e.schedule_enabled.checked,schedule_mode:e.schedule_mode.value,time:e.time.value,timezone:e.timezone.value.trim(),weekday:Number(e.weekday.value)}};
 if(password||e.clear_password.checked) payload.password=e.clear_password.checked?'':password;
 if(backupPassword) payload.backup_password=backupPassword;
 await api('backups/webdav','PUT',payload);if(generation!==remoteBackups.generation||!config||page!=='backups') return;form.dataset.dirty='false';e.password.value='';e.backup_password.value='';e.confirm_backup_password.value='';
 const fresh=await api('config');if(generation!==remoteBackups.generation||!config||page!=='backups') return;applyConfig(fresh);remoteBackups.listRequest++;remoteBackups.files=[];remoteBackups.loading=false;await loadRemoteBackups();toast('WebDAV 备份设置已保存');
}
async function remoteBackupAction(button) {
 const action=button.dataset.action;
 if(action==='refresh-remote-backups') return loadRemoteBackups(true);
 if(action==='list-webdav-backups') return listRemoteBackups();
 if(action==='inspect-webdav-backup') {
  $('#webdav-restore-form').reset();$('#webdav-restore-form').elements.name.value=button.dataset.file;$('#webdav-restore-file').textContent=button.dataset.file;openDialog($('#webdav-restore-dialog'));return;
 }
 button.disabled=true;button.setAttribute('aria-busy','true');
 try {
  if(action==='test-webdav-backup') {const result=await api('backups/webdav/test','POST',{});toast(result.message);}
  if(action==='run-webdav-backup') {await api('backups/webdav/run','POST',{});await loadRemoteBackups();toast('整站备份已开始');}
  if(action==='download-webdav-backup') {
   const response=await fetch('/api/backups/webdav/download',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json','X-Gatehouse-Request':'1'},body:JSON.stringify({name:button.dataset.file})});
   if(!response.ok){const result=await response.json();throw new Error(result.error||'远程备份下载失败');}
   const url=URL.createObjectURL(await response.blob()),link=document.createElement('a');link.href=url;link.download=button.dataset.file;document.body.append(link);link.click();link.remove();setTimeout(()=>URL.revokeObjectURL(url),30000);
  }
 } catch(error){toast(error.message);} finally{button.disabled=false;button.removeAttribute('aria-busy');}
}
async function inspectWebDAVBackup(form) {
 const generation=remoteBackups.generation;
 const payload={name:form.elements.name.value};if(form.elements.password.value) payload.password=form.elements.password.value;
 const preview=await api('backups/webdav/inspect','POST',payload);form.elements.password.value='';if(generation!==remoteBackups.generation||!config||page!=='backups'||!$('#webdav-restore-dialog').open||form.elements.name.value!==payload.name) return;closeDialog($('#webdav-restore-dialog'));maintenancePreview={...preview,kind:'restore'};render();toast('远程备份已检查，请审阅预览后确认恢复');
}

document.addEventListener('input',event=>{if(event.target.closest('#webdav-backup-form')) event.target.closest('form').dataset.dirty='true';});
document.addEventListener('change',event=>{if(event.target.closest('#webdav-backup-form')) event.target.closest('form').dataset.dirty='true';});
