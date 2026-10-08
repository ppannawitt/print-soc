import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import '../cmd/socprint-macos/ui/model.js';
const M=globalThis.SPModel;
const printer={ID:'psc008',Location:'COM1, Basement',Access:'public',Paper:'A4',Kind:'black and white',Banner:'Yes',Queues:['psc008','psc008-sx','psc008-nb']};
const basic={printer,queue:'psc008',copies:'1',scalePercent:'100',orientation:'portrait',scaleMode:'fit',autoRotate:true,nUp:1,pageMode:'all',pageCount:7,pageRange:'',selectedPages:[]};
test('ranges preserve explicit page order, reject invalid input and empty selection',()=>{
  assert.deepEqual(M.pages('1-3, 5, 2',7),[1,2,3,5,2]);
  for(const r of ['0','8','4-2','1,,3','abc','9007199254740992'])assert.throws(()=>M.pages(r,7));
  assert.throws(()=>M.options({...basic,pageMode:'selection'}));
  assert.equal(M.options({...basic,pageMode:'selection',selectedPages:[2,6]}).PageRange,'2,6');
});
test('printer modes do not silently substitute unsupported combinations',()=>{
  assert.equal(M.queueFor(printer,true,true),'');
  assert.equal(M.queueFor(printer,false,true),'psc008-nb');
  const colour={...printer,Kind:'colour',Banner:'No',Queues:['colour','colour-dx']};
  assert.equal(M.queueFor(colour,false,true),'colour-dx');
});
test('print options validate copies, scale and layout and are immutable',()=>{
  for(const copies of ['0','100','1.5','1e1'])assert.throws(()=>M.options({...basic,copies}));
  assert.throws(()=>M.options({...basic,nUp:3}));
  assert.throws(()=>M.options({...basic,scalePercent:'Infinity'}));
  assert.ok(Object.isFrozen(M.options(basic)));
});
function harness(){
  const messages=[],timers=new Map(),listeners=new Map();let timerID=0;
  const root={innerHTML:''};
  const context=vm.createContext({console,SPModel:M,setTimeout:(fn)=>{timers.set(++timerID,fn);return timerID;},clearTimeout:id=>timers.delete(id),setInterval:()=>0,requestAnimationFrame:()=>{},document:{hidden:false,activeElement:null,addEventListener:(event,fn)=>listeners.set(event,fn),querySelectorAll:()=>[],querySelector:()=>null,getElementById:id=>id==='root'?root:null},window:{addEventListener:()=>{},webkit:{messageHandlers:{socprint:{postMessage:message=>messages.push(message)}}}},confirm:()=>true});
  vm.runInContext(fs.readFileSync(new URL('../cmd/socprint-macos/ui/app.js',import.meta.url),'utf8'),context);
  vm.runInContext('render=()=>{}; state.termsAccepted=true; state.printers='+JSON.stringify([printer])+'; state.printer=state.printers[0];state.username="student";state.formUsername="student";',context);
  messages.length=0;
  function reply(message,ok=true,data={}){context.window.socprintReceive({ID:message.ID,OK:ok,Data:data,Code:ok?'':'noKey',Error:ok?'':'SSH Jump needs a registered key.'});}
  return {context,messages,reply,timers,input:target=>listeners.get("input")({target}),click:action=>listeners.get("click")({target:{closest:()=>({dataset:{action}})}}),run:code=>vm.runInContext(code,context)};
}
const tick=()=>new Promise(resolve=>setImmediate(resolve));
test('delayed sign-in failure cannot redirect to Queues or show another alert',async()=>{
  const h=harness();h.run('navigate("Queues");state.queueName="psc008";');
  const done=h.run('refreshQueue()');assert.equal(h.messages[0].Action,'signIn');
  h.run('navigate("Account")');h.reply(h.messages[0],false);await done;
  assert.equal(h.run('state.page'),'Account');assert.equal(h.messages.filter(m=>m.Action==='nativeAlert').length,0);
});
test('queue polling never overlaps, alerts once and stops after failure until explicit retry',async()=>{
  const h=harness();h.run('navigate("Queues");state.queueName="psc008";state.connected=true;');
  const first=h.run('refreshQueue(false)');await h.run('refreshQueue(false)');assert.equal(h.messages.length,1);
  h.reply(h.messages[0],false);await first;await tick();
  const alert=h.messages.find(m=>m.Action==='nativeAlert');assert.ok(alert);h.reply(alert,true,{ButtonIndex:-1});await tick();
  await h.run('refreshQueue(false)');assert.equal(h.messages.filter(m=>m.Action==='queue').length,1);assert.equal(h.messages.filter(m=>m.Action==='nativeAlert').length,1);
  const retry=h.run('refreshQueue(true)');const login=h.messages.find(m=>m.Action==='signIn');assert.ok(login);
  h.reply(login,true,{SignedIn:true,Username:'student',Route:'Direct SSH'});await tick();
  h.reply(h.messages.filter(m=>m.Action==='queue').at(-1),true,{Queue:'psc008',Jobs:[],KnownEmpty:true});await retry;assert.equal(h.run('state.connected'),true);
});
test('changing queues ignores old data and retains the current selection',async()=>{
  const h=harness();h.run('navigate("Queues");state.queueName="psc008";state.connected=true;');
  const first=h.run('refreshQueue()');h.run('operations.changeQueue();state.queueName="psc008-sx";state.queueData=null;');
  h.reply(h.messages[0],true,{Queue:'psc008',Jobs:[{ID:'wrong'}]});await first;assert.equal(h.run('state.queueData'),null);assert.equal(h.messages[1].Queue,'psc008-sx');h.reply(h.messages[1],true,{Queue:'psc008-sx',Jobs:[],KnownEmpty:true});await tick();assert.equal(h.run('state.queueData.Queue'),'psc008-sx');
});
test('trust cancellation never submits or navigates',async()=>{
  const h=harness();h.run('navigate("Print")');const done=h.run('connectAccount(null,operations.capture("print"))');
  h.reply(h.messages[0],true,{NeedsTrust:true,Host:'example',Fingerprint:'SHA256:test'});await tick();
  assert.equal(h.messages[1].Action,'nativeAlert');h.reply(h.messages[1],true,{ButtonIndex:0});await tick();
  assert.equal(h.messages[2].Action,'cancelTrust');h.reply(h.messages[2]);await done;assert.equal(h.run('state.page'),'Print');assert.equal(h.run('state.connected'),false);
});
test('an account change invalidates pending connection results',async()=>{
  const h=harness();const done=h.run('connectAccount()');h.run('operations.changeAccount();state.username="another";');h.reply(h.messages[0],true,{SignedIn:true,Username:'student'});await done;assert.equal(h.run('state.connected'),false);assert.equal(h.run('state.username'),'another');
});
test('a print snapshot remains immutable while preparation is pending',async()=>{
  const h=harness();h.run('Object.assign(state,'+JSON.stringify({...basic,documentHandle:'source',fileName:'notes.pdf',connected:true})+');');
  const done=h.run('connectThenPrint()');assert.equal(h.messages[0].Action,'preparePDF');
  h.run('state.copies="9";state.queue="psc008-sx";');h.reply(h.messages[0],true,{PreparedHandle:'prepared'});await tick();
  assert.deepEqual(JSON.parse(JSON.stringify(h.messages[1])),{ID:h.messages[1].ID,Action:'submitPrint',PreparedHandle:'prepared'});
  h.reply(h.messages[1],true,{State:'submitted',OperationID:'123',Message:'Accepted'});await tick();h.reply(h.messages[2]);await done;
  assert.equal(h.run('JSON.parse(state.jobs[0].PrintSettings).Copies'),1);assert.equal(h.run('state.jobs[0].Queue'),'psc008');
});

test('unsaved account text cannot relabel the active account or continue a pending action',async()=>{
  const h=harness();h.run('navigate("Account")');const done=h.run('connectAccount()');
  h.input({id:'username',value:'another'});h.reply(h.messages[0],true,{SignedIn:true,Username:'student'});await done;
  assert.equal(h.run('state.username'),'student');assert.equal(h.run('state.formUsername'),'another');assert.equal(h.run('state.connected'),false);
});
test('abandoned preparation is released without submitting or retrying',async()=>{
  const h=harness();h.run('Object.assign(state,'+JSON.stringify({...basic,documentHandle:'source',fileName:'notes.pdf',connected:true})+');');
  const done=h.run('connectThenPrint()');h.run('navigate("Help")');h.reply(h.messages[0],true,{PreparedHandle:'prepared'});await tick();
  assert.equal(h.messages[1].Action,'releaseDocument');h.reply(h.messages[1]);await done;assert.equal(h.messages.some(m=>m.Action==='submitPrint'),false);
});
test('file pickers and native alerts have no timer that can interrupt interaction',()=>{
  const h=harness();const timers=h.timers.size;h.run('request("choosePDF");request("nativeAlert",{Title:"Test"})');assert.equal(h.timers.size,timers);
});
test('screens have no top banner or coloured notification boxes',()=>{
  const h=harness();for(const page of ['Print','Printers','Jobs','Queues','Help','Account']){
    h.run('state.page='+JSON.stringify(page)+';state.accountError={Error:"bad"};state.queueError="bad";state.jobsError="bad";state.printError={Error:"bad"};');
    const html=h.run('appShell()');assert.doesNotMatch(html,/class="toolbar"|inline-error|print-error-strip|Local document preview/);
  }
});

test('leaving Account while saving cannot continue abandoned public-key export',async()=>{
  const h=harness();h.run('navigate("Account")');const done=h.click('copyKey');assert.equal(h.messages[0].Action,'saveCredentials');
  h.run('navigate("Print")');h.reply(h.messages[0],true,{Username:'student',KeyPath:'approved-key',HasPassword:true});await done;
  assert.equal(h.messages.some(m=>m.Action==='getPublicKey'),false);assert.equal(h.messages.some(m=>m.Action==='nativeAlert'),false);assert.equal(h.run('state.page'),'Print');
});

test('printer directory renders the complete catalog, search and restricted access',()=>{
  const h=harness();const printers=JSON.parse(fs.readFileSync(new URL('../internal/catalog/printers.json',import.meta.url),'utf8')).map(p=>({ID:p.id,Location:p.location,Model:p.model,Kind:p.kind,Paper:p.paper,Access:p.access,Banner:p.banner,Queues:p.queues}));
  h.run('state.printers='+JSON.stringify(printers)+';state.includeRestricted=true;');
  const html=h.run('renderPrinters()');assert.equal(typeof html,'string');assert.doesNotMatch(html,/undefined/);
  for(const p of printers)assert.ok(html.includes('data-printer="'+p.ID+'"'),p.ID+' must be listed');
  h.run('state.pageSearch="psc008"');const searched=h.run('renderPrinters()');assert.ok(searched.includes('data-printer="psc008"'));assert.ok(!searched.includes('data-printer="psc011"'));
  h.run('state.pageSearch="";state.includeRestricted=false;');const publicOnly=h.run('renderPrinters()');for(const p of printers.filter(p=>p.Access!=='public'))assert.ok(!publicOnly.includes('data-printer="'+p.ID+'"'));
});


test('v1 account offers manual registration without a registration action',()=>{
  const h=harness();h.run('state.keyPath="/fixture/key"');
  const html=h.run('renderAccount()');
  assert.ok(html.includes('Copy public key'));assert.ok(html.includes('SSH key guide'));
  assert.ok(!html.includes('data-action="registerKey"'));assert.ok(!html.includes('Registered with SoC'));
});
test('key creation never starts a connection or automatic registration',async()=>{
  const h=harness();h.run('navigate("Account");saveAccount=async()=>true;document.getElementById=id=>({value:"fixture-passphrase"});');
  const done=h.click('createKey');await tick();const create=h.messages.find(m=>m.Action==='createKey');assert.ok(create);
  h.reply(create,true,{KeyPath:'/fixture/key',PublicKey:'ssh-ed25519 fixture',ManualRegistrationRequired:true,Disconnected:true});await done;await tick();
  assert.ok(h.messages.every(m=>['createKey','nativeAlert'].includes(m.Action)));
  assert.equal(h.run('state.connected'),false);assert.equal(h.run('state.trust'),null);
  assert.ok(h.messages.find(m=>m.Action==='nativeAlert').Message.includes('manually'));
});
test('copying an existing public key uses local export after saving the account',async()=>{
  const h=harness();h.run('navigate("Account");state.keyPath="/fixture/key";');
  const done=h.click('copyKey');const save=h.messages.find(m=>m.Action==='saveCredentials');assert.ok(save);
  h.reply(save,true,{Username:'student',KeyPath:'/fixture/key'});await tick();
  const exportRequest=h.messages.find(m=>m.Action==='getPublicKey');assert.ok(exportRequest);assert.equal(exportRequest.KeyPath,undefined);
  h.reply(exportRequest,true,{PublicKey:'ssh-ed25519 fixture'});await tick();
  const copy=h.messages.find(m=>m.Action==='copyText');assert.ok(copy);assert.equal(copy.Text,'ssh-ed25519 fixture');
  h.reply(copy);await done;
  assert.ok(h.messages.every(m=>['saveCredentials','getPublicKey','copyText','nativeAlert'].includes(m.Action)));
});
