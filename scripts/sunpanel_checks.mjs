import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const source=readFileSync(new URL('../internal/gateway/web/sunpanel-admin.js',import.meta.url),'utf8');
const handlers={},button={},error={};
const links=['internal','external','internal','external'].map(kind=>({dataset:{sunpanelOpen:kind},attributes:{},address:{},note:{},classList:{toggle(){}},
  setAttribute(name,value){this.attributes[name]=value;},removeAttribute(name){delete this.attributes[name];},
  querySelector(selector){return selector==='[data-sunpanel-url]'?this.address:this.note;},
}));
const form={id:'sunpanel-form',elements:{enabled:{checked:true},port:{value:'17777'},launch_url:{value:''},external_url:{value:''}},querySelector:selector=>selector==='.error'?error:button};
let saved,rendered=0;
const context=vm.createContext({URL,structuredClone,form,location:new URL('http://192.168.2.25:16666/#sunpanel'),config:{sunpanel:{enabled:true,port:17777}},status:{sunpanel_storage_supported:true},
  esc:value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])),
  heading:()=>'',panel:(title,description,body)=>body,icon:()=>'',render:()=>{rendered++;},save:async next=>{saved=structuredClone(next);},
  document:{addEventListener:(type,handler)=>{handlers[type]=handler;},querySelector:()=>null,querySelectorAll:()=>links},
});
vm.runInContext(source,context);
assert.equal(vm.runInContext('sunPanelLaunchURL()',context),'http://192.168.2.25:17777/');
assert.equal(vm.runInContext("sunPanelLaunchURL(config.sunpanel,'external')",context),'');
context.location=new URL('https://[fd00::25]:16666/#sunpanel');
assert.equal(vm.runInContext('sunPanelLaunchURL()',context),'http://[fd00::25]:17777/');
context.location=new URL('http://192.168.2.25:16666/#sunpanel');
form.elements.external_url.value='https://panel.example.test/desk/#/';
for(const [value,expected] of [['http://192.168.2.25:16680/','http://192.168.2.25:16680/'],['/sunpanel/#/','http://192.168.2.25:16666/sunpanel/#/'],['','http://192.168.2.25:17777/'],['javascript:alert(1)','http://192.168.2.25:17777/'],['/\\evil.example.test/','http://192.168.2.25:17777/']]) {
  context.config.sunpanel.launch_url=value;
  assert.equal(vm.runInContext('sunPanelLaunchURL()',context),expected);
  form.elements.launch_url.value=value;vm.runInContext('syncSunPanelPreview(form)',context);
  for(const link of links){assert.equal(link.href,link.dataset.sunpanelOpen==='internal'?expected:form.elements.external_url.value);assert.equal(link.address.textContent,link.href);assert.equal(link.attributes['aria-disabled'],'false');}
}
form.elements.launch_url.value='';
for(const value of ['', '/sunpanel/', 'javascript:alert(1)', 'https://user:FAKE_PASSWORD@example.test/', '/\\evil.example.test/']) {
  form.elements.external_url.value=value;vm.runInContext('syncSunPanelPreview(form)',context);
  assert.equal(links[0].href,'http://192.168.2.25:17777/');
  for(const link of [links[1],links[3]]){assert.equal(link.href,'#');assert.equal(link.attributes['aria-disabled'],'true');assert.equal(link.address.textContent,'外网地址未配置');}
}
form.elements.enabled.checked=false;vm.runInContext('syncSunPanelPreview(form)',context);
assert.ok(links.every(link=>link.attributes['aria-disabled']==='true'));
let prevented=false;
handlers.click({target:{closest:selector=>selector==='[data-sunpanel-open][aria-disabled="true"]'?links[1]:null},preventDefault(){prevented=true;}});
assert.equal(prevented,true,'disabled link navigated away from the page');
context.config.sunpanel={enabled:true,port:17777,launch_url:'http://192.168.2.25:16680/',external_url:'https://panel.example.test/desk/#/'};
const markup=vm.runInContext('sunPanelHTML()',context);
for(const kind of ['internal','external'])assert.equal((markup.match(new RegExp(`data-sunpanel-open="${kind}"`,'g'))||[]).length,2);
assert.equal((markup.match(/href="https:\/\/panel.example.test\/desk\/#\/"/g)||[]).length,2);
assert.equal((markup.match(/href="http:\/\/192\.168\.2\.25:16680\/"/g)||[]).length,2);
assert.match(markup,/<iframe[^>]*src="\/sunpanel\/"/,'embedded preview changed to a configured launch URL');
form.elements.enabled.checked=true;
form.elements.launch_url.value='  /sunpanel/#/  ';form.elements.external_url.value='  https://panel.example.test/custom/  ';
await handlers.submit({target:form,preventDefault(){}});
assert.equal(saved.sunpanel.launch_url,'/sunpanel/#/');assert.equal(saved.sunpanel.external_url,'https://panel.example.test/custom/');assert.equal(saved.sunpanel.port,17777);assert.equal(saved.sunpanel.enabled,true);assert.equal(rendered,1);assert.equal(button.disabled,false);
form.elements.launch_url.value='';form.elements.external_url.value='';await handlers.submit({target:form,preventDefault(){}});
assert.equal(saved.sunpanel.launch_url,'');assert.equal(saved.sunpanel.external_url,'');
console.log('Sun-Panel: separate LAN/external links, default host/port, IPv6, URL validation, disabled links, immediate save and reset: PASS');
