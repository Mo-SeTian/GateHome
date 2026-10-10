import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { randomFillSync } from 'node:crypto';
import vm from 'node:vm';

const source = readFileSync(new URL('../internal/gateway/web/app.js', import.meta.url), 'utf8');
const helper = source.match(/function newID\(\) \{[\s\S]*?\n\}/)?.[0];
assert.ok(helper, 'shared ID generator is missing');
const context = vm.createContext({ crypto: { getRandomValues: randomFillSync } });
vm.runInContext(helper, context);
const ids = new Set();
for (let i = 0; i < 1000; i++) {
  const id = vm.runInContext('newID()', context);
  assert.match(id, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  ids.add(id);
}
assert.equal(ids.size, 1000);
assert.ok(!source.includes('crypto.randomUUID('), 'a create flow still requires a secure context');
console.log('ID generation without crypto.randomUUID: PASS (all create flows share the generator)');

const domainHelpers=source.slice(source.indexOf('function ddnsPrefixes('),source.indexOf('function updateDomainPreview('));
assert.ok(domainHelpers.includes('function ddnsHostsConfig('),'independent domain editor is missing');
const domains=vm.createContext({structuredClone});
vm.runInContext('const clone=value=>structuredClone(value);'+domainHelpers,domains);
domains.group={zone:'example.com',hosts:['nas.example.com','app.nas.example.com','example.com']};
domains.values=[' NAS\nphotos\n@ ','nas\napp.nas\n@','NAS\nnas.example.com','\n','photos\n@'];
assert.equal(vm.runInContext('ddnsPrefixes(group)',domains),'nas\napp.nas\n@');
assert.deepEqual(Array.from(vm.runInContext('ddnsHosts(group.zone,ddnsPrefixes(group))',domains)),domains.group.hosts);
assert.deepEqual(Array.from(vm.runInContext("ddnsHosts('example.com',values[0])",domains)),['nas.example.com','photos.example.com','example.com']);
assert.deepEqual(Array.from(vm.runInContext("ddnsHosts('new.example',values[1])",domains)),['nas.new.example','app.nas.new.example','new.example']);
assert.deepEqual(Array.from(vm.runInContext("ddnsHosts('example.com','nas.example.com')",domains)),['nas.example.com']);
assert.throws(()=>vm.runInContext("ddnsHosts('example.com',values[2])",domains),/重复/);
assert.throws(()=>vm.runInContext("ddnsHosts('','nas')",domains),/根域名/);
assert.throws(()=>vm.runInContext("ddnsHosts('example.com',values[3])",domains),/1–100/);
domains.tooMany=Array.from({length:101},(_,i)=>'host'+i).join('\n');
assert.throws(()=>vm.runInContext('ddnsHosts(group.zone,tooMany)',domains),/1–100/);
const original={ddns:{groups:[{id:'one',name:'Home',provider:'cloudflare',zone:'example.com',hosts:['nas.example.com'],mode:'dual',enabled:true,interval:600,interface:'ens160',ipv4_source:'url',ipv6_source:'interface',ipv4_url:'https://example.com/ip',ipv6_urls:['https://example.com/ip6']},{id:'two',zone:'other.example',hosts:['nas.other.example']}]},outbound_proxy:{enabled:false,url:'http://127.0.0.1:7890'},routes:[{host:'nas.example.com',group_id:'proxy-one'}],acme:{enabled:false,dns_groups:{'nas.example.com':'one'}}};
domains.original=structuredClone(original);
const edited=vm.runInContext("ddnsHostsConfig(original,'one',values[4])",domains);
const expected=structuredClone(original);expected.ddns.groups[0].hosts=['photos.example.com','example.com'];
assert.deepEqual(structuredClone(edited),expected);
assert.deepEqual(domains.original,original,'editing domains mutated the original configuration');
assert.throws(()=>vm.runInContext("ddnsHostsConfig(original,'missing','nas')",domains),/不存在/);
console.log('DDNS prefixes, completion, duplicates and domains-only changes: PASS');

const proxyHelpers=source.slice(source.indexOf('function proxyHost('),source.indexOf('function updateRouteDomain('));
const proxy=vm.createContext({});vm.runInContext(proxyHelpers,proxy);
proxy.group={domain_suffix:'example.com'};
for(const [input,expected] of [['nas','nas.example.com'],[' NAS ','nas.example.com'],['@','example.com'],['nas.example.com','nas.example.com'],['api.dev','api.dev.example.com']]) {
  proxy.input=input;assert.equal(vm.runInContext('proxyHost(group,input)',proxy),expected);
  proxy.host=expected;assert.equal(vm.runInContext('proxyHost(group,proxyPrefix(group,host))',proxy),expected);
}
assert.equal(vm.runInContext("proxyHost({},'old.other.example')",proxy),'old.other.example');
assert.equal(vm.runInContext("proxyHost(group,'old.other.example',true)",proxy),'old.other.example');
console.log('Proxy suffix completion, root name and legacy full domains: PASS');

const hostValidation=source.match(/function validateRouteHost\([\s\S]*?\n\}/)?.[0];
assert.ok(hostValidation,'missing route domain validation');
const hostAttributes=new Map(),hostField={value:'',setCustomValidity(message){this.validationMessage=message;},setAttribute(key,value){hostAttributes.set(key,value);},removeAttribute(key){hostAttributes.delete(key);}},hostError={textContent:'',hidden:true},hostLabel={textContent:'子域名前缀'};
const routeValidation=vm.createContext({form:{elements:{host:hostField}},$:(selector)=>selector==='#route-host-error'?hostError:hostLabel});
vm.runInContext(hostValidation,routeValidation);
const validateHost=(showError=false)=>vm.runInContext('validateRouteHost(form,'+showError+')',routeValidation);
assert.equal(validateHost(),false);assert.equal(hostError.hidden,true,'initial form must not show an error before interaction');
assert.equal(validateHost(true),false);assert.match(hostError.textContent,/子域名前缀/);assert.equal(hostAttributes.get('aria-invalid'),'true');assert.equal(hostError.hidden,false);
hostField.value='   ';assert.equal(validateHost(true),false,'whitespace must not pass required domain validation');
for(const value of ['nas','@','nas.example.com']) {
  hostField.value=value;assert.equal(validateHost(true),true);assert.equal(hostField.validationMessage,'');assert.equal(hostError.hidden,true);assert.equal(hostAttributes.has('aria-invalid'),false);
}
hostLabel.textContent='访问域名';hostField.value='';assert.equal(validateHost(true),false);assert.match(hostError.textContent,/访问域名/);
validateHost();assert.equal(hostError.hidden,true,'reopening a form must reset the previous error');
console.log('Route domain validation: visible errors, whitespace rejection, prefix/root/full-domain correction and reset: PASS');
const chartHelpers=source.slice(source.indexOf('const statisticsColors='),source.indexOf('function statisticsHTML('));
const charts=vm.createContext({config:{routes:[{group_id:'default',host:'nas.example.com',name:'<script>test</script>'}]},groupName:()=> 'Home',esc:value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])),date:value=>String(value)});
vm.runInContext(chartHelpers,charts);
charts.data={total:2,trend:[{time:'2026-10-08T00:00:00Z',normal:1,blocked:1,failed:0}],statuses:[{label:'2xx',count:1},{label:'4xx',count:1}],rules:[{rule:'default/nas.example.com',total:2,normal:1,blocked:1,failed:0}]};
for(const expression of ['trendChart(data)','statusChart(data)','rulesChart(data)','trendChart(data,true)','statusChart(data,true)','rulesChart(data,true)']) {
  const svg=vm.runInContext(expression,charts);assert.ok(svg.startsWith('<svg'));assert.ok(!/NaN|Infinity|<script>/.test(svg));
}
assert.ok(vm.runInContext('rulesChart(data)',charts).includes('&lt;script&gt;'));
assert.ok(!vm.runInContext('statisticsLegend()',charts).includes('style='),'chart legend violates CSP');
console.log('SVG charts: valid coordinates, escaped labels and CSP-compatible legend: PASS');

const upstream=vm.createContext({});
vm.runInContext(source.slice(source.indexOf('function upstreamParts('),source.indexOf('function openRoute(')),upstream);
for(const [input,scheme,address] of [['http://192.168.1.10:5000','http','192.168.1.10:5000'],['https://nas.lan:443/','https','nas.lan:443'],['https://[2001:db8::1]:5000','https','[2001:db8::1]:5000'],['','http','']]) {
  upstream.input=input;
  assert.deepEqual(structuredClone(vm.runInContext('upstreamParts(input)',upstream)),{scheme,address});
  if(address) {upstream.scheme=scheme;upstream.address=address;assert.equal(vm.runInContext('upstreamURL(scheme,address)',upstream),scheme+'://'+address);}
}
assert.throws(()=>vm.runInContext("upstreamURL('ftp','nas.lan:21')",upstream),/请选择/);
assert.throws(()=>vm.runInContext("upstreamURL('http','https://nas.lan:443')",upstream),/请选择/);
console.log('Upstream scheme selection, legacy URLs, IPv6 and double-prefix rejection: PASS');

const records=vm.createContext({dnsRecords:{home:[{host:'nas.example.test',ipv4:['192.0.2.1'],ipv6:['2001:db8::1'],checked_at:'2026-10-08T00:00:00Z'},{host:'photos.example.test',ipv4:['192.0.2.2'],ipv6:[],checked_at:'2026-10-08T00:00:00Z'}]},dnsRecordsError:'',esc:charts.esc,date:()=> 'test date'});
vm.runInContext(source.slice(source.indexOf('function domainAddressHTML('),source.indexOf('async function loadDNSRecords(')),records);
records.group={id:'home',name:'Home',hosts:['nas.example.test','photos.example.test']};
const table=vm.runInContext('ddnsRecordsTable(group)',records);
const [nas,photos]=table.match(/<tr><td[\s\S]*?<\/tr>/g);
assert.ok(nas.includes('192.0.2.1')&&nas.includes('2001:db8::1')&&!nas.includes('192.0.2.2'));
assert.ok(photos.includes('192.0.2.2')&&!photos.includes('192.0.2.1')&&photos.includes('无记录'));
assert.ok(vm.runInContext("domainAddressHTML({ipv4_error:'DNS 查询失败'},'ipv4')",records).includes('DNS 查询失败'));
assert.ok(vm.runInContext("domainAddressHTML({running:true},'ipv6')",records).includes('查询中'));
console.log('DDNS per-domain columns, missing records, querying and failure states: PASS');

const certificateSettingsSource=source.slice(source.indexOf('function openCertificateSettings('),source.indexOf('function certificatesHTML('));
const certificateSettingsForm={elements:{enabled:{},email:{},staging:{},terms:{}},reset(){this.resets=(this.resets||0)+1;}},certificateSettingsError={},certificateSettingsJob={},certificateSettingsDialog={};
const certificateSettings=vm.createContext({config:{acme:{enabled:false,email:'test@example.test',staging:true,accept_terms:false}},status:{jobs:{}},form:certificateSettingsForm,$:(selector)=>selector==='#cert-form'?certificateSettingsForm:selector==='.error'?certificateSettingsError:selector==='[data-job-summary]'?certificateSettingsJob:certificateSettingsDialog,openDialog:dialog=>{assert.equal(dialog,certificateSettingsDialog);},date:value=>value});
vm.runInContext(certificateSettingsSource,certificateSettings);vm.runInContext('openCertificateSettings()',certificateSettings);
assert.equal(certificateSettingsForm.elements.email.value,'test@example.test');assert.equal(certificateSettingsForm.elements.staging.value,'true');assert.equal(certificateSettingsForm.elements.enabled.checked,false);assert.equal(certificateSettingsForm.elements.terms.checked,false);
const certificatePage=source.slice(source.indexOf('function certificatesHTML('),source.indexOf('function onlineUpdateHTML('));
assert.ok(!certificatePage.includes('<form'),'certificate page still exposes permanent settings as an inline form');
console.log('Certificate settings modal: saved global values restored; task page has no inline settings form: PASS');

const certificateList=vm.createContext({config:{acme:{requests:[{id:'one',domains:['example.test'],enabled:true}],staging:true}},status:{certificates:[{id:'one',not_after:'0001-01-01T00:00:00Z'}]},certificateCredentialsConfigured:{},esc:value=>String(value),date:()=> 'test date',objectMenu:()=> '',badge:()=> '',groupPanel:(kind,id,title,summary,body)=>summary+body});
vm.runInContext(source.slice(source.indexOf('function certificateTable('),source.indexOf('function openCertificate(')),certificateList);
let certificateMarkup=vm.runInContext('certificateTable()',certificateList);
assert.ok(certificateMarkup.includes('尚未签发')&&!certificateMarkup.includes(' · 到期 '),'an unsigned certificate must not show a zero expiry date');
certificateList.status.certificates[0].not_after='2027-01-01T00:00:00Z';
certificateMarkup=vm.runInContext('certificateTable()',certificateList);
assert.ok(certificateMarkup.includes(' · 到期 test date')&&!certificateMarkup.includes('尚未签发'));
console.log('Certificate task summary: unsigned and issued expiry dates: PASS');
