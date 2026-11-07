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
console.log('Action menus: narrow left edge, lower viewport edge and repositioning PASS');

let healthWrites=0,healthFocus=0;
const healthCell={dataset:{serviceHealth:'route'},value:'current',contains:()=>true,get innerHTML(){return this.value;},set innerHTML(value){this.value=value;healthWrites++;}};
const healthRefresh=vm.createContext({
  document:{activeElement:{},querySelectorAll:()=>[healthCell]},
  $:selector=>selector==='button'?{focus:()=>{healthFocus++;}}:null,
  api:async()=>({routes:{},notices:[]}),
  serviceHealthBadgeHTML:()=> 'current',serviceHealthSummaryHTML:()=>'',serviceHealthNoticesHTML:()=>'',toast:()=>{}
});
vm.runInContext("let config={},serviceHealthLoading=false,serviceHealthRequest=0,serviceHealthData={routes:{}},page='routes';"+source.slice(source.indexOf('async function refreshServiceHealth('),source.indexOf('function openServiceHealthDetails(')),healthRefresh);
await vm.runInContext('refreshServiceHealth()',healthRefresh);assert.equal(healthWrites,0,'unchanged health polling replaced a focused button');
healthCell.value='older';await vm.runInContext('refreshServiceHealth()',healthRefresh);assert.equal(healthWrites,1);assert.equal(healthFocus,1,'changed health results discarded keyboard focus');
console.log('Health refresh: unchanged DOM continuity and changed-result focus restoration PASS');
