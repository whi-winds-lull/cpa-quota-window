(function(root) {
 'use strict';
 const formatters=new Map();
 function formatter(timezone) {
  const zone=typeof timezone==='string'&&timezone.trim()?timezone.trim():'UTC';
  if(formatters.has(zone))return formatters.get(zone);
  try {
   const value=new Intl.DateTimeFormat('en',{timeZone:zone,calendar:'gregory',numberingSystem:'latn',year:'numeric',month:'2-digit',day:'2-digit'});
   if(formatters.size>=16)formatters.delete(formatters.keys().next().value);
   formatters.set(zone,value);return value;
  }catch{return null;}
 }
 function timestamp(value) {
  if(value===undefined||value===null||value===''||typeof value==='boolean')return NaN;
  try {return new Date(value).getTime();}catch{return NaN;}
 }
 function dateKey(value,timezone='UTC') {
  const at=timestamp(value),format=formatter(timezone);
  if(!Number.isFinite(at)||!format)return '';
  try {
   const parts=Object.fromEntries(format.formatToParts(new Date(at)).map(part=>[part.type,part.value]));
   return `${parts.year.padStart(4,'0')}-${parts.month}-${parts.day}`;
  }catch{return '';}
 }
 function calendarDate(value) {
  if(typeof value!=='string'||!/^\d{4}-\d{2}-\d{2}$/.test(value))return null;
  const [year,month,day]=value.split('-').map(Number);
  if(year<1||month<1||month>12||day<1||day>31)return null;
  const date=new Date(0);date.setUTCFullYear(year,month-1,day);date.setUTCHours(0,0,0,0);
  return date.getUTCFullYear()===year&&date.getUTCMonth()===month-1&&date.getUTCDate()===day?date:null;
 }
 function shiftDate(value,delta) {
  const date=calendarDate(value);
  if(!date||!Number.isFinite(delta))return value;
  date.setUTCDate(date.getUTCDate()+Math.trunc(delta));
  const year=date.getUTCFullYear();
  if(year<1||year>9999)return value;
  return `${String(year).padStart(4,'0')}-${String(date.getUTCMonth()+1).padStart(2,'0')}-${String(date.getUTCDate()).padStart(2,'0')}`;
 }
 function recordKey(row,occurrences) {
  if((typeof row.id==='string'&&row.id)||(typeof row.id==='number'&&Number.isFinite(row.id)))return `id:${row.id}`;
  // Legacy records without IDs can still be read; status updates do not change their key.
  const base=JSON.stringify([row.scheduled_for,row.auth_id||row.label||'',row.started_at||'',row.mode||'']);
  const count=occurrences.get(base)||0;occurrences.set(base,count+1);return `legacy:${base}:${count}`;
 }
 class View {
  constructor({pageSize=10}={}) {
   this.pageSize=Number.isInteger(pageSize)&&pageSize>0?pageSize:10;
   this._now=Date.now();this._timezone='UTC';this._today=dateKey(this._now,'UTC');this._date=this._today;
   this._followToday=true;this._page=1;this._frozen=false;this._order=[];this._day=new Map();this._latest=[];this._stored=0;
  }
  update(status={}) {
   const input=status&&typeof status==='object'?status:{};
   const requested=input.config?.timezone;
   const zone=typeof requested==='string'&&requested.trim()&&formatter(requested)?requested.trim():'UTC';
   const at=timestamp(input.now);if(Number.isFinite(at))this._now=at;
   const today=dateKey(this._now,zone);
   const changed=zone!==this._timezone||(this._followToday&&today!==this._date);
   this._timezone=zone;this._today=today;if(this._followToday)this._date=today;
   if(changed){this._page=1;this._frozen=false;}
   const rows=Array.isArray(input.results)?input.results:[];this._stored=rows.length;
   this._entries=[];const occurrences=new Map();
   rows.forEach((row,index)=>{
    if(!row||typeof row!=='object')return;
    const at=timestamp(row.scheduled_for);if(!Number.isFinite(at))return;
    this._entries.push({key:recordKey(row,occurrences),row,at,index,date:dateKey(at,zone)});
   });
   this._selectDay();return this;
  }
  _selectDay() {
   const entries=(this._entries||[]).filter(entry=>entry.date===this._date).sort((a,b)=>b.at-a.at||b.index-a.index);
   this._day=new Map();this._latest=[];
   for(const entry of entries){if(this._day.has(entry.key))continue;this._day.set(entry.key,entry.row);this._latest.push(entry.key);}
   if(this._frozen)this._order=this._order.filter(key=>this._day.has(key));else this._order=[...this._latest];
   this._page=Math.min(this._page,Math.max(1,Math.ceil(this._order.length/this.pageSize)));
  }
  _latestPage() {this._page=1;this._frozen=false;this._order=[...this._latest];}
  setDate(date) {
   if(!calendarDate(date))return this;
   this._date=date;this._followToday=false;this._page=1;this._frozen=false;this._selectDay();return this;
  }
  moveDay(delta) {return this.setDate(shiftDate(this._date,delta));}
  goToday() {
   this._followToday=true;this._date=this._today;this._page=1;this._frozen=false;this._selectDay();return this;
  }
  setPage(page) {
   const requested=Number(page);if(!Number.isFinite(requested))return this;
   const next=Math.max(1,Math.min(Math.trunc(requested),Math.max(1,Math.ceil(this._order.length/this.pageSize))));
   if(next===1)this._latestPage();else{this._page=next;this._frozen=true;}
   return this;
  }
  snapshot() {
   const total=this._order.length,start=(this._page-1)*this.pageSize;
   return {
    rows:this._order.slice(start,start+this.pageSize).map(key=>this._day.get(key)),
    date:this._date,today:this._today,timezone:this._timezone,page:this._page,pages:Math.max(1,Math.ceil(total/this.pageSize)),
    total,totalStored:this._stored,newCount:this._latest.reduce((count,key)=>count+(this._order.includes(key)?0:1),0),
    start:total?start+1:0,end:Math.min(start+this.pageSize,total)
   };
  }
 }
 root.CPAHistory={View,dateKey};
 if(typeof module!=='undefined')module.exports=root.CPAHistory;
})(globalThis);
