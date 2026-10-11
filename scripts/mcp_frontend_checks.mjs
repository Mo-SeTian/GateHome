import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const source=readFileSync(new URL('../internal/gateway/web/mcp-admin.js',import.meta.url),'utf8');
let pending=[];
const target={innerHTML:''},tokenField={removed:false,remove(){this.removed=true;}};
const context=vm.createContext({
  config:{},page:'settings',location:{origin:'http://127.0.0.1:16666'},
  $:selector=>selector==='#mcp-settings'?target:selector==='#mcp-token'?tokenField:null,
  esc:value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])),
  icon:()=>'',panel:(title,description,body,actions)=>title+description+body+actions,
  hydrateIcons:()=>{},toast:()=>{},applyConfig:()=>{context.applied=true;},
  api:()=>new Promise(resolve=>pending.push(resolve))
});
vm.runInContext(source,context);

const load=vm.runInContext('loadMCPSettings()',context);
pending.shift()({config:{enabled:false,read:false,write:false,maintenance:false,sunpanel_user_id:0},token_configured:false,revision:1,token:''});
await load;
assert.equal(vm.runInContext('mcpSettings.data.config.read',context),true);
assert.ok(target.innerHTML.includes('name="read" type="checkbox" checked'));
assert.ok(!target.innerHTML.includes('name="write" type="checkbox" checked'));
assert.ok(!target.innerHTML.includes('name="maintenance" type="checkbox" checked'));

vm.runInContext('mcpSettings.token="TEST_ONLY_FRONTEND_TOKEN"',context);
const html=vm.runInContext('mcpSettingsHTML()',context);
assert.ok(html.includes('id="mcp-token" type="password" readonly'));
assert.ok(!source.includes('localStorage')&&!source.includes('sessionStorage'));
vm.runInContext('resetMCPSettings()',context);
assert.equal(vm.runInContext('mcpSettings.token',context),'');
assert.equal(tokenField.removed,true);

const staleLoad=vm.runInContext('loadMCPSettings()',context);
vm.runInContext('resetMCPSettings();config=undefined',context);
pending.shift()({config:{enabled:true,read:true},token_configured:true,revision:2});
await staleLoad;
assert.equal(vm.runInContext('mcpSettings.data',context),null,'a late request restored logged-out authorization');

vm.runInContext('config={};mcpSettings.data={revision:3};',context);
const save=vm.runInContext('saveMCPSettings(null,true)',context);
pending.shift()({config:{enabled:true,read:true},token_configured:true,revision:4,token:'TEST_ONLY_FRONTEND_TOKEN'});
await new Promise(setImmediate);
assert.equal(pending.length,1);
vm.runInContext('resetMCPSettings();config=undefined',context);
pending.shift()({config:{},revision:4});
await save;
assert.equal(context.applied,undefined,'late config refresh resurrected a logged-out configuration');
assert.equal(vm.runInContext('mcpSettings.token',context),'','late save restored a dismissed credential');
console.log('MCP settings: query-only default, masked ephemeral token and stale request/logout guards: PASS');
