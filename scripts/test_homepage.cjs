'use strict';
const assert=require('node:assert/strict');
const {homepageSheets,homepageURL}=require('../internal/gateway/web/homepage.js');
const links=Array.from({length:14},(_,i)=>({id:String(i),favorite:i===9}));
const group={pages:[{id:'daily',rows:2,columns:3,mobile_columns:2,links},{id:'backup',rows:1,columns:4,mobile_columns:2,links:[]}]};
const desktop=homepageSheets(group,false,960),mobile=homepageSheets(group,true,343),narrow=homepageSheets(group,true,280);
assert.equal(desktop.length,4);assert.equal(mobile.length,5);assert.equal(narrow.length,8);
for(const sheets of [desktop,mobile,narrow]){
 assert.equal(sheets[0].links[0].id,'9');assert.equal(new Set(sheets.map(p=>p.id)).size,sheets.length);
 assert.deepEqual(sheets.flatMap(p=>p.links).map(l=>l.id).sort(),links.map(l=>l.id).sort());
 assert(sheets.every(p=>p.links.length<=p.columns*p.page.rows));
}
assert.equal(links[0].id,'0');
const smaller=homepageSheets({pages:[{...group.pages[0],rows:1,columns:1}]},false,960);
assert.equal(smaller.length,14);assert.equal(smaller.flatMap(p=>p.links).length,14);
for(const url of ['javascript:alert(1)','data:text/html,test','https://user:password@example.test','//example.test',''])assert.equal(homepageURL(url),'');
assert.equal(homepageURL('http://192.168.2.10:5000/'),'http://192.168.2.10:5000/');
assert.equal(homepageURL('https://[2001:db8::1]:18443/'),'https://[2001:db8::1]:18443/');
console.log('Homepage pagination, responsive capacity, ordering and URL validation passed');
