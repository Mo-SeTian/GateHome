import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';
const code=readFileSync(new URL('../internal/gateway/web/dashboard.js',import.meta.url),'utf8');
const listeners=new Map(),windowEvents=new Map();
const context=vm.createContext({URL,FormData:class{},structuredClone,clone:structuredClone,config:{groups:[{id:'home',http_port:18080,https_port:18443}],routes:[],dashboard:{widgets:['cpu','map','memory']}},status:{version:'TEST',requests:0},page:'overview',busy:false,icon:()=>'',esc:v=>String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])),date:v=>String(v),compactCharts:{matches:false},document:{addEventListener:(name,fn)=>listeners.set(name,fn),querySelectorAll:()=>[]},window:{addEventListener:(name,fn)=>windowEvents.set(name,fn)},$ :()=>null,toast:()=>{},render:()=>{},prepareTables:()=>{},hydrateIcons:()=>{},console});
vm.runInContext(code,context);
const run=expression=>vm.runInContext(expression,context);
const plain=value=>structuredClone(value);
context.heading=(kicker,title,description,actions)=>actions;context.addRouteButton=()=>'';
context.routeHealthStats=()=>({ok:0,waiting:0,bad:0,total:0,failing:[],options:{enabled:true}});
context.routeTable=()=>'<table></table>';context.groupBadge=()=>'';context.portsText=()=>'';context.eventsHTML=()=>'';
context.trendChart=data=>`<svg aria-label="${data.total}"></svg>`;
assert.ok(run('dashboardHTML()').includes('编辑布局'));
assert.ok(!run('dashboardHTML()').includes('data-widget-handle='),'view mode exposed drag handles');
assert.ok(!run('dashboardHTML()').includes('data-action="configure-dashboard"'),'view mode exposed configuration controls');
for(const [ids,expected] of [
  [[],[]],
  [['cpu'],[2]],
  [['cpu','memory'],[1,1]],
  [['cpu','server','memory'],[2,2,2]],
  [['cpu','memory','requests','storage','blocked'],[1,1,2,2,2]],
  [['server','cpu','memory','storage','traffic','transfer','requests','blocked'],[2,1,1,2,2,2,1,1]]
]) {
  context.mobileIDs=ids;
  assert.deepEqual(plain(run('mobileWidgetSpans(mobileIDs)')),expected,'mobile grid left an unpaired metric beside a full-width widget');
  assert.deepEqual(context.mobileIDs,ids,'responsive layout changed the saved widget order');
}
const mobileHTML=run('dashboardHTML()');
assert.match(mobileHTML,/data-widget="cpu" data-mobile-span="2"/);
assert.match(mobileHTML,/data-widget="memory" data-mobile-span="2"/);
assert.deepEqual([...mobileHTML.matchAll(/data-widget="([^"]+)"/g)].map(match=>match[1]),['cpu','map','memory']);
const savedWidgets=context.config.dashboard.widgets;
context.config.dashboard.widgets=plain(run('widgetCatalog.map(widget=>widget[0])'));
assert.match(run('dashboardHTML()'),/class="workspace-lane"[\s\S]+id="workspace-system"/);
context.compactCharts.matches=true;
assert.match(run('dashboardHTML()'),/id="workspace-summary"[\s\S]+id="workspace-attention"[\s\S]+id="workspace-system"[\s\S]+data-widget="trend"/);
context.compactCharts.matches=false;
context.config.dashboard.widgets=savedWidgets;
run('dashboardView={access:{total:77,blocked:3,trend:[{total:50},...Array.from({length:6},()=>({total:1,time:0}))]}}');
assert.match(run('workspaceSummaryHTML()'),/24 小时访问<\/span><strong>77/);
assert.match(run('workspaceTrendHTML()'),/aria-label="77"/);
run('setDashboardTrend(6)');
assert.match(run('workspaceTrendHTML()'),/aria-label="6"/);
run('resetDashboard()');
context.routeHealthStats=()=>({ok:0,waiting:0,bad:0,total:0,failing:[],options:{enabled:false}});
assert.match(run('workspaceAttentionHTML()'),/data-action="configure-service-health"/,'paused checks have no actionable entry');
context.routeHealthStats=()=>({ok:0,waiting:0,bad:0,total:0,failing:[],options:{enabled:true}});
assert.match(run('workspaceAttentionHTML()'),/href="#routes"/,'attention panel has no service destination');
console.log('Dashboard mobile grid: pairs, orphan metrics, custom order and full-width content PASS');
let viewWrites=0;context.save=async()=>{viewWrites++;};
await run("moveDashboardWidget('cpu',1)");
await run("persistWidgetOrder(['map','cpu','memory'],'map')");
assert.equal(viewWrites,0,'view mode allowed layout mutation');
run('toggleDashboardEditing()');
assert.equal(run('dashboardEditing'),true);
assert.ok(run('dashboardHTML()').includes('data-widget-handle='));
assert.ok(run('dashboardHTML()').includes('data-action="configure-dashboard"'));
assert.ok(run('dashboardHTML()').includes('完成'));
assert.deepEqual(plain(run("reorderedWidgets(['cpu','map','memory'],'cpu','memory',true)")),['map','memory','cpu']);
assert.deepEqual(plain(run("reorderedWidgets(['cpu','map','memory'],'memory','cpu',false)")),['memory','cpu','map']);
assert.deepEqual(plain(run("reorderedWidgets(['cpu','map'],'cpu','cpu')")),['cpu','map']);
assert.deepEqual(plain(run("reorderedWidgets(['cpu','map'],'invalid','cpu')")),['cpu','map']);
context.route={group_id:'home',host:'nas.example.test',upstream:'http://192.168.2.10:5000/',tls:true};
assert.equal(run('publicServiceURL(route)'),'https://nas.example.test:18443/');
context.route.tls=false;assert.equal(run('publicServiceURL(route)'),'http://nas.example.test:18080/');
context.config.groups[0].http_port=0;assert.equal(run('publicServiceURL(route)'),'https://nas.example.test:18443/');
context.config.groups[0].https_port=0;assert.equal(run('publicServiceURL(route)'),'');
for(const value of ['javascript:alert(1)','file:///private/config','data:text/html,unsafe','http://test:TEST_ONLY_SECRET@localhost/','//evil.test']) {
 context.value=value;assert.equal(run('safeServiceURL(value)'),'');
}
context.config.groups[0].http_port=18080;
const links=run('serviceAddressHTML(route)');assert.equal((links.match(/target="_blank"/g)||[]).length,2);assert.equal((links.match(/rel="noopener noreferrer"/g)||[]).length,2);
context.route.upstream='javascript:<script>alert(1)</script>';
assert.ok(!run('serviceAddressHTML(route)').includes('href="javascript:'));
assert.equal(run('bytesLabel(null)'),'—');assert.match(run('bytesLabel(1048576)'),/MiB/);

// A failed save does not mutate the active layout. A successful save applies once.
const original=plain(context.config);
let saves=0;
context.save=async()=>{saves++;throw new Error('TEST_ONLY_FAILURE');};
await run("persistWidgetOrder(['map','cpu','memory'],'map')");assert.deepEqual(context.config,original);assert.equal(saves,1);
context.save=async(next)=>{saves++;context.config=next;};
await run("moveDashboardWidget('cpu',1)");assert.deepEqual(plain(context.config.dashboard.widgets),['map','cpu','memory']);assert.equal(saves,2);
await run("moveDashboardWidget('map',-1)");assert.equal(saves,2,'boundary move sent a write');
run('toggleDashboardEditing()');
assert.equal(run('dashboardEditing'),false);
assert.ok(!run('dashboardHTML()').includes('data-widget-handle='),'done did not restore view mode');

// Competing refreshes and logout must revoke stale data responses.
const resolvers=[];context.api=()=>new Promise(resolve=>resolvers.push(resolve));
const first=run('loadDashboard()'),second=run('loadDashboard()');
resolvers[1]({resources:{},sampled_at:'NEW'});await second;resolvers[0]({resources:{},sampled_at:'OLD'});await first;
assert.equal(run('dashboardView.sampled_at'),'NEW');
const revoked=run('loadDashboard()');run('resetDashboard()');resolvers[2]({resources:{},sampled_at:'REVOKED'});await revoked;
assert.equal(run('dashboardView'),null);
assert.equal(run('dashboardEditing'),false,'logout retained editing mode');

// Polling animates changed values only; unchanged cards and reduced motion stay still.
const numberMotion=[];
let metricValue='0次';
const metricBody={querySelectorAll:()=>[{textContent:metricValue,animate:(frames,options)=>numberMotion.push({frames,options})}],set innerHTML(html){metricValue=html.match(/class="widget-metric">([^<]+)/)[1]+'次';}};
context.document.querySelectorAll=()=>[{dataset:{widget:'requests'},querySelector:()=>metricBody}];
context.reducedMotion={matches:false};context.api=async()=>({resources:{},sampled_at:'METRIC'});
context.status.requests=1;await run('loadDashboard()');assert.equal(metricValue,'1次');assert.equal(numberMotion.length,1);
await run('loadDashboard()');assert.equal(numberMotion.length,1,'unchanged polling replayed number motion');
context.reducedMotion.matches=true;context.status.requests=2;await run('loadDashboard()');assert.equal(metricValue,'2次');assert.equal(numberMotion.length,1,'reduced motion animated a metric update');
context.document.querySelectorAll=()=>[];
console.log('Dashboard metrics: immediate values, changed-only feedback and reduced motion PASS');

// A pointer cancellation removes visual state without committing any data.
let released=false,removed=false;
const handle={hasPointerCapture:()=>true,releasePointerCapture:()=>{released=true;},setAttribute:()=>{}};
const card={classList:{remove:()=>{}},style:{removeProperty:()=>{removed=true;}}};
context.handle=handle;context.card=card;
run("dashboardDrag={handle,card,pointer:1,active:true,target:{id:'map'}};");
listeners.get('pointercancel')({pointerId:1});assert.equal(run('dashboardDrag'),null);assert.ok(released&&removed);assert.equal(saves,2);

context.config.dashboard.map_province='江苏';
context.regions={visits:[{id:1,ip:'<script>test</script>',province:'江苏',region:'<img src=x>',time:'TEST',blocked:true}],provinces:[],unique_ips:1};
const map=run('dashboardMapHTML(regions)');assert.ok(!map.includes('<script>'));assert.ok(!map.includes('<img'));assert.ok(map.includes('&lt;script&gt;'));assert.ok(map.includes('visit-blocked'));assert.ok(map.includes('/china-outline.svg'));
console.log('Dashboard: safe dual links, protocol/port choice, ordering/boundaries, failed save, stale reads/logout, drag cancellation and escaped map metadata PASS');
