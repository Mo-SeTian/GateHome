'use strict';
const assert=require('node:assert/strict'),vm=require('node:vm'),fs=require('node:fs');
const source=fs.readFileSync('internal/gateway/web/homepage-editor.js','utf8');
const initial={title:'Own desktop',tone:'forest',shade:50,clock_color:'#000000',background:'stored-background',custom_css:'.gh-name{color:white}',compact:false,show_addresses:false,public:false,widgets:{clock:true,search:true},search_engines:[{id:'one',name:'Search',url:'https://example.test/?q={query}',image:''}],groups:[{id:'group',name:'Group',pages:[{id:'page',name:'Page',rows:3,columns:6,mobile_columns:4,links:[]}]}]};
async function saveCase(kind,e,queries,change){
 const events={},notice={textContent:''},dialog={open:true,querySelector:()=>null,close(){this.open=false;},setAttribute(){},showModal(){this.open=true;}},toggle={focus(){}},doc={homepage:structuredClone(initial),space_id:'admin',revision:1};let saved;
 const context=vm.createContext({document:{addEventListener:(name,fn)=>events[name]=fn,querySelector:s=>s==='#ghe-dialog'?dialog:s==='.gh-notice'?notice:s==='#gh-floating-toggle'?toggle:null},crypto:{randomUUID:()=> 'test-id'},homepageURL:v=>v,matchMedia:()=>({matches:true})});
 vm.runInContext(source,context);
 const editor=context.createHomepageEditor({request:async(path,body)=>{if(!body)return structuredClone(doc);saved=structuredClone(body);return {...doc,homepage:saved.homepage,revision:2};},changed(){},context:()=>({group:initial.groups[0],sheet:{page:initial.groups[0].pages[0]}}),icon:()=>'',esc:String,loggedIn:async()=>{}});
 await editor.toggle();
 const error={textContent:'',focus(){}},form={dataset:{gheForm:kind},elements:e,querySelector:s=>s==='.ghe-error'?error:queries[s],querySelectorAll:s=>s==='button'?[]:queries[s]||[]};
 await events.submit({target:form,preventDefault(){}});
 assert.equal(error.textContent,'');assert(saved,'no update sent');assert.equal(saved.revision,1);
 const expected=structuredClone(initial);change(expected);assert.deepEqual(saved.homepage,expected,kind+' changed unrelated settings');
}
(async()=>{
 await saveCase('clock',{clock:{checked:false},clock_color:{value:'#abcdef'}},{},h=>{h.clock_color='#abcdef';h.widgets.clock=false;});
 await saveCase('appearance',{tone:{value:'midnight'},shade:{value:'30'}},{'[data-image-kind=backgrounds] [data-image]':{value:'new-background'}},h=>{h.tone='midnight';h.shade=30;h.background='new-background';});
 await saveCase('display',{title:{value:'  Renamed  '},compact:{checked:true},show_addresses:{checked:true},public:{checked:true}},{},h=>{h.title='Renamed';h.compact=true;h.show_addresses=true;h.public=true;});
 await saveCase('css',{custom_css:{value:'.gh-art{border-radius:50%}'}},{},h=>{h.custom_css='.gh-art{border-radius:50%}';});
 const row={dataset:{engineId:'new'},querySelector:s=>({value:({'[data-engine-name]':' New ','[data-engine-url]':'https://new.example.test/?q={query}','[data-image]':'icon'})[s]})};
 await saveCase('search',{search:{checked:false}},{'.ghe-engine':[row]},h=>{h.widgets.search=false;h.search_engines=[{id:'new',name:'New',url:'https://new.example.test/?q={query}',image:'icon'}];});
 console.log('Independent homepage settings preserve unrelated content: PASS');
})().catch(e=>{console.error(e);process.exitCode=1;});
