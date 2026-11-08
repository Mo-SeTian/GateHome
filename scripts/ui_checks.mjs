import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

const source=readFileSync(new URL('../internal/gateway/web/app.js',import.meta.url),'utf8');
let Loader,preference,intersection,destroyed=0,observed=0,removed=0;
const updates=[],media={matches:false,addEventListener:(_,fn)=>{preference=fn;},removeEventListener:(_,fn)=>{assert.equal(fn,preference);removed++;}};
const component=vm.createContext({
  HTMLElement:class {setAttribute(){}},
  customElements:{define:(name,element)=>{assert.equal(name,'gatehome-loader');Loader=element;}},
  matchMedia:()=>media,
  IntersectionObserver:class {constructor(fn){intersection=fn;}observe(){observed++;}disconnect(){observed--;}},
  mount:()=>({update:value=>updates.push(value),destroy:()=>{destroyed++;}})
});
vm.runInContext(readFileSync(new URL('../internal/gateway/ui/leibox/loader.js',import.meta.url),'utf8').replace(/^import .*;\n/,''),component);
const loader=new Loader();loader.connectedCallback();
intersection([{isIntersecting:false}]);assert.equal(updates.at(-1).reducedMotion,true,'offscreen animation kept running');
intersection([{isIntersecting:true}]);assert.equal(updates.at(-1).reducedMotion,false);
media.matches=true;preference();assert.equal(updates.at(-1).reducedMotion,true);
loader.disconnectedCallback();assert.equal(destroyed,1);assert.equal(observed,0);assert.equal(removed,1);
loader.connectedCallback();loader.disconnectedCallback();assert.equal(destroyed,2);assert.equal(observed,0);assert.equal(removed,2);
console.log('LeiBox lifecycle: offscreen pause, reduced motion, disconnect cleanup and reconnect PASS');

let click;
const clicks=vm.createContext({document:{addEventListener:(_,listener)=>{click=listener;}},config:{},busy:false});
vm.runInContext(source.match(/^document.addEventListener\('click',async event=>\{[\s\S]*?^\}\);/m)[0],clicks);
await assert.doesNotReject(click({target:{closest:()=>({dataset:{},id:''})}}),'a normal submit button reached a missing action string');
console.log('Shared buttons: form submission without data-action does not throw PASS');

const focusDocument={activeElement:null},focusables=[];
for(let i=0;i<3;i++) focusables.push({disabled:false,tabIndex:0,closest:()=>null,getClientRects:()=>[{}],focus(){focusDocument.activeElement=this;}});
const focusDialog={querySelectorAll:()=>focusables,contains:el=>focusables.includes(el)};
const focus=vm.createContext({document:focusDocument,$:()=>focusDialog});
vm.runInContext(source.slice(source.indexOf('function trapDialogFocus('),source.indexOf('function openDialog(')),focus);
let prevented=0;focus.event={key:'Tab',shiftKey:false,preventDefault:()=>{prevented++;}};
focusDocument.activeElement=focusables[2];vm.runInContext('trapDialogFocus(event)',focus);assert.equal(focusDocument.activeElement,focusables[0]);
focus.event.shiftKey=true;vm.runInContext('trapDialogFocus(event)',focus);assert.equal(focusDocument.activeElement,focusables[2]);
focusDocument.activeElement=focusables[1];vm.runInContext('trapDialogFocus(event)',focus);assert.equal(prevented,2,'ordinary tab order was replaced');
focusDocument.activeElement={};focus.event.shiftKey=false;vm.runInContext('trapDialogFocus(event)',focus);assert.equal(focusDocument.activeElement,focusables[0]);
focusables.push({disabled:false,tabIndex:0,closest:()=>({querySelector:()=>({contains:()=>false})}),getClientRects:()=>[{}],focus(){throw Error('closed disclosure received focus');}});
focusDocument.activeElement=focusables[2];vm.runInContext('trapDialogFocus(event)',focus);assert.equal(focusDocument.activeElement,focusables[0]);
console.log('Modal keyboard: forward/backward boundaries, ordinary order and outside focus recovery PASS');

const menuClasses=new Set();
let anchor={right:110,top:600,bottom:644};
const menu={classList:{remove:(...names)=>names.forEach(name=>menuClasses.delete(name)),toggle:(name,on)=>on?menuClasses.add(name):menuClasses.delete(name)},querySelector:selector=>({getBoundingClientRect:()=>selector==='summary'?anchor:{width:160,height:144}})};
const menuContext=vm.createContext({window:{innerHeight:740},menu});
vm.runInContext(source.slice(source.indexOf('function positionObjectMenu('),source.indexOf('function routeTable(')),menuContext);
vm.runInContext('positionObjectMenu(menu)',menuContext);assert.equal(menuClasses.has('align-start'),true);assert.equal(menuClasses.has('opens-up'),true);
anchor={right:300,top:100,bottom:144};vm.runInContext('positionObjectMenu(menu)',menuContext);assert.equal(menuClasses.size,0,'old menu geometry survived a new position');
menuContext.window.innerWidth=390;menuContext.window.innerHeight=844;anchor={right:300,top:576,bottom:620};
vm.runInContext('positionObjectMenu(menu)',menuContext);assert.equal(menuClasses.has('opens-up'),false,'phone menu flipped up although the bottom navigation is gone');
anchor={right:300,top:690,bottom:734};menuClasses.clear();
vm.runInContext('positionObjectMenu(menu)',menuContext);assert.equal(menuClasses.has('opens-up'),true,'phone menu ran past the viewport edge');
console.log('Action menus: narrow left edge, lower viewport edge and repositioning PASS');

let healthWrites=0,healthFocus=0;
let failingCard=false;
const card={classList:{toggle:(_,value)=>{failingCard=value;}}};
const healthCell={dataset:{serviceHealth:'route'},value:'current',contains:()=>true,closest:()=>card,get innerHTML(){return this.value;},set innerHTML(value){this.value=value;healthWrites++;}};
const healthRefresh=vm.createContext({
  document:{activeElement:{},querySelectorAll:()=>[healthCell]},
  $:selector=>selector==='button'?{focus:()=>{healthFocus++;}}:null,
  api:async()=>({routes:{},notices:[]}),
  serviceHealthBadgeHTML:()=> 'current',serviceHealthSample:()=>({healthy:true}),serviceHealthNoticesHTML:()=>'',toast:()=>{}
});
vm.runInContext("let config={},serviceHealthPending=null,serviceHealthRequest=0,serviceHealthData={routes:{}},page='routes';"+source.slice(source.indexOf('async function refreshServiceHealth('),source.indexOf('function openServiceHealthDetails(')),healthRefresh);
await vm.runInContext('refreshServiceHealth()',healthRefresh);assert.equal(healthWrites,0,'unchanged health polling replaced a focused button');
healthCell.value='older';await vm.runInContext('refreshServiceHealth()',healthRefresh);assert.equal(healthWrites,1);assert.equal(healthFocus,1,'changed health results discarded keyboard focus');
healthRefresh.serviceHealthSample=()=>({healthy:false});
await vm.runInContext('refreshServiceHealth()',healthRefresh);assert.equal(failingCard,true,'a failed poll did not mark the service card');
healthRefresh.serviceHealthSample=()=>({healthy:true});
await vm.runInContext('refreshServiceHealth()',healthRefresh);assert.equal(failingCard,false,'the service card still showed a failure after recovery');
let finishHealthRequest,healthRequests=0;
healthRefresh.api=()=>new Promise(resolve=>{healthRequests++;finishHealthRequest=resolve;});
const backgroundRefresh=vm.runInContext('refreshServiceHealth()',healthRefresh);
const manualRefresh=vm.runInContext('refreshServiceHealth()',healthRefresh);
let manualDone=false;
manualRefresh.then(()=>{manualDone=true;});
await new Promise(resolve=>setImmediate(resolve));
assert.equal(healthRequests,1,'a manual refresh duplicated an active poll');
assert.equal(manualDone,false,'a manual refresh completed before the shared request');
finishHealthRequest({routes:{},notices:[]});
await Promise.all([backgroundRefresh,manualRefresh]);
console.log('Health refresh: stable focus, recovery and request deduplication PASS');

const routeSlice = source.slice(source.indexOf("let routeFilter = '';"), source.indexOf('function prepareTables('));
const routeContext = vm.createContext({
  config: {
    routes: [
      { id: 'nas', group_id: 'g1', host: 'nas.example.com', name: '家庭存储', upstream: 'http://192.168.1.10:5000', enabled: true, tls: true, auth: { enabled: true }, firewall_id: '' },
      { id: 'media', group_id: 'g2', host: 'media.example.com', name: '媒体中心', upstream: 'http://192.168.1.30:8096', enabled: true, tls: true, auth: { enabled: false }, firewall_id: '' }
    ],
    groups: [{ id: 'g1', name: '家庭服务', enabled: true }, { id: 'g2', name: '影音与下载', enabled: true }],
    firewalls: [], ddns: {groups:[]}, service_health:{enabled:true,interval:60}
  },
  serviceHealthData: { routes: { nas: { healthy: true, label: '正常', duration_ms: 12, status: 200, checked_at: new Date().toISOString() }, media: { healthy: false, label: '连接超时', duration_ms: 2500, status: 0, checked_at: new Date().toISOString() } }, options: { interval: 60 } },
  icon: () => '<svg class="icon"></svg>', esc: value => String(value ?? ''),
  objectMenu: (_,actions) => '<details class="object-menu">'+actions+'</details>', badge: text => '<span class="badge">'+text+'</span>',
  routeImageHTML: () => '', firewallWaiting: () => false,
  serviceAddressHTML: route => '<a class="service-address-link" href="#">' + route.upstream + '</a>',
  serviceHealthBadgeHTML: () => '<button class="service-health-result"><span class="health-label">正常</span><span class="mono">12 ms</span></button>',
  empty: () => '', addRouteButton: () => '', discoveryButton:()=>'', portsText:()=>'', groupBadge:()=>'', heading:()=>'', serviceHealthNoticesHTML:()=>'', groupPanel:(_,id,title,description,body,actions)=>title+body+actions,
  reducedMotion: { matches: true }, render: () => { routeContext.rendered = (routeContext.rendered || 0) + 1; }, $: () => null
});
vm.runInContext(source.match(/^function routeActive\(.*$/m)[0]+'\n'+routeSlice, routeContext);
const cards = vm.runInContext('routeCards("g1")', routeContext);
assert.match(cards, /class="cards"/, 'proxy services did not render as a card grid');
assert.match(cards, /class="route-card"/);
assert.match(cards, /class="rc-name">家庭存储</, 'card identity missing');
assert.match(cards, /data-route-key="g1\/nas.example.com"/, 'card lost its route key');
assert.match(cards, /rc-host">nas.example.com</);
assert.equal((cards.match(/class="route-card/g) || []).length, 1, 'group filter leaked other groups into the card grid');
assert.match(cards, /rc-tag">HTTPS</, 'protocol tag missing');
assert.match(cards, /独立账号/, 'access tag missing');

const failingCards = vm.runInContext('routeCards("g2")', routeContext);
assert.match(failingCards, /route-card is-failing/, 'failing service did not get the alert bar');
assert.match(failingCards, /公开访问/, 'public access tag missing');

const band = vm.runInContext('routeStatusBandHTML()', routeContext);
assert.match(band, /class="status-band"/);
assert.match(band, /服务总数/);
assert.match(band, /媒体中心 检查异常/, 'failing service was not named in the status band');
assert.match(band, />1</, 'failing count missing from the band');

const chips = vm.runInContext('routeFilterChipsHTML()', routeContext);
assert.match(chips, /class="chip active"[^>]*data-group=""/, 'all-groups chip was not the default selection');
assert.match(chips, /家庭服务<span class="chip-count">1</);
assert.match(chips, /影音与下载<span class="chip-count">1</);
assert.match(chips, /data-no-icon/, 'chips must opt out of the automatic button icon');

vm.runInContext('filterRoutes({dataset:{group:"g2"}})', routeContext);
assert.equal(routeContext.rendered, 1, 'filtering did not re-render the page');
assert.equal(vm.runInContext('routeFilter', routeContext), 'g2');
const filteredChips = vm.runInContext('routeFilterChipsHTML()', routeContext);
assert.match(filteredChips, /chip active"[^>]*data-group="g2"/, 'active filter chip not reflected');
const filteredGroups=vm.runInContext('groupsHTML()',routeContext);
for(const action of ['edit-group','toggle-group','delete-group']) {
  assert.match(filteredGroups,new RegExp('data-action="'+action+'" data-index="1"'),'filtering changed the saved group index');
}
assert.doesNotMatch(filteredGroups,/data-action="(?:edit|toggle|delete)-group" data-index="0"/);
vm.runInContext('filterRoutes({dataset:{group:"g2"}})', routeContext);
assert.equal(routeContext.rendered, 1, 're-selecting the same group re-rendered needlessly');
console.log('Proxy cards: card grid, group filter, failing bar and status band PASS');

routeContext.serviceHealthData.routes.media={state:'pending',healthy:false,checked_at:'0001-01-01T00:00:00Z'};
assert.match(vm.runInContext('routeStatusBandHTML()',routeContext),/1 个服务等待检测/);
assert.doesNotMatch(vm.runInContext('routeCards("g2")',routeContext),/is-failing/);
routeContext.config.service_health.enabled=false;
assert.match(vm.runInContext('routeStatusBandHTML()',routeContext),/健康检查已暂停/);
assert.match(vm.runInContext('routeStatusBandHTML()',routeContext),/data-health-bad>0</);
assert.doesNotMatch(vm.runInContext('routeCards("g1")',routeContext),/is-failing/);
routeContext.config.service_health.enabled=true;
routeContext.config.routes[1].enabled=false;
routeContext.serviceHealthData.routes.media={state:'timeout',healthy:false,checked_at:new Date().toISOString()};
assert.doesNotMatch(vm.runInContext('routeCards("g2")',routeContext),/is-failing/);
console.log('Health status: waiting, paused, and disabled services are not failures PASS');

const forwarding=vm.createContext({
  config:{forwards:[],firewalls:[]},status:{},
  esc:value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])),
  badge:(text,kind='')=>`<span class="${kind}">${text}</span>`,
  icon:()=>'',heading:()=>'',empty:(title)=>title,objectMenu:(_name,body)=>body,
  firewallName:()=> '不限制来源 IP',firewallWaiting:()=>false
});
vm.runInContext("let forwardProtocolFilter='';"+source.slice(source.indexOf('function forwardState('),source.indexOf('function openForward(')),forwarding);
assert.match(vm.runInContext('forwardsHTML()',forwarding),/还没有转发规则/);
forwarding.rule={id:'native',name:'<script>test</script>',protocol:'tcp',listen_port:20022,upstream:'[::1]:22',firewall_id:'',enabled:true};
forwarding.config.forwards=[forwarding.rule];
assert.match(vm.runInContext('forwardState(rule)',forwarding),/启用待重启/);
forwarding.status.forward_listeners=[structuredClone(forwarding.rule)];
assert.match(vm.runInContext('forwardState(rule)',forwarding),/正在监听/);
forwarding.rule.name='rename only';
assert.match(vm.runInContext('forwardState(rule)',forwarding),/正在监听/);
for(const [key,value] of [['upstream','[::1]:23'],['listen_port',20023],['protocol','udp'],['firewall_id','private']]) {
  const previous=forwarding.rule[key];forwarding.rule[key]=value;
  assert.match(vm.runInContext('forwardState(rule)',forwarding),/修改待重启/);
  forwarding.rule[key]=previous;
}
forwarding.rule.protocol='udp';
forwarding.status.forward_listeners[0].protocol='udp';
forwarding.rule.udp_idle_seconds=300;
assert.match(vm.runInContext('forwardState(rule)',forwarding),/正在监听/);
forwarding.rule.udp_idle_seconds=600;
assert.match(vm.runInContext('forwardState(rule)',forwarding),/修改待重启/);
forwarding.rule.udp_idle_seconds=300;
forwarding.rule.enabled=false;
assert.match(vm.runInContext('forwardState(rule)',forwarding),/停用待重启/);
forwarding.status.forward_listeners=[];
assert.match(vm.runInContext('forwardState(rule)',forwarding),/已停用/);
forwarding.rule.name='<script>test</script>';
const forwardHTML=vm.runInContext('forwardsHTML()',forwarding);
assert.ok(!forwardHTML.includes('<script>'));
assert.match(forwardHTML,/&lt;script&gt;/);
assert.match(forwardHTML,/\[::1\]:22/);
for(const action of ['edit-forward','toggle-forward','delete-forward']) assert.match(forwardHTML,new RegExp('data-action="'+action+'" data-index="0"'));
forwarding.config.forwards.unshift({...forwarding.rule,id:'tcp',name:'TCP fixture',protocol:'tcp'});
const udpOnly=vm.runInContext("forwardProtocolFilter='udp';forwardsHTML()",forwarding);
assert.doesNotMatch(udpOnly,/TCP fixture/);
assert.match(udpOnly,/data-action="edit-forward" data-index="1"/,'filter changed the original rule index');
console.log('Port forwards: running versus pending changes, disabled rules, IPv6 and escaped names PASS');

const udpInput={disabled:false,closest:()=>udpLabel},udpLabel={hidden:false},udpForm={elements:{protocol:{value:'tcp'},udp_idle_seconds:udpInput}};
const protocolContext=vm.createContext({form:udpForm});
vm.runInContext(source.match(/function updateForwardProtocol\(form\) \{[\s\S]*?\n\}/)[0],protocolContext);
vm.runInContext('updateForwardProtocol(form)',protocolContext);
assert.equal(udpInput.disabled,true);assert.equal(udpLabel.hidden,true);
udpForm.elements.protocol.value='udp';
vm.runInContext('updateForwardProtocol(form)',protocolContext);
assert.equal(udpInput.disabled,false);assert.equal(udpLabel.hidden,false);
console.log('Port forwards: protocol-specific UDP timeout validation PASS');
