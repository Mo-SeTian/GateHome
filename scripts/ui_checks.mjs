import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

const source=readFileSync(new URL('../internal/gateway/web/app.js',import.meta.url),'utf8');
const health=vm.createContext({
  icon:()=>'',
  config:{service_health:{enabled:true,interval:60},groups:[{id:'home',enabled:true},{id:'off',enabled:false}],routes:[{id:'ok',group_id:'home',enabled:true},{id:'bad',group_id:'home',enabled:true},{id:'waiting',group_id:'home',enabled:true},{id:'off',group_id:'off',enabled:true},{id:'paused',group_id:'home',enabled:false}]},
  serviceHealthData:{routes:{ok:{healthy:true,checked_at:'2026-11-07T06:32:00Z'},bad:{healthy:false,checked_at:'2026-11-07T06:32:00Z'},off:{healthy:false,checked_at:'2026-11-07T06:32:00Z'}}}
});
vm.runInContext(source.match(/^function routeActive\(.*$/m)[0]+source.slice(source.indexOf('function serviceHealthSummaryHTML('),source.indexOf('function serviceHealthPanelHTML(')),health);
const summary=()=>vm.runInContext('serviceHealthSummaryHTML()',health);
assert.match(summary(),/正常 <b>1<\/b>/);
assert.match(summary(),/异常 <b>1<\/b>/);
assert.match(summary(),/等待检测 1/);
health.serviceHealthData.routes.waiting={state:'pending',healthy:false,checked_at:'0001-01-01T00:00:00Z'};
assert.match(summary(),/等待检测 1/,'a missing sample was reported as a failed service');
health.config.service_health.enabled=false;
assert.match(summary(),/已暂停检查/);
assert.doesNotMatch(summary(),/正常|异常/,'paused checks still announced current health');
console.log('Health toolbar: real samples, pending results, disabled groups/routes and paused checks PASS');

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

let healthWrites=0,healthFocus=0,checkingAdds=0;
const healthCell={dataset:{serviceHealth:'route'},value:'current',contains:()=>true,get innerHTML(){return this.value;},set innerHTML(value){this.value=value;healthWrites++;}};
const healthRefresh=vm.createContext({
  document:{activeElement:{},querySelectorAll:()=>[healthCell],body:{classList:{add:name=>{if(name==='is-checking')checkingAdds++;},remove:()=>{}}}},
  $:selector=>selector==='button'?{focus:()=>{healthFocus++;}}:null,
  api:async()=>({routes:{},notices:[]}),
  serviceHealthBadgeHTML:()=> 'current',serviceHealthSummaryHTML:()=>'',serviceHealthNoticesHTML:()=>'',toast:()=>{}
});
vm.runInContext("let config={},serviceHealthLoading=false,serviceHealthRequest=0,serviceHealthData={routes:{}},page='routes';"+source.slice(source.indexOf('async function refreshServiceHealth('),source.indexOf('function openServiceHealthDetails(')),healthRefresh);
await vm.runInContext('refreshServiceHealth()',healthRefresh);assert.equal(healthWrites,0,'unchanged health polling replaced a focused button');
healthCell.value='older';await vm.runInContext('refreshServiceHealth()',healthRefresh);assert.equal(healthWrites,1);assert.equal(healthFocus,1,'changed health results discarded keyboard focus');
assert.equal(checkingAdds,0,'the five-second poll started the checking sweep on every card');
await vm.runInContext('refreshServiceHealth(true)',healthRefresh);assert.equal(checkingAdds,1,'an explicit recheck did not start the checking sweep');
console.log('Health refresh: unchanged DOM continuity, changed-result focus restoration and quiet polling PASS');

const routeSlice = source.slice(source.indexOf("let routeFilter = '';"), source.indexOf('function groupsHTML()'));
const routeContext = vm.createContext({
  config: {
    routes: [
      { id: 'nas', group_id: 'g1', host: 'nas.example.com', name: '家庭存储', upstream: 'http://192.168.1.10:5000', enabled: true, tls: true, auth: { enabled: true }, firewall_id: '' },
      { id: 'media', group_id: 'g2', host: 'media.example.com', name: '媒体中心', upstream: 'http://192.168.1.30:8096', enabled: true, tls: true, auth: { enabled: false }, firewall_id: '' }
    ],
    groups: [{ id: 'g1', name: '家庭服务', enabled: true }, { id: 'g2', name: '影音与下载', enabled: true }],
    firewalls: []
  },
  serviceHealthData: { routes: { nas: { healthy: true, label: '正常', duration_ms: 12, status: 200, checked_at: new Date().toISOString() }, media: { healthy: false, label: '连接超时', duration_ms: 2500, status: 0, checked_at: new Date().toISOString() } }, options: { interval: 60 } },
  icon: () => '<svg class="icon"></svg>', esc: value => String(value ?? ''),
  objectMenu: () => '<details class="object-menu"></details>', badge: () => '<span class="badge"></span>',
  routeActive: route => route.enabled, routeImageHTML: () => '', firewallWaiting: () => false,
  serviceAddressHTML: route => '<a class="service-address-link" href="#">' + route.upstream + '</a>',
  serviceHealthBadgeHTML: () => '<button class="service-health-result"><span class="health-label">正常</span><span class="mono">12 ms</span></button>',
  empty: () => '', addRouteButton: () => '', reducedMotion: { matches: true }, render: () => { routeContext.rendered = (routeContext.rendered || 0) + 1; }, $: () => null
});
vm.runInContext(routeSlice, routeContext);
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
assert.match(band, /媒体中心 未响应/, 'failing service was not named in the status band');
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
vm.runInContext('filterRoutes({dataset:{group:"g2"}})', routeContext);
assert.equal(routeContext.rendered, 1, 're-selecting the same group re-rendered needlessly');
console.log('Proxy cards: card grid, group filter, failing bar and status band PASS');
