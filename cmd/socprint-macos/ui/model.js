/* Shared deterministic UI rules. No native bridge or browser dependencies. */
(function(root) {
  'use strict';
  function pages(range, count) {
    if (!Number.isInteger(count) || count < 1) throw Error('Choose a readable PDF first.');
    if (!range.trim()) return Array.from({length:count}, (_,i)=>i+1);
    const result=[];
    for (const part of range.split(',')) {
      const m=part.trim().match(/^(\d+)(?:\s*-\s*(\d+))?$/);
      if (!m) throw Error('Enter page numbers like 1–3, 5.');
      const first=Number(m[1]), last=Number(m[2]||m[1]);
      if (!Number.isSafeInteger(first)||!Number.isSafeInteger(last)||first<1||last<first||last>count) throw Error('Choose pages from 1 to '+count+'.');
      if (result.length+last-first+1>100000) throw Error('That selection contains too many pages.');
      for(let i=first;i<=last;i++) result.push(i);
    }
    return result;
  }
  function mode(printer, queue) {
    return {simplex:queue.endsWith('-sx')||(printer.Kind==='colour'&&!queue.endsWith('-dx')),noBanner:queue.includes('-nb')||printer.Banner!=='Yes'};
  }
  function queueFor(printer, simplex, noBanner) {
    return printer?.Queues.find(q=>{const m=mode(printer,q);return m.simplex===simplex&&m.noBanner===noBanner;})||'';
  }
  function options(state) {
    const p=state.printer;
    if(!p||p.Access!=='public'||!p.Queues.includes(state.queue)) throw Error('Choose an available student printer.');
    const copies=Number(state.copies), scale=Number(state.scalePercent);
    if(!/^\d+$/.test(String(state.copies))||copies<1||copies>99) throw Error('Choose a whole number of copies from 1 to 99.');
    if(!['portrait','landscape'].includes(state.orientation)||!['fit','fill','percent'].includes(state.scaleMode)) throw Error('Check orientation and scaling.');
    if(!Number.isFinite(scale)||scale<1||scale>400) throw Error('Choose a scale from 1% to 400%.');
    if(![1,2,4,6,9,16].includes(Number(state.nUp))) throw Error('Choose a supported layout.');
    const range=state.pageMode==='all'?'':state.pageMode==='selection'?state.selectedPages.join(','):state.pageRange.trim();
    if(state.pageMode==='selection'&&!range) throw Error('Select at least one page in the preview.');
    pages(range,state.pageCount);
    return Object.freeze({PageRange:range,Copies:copies,Paper:p.Paper,Orientation:state.orientation,AutoRotate:state.autoRotate,ScaleMode:state.scaleMode,ScalePercent:scale,PagesPerSheet:Number(state.nUp)});
  }
  class Operations {
    constructor(){this.page=0;this.account=0;this.queue=0;this.serial=0;this.active=new Map();}
    capture(kind){return {kind,id:++this.serial,page:this.page,account:this.account,queue:this.queue};}
    current(ctx){return ctx.account===this.account&&ctx.page===this.page&&(ctx.kind!=='queue'||ctx.queue===this.queue);}
    begin(kind){if(this.active.has(kind))return null;const ctx=this.capture(kind);this.active.set(kind,ctx);return ctx;}
    end(ctx){if(this.active.get(ctx.kind)===ctx)this.active.delete(ctx.kind);}
    navigate(){this.page++;}
    changeAccount(){this.account++;this.page++;this.queue++;}
    changeQueue(){this.queue++;}
  }
  root.SPModel={pages,mode,queueFor,options,Operations};
})(typeof globalThis==='undefined'?this:globalThis);
