'use strict';
(function(root){
  const repo='https://github.com/ppannawitt/print-soc';
  function verifiedDownload(release){
    if(!release||release.draft||release.prerelease||!/^v\d+\.\d+\.\d+$/.test(release.tag_name)||!Array.isArray(release.assets))return null;
    const names=['Print-at-SoC-Universal.dmg','Print-at-SoC-Universal.dmg.sha256','release-validation.json'];
    const assets=names.map(name=>release.assets.find(a=>a.name===name&&a.state==='uploaded'&&a.size>0));
    if(assets.some(a=>!a))return null;
    const prefix=repo+'/releases/download/'+release.tag_name+'/';
    if(assets.some(a=>a.browser_download_url!==prefix+a.name))return null;
    return {version:release.tag_name,download:assets[0].browser_download_url,checksum:assets[1].browser_download_url};
  }
  root.PrintSoCRelease={verifiedDownload};
  if(!root.document)return;
  const status=document.getElementById('release-status');
  fetch('https://api.github.com/repos/ppannawitt/print-soc/releases/latest',{headers:{Accept:'application/vnd.github+json'},signal:AbortSignal.timeout(8000)})
    .then(response=>{if(response.status===404)return null;if(!response.ok)throw new Error('unavailable');return response.json();})
    .then(release=>{
      const result=verifiedDownload(release);if(!result)return;
      const link=document.createElement('a');link.id='download';link.className='download-button';link.href=result.download;link.textContent='Download for macOS';
      document.getElementById('download').replaceWith(link);
      status.textContent=result.version+' · Universal Mac app';
      const checksum=document.getElementById('checksum');checksum.href=result.checksum;checksum.textContent='SHA-256 checksum';checksum.hidden=false;
    }).catch(()=>{status.textContent='Release status is unavailable. Check GitHub Releases for verified downloads.';});
})(typeof globalThis!=='undefined'?globalThis:this);
