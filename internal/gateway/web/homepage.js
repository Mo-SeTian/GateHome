'use strict';
function homepageURL(value) {try {const u=new URL(value);return ['http:','https:'].includes(u.protocol)&&!u.username&&!u.password?u.href:'';}catch{return '';}}
function homepageSheets(group, mobile, availableWidth) {
  return (group?.pages||[]).flatMap(page=>{
    const columns=mobile?Math.max(1,Math.min(page.columns,page.mobile_columns,Math.floor((availableWidth+10)/160))):page.columns;
    const capacity=page.rows*columns,links=[...(page.links||[])].sort((a,b)=>Number(b.favorite)-Number(a.favorite));
    return Array.from({length:Math.max(1,Math.ceil(links.length/capacity))},(_,index)=>({id:page.id+':'+index,page,columns,index,links:links.slice(index*capacity,(index+1)*capacity)}));
  });
}
if(typeof module!=='undefined') module.exports={homepageURL,homepageSheets};
if(typeof document!=='undefined') (()=>{
  const $=s=>document.querySelector(s),esc=s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const icon=name=>`<svg aria-hidden="true"><use href="/icons.svg#${name}"></use></svg>`;
  let config=null,groupID='',active={},network='lan',query='',swipe=null,ignoreClick=false;
  try {const saved=JSON.parse(localStorage.getItem('gatehomepage-view')||'{}');groupID=saved.group||'';active=saved.active||{};network=saved.network==='wan'?'wan':'lan';}catch{}
  const remember=()=>{try{localStorage.setItem('gatehomepage-view',JSON.stringify({group:groupID,active,network}));}catch{}};
  async function request(path,body) {
    const response=await fetch(path,{method:body?'POST':'GET',credentials:'same-origin',headers:body?{'Content-Type':'application/json','X-Gatehouse-Request':'1'}:{},body:body?JSON.stringify(body):undefined});
    let data;try{data=await response.json();}catch{throw new Error('首页暂不可用，请检查是否已启用并重启服务');}
    if(!response.ok){const e=new Error(data.error||'请求失败');e.status=response.status;throw e;}return data;
  }
  function current() {
    const group=config.groups.find(g=>g.id===groupID)||config.groups[0];if(!group)return {group:null,sheets:[],sheet:null};
    groupID=group.id;const sheets=homepageSheets(group,matchMedia('(max-width:700px)').matches,$('.gh-link-grid').clientWidth||window.innerWidth-32);
    const sheet=sheets.find(p=>p.id===active[group.id])||sheets[0];active[group.id]=sheet.id;return {group,sheets,sheet};
  }
  function card(link,context='') {
    const address=homepageURL(link[network]),image=link.image?`<img src="/images/${esc(link.image)}" alt="" loading="lazy" decoding="async">`:icon('server');
    return `<article class="gh-item ${address?'':'gh-unavailable'}">${link.favorite?`<span class="gh-favorite" aria-label="已置顶">${icon('pin')}</span>`:''}<${address?'a':'div'} class="gh-open" ${address?`href="${esc(address)}" target="_blank" rel="noopener noreferrer"`:''}><span class="gh-art">${image}</span><span class="gh-copy"><span class="gh-name">${esc(link.name)}</span>${link.description?`<span class="gh-description">${esc(link.description)}</span>`:''}${context?`<span class="gh-context">${esc(context)}</span>`:''}${!address?`<span class="gh-address">未设置${network==='lan'?'内网':'外网'}地址</span>`:config.show_addresses?`<span class="gh-address">${esc(address)}</span>`:''}</span></${address?'a':'div'}></article>`;
  }
  function render(animate=false) {
    if(!config)return;
    document.body.dataset.tone=config.tone;document.body.classList.toggle('gh-compact',config.compact);$('.gh-space-title').textContent=config.title;document.title=config.title+' · GateHomePage';
    document.body.classList.toggle('gh-has-background',!!config.background);$('.gh-wall').style.backgroundImage=config.background?`url("/background/${config.background}")`:'';document.body.style.setProperty('--gh-shade',config.shade/100);
    $('[data-net=lan]').setAttribute('aria-pressed',String(network==='lan'));$('[data-net=wan]').setAttribute('aria-pressed',String(network==='wan'));
    $('.gh-tabs').innerHTML=config.groups.map(g=>`<button type="button" class="gh-tab" data-group="${esc(g.id)}" aria-pressed="${g.id===groupID}">${esc(g.name)}</button>`).join('');
    const {group,sheets,sheet}=current(),grid=$('.gh-link-grid');
    $('.gh-tabs').querySelectorAll('button').forEach(b=>b.setAttribute('aria-pressed',String(b.dataset.group===groupID)));
    let content='';
    if(query.trim()) {
      const needle=query.trim().toLocaleLowerCase(),found=config.groups.flatMap(g=>g.pages.flatMap(p=>p.links.filter(l=>[l.name,l.description,l.lan,l.wan,g.name,p.name].some(v=>v.toLocaleLowerCase().includes(needle))).map(l=>({l,context:g.name+' · '+p.name}))));
      content=found.map(x=>card(x.l,x.context)).join('');$('.gh-section-name').textContent='搜索结果';$('.gh-section-meta').textContent=found.length+' 个链接';$('.gh-pager').innerHTML='';
      grid.style.setProperty('--gh-columns',String(sheet?.columns||Math.min(3,Math.max(1,Math.floor((grid.clientWidth+10)/160)))));
    }else if(sheet) {
      grid.style.setProperty('--gh-columns',String(sheet.columns));content=sheet.links.map(l=>card(l)).join('');$('.gh-section-name').textContent=sheet.page.name;$('.gh-section-meta').textContent=sheet.page.links.length+' 个链接 · '+sheet.page.rows+' 行 / '+sheet.columns+' 列';
      $('.gh-pager').innerHTML=sheets.map((p,i)=>`<button type="button" class="gh-page-dot" data-sheet="${esc(p.id)}" aria-current="${p.id===sheet.id?'page':'false'}" aria-label="第 ${i+1} 页：${esc(p.page.name)}${p.index?'（续页 '+(p.index+1)+'）':''}"><span aria-hidden="true"></span></button>`).join('');
    }else { $('.gh-section-name').textContent='我的应用';$('.gh-section-meta').textContent='';$('.gh-pager').innerHTML=''; }
    grid.innerHTML=content||`<div class="gh-empty">${query?'没有找到匹配的链接':'这里还没有链接。点击右上角编辑桌面，在管理界面添加。'}</div>`;
    $('#gh-clear').hidden=!query;remember();
    if(animate&&!matchMedia('(prefers-reduced-motion:reduce)').matches)grid.animate([{opacity:.55,transform:'translateY(5px)'},{opacity:1,transform:'translateY(0)'}],{duration:150,easing:'ease-out'});
  }
  function turn(direction) {if(query)return;const {group,sheets,sheet}=current(),index=sheets.indexOf(sheet),next=sheets[index+direction];if(!next)return false;active[group.id]=next.id;render(true);return true;}
  document.addEventListener('click',event=>{
    if(ignoreClick&&event.target.closest('.gh-open')){event.preventDefault();ignoreClick=false;return;}
    const button=event.target.closest('button');if(!button)return;
    if(button.dataset.group){groupID=button.dataset.group;query='';$('.gh-search input').value='';render(true);$('.gh-tabs button[data-group="'+CSS.escape(groupID)+'"]').focus();}
    if(button.dataset.sheet){const {group}=current();active[group.id]=button.dataset.sheet;render(true);$('.gh-page-dot[data-sheet="'+CSS.escape(button.dataset.sheet)+'"]').focus();}
    if(button.dataset.net){network=button.dataset.net;render();}
    if(button.id==='gh-clear'){query='';$('.gh-search input').value='';render();$('.gh-search input').focus();}
  });
  $('.gh-search input').addEventListener('input',event=>{query=event.target.value;render();});
  $('.gh-link-grid').addEventListener('error',event=>{if(event.target.tagName==='IMG')event.target.parentElement.innerHTML=icon('server');},true);
  $('#gh-content').addEventListener('keydown',event=>{if(event.target.closest('input'))return;if(event.key==='ArrowLeft'||event.key==='ArrowRight'){if(turn(event.key==='ArrowLeft'?-1:1)){event.preventDefault();$('.gh-page-dot[aria-current=page]').focus();}}});
  $('.gh-link-grid').addEventListener('touchstart',event=>{ignoreClick=false;swipe=event.touches.length===1?{x:event.touches[0].clientX,y:event.touches[0].clientY}:null;},{passive:true});
  $('.gh-link-grid').addEventListener('touchend',event=>{if(!swipe||!event.changedTouches.length)return;const dx=event.changedTouches[0].clientX-swipe.x,dy=event.changedTouches[0].clientY-swipe.y;swipe=null;if(Math.abs(dx)>60&&Math.abs(dx)>Math.abs(dy)*1.5)ignoreClick=!!turn(dx>0?-1:1);},{passive:true});
  $('.gh-link-grid').addEventListener('touchcancel',()=>{swipe=null;ignoreClick=false;},{passive:true});
  window.addEventListener('resize',()=>render());
  function clock(){if(document.hidden)return;const now=new Date();$('.gh-clock').textContent=now.toLocaleTimeString('zh-CN',{hour:'2-digit',minute:'2-digit',hour12:false});$('.gh-date').textContent=now.toLocaleDateString('zh-CN',{month:'long',day:'numeric',weekday:'long'});}
  async function load(){try{config=await request('/api/homepage');$('#gh-login').hidden=true;$('#gh-login .gh-error').textContent='';$('#gh-content').hidden=false;$('#gh-logout').hidden=config.public;$('#gh-error').textContent='';if(!$('#gh-custom-css')){const link=document.createElement('link');link.id='gh-custom-css';link.rel='stylesheet';link.href='/custom.css';document.head.append(link);}render();}catch(e){$('#gh-content').hidden=true;if(e.status===401){config=null;$('#gh-login').hidden=false;$('#gh-custom-css')?.remove();}else $('#gh-error').textContent=e.message;}}
  $('#gh-login form').addEventListener('submit',async event=>{event.preventDefault();const form=event.target,button=form.querySelector('button');button.disabled=true;form.querySelector('.gh-error').textContent='';try{const username=form.elements.username.value,password=form.elements.password.value;form.elements.password.value='';await request('/login',{username,password});await load();}catch(e){form.querySelector('.gh-error').textContent=e.message;}finally{button.disabled=false;}});
  $('#gh-logout').addEventListener('click',async()=>{try{await request('/logout',{});await load();}catch(e){$('#gh-error').textContent=e.message;}});
  clock();setInterval(clock,15000);document.addEventListener('visibilitychange',clock);load();
})();
