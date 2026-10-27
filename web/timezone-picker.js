(function(root) {
 'use strict';
 let nextID=0;
 const copy={
  'zh-CN':{search:'搜索时区，例如 Tokyo、New York',label:'搜索时区',list:'可选时区',empty:'没有匹配的时区',count:n=>`${n} 个时区`,selected:'当前选择'},
  en:{search:'Search time zones, e.g. Tokyo, New York',label:'Search time zones',list:'Available time zones',empty:'No matching time zones',count:n=>`${n} time zones`,selected:'Selected'}
 };
 const normalize=value=>String(value).toLowerCase().replace(/_/g,' ').trim();
 function mount({input,zones=[],language='zh-CN',onChange}={}) {
  if(!input?.ownerDocument)throw new TypeError('A timezone input is required');
  const doc=input.ownerDocument,win=doc.defaultView||root,id=`cpa-timezone-${++nextID}`;
  let source=zones,locale=language,opened=false,destroyed=false,active=-1,filtered=[],options=[];
  const saved={readOnly:input.readOnly,attributes:{}};
  for(const name of ['role','aria-haspopup','aria-controls','aria-expanded','aria-activedescendant','aria-autocomplete','list'])saved.attributes[name]=input.getAttribute(name);
  const make=(tag,cls)=>{const el=doc.createElement(tag);el.className=cls;return el;};
  const popup=make('div','cpaTimezonePopup'),search=make('input','cpaTimezoneSearch'),list=make('div','cpaTimezoneList'),status=make('div','cpaTimezoneStatus');
  popup.hidden=true;search.type='search';search.autocomplete='off';search.spellcheck=false;list.id=id;list.setAttribute('role','listbox');status.setAttribute('role','status');status.setAttribute('aria-live','polite');
  search.setAttribute('role','combobox');search.setAttribute('aria-controls',id);search.setAttribute('aria-autocomplete','list');search.setAttribute('aria-expanded','false');
  popup.append(search,list,status);doc.body.append(popup);
  input.readOnly=true;input.removeAttribute('list');input.classList.add('cpaTimezoneTrigger');input.setAttribute('role','combobox');input.setAttribute('aria-haspopup','listbox');input.setAttribute('aria-controls',id);input.setAttribute('aria-expanded','false');input.setAttribute('aria-autocomplete','none');
  const listeners=[];
  const listen=(target,type,handler,options)=>{target.addEventListener(type,handler,options);listeners.push(()=>target.removeEventListener(type,handler,options));};
  const strings=()=>copy[String(typeof locale==='function'?locale():locale).startsWith('zh')?'zh-CN':'en'];
  const allZones=()=>[...new Set([...(Array.isArray(source)?source:[]),input.value].filter(value=>typeof value==='string'&&value.trim()).map(value=>value.trim()))].sort((a,b)=>a.localeCompare(b));
  function position(){
   if(!opened)return;
   const rect=input.getBoundingClientRect(),margin=12,gap=6,width=Math.max(0,Math.min(Math.max(rect.width,300),win.innerWidth-margin*2));
   const below=win.innerHeight-rect.bottom-margin-gap,above=rect.top-margin-gap,useAbove=below<220&&above>below;
   const height=Math.max(100,Math.min(352,useAbove?above:below));
   popup.style.width=`${width}px`;popup.style.left=`${Math.max(margin,Math.min(rect.left,win.innerWidth-width-margin))}px`;
   popup.style.setProperty('--cpa-timezone-height',`${height}px`);
   popup.style.top=useAbove?'auto':`${rect.bottom+gap}px`;popup.style.bottom=useAbove?`${win.innerHeight-rect.top+gap}px`:'auto';
  }
  function highlight(index,scroll=false){
   active=filtered.length?Math.max(0,Math.min(index,filtered.length-1)):-1;
   options.forEach((option,i)=>option.classList.toggle('is-active',i===active));
   for(const control of [input,search]){
    if(opened&&active>=0)control.setAttribute('aria-activedescendant',options[active].id);else control.removeAttribute('aria-activedescendant');
   }
   if(scroll&&active>=0)options[active].scrollIntoView?.({block:'nearest'});
  }
  function render(){
   const text=strings(),query=normalize(search.value),tokens=query.split(/\s+/).filter(Boolean);
   search.placeholder=text.search;search.setAttribute('aria-label',text.label);list.setAttribute('aria-label',text.list);
   filtered=allZones().filter(zone=>tokens.every(token=>normalize(zone).includes(token)));options=[];list.replaceChildren();
   filtered.forEach((zone,index)=>{
    const option=make('div','cpaTimezoneOption'),name=make('span','cpaTimezoneName'),mark=make('span','cpaTimezoneSelected');
    option.id=`${id}-${index}`;option.setAttribute('role','option');option.setAttribute('aria-selected',String(zone===input.value));
    name.textContent=zone;mark.textContent=zone===input.value?'✓':'';mark.setAttribute('aria-hidden','true');if(zone===input.value)option.title=text.selected;
    option.append(name,mark);option.addEventListener('pointerdown',event=>event.preventDefault());option.addEventListener('click',()=>choose(index));list.append(option);options.push(option);
   });
   status.textContent=filtered.length?text.count(filtered.length):text.empty;
   highlight(Math.max(0,filtered.indexOf(input.value)),false);
  }
  function close(restoreFocus=false){
   if(!opened)return;opened=false;popup.hidden=true;input.setAttribute('aria-expanded','false');search.setAttribute('aria-expanded','false');input.removeAttribute('aria-activedescendant');search.removeAttribute('aria-activedescendant');
   if(restoreFocus)input.focus();
  }
  function open(){
   if(destroyed||input.disabled||opened)return;
   opened=true;search.value='';popup.hidden=false;input.setAttribute('aria-expanded','true');search.setAttribute('aria-expanded','true');render();position();search.focus();highlight(active,true);
  }
  function choose(index){
   const value=filtered[index];if(value===undefined)return;const changed=input.value!==value;input.value=value;close(true);
   if(changed){input.dispatchEvent(new win.Event('input',{bubbles:true}));if(typeof onChange==='function')onChange(value);}
  }
  function keys(event){
   if(event.key==='Escape'&&opened){event.preventDefault();event.stopPropagation();close(true);return;}
   if(event.key==='Tab'){close(true);return;}
   if(['ArrowDown','ArrowUp'].includes(event.key)){
    event.preventDefault();if(!opened){open();return;}highlight(active+(event.key==='ArrowDown'?1:-1),true);return;
   }
   if(event.key==='Enter'||(event.key===' '&&event.target===input)){
    event.preventDefault();if(!opened)open();else choose(active);return;
   }
   if(opened&&event.target===search&&(event.key==='Home'||event.key==='End')&&event.altKey){event.preventDefault();highlight(event.key==='Home'?0:filtered.length-1,true);}
  }
  listen(input,'click',()=>opened?close():open());listen(input,'keydown',keys);listen(search,'keydown',keys);listen(search,'input',render);
  listen(doc,'pointerdown',event=>{if(opened&&event.target!==input&&!popup.contains(event.target))close();});
  listen(doc,'focusin',event=>{if(opened&&event.target!==input&&!popup.contains(event.target))close();});
  listen(win,'resize',position);listen(win,'scroll',event=>{if(event.target===win||event.target===doc||!popup.contains(event.target))position();},true);
  function refresh(next={}){
   if(destroyed)return;if(next.zones!==undefined)source=next.zones;if(next.language!==undefined)locale=next.language;
   if(opened&&input.disabled)close();render();position();
  }
  function destroy(){
   if(destroyed)return;close();destroyed=true;listeners.forEach(remove=>remove());popup.remove();input.readOnly=saved.readOnly;input.classList.remove('cpaTimezoneTrigger');
   for(const [name,value] of Object.entries(saved.attributes)){if(value===null)input.removeAttribute(name);else input.setAttribute(name,value);}
  }
  refresh();return {open,close,refresh,destroy};
 }
 root.CPATimezonePicker={mount};
 if(typeof module!=='undefined')module.exports=root.CPATimezonePicker;
})(globalThis);
