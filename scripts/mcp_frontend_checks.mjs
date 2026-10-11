import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const source=readFileSync(new URL('../internal/gateway/web/mcp-admin.js',import.meta.url),'utf8');
let pending=[],calls=[],confirmations=0;
const copied=[];
const target={innerHTML:''},tokenField={removed:false,remove(){this.removed=true;}};
const context=vm.createContext({
  config:{},page:'mcp',location:{origin:'http://127.0.0.1:16666'},
  $:selector=>selector==='#mcp-settings'?target:selector==='#mcp-token'?tokenField:null,
  esc:value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])),
  icon:()=>'',heading:(eyebrow,title,description,actions)=>title+description+actions,panel:(title,description,body,actions)=>title+description+body+actions,
  hydrateIcons:()=>{},toast:()=>{},applyConfig:()=>{context.applied=true;},
  confirm:()=>{confirmations++;return false;},navigator:{clipboard:{writeText:async text=>copied.push(text)}},
  api:(path,method,payload)=>{calls.push({path,method,payload});return new Promise(resolve=>pending.push(resolve));}
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
const clientConfig=JSON.parse(vm.runInContext('mcpClientConfig()',context));
assert.equal(clientConfig.mcpServers.gatehome.url,'http://127.0.0.1:16666/mcp');
assert.equal(clientConfig.mcpServers.gatehome.headers.Authorization,'Bearer YOUR_API_TOKEN');
assert.ok(!vm.runInContext('mcpClientConfig()',context).includes('TEST_ONLY_FRONTEND_TOKEN'));
assert.ok(vm.runInContext('mcpHTML()',context).includes('AI 接入步骤'));
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
vm.runInContext('config={};page="mcp";mcpSettings.data={config:{enabled:false,read:true},token_configured:false,revision:5}',context);
const button={dataset:{action:'mcp-rotate-token'},disabled:false,setAttribute(){},removeAttribute(){}};
context.button=button;
const create=vm.runInContext('mcpSettingsAction(button)',context);
assert.equal(confirmations,0,'creating the first token should not ask to revoke an old token');
assert.equal(calls.at(-1).path,'mcp/rotate-token');
assert.equal(calls.at(-1).method,'POST');
assert.equal(calls.at(-1).payload.revision,5);
assert.equal(calls.at(-1).payload.config,undefined,'creating a token must not silently enable service or grant permissions');
pending.shift()({config:{enabled:false,read:false,write:false,maintenance:false,sunpanel_user_id:0},token_configured:true,revision:6,token:'TEST_ONLY_NEW_TOKEN'});
await new Promise(setImmediate);
pending.shift()({config:{},revision:6});
await create;
assert.equal(button.disabled,false);
assert.equal(vm.runInContext('mcpSettings.data.config.enabled',context),false);
assert.equal(vm.runInContext('mcpSettings.data.config.read',context),true);
assert.equal(vm.runInContext('mcpSettings.data.token',context),'','returned token should only remain in the ephemeral token field');
await vm.runInContext('mcpSettingsAction(button)',context);
assert.equal(confirmations,1,'regenerating an existing token must confirm revocation');
assert.equal(pending.length,0,'a cancelled regeneration must not call the API');
context.copyButton={dataset:{action:'mcp-copy-config'}};
await vm.runInContext('mcpSettingsAction(copyButton)',context);
assert.equal(JSON.parse(copied.at(-1)).mcpServers.gatehome.headers.Authorization,'Bearer YOUR_API_TOKEN');
context.copyButton.dataset.action='mcp-copy-endpoint';
await vm.runInContext('mcpSettingsAction(copyButton)',context);
assert.equal(copied.at(-1),'http://127.0.0.1:16666/mcp');

const olderWrite=vm.runInContext('saveMCPSettings(null,true)',context);
const olderResult=pending.shift();
const newerWrite=vm.runInContext('saveMCPSettings(null,true)',context);
const newerResult=pending.shift();
newerResult({config:{enabled:true,read:true},token_configured:true,revision:8,token:'TEST_ONLY_NEWER_TOKEN'});
await new Promise(setImmediate);
const newerConfig=pending.shift();
olderResult({config:{enabled:true,read:true},token_configured:true,revision:7,token:'TEST_ONLY_OLDER_TOKEN'});
await olderWrite;
assert.equal(vm.runInContext('mcpSettings.data.revision',context),8,'an older write response replaced a newer authorization');
assert.equal(vm.runInContext('mcpSettings.token',context),'TEST_ONLY_NEWER_TOKEN','an older write response restored a revoked token');
newerConfig({config:{},revision:8});
await newerWrite;

vm.runInContext('resetMCPSettings()',context);
const leaving=vm.runInContext('loadMCPSettings()',context);
const beforeNavigation=target.innerHTML;
vm.runInContext('page="settings";resetMCPSettings()',context);
pending.shift()({config:{enabled:true,read:true},token_configured:true,revision:7});
await leaving;
assert.equal(target.innerHTML,beforeNavigation,'a response arriving after navigation must not render settings content');
assert.equal(vm.runInContext('mcpSettings.data',context),null);
console.log('MCP settings: query-only default, explicit API token creation, credential-free client template and navigation/logout guards: PASS');
