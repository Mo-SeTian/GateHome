import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

const source=readFileSync(new URL('../internal/gateway/web/app.js',import.meta.url),'utf8');
const helpers=source.slice(source.indexOf('function onlineUpdateHTML('),source.indexOf('function maintenanceHTML('));
const root={innerHTML:''},calls=[];
let approve=true,loggedOut=false;
const context=vm.createContext({
  githubTokenConfigured:false,config:{outbound_proxy:{enabled:true}},status:{version:'0.0.21',maintenance_available:true,update_history_available:true},
  $:selector=>selector==='#online-update'?root:null,hydrateIcons:()=>{},panel:(title,description,body)=>body,icon:()=>'',
  esc:value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])),
  date:value=>String(value),confirm:()=>approve,showLogin:()=>{loggedOut=true;vm.runInContext('onlineUpdate.phase="";onlineUpdate.release=null;',context);},toast:()=>{},setTimeout:()=>1,clearTimeout:()=>{},
});
vm.runInContext('const onlineUpdate={channel:"stable",release:null,phase:"",error:"",job:null,timer:null,request:0};let maintenancePreview=null;'+helpers,context);
const release={version:'0.0.22',update_available:true,size:1048576,published_at:'2026-10-09',release_url:'https://github.com/Mo-SeTian/GateHome/releases/tag/v0.0.22'};
const snapshot=()=>JSON.parse(vm.runInContext('JSON.stringify(onlineUpdate)',context));

let finishCheck;
context.api=async(path,method,body)=>{
  calls.push({path,method,body});
  return await new Promise(resolve=>{finishCheck=resolve;});
};
const pending=vm.runInContext('checkOnlineUpdate()',context);
assert.equal(snapshot().phase,'checking');
assert.match(root.innerHTML,/aria-busy="true"/);
assert.match(root.innerHTML,/data-action="check-online-update" disabled/);
await vm.runInContext('checkOnlineUpdate()',context);
assert.equal(calls.length,1,'repeated click sent a duplicate request');
finishCheck(release);await pending;
assert.equal(snapshot().phase,'');
assert.match(root.innerHTML,/使用已保存的出站代理/);
assert.match(root.innerHTML,/有新版本可用/);
assert.ok(!/data-action="install-online-update" disabled/.test(root.innerHTML));

calls.length=0;approve=false;
await vm.runInContext('installOnlineUpdate()',context);
assert.equal(calls.length,0,'update proceeded without the restart confirmation');
approve=true;
context.api=async(path,method,body)=>{calls.push({path,method,body});throw new Error('下载校验失败');};
await vm.runInContext('installOnlineUpdate()',context);
assert.equal(calls.length,1);
assert.equal(calls[0].path,'maintenance/download-online-update');
assert.equal(calls[0].body.version,release.version);
assert.equal(loggedOut,false,'download failure interrupted the active session');
assert.match(root.innerHTML,/下载校验失败/);
assert.equal(snapshot().phase,'');

calls.length=0;
context.api=async(path,method,body)=>{
  calls.push({path,method,body});
  if(path.endsWith('download-online-update')) return {phase:'checking',version:release.version};
  return {phase:'error',version:release.version,error:'后台下载校验失败'};
};
await vm.runInContext('installOnlineUpdate()',context);
assert.equal(calls.length,2);
assert.equal(calls[1].path,'maintenance/online-update-status');
assert.match(root.innerHTML,/后台下载校验失败/);
assert.equal(loggedOut,false);
assert.equal(snapshot().phase,'');

calls.length=0;
let finishDownload;
context.api=async(path,method,body)=>{
  calls.push({path,method,body});
  if(path.endsWith('download-online-update')) return await new Promise(resolve=>{finishDownload=resolve;});
  return {phase:'downloading',version:release.version,total:1048576,downloaded:524288};
};
const installing=vm.runInContext('installOnlineUpdate()',context);
assert.equal(snapshot().phase,'downloading');
assert.match(root.innerHTML,/1 \/ 3/);
await vm.runInContext('installOnlineUpdate()',context);
assert.equal(calls.length,1,'double click downloaded twice');
finishDownload({phase:'checking',version:release.version});await installing;
assert.equal(calls.length,2);
assert.match(root.innerHTML,/50%/);
assert.match(root.innerHTML,/aria-label="更新包下载进度"/);
assert.match(root.innerHTML,/data-action="install-online-update" disabled/);
context.api=async()=>({phase:'verifying',version:release.version});
await vm.runInContext('loadOnlineUpdateStatus()',context);
assert.equal(snapshot().phase,'applying');
assert.match(root.innerHTML,/2 \/ 3/);
context.api=async()=>({phase:'restarting',version:release.version});
await vm.runInContext('loadOnlineUpdateStatus()',context);
assert.equal(loggedOut,true);
assert.equal(snapshot().release,null);
assert.equal(snapshot().phase,'');

// Reloading the update page can recover an active server-side download.
context.api=async()=>({phase:'downloading',version:release.version,total:100,downloaded:25});
await vm.runInContext('loadOnlineUpdateStatus()',context);
assert.equal(snapshot().phase,'downloading');
assert.match(root.innerHTML,/25%/);
vm.runInContext('onlineUpdate.phase="";',context);

context.api=async()=>{throw new Error('<script>test</script>');};
await vm.runInContext('checkOnlineUpdate()',context);
assert.ok(!root.innerHTML.includes('<script>'));
assert.match(root.innerHTML,/&lt;script&gt;/);
assert.match(root.innerHTML,/data-action="install-online-update" disabled/);

vm.runInContext('selectUpdateChannel("beta")',context);
assert.equal(snapshot().channel,'beta');
assert.equal(snapshot().release,null,'changing channel retained the previous release');
let finishBeta;
context.api=async(path,method,body)=>{
  assert.equal(body.channel,'beta');
  return new Promise(resolve=>{finishBeta=resolve;});
};
const betaCheck=vm.runInContext('checkOnlineUpdate()',context);
vm.runInContext('selectUpdateChannel("stable")',context);
assert.equal(snapshot().channel,'beta','channel changed during a version check');
finishBeta({...release,channel:'beta',version:'0.0.23-beta.10'});await betaCheck;
assert.equal(snapshot().release.version,'0.0.23-beta.10');
context.api=async()=>({channel:'beta',version:'',update_available:false});
await vm.runInContext('checkOnlineUpdate()',context);
assert.match(root.innerHTML,/暂未发布 Beta/);
assert.ok(!root.innerHTML.includes('href=""'),'empty beta channel rendered an empty release link');
assert.match(root.innerHTML,/data-action="install-online-update" disabled/);
// An old request must not restore authenticated page state after logout.
context.api=async()=>new Promise(resolve=>{finishBeta=resolve;});
const staleCheck=vm.runInContext('checkOnlineUpdate()',context);
vm.runInContext('onlineUpdate.request++;onlineUpdate.phase="";config=undefined;',context);
finishBeta(release);await staleCheck;
assert.equal(snapshot().release,null,'a stale check resurrected release state after logout');
context.config={outbound_proxy:{enabled:true}};
console.log('Online update: duplicate prevention, proxy label, restart confirmation, failure preservation, background progress, reload recovery and error escaping: PASS');
console.log('Update channels: independent selection, pending guard, empty Beta and stale login response: PASS');

let reloads=0;
const versionContext=vm.createContext({
  status:{version:'0.0.60'},page:'settings',
  api:async()=>({version:'0.0.61',uptime_seconds:0,restart_required:false}),
  location:{reload(){reloads++;}},
  $:()=>({classList:{add(){},remove(){}}})
});
vm.runInContext(source.slice(source.indexOf('async function refreshStatus('),source.indexOf('async function load()')),versionContext);
await assert.rejects(vm.runInContext('refreshStatus()',versionContext),/正在重新加载界面/);
assert.equal(reloads,1,'a new server version retained the old page assets');
versionContext.status={version:'0.0.61'};
await vm.runInContext('refreshStatus()',versionContext);
versionContext.status={};
await vm.runInContext('refreshStatus()',versionContext);
assert.equal(reloads,1,'initial load or unchanged server version caused a reload loop');
console.log('Version change: reload old page assets; initial and unchanged versions do not loop PASS');
