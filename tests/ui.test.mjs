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
test('automatic queue polling does not trigger jump-key notifications',async()=>{
  const h=harness();h.run('state.page="Queues";state.queueName="psc008";state.connected=true;state.route="Direct";');
  const done=h.run('refreshQueue(false,false)');const queue=h.messages.find(message=>message.Action==='queue');assert.ok(queue);
  h.reply(queue,true,{Queue:'psc008',Jobs:[],KnownEmpty:true});await done;await tick();
  assert.equal(h.messages.some(message=>message.Action==='checkJumpKey'),false);
  assert.equal(h.messages.some(message=>message.Action==='showJumpSetupNotification'),false);
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


test('account keeps custom key controls and explains SoC jump-host setup',()=>{
  const h=harness();h.run('state.keyPath="/fixture/key"');
  const html=h.run('renderAccount()');
  assert.ok(html.includes('Copy public key'));assert.ok(html.includes('SSH key guide'));
  assert.ok(html.includes('outside the SoC network'));assert.ok(html.includes('Choose existing key'));
  assert.ok(html.includes('Create and enroll'));
});
test('Account omits the jump-host status row and keeps key setup controls',()=>{
  const h=harness();h.run('state.jumpSetup={Status:"noKey"}');
  const account=h.run('renderAccount()');
  assert.doesNotMatch(account,/jump-access-row|Check jump access|Ready for printing from COM2/);
  assert.ok(account.includes('SoC jump host'));
  assert.ok(!account.includes('data-action="autoEnrollJumpKey"'));
  assert.ok(!account.includes('data-action="chooseKeyForJump"'));
  assert.ok(account.includes('data-action="showCreateKey"'));
  assert.ok(account.includes('data-action="chooseKey"'));
  h.run('state.page="Print"');
  assert.doesNotMatch(h.run('appShell()'),/You don’t have an SSH key set up|jump-setup/);
});
test('Account keeps enrollment choices without restoring the removed status row',()=>{
  const h=harness();
  h.run('state.keyPath="/fixture/key";state.jumpSetup={Status:"rejected"}');
  let html=h.run('renderAccount()');
  assert.ok(html.includes('Enroll this key'));assert.ok(!html.includes('Create and enroll a new key'));
  h.run('state.jumpSetup={Status:"enrollmentFailed"}');html=h.run('renderAccount()');
  assert.doesNotMatch(html,/SoC did not accept this key update|jump-access-row|Check jump access/);
  assert.ok(html.includes('Create and enroll replacement key'));assert.ok(html.includes('Choose existing key'));
  h.run('state.jumpSetup={Status:"locked"}');html=h.run('renderAccount()');
  assert.ok(html.includes('Key passphrase'));
  h.run('state.jumpSetup={Status:"network"};state.keyPath="/fixture/key"');html=h.run('renderAccount()');
  assert.doesNotMatch(html,/Retry enrollment|Set up automatically|Check jump access|jump-access-row/);
});
test('missing registered public keys are requested before enrollment continues',()=>{
  const h=harness();h.run('state.jumpMissingFingerprints=["SHA256:oldkey"]');
  const html=h.run('renderJumpPublicKeyForm()');
  assert.ok(html.includes('SHA256:oldkey'));assert.ok(html.includes('id="jump-public-keys"'));
  assert.ok(html.includes('data-action="submitJumpPublicKeys"'));
});
test('jump setup notifications follow a direct user check and are silent on other routes',async()=>{
  const h=harness();
  h.run('state.connected=true;state.route="Jump host";');
  await h.run('checkJumpAccess(true,true)');
  assert.equal(h.messages.some(message=>message.Action==='checkJumpKey'),false);
  h.run('state.route="Direct";');
  const done=h.run('checkJumpAccess(true,true)');
  const check=h.messages.find(message=>message.Action==='checkJumpKey');assert.ok(check);
  h.reply(check,true,{Status:'noKey'});await done;await tick();
  const notification=h.messages.find(message=>message.Action==='showJumpSetupNotification');
  assert.ok(notification);assert.equal(notification.Status,'noKey');
  assert.equal(h.messages.some(message=>message.Action==='nativeAlert'&&message.Message.includes('SSH key set up')),false);
  h.reply(notification,true,{Delivered:true});
  h.run('window.socprintCommand(["jumpLater"]);lastJumpNotification=0;notifyJumpSetup("noKey");');
  assert.equal(h.messages.filter(message=>message.Action==='showJumpSetupNotification').length,1);
});
test('native notification actions open Account and dispatch the selected setup flow',()=>{
  const h=harness();h.run('activateAction=action=>state.lastNotificationAction=action;');
  for(const [command,action] of [['jumpAutoSetup','autoEnrollJumpKey'],['jumpUseOwnKey','chooseKeyForJump'],['jumpEnrollKey','enrollJumpKey'],['jumpChooseKey','chooseKeyForJump'],['jumpCreateKey','autoEnrollJumpKey']]) {
    h.run('window.socprintCommand('+JSON.stringify([command])+')');
    assert.equal(h.run('state.page'),'Account');assert.equal(h.run('state.lastNotificationAction'),action);
  }
  h.run('window.socprintCommand(["jumpLater"])');assert.equal(h.run('state.jumpDismissed'),true);
});
test('manual Account checks update status without sending a notification',async()=>{
  const h=harness();h.run('state.keyPath="/fixture/key";state.connected=true;state.route="Direct";');
  const done=h.run('checkJumpAccess(true,false)');const check=h.messages.find(message=>message.Action==='checkJumpKey');
  h.reply(check,true,{Status:'rejected'});await done;await tick();
  assert.equal(h.messages.some(message=>message.Action==='showJumpSetupNotification'),false);
  assert.ok(h.run('renderAccount()').includes('Enroll this key'));
});
test('a successful direct connection starts a non-blocking setup check',async()=>{
  const h=harness();
  const done=h.run('connectAccount()');const login=h.messages.find(message=>message.Action==='signIn');assert.ok(login);
  h.reply(login,true,{Username:'student',Route:'Direct'});await done;await tick();
  const check=h.messages.find(message=>message.Action==='checkJumpKey');assert.ok(check);
  h.reply(check,true,{Status:'network'});await tick();
  assert.equal(h.messages.some(message=>message.Action==='showJumpSetupNotification'),false);
});
test('a user queue reload starts a non-blocking direct-network setup check',async()=>{
  const h=harness();h.run('state.page="Queues";state.queueName="psc008";state.connected=true;state.route="Direct";');
  const done=h.run('refreshQueue(false)');const queue=h.messages.find(message=>message.Action==='queue');
  h.reply(queue,true,{Queue:'psc008',Jobs:[],KnownEmpty:true});await done;await tick();
  assert.ok(h.messages.some(message=>message.Action==='checkJumpKey'));
});
test('a print attempt checks jump access without waiting for the check',async()=>{
  const h=harness();h.run('state.page="Print";state.connected=true;state.route="Direct";');
  const snapshot={document:'doc',fileName:'fixture.pdf',username:'student',printer:{ID:'psc008'},queue:'psc008',options:{Copies:1}};
  const done=h.run('sendConfirmedPrint('+JSON.stringify(snapshot)+',operations.capture("print"))');
  const prepare=h.messages.find(message=>message.Action==='preparePDF');h.reply(prepare,true,{PreparedHandle:'prepared'});await tick();
  const submit=h.messages.find(message=>message.Action==='submitPrint');h.reply(submit,true,{State:'submitted'});await tick();
  const check=h.messages.find(message=>message.Action==='checkJumpKey');assert.ok(check);
  h.reply(check,true,{Status:'noKey'});await tick();
  const release=h.messages.find(message=>message.Action==='releaseDocument');h.reply(release,true,{Released:true});await done;
  assert.ok(h.messages.some(message=>message.Action==='showJumpSetupNotification'&&message.Status==='noKey'));
});
test('a jump-host network error stays silent and distinct from a rejected key',async()=>{
  const h=harness();h.run('state.connected=true;state.route="Direct";');
  const done=h.run('checkJumpAccess(true,true)');const check=h.messages.find(message=>message.Action==='checkJumpKey');
  h.reply(check,true,{Status:'network'});await done;await tick();
  assert.equal(h.run('state.jumpSetup.Status'),'network');
  assert.equal(h.messages.some(message=>message.Action==='showJumpSetupNotification'),false);
});
test('account edits invalidate a pending jump-key check result',async()=>{
  const h=harness();h.run('state.connected=true;state.route="Direct";');
  const done=h.run('checkJumpAccess(true)');const check=h.messages.find(m=>m.Action==='checkJumpKey');assert.ok(check);
  h.run('operations.changeAccount();invalidateJumpSetup();');
  h.reply(check,true,{Status:'rejected'});await done;
  assert.equal(h.run('state.jumpSetup'),null);
});
test('a new key starts its own check while an old account check is still pending',async()=>{
  const h=harness();h.run('state.connected=true;state.route="Direct";');
  const oldDone=h.run('checkJumpAccess(true,true)');
  const oldCheck=h.messages.find(m=>m.Action==='checkJumpKey');assert.ok(oldCheck);
  h.run('operations.changeAccount();invalidateJumpSetup();');
  const newDone=h.run('checkJumpAccess(true,true)');
  const checks=h.messages.filter(m=>m.Action==='checkJumpKey');assert.equal(checks.length,2);
  h.reply(oldCheck,true,{Status:'rejected'});await oldDone;
  assert.equal(h.run('state.jumpSetup'),null);
  assert.equal(h.run('state.jumpChecking'),true);
  h.reply(checks[1],true,{Status:'ready'});await newDone;await tick();
  assert.equal(h.run('state.jumpSetup.Status'),'ready');
});
test('cancelling a missing-key prompt discards supplied lines without enrolling them',()=>{
  const h=harness();h.run('state.jumpMissingFingerprints=["SHA256:old"];state.jumpExpectedFingerprints=["SHA256:old"];state.jumpPublicKeyDraft="ssh-ed25519 fixture";');
  h.click('cancelJumpEnrollment');
  assert.deepEqual(JSON.parse(JSON.stringify(h.run('state.jumpMissingFingerprints'))),[]);
  assert.equal(h.run('state.jumpPublicKeyDraft'),'');
  assert.equal(h.messages.some(message=>message.Action==='enrollJumpKey'),false);
});
test('creating and enrolling a managed key never sends its passphrase to the UI bridge',async()=>{
  const h=harness();h.run('saveAccount=async()=>true;state.jumpSetup={Status:"noKey"};');
  const done=h.click('autoEnrollJumpKey');await tick();
  const enroll=h.messages.find(m=>m.Action==='createAndEnrollKey');assert.ok(enroll);
  assert.equal(enroll.Passphrase,undefined);assert.equal(enroll.KeyPassphrase,undefined);
  h.reply(enroll,true,{Enrolled:true,Ready:true,KeyPath:'/managed/key',NewKeyCreated:true,Disconnected:true});
  await done;
  assert.equal(h.run('state.keyPath'),'/managed/key');assert.equal(h.run('state.connected'),false);
  assert.equal(h.run('state.jumpSetup.Status'),'ready');
});
test('creating a passphrase-protected key immediately starts SoC enrollment',async()=>{
  const h=harness();h.run('navigate("Account");saveAccount=async()=>true;document.getElementById=id=>({value:"fixture-passphrase"});');
  const done=h.click('createKey');await tick();const create=h.messages.find(m=>m.Action==='createKey');assert.ok(create);
  h.reply(create,true,{KeyPath:'/fixture/key',PublicKey:'ssh-ed25519 fixture',Disconnected:true});await tick();
  const enroll=h.messages.find(m=>m.Action==='enrollJumpKey');assert.ok(enroll);
  h.reply(enroll,true,{Enrolled:true,Ready:true,KeyPath:'/fixture/key'});await done;
  assert.ok(h.messages.every(m=>['createKey','enrollJumpKey','clearJumpSetupNotification','nativeAlert'].includes(m.Action)));
  assert.equal(h.run('state.connected'),false);assert.equal(h.run('state.trust'),null);
  assert.equal(h.run('state.jumpSetup.Status'),'ready');
  assert.equal(h.messages.some(m=>m.Action==='nativeAlert'&&m.Message.includes('Enroll it with the SoC SSH Key Service')),false);
});
test('confirmed enrollment stays successful while jump verification is pending',async()=>{
  const h=harness();h.run('saveAccount=async()=>true;state.jumpSetup={Status:"rejected"};');
  const done=h.click('autoEnrollJumpKey');await tick();
  const enroll=h.messages.find(m=>m.Action==='createAndEnrollKey');assert.ok(enroll);
  h.reply(enroll,true,{Enrolled:true,Ready:false,Status:'registered',VerificationStatus:'network',KeyPath:'/managed/key',NewKeyCreated:true,Message:'Your SSH key is registered with SoC.'});
  await done;
  assert.equal(h.run('state.jumpSetup.Status'),'registered');
  assert.ok(!h.run('renderAccount()').includes('Enroll this key'));
  assert.equal(h.run('state.accountError'),null);
  assert.ok(h.messages.some(m=>m.Action==='nativeAlert'&&m.Title==='SSH key registered'));
  assert.equal(h.messages.some(m=>m.Action==='showJumpSetupNotification'),false);
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
