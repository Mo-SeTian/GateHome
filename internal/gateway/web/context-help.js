'use strict';
let activeContextHelp=null,contextHelpTimer;
function closeContextHelp() {
  clearTimeout(contextHelpTimer);
  if(activeContextHelp) {activeContextHelp.open=false;delete activeContextHelp.dataset.pinned;activeContextHelp=null;}
}
function showContextHelp(help) {
  clearTimeout(contextHelpTimer);
  if(activeContextHelp!==help) closeContextHelp();
  activeContextHelp=help;help.open=true;
  const popup=help.querySelector('.help-popup'),anchor=help.querySelector('summary').getBoundingClientRect();
  const width=popup.getBoundingClientRect().width,margin=12;
  popup.style.left=Math.max(margin,Math.min(anchor.left,document.documentElement.clientWidth-width-margin))+'px';
  const height=popup.getBoundingClientRect().height;
  popup.style.top=(anchor.bottom+8+height<=window.innerHeight-margin?anchor.bottom+8:Math.max(margin,anchor.top-height-8))+'px';
}
function contextHelpTitle(note,scope) {
  const label=note.closest('label');
  if(label) {
    let caption=label.querySelector('.field-caption')||label.querySelector(':scope>span:not(.sr-only)');
    if(!caption) {
      caption=document.createElement('span');caption.className='field-caption';
      for(const child of [...label.childNodes]) if(child.nodeType===Node.TEXT_NODE&&child.textContent.trim()) caption.append(child);
      label.prepend(caption);
    }
    const title=caption.querySelector('b')||caption;
    if(title===caption) caption.classList.add('field-caption');
    const input=label.querySelector('input,select,textarea');
    if(input&&!input.hasAttribute('aria-label')&&!input.hasAttribute('aria-labelledby')) input.setAttribute('aria-label',title.textContent.trim());
    return title;
  }
  const toggle=note.closest('.toggle-row');
  if(toggle) return toggle.querySelector('b');
  const item=note.closest('.sunpanel-guide li,.mcp-steps li,.editor-upstream');
  if(item) return item.querySelector('b');
  const head=note.closest('.page-heading,.panel-heading,.dialog-heading');
  if(head) return head.querySelector('h1,h2,h3');
  const owner=note.closest('.login-card,form,section,.panel,details');
  return owner?.querySelector('h3,h2')||note.closest('.panel')?.querySelector('h2')||scope.querySelector('.page-heading h1');
}
function prepareContextHelp(scope=document) {
  for(const heading of scope.querySelectorAll('.help-heading')) heading.querySelector('summary')?.setAttribute('aria-label','查看'+heading.querySelector('h1,h2,h3').textContent.trim()+'的说明');
  const selectors='.page-heading>div>p,.panel-heading>div:not(.group-heading)>p,.dialog-heading>div>p:not(.mono):not([data-editor-domain]),.toggle-row>div>p,label>small:not([id]),.mcp-permissions small,.sunpanel-guide small,.sunpanel-identity p,.mcp-steps p,.login-card>.muted,.login-note,p.form-note,.hint:not(.amber),.context-note';
  for(const note of scope.querySelectorAll(selectors)) {
    if(note.hasAttribute('data-visible-note')||note.closest('.context-help,.empty')||!note.textContent.trim()) continue;
    const title=contextHelpTitle(note,scope);
    if(!title) continue;
    let help=title.matches('.field-caption')?title.querySelector(':scope>.context-help'):title.nextElementSibling;
    if(!help?.matches('.context-help')) {
      help=document.createElement('details');help.className='context-help';
      const summary=document.createElement('summary');summary.setAttribute('aria-label','查看'+title.textContent.trim()+'的说明');summary.innerHTML=icon('info');
      const popup=document.createElement('div');popup.className='help-popup';popup.setAttribute('role','note');
      help.append(summary,popup);
      if(title.matches('h1,h2,h3')) {
        let wrapper=title.closest('.help-heading');
        if(!wrapper) {wrapper=document.createElement('div');wrapper.className='help-heading';title.before(wrapper);wrapper.append(title);}
        wrapper.append(help);
      } else if(title.matches('.field-caption')) title.append(help);
      else title.after(help);
      help.addEventListener('pointerenter',()=>showContextHelp(help));
      help.addEventListener('pointerleave',()=>{contextHelpTimer=setTimeout(()=>{if(!help.dataset.pinned&&!help.contains(document.activeElement)) closeContextHelp();},120);});
      help.addEventListener('focusin',()=>showContextHelp(help));
      help.addEventListener('focusout',event=>{if(!help.contains(event.relatedTarget)) closeContextHelp();});
      summary.addEventListener('click',event=>{event.preventDefault();if(help.dataset.pinned) closeContextHelp();else {showContextHelp(help);help.dataset.pinned='true';}});
    }
    const disclosure=note.closest('.field-help');
    if(note.matches('.widget-note.context-note')) help.querySelector('.widget-note.context-note')?.remove();
    help.querySelector('.help-popup').append(note);
    if(disclosure&&!disclosure.querySelector('p')) disclosure.remove();
  }
}
document.addEventListener('click',event=>{if(activeContextHelp&&!activeContextHelp.contains(event.target)) closeContextHelp();});
document.addEventListener('keydown',event=>{
  if(event.key==='Escape'&&activeContextHelp) {closeContextHelp();event.preventDefault();event.stopPropagation();}
},true);
document.addEventListener('scroll',event=>{if(!event.target.closest?.('.help-popup')) closeContextHelp();},true);
window.addEventListener('resize',closeContextHelp);
