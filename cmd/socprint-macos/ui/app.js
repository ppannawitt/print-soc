
"use strict";
const state = {
  page:"Print", printers:[], username:"", formUsername:"", keyPath:"", hasPassword:false,
  hasKeyPassphrase:false, connected:false, route:"", network:null, busy:"",
  accountError:null, accountNotice:"", trust:null, generatedPublicKey:"",
  formPassword:"", formKeyPassphrase:"", filePath:"", fileName:"", fileSize:0,
  pageCount:0, pageRange:"", copies:"1",
  printAttemptInProgress:false, printHelpInFlight:false,
  documentHandle:"", orientation:"portrait", autoRotate:true, scaleMode:"fit", scalePercent:"100", nUp:1, pageMode:"all", selectedPages:[], previewStart:0, previewImages:{}, previewError:"", previewBusy:false, jobsError:"", queueLoading:false,
  printStep:0, printer:null, queue:"", simplex:false, noBanner:false,
  printResult:null, printerSearch:"", directoryPrinter:"",
  jobs:[], queuePrinter:"", queueName:"", queueData:null, queueError:"",
  includeRestricted:true, pageSearch:"", termsVersion:"2", termsAccepted:false,
  termsChecked:false, termsSaving:false,
  passwordVisible:false, keyPassphraseVisible:false,
  revealedSavedPassword:false, revealedSavedKeyPassphrase:false
};
const operations=new SPModel.Operations();
let previewGeneration=0,previewTimer=null;
let nextID=0;
let nativeAlertQueue=Promise.resolve();
const pending = new Map();
function request(action,data) {
  const id=String(++nextID);
  const message=Object.assign({ID:id,Action:action},data||{});
  return new Promise(function(resolve,reject) {
    const interactive=["choosePDF","chooseKey","nativeAlert","revealSecret"].includes(action);
    const timer=interactive?null:setTimeout(function(){pending.delete(id);reject({Code:"timeout",Error:action==="submitPrint"?"Submission did not return in time. Check Jobs before retrying.":"The action timed out. Try again."});},action==="submitPrint"?240000:action==="preparePDF"?150000:90000);
    pending.set(id,{resolve:resolve,reject:reject,timer:timer});
    if (!window.webkit || !window.webkit.messageHandlers || !window.webkit.messageHandlers.socprint) {
      clearTimeout(timer);pending.delete(id); reject({Code:"bridgeUnavailable",Error:"The app connection is unavailable."}); return;
    }
    window.webkit.messageHandlers.socprint.postMessage(message);
  });
}
window.socprintReceive=function(response) {
  const item=pending.get(String(response.ID));
  if(!item) return;
  clearTimeout(item.timer);pending.delete(String(response.ID));
  if(response.OK) item.resolve(response.Data||{});
  else item.reject({Code:response.Code||"error",Error:response.Error||"The request could not be completed.",Data:response.Data||{}});
};
function esc(value) {
  return String(value==null?"":value).replace(/[&<>"']/g,function(c){return {"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;","'":"&#39;"}[c];});
}
function bytes(value) {
  const n=Number(value)||0;
  if(n<1024) return n+" B";
  if(n<1024*1024) return (n/1024).toFixed(0)+" KB";
  if(n<1024*1024*1024) return (n/1024/1024).toFixed(1)+" MB";
  return (n/1024/1024/1024).toFixed(2)+" GB";
}
function friendlyState(value) {
  const names={submitted:"Submitted",queued:"Queued","gone":"No longer in queue",failed:"Failed",unknown:"Outcome unknown",cancelled:"Cancelled",pending:"Preparing"};
  return names[value]||value;
}
function setBusy(action) { state.busy=action; render(); }
function showNativeAlert(title,message,type,actions,duration,details,guard) {
  const items=Array.isArray(actions)?actions.slice(0,3):[];
  const buttons=items.map(function(item){return item.label;});
  if(!buttons.length)buttons.push("OK");
  else if(buttons.length===1)buttons.push("OK");
  const text=String(message||"")+(details?"\n\n"+details:"");
  const pendingAlert=nativeAlertQueue.then(function(){
    if(guard&&!guard())return {ButtonIndex:-1};
    return request("nativeAlert",{Title:title||"SimplyPrint @ SoC",Message:text,Style:type||"info",Buttons:buttons});
  });
  nativeAlertQueue=pendingAlert.then(function(){},function(){});
  pendingAlert.then(function(response){
    const selected=items[Number(response.ButtonIndex)];
    if(selected&&selected.action)activateAction(selected.action);
  },function(){
    // The WebKit alert delegate also presents a native macOS sheet.
    try { window.alert(String(title||"SimplyPrint @ SoC")+"\n\n"+text); } catch(_) {}
  });
  return pendingAlert;
}
function activateAction(action) {
  const proxy=document.createElement("button");
  proxy.dataset.action=action;
  proxy.hidden=true;
  document.body.appendChild(proxy);
  proxy.click();
  proxy.remove();
}
function clearNotification() {
  // Alerts are native sheets and are dismissed by macOS when the user acts.
}
function updateEyeButton(action,visible,label) {
  const button=document.querySelector('[data-action="'+action+'"]');
  if(!button)return;
  const verb=visible?"Hide ":"Show ";
  button.innerHTML=eyeIcon(visible);
  button.setAttribute("aria-label",verb+label);
  button.title=verb+label;
}
async function toggleSecretVisibility(fieldID,secret,visibleKey,revealedKey,valueKey,hasSaved) {
  const ctx=operations.capture("secret");
  let field=document.getElementById(fieldID);
  if(!field)return;
  const action=secret==="password"?"togglePassword":"toggleKeyPassphrase";
  const label=secret==="password"?"saved SoC password with Touch ID or Mac password":"SSH key passphrase with Touch ID or Mac password";
  if(field.type==="text") {
    field.type="password";
    state[visibleKey]=false;
    if(state[revealedKey]) {
      field.value="";
      state[valueKey]="";
      state[revealedKey]=false;
    }
    updateEyeButton(action,false,label);
    return;
  }
  if(!field.value&&!hasSaved) {
    showNativeAlert("Nothing to reveal","Enter a value first, then use Touch ID or your Mac password to show it.","info",[],0);
    return;
  }
  if(!field.value&&hasSaved) {
    const enteredUsername=(document.getElementById("username")||{}).value||"";
    if(enteredUsername.trim()!==state.username) {
      showNativeAlert("Save account name first","Save the new username before viewing its saved credentials.","warning",[],0);
      return;
    }
    try {
      const response=await request("revealSecret",{Secret:secret,Retrieve:true});
      if(!operations.current(ctx)||state.page!=="Account")return;
      field=document.getElementById(fieldID);if(!field)return;
      field.value=response.Value||"";
      state[valueKey]=field.value;
      state[revealedKey]=!!field.value;
    } catch(error) {
      if(operations.current(ctx)&&state.page==="Account"&&error.Code!=="authenticationCancelled") showNativeAlert("Could not reveal credential",error.Error||"Mac authentication could not be completed.","error",[],0);
      return;
    }
  } else {
    try {
      await request("revealSecret",{Secret:secret,Retrieve:false});
    } catch(error) {
      if(operations.current(ctx)&&state.page==="Account"&&error.Code!=="authenticationCancelled") showNativeAlert("Could not reveal credential",error.Error||"Mac authentication could not be completed.","error",[],0);
      return;
    }
  }
  if(!operations.current(ctx)||state.page!=="Account")return;
  field=document.getElementById(fieldID);if(!field)return;
  field.type="text";
  state[visibleKey]=true;
  updateEyeButton(action,true,label);
  field.focus();
}
function hideRevealedSecrets() {
  const password=document.getElementById("password");
  const keyPassphrase=document.getElementById("key-pass");
  if(state.revealedSavedPassword) {state.formPassword="";if(password)password.value="";}
  if(state.revealedSavedKeyPassphrase) {state.formKeyPassphrase="";if(keyPassphrase)keyPassphrase.value="";}
  state.revealedSavedPassword=false;state.revealedSavedKeyPassphrase=false;
  state.passwordVisible=false;state.keyPassphraseVisible=false;
}
async function showNativePrintHelp() { notifyError({Code:"noKey",Error:"SSH Jump requires a registered key. Set up a key in Account or reconnect to the SoC network."},"print"); }
function errorTitle(error,context) {
  const code=error&&error.Code||"";
  const titles={authentication:"Sign-in failed",noKey:"SSH key required",keyEnrollment:"SSH key not registered",keyPassphrase:"SSH key could not be unlocked",changedHostKey:"Server identity changed",hostKeyChanged:"Server identity changed",network:"Connection failed",timeout:"Connection timed out",dns:"Server not found",invalidFile:"Could not open PDF",fileTooLarge:"PDF is too large",invalidPageRange:"Check page selection",pdfPagesUnavailable:"Could not prepare selected pages",invalidCopies:"Check number of copies",queueFailed:"Connection failed",signInRequired:"Connection failed"};
  if(titles[code]) return titles[code];
  return context==="print"?"Could not print":context==="queue"?"Could not check queue":context==="jobs"?"Could not refresh jobs":context==="startup"?"SimplyPrint @ SoC could not start":context==="terms"?"Could not save acceptance":"Could not save settings";
}
function notifyError(error,context) {
  let message=error&&(error.Error||error.message)||String(error||"The request could not be completed.");
  const code=error&&error.Code||"";
  let details="";
  const actions=[];
  if(code==="noKey") {
    details="Connect to NUS VPN if needed, or set up a key while connected to the SoC network.";
    message="SSH Jump needs a registered key.";
    actions.push({label:"Set up SSH key",action:"accountSettingsForKey",primary:true});
  }
  else if(["noAccount","noPassword","authentication","keyEnrollment","keyPassphrase","changedHostKey","hostKeyChanged"].indexOf(code)>=0) actions.push({label:"Account settings",action:"accountSettings"});
  if(context==="print"&&code==="network") actions.push({label:"Retry",action:"retryPrint"});
  if(context==="queue"&&["network","queueFailed","signInRequired"].indexOf(code)>=0) actions.push({label:"Retry",action:"retryQueue"});
  const ctx=operations.capture(context);
  return showNativeAlert(errorTitle(error,context),message,"error",actions,0,details,()=>operations.current(ctx));
}
function button(label,action,kind,extra) {
  return '<button class="btn '+(kind||"")+'" data-action="'+esc(action)+'" '+(extra||"")+'>'+label+'</button>';
}
function linkButton(label,url) {
  return '<button class="link-btn" data-action="openURL" data-url="'+esc(url)+'">'+label+'</button>';
}
function input(label,id,value,type,placeholder,note) {
  return '<div class="field"><label for="'+esc(id)+'">'+label+'</label><input id="'+esc(id)+'" type="'+(type||"text")+'" value="'+esc(value||"")+'" placeholder="'+esc(placeholder||"")+'">'+(note?'<div class="field-note">'+note+'</div>':"")+'</div>';
}
function eyeIcon(visible) {
  return visible
    ? '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 3l18 18M10.6 10.6a2 2 0 002.8 2.8M9.9 5.2A10.8 10.8 0 0112 5c6.4 0 10 7 10 7a15.8 15.8 0 01-4.1 4.8M6.2 6.2C3.5 8 2 12 2 12s3.6 7 10 7a10.8 10.8 0 004.2-.8"/></svg>'
    : '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/></svg>';
}
function secretField(id,value,placeholder,visible,action,label) {
  return '<div class="secret-field"><input class="secret-input" id="'+id+'" type="'+(visible?'text':'password')+'" value="'+esc(value)+'" placeholder="'+esc(placeholder)+'" autocomplete="off"><button class="eye-toggle" type="button" data-action="'+action+'" aria-label="'+(visible?'Hide ':'Show ')+label+'" title="'+(visible?'Hide ':'Show ')+label+'">'+eyeIcon(visible)+'</button></div>';
}
function appShell() {
  if(!state.termsAccepted)return renderTerms();
  const pages=["Print","Printers","Jobs","Queues","Help"];
  const renderers={Print:renderPrint,Printers:renderPrinters,Jobs:renderJobs,Queues:renderQueues,Help:renderHelp,Account:renderAccount};
  return '<div class="app"><aside class="sidebar"><div class="brand"><img src="socprint://app/logo.svg" alt=""><div><div class="brand-name">SimplyPrint @ SoC</div></div></div><nav aria-label="Main navigation">'+pages.map(page=>'<button class="nav-item '+(state.page===page?'active':'')+'" data-action="navigate" data-page="'+page+'" '+(state.page===page?'aria-current="page"':'')+'><span class="nav-icon" aria-hidden="true">'+icon(page)+'</span>'+page+'</button>').join('')+'</nav><div class="sidebar-bottom"><button class="account-mini '+(state.page==='Account'?'active':'')+'" data-action="navigate" data-page="Account"><span class="nav-icon" aria-hidden="true">'+icon('Account')+'</span>Account</button></div></aside><main class="main"><section class="content '+(state.page==='Print'&&!state.printResult?'print-content':state.page==='Queues'?'queue-content':'')+'">'+(renderers[state.page]||renderHelp)()+'</section></main></div>';
}
function renderTerms() {
  return '<main class="terms-gate"><header class="terms-head"><p class="terms-kicker">SimplyPrint @ SoC</p><h1>Disclaimer and Terms of Use</h1><p>Review and accept these terms to continue.</p></header><article class="terms-copy">'+
    '<section><p>This software is built by fellow students, primarily to support students in the <strong>NUS School of Computing (SoC)</strong>. It is an independent student project and is <strong>not affiliated with, endorsed by, or officially supported by the National University of Singapore (NUS) or the NUS School of Computing</strong>.</p></section>'+
    '<section><h2>Use at Your Own Risk</h2><p>We do our best to make this software useful, accurate, and reliable, but mistakes or unexpected issues may still happen.</p><p>The software is provided <strong>“as is” and “as available”</strong>, without any guarantees regarding its accuracy, availability, security, or suitability for a particular purpose.</p><p>By using this software, you understand that you are doing so at your own risk. The developers are not responsible for any loss, damage, or other consequences that may result from using or relying on the software.</p></section>'+
    '<section><h2>Privacy &amp; Security</h2><p>We respect your privacy. We do <strong>not intentionally collect or store your personal information</strong>, unless we clearly tell you otherwise.</p><p>As with any software, we still encourage users to avoid entering sensitive or confidential information unless necessary.</p><p>SimplyPrint @ SoC stores your username and print history locally on this Mac. Passwords and key passphrases are saved in macOS Keychain only when you choose to save them. The PDF you choose is uploaded to SoC servers to process your print job. SSH keys are created locally. You register your public key manually with SoC; the app never submits or changes the registered-key list. The app does not send these details to the project developers.</p></section>'+
    '<section><h2>Non-Commercial Use</h2><p>This project is made for <strong>personal and educational use</strong>.</p><p>Please do not sell, commercially redistribute, or use this software as part of a commercial product or service without first obtaining permission from the developers.</p></section>'+
    '<section><h2>Attribution</h2><p>You’re welcome to learn from, reference, or build upon this project for educational purposes.</p><p>If you use the software, source code, or materials from this project in your own work, please provide <strong>appropriate acknowledgement and citation</strong> to the original project and its developers.</p></section></article>'+
    '<footer class="terms-footer"><label class="terms-agree"><input id="terms-agree" type="checkbox" '+(state.termsChecked?"checked":"")+'><span>I have read and agree to the Disclaimer and Terms of Use.</span></label><div class="terms-actions"><button class="btn primary" data-action="acceptTerms" '+(!state.termsChecked||state.termsSaving?"disabled":"")+'>'+(state.termsSaving?"Saving…":"Accept and continue")+'</button><button class="btn" data-action="quitApp">Quit</button></div></footer></main>';
}
function render() {
  const focused=document.activeElement,id=focused&&focused.id;
  const start=focused&&focused.selectionStart,end=focused&&focused.selectionEnd;
  const scrolls=Array.from(document.querySelectorAll('.content,.settings-pane,.preview-scroll,.printer-browser-list')).map(el=>[el.className,el.scrollTop]);
  const drafts={};document.querySelectorAll('input').forEach(el=>{if(el.id&& !['checkbox','radio'].includes(el.type)) drafts[el.id]=el.value;});
  document.getElementById('root').innerHTML=appShell();
  // Non-model draft fields (e.g. a new key passphrase) survive background updates.
  for(const key of ['new-key-pass','confirm-key-pass']){const el=document.getElementById(key);if(el&&key in drafts)el.value=drafts[key];}
  for(const [className,top] of scrolls){const el=Array.from(document.querySelectorAll('.content,.settings-pane,.preview-scroll,.printer-browser-list')).find(el=>el.className===className);if(el)el.scrollTop=top;}
  if(id){const el=document.getElementById(id);if(el){el.focus({preventScroll:true});if(typeof start==='number'&&['text','password','search'].includes(el.type))el.setSelectionRange(start,end);}}
}
function renderAccount() {
  const keyName=state.keyPath?state.keyPath.split("/").pop():"No key selected";
  const replacingKey=!!state.keyPath;
  let keyActions='<div class="key-actions"><div class="key-actions-main">'+
    button(replacingKey?"Create replacement key…":"Create new key…","showCreateKey","primary")+
    button("Choose existing key…","chooseKey","")+'</div>'+
    (state.keyPath?'<div class="key-actions-more">'+
      button("Copy public key","copyKey","link-btn")+
      button("Remove key","clearKey","link-btn danger")+'</div>':"")+'</div>';
  let createForm="";
  if(state.showCreateKey) {
    createForm='<section class="create-key-form" aria-labelledby="create-key-title"><h2 id="create-key-title">'+(replacingKey?"Replace SSH key":"Create an SSH key")+'</h2><p class="create-key-help">Protect the private key with a passphrase.</p><div class="field-grid">'+
      input("Passphrase","new-key-pass","","password","At least 8 characters","")+input("Confirm passphrase","confirm-key-pass","","password","Enter it again","")+
      '</div><div class="btn-row">'+button(state.busy==="key"?(replacingKey?"Replacing…":"Creating…"):(replacingKey?"Replace key":"Create key"),"createKey","primary",state.busy==="key"?"disabled":"")+' '+button("Cancel","hideCreateKey","")+'</div></section>';
  }

  return '<div class="page-head"><div><h1>Account</h1></div></div>'+
    '<div class="account-sections">'+'<div class="form-group account-status"><span>'+ (state.connected?'Connected · '+esc(state.route):'Not connected')+'</span><div class="btn-row">'+button(state.connected?'Disconnect':'Connect',state.connected?'disconnect':'connect','',state.busy?'disabled':'')+'</div></div><h2 class="group-title account-section-title">SoC account</h2><p class="account-create-note">If you do not have mySoC account yet, '+linkButton("create account here","https://mysoc.nus.edu.sg/~newacct/")+'.</p><div class="form-group">'+
    '<div class="form-row"><label for="username">Username</label><input id="username" type="text" value="'+esc(state.formUsername)+'" placeholder="SoC username" autocomplete="username"></div>'+
    '<div class="form-row"><label for="password">Password</label>'+secretField("password",state.formPassword,state.hasPassword?"Saved in Keychain":"SoC password",state.passwordVisible,"togglePassword",state.hasPassword?"saved SoC password · authenticate to reveal":"SoC password")+'</div></div>'+

    '<div class="group-actions">'+button(state.busy==="save"?"Saving…":"Save","saveCredentials","",state.busy==="save"?"disabled":"")+(state.hasPassword?button("Forget credentials","forgetCredentials","subtle danger"):"")+'</div>'+
    '<h2 class="group-title account-section-title key-section-title">SSH key</h2><div class="key-settings">'+
    '<div class="key-file-row"><div class="key-file-copy"><span class="key-label">Private key file</span><code title="'+esc(state.keyPath)+'">'+esc(keyName)+'</code></div></div>'+
    '<div class="key-pass-row"><label for="key-pass">Key passphrase</label>'+secretField("key-pass",state.formKeyPassphrase,state.hasKeyPassphrase?"Saved in Keychain":"Passphrase",state.keyPassphraseVisible,"toggleKeyPassphrase",state.hasKeyPassphrase?"SSH key passphrase · authenticate to reveal":"SSH key passphrase")+'<span class="keychain-hint">'+(state.hasKeyPassphrase?"Saved · authenticate to reveal":"")+'</span></div>'+
    keyActions+'<p class="account-create-note">Register your public key manually. '+linkButton("SSH key guide","https://ppannawitt.github.io/print-soc/ssh-keys.html")+'.</p>'+createForm+'</div></div>';
}
function stepper(current) {
  const labels=["PDF","Printer","Options","Review"];
  return '<div class="stepper">'+labels.map(function(label,i){
    return (i?'<div class="step-line"></div>':"")+'<div class="step '+(i===current?"current":(i<current?"done":""))+'"><span class="num">'+(i<current?"✓":String(i+1))+'</span><span>'+label+'</span></div>';
  }).join("")+'</div>';
}
function publicPrinters() {
  return state.printers.filter(function(p){return p.Access==="public";});
}
function groupedLocations(printers) {
  const groups=[],byLocation=new Map();
  printers.forEach(function(printer){
    const name=printer.Location||"Location not listed";
    let group=byLocation.get(name);
    if(!group){group={name:name,printers:[]};byLocation.set(name,group);groups.push(group);}
    group.printers.push(printer);
  });
  return groups;
}
function locationRows(printers) {
  if(!printers.length) return '<div class="empty">Printer locations could not be loaded. Restart SimplyPrint @ SoC and try again.</div>';
  return groupedLocations(printers).map(function(group){
    return '<section class="location-group"><h3 class="location-heading">'+esc(group.name)+'<span>'+group.printers.length+' printer'+(group.printers.length===1?'':'s')+'</span></h3>'+group.printers.map(function(p){
      return '<div class="location-row"><strong>'+esc(p.ID)+(p.Access!=="public"?' <span class="pill amber">Restricted</span>':"")+'</strong><span class="location-spec">'+esc(p.Paper)+' · '+esc(p.Kind)+'</span><span class="small muted">'+p.Queues.length+' queues</span></div>';
    }).join('')+'</section>';
  }).join('');
}
function recentFiles(limit) {
  const rows=state.jobs.filter(function(job){return !state.username||job.Username===state.username;}).slice(0,limit||5);
  if(!rows.length) return '<div class="empty">No submissions yet.</div>';
  return rows.map(function(job){
    const date=job.SubmittedAt?new Date(job.SubmittedAt).toLocaleString(undefined,{month:'short',day:'numeric',hour:'numeric',minute:'2-digit'}):'';
    return '<div class="recent-row"><div><strong title="'+esc(job.FileName)+'">'+esc(job.FileName)+'</strong><small>'+esc(job.PrinterID)+' · '+esc(job.Queue)+'</small></div><div class="recent-state '+esc(job.State)+'">'+esc(friendlyState(job.State))+'<div class="recent-date">'+esc(date)+'</div></div></div>';
  }).join('');
}
function queueMode(printer,queue) { return SPModel.mode(printer,queue); }
function queueFor(printer,simplex,noBanner) { return SPModel.queueFor(printer,simplex,noBanner); }
function readPrintOptions() {
  try {const value=SPModel.options(state);return {value:value,pageRange:value.PageRange,copies:value.Copies};}
  catch(e){return {error:e.message};}
}
function setPrinter(printer) {
  state.printer=printer||null;
  state.queue=printer?printer.Queues[0]:"";
  if(printer){const mode=queueMode(printer,state.queue);state.simplex=mode.simplex;state.noBanner=mode.noBanner;}
  schedulePreview();
}
function setPrintMode(key,value) {
  const simplex=key==="simplex"?!!value:state.simplex,noBanner=key==="noBanner"?!!value:state.noBanner;
  const queue=queueFor(state.printer,simplex,noBanner);
  if(!queue)return;
  state.queue=queue;state.simplex=simplex;state.noBanner=noBanner;render();
}
function renderPrint() {
  if(state.printResult)return renderPrintResult();
  const printer=state.printer,opts=readPrintOptions(),value=opts.value;
  const queue=printer?queueMode(printer,state.queue):null;
  const row=(label,control)=>'<div class="print-row"><label>'+label+'</label>'+control+'</div>';
  const select=(id,items,current)=>'<select id="'+id+'" aria-label="'+id.replaceAll('-',' ')+'">'+items.map(([v,label,disabled])=>'<option value="'+esc(v)+'" '+(String(v)===String(current)?'selected':'')+' '+(disabled?'disabled':'')+'>'+esc(label)+'</option>').join('')+'</select>';
  let preview='';
  if(state.documentHandle){
    let count=0;try{count=Math.ceil(SPModel.pages(state.pageMode==='selection'?'':(value?.PageRange||''),state.pageCount).length/(state.pageMode==='selection'?1:Number(state.nUp)));}catch(_){}
    const visible=Array.from({length:Math.min(3,Math.max(0,count-state.previewStart))},(_,i)=>state.previewStart+i);
    preview='<div class="document-bar"><div class="document-name"><strong title="'+esc(state.fileName)+'">'+esc(state.fileName)+'</strong><small>'+bytes(state.fileSize)+' · '+state.pageCount+' pages</small></div>'+button('Change…','choosePDF','')+'</div><div class="preview-scroll">'+(state.pageMode==='selection'?'<div class="selection-caption">'+state.selectedPages.length+' pages selected.</div>':'')+visible.map(sheet=>{
      const img=state.previewImages[sheet];return '<figure class="sheet-preview">'+(img?'<img src="'+img.Image+'" alt="'+(state.pageMode==='selection'?'Document page ':'Output sheet ')+(sheet+1)+'">':'<div class="preview-loading" role="status">'+(state.previewError?'Preview unavailable':'Preparing preview…')+'</div>')+(state.pageMode==='selection'?'<button class="thumbnail-select '+(state.selectedPages.includes(sheet+1)?'selected':'')+'" data-action="togglePage" data-number="'+(sheet+1)+'" aria-pressed="'+state.selectedPages.includes(sheet+1)+'">'+(state.selectedPages.includes(sheet+1)?'✓ ':'')+'Page '+(sheet+1)+'</button>':'')+'<figcaption class="sheet-label">'+(state.pageMode==='selection'?'Page ':'Sheet ')+(sheet+1)+' of '+count+'</figcaption></figure>';
    }).join('')+'</div><div class="preview-navigation">'+button('‹','previewPrevious','',state.previewStart===0?'disabled aria-label="Previous preview sheets"':'aria-label="Previous preview sheets"')+'<span>'+Math.min(state.previewStart+1,count)+'–'+Math.min(state.previewStart+3,count)+' of '+count+'</span>'+button('›','previewNext','',state.previewStart+3>=count?'disabled aria-label="Next preview sheets"':'aria-label="Next preview sheets"')+'</div>';
  }else preview='<div class="preview-empty">'+icon('Print')+'<h2>Choose a document</h2>'+button('Choose PDF…','choosePDF','primary')+'<small class="muted">PDF · up to 1 GB</small></div>';
  const printerItems=publicPrinters().map(p=>[p.ID,p.ID+' · '+p.Location.replace(/\s*\([^)]*\)/g,'')]);
  const sideItems=[['duplex','Double-sided',!printer||!queueFor(printer,false,state.noBanner)],['simplex','Single-sided',!printer||!queueFor(printer,true,state.noBanner)]];
  const bannerItems=[['yes','Include banner',!printer||!queueFor(printer,state.simplex,false)],['no','No banner',!printer||!queueFor(printer,state.simplex,true)]];
  const pageChoice=(mode,label,extra='')=>'<label><input type="radio" name="pages" value="'+mode+'" '+(state.pageMode===mode?'checked':'')+'>'+label+extra+'</label>';
  const settings='<div class="print-group">'+row('Printer',select('printer-select',printerItems,printer?.ID))+row('Paper', '<span>'+esc(printer?.Paper||'—')+(printer?.Paper==='A3'?' · 297 × 420 mm':' · 210 × 297 mm')+'</span>')+row('Colour','<span>'+esc(printer?.Kind||'—')+'</span>')+'</div><div class="print-group">'+row('Copies','<input id="copy-count" aria-label="Copies" type="number" min="1" max="99" value="'+esc(state.copies)+'">')+'<div class="print-row"><div class="page-choices"><strong>Pages</strong>'+pageChoice('all','All '+(state.pageCount||'')+' pages')+pageChoice('range','Range','<input id="page-range" aria-label="Page range" placeholder="1–3, 5" value="'+esc(state.pageRange)+'" '+(state.pageMode!=='range'?'disabled':'')+'>')+pageChoice('selection','Selection in preview')+'</div></div>'+row('Orientation',select('orientation', [['portrait','Portrait'],['landscape','Landscape']],state.orientation))+'</div><h2 class="print-section">Preview & scaling</h2><div class="print-group">'+row('Auto rotate','<label class="checkbox-label"><input id="auto-rotate" type="checkbox" '+(state.autoRotate?'checked':'')+'>Match page to paper</label>')+row('Scale',select('scale-mode',[['fit','Fit entire page'],['fill','Fill paper (crop)'],['percent','Custom percentage']],state.scaleMode))+(state.scaleMode==='percent'?row('Percentage','<input id="scale-percent" aria-label="Scale percentage" type="number" min="1" max="400" value="'+esc(state.scalePercent)+'">'):'')+'</div><h2 class="print-section">Layout</h2><div class="print-group">'+row('Pages per sheet',select('n-up',[1,2,4,6,9,16].map(n=>[n,n+' '+(n===1?'page':'pages')]),state.nUp))+row('Sides',select('sides',sideItems,state.simplex?'simplex':'duplex'))+row('Banner',select('banner',bannerItems,state.noBanner?'no':'yes'))+'</div>'+(!state.documentHandle?'<section class="recent-section"><div class="recent-heading"><h2>Recent submissions</h2>'+button('View all','navigate','link-btn','data-page="Jobs"')+'</div><div class="recent-list">'+recentFiles(3)+'</div></section>':'');
  return '<div class="print-workspace"><section class="preview-pane" aria-label="Document preview">'+preview+'</section><section class="settings-pane" aria-label="Print settings">'+settings+'</section></div><footer class="print-footer"><div><strong>'+esc(printer?printer.ID+' · '+printer.Location:'Select a printer')+'</strong><small>'+esc(opts.error||((value?.PageRange?'Pages '+value.PageRange:'All pages')+' · '+state.copies+' '+(Number(state.copies)===1?'copy':'copies')+' · '+(queue?.simplex?'Single-sided':'Double-sided')+' · '+state.queue))+'</small></div>'+button(state.busy==='print'?'Sending…':'Print','confirmPrint','primary',(!state.documentHandle||!!opts.error||!!state.busy||state.printAttemptInProgress)?'disabled':'')+'</footer>';
}
function printerCards() {
  const query=state.printerSearch.toLowerCase().trim();
  const rows=publicPrinters().filter(function(p){return !query||[p.ID,p.Location,p.Model,p.Kind,p.Paper].join(" ").toLowerCase().indexOf(query)>=0;});
  if(!rows.length) return '<div class="empty">No printers found.</div>';
  return rows.map(function(p){
    return '<button class="printer-option" data-action="selectPrinter" data-printer="'+esc(p.ID)+'"><strong>'+esc(p.ID)+'</strong><div class="loc">'+esc(p.Location)+'</div><div class="printer-meta"><span class="pill">'+esc(p.Paper)+'</span><span class="pill">'+esc(p.Kind)+'</span><span class="pill">'+p.Queues.length+' queue'+(p.Queues.length===1?"":"s")+'</span></div></button>';
  }).join("");
}
function renderReview() {
  return '<h2>Review before printing</h2><p class="muted">Check the file and queue before submitting.</p><div class="summary">'+
    summary("File",state.fileName)+summary("Size",bytes(state.fileSize))+summary("Location",state.printer.Location)+summary("Printer",state.printer.ID)+
    summary("Paper and colour",state.printer.Paper+" · "+state.printer.Kind)+summary("Print mode",state.queue.indexOf("-sx")>=0?"Single-sided":"Double-sided")+
    summary("Banner",String(state.printer.Banner||"Not specified"))+summary("Exact queue",state.queue)+summary("Account",state.username+"@stu")+
    '</div><div class="review-note">Accepted does not mean printed.</div>'+
    '<div class="btn-row">'+button("Back","stepBack","")+' '+button(state.busy==="print"?"Sending…":"Confirm and print","confirmPrint","primary",state.busy==="print"?"disabled":"")+'</div>';
}
function summary(label,value) {
  return '<div class="summary-row"><div class="label">'+esc(label)+'</div><div class="value">'+esc(value)+'</div></div>';
}
function renderPrintResult() {
  const r=state.printResult;
  const unknown=r.State==="unknown", ok=r.State==="submitted", failed=r.State==="failed";
  const title=ok?"Submitted":(unknown?"Outcome unknown":(failed?"Print not confirmed":"Print failed"));
  const icon=ok?"✓":(unknown?"?":"!");
  const cls=ok?"":(unknown?"warn":"fail");
  const actions=unknown
    ? button("Open Jobs to check","navigate","primary",'data-page="Jobs"')+' '+button("Check this queue","goQueue","")
    : button("Print another PDF","newPrint","primary")+' '+button("View Jobs","navigate","",'data-page="Jobs"');
  return '<div class="page-head"><div><h1>Print a PDF</h1><p>Submission result</p></div></div>'+'<div class="card"><div class="result-icon '+cls+'">'+icon+'</div><div class="result-title">'+title+'</div><div class="result-copy">'+esc(r.Message||"")+'</div>'+
    '<div class="summary">'+summary("File",r.FileName||state.fileName)+summary("Queue",r.Queue||state.queue)+
    summary("Pages",r.PageRange||"All pages")+summary("Copies",String(r.Copies||1))+
    (r.SpoolerID?summary("Server job ID",r.SpoolerID):"")+(r.OperationID?summary("Operation reference",r.OperationID):"")+'</div>'+
    '<div class="btn-row">'+actions+'</div></div>';
}
function renderPrinters() {
  const q=state.pageSearch.toLowerCase().trim();
  const rows=state.printers.filter(function(p){
    if(!state.includeRestricted&&p.Access!=="public") return false;
    return !q||[p.ID,p.Location,p.Model,p.Kind,p.Paper,p.Access].join(" ").toLowerCase().indexOf(q)>=0;
  });
  if(rows.length&&!rows.some(function(p){return p.ID===state.directoryPrinter;})) state.directoryPrinter=rows[0].ID;
  const selected=rows.find(function(p){return p.ID===state.directoryPrinter;})||null;
  const groups=[],groupMap=new Map();
  rows.forEach(function(p){const building=(p.Location.match(/^(AS6|COM\d+)/)||[])[0]||"Other";let group=groupMap.get(building);if(!group){group={name:building,printers:[]};groupMap.set(building,group);groups.push(group);}group.printers.push(p);});
  const list=groups.map(function(group){return '<div class="printer-building"><div class="location-heading">'+esc(group.name)+'<span>'+group.printers.length+'</span></div>'+group.printers.map(function(p){return '<button class="printer-list-row '+(selected&&selected.ID===p.ID?"selected":"")+'" data-action="inspectPrinter" data-printer="'+esc(p.ID)+'"><strong>'+esc(p.ID)+(p.Access!=="public"?' <span class="pill amber">Restricted</span>':"")+'</strong><small>'+esc(p.Location)+'</small></button>';}).join("")+'</div>';}).join("");
  const detail=selected?'<div class="printer-detail"><h2>'+esc(selected.ID)+'</h2><div class="detail-location">'+esc(selected.Location)+'</div><div class="detail-grid">'+
    '<div class="setting-row"><span>Paper</span><strong>'+esc(selected.Paper)+'</strong></div><div class="setting-row"><span>Print</span><strong>'+esc(selected.Kind)+'</strong></div></div>'+
    '<div class="control-label">Available queues</div><div class="queue-tags">'+selected.Queues.map(function(queue){const mode=queueMode(selected,queue);return '<span class="queue-tag"><code>'+esc(queue)+'</code><small>'+(mode.simplex?"Single-sided":"Double-sided")+(mode.noBanner?" · No banner":"")+'</small></span>';}).join("")+'</div>'+
    (selected.Access==="public"?'<div class="btn-row">'+button("Print to this printer","printToPrinter","primary",'data-printer="'+esc(selected.ID)+'"')+'</div>':'<div class="group-caption">This printer is not available to student accounts.</div>')+'</div>':'<div class="printer-detail empty">No matching printers.</div>';
  return '<div class="catalog-tools"><input class="search" id="catalog-search" placeholder="Search printers or locations" value="'+esc(state.pageSearch)+'"><button class="link-btn" data-action="toggleRestricted">'+(state.includeRestricted?"Hide restricted printers":"Show restricted printers")+'</button></div><div class="printer-browser"><div class="printer-browser-list">'+list+'</div>'+detail+'</div>';
}
function renderJobs() {
  const rows=state.jobs;
  return '<div class="page-head"><div><h1>Jobs</h1></div>'+button("Refresh","refreshJobs","")+'</div>'+
    '<div class="card">'+(rows.length?'<table class="table"><thead><tr><th>File</th><th>Printer</th><th>Submitted</th><th>Status</th><th></th></tr></thead><tbody>'+rows.map(function(j){
      const date=new Date(j.SubmittedAt).toLocaleString();
      const cancel=(j.State==="queued"&&j.SpoolerID)?button("Cancel","cancelJob","subtle",'data-job="'+esc(j.ID)+'"'):"";
      return '<tr><td><strong>'+esc(j.FileName)+'</strong><div class="small muted">'+esc(j.Queue)+'</div><div class="small muted">'+esc(jobOptions(j))+'</div></td><td>'+esc(j.PrinterID)+'</td><td class="small">'+esc(date)+'</td><td><span class="state '+esc(j.State)+'">'+esc(friendlyState(j.State))+'</span>'+(j.State==="unknown"?'<div class="small muted">Check queue before resending.</div>':"")+'</td><td>'+cancel+'</td></tr>';
    }).join("")+'</tbody></table>':'<div class="empty"><strong>No print jobs yet.</strong></div>')+'</div>';
}
function publicQueues() {
  const rows=[];
  publicPrinters().forEach(function(p){p.Queues.forEach(function(q){rows.push({printer:p,queue:q});});});
  return rows;
}
function renderQueues() {
  const queues=publicQueues(),data=state.queueData;
  const jobs=data&&Array.isArray(data.Jobs)?data.Jobs:[];
  const canRender=!!data&&(data.KnownEmpty||jobs.length>0);
  const table=jobs.length?'<table class="table queue-table"><thead><tr><th>Rank</th><th>Owner</th><th>Job</th><th>Document</th><th>Size</th></tr></thead><tbody>'+jobs.map(j=>'<tr><td>'+esc(j.Rank)+'</td><td>'+esc(j.Owner)+'</td><td class="mono">'+esc(j.ID)+'</td><td>'+esc(j.FileName)+'</td><td>'+esc(j.Size)+'</td></tr>').join('')+'</tbody></table>':'';
  const empty=state.queueLoading?'Loading…':canRender?'Queue empty':state.queueError?'Queue unavailable':'Choose a printer queue';
  const refresh='<button class="icon-button" data-action="refreshQueue" aria-label="Refresh queue" title="Refresh queue" '+(state.queueLoading?'disabled':'')+'><svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M20 7v5h-5M4 17v-5h5M19 11a7 7 0 0 0-12-5L4 9m16 6-3 3A7 7 0 0 1 5 13"/></svg></button>';
  return '<div class="queue-view"><div class="queue-controls"><select id="queue-select" aria-label="Printer queue"><option value="">Choose queue…</option>'+queues.map(x=>'<option value="'+esc(x.queue)+'" '+(x.queue===state.queueName?'selected':'')+'>'+esc(x.queue+' · '+x.printer.Location.replace(/\s*\([^)]*\)/g,''))+'</option>').join('')+'</select>'+refresh+'</div><div class="queue-body">'+(table||data&&!canRender?table||'<pre class="queue-output">'+esc(data.Output)+'</pre>':'<div class="queue-blank">'+icon('Queues')+'<span role="status">'+empty+'</span></div>')+'</div>'+(data?'<footer class="queue-footer"><span>'+jobs.length+' '+(jobs.length===1?'job':'jobs')+'</span><span>Updated '+new Date(data.CheckedAt).toLocaleTimeString(undefined,{hour:'2-digit',minute:'2-digit'})+'</span></footer>':'')+'</div>';
}
function renderHelp() {
  return '<div class="page-head"><h1>Printing at SoC</h1></div><div class="help-grid"><section><h2>Getting started</h2><p>Choose a PDF, select a printer, then print.</p><div class="btn-row">'+linkButton('App &amp; manual printing guide','https://ppannawitt.github.io/print-soc/printing.html')+'</div><div class="btn-row">'+linkButton('Create SoC account','https://mysoc.nus.edu.sg/~newacct/')+' '+linkButton('Enable Unix access','https://mysoc.nus.edu.sg/~myacct/services.cgi')+'</div></section><section><h2>Connection and SSH keys</h2><p>Outside NUS, use NUS VPN. Register SSH keys manually before using the jump host.</p><div class="btn-row">'+linkButton('Network access guide','https://dochub.comp.nus.edu.sg/cf/guides/unix/outside-access')+' '+linkButton('SSH key guide','https://ppannawitt.github.io/print-soc/ssh-keys.html')+'</div></section><section><h2>Submission status</h2><p>Submitted confirms server acceptance. For an unknown outcome, check the queue before sending again.</p></section><section><h2>Privacy</h2><p>Credentials use Keychain. History and previews stay local. PDFs are sent only to SoC. No telemetry.</p><p>SimplyPrint @ SoC is an independent student project and is not an official NUS application.</p></section></div>';
}

function accountValues() {
  return {
    Username:document.getElementById("username")?document.getElementById("username").value:state.formUsername,
    Password:(document.getElementById("password")||{}).value||"",
    KeyPath:(document.getElementById("key-path")||{}).value||state.keyPath,
    KeyPassphrase:(document.getElementById("key-pass")||{}).value||""
  };
}
async function saveAccount(options) {
  options=options||{};
  if(state.busy==='save')return false;
  const values=accountValues();
  operations.changeAccount();
  const ctx=operations.capture('save');
  const previousUsername=state.username;
  const previousKeyPath=state.keyPath;
  state.accountError=null;state.accountNotice="";
  if(!values.Username.trim()) {state.accountError={Code:"validation",Error:"Enter your SoC username."};notifyError(state.accountError,"account");render();return false;}
  state.formUsername=values.Username.trim();state.keyPath=values.KeyPath;
  state.formPassword=values.Password;state.formKeyPassphrase=values.KeyPassphrase;
  setBusy("save");
  try {
    const d=await request("saveCredentials",values);
    if(ctx.account!==operations.account)return false;
    state.username=d.Username;state.formUsername=d.Username;state.keyPath=d.KeyPath||"";
    if(previousUsername!==state.username||previousKeyPath!==state.keyPath) {

      state.generatedPublicKey="";
    }
    state.hasPassword=!!d.HasPassword;
    state.hasKeyPassphrase=!!d.HasKeyPassphrase;
    state.formPassword="";
    state.formKeyPassphrase="";
    state.passwordVisible=false;state.keyPassphraseVisible=false;
    state.revealedSavedPassword=false;state.revealedSavedKeyPassphrase=false;
    if(d.Disconnected){state.connected=false;state.route="";}
    state.accountNotice=d.VaultWarning||"Credentials saved securely on this Mac.";
    if(!options.silent&&operations.current(ctx))showNativeAlert(d.VaultWarning?"Credential storage":"Credentials saved",state.accountNotice,d.VaultWarning?"warning":"success",[],0);
    state.busy="";
    render();return operations.current(ctx);
  } catch(e) {
    if(ctx.account===operations.account){state.busy="";state.accountError=e;if(operations.current(ctx))notifyError(e,"account");render();}return false;
  }
}
async function connectAccount(values,ctx) {
  values=values||(state.page==='Account'?accountValues():{Username:state.username,KeyPath:state.keyPath});
  ctx=ctx||operations.capture('account');
  if(operations.active.has('connect'))return false;
  const attempt=operations.begin('connect');state.busy='signIn';render();
  try {
    let d=await request('signIn',values);
    while(d.NeedsTrust){
      if(!operations.current(ctx)){await request('cancelTrust');return false;}
      state.trust=d;
      const answer=await request('nativeAlert',{Title:'Verify this server',Message:'Compare this fingerprint with SoC IT before continuing.\n\n'+d.Host+'\n'+d.Fingerprint,Style:'warning',Buttons:['Cancel','Trust and continue']});
      if(answer.ButtonIndex!==1||!operations.current(ctx)){await request('cancelTrust');state.trust=null;return false;}
      d=await request('trustHost');
    }
    state.trust=null;
    if(!operations.current(ctx))return false;
    state.connected=true;state.username=d.Username||values.Username;state.formUsername=state.username;state.route=d.Route||'';state.accountError=null;state.accountNotice=d.VaultWarning||'';
    state.formPassword='';state.formKeyPassphrase='';
    return true;
  }catch(e){
    if(operations.current(ctx)){state.connected=false;state.accountError=e;state.accountNotice='';if(ctx.kind==='queue')state.queueError=e.Error;else if(ctx.kind==='print')state.printError=e;notifyError(e,ctx.kind==='queue'?'queue':ctx.kind==='print'?'print':'account');}
    return false;
  }finally{operations.end(attempt);if(state.busy==='signIn')state.busy='';render();}
}
async function pickPDF() {
  if(state.printAttemptInProgress)return;
  try {
    const picked=await request('choosePDF');if(picked.Cancelled)return;
    const previous=state.documentHandle;
    state.documentHandle=picked.DocumentHandle;state.fileName=picked.FileName;state.fileSize=picked.Size;state.pageCount=picked.PageCount;
    state.filePath='';state.pageRange='';state.pageMode='all';state.selectedPages=[];state.previewStart=0;
    state.printResult=null;state.printError=null;render();schedulePreview();
    if(previous)await request('releaseDocument',{Handle:previous});
  }catch(e){state.printError=e;notifyError(e,'print');render();}
}
async function sendConfirmedPrint(snapshot,ctx) {
  let prepared='';state.busy='print';render();
  try {
    const d=await request('preparePDF',{DocumentHandle:snapshot.document,Options:snapshot.options,PrinterID:snapshot.printer.ID,Queue:snapshot.queue,Username:snapshot.username});
    prepared=d.PreparedHandle;
    if(!operations.current(ctx))return;
    const result=await request('submitPrint',{PreparedHandle:prepared});
    if(result.OperationID)state.jobs=[{ID:result.OperationID,Username:snapshot.username,FileName:snapshot.fileName,PrinterID:snapshot.printer.ID,Queue:snapshot.queue,SubmittedAt:new Date().toISOString(),State:result.State,SpoolerID:result.SpoolerID||'',Message:result.Message||'',PrintSettings:JSON.stringify(snapshot.options)}].concat(state.jobs);
    if(operations.current(ctx)){state.printResult=result;state.printError=null;announce(result.Message);if(result.State==='failed'||result.State==='unknown'||result.Warning)notifyError({Error:result.Warning||result.Message},'print');}
  }catch(e){if(operations.current(ctx)){state.printError=e;if(e.Code==='timeout')state.printResult={State:'unknown',FileName:snapshot.fileName,Queue:snapshot.queue,PageRange:snapshot.options.PageRange,Copies:snapshot.options.Copies,Message:e.Error};notifyError(e,'print');}}
  finally{state.busy='';if(prepared)try{await request('releaseDocument',{Handle:prepared});}catch(e){state.printError=e;if(state.printResult)state.printResult.Warning=e.Error||'Local temporary PDF cleanup failed.';notifyError(e,'print');}render();}
}
async function connectThenPrint() {
  if(state.printAttemptInProgress||state.printResult?.State==='unknown')return;
  const read=readPrintOptions();if(read.error||!state.documentHandle){notifyError({Error:read.error||'Choose a PDF first.'},'print');return;}
  const ctx=operations.capture('print');
  const snapshot=Object.freeze({document:state.documentHandle,fileName:state.fileName,username:state.username,printer:{...state.printer},queue:state.queue,options:read.value});
  state.printAttemptInProgress=true;state.printError=null;render();
  try {
    const m=queueMode(snapshot.printer,snapshot.queue),o=snapshot.options;
    if(!confirm('Print “'+snapshot.fileName+'”?\n\n'+snapshot.printer.ID+' · '+snapshot.printer.Location+'\n'+o.Paper+' · '+snapshot.printer.Kind+' · '+(m.simplex?'Single-sided':'Double-sided')+' · '+(m.noBanner?'No banner':'Banner included')+'\nPages: '+(o.PageRange||'All')+' · Copies: '+o.Copies+'\n'+o.Orientation+' · '+o.ScaleMode+(o.ScaleMode==='percent'?' '+o.ScalePercent+'%':'')+' · '+o.PagesPerSheet+' pages per sheet\nQueue: '+snapshot.queue))return;
    if(!operations.current(ctx))return;
    if(!state.connected&&!await connectAccount(null,ctx))return;
    if(operations.current(ctx))await sendConfirmedPrint(snapshot,ctx);
  }finally{state.printAttemptInProgress=false;render();}
}
async function refreshJobs(explicit=false) {
  const ctx=operations.begin('jobs');if(!ctx)return;
  try{const d=await request('refreshJobs');if(operations.current(ctx)){state.jobs=d.Jobs||[];state.jobsError='';}}
  catch(e){if(operations.current(ctx)){state.jobsError=e.Error;notifyError(e,'jobs');}}
  finally{operations.end(ctx);render();}
}
async function refreshQueue(allowConnect=true) {
  if(state.page!=='Queues'||!state.queueName||(!allowConnect&&!state.connected))return;
  const ctx=operations.begin('queue');if(!ctx)return;
  const queue=state.queueName;state.queueLoading=true;state.queueError='';render();
  try {
    if(!state.connected&&!await connectAccount(null,ctx))return;
    if(!operations.current(ctx))return;
    const data=await request('queue',{Queue:queue});
    if(operations.current(ctx)){state.queueData=data;announce('Queue updated.');}
  }catch(e){if(operations.current(ctx)){state.connected=false;state.queueError=e.Error||'Connection failed.';notifyError(e,'queue');}}
  finally{operations.end(ctx);state.queueLoading=false;render();if(ctx.queue!==operations.queue&&state.page==='Queues'&&state.connected)refreshQueue(false);}
}
document.addEventListener("click",async function(event) {
  const target=event.target.closest("[data-action]"); if(!target) return;
  const action=target.dataset.action;
  if(action==="acceptTerms") {
    if(!state.termsChecked||state.termsSaving)return;
    state.termsSaving=true;render();
    try {
      await request("acceptTerms",{TermsVersion:state.termsVersion});
      state.termsAccepted=true;state.termsSaving=false;state.page="Print";render();
      request("detectNetwork").then(function(net){state.network=net;render();}).catch(function(){});
    } catch(e) {state.termsSaving=false;render();notifyError(e,"terms");}
    return;
  }
  if(action==="quitApp") {request("quitApp").catch(function(){});return;}
  if(action==="accountSettings") {
    clearNotification();navigate("Account");render();return;
  }
  if(action==="accountSettingsForKey") {
    clearNotification();navigate("Account");if(!state.keyPath)state.showCreateKey=true;render();return;
  }
  if(action==="retryPrint") {clearNotification();await connectThenPrint();return;}
  if(action==="retryQueue") {clearNotification();await refreshQueue(true);return;}
  if(action==="navigate") {
    if(target.dataset.page!=="Account")hideRevealedSecrets();
    navigate(target.dataset.page||"Print");
    if(state.page==="Jobs") refreshJobs();
    if(state.page==="Queues") {
      if(!state.queueName) {const q=publicQueues()[0];if(q)state.queueName=q.queue;}
      render();if(state.queueName)refreshQueue();return;
    }
    render();return;
  }
  if(action==="openURL") {request("openURL",{URL:target.dataset.url}).catch(function(e){notifyError(e,"account");});return;}
  if(action==="togglePassword") {await toggleSecretVisibility("password","password","passwordVisible","revealedSavedPassword","formPassword",state.hasPassword);return;}
  if(action==="toggleKeyPassphrase") {await toggleSecretVisibility("key-pass","keyPassphrase","keyPassphraseVisible","revealedSavedKeyPassphrase","formKeyPassphrase",state.hasKeyPassphrase);return;}
  if(action==="saveCredentials") {await saveAccount();return;}
  if(action==="forgetCredentials") {
    operations.changeAccount();
    if(!confirm("Forget the saved SoC password and SSH key passphrase from this Mac?")) return;
    try {await request("signOut",{Forget:true});state.username="";state.formUsername="";state.keyPath="";state.hasPassword=false;state.hasKeyPassphrase=false;state.formPassword="";state.formKeyPassphrase="";state.passwordVisible=false;state.keyPassphraseVisible=false;state.revealedSavedPassword=false;state.revealedSavedKeyPassphrase=false;state.generatedPublicKey="";state.connected=false;state.accountNotice="Saved credentials removed from Keychain.";state.accountError=null;showNativeAlert("Credentials removed",state.accountNotice,"success",[],3500);render();}
    catch(e){state.accountError=e;notifyError(e,"account");render();}return;
  }
  if(action==="disconnect") {
    operations.changeAccount();
    try {await request("signOut",{Forget:false});state.connected=false;state.accountNotice="Disconnected. Saved credentials are still available.";showNativeAlert("Disconnected",state.accountNotice,"info",[],3500);render();}
    catch(e){state.accountError=e;notifyError(e,"account");render();}return;
  }
  if(action==="chooseKey") {
    try {const d=await request("chooseKey");if(!d.Cancelled){const changed=d.Path!==state.keyPath;state.keyPath=d.Path;state.generatedPublicKey="";if(changed){state.hasKeyPassphrase=false;state.formKeyPassphrase="";state.revealedSavedKeyPassphrase=false;state.keyPassphraseVisible=false;}state.accountError=null;state.accountNotice="Key selected. Save the settings before connecting.";showNativeAlert("SSH key selected",state.accountNotice,"success",[],3500);render();}}
    catch(e){state.accountError=e;notifyError(e,"account");render();}return;
  }
  if(action==="clearKey") {
    operations.changeAccount();
    try {const vals=accountValues();const d=await request("saveCredentials",{Username:vals.Username,Password:vals.Password,ClearKey:true});state.keyPath="";state.generatedPublicKey="";state.hasKeyPassphrase=false;state.formKeyPassphrase="";state.keyPassphraseVisible=false;state.revealedSavedKeyPassphrase=false;if(d.Disconnected){state.connected=false;state.route="";}state.accountNotice="SSH key removed from this account profile.";state.accountError=null;showNativeAlert("SSH key removed",state.accountNotice,"success",[],3500);render();}
    catch(e){state.accountError=e;notifyError(e,"account");render();}return;
  }
  if(action==="showCreateKey") {
    const username=document.getElementById("username");
    const password=document.getElementById("password");
    const keyPassphrase=document.getElementById("key-pass");
    if(username)state.formUsername=username.value.trim();
    if(password)state.formPassword=password.value;
    if(keyPassphrase)state.formKeyPassphrase=keyPassphrase.value;
    state.accountError=null;state.showCreateKey=true;render();
    requestAnimationFrame(function(){
      const firstField=document.getElementById("new-key-pass");
      if(firstField){firstField.scrollIntoView({behavior:"smooth",block:"center"});firstField.focus();}
    });
    return;
  }
  if(action==="hideCreateKey") {state.accountError=null;state.showCreateKey=false;render();return;}
  if(action==="createKey") {
    const pass=(document.getElementById("new-key-pass")||{}).value||"";
    const confirmPass=(document.getElementById("confirm-key-pass")||{}).value||"";
    if(pass!==confirmPass){state.accountError={Code:"validation",Error:"The key passphrases do not match."};notifyError(state.accountError,"account");render();return;}
    const saved=await saveAccount({silent:true});
    if(!saved)return;
    const ctx=operations.capture('createKey');
    state.busy="key";state.accountError=null;render();
    try {
      const d=await request("createKey",{Passphrase:pass});
      if(!operations.current(ctx)){if(d.NeedsTrust)request('cancelTrust').catch(()=>{});state.busy='';render();return;}
      state.keyPath=d.KeyPath;state.generatedPublicKey=d.PublicKey;state.hasKeyPassphrase=!d.VaultWarning;state.formKeyPassphrase=pass;state.showCreateKey=false;
      if(d.Disconnected){state.connected=false;state.route="";}
      state.busy="";render();
      state.accountNotice=[d.VaultWarning,"Register this public key manually with SoC before using SSH Jump."].filter(Boolean).join(" ");
      showNativeAlert("SSH key created",state.accountNotice,d.VaultWarning?"warning":"info",[{label:"Open SSH key guide",action:"keyGuide"}]);
    } catch(e){state.busy="";if(operations.current(ctx)){state.accountError=e;notifyError(e,"account");}render();}return;
  }
  if(action==="keyGuide") {await request("openURL",{URL:"https://ppannawitt.github.io/print-soc/ssh-keys.html"}).catch(e=>notifyError(e,"account"));return;}
  if(action==="copyKey") {
    let ctx=operations.capture('account');
    try {
      let publicKey=state.generatedPublicKey;
      if(!publicKey){if(!await saveAccount({silent:true}))return;ctx=operations.capture("account");publicKey=(await request("getPublicKey")).PublicKey;}
      if(!operations.current(ctx))return;
      await request("copyText",{Text:publicKey});
      showNativeAlert("Public key copied","Paste it into SoC’s SSH Key Service using the manual guide.","info",[]);
    }catch(e){if(operations.current(ctx))notifyError(e,"account");}return;
  }
  if(action==='cancelTrust'){await request('cancelTrust').catch(()=>{});state.trust=null;state.busy='';render();return;}
  if(action==='connect'){await connectAccount();return;}
  if(action==='togglePage'){const n=Number(target.dataset.number);state.selectedPages=state.selectedPages.includes(n)?state.selectedPages.filter(p=>p!==n):state.selectedPages.concat(n).sort((a,b)=>a-b);render();return;}
  if(action==='previewPrevious'||action==='previewNext'){state.previewStart=Math.max(0,state.previewStart+(action==='previewNext'?3:-3));render();schedulePreview(false);return;}
  if(action==="choosePDF") {await pickPDF();return;}
  if(action==="setSides") {setPrintMode("simplex",target.dataset.sides==="simplex");return;}
  if(action==="setBanner") {setPrintMode("noBanner",target.dataset.banner==="no");return;}
  if(action==="inspectPrinter") {state.directoryPrinter=target.dataset.printer;render();return;}
  if(action==="toggleRestricted") {state.includeRestricted=!state.includeRestricted;render();return;}
  if(action==="printToPrinter") {
    const printer=state.printers.find(function(p){return p.ID===target.dataset.printer;});
    setPrinter(printer);navigate("Print");state.printResult=null;state.printError=null;render();return;
  }
  if(action==="selectPrinter") {
    state.printer=state.printers.find(function(p){return p.ID===target.dataset.printer;});
    if(state.printer){setPrinter(state.printer);state.printError=null;render();}return;
  }
  if(action==="selectQueue") {state.queue=target.dataset.queue;render();return;}
  if(action==="stepBack") {if(state.printStep===3)state.printStep=2;else if(state.printStep===2)state.printStep=1;else state.printStep=0;render();return;}
  if(action==="stepContinue") {if(state.printStep===2&&state.queue)state.printStep=3;render();return;}
  if(action==="confirmPrint") {await connectThenPrint();return;}
  if(action==="newPrint") {if(state.documentHandle)request("releaseDocument",{Handle:state.documentHandle}).catch(()=>{});state.documentHandle="";previewGeneration++;state.filePath="";state.fileName="";state.fileSize=0;state.pageCount=0;state.pageRange="";state.copies="1";state.printResult=null;state.printError=null;render();return;}
  if(action==="refreshJobs") {await refreshJobs(true);return;}
  if(action==="refreshQueue") {await refreshQueue();return;}
  if(action==="goQueue") {state.queueName=state.queue;operations.changeQueue();navigate("Queues");render();await refreshQueue();return;}
  if(action==="cancelJob") {
    if(!confirm("Cancel this verified job from the SoC queue?"))return;
    try {await request("cancelJob",{JobID:target.dataset.job});await showNativeAlert("Cancellation confirmed", "The selected job was cancelled.", "info", []);await refreshJobs();}
    catch(e){notifyError(e,"jobs");}return;
  }
});
document.addEventListener("input",function(event) {
  if(event.target.id==="password"){state.formPassword=event.target.value;if(state.passwordVisible)state.revealedSavedPassword=false;}
  if(event.target.id==="key-pass"){state.formKeyPassphrase=event.target.value;if(state.keyPassphraseVisible)state.revealedSavedKeyPassphrase=false;}
  if(event.target.id==="username"){state.formUsername=event.target.value;operations.changeAccount();hideRevealedSecrets();}
  if(event.target.id==="page-range"){state.pageRange=event.target.value;schedulePreview();}
  if(event.target.id==="copy-count"){state.copies=event.target.value;schedulePreview();}
  if(event.target.id==="scale-percent"){state.scalePercent=event.target.value;schedulePreview();}
  if(event.target.id==="printer-search") {
    state.printerSearch=event.target.value;
    const list=document.getElementById("printer-list");if(list)list.innerHTML=printerCards();
  }
  if(event.target.id==="catalog-search") {state.pageSearch=event.target.value;const pos=event.target.selectionStart;render();const next=document.getElementById("catalog-search");if(next){next.focus();next.setSelectionRange(pos,pos);}}
});
document.addEventListener("change",function(event) {
  if(event.target.id==="terms-agree") {
    state.termsChecked=event.target.checked;
    const accept=document.querySelector('[data-action="acceptTerms"]');
    if(accept)accept.disabled=!state.termsChecked||state.termsSaving;
  }
  if(event.target.name==="pages"){state.pageMode=event.target.value;state.previewStart=0;render();schedulePreview();}
  if(["orientation","scale-mode","n-up","auto-rotate"].includes(event.target.id)){const fields={orientation:"orientation","scale-mode":"scaleMode","n-up":"nUp","auto-rotate":"autoRotate"};state[fields[event.target.id]]=event.target.type==="checkbox"?event.target.checked:event.target.value;state.previewStart=0;render();schedulePreview();}
  if(event.target.id==="sides")setPrintMode("simplex",event.target.value==="simplex");
  if(event.target.id==="banner")setPrintMode("noBanner",event.target.value==="no");
  if(event.target.id==="printer-select") {const p=state.printers.find(function(x){return x.ID===event.target.value;});setPrinter(p);state.printError=null;render();}
  if(event.target.id==="queue-select"){operations.changeQueue();state.queueName=event.target.value;state.queueData=null;state.queueError="";render();if(state.queueName)refreshQueue();}
});
document.addEventListener("keydown",function(event) {
  if(event.key==="Enter"&&event.target.id==="password"){event.preventDefault();saveAccount();}
  if(event.metaKey&&event.key.toLowerCase()==="o"){event.preventDefault();hideRevealedSecrets();navigate("Print");state.printResult=null;render();pickPDF();return;}
  if(event.metaKey&&event.key==="Enter"&&state.page==="Print"){event.preventDefault();const print=document.querySelector('[data-action="confirmPrint"]');if(print&&!print.disabled)connectThenPrint();return;}
  if(event.metaKey&&event.key===","){event.preventDefault();hideRevealedSecrets();navigate("Account");render();}
});
function icon(name) {
  const paths={Print:'M6 9V3h12v6M6 17H3V9h18v8h-3M6 14h12v7H6zM17 11h1',Printers:'M4 4h16v16H4zM8 8h8M8 12h8M8 16h5',Jobs:'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM12 7v5l3 2',Queues:'M7 6h14M7 12h14M7 18h14M3 6h1M3 12h1M3 18h1',Help:'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM9 9a3 3 0 0 1 6 0c0 2-3 2-3 4M12 16v1',Account:'M12 3a4 4 0 1 0 0 8 4 4 0 0 0 0-8zM4 21v-3a8 8 0 0 1 16 0v3'};
  return '<svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="'+(paths[name]||paths.Print)+'"/></svg>';
}
function announce(message){const el=document.getElementById('announcer');if(el)el.textContent=message;}

function jobOptions(job){try{const o=JSON.parse(job.PrintSettings||'{}');return o.Paper?[o.PageRange?'Pages '+o.PageRange:'All pages',o.Copies+' copies',o.Orientation,o.PagesPerSheet+' per sheet'].join(' · '):'';}catch(_){return '';}}
function navigate(page){
  if(!['Print','Printers','Jobs','Queues','Help','Account'].includes(page))return;
  if(state.page!==page){operations.navigate();if(state.trust){request('cancelTrust').catch(()=>{});state.trust=null;}}
  if(page!=='Account')hideRevealedSecrets();state.page=page;
}
function schedulePreview(reset=true){
  clearTimeout(previewTimer);
  if(reset){previewGeneration++;state.previewImages={};state.previewError='';}
  if(!state.documentHandle)return;
  previewTimer=setTimeout(updatePreview,250);
}
let previewRunning=false,previewAgain=false;
async function updatePreview(){
  if(previewRunning){previewAgain=true;return;}
  const read=readPrintOptions();
  // Selection browsing always shows all source pages, even before the first selection.
  let options=read.value;
  if(state.pageMode==='selection'){try{options=SPModel.options({...state,pageMode:'all',nUp:1});}catch(e){state.previewError=e.message;render();return;}}
  if(!options){state.previewError=read.error;render();return;}
  const generation=previewGeneration,documentHandle=state.documentHandle,start=state.previewStart;
  const count=Math.ceil(SPModel.pages(options.PageRange,state.pageCount).length/options.PagesPerSheet);
  if(start>=count)state.previewStart=0;
  previewRunning=true;state.previewBusy=true;
  try{
    for(let sheet=state.previewStart;sheet<Math.min(state.previewStart+3,count);sheet++){
      if(generation!==previewGeneration||documentHandle!==state.documentHandle)break;
      if(state.previewImages[sheet])continue;
      const data=await request('previewPDF',{DocumentHandle:documentHandle,Options:options,Sheet:sheet});
      if(generation!==previewGeneration||documentHandle!==state.documentHandle)break;
      state.previewImages[sheet]=data;
      const keys=Object.keys(state.previewImages);while(keys.length>24)delete state.previewImages[keys.shift()];
      render();
    }
  }catch(e){if(generation===previewGeneration){state.previewError=e.Error||'Preview could not be prepared.';if(state.page==='Print')notifyError(e,'print');}}
  finally{previewRunning=false;state.previewBusy=false;render();if(previewAgain){previewAgain=false;schedulePreview(false);}}
}
window.socprintCommand=function(args){
  if(!state.termsAccepted)return;
  const command=args[0];
  if(command==='settings'){navigate('Account');render();}
  if(command==='open'){navigate('Print');render();pickPDF();}
  if(command==='print'){navigate('Print');render();if(state.documentHandle)connectThenPrint();else pickPDF();}
};
window.addEventListener('blur',function(){hideRevealedSecrets();render();});
document.addEventListener('visibilitychange',function(){if(document.hidden){hideRevealedSecrets();render();}});

request("initialize").then(function(d){
  state.printers=d.Printers||[];state.jobs=d.Jobs||[];state.username=d.Username||"";state.formUsername=state.username;state.keyPath=d.KeyPath||"";
  state.termsVersion=d.TermsVersion||"2";
  state.termsAccepted=d.AcceptedTermsVersion===state.termsVersion;
  state.hasPassword=!!d.HasSavedPassword;state.hasKeyPassphrase=!!d.HasSavedKeyPassphrase;
  state.directoryPrinter=publicPrinters().length?publicPrinters()[0].ID:"";
  if(publicPrinters().length)setPrinter(publicPrinters()[0]);
  render();
  if(state.termsAccepted)request("detectNetwork").then(function(net){state.network=net;if(!state.connected)render();}).catch(function(){});
}).catch(function(e){
  state.printers=[];render();notifyError(e||{Error:"Restart the app and try again."},"startup");
});
setInterval(function(){
  if(!document.hidden&&state.page==="Jobs"&&!state.busy&&!state.jobsError) refreshJobs();
  if(!document.hidden&&state.page==="Queues"&&state.queueName&&!state.busy&&state.connected) refreshQueue(false);
},10000);
