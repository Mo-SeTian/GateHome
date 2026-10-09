'use strict';
function createHomepageEditor({request,changed,context,icon,esc,loggedIn}) {
  const $=s=>document.querySelector(s),clone=value=>JSON.parse(JSON.stringify(value)),newID=()=>crypto.randomUUID();
  let doc=null,editing=false,busy=false,version=0,opener=null,undo=null,loginEditing=true;
  const button=(label,action,attributes='',primary=false)=>`<button type="button" class="gh-button ${primary?'gh-primary':''}" data-ghe-action="${action}" ${attributes}>${label}</button>`;
  const notice=message=>{$('.gh-notice').textContent=message;};
  function apply(value){doc=value;changed(doc.homepage,doc.space_id,doc.revision);}
  async function save(home,deleted=false){const previous=clone(doc.homepage),result=await request('/api/editor/config',{homepage:home,revision:doc.revision},'PUT');undo=deleted?previous:null;apply(result);notice('桌面已保存');}
  function find(home,id){for(const group of home.groups){if(group.id===id)return {group};for(const page of group.pages){if(page.id===id)return {group,page};const link=page.links.find(l=>l.id===id);if(link)return {group,page,link};}}return {};}
  function options(selected=''){return doc.homepage.groups.flatMap(g=>g.pages.map(p=>`<option value="${esc(p.id)}" ${p.id===selected?'selected':''}>${esc(g.name)} / ${esc(p.name)}</option>`)).join('');}
  function modal(kind,title,fields,id=''){
    let dialog=$('#ghe-dialog');if(!dialog){dialog=document.createElement('dialog');dialog.id='ghe-dialog';dialog.addEventListener('cancel',event=>{event.preventDefault();close();});document.body.append(dialog);}
    version++;opener=document.activeElement;
    dialog.innerHTML=`<form data-ghe-form="${kind}" data-id="${esc(id)}"><header><div><small>GATEHOMEPAGE</small><h2 id="ghe-dialog-title">${title}</h2></div>${button(icon('close'),'close','aria-label="关闭"')}</header><div class="ghe-body">${fields}<p class="ghe-error" role="alert" tabindex="-1"></p></div><footer>${button('取消','close')}<button type="submit" class="gh-button ${kind==='delete'?'ghe-danger':'gh-primary'}">${kind==='login'?(loginEditing?'登录并编辑':'登录'):kind==='delete'?'确认删除':kind==='pages'?'完成':'保存'}</button></footer></form>`;
    dialog.setAttribute('aria-labelledby','ghe-dialog-title');dialog.showModal();if(!matchMedia('(prefers-reduced-motion:reduce)').matches)dialog.animate([{opacity:0,transform:'translateY(8px)'},{opacity:1,transform:'translateY(0)'}],{duration:160,easing:'ease-out'});
  }
  function close(){if(busy)return;version++;const dialog=$('#ghe-dialog');dialog?.querySelector('form')?.reset();dialog?.close();if(opener?.isConnected&&opener.getClientRects().length)opener.focus();else $('#gh-floating-toggle').focus();}
  function login(edit=true){loginEditing=edit;modal('login','登录后编辑桌面','<p class="ghe-note">账号由 GateHome 管理，每个账号拥有独立桌面。</p><label>首页账号<input name="username" autocomplete="username" maxlength="64" required></label><label>首页密码<input name="password" type="password" autocomplete="current-password" maxlength="72" required></label>');}
  async function begin(){try{const value=await request('/api/editor/config');editing=true;apply(value);}catch(error){if(error.status===401)login();else notice(error.message);}}
  function imageFields(id,kind='images',engine=null){
    let preview=id?`<img src="/api/editor/${kind}/${esc(id)}" alt="图片预览">`:engine?`<img src="${esc(homepageEngineImage(engine))}" alt="默认搜索图标">`:'<span>尚未选择图片</span>';
    return `<fieldset class="ghe-image-fields" data-image-kind="${kind}"><legend>${kind==='backgrounds'?'背景图片':'图片'}</legend><input type="hidden" data-image value="${esc(id||'')}"><div class="ghe-preview">${preview}</div><label>上传图片<input type="file" data-ghe-upload accept="image/png,image/jpeg,image/webp,image/gif${kind==='images'?',image/x-icon':''}"></label><div class="ghe-image-url"><label>图片 URL<input type="text" inputmode="url" data-image-url placeholder="https://example.com/image.png" maxlength="2048"></label>${button(icon('download')+'获取','image-import')}</div>${button('移除图片','image-clear')}<p class="ghe-note">支持 PNG、JPEG、GIF、WebP${kind==='images'?'、ICO':''}，最大 5 MiB。图片保存到当前用户的独立目录。</p></fieldset>`;
  }
  function engineRow(engine){return `<div class="ghe-engine" data-engine-id="${esc(engine.id)}"><div class="ghe-form-grid"><label>名称<input data-engine-name value="${esc(engine.name)}" maxlength="20" required></label><label>搜索地址<input data-engine-url type="url" value="${esc(engine.url)}" placeholder="https://example.com/search?q={query}" maxlength="2048" required></label></div>${button(icon('trash'),'engine-remove','aria-label="移除搜索引擎"')}<details><summary>自定义图标</summary>${imageFields(engine.image,'images',engine)}</details></div>`;}
  function open(action,id){
    const home=doc.homepage,{group:currentGroup,sheet}=context(),{group,page,link}=find(home,id);
    if(action==='settings')return modal('settings','桌面设置',`<label>空间标题<input name="title" value="${esc(home.title)}" maxlength="60" required></label><div class="ghe-form-grid"><label>背景色调<select name="tone">${[['forest','彩霞（默认）'],['dusk','暮色紫'],['midnight','午夜蓝']].map(([v,t])=>`<option value="${v}" ${home.tone===v?'selected':''}>${t}</option>`).join('')}</select></label><label>背景遮罩（0–90%）<input name="shade" type="number" min="0" max="90" value="${home.shade}" required></label><label>时钟文字颜色<input type="color" data-clock-color aria-label="选择时钟文字颜色" value="${esc(home.clock_color||'#000000')}"><small>默认黑色，可点击色块选择。</small></label><label>颜色值（HEX）<input name="clock_color" value="${esc(home.clock_color||'#000000')}" maxlength="7" pattern="#[a-fA-F0-9]{6}" placeholder="#000000" required><small>格式：#RRGGBB，例如 #000000。</small></label><label class="ghe-check"><input name="compact" type="checkbox" ${home.compact?'checked':''}>紧凑卡片</label><label class="ghe-check"><input name="show_addresses" type="checkbox" ${home.show_addresses?'checked':''}>显示链接地址</label></div><label class="ghe-check"><input name="public" type="checkbox" ${home.public?'checked':''}>允许访客查看此桌面</label><p class="ghe-note">开启后，持有桌面链接的访客可查看你的应用及地址。桌面链接：<a href="/?space=${esc(doc.space_id)}" target="_blank" rel="noopener noreferrer">打开此桌面</a></p><fieldset><legend>搜索引擎</legend><div class="ghe-engines">${home.search_engines.map(engineRow).join('')}</div>${button(icon('plus')+'添加搜索引擎','engine-add')}<p class="ghe-note">搜索地址中使用一次 {query} 代表关键词，最多 12 项。</p></fieldset>${imageFields(home.background,'backgrounds')}<label>自定义 CSS<textarea name="custom_css" rows="8" maxlength="32768" spellcheck="false" placeholder=".gh-item { border-radius: 22px; }">${esc(home.custom_css)}</textarea><small>最多 32 KiB，只作用于你的桌面。</small></label>`);
    if(action==='widgets')return modal('widgets','桌面组件',`<p class="ghe-note">勾选添加组件，取消勾选移除组件。应用卡片可通过“添加应用”创建。</p><label class="ghe-check"><input name="clock" type="checkbox" ${home.widgets?.clock!==false?'checked':''}>时间与日期</label><label class="ghe-check"><input name="search" type="checkbox" ${home.widgets?.search!==false?'checked':''}>网页搜索</label>`);
    if(action==='group-new'||action==='group-edit')return modal(action,group?'分组设置':'添加分组',`<label>分组名称<input name="name" value="${esc(group?.name||'')}" maxlength="32" required></label>${group?button(icon('trash')+'删除分组','group-delete',`data-id="${esc(group.id)}"`):'<p class="ghe-note">创建后可在页面管理中设置行列数，添加多页。</p>'}`,id);
    if(action==='pages')return modal('pages','页面管理',`<div class="ghe-page-list">${currentGroup.pages.map(p=>`<div><span><b>${esc(p.name)}</b><small>${p.rows} 行 × ${p.columns} 列 · ${p.links.length} 个应用</small></span>${button(icon('edit'),'page-edit',`data-id="${esc(p.id)}" aria-label="编辑${esc(p.name)}"`)}${button(icon('trash'),'page-delete',`data-id="${esc(p.id)}" aria-label="删除${esc(p.name)}" ${currentGroup.pages.length===1?'disabled':''}`)}</div>`).join('')}</div>${button(icon('plus')+'添加页面','page-new',`data-id="${esc(currentGroup.id)}"`)}`,currentGroup.id);
    if(action==='page-new'||action==='page-edit')return modal(action,page?'页面设置':'添加页面',`<label>页面名称<input name="name" value="${esc(page?.name||'')}" maxlength="32" required></label><div class="ghe-form-grid"><label>每页行数<input name="rows" type="number" min="1" max="8" value="${page?.rows||2}" required></label><label>桌面列数<input name="columns" type="number" min="1" max="8" value="${page?.columns||3}" required></label><label>手机端最多列数<input name="mobile_columns" type="number" min="1" max="3" value="${page?.mobile_columns||2}" required></label></div>`,id||currentGroup.id);
    if(action==='link-new'||action==='link-edit')return modal(action,link?'编辑应用':'添加应用',`<label>所属页面<select name="page" required>${options(page?.id||sheet?.page.id)}</select></label><label>页面内位置（置顶应用优先）<input name="position" type="number" min="1" max="200" value="${link?page.links.indexOf(link)+1:(sheet?.page.links.length||0)+1}" required></label><div class="ghe-form-grid"><label>应用名称<input name="name" value="${esc(link?.name||'')}" maxlength="32" required></label><label>描述<input name="description" value="${esc(link?.description||'')}" maxlength="80"></label></div><label>内网链接<input name="lan" type="url" value="${esc(link?.lan||'')}" placeholder="http://192.168.2.10:5000/" maxlength="2048"></label><label>外网链接<input name="wan" type="url" value="${esc(link?.wan||'')}" placeholder="https://nas.example.com/" maxlength="2048"></label><label class="ghe-check"><input name="favorite" type="checkbox" ${link?.favorite?'checked':''}>置顶到当前页面</label>${imageFields(link?.image)}${link?button(icon('trash')+'删除应用','link-delete',`data-id="${esc(link.id)}"`):''}`,id);
  }
  async function upload(field,file){
    const token=version,dialog=$('#ghe-dialog'),buttons=[...dialog.querySelectorAll('button')];busy=true;buttons.forEach(b=>b.disabled=true);dialog.querySelector('.ghe-error').textContent='';
    try{let body;if(file){if(file.size>5*1024*1024)throw new Error('图片不能超过 5 MiB');body=new FormData();body.append('file',file);}else{const url=field.querySelector('[data-image-url]').value.trim();if(!homepageURL(url))throw new Error('请填写有效的 HTTP / HTTPS 图片 URL');body={url};}const result=await request('/api/editor/'+field.dataset.imageKind+(file?'/upload':'/import'),body);if(token!==version||!dialog.open)return;field.querySelector('[data-image]').value=result.id;field.querySelector('.ghe-preview').innerHTML=`<img src="${esc(result.url)}" alt="图片预览">`;}
    catch(error){if(token===version){const message=dialog.querySelector('.ghe-error');message.textContent=error.message;message.focus();}}
    finally{busy=false;if(token===version)buttons.forEach(b=>b.disabled=false);}
  }
  async function importServices(){const result=await request('/api/editor/importable');modal('import','从 GateHome 导入',`<label>导入到页面<select name="page" required>${options(context().sheet?.page.id)}</select></label><div class="ghe-import-list">${result.services.map(s=>`<label class="ghe-check"><input type="checkbox" name="service" value="${esc(s.id)}"><span>${s.image?`<img src="/api/editor/source-images/${esc(s.image)}" alt="">`:icon('server')}<b>${esc(s.name)}</b><small>${esc(s.lan)}<br>${esc(s.wan||'未设置外网监听')}</small></span></label>`).join('')||'<p class="ghe-note">GateHome 暂无反代服务。</p>'}</div><p class="ghe-note">复制应用名称、图标和内外网地址到你的桌面，之后可独立修改。</p>`);}
  document.addEventListener('input',event=>{
    const field=event.target,form=field.closest('[data-ghe-form="settings"]');if(!form)return;
    if(field.hasAttribute('data-clock-color'))form.elements.clock_color.value=field.value;
    if(field.name==='clock_color'&&/^#[a-fA-F0-9]{6}$/.test(field.value))form.querySelector('[data-clock-color]').value=field.value;
  });
  document.addEventListener('change',event=>{if(event.target.hasAttribute('data-ghe-upload')&&!busy){const file=event.target.files[0];if(file)upload(event.target.closest('.ghe-image-fields'),file);}});
  document.addEventListener('click',async event=>{
    const target=event.target.closest('[data-ghe-action]');if(!target||busy)return;const action=target.dataset.gheAction,id=target.dataset.id;
    if(action==='close'){close();return;}
    if(action==='login'){login();return;}
    if(action==='engine-add'){const list=$('#ghe-dialog .ghe-engines');if(list.children.length>=12){$('#ghe-dialog .ghe-error').textContent='最多支持 12 个搜索引擎';return;}list.insertAdjacentHTML('beforeend',engineRow({id:newID(),name:'',url:'',image:''}));list.lastElementChild.querySelector('input').focus();return;}
    if(action==='engine-remove'){const row=target.closest('.ghe-engine');if(row.parentElement.children.length>1)row.remove();else $('#ghe-dialog .ghe-error').textContent='请至少保留一个搜索引擎';return;}
    if(action==='image-import')return upload(target.closest('.ghe-image-fields'),null);
    if(action==='image-clear'){const field=target.closest('.ghe-image-fields');field.querySelector('[data-image]').value='';field.querySelector('[data-ghe-upload]').value='';field.querySelector('.ghe-preview').innerHTML='<span>默认图片</span>';return;}
    if(!doc)return;
    if(action==='import'){try{await importServices();}catch(error){notice(error.message);}return;}
    if(action==='undo'){busy=true;try{await save(clone(undo));}catch(error){notice(error.message);}finally{busy=false;}return;}
    if(['group-delete','page-delete','link-delete'].includes(action)){
      const what=action==='group-delete'?'分组及其中的页面和应用':action==='page-delete'?'页面及其中的应用':'应用';
      modal('delete','确认删除',`<p>删除此${what}？</p><p class="ghe-note">保存后可通过“撤销删除”恢复本次删除。</p>`,id);$('#ghe-dialog form').dataset.deleteAction=action;return;
    }
    open(action,id);
  });
  document.addEventListener('submit',async event=>{
    const form=event.target,kind=form.dataset.gheForm;if(!kind)return;event.preventDefault();if(busy)return;busy=true;const buttons=[...form.querySelectorAll('button')];buttons.forEach(b=>b.disabled=true);const error=form.querySelector('.ghe-error');error.textContent='';
    try{
      const e=form.elements;if(kind==='login'){const username=e.username.value,password=e.password.value;e.password.value='';await request('/login',{username,password});await loggedIn();editing=loginEditing;apply(await request('/api/editor/config'));}
      else if(kind==='delete'){
        const home=clone(doc.homepage),id=form.dataset.id,action=form.dataset.deleteAction,found=find(home,id);
        if(action==='group-delete')home.groups=home.groups.filter(g=>g.id!==id);
        if(action==='page-delete'){if(found.group.pages.length<=1)throw new Error('每组至少保留一个页面');found.group.pages=found.group.pages.filter(p=>p.id!==id);}
        if(action==='link-delete')found.page.links=found.page.links.filter(l=>l.id!==id);
        await save(home,true);
      }
      else if(kind==='import'){const services=[...form.querySelectorAll('[name=service]:checked')].map(input=>input.value);if(!services.length)throw new Error('请至少选择一项反代服务');apply(await request('/api/editor/import',{page_id:e.page.value,services,revision:doc.revision}));undo=null;}
      else if(kind!=='pages'){
        const home=clone(doc.homepage),id=form.dataset.id,{group,page,link}=find(home,id);
        if(kind==='settings')Object.assign(home,{title:e.title.value.trim(),tone:e.tone.value,clock_color:e.clock_color.value.toLowerCase(),shade:Number(e.shade.value),compact:e.compact.checked,show_addresses:e.show_addresses.checked,public:e.public.checked,background:form.querySelector('[data-image-kind=backgrounds] [data-image]').value,custom_css:e.custom_css.value,search_engines:[...form.querySelectorAll('.ghe-engine')].map(row=>({id:row.dataset.engineId,name:row.querySelector('[data-engine-name]').value.trim(),url:row.querySelector('[data-engine-url]').value.trim(),image:row.querySelector('[data-image]').value}))});
        if(kind==='widgets')home.widgets={clock:e.clock.checked,search:e.search.checked};
        if(kind==='group-new')home.groups.push({id:newID(),name:e.name.value.trim(),pages:[{id:newID(),name:'日常',rows:2,columns:3,mobile_columns:2,links:[]}]});
        if(kind==='group-edit')group.name=e.name.value.trim();
        if(kind==='page-new'||kind==='page-edit'){const values={name:e.name.value.trim(),rows:Number(e.rows.value),columns:Number(e.columns.value),mobile_columns:Number(e.mobile_columns.value)};if(page)Object.assign(page,values);else group.pages.push({id:newID(),...values,links:[]});}
        if(kind==='link-new'||kind==='link-edit'){
          const values={id:link?.id||newID(),name:e.name.value.trim(),description:e.description.value.trim(),lan:e.lan.value.trim(),wan:e.wan.value.trim(),favorite:e.favorite.checked,image:form.querySelector('[data-image]').value};
          if(!values.lan&&!values.wan)throw new Error('内网和外网链接至少填写一项');if([values.lan,values.wan].some(v=>v&&!homepageURL(v)))throw new Error('链接须为不含账号密码的 HTTP / HTTPS 地址');
          const target=find(home,e.page.value).page;if(link)page.links=page.links.filter(l=>l.id!==id);target.links.splice(Math.max(0,Math.min(target.links.length,Number(e.position.value)-1)),0,values);
        }
        await save(home);
      }
      busy=false;close();
    }catch(failure){error.textContent=failure.message;error.focus();}finally{busy=false;buttons.forEach(b=>b.disabled=false);}
  });
  return {
    get editing(){return editing;},
    async toggle(){if(editing){editing=false;changed(doc.homepage,doc.space_id,doc.revision);}else await begin();},
    login,
    reset(){editing=false;doc=null;undo=null;},
    toolbar(){if(!editing)return '';const {group}=context();return `<div class="ghe-toolbar" role="toolbar" aria-label="桌面编辑工具">${button(icon('plus')+'添加分组','group-new')}${button(icon('layers')+'分组设置','group-edit',`data-id="${esc(group?.id||'')}" ${group?'':'disabled'}`)}${button('页面管理','pages',group?'':'disabled')}${button(icon('plus')+'添加应用','link-new',group?'':'disabled',true)}${button(icon('download')+'从 GateHome 导入','import',group?'':'disabled')}${button('组件','widgets')}${button(icon('settings')+'桌面设置','settings')}${undo?button('撤销删除','undo'):''}</div>`;},
    cardControls(link){return editing?`<div class="ghe-card-actions">${button(icon('edit'),'link-edit',`data-id="${esc(link.id)}" aria-label="编辑${esc(link.name)}"`)}${button(icon('trash'),'link-delete',`data-id="${esc(link.id)}" aria-label="删除${esc(link.name)}"`)}</div>`:'';}
  };
}
