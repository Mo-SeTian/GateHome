'use strict';
const assert=require('node:assert/strict');
const {homepageSheets,homepageURL,homepageSearch,homepageEngineImage}=require('../internal/gateway/web/homepage.js');
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
const keyword='GateHome 中文 & +?#/ 100%';
for(const [template,key] of [['https://www.baidu.com/s?wd={query}','wd'],['https://www.google.com/search?q={query}','q'],['https://search.example.test/?q={query}&lang=zh','q']]){
 const result=new URL(homepageSearch(template,' '+keyword+' '));
 assert.equal(result.searchParams.get(key),keyword);assert.equal(result.searchParams.size,template.includes('lang=zh')?2:1);
}
assert.equal(new URL(homepageSearch('https://search.example.test/find/{query}','hello/世界')).pathname,'/find/hello%2F%E4%B8%96%E7%95%8C');
assert.equal(homepageSearch('https://www.baidu.com/s?wd={query}','  '),'');
for(const template of ['javascript:alert(1)','https://user:password@example.test/?q={query}','https://example.test/search','https://example.test/?q={query}&another={query}','https://{query}.example.test/','https://example.test/#{query}'])assert.equal(homepageSearch(template,keyword),'');
assert.equal(homepageEngineImage({id:'renamed',url:'https://www.baidu.com/s?wd={query}'}),'/search-baidu.svg');
assert.equal(homepageEngineImage({url:'https://www.google.com/search?q={query}'}),'/search-google.svg');
assert.equal(homepageEngineImage({id:'google',url:'https://www.google.com.evil.test/?q={query}'}),'/search-generic.svg');
assert.equal(homepageEngineImage({url:'https://www.google.com/search?q={query}',image:'a'.repeat(64)}),'/images/'+'a'.repeat(64));
assert.equal(homepageEngineImage({url:'',image:'../../state.json'}),'/search-generic.svg');
console.log('Homepage pagination, responsive capacity, search encoding, icon selection and URL validation passed');
