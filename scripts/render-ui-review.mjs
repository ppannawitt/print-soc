// Generate offline review pages from the real render functions, without a live native bridge.
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
const output=process.argv[2];if(!output)throw Error('Pass a temporary output directory.');
fs.mkdirSync(output,{recursive:true});
const sources=path.resolve('cmd/socprint-macos/ui');
const context=vm.createContext({setTimeout:()=>0,clearTimeout:()=>{},setInterval:()=>{},requestAnimationFrame:()=>{},window:{addEventListener:()=>{},webkit:{messageHandlers:{socprint:{postMessage:()=>{}}}}},document:{addEventListener:()=>{},querySelectorAll:()=>[],querySelector:()=>null,getElementById:()=>null,hidden:false}});
vm.runInContext(fs.readFileSync(path.join(sources,'model.js'),'utf8'),context);
vm.runInContext(fs.readFileSync(path.join(sources,'app.js'),'utf8'),context);
const printers=JSON.parse(fs.readFileSync('internal/catalog/printers.json','utf8')).map(p=>Object.fromEntries(Object.entries(p).map(([k,v])=>[k[0].toUpperCase()+k.slice(1),v])));
const previewPath=process.argv[3];const preview=previewPath?`data:image/png;base64,${fs.readFileSync(previewPath).toString('base64')}`:'';
const state={termsAccepted:true,printers,printer:printers[0],queue:printers[0].Queues[0],username:'student',documentHandle:'review',fileName:'Sample document.pdf',fileSize:32768,pageCount:1,previewImages:preview?{0:{Image:preview}}:{},jobs:[{ID:'sample',FileName:'Sample document.pdf',PrinterID:'psc008',Queue:'psc008',SubmittedAt:'2026-10-08T01:00:00+08:00',State:'submitted',PrintSettings:'{"Copies":1,"Paper":"A4","Orientation":"portrait","PagesPerSheet":1}'}]};
vm.runInContext('Object.assign(state,'+JSON.stringify(state)+')',context);
const css=fs.readFileSync(path.join(sources,'style.css'),'utf8');
fs.writeFileSync(path.join(output,'light.css'),css.replace('@media(prefers-color-scheme:dark)', '@media not all'));
fs.writeFileSync(path.join(output,'dark.css'),css.replace('@media(prefers-color-scheme:dark)', '@media all'));
fs.copyFileSync(path.join(sources,'logo.svg'),path.join(output,'logo.svg'));
for(const theme of ['light','dark'])for(const page of ['Print','Printers','Jobs','Queues','Help','Account']){
  vm.runInContext('state.page='+JSON.stringify(page),context);
  const html=vm.runInContext('appShell()',context).replaceAll('socprint://app/logo.svg','logo.svg');
  fs.writeFileSync(path.join(output,`${page.toLowerCase()}-${theme}.html`),`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Print @ SoC · ${page} · ${theme} review</title><link rel="stylesheet" href="${theme}.css"><body>${html}</body></html>`);
}
console.log(`Generated 12 offline review pages in ${output}`);
