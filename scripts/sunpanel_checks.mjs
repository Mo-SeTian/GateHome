import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const source=readFileSync(new URL('../internal/gateway/web/sunpanel-admin.js',import.meta.url),'utf8');
const handlers={},links=[{},{}],address={},field={value:''},button={},error={};
const form={id:'sunpanel-form',elements:{enabled:{checked:true},port:{value:'17777'},launch_url:field},querySelector:selector=>selector==='.error'?error:button};
let saved,rendered=0;
const context=vm.createContext({URL,structuredClone,location:new URL('http://192.168.2.25:16666/#sunpanel'),config:{sunpanel:{enabled:true,port:17777}},status:{sunpanel_storage_supported:true},
  esc:value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])),
  heading:()=>'',panel:(title,description,body)=>body,icon:()=>'',render:()=>{rendered++;},save:async next=>{saved=structuredClone(next);},
  document:{addEventListener:(type,handler)=>{handlers[type]=handler;},querySelector:selector=>selector==='[data-sunpanel-url]'?address:null,querySelectorAll:()=>links},
});
for(const link of links){link.setAttribute=()=>{};link.removeAttribute=()=>{};}
vm.runInContext(source,context);
assert.equal(vm.runInContext('sunPanelLaunchURL()',context),'http://192.168.2.25:17777/');
context.location=new URL('https://[fd00::25]:16666/#sunpanel');
assert.equal(vm.runInContext('sunPanelLaunchURL()',context),'http://[fd00::25]:17777/');
context.location=new URL('http://192.168.2.25:16666/#sunpanel');
for(const [value,expected] of [['https://panel.example.test/desk/#/','https://panel.example.test/desk/#/'],['/sunpanel/#/','http://192.168.2.25:16666/sunpanel/#/'],['','http://192.168.2.25:17777/'],['javascript:alert(1)','http://192.168.2.25:17777/'],['/\\evil.example.test/','http://192.168.2.25:17777/']]) {
  context.config.sunpanel.launch_url=value;
  assert.equal(vm.runInContext('sunPanelLaunchURL()',context),expected);
  field.value=value;context.form=form;vm.runInContext('syncSunPanelPreview(form)',context);
  assert.equal(links[0].href,expected);assert.equal(links[1].href,expected);assert.equal(address.textContent,expected);
}
context.config.sunpanel.launch_url='https://panel.example.test/desk/#/';
const markup=vm.runInContext('sunPanelHTML()',context);
assert.equal((markup.match(/href="https:\/\/panel.example.test\/desk\/#\/"/g)||[]).length,2);
assert.match(markup,/<iframe[^>]*src="\/sunpanel\/"/,'embedded preview changed to the custom external URL');
field.value='  https://panel.example.test/custom/  ';
await handlers.submit({target:form,preventDefault(){}});
assert.equal(saved.sunpanel.launch_url,'https://panel.example.test/custom/');assert.equal(saved.sunpanel.port,17777);assert.equal(saved.sunpanel.enabled,true);assert.equal(rendered,1);assert.equal(button.disabled,false);
field.value='';await handlers.submit({target:form,preventDefault(){}});assert.equal(saved.sunpanel.launch_url,'');
console.log('Sun-Panel: LAN host/port default, IPv6, custom and local paths, both launch links, immediate save and reset: PASS');
