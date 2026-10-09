'use strict';
function homepageURL(value) {try {const u=new URL(value);return ['http:','https:'].includes(u.protocol)&&!u.username&&!u.password?u.href:'';}catch{return '';}}
function homepageSearch(template,query) {
  const term=query.trim();if(!term||typeof template!=='string'||template.split('{query}').length!==2)return '';
  try {const u=new URL(template);if(!homepageURL(template)||(!decodeURIComponent(u.pathname).includes('{query}')&&!u.search.includes('{query}')))return '';return homepageURL(template.replace('{query}',encodeURIComponent(term)));}catch{return '';}
}
function homepageEngineImage(engine) {
  if(/^[a-f0-9]{64}$/.test(engine.image||''))return '/images/'+engine.image;
  try {const host=new URL(engine.url).hostname;if(['baidu.com','www.baidu.com'].includes(host))return '/search-baidu.svg';if(['google.com','www.google.com'].includes(host))return '/search-google.svg';}catch{}
  return '/search-generic.svg';
}
function homepageSheets(group, mobile, availableWidth) {
  return (group?.pages||[]).flatMap(page=>{
    const columns=mobile?Math.max(1,Math.min(page.columns,page.mobile_columns,Math.floor((availableWidth+10)/160))):page.columns;
    const capacity=page.rows*columns,links=[...(page.links||[])].sort((a,b)=>Number(b.favorite)-Number(a.favorite));
    return Array.from({length:Math.max(1,Math.ceil(links.length/capacity))},(_,index)=>({id:page.id+':'+index,page,columns,index,links:links.slice(index*capacity,(index+1)*capacity)}));
  });
}
function homepageWheelGesture() {
  let total=0,last=-Infinity,axis=0,committed=false,passThrough=false;
  return {
    reset(){total=0;last=-Infinity;axis=0;committed=false;passThrough=false;},
    step(delta,time,canTurn){
      if(!delta)return null;
      const direction=Math.sign(delta);
      if(time-last>200||direction!==axis){total=0;committed=false;passThrough=false;}
      last=time;axis=direction;
      if(committed)return 0;
      if(passThrough)return null;
      if(!canTurn){total=0;passThrough=true;return null;}
      total+=delta;if(Math.abs(total)<60)return 0;
      committed=true;return direction;
    }
  };
}
if(typeof module!=='undefined') module.exports={homepageURL,homepageSheets,homepageSearch,homepageEngineImage,homepageWheelGesture};
if(typeof document!=='undefined') (()=>{
  const $=s=>document.querySelector(s),esc=s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const icon=name=>`<svg aria-hidden="true"><use href="/icons.svg#${name}"></use></svg>`;
  let config=null,groupID='',active={},network='lan',engine='',swipe=null,ignoreClick=false,drag=null,pageAnimation=null,viewSpace='',currentUser=null;
  const wheel=homepageWheelGesture();
  function view(id){if(id===viewSpace)return;viewSpace=id;groupID='';active={};network='lan';engine='';try{const saved=JSON.parse(localStorage.getItem('gatehomepage-view:'+id)||(id==='admin'?localStorage.getItem('gatehomepage-view'):null)||'{}');groupID=saved.group||'';active=saved.active||{};network=saved.network==='wan'?'wan':'lan';engine=saved.search_engine||'';}catch{}}
  const remember=()=>{try{if(viewSpace)localStorage.setItem('gatehomepage-view:'+viewSpace,JSON.stringify({group:groupID,active,network,search_engine:engine}));}catch{}};
  const asset=path=>path+(path.includes('?')?'&':'?')+'space='+encodeURIComponent(viewSpace);
  const engineArt=e=>`<span class="gh-engine-art"><img src="${esc(e.image?asset(homepageEngineImage(e)):homepageEngineImage(e))}" alt="" decoding="async"></span>`;
  function closeEngines(focus=false){$('#gh-engine-menu').hidden=true;$('#gh-engine').setAttribute('aria-expanded','false');if(focus)$('#gh-engine').focus();}
  function renderEngines(){
    const engines=config.search_engines||[];if(!engines.some(e=>e.id===engine))engine=engines[0]?.id||'';
    const selected=engines.find(e=>e.id===engine),button=$('#gh-engine');button.innerHTML=selected?engineArt(selected):'';button.title=selected?selected.name+' · 切换搜索引擎':'切换搜索引擎';button.setAttribute('aria-label',button.title);
    $('#gh-engine-menu').style.setProperty('--gh-engines',String(Math.min(4,engines.length)));$('#gh-engine-menu').innerHTML=engines.map(e=>`<button type="button" class="gh-engine-option" data-engine="${esc(e.id)}" title="${esc(e.name)}" aria-label="${esc(e.name)}" aria-pressed="${e.id===engine}">${engineArt(e)}</button>`).join('');$('.gh-search').hidden=!engines.length||config.widgets?.search===false;closeEngines();
  }
  async function request(path,body,method=body?'POST':'GET') {
    const multipart=body instanceof FormData;
    const response=await fetch(path,{method,credentials:'same-origin',headers:body?(multipart?{'X-Gatehouse-Request':'1'}:{'Content-Type':'application/json','X-Gatehouse-Request':'1'}):{},body:body?(multipart?body:JSON.stringify(body)):undefined});
    let data;try{data=await response.json();}catch{throw new Error('首页暂不可用，请检查是否已启用并重启服务');}
    if(!response.ok){const e=new Error(data.error||'请求失败');e.status=response.status;throw e;}return data;
  }
  function current() {
    const group=config.groups.find(g=>g.id===groupID)||config.groups[0];if(!group)return {group:null,sheets:[],sheet:null};
    groupID=group.id;const sheets=homepageSheets(group,matchMedia('(max-width:700px)').matches,$('.gh-link-grid').clientWidth||window.innerWidth-32);
    const sheet=sheets.find(p=>p.id===active[group.id])||sheets[0];active[group.id]=sheet.id;return {group,sheets,sheet};
  }
  function card(link) {
    const address=homepageURL(link[network]),image=link.image?`<img src="${esc(asset('/images/'+link.image))}" alt="" loading="lazy" decoding="async">`:icon('server'),tag=editor.editing?'button':address?'a':'div';
    return `<article class="gh-item ${!editor.editing&&!address?'gh-unavailable':''}">${link.favorite?`<span class="gh-favorite" aria-label="已置顶">${icon('pin')}</span>`:''}<${tag} class="gh-open" ${editor.editing?`type="button" data-ghe-action="link-edit" data-id="${esc(link.id)}"`:address?`href="${esc(address)}" target="_blank" rel="noopener noreferrer"`:''}><span class="gh-art">${image}</span><span class="gh-copy"><span class="gh-name">${esc(link.name)}</span>${link.description?`<span class="gh-description">${esc(link.description)}</span>`:''}${!address?`<span class="gh-address">未设置${network==='lan'?'内网':'外网'}地址</span>`:config.show_addresses?`<span class="gh-address">${esc(address)}</span>`:''}</span></${tag}>${editor.cardControls(link)}</article>`;
  }
  function resetDrag(){const grid=$('.gh-link-grid');if(drag&&grid.hasPointerCapture(drag.id))grid.releasePointerCapture(drag.id);drag=null;grid.classList.remove('gh-dragging');grid.style.transform='';}
  function render(animate=false,direction=0) {
    if(!config)return;
    resetDrag();
    document.body.classList.toggle('gh-editing',editor.editing);$('#gh-edit').innerHTML=icon('edit')+(editor.editing?'完成编辑':'编辑');$('.gh-summary').hidden=config.widgets?.clock===false;
    document.body.dataset.tone=config.tone;document.body.classList.toggle('gh-compact',config.compact);$('.gh-space-title').textContent=config.title;document.title=config.title+' · GateHomePage';
    document.body.classList.toggle('gh-has-background',!!config.background);$('.gh-wall').style.backgroundImage=config.background?`url("${asset('/background/'+config.background)}")`:'';document.body.style.setProperty('--gh-shade',config.shade/100);
    renderEngines();
    $('[data-net=lan]').setAttribute('aria-pressed',String(network==='lan'));$('[data-net=wan]').setAttribute('aria-pressed',String(network==='wan'));
    $('.gh-tabs').innerHTML=config.groups.map(g=>`<button type="button" class="gh-tab" data-group="${esc(g.id)}" aria-pressed="${g.id===groupID}">${esc(g.name)}</button>`).join('');
    const {group,sheets,sheet}=current(),grid=$('.gh-link-grid');
    $('.gh-tabs').querySelectorAll('button').forEach(b=>b.setAttribute('aria-pressed',String(b.dataset.group===groupID)));
    $('.gh-section-meta').textContent=group?group.pages.reduce((count,page)=>count+page.links.length,0)+' 个链接':'';
    grid.setAttribute('aria-label',group?group.name+'的链接':'当前页面链接');
    let content='';
    if(sheet) {
      grid.style.setProperty('--gh-columns',String(sheet.columns));content=sheet.links.map(l=>card(l)).join('');
      $('.gh-pager').innerHTML=sheets.map((p,i)=>`<button type="button" class="gh-page-dot" data-sheet="${esc(p.id)}" aria-current="${p.id===sheet.id?'page':'false'}" aria-label="第 ${i+1} 页：${esc(p.page.name)}${p.index?'（续页 '+(p.index+1)+'）':''}"><span aria-hidden="true"></span></button>`).join('');
    }else { $('.gh-pager').innerHTML=''; }
    grid.innerHTML=content||'<div class="gh-empty">这里还没有应用。点击右上角“编辑”，添加分组和应用。</div>';
    $('#gh-editor-tools').innerHTML=editor.toolbar();
    remember();
    pageAnimation?.cancel();pageAnimation=null;
    if(animate&&!matchMedia('(prefers-reduced-motion:reduce)').matches)pageAnimation=grid.animate([{opacity:.55,transform:direction?`translateX(${direction*18}px)`:'translateY(5px)'},{opacity:1,transform:'translate(0)'}],{duration:150,easing:'ease-out'});
  }
  function turn(direction) {const {group,sheets,sheet}=current(),index=sheets.indexOf(sheet),next=sheets[index+direction];if(!next)return false;active[group.id]=next.id;render(true,direction);return true;}
  document.addEventListener('click',event=>{
    if(!event.target.closest('.gh-engine-picker'))closeEngines();if(!event.target.closest('.gh-account')){ $('#gh-account-menu').hidden=true;$('#gh-user').setAttribute('aria-expanded','false');}
    if(ignoreClick){ignoreClick=false;if(event.target.closest('.gh-open')){event.preventDefault();return;}}
    const button=event.target.closest('button');if(!button)return;
    if(button.dataset.engine){engine=button.dataset.engine;remember();renderEngines();$('#gh-engine').focus();}
    if(button.dataset.group){wheel.reset();groupID=button.dataset.group;render(true);$('.gh-tabs button[data-group="'+CSS.escape(groupID)+'"]').focus();}
    if(button.dataset.sheet){wheel.reset();const {group}=current();active[group.id]=button.dataset.sheet;render(true);$('.gh-page-dot[data-sheet="'+CSS.escape(button.dataset.sheet)+'"]').focus();}
    if(button.dataset.net){network=button.dataset.net;render();}
  });
  $('#gh-engine').addEventListener('click',event=>{const menu=$('#gh-engine-menu');if(!menu.hidden){closeEngines();return;}menu.hidden=false;$('#gh-engine').setAttribute('aria-expanded','true');if(event.detail===0)menu.querySelector('[aria-pressed=true]')?.focus();if(!matchMedia('(prefers-reduced-motion:reduce)').matches)menu.animate([{opacity:0,transform:'translateY(-4px)'},{opacity:1,transform:'translateY(0)'}],{duration:140,easing:'ease-out'});});
  $('.gh-engine-picker').addEventListener('keydown',event=>{if(event.key==='Escape'&&!$('#gh-engine-menu').hidden){event.preventDefault();event.stopPropagation();closeEngines(true);}});
  $('.gh-engine-picker').addEventListener('focusout',event=>{if(!event.currentTarget.contains(event.relatedTarget))closeEngines();});
  $('.gh-engine-picker').addEventListener('error',event=>{if(event.target.tagName==='IMG'&&!event.target.src.endsWith('/search-generic.svg'))event.target.src='/search-generic.svg';},true);
  $('.gh-search').addEventListener('submit',event=>{event.preventDefault();const input=$('.gh-search input'),provider=config?.search_engines?.find(e=>e.id===engine),target=homepageSearch(provider?.url||'',input.value);if(target)window.open(target,'_blank','noopener,noreferrer');else input.focus();});
  $('.gh-link-grid').addEventListener('error',event=>{if(event.target.tagName==='IMG')event.target.parentElement.innerHTML=icon('server');},true);
  $('#gh-content').addEventListener('keydown',event=>{if(event.target.closest('input'))return;if(event.key==='ArrowLeft'||event.key==='ArrowRight'){wheel.reset();if(turn(event.key==='ArrowLeft'?-1:1)){event.preventDefault();$('.gh-page-dot[aria-current=page]').focus();}}});
  const grid=$('.gh-link-grid');
  grid.addEventListener('wheel',event=>{
    if(!config||drag||event.ctrlKey||event.metaKey||event.altKey){wheel.reset();return;}
    const horizontal=Math.abs(event.deltaX)>Math.abs(event.deltaY),delta=horizontal?event.deltaX:event.deltaY,rect=grid.getBoundingClientRect();
    const unit=event.deltaMode===1?(parseFloat(getComputedStyle(grid).lineHeight)||24):event.deltaMode===2?(horizontal?grid.clientWidth:grid.clientHeight):1;
    const {sheets,sheet}=current(),visible=horizontal||(rect.top>=8&&rect.bottom<=innerHeight-8),direction=wheel.step(delta*unit,performance.now(),visible&&!!sheets[sheets.indexOf(sheet)+Math.sign(delta)]);
    if(direction===null)return;event.preventDefault();if(direction)turn(direction);
  },{passive:false});
  grid.addEventListener('pointerdown',event=>{
    if(event.pointerType!=='mouse'||event.button!==0||editor.editing)return;ignoreClick=false;
    if(event.ctrlKey||event.metaKey||event.altKey)return;
    wheel.reset();drag={id:event.pointerId,x:event.clientX,y:event.clientY,locked:false};
  });
  grid.addEventListener('pointermove',event=>{
    if(!drag||event.pointerId!==drag.id)return;
    const dx=event.clientX-drag.x,dy=event.clientY-drag.y;
    if(!drag.locked){if(Math.abs(dy)>10&&Math.abs(dy)>Math.abs(dx)){resetDrag();return;}if(Math.abs(dx)<8||Math.abs(dx)<Math.abs(dy)*1.5)return;drag.locked=true;grid.setPointerCapture(drag.id);pageAnimation?.cancel();pageAnimation=null;grid.classList.add('gh-dragging');}
    event.preventDefault();const {sheets,sheet}=current(),next=sheets[sheets.indexOf(sheet)+(dx<0?1:-1)];
    if(!matchMedia('(prefers-reduced-motion:reduce)').matches)grid.style.transform=`translateX(${Math.max(-64,Math.min(64,next?dx:dx/4))}px)`;
  });
  grid.addEventListener('pointerup',event=>{
    if(!drag||event.pointerId!==drag.id)return;
    const dx=event.clientX-drag.x,locked=drag.locked,transform=grid.style.transform;resetDrag();if(!locked)return;ignoreClick=true;
    if(Math.abs(dx)>=60&&turn(dx<0?1:-1))return;
    if(transform&&!matchMedia('(prefers-reduced-motion:reduce)').matches)pageAnimation=grid.animate([{transform},{transform:'translateX(0)'}],{duration:150,easing:'ease-out'});
  });
  grid.addEventListener('pointercancel',event=>{if(event.pointerId===drag?.id)resetDrag();});
  grid.addEventListener('lostpointercapture',event=>{if(event.pointerId===drag?.id)resetDrag();});
  grid.addEventListener('pointerleave',()=>{if(drag&&!drag.locked)resetDrag();});
  grid.addEventListener('dragstart',event=>{if(drag)event.preventDefault();});
  $('.gh-link-grid').addEventListener('touchstart',event=>{wheel.reset();ignoreClick=false;swipe=event.touches.length===1?{x:event.touches[0].clientX,y:event.touches[0].clientY}:null;},{passive:true});
  $('.gh-link-grid').addEventListener('touchend',event=>{if(!swipe||!event.changedTouches.length)return;const dx=event.changedTouches[0].clientX-swipe.x,dy=event.changedTouches[0].clientY-swipe.y;swipe=null;if(Math.abs(dx)>60&&Math.abs(dx)>Math.abs(dy)*1.5)ignoreClick=!!turn(dx>0?-1:1);},{passive:true});
  $('.gh-link-grid').addEventListener('touchcancel',()=>{swipe=null;ignoreClick=false;},{passive:true});
  window.addEventListener('blur',()=>{wheel.reset();resetDrag();swipe=null;});
  window.addEventListener('resize',()=>{wheel.reset();render();});
  function clock(){if(document.hidden)return;const now=new Date();$('.gh-clock').textContent=now.toLocaleTimeString('zh-CN',{hour:'2-digit',minute:'2-digit',hour12:false});$('.gh-date').textContent=now.toLocaleDateString('zh-CN',{month:'long',day:'numeric',weekday:'long'});}
  function stylesheet(revision=''){let link=$('#gh-custom-css');if(!link){link=document.createElement('link');link.id='gh-custom-css';link.rel='stylesheet';document.head.append(link);}link.href=asset('/custom.css')+'&revision='+encodeURIComponent(revision);}
  async function session(){const me=await request('/api/me');currentUser=me;$('#gh-account-menu').hidden=true;$('#gh-user').setAttribute('aria-expanded','false');$('#gh-user').textContent=me.authenticated?me.username:'登录';return me;}
  async function loggedIn(){const me=await session(),url=new URL(location.href);url.searchParams.set('space',me.user_id);history.replaceState(null,'',url);await load();}
  const editor=createHomepageEditor({request,context:current,icon,esc,loggedIn,changed(home,id,revision){view(id);config=home;const url=new URL(location.href);url.searchParams.set('space',id);history.replaceState(null,'',url);$('#gh-content').hidden=false;$('#gh-login').hidden=true;stylesheet(revision);render();}});
  async function load(){try{const space=new URL(location.href).searchParams.get('space');config=await request('/api/homepage'+(space?'?space='+encodeURIComponent(space):''));view(config.space_id);$('#gh-login').hidden=true;$('#gh-login .gh-error').textContent='';$('#gh-content').hidden=false;$('#gh-error').textContent='';stylesheet();render();await session();}catch(e){$('#gh-content').hidden=true;$('.gh-search').hidden=true;if(e.status===401){config=null;$('#gh-login').hidden=false;$('#gh-custom-css')?.remove();$('.gh-wall').style.backgroundImage='';document.body.classList.remove('gh-has-background');document.title='GateHomePage';}else $('#gh-error').textContent=e.message;}}
  $('#gh-edit').addEventListener('click',()=>editor.toggle());$('#gh-user').addEventListener('click',()=>{if(!currentUser?.authenticated){editor.login(false);return;}const menu=$('#gh-account-menu');menu.hidden=!menu.hidden;$('#gh-user').setAttribute('aria-expanded',String(!menu.hidden));});$('#gh-switch-user').addEventListener('click',()=>{ $('#gh-account-menu').hidden=true;$('#gh-user').setAttribute('aria-expanded','false');editor.login(false);});$('.gh-account').addEventListener('keydown',event=>{if(event.key==='Escape'){ $('#gh-account-menu').hidden=true;$('#gh-user').setAttribute('aria-expanded','false');$('#gh-user').focus();}});
  $('#gh-login form').addEventListener('submit',async event=>{event.preventDefault();const form=event.target,button=form.querySelector('button');button.disabled=true;form.querySelector('.gh-error').textContent='';try{const username=form.elements.username.value,password=form.elements.password.value;form.elements.password.value='';await request('/login',{username,password});await loggedIn();}catch(e){form.querySelector('.gh-error').textContent=e.message;}finally{button.disabled=false;}});
  $('#gh-logout').addEventListener('click',async()=>{try{await request('/logout',{});editor.reset();document.body.classList.remove('gh-editing');$('#gh-editor-tools').innerHTML='';await session();await load();}catch(e){$('#gh-error').textContent=e.message;}});
  $('#gh-edit').disabled=true;
  clock();setInterval(clock,15000);document.addEventListener('visibilitychange',clock);load().then(()=>{$('#gh-edit').disabled=false;if(location.hash==='#edit')editor.toggle();});
})();
