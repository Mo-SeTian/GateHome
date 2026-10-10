import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

const source=readFileSync(new URL('../internal/gateway/web/app.js',import.meta.url),'utf8');
const helpers=source.slice(source.indexOf('function onlineUpdateHTML('),source.indexOf('function maintenanceHTML('));
const root={innerHTML:''},calls=[];
let approve=true,loggedOut=false;
const context=vm.createContext({
  githubTokenConfigured:false,config:{outbound_proxy:{enabled:true}},status:{version:'0.0.21',maintenance_available:true},
  $:()=>root,hydrateIcons:()=>{},panel:(title,description,body)=>body,icon:()=>'',
  esc:value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])),
  date:value=>String(value),confirm:()=>approve,showLogin:()=>{loggedOut=true;vm.runInContext('onlineUpdate.phase="";onlineUpdate.release=null;',context);},toast:()=>{},setTimeout:()=>1,clearTimeout:()=>{},
});
vm.runInContext('const onlineUpdate={release:null,phase:"",error:"",job:null,timer:null,request:0};let maintenancePreview=null;'+helpers,context);
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

// Reloading the settings page can recover an active server-side download.
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
console.log('Online update: duplicate prevention, proxy label, restart confirmation, failure preservation, background progress, reload recovery and error escaping: PASS');
