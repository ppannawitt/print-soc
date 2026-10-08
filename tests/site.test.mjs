import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
const site=new URL('../site/',import.meta.url);
const context=vm.createContext({});vm.runInContext(fs.readFileSync(new URL('release.js',site),'utf8'),context);
const validate=context.PrintSoCRelease.publishedDownload;
const prefix='https://github.com/ppannawitt/print-soc/releases/download/v1.0.0/';
const assets=['Print-at-SoC-Universal.dmg','Print-at-SoC-Universal.dmg.sha256','release-validation.json'].map(name=>({name,state:'uploaded',size:1,browser_download_url:prefix+name}));
const release={tag_name:'v1.0.0',draft:false,prerelease:false,assets};
test('release lookup accepts only stable repository assets with a DMG, checksum and validation record',()=>{
  assert.equal(validate(release).download,prefix+'Print-at-SoC-Universal.dmg');
  for(const bad of [null,{...release,draft:true},{...release,prerelease:true},{...release,tag_name:'v1.0.0-rc1'},{...release,assets:assets.slice(0,2)},{...release,assets:assets.map(a=>({...a,browser_download_url:'https://evil.invalid/'+a.name}))},{...release,assets:assets.map(a=>({...a,size:0}))},{...release,assets:assets.map(a=>({...a,name:a.name.replace('Universal','Universal-Development')}))}])assert.equal(validate(bad),null);
});
test('all website pages have valid local navigation, language and accessible main content',()=>{
  for(const name of ['index.html','printing.html','ssh-keys.html']){
    const html=fs.readFileSync(new URL(name,site),'utf8');
    assert.ok(html.includes('lang="en"'));assert.ok(html.includes('id="main"'));assert.ok(html.includes('aria-current="page"'));assert.ok(html.includes('Skip to content'));
    for(const match of html.matchAll(/(?:href|src)="([^"#]+)"/g)){
      const href=match[1];if(/^https:\/\//.test(href))continue;
      assert.ok(fs.existsSync(new URL(href.split('#')[0],site)),name+': missing local file '+href);
      if(href.includes('#'))assert.ok(fs.readFileSync(new URL(href.split('#')[0],site),'utf8').includes('id="'+href.split('#')[1]+'"'));
    }
  }
});
test('published v1 has a direct download, checksum and truthful signing notice',()=>{
  const home=fs.readFileSync(new URL('index.html',site),'utf8');
  assert.ok(home.includes('Available now'));assert.ok(home.includes('Not Apple-notarized'));
  assert.ok(home.includes('href="'+prefix+'Print-at-SoC-Universal.dmg"'));
  assert.ok(home.includes('href="'+prefix+'Print-at-SoC-Universal.dmg.sha256"'));
  assert.doesNotMatch(home, /V1 is being prepared|<button id="download"|<a[^>]*>GitHub<\/a>/);
  for(const name of ['index.html','printing.html','ssh-keys.html'])assert.doesNotMatch(fs.readFileSync(new URL(name,site),'utf8'),/xattr|spctl --master-disable|curl.+\|\s*(?:sh|bash)/);
});
