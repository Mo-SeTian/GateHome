import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

const source=readFileSync(new URL('../internal/gateway/web/app.js',import.meta.url),'utf8');
const motion=source.slice(source.indexOf('function openDialog('),source.indexOf('function setNavigation('));
const reducedMotion={matches:false};
const context=vm.createContext({reducedMotion,getComputedStyle:()=>({opacity:'1',transform:'none'}),document:{activeElement:{}},focusSelector:()=> 'opener'});
vm.runInContext('let routeImageRequest=0,routeImagePending=false;const dialogMotions=new WeakMap(),dialogOpeners=new WeakMap();'+motion,context);

// Control completion deterministically: a canceled exit must never close a reopened dialog.
class ControlledAnimation {
  constructor(options) { this.options=options;this.canceled=false; }
  get finished() {
    return this.promise??=new Promise((resolve,reject)=>{this.resolve=resolve;this.reject=reject;});
  }
  cancel() { this.canceled=true;this.reject?.(new Error('canceled')); }
  finish() { if(!this.canceled) this.resolve?.(); }
}
const dialog={open:false,opens:0,closes:0,animations:[],showModal(){this.open=true;this.opens++;},close(){this.open=false;this.closes++;},animate(frames,options){const a=new ControlledAnimation(options);a.frames=frames;this.animations.push(a);return a;}};
context.dialog=dialog;
const call=name=>vm.runInContext(name+'(dialog)',context);
const settle=async()=>{await Promise.resolve();await Promise.resolve();};

call('openDialog');
assert.equal(dialog.open,true);
assert.equal(dialog.opens,1);
assert.equal(dialog.animations[0].options.duration,240);
call('closeDialog');
assert.equal(dialog.open,true,'exit should retain modal semantics until completion');
const canceledExit=dialog.animations.at(-1);
call('openDialog');
assert.equal(canceledExit.canceled,true);
canceledExit.finish();await settle();
assert.equal(dialog.closes,0,'stale exit closed a reopened dialog');
assert.equal(dialog.opens,1,'interruption opened an already-open native dialog');

call('closeDialog');
const replacedExit=dialog.animations.at(-1);
call('closeDialog');
const finalExit=dialog.animations.at(-1);
replacedExit.finish();await settle();
assert.equal(dialog.closes,0);
finalExit.finish();await settle();
assert.equal(dialog.closes,1,'commit must close exactly once');
assert.equal(dialog.open,false);
assert.equal(finalExit.canceled,true,'settled dialog retained an exit transform');
call('closeDialog');
assert.equal(dialog.closes,1);

reducedMotion.matches=true;
const animationCount=dialog.animations.length;
call('openDialog');call('closeDialog');
assert.equal(dialog.open,false);
assert.equal(dialog.animations.length,animationCount,'reduced motion created a travel animation');
assert.equal(dialog.closes,2);
dialog.id='route-dialog';
call('openDialog');assert.equal(dialog.opens,3,'route editing must always use a native modal');call('closeDialog');
console.log('Dialog entry, canceled/replaced exit, exact settling and reduced motion: PASS');

const collapsedGroups=new Set(),groupClasses=new Set(),groupAttributes=new Map([['aria-expanded','true'],['aria-controls','group-panel-proxy-home']]);
const groupBody={hidden:false,animations:[],getAnimations(){return this.animations;},animate(frames,options){const a=new ControlledAnimation(options);this.animations.push(a);return a;}};
const disclosure={dataset:{panel:'proxy:home'},getAttribute:key=>groupAttributes.get(key),setAttribute:(key,value)=>groupAttributes.set(key,value),querySelector:()=>({textContent:'Home'}),closest:()=>({classList:{toggle(name,on){if(on) groupClasses.add(name);else groupClasses.delete(name);}}})};
const groupMotion=vm.createContext({collapsedGroups,reducedMotion:{matches:false},document:{getElementById:()=>groupBody},icon:()=>'<svg></svg>',esc:value=>String(value).replace(/</g,'&lt;'),disclosure});
vm.runInContext(source.slice(source.indexOf('function groupPanel('),source.indexOf('function actionLabel(')),groupMotion);
const toggle=()=>vm.runInContext('toggleGroupPanel(disclosure)',groupMotion);
toggle();assert.equal(groupBody.hidden,true);assert.equal(groupAttributes.get('aria-expanded'),'false');assert.equal(collapsedGroups.has('proxy:home'),true);
assert.match(vm.runInContext("groupPanel('proxy','home','Home','Summary','Content','','')",groupMotion),/class="group-panel-body" hidden/,'rerender lost the collapsed state');
assert.doesNotMatch(vm.runInContext("groupPanel('ddns','home','DNS','Summary','Content','','')",groupMotion),/class="group-panel-body" hidden/,'one group type affected another');
toggle();assert.equal(groupBody.hidden,false);assert.equal(groupAttributes.get('aria-expanded'),'true');assert.equal(groupBody.animations.length,1);
const interruptedGroup=groupBody.animations[0];toggle();assert.equal(interruptedGroup.canceled,true);assert.equal(groupBody.hidden,true,'old entry left a collapsed panel visible');
toggle();toggle();toggle();assert.equal(groupBody.hidden,false);assert.equal(collapsedGroups.has('proxy:home'),false);
groupMotion.reducedMotion.matches=true;const groupAnimationCount=groupBody.animations.length;toggle();toggle();assert.equal(groupBody.animations.length,groupAnimationCount,'reduced motion animated a group reveal');
assert.equal(groupAttributes.get('aria-label'),'收起Home');
console.log('Group collapse: independent state, rerender retention, rapid reversal and reduced motion: PASS');

const editor={open:true,closes:0,close(){this.open=false;this.closes++;}},editorForm={dataset:{dirty:'true'}};
const discard=vm.createContext({$:selector=>selector==='#route-dialog'?editor:editorForm,dialogMotions:new WeakMap(),confirm:()=>false});
vm.runInContext(source.slice(source.indexOf('function leaveRouteEditor('),source.indexOf('function subscriptionsHTML(')),discard);
assert.equal(vm.runInContext('leaveRouteEditor()',discard),false);assert.equal(editor.open,true,'canceling discard must retain the editor');assert.equal(editor.closes,0);
discard.confirm=()=>true;assert.equal(vm.runInContext('leaveRouteEditor()',discard),true);assert.equal(editor.closes,1);
console.log('Unsaved service edits: canceled navigation retains the editor; confirmed discard closes it: PASS');

const pendingEditor=vm.createContext({busy:true});
vm.runInContext(source.slice(source.indexOf('function openRoute('),source.indexOf('function firewallGroupFields(',source.indexOf('function openRoute('))),pendingEditor);
assert.doesNotThrow(()=>vm.runInContext('openRoute(1)',pendingEditor),'a pending configuration write exposed stale route indexes');
console.log('Pending configuration write: opening a service waits for the committed list: PASS');

const pending=[];
const statistics=vm.createContext({URLSearchParams,api:()=>new Promise((resolve,reject)=>pending.push({resolve,reject})),render:()=>{}});
vm.runInContext("let statisticsRequest=0,statisticsLoading=false,statisticsError='',statisticsView={marker:'retained'},statisticsHours='24',statisticsRule='',page='statistics';"+source.slice(source.indexOf('async function loadStatistics('),source.indexOf('function downloadStatistics(')),statistics);
const first=vm.runInContext('loadStatistics()',statistics);
assert.equal(vm.runInContext('statisticsView.marker',statistics),'retained','refresh discarded visible charts');
assert.equal(vm.runInContext('statisticsLoading',statistics),true);
const second=vm.runInContext('loadStatistics()',statistics);
pending[1].resolve({marker:'latest'});await second;
pending[0].resolve({marker:'stale'});await first;
assert.equal(vm.runInContext('statisticsView.marker',statistics),'latest','late response replaced the selected result');
assert.equal(vm.runInContext('statisticsLoading',statistics),false);
const failed=vm.runInContext('loadStatistics()',statistics);
pending[2].reject(new Error('test request failed'));
await assert.rejects(failed,/test request failed/);
assert.equal(vm.runInContext('statisticsView.marker',statistics),'latest','failed refresh discarded last valid charts');
assert.equal(vm.runInContext('statisticsLoading',statistics),false,'failed refresh left loading feedback active');
assert.equal(vm.runInContext('statisticsError',statistics),'test request failed');
console.log('Statistics refresh continuity, competing responses and failure recovery: PASS');

const pendingBlocks=[];
let blockRenders=0;
const blocks=vm.createContext({Date,document:{querySelectorAll:()=>[]},api:()=>new Promise((resolve,reject)=>pendingBlocks.push({resolve,reject})),render:()=>{blockRenders++;}});
vm.runInContext("let ipBlockRequest=0,ipBlockView={marker:'retained'},ipBlockReceived=0,ipBlockError='',page='ip-blocks';"+source.slice(source.indexOf('async function loadIPBlocks('),source.indexOf('function openIPBlock(')),blocks);
const older=vm.runInContext('loadIPBlocks()',blocks),newer=vm.runInContext('loadIPBlocks()',blocks);
pendingBlocks[1].resolve({marker:'latest'});await newer;pendingBlocks[0].resolve({marker:'stale'});await older;
assert.equal(vm.runInContext('ipBlockView.marker',blocks),'latest','late list response restored old blocks');
const beforeMutation=vm.runInContext('loadIPBlocks()',blocks);
vm.runInContext("++ipBlockRequest;ipBlockView={marker:'released'}",blocks);
pendingBlocks[2].resolve({marker:'stale block'});await beforeMutation;
assert.equal(vm.runInContext('ipBlockView.marker',blocks),'released','in-flight poll restored a manually released IP');
const failedBlocks=vm.runInContext('loadIPBlocks()',blocks);
pendingBlocks[3].reject(new Error('test list failure'));await assert.rejects(failedBlocks,/test list failure/);
assert.equal(vm.runInContext('ipBlockView.marker',blocks),'released');
assert.equal(vm.runInContext('ipBlockError',blocks),'test list failure');
vm.runInContext("ipBlockView={entries:[]};ipBlockError=''",blocks);
const rendersBeforePoll=blockRenders,quietPoll=vm.runInContext('loadIPBlocks(true)',blocks);
pendingBlocks[4].resolve({entries:[],now:new Date().toISOString()});await quietPoll;
assert.equal(blockRenders,rendersBeforePoll,'unchanged polling replaced live buttons and form focus');
console.log('IP list: competing reads, mutation invalidation and failed refresh continuity: PASS');

const pendingEvents=[],eventFocus=[];
const records=vm.createContext({URLSearchParams,Date,document:{activeElement:{dataset:{action:'event-page'}}},focusSelector:()=> '#opener',$:selector=>({focus:()=>eventFocus.push(selector)}),api:path=>new Promise((resolve,reject)=>pendingEvents.push({path,resolve,reject})),render:()=>{}});
vm.runInContext("let page='firewall-logs';"+source.match(/^function emptyEventFilters.*$/m)[0]+source.match(/^const securityEvents=.*$/m)[0]+source.slice(source.indexOf('async function loadSecurityEvents('),source.indexOf('function securityStatisticsHTML(')),records);
vm.runInContext("securityEvents.firewall.view={marker:'retained',through:125};securityEvents.firewall.filters.ip='8.8.8.8';",records);
const eventFirst=vm.runInContext("loadSecurityEvents('firewall')",records);
assert.equal(vm.runInContext('securityEvents.firewall.view.marker',records),'retained');
vm.runInContext("securityEvents.firewall.filters.ip='1.1.1.1'",records);
const eventSecond=vm.runInContext("loadSecurityEvents('firewall')",records);
pendingEvents[1].resolve({marker:'latest',through:126});await eventSecond;
pendingEvents[0].resolve({marker:'stale',through:125});await eventFirst;
assert.equal(vm.runInContext('securityEvents.firewall.view.marker',records),'latest');
vm.runInContext("securityEvents.firewall.filters.ip='8.8.8.8'",records);
const eventPage=vm.runInContext("loadSecurityEvents('firewall',2,true)",records);
const pageQuery=new URLSearchParams(pendingEvents[2].path.split('?')[1]);
assert.equal(pageQuery.get('through'),'126');assert.equal(pageQuery.get('ip'),'1.1.1.1');assert.equal(pageQuery.get('page'),'2');
pendingEvents[2].resolve({marker:'page2',through:126});await eventPage;
assert.ok(eventFocus.includes('#firewall-event-result'),'page changes must move focus to the result summary');
const eventFailed=vm.runInContext("loadSecurityEvents('firewall')",records);
pendingEvents[3].reject(new Error('invalid IP'));await assert.rejects(eventFailed,/invalid IP/);
assert.equal(vm.runInContext('securityEvents.firewall.view.marker',records),'page2');
assert.equal(vm.runInContext('securityEvents.firewall.loading',records),false);
assert.equal(vm.runInContext('securityEvents.firewall.error',records),'invalid IP');
assert.ok(eventFocus.includes('#firewall-event-error'));
const safety=vm.runInContext("loadSecurityEvents('security')",records);
pendingEvents[4].resolve({marker:'safety'});await safety;
assert.equal(vm.runInContext('securityEvents.firewall.view.marker',records),'page2','statistics events replaced firewall results');
const revoked=vm.runInContext("loadSecurityEvents('firewall')",records);
vm.runInContext('securityEvents.firewall.request++;securityEvents.firewall.view=null;securityEvents.firewall.loading=false;',records);
pendingEvents[5].resolve({marker:'revoked'});await revoked;
assert.equal(vm.runInContext('securityEvents.firewall.view',records),null,'a response after logout restored old events');
console.log('Security records: independent lists, latest response, retained errors, applied filter snapshot and revoked reads: PASS');
