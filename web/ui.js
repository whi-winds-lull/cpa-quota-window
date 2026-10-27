(function(root) {
 'use strict';
 function planOf(account) {
  const claim = account.id_token && typeof account.id_token === 'object' ? account.id_token.plan_type : '';
  const raw = String(account.plan_type || claim || '').trim().toLowerCase();
  return ({plus:'Plus',team:'Team / Business',business:'Team / Business',pro:'Pro',free:'Free',enterprise:'Enterprise',ultra:'Ultra'})[raw] || 'unknown';
 }
 function language(win) {
  try { const lang=win.parent.document.documentElement.lang; if (win.parent!==win&&lang) return lang.startsWith('zh')?'zh-CN':'en'; } catch {}
  try {
   const data=JSON.parse(win.localStorage.getItem('cli-proxy-language'));
   const lang=typeof data==='string'?data:data?.state?.language;
   if(lang)return lang.startsWith('zh')?'zh-CN':'en';
  } catch {}
  return (win.navigator.language||'en').startsWith('zh')?'zh-CN':'en';
 }
 function theme(win) {
  // The live host DOM is authoritative, including CPA's light mode without an attribute.
  try {
   if(win.parent&&win.parent!==win){
    const element=win.parent.document.documentElement;
    const mode=element.dataset?.theme??element.getAttribute?.('data-theme');
    if(mode==='dark')return 'dark';
    if(!mode||mode==='light'||mode==='white')return 'light';
   }
  }catch{}
  try {
   const data=JSON.parse(win.localStorage.getItem('cli-proxy-theme'));
   const mode=typeof data==='string'?data:data?.state?.theme;
   if(mode==='dark')return 'dark';
   if(mode==='light'||mode==='white')return 'light';
  }catch{}
  try{return win.matchMedia?.('(prefers-color-scheme: dark)').matches?'dark':'light';}catch{return 'light';}
 }
 function applyAppearance(win) {
  win.document.documentElement.dataset.theme=theme(win);
  win.document.documentElement.lang=language(win);
 }
 function zones(intl=Intl) {
  let values=[];try { values=intl.supportedValuesOf('timeZone'); } catch {}
  return [...new Set(['UTC','Asia/Shanghai',...values])].sort();
 }
 function offset(zone, at=new Date()) {
  try { return new Intl.DateTimeFormat('en',{timeZone:zone,timeZoneName:'shortOffset'}).formatToParts(at).find(p=>p.type==='timeZoneName').value.replace('GMT','UTC'); } catch { return ''; }
 }
 function commonModels(ids, catalog, provider) {
  if (!ids.length) return [];
  const lists=ids.map(id=>catalog.get(id));
  if(lists.some(list=>!Array.isArray(list))) return [];
  const values=lists[0].filter(m=>lists.every(list=>list.some(x=>x.id===m.id)));
  return [...new Map(values.filter(m=>provider!=='antigravity'||/gemini/i.test(m.id)).map(m=>[m.id,m])).values()].sort((a,b)=>a.id.localeCompare(b.id));
 }
 const fields=['schedule_enabled','times','timezone','accounts','model'];
 function plan(config,provider) { return Object.fromEntries(fields.map(k=>[k,config[(provider==='antigravity'?'antigravity_':'')+k]])); }
 function mergePlan(config,provider,value) { const out={...config}; for(const k of fields) out[(provider==='antigravity'?'antigravity_':'')+k]=value[k]; return out; }
 root.CPAWindowUI={planOf,language,theme,applyAppearance,zones,offset,commonModels,plan,mergePlan};
 if(typeof module!=='undefined') module.exports=root.CPAWindowUI;
})(globalThis);
