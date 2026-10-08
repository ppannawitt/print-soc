'use strict';
(function(root){
  const repo='https://github.com/ppannawitt/print-soc';
  function publishedDownload(release){
    if(!release||release.draft||release.prerelease||!/^v\d+\.\d+\.\d+$/.test(release.tag_name)||!Array.isArray(release.assets))return null;
    const names=['SimplyPrint-at-SoC-Universal.dmg','SimplyPrint-at-SoC-Universal.dmg.sha256','release-validation.json'];
    const assets=names.map(name=>release.assets.find(a=>a.name===name&&a.state==='uploaded'&&a.size>0));
    if(assets.some(a=>!a))return null;
    const prefix=repo+'/releases/download/'+release.tag_name+'/';
    if(assets.some(a=>a.browser_download_url!==prefix+a.name))return null;
    return {version:release.tag_name,download:assets[0].browser_download_url,checksum:assets[1].browser_download_url};
  }
  root.PrintSoCRelease={publishedDownload};
  if(!root.document)return;
  fetch('https://api.github.com/repos/ppannawitt/print-soc/releases/latest',{headers:{Accept:'application/vnd.github+json'},signal:AbortSignal.timeout(8000)})
    .then(response=>{if(response.status===404)return null;if(!response.ok)throw new Error('unavailable');return response.json();})
    .then(release=>{
      const result=publishedDownload(release);if(!result||result.version==='v1.0.1')return;
      const link=document.createElement('a');link.id='download';link.className='download-button';link.href=result.download;link.textContent='Download for macOS';
      document.getElementById('download').replaceWith(link);
      document.getElementById('current-release').textContent='Current release: '+result.version;
    }).catch(()=>{/* The published v1 download remains usable if the API is unavailable. */});
})(typeof globalThis!=='undefined'?globalThis:this);
