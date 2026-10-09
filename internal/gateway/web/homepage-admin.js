'use strict';
let homepageEditing=false,homepageDialogVersion=0,homepageDeleted=null;
const homepageClosedGroups=new Set();
function homepageValue() {return config.homepage?.port?config.homepage:{enabled:false,port:16680,public:true,title:'我的数字空间',tone:'forest',background:'',shade:50,compact:false,show_addresses:false,custom_css:'',search_engines:[{id:'baidu',name:'百度',url:'https://www.baidu.com/s?wd={query}'},{id:'google',name:'Google',url:'https://www.google.com/search?q={query}'}],groups:[]};}
function hpButton(label,action,attributes='',primary=false) {return `<button type="button" class="${primary?'primary':'secondary'}" data-hp-action="${action}" ${attributes}>${label}</button>`;}
function hpPageOptions(selected='') {return homepageValue().groups.flatMap(g=>g.pages.map(p=>`<option value="${esc(p.id)}" ${p.id===selected?'selected':''}>${esc(g.name)} / ${esc(p.name)}</option>`)).join('');}
function homepageHTML() {
  const h=homepageValue(),count=h.groups.reduce((n,g)=>n+g.pages.reduce((m,p)=>m+p.links.length,0),0),url=new URL(location.href);url.protocol='http:';url.port=h.port;url.pathname='/';url.search='';url.hash='';
  const actions=hpButton(icon('edit')+(homepageEditing?'完成编辑':'编辑桌面'),'edit')+hpButton(icon('settings')+'桌面设置','settings')+`<a class="secondary inline-icon-link" href="${esc(url.href)}" target="_blank" rel="noopener noreferrer">${icon('external-link')}打开首页</a>`;
  return heading('GATEHOMEPAGE','浏览器首页','独立端口的沉浸工作台，按组管理页面和应用链接。',actions)+
    `<div class="hp-summary">${badge(h.enabled?'已开启':'未开启',h.enabled?'':'gray')}<span>端口 ${h.port} · ${count} 个链接 · ${h.public?'访客可查看':'登录后查看'}</span>${status.restart_required?'<span class="badge amber">监听变更重启后生效</span>':''}</div>`+
    (homepageEditing?`<div class="hp-editbar">${hpButton(icon('plus')+'添加分组','group-new','',true)}${hpButton(icon('download')+'从 GateHome 导入','import',h.groups.length?'':'disabled')}<span>编辑仅作用于首页；反代配置保存后不自动覆盖已导入的链接。</span></div>`:'')+
    h.groups.map(g=>`<details class="panel hp-group" data-hp-group="${esc(g.id)}" ${homepageClosedGroups.has(g.id)?'':'open'}><summary><span>${icon('layers')}<b>${esc(g.name)}</b><small>${g.pages.length} 个页面</small></span>${icon('chevron-down')}</summary><div class="hp-group-body">${homepageEditing?`<div class="hp-group-actions">${hpButton('重命名分组','group-edit',`data-id="${esc(g.id)}"`)}${hpButton(icon('plus')+'添加页面','page-new',`data-group="${esc(g.id)}"`)}${hpButton(icon('trash')+'删除分组','group-delete',`data-id="${esc(g.id)}"`)}</div>`:''}${g.pages.map(p=>`<section class="hp-page"><div class="hp-page-heading"><div><h2>${esc(p.name)}</h2><p>${p.rows} 行 × ${p.columns} 列 · 手机端最多 ${p.mobile_columns} 列 · ${p.links.length} 个链接</p></div>${homepageEditing?`<div class="hp-page-actions">${hpButton('页面设置','page-edit',`data-id="${esc(p.id)}"`)}${hpButton(icon('plus')+'添加链接','link-new',`data-page="${esc(p.id)}"`,true)}${hpButton(icon('trash'),'page-delete',`data-id="${esc(p.id)}" aria-label="删除${esc(p.name)}" ${g.pages.length===1?'disabled':''}`)}</div>`:''}</div><div class="hp-link-list">${p.links.map((l,i)=>`<article class="hp-link"><span class="hp-image">${l.image?`<img src="/api/route-images/${esc(l.image)}" alt="" loading="lazy">`:icon('server')}</span><div class="hp-link-copy"><b>${esc(l.name)}${l.favorite?' · 置顶':''}</b><small>${esc(l.description||'应用链接')}</small><span>${l.lan?`<a href="${esc(safeServiceURL(l.lan))}" target="_blank" rel="noopener noreferrer">内网：${esc(l.lan)}</a>`:'未设置内网地址'}</span><span>${l.wan?`<a href="${esc(safeServiceURL(l.wan))}" target="_blank" rel="noopener noreferrer">外网：${esc(l.wan)}</a>`:'未设置外网地址'}</span></div>${homepageEditing?`<div class="hp-link-actions">${hpButton(icon('chevron-up'),'link-up',`data-id="${esc(l.id)}" aria-label="上移${esc(l.name)}" ${i===0?'disabled':''}`)}${hpButton(icon('chevron-down'),'link-down',`data-id="${esc(l.id)}" aria-label="下移${esc(l.name)}" ${i===p.links.length-1?'disabled':''}`)}${hpButton(icon('edit'),'link-edit',`data-id="${esc(l.id)}" aria-label="编辑${esc(l.name)}"`)}${hpButton(icon('trash'),'link-delete',`data-id="${esc(l.id)}" aria-label="删除${esc(l.name)}"`)}</div>`:''}</article>`).join('')||'<p class="form-note">此页面还没有链接。</p>'}</div></section>`).join('')}</div></details>`).join('')+
    (!h.groups.length?empty('创建你的数字空间','添加分组和页面，再导入反代服务或填写链接。',homepageEditing?hpButton('添加分组','group-new','',true):hpButton('编辑桌面','edit','',true)):'')+
    (homepageDeleted?`<p class="hp-undo">已删除链接 ${esc(homepageDeleted.link.name)} ${hpButton('撤销','undo')}</p>`:'')+
    '<p class="form-note">行列数决定每页容量，超出容量自动生成圆点续页。手机端根据屏幕宽度减少列数，并保留全部链接。</p>';
}
function hpFind(h,id) {for(const group of h.groups){if(group.id===id)return {group};for(const page of group.pages){if(page.id===id)return {group,page};const link=page.links.find(l=>l.id===id);if(link)return {group,page,link};}}return {};}
async function hpSave(h) {applyConfig(await api('homepage','PUT',{homepage:h,revision}));await refreshStatus();render();toast('首页配置已保存'+(status.restart_required?'，请重启以应用监听变更':''));}
function hpDialog(kind,title,fields,id='',group='') {
  let dialog=$('#homepage-dialog');if(!dialog){dialog=document.createElement('dialog');dialog.id='homepage-dialog';document.body.append(dialog);}
  homepageDialogVersion++;
  dialog.innerHTML=`<form data-homepage="true" data-kind="${kind}" data-id="${esc(id)}" data-group="${esc(group)}"><div class="dialog-heading"><div><span class="eyebrow">GATEHOMEPAGE</span><h2>${title}</h2></div><button type="button" class="icon-button" data-action="close-dialog" aria-label="关闭">${icon('close')}</button></div><div class="dialog-body">${fields}<p class="error" role="alert"></p></div><div class="dialog-actions"><button type="button" class="secondary" data-action="close-dialog">取消</button><button type="submit" class="primary">保存</button></div></form>`;
  hydrateIcons(dialog);openDialog(dialog);
}
function hpImageFields(id,background=false) {
  return `<fieldset class="hp-image-fields"><legend>${background?'背景图片':'链接图片'}</legend><input type="hidden" name="image" value="${esc(id||'')}"><div class="hp-image-preview">${id?`<img src="/api/${background?'homepage-backgrounds':'route-images'}/${esc(id)}" alt="${background?'背景':'图标'}预览">`:'<span>尚未选择图片</span>'}</div><label>上传图片<input name="image_file" type="file" accept="image/png,image/jpeg,image/webp,image/gif${background?'':',image/x-icon'}" data-hp-upload="${background?'background':'icon'}"></label><div class="hp-image-url"><label>图片 URL<input name="image_url" type="url" placeholder="https://example.com/image.png" maxlength="2048"></label>${hpButton(icon('download')+'获取图片','image-import',`data-background="${background}"`)}</div>${hpButton('移除图片','image-clear')}<p class="form-note">支持 PNG、JPEG、GIF、WebP${background?'':'、ICO'}，最大 5 MiB。URL 图片由服务器下载保存${background?'，背景优化到最长 2560 像素':'，图标优化到最长 128 像素'}，GIF 使用首帧。</p></fieldset>`;
}
function hpEnginePreview(image,url) {
  let src=image?'/api/route-images/'+image:'/search-generic.svg';
  if(!image)try{const host=new URL(url).hostname;if(['baidu.com','www.baidu.com'].includes(host))src='/search-baidu.svg';if(['google.com','www.google.com'].includes(host))src='/search-google.svg';}catch{}
  return `<img src="${esc(src)}" alt="搜索引擎图标预览">`;
}
function hpEngineRow(engine) {
  return `<div class="hp-search-engine" data-engine-id="${esc(engine.id)}"><div class="form-grid"><label>搜索引擎名称<input data-engine-name value="${esc(engine.name)}" maxlength="20" required></label><label>搜索地址<input data-engine-url type="url" value="${esc(engine.url)}" maxlength="2048" placeholder="https://example.com/search?q={query}" required></label></div>${hpButton(icon('trash')+'移除','engine-remove',`aria-label="移除${esc(engine.name||'搜索引擎')}"`)}<details class="hp-engine-icon-tools"><summary><span class="hp-engine-preview">${hpEnginePreview(engine.image,engine.url)}</span>自定义图标</summary><div class="hp-engine-custom"><input type="hidden" data-engine-image value="${esc(engine.image||'')}"><label>上传图标<input type="file" accept="image/png,image/jpeg,image/webp,image/gif,image/x-icon" data-hp-engine-upload></label><div class="hp-image-url"><label>图标 URL<input type="url" data-engine-image-url placeholder="https://example.com/icon.png" maxlength="2048"></label>${hpButton(icon('download')+'获取图标','engine-icon-import')}</div>${hpButton('恢复默认图标','engine-icon-clear')}<p class="form-note">支持 PNG、JPEG、GIF、WebP、ICO，最大 5 MiB。保存后图标与配置一起备份。</p></div></details></div>`;
}
function hpSearchFields(h) {
  return `<fieldset class="hp-search-fields"><legend>搜索引擎</legend><div class="hp-engine-list">${h.search_engines.map(hpEngineRow).join('')}</div>${hpButton(icon('plus')+'添加搜索引擎','engine-add')}<p class="form-note">用 {query} 表示关键词，例如 https://www.baidu.com/s?wd={query}。第一项作为默认引擎，每个浏览器记住自己的选择。支持 1–12 个引擎。</p></fieldset>`;
}
function hpOpen(action,button) {
  const h=homepageValue(),id=button.dataset.id,{group,page,link}=hpFind(h,id);
  if(action==='settings')return hpDialog('settings','桌面设置',`<div class="toggle-row"><div><b>启用 GateHomePage</b><p>独立监听端口，启停或修改端口后需要重启服务。</p></div><label class="switch"><input name="enabled" type="checkbox" ${h.enabled?'checked':''}><span></span><span class="sr-only">启用首页</span></label></div><div class="form-grid"><label>首页端口<input name="port" type="number" min="1024" max="65535" value="${h.port}" required></label><label>访问方式<select name="public"><option value="true" ${h.public?'selected':''}>允许访客查看</option><option value="false" ${!h.public?'selected':''}>管理员登录后查看</option></select></label><label class="span-2">空间标题<input name="title" value="${esc(h.title)}" maxlength="60" required></label><label>背景色调<select name="tone">${[['forest','森林绿'],['dusk','暮色紫'],['midnight','午夜蓝']].map(([v,t])=>`<option value="${v}" ${h.tone===v?'selected':''}>${t}</option>`).join('')}</select></label><label>背景遮罩（0–90%）<input name="shade" type="number" min="0" max="90" value="${h.shade}" required></label><label class="check-label"><input name="compact" type="checkbox" ${h.compact?'checked':''}>紧凑卡片</label><label class="check-label"><input name="show_addresses" type="checkbox" ${h.show_addresses?'checked':''}>显示当前网络的链接地址</label></div>${hpSearchFields(h)}${hpImageFields(h.background,true)}<label>自定义 CSS<textarea name="custom_css" rows="9" maxlength="32768" class="mono" spellcheck="false" placeholder=".gh-main { max-width: 1200px; }&#10;.gh-link-grid { --gh-gap: 20px; }">${esc(h.custom_css)}</textarea><small>只作用于独立首页，最多 32 KiB。可用 .gh-page、.gh-main、.gh-link-grid、.gh-item、.gh-open、.gh-art、.gh-copy 等选择器；外部脚本、字体、样式和图片请求被限制。</small></label><p class="form-note">允许访客查看时，选定的内外网地址也会展示给访客。编辑配置始终需要管理员登录。</p>`);
  if(action==='group-new'||action==='group-edit')return hpDialog(action,'分组设置',`<label>分组名称<input name="name" value="${esc(group?.name||'')}" maxlength="32" required></label>`,id);
  if(action==='page-new'||action==='page-edit')return hpDialog(action,'页面设置',`<label>页面名称<input name="name" value="${esc(page?.name||'')}" maxlength="32" required></label><div class="form-grid"><label>每页行数<input name="rows" type="number" min="1" max="8" value="${page?.rows||2}" required></label><label>桌面列数<input name="columns" type="number" min="1" max="8" value="${page?.columns||3}" required></label><label>手机端最多列数<input name="mobile_columns" type="number" min="1" max="3" value="${page?.mobile_columns||2}" required></label></div><p class="form-note">超出行数 × 列数的链接自动放到圆点续页。手机端按实际宽度减少列数，不丢失链接。</p>`,id,button.dataset.group);
  if(action==='link-new'||action==='link-edit')return hpDialog(action,link?'编辑链接':'添加链接',`<label>所属页面<select name="page" required>${hpPageOptions(page?.id||button.dataset.page)}</select></label><div class="form-grid"><label>链接名称<input name="name" value="${esc(link?.name||'')}" maxlength="32" required></label><label>描述<input name="description" value="${esc(link?.description||'')}" maxlength="80"></label><label class="span-2">内网链接<input name="lan" type="url" value="${esc(link?.lan||'')}" maxlength="2048" placeholder="http://192.168.2.10:5000/"></label><label class="span-2">外网链接<input name="wan" type="url" value="${esc(link?.wan||'')}" maxlength="2048" placeholder="https://nas.example.com:18443/"></label><label class="check-label span-2"><input name="favorite" type="checkbox" ${link?.favorite?'checked':''}>置顶到当前页面前面</label></div><p class="form-note">内外网至少填写一项。首页切换网络时，缺少对应地址的链接会显示未设置。</p>${hpImageFields(link?.image)}`,id);
  if(action==='import')return hpDialog('import','从 GateHome 导入',`<label>导入到页面<select name="page" required>${hpPageOptions()}</select></label><fieldset class="hp-import-list"><legend>选择反代服务</legend>${config.routes.map((r,i)=>`<label class="check-label"><input type="checkbox" name="route" value="${i}"><span><b>${esc(r.name)}</b><small>${esc(r.upstream)}<br>${esc(publicServiceURL(r)||'未设置对外监听端口')}</small></span></label>`).join('')||'<p class="form-note">暂无反代规则，请先添加反代服务。</p>'}</fieldset><p class="form-note">导入名称、已有图片、内网地址和域名端口；随后可独立修改。</p>`);
}
async function hpLoadImage(form,file,background,row=null) {
  if(busy)return;
  const token=homepageDialogVersion,endpoint=background?'homepage-backgrounds':'route-images';
  const buttons=[...form.querySelectorAll('button')];busy=true;buttons.forEach(b=>b.disabled=true);form.querySelector('.error').textContent='';
  try {
    let body;if(file){if(file.size>5*1024*1024)throw new Error('图片不能超过 5 MiB');body=new FormData();body.append('file',file);}else {const url=(row?row.querySelector('[data-engine-image-url]'):form.elements.image_url).value.trim();if(!safeServiceURL(url))throw new Error('请填写有效的 HTTP / HTTPS 图片 URL');body={url};}
    const result=await api(endpoint+(file?'/upload':'/import'),'POST',body);
    if(token!==homepageDialogVersion||!form.closest('dialog').open)return;
    (row?row.querySelector('[data-engine-image]'):form.elements.image).value=result.id;(row?row.querySelector('.hp-engine-preview'):form.querySelector('.hp-image-preview')).innerHTML=`<img src="${esc(result.url)}" alt="图片预览">`;
  }catch(e){if(token===homepageDialogVersion)form.querySelector('.error').textContent=e.message;}finally{busy=false;if(token===homepageDialogVersion)buttons.forEach(b=>b.disabled=false);}
}
document.addEventListener('toggle',event=>{const group=event.target.dataset.hpGroup;if(group){if(event.target.open)homepageClosedGroups.delete(group);else homepageClosedGroups.add(group);}},true);
document.addEventListener('change',event=>{if(event.target.hasAttribute('data-hp-engine-upload')){const file=event.target.files[0];if(file)hpLoadImage(event.target.form,file,false,event.target.closest('.hp-search-engine'));}else if(event.target.dataset.hpUpload){const file=event.target.files[0];if(file)hpLoadImage(event.target.form,file,event.target.dataset.hpUpload==='background');}});
document.addEventListener('input',event=>{if(event.target.hasAttribute('data-engine-url')){const row=event.target.closest('.hp-search-engine');row.querySelector('.hp-engine-preview').innerHTML=hpEnginePreview(row.querySelector('[data-engine-image]').value,event.target.value);}});
document.addEventListener('click',async event=>{
  const button=event.target.closest('[data-hp-action]');if(!button||!config||busy)return;const action=button.dataset.hpAction;
  if(action==='edit'){homepageEditing=!homepageEditing;render();return;}
  if(action==='engine-add'){const form=button.closest('form'),list=form.querySelector('.hp-engine-list');if(list.children.length>=12){form.querySelector('.error').textContent='最多支持 12 个搜索引擎';return;}list.insertAdjacentHTML('beforeend',hpEngineRow({id:newID(),name:'',url:''}));hydrateIcons(list);list.lastElementChild.querySelector('input').focus();return;}
  if(action==='engine-remove'){const row=button.closest('.hp-search-engine');if(row.parentElement.children.length===1){button.closest('form').querySelector('.error').textContent='请至少保留一个搜索引擎';return;}row.remove();return;}
  if(action==='engine-icon-import')return hpLoadImage(button.closest('form'),null,false,button.closest('.hp-search-engine'));
  if(action==='engine-icon-clear'){const row=button.closest('.hp-search-engine');row.querySelector('[data-engine-image]').value='';row.querySelector('[data-hp-engine-upload]').value='';row.querySelector('.hp-engine-preview').innerHTML=hpEnginePreview('',row.querySelector('[data-engine-url]').value);return;}
  if(action==='image-import')return hpLoadImage(button.closest('form'),null,button.dataset.background==='true');
  if(action==='image-clear'){const form=button.closest('form');form.elements.image.value='';form.elements.image_file.value='';form.querySelector('.hp-image-preview').innerHTML='<span>尚未选择图片</span>';return;}
  if(['settings','group-new','group-edit','page-new','page-edit','link-new','link-edit','import'].includes(action))return hpOpen(action,button);
  const h=clone(homepageValue()),{group,page,link}=hpFind(h,button.dataset.id);
  if(action==='group-delete'){if(!confirm('删除此分组及其中所有页面和链接？'))return;h.groups=h.groups.filter(g=>g.id!==group.id);}
  if(action==='page-delete'){if(group.pages.length<2)return;if(page.links.length&&!confirm('删除此页面及其中所有链接？'))return;group.pages=group.pages.filter(p=>p.id!==page.id);}
  let deleted;
  if(action==='link-delete'){deleted={link:clone(link),pageID:page.id,index:page.links.indexOf(link)};page.links=page.links.filter(l=>l.id!==link.id);}
  if(action==='link-up'||action==='link-down'){const index=page.links.indexOf(link),next=index+(action==='link-up'?-1:1);if(next<0||next>=page.links.length)return;[page.links[index],page.links[next]]=[page.links[next],page.links[index]];}
  if(action==='undo'){if(!homepageDeleted)return;const target=hpFind(h,homepageDeleted.pageID).page;if(!target){toast('原页面已删除，无法撤销');return;}target.links.splice(homepageDeleted.index,0,homepageDeleted.link);}
  busy=true;try{await hpSave(h);if(deleted)homepageDeleted=deleted;if(action==='undo')homepageDeleted=null;render();}catch(e){toast(e.message);}finally{busy=false;}
});
document.addEventListener('submit',event=>{
  const form=event.target;if(!form.dataset.homepage)return;event.preventDefault();
  submitForm(form,async()=>{
    const h=clone(homepageValue()),e=form.elements,kind=form.dataset.kind,{group,page,link}=hpFind(h,form.dataset.id);
    if(kind==='settings')Object.assign(h,{enabled:e.enabled.checked,port:Number(e.port.value),public:e.public.value==='true',title:e.title.value.trim(),tone:e.tone.value,shade:Number(e.shade.value),compact:e.compact.checked,show_addresses:e.show_addresses.checked,background:e.image.value,custom_css:e.custom_css.value,search_engines:[...form.querySelectorAll('.hp-search-engine')].map(row=>({id:row.dataset.engineId,name:row.querySelector('[data-engine-name]').value.trim(),url:row.querySelector('[data-engine-url]').value.trim(),image:row.querySelector('[data-engine-image]').value}))});
    if(kind==='group-new')h.groups.push({id:newID(),name:e.name.value.trim(),pages:[{id:newID(),name:'日常',rows:2,columns:3,mobile_columns:2,links:[]}]});
    if(kind==='group-edit')group.name=e.name.value.trim();
    if(kind==='page-new'||kind==='page-edit'){const values={name:e.name.value.trim(),rows:Number(e.rows.value),columns:Number(e.columns.value),mobile_columns:Number(e.mobile_columns.value)};if(page)Object.assign(page,values);else hpFind(h,form.dataset.group).group.pages.push({id:newID(),...values,links:[]});}
    if(kind==='link-new'||kind==='link-edit'){
      const values={id:link?.id||newID(),name:e.name.value.trim(),description:e.description.value.trim(),lan:e.lan.value.trim(),wan:e.wan.value.trim(),favorite:e.favorite.checked,image:e.image.value};
      if(!values.lan&&!values.wan)throw new Error('内网链接和外网链接至少填写一项');
      if([values.lan,values.wan].some(v=>v&&!safeServiceURL(v)))throw new Error('链接须为不含账号密码的 HTTP / HTTPS 地址');
      const target=hpFind(h,e.page.value).page;if(!target)throw new Error('请选择所属页面');
      if(link&&target.id===page.id)Object.assign(link,values);else{if(link)page.links=page.links.filter(l=>l.id!==link.id);target.links.push(values);}
    }
    if(kind==='import'){
      const selected=[...form.querySelectorAll('[name=route]:checked')].map(input=>config.routes[Number(input.value)]);if(!selected.length)throw new Error('请至少选择一项反代服务');
      const target=hpFind(h,e.page.value).page;if(!target)throw new Error('请选择导入页面');
      for(const route of selected){const wan=publicServiceURL(route);if(!target.links.some(l=>l.lan===route.upstream&&l.wan===wan))target.links.push({id:newID(),name:route.name,description:'',lan:route.upstream,wan,image:route.image||'',favorite:false});}
    }
    await hpSave(h);homepageDialogVersion++;closeDialog(form.closest('dialog'));
  });
});
