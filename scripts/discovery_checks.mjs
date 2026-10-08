import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const source=readFileSync(new URL('../internal/gateway/web/app.js',import.meta.url),'utf8');
const context=vm.createContext({structuredClone,TextEncoder,icon:()=>'',badge:value=>value});
vm.runInContext('const clone=value=>structuredClone(value);'+source.match(/^const esc = .*$/m)[0],context);
vm.runInContext(source.match(/^function badge\(.*$/m)[0],context);
vm.runInContext(source.slice(source.indexOf('function proxyHost('),source.indexOf('function updateRouteDomain(')),context);
vm.runInContext(source.slice(source.indexOf('function discoveredRoutesConfig('),source.indexOf('function abandonDiscovery(')),context);
const original={groups:[{id:'home',domain_suffix:'example.test'},{id:'lab'}],routes:[{group_id:'home',host:'nas.example.test',auth:{enabled:true,username:'visitor'}}],ddns:{groups:[{id:'dns',hosts:['nas.example.test']}]},firewalls:[{id:'one'}],other:{preserve:true}};
context.original=structuredClone(original);
context.rows=[{name:'相册',host:' PHOTOS ',upstream:'http://192.168.2.10:8080'},{name:'根入口',host:'@',upstream:'http://192.168.2.10:5000'}];
const next=structuredClone(vm.runInContext("discoveredRoutesConfig(original,'home',rows,true,'one')",context));
assert.deepEqual(context.original,original,'bulk creation mutated the original configuration');
assert.deepEqual(next.routes.slice(0,1),original.routes,'bulk creation changed an existing service');
assert.deepEqual(next.ddns,original.ddns);assert.deepEqual(next.firewalls,original.firewalls);assert.deepEqual(next.other,original.other);
assert.deepEqual(next.routes.slice(1).map(r=>r.host),['photos.example.test','example.test']);
assert.ok(next.routes.slice(1).every(r=>r.tls&&r.enabled&&r.firewall_id==='one'&&!r.auth.enabled));
context.rows=[{name:'IPv6 服务',host:'ipv6.other.test',upstream:'http://[fd00::10]:8080'}];
assert.equal(vm.runInContext("discoveredRoutesConfig(original,'lab',rows,false,'').routes[1].upstream",context),'http://[fd00::10]:8080');
for(const [rows,message] of [
  [[],/至少选择/],
  [[{name:'one',host:'nas',upstream:'http://192.168.2.10:80'}],/重复/],
  [[{name:'one',host:'',upstream:'http://192.168.2.10:80'}],/有效/],
  [[{name:'one',host:'bad_<img',upstream:'http://192.168.2.10:80'}],/有效/],
  [[{name:' ',host:'new',upstream:'http://192.168.2.10:80'}],/服务名称/],
  [[{name:'中文'.repeat(30),host:'new',upstream:'http://192.168.2.10:80'}],/服务名称/],
  [[{name:'one',host:'new',upstream:'https://192.168.2.10:443',tls_untrusted:true}],/证书未受信任/],
  [[{name:'one',host:'new',upstream:'http://192.168.2.10:80'},{name:'two',host:'NEW',upstream:'http://192.168.2.10:81'}],/重复/],
]) {
  context.rows=rows;assert.throws(()=>vm.runInContext("discoveredRoutesConfig(original,'home',rows,true,'')",context),message);
}
context.rows=[{name:'one',host:'new',upstream:'http://192.168.2.10:80'}];
assert.throws(()=>vm.runInContext("discoveredRoutesConfig(original,'missing',rows,true,'')",context),/不存在/);
context.original.routes=Array.from({length:100},(_,i)=>({group_id:'home',host:'existing'+i+'.example.test'}));
assert.throws(()=>vm.runInContext("discoveredRoutesConfig(original,'home',rows,true,'')",context),/最多 100/);
console.log('Discovery: batch creation, suffix/root/IPv6, duplicates, limits and existing configuration preservation: PASS');

for(const payload of ['<script>alert(1)</script>','"><img src=x onerror=alert(1)>','</article><svg onload=alert(1)>']) {
  context.row={port:8080,name:payload,scheme:payload,upstream:payload,status:payload,icon:'https://evil.example/icon.svg'};
  const markup=vm.runInContext('discoveryRowHTML(row)',context);
  assert.ok(!/<(?:script|img|svg)\b/i.test(markup),'discovered metadata became active HTML');
  assert.ok(markup.includes(vm.runInContext('esc(row.name)',context)));
}
context.row={port:8080,name:'NAS',scheme:'http',upstream:'http://192.168.2.10:8080',status:200,icon:'/api/service-discovery/test/icon/8080'};
assert.ok(vm.runInContext('discoveryRowHTML(row)',context).includes('data-discovery-icon'));
context.row.tls_untrusted=true;
assert.match(vm.runInContext('discoveryRowHTML(row)',context),/type="checkbox"[^>]+disabled/);
console.log('Discovery: malicious website metadata escaped, external icons refused and untrusted TLS marked: PASS');

let resolveStart,resolvePoll;
const cancelled=[],statusElement={textContent:'',focus(){}},dialog={open:true},resultElement={innerHTML:'old'};
const ip={value:'127.0.0.1',reportValidity:()=>true},ports={value:'8080',reportValidity:()=>true};
const form={elements:{ip,ports,port_mode:{value:'custom'}}};
const scan={id:'scan-one',state:'running',services:[]};
const state={request:0,timer:null,scan:null,rows:new Map(),pending:false};
const lifecycle=vm.createContext({discovery:state,setTimeout:()=>1,clearTimeout(){},renderDiscoveryProgress(){},receiveDiscovery(s){state.scan=s;},empty:()=>'',
  $:selector=>selector==='#discovery-form'?form:selector==='#discovery-dialog'?dialog:selector==='#discovery-results'?resultElement:statusElement,
  api:(path,method)=>{if(path.endsWith('/cancel')){cancelled.push(path);return Promise.resolve(scan);}return new Promise(resolve=>{if(method==='POST')resolveStart=resolve;else resolvePoll=resolve;});}
});
for(const name of ['abandonDiscovery','startDiscovery','pollDiscovery']) {
  vm.runInContext(source.match(new RegExp('(?:async )?function '+name+'\\([^]*?\\n\\}'))[0],lifecycle);
}
const start=vm.runInContext('startDiscovery()',lifecycle);
vm.runInContext('abandonDiscovery()',lifecycle);dialog.open=false;
resolveStart(scan);await start;
assert.equal(state.scan,null);assert.equal(resultElement.innerHTML,'old');assert.deepEqual(cancelled,['service-discovery/scan-one/cancel']);
dialog.open=true;state.scan=scan;
const poll=vm.runInContext('pollDiscovery(discovery.request)',lifecycle);
vm.runInContext('abandonDiscovery()',lifecycle);dialog.open=false;
resolvePoll({...scan,state:'completed'});await poll;
assert.equal(state.scan,null,'late progress reopened a closed scan');
console.log('Discovery: closing during start cancels the returned job; late progress cannot overwrite a closed/reopened flow: PASS');
