import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const source=readFileSync(new URL('../internal/gateway/web/app.js',import.meta.url),'utf8');
const context=vm.createContext({
  icon:()=>'',date:()=> 'test date',groupName:()=> 'Test group',
  config:{routes:[],firewalls:[]},status:{events:[]},
  logScope:'project',logCategory:'',logResult:'',logRule:'',logMethod:'',logStatus:'',logSearch:'',logIP:'',logFrom:'',logTo:'',logPage:1,logSize:50,
  logView:{entries:[],pages:1,total:1}
});
vm.runInContext(source.match(/^const esc = .*$/m)[0],context);
vm.runInContext(source.slice(source.indexOf('function badge('),source.indexOf('function actionLabel(')),context);
vm.runInContext(source.slice(source.indexOf('function eventsHTML('),source.indexOf('function groupName(')),context);
vm.runInContext(source.slice(source.indexOf('function logCategoryName('),source.indexOf('async function loadLogs(')),context);
vm.runInContext(source.slice(source.indexOf('function securityEngineName('),source.indexOf('const statisticsColors=')),context);
vm.runInContext(source.match(/^const securityEvents=.*$/m)[0],context);
const payloads=[
  '<script>alert(1)</script>',
  '"><img src=x onerror=alert(1)>',
  '</td><svg onload=alert(1)>',
  '&lt;img src=x onerror=alert(1)&gt;',
  "'><iframe srcdoc='<script>alert(1)</script>'>"
];
for(const payload of payloads) {
  context.payload=payload;
  context.logView.entries=[{id:1,time:'2026-10-08T00:00:00Z',category:payload,action:payload,target:payload,path:payload,method:payload,remote:payload,message:payload,status:404,duration_ms:1,ok:false}];
  context.status.events=[{kind:'ddns',message:payload,time:'2026-10-08T00:00:00Z',ok:false}];
  context.logView.entries[0].remote_region=payload;context.logScope='access';
  for(const expression of ['logsHTML()','eventsHTML()']) {
    const html=vm.runInContext(expression,context);
    assert.ok(!/<(?:script|img|svg|iframe)\b/i.test(html),'untrusted content became an HTML element');
    assert.ok(html.includes(vm.runInContext('esc(payload)',context)),'untrusted content was not escaped');
  }
}
console.log('Security: malicious log/event strings remain escaped text (10 rendering cases): PASS');

context.ipBlockView={entries:[],now:new Date().toISOString()};context.ipBlockReceived=Date.now();context.ipBlockRule='';context.ipBlockSearch='';context.ipBlockPage=1;context.ipBlockError='';
context.statisticsRuleName=key=>key;context.Date=Date;
vm.runInContext(source.slice(source.indexOf('function freezeNow('),source.indexOf('async function loadIPBlocks(')),context);

for(const payload of payloads) {
  context.payload=payload;
  context.ipBlockView.entries=[{ip:payload,rule:payload,rule_name:payload,reason:payload,source:'automatic',failures:5,started_at:new Date().toISOString(),until:new Date(Date.now()+3600000).toISOString()}];
  context.securityData={firewalls:[{name:payload,id:payload,reason:payload,order:1,match:'exclude',count:1,last_seen:new Date().toISOString()}],security_rules:[{engine:payload,rule_id:941100,name:payload,count:1,blocked:1,last_seen:new Date().toISOString()}],security_records:[{security:[{engine:payload,rule_id:941100,name:payload,action:'detect',score:5,severity:payload}],time:new Date().toISOString(),remote:payload,rule:payload,method:payload,path:payload,message:payload,status:403,outcome:'blocked',firewall:{reason:payload}}]};
  context.config.firewalls=[{id:payload,name:payload}];
  context.securityData.security_records[0].remote_region=payload;
  context.securityData.security_records[0].id=1;
  vm.runInContext('securityEvents.security.view={entries:securityData.security_records,page:1,pages:1,total:1};securityEvents.firewall.view=securityEvents.security.view;securityEvents.firewall.filters.ip=payload;',context);
  for(const expression of ['ipBlocksHTML()','securityStatisticsHTML(securityData)',"securityEventsHTML('firewall')"]) {
    const html=vm.runInContext(expression,context);
    assert.ok(!/<(?:script|img|iframe)\b/i.test(html),'IP/security metadata became an HTML element');
    assert.ok(!/<svg[^>]+onload=/i.test(html),'malicious SVG became active markup');
    assert.ok(html.includes(vm.runInContext('esc(payload)',context)),'IP/security metadata was not escaped');
  }
}
console.log('Security: freeze reasons, IP attributes and firewall records remain escaped (15 cases, including region labels and filter values): PASS');
