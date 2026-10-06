import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {resolve,sep,extname} from 'node:path';
import {fileURLToPath} from 'node:url';
import {nativeBrowser,browserAvailable} from './test-browser.mjs';

const root=resolve(fileURLToPath(new URL('public/',import.meta.url)));
const brand=resolve(fileURLToPath(new URL('../brand/',import.meta.url)));

test('native active tabs, live viewport resize, credential continuation and transient capture recovery',{skip:browserAvailable?false:'Native browser unavailable'},async t=>{
  const mutations=[];
  let owner='human',epoch=2,width=1280,height=800,failedViews=0,viewRequests=0,tabsRequests=0,unselected=false;
  const shots=new Map();
  const runs=[{id:'initial',session:'fixture-chat',status:'completed',model:'fixture-model',prompt:'Fill the fixture form.',text:'The password is missing. Please supply one to continue.',steps:2,started:'2026-10-06T00:00:00Z'}];
  const handler=async(req,res)=>{
    const url=new URL(req.url,'http://fixture');
    const json=(value,status=200)=>{res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(value));};
    if(url.pathname.startsWith('/api/')){
      let payload;
      if(req.method==='POST'){let body='';for await(const bytes of req)body+=bytes;payload=JSON.parse(body);assert.equal(req.headers['x-csrf-token'],'fixture-csrf');mutations.push({path:url.pathname,payload});}
      switch(url.pathname){
        case '/api/me':json({user:{id:'fixture',username:'FixtureUser',role:'admin'},csrf:'fixture-csrf'});break;
        case '/api/providers':json({providers:[{id:'fixture',name:'Fixture provider',configured:true,models:[{id:'fixture-model',name:'Fixture model'}],quota:{status:'unavailable'}}],defaultProvider:'fixture',defaultModel:'fixture-model'});break;
        case '/api/sessions':json([{id:'fixture-chat',name:'Fixture form'}]);break;
        case '/api/task-profiles':json({profiles:[]});break;
        case '/api/documents/capabilities':json({ocr:true,pdfText:true});break;
        case '/api/task-selection':json({profileId:''});break;
        case '/api/attachments':json({attachments:[]});break;
        case '/api/task-profiles/proposals':json({proposals:[]});break;
        case '/api/core/v1/runs':
          if(req.method==='POST'){
            assert.equal(payload.continue,true);
            owner='agent';epoch++;
            const run={id:'followup',session:'fixture-chat',status:'running',model:'fixture-model',prompt:payload.prompt,steps:1,started:'2026-10-06T00:01:00Z'};runs.push(run);json(run);
          }else json(runs);
          break;
        case '/api/core/v1/control':
          if(req.method==='POST'){owner=payload.owner;epoch++;if(owner==='human'&&runs.at(-1).status==='running'){runs.at(-1).status='cancelled';runs.at(-1).error='context canceled';}}
          json({owner,epoch,tab:'form',active:false});break;
        case '/api/core/v1/browser/tabs':tabsRequests++;json(unselected?[{id:'form',url:'https://form.example/fixture',title:'Fixture form',active:false}]:[{id:'form',url:'https://form.example/fixture',title:'Fixture form',active:true},{id:'background',url:'',title:'',active:false}]);break;
        case '/api/core/v1/browser/viewport':
          assert.equal(payload.session,'fixture-chat');assert.equal(payload.tab,'form');
          await new Promise(r=>setTimeout(r,120));width=payload.width;height=payload.height;json(payload);break;
        case '/api/core/v1/view':
          viewRequests++;
          if(failedViews>0){failedViews--;json({error:'Temporary fixture capture failure'},503);break;}
          json({tab:url.searchParams.get('tab'),mimeType:'image/png',data:shots.get(`${width}x${height}`),viewport:{width,height}});break;
        case '/api/core/v1/events':case '/api/core/v1/approvals':json([]);break;
        default:json({error:'Unknown fixture route'},404);
      }
      return true;
    }
    const pathname=url.pathname==='/'?'/index.html':url.pathname;
    const base=pathname.startsWith('/brand/')?brand:root;
    const path=resolve(base,'.'+(pathname.startsWith('/brand/')?pathname.slice(6):pathname));
    if(!path.startsWith(base+sep))return false;
    try{const data=await readFile(path);res.writeHead(200,{'Content-Type':{'.html':'text/html','.js':'text/javascript','.mjs':'text/javascript','.css':'text/css','.svg':'image/svg+xml'}[extname(path)]||'application/octet-stream'});res.end(data);return true;}catch{return false;}
  };
  const {evaluate}=await nativeBrowser(t,{handler,ready:'!!document.querySelector(".landing")'});
  for(const [w,h] of [[1280,800],[1920,1080]])shots.set(`${w}x${h}`,await evaluate(`(()=>{const canvas=document.createElement('canvas');canvas.width=${w};canvas.height=${h};canvas.getContext('2d').fillRect(0,0,${w},${h});return canvas.toDataURL('image/png').split(',')[1]})()`));
  const wait=async expression=>{const deadline=Date.now()+8000;while(Date.now()<deadline){if(await evaluate(expression))return;await new Promise(r=>setTimeout(r,25));}throw new Error('Frontend condition timed out: '+expression);};
  const waitServer=async predicate=>{const deadline=Date.now()+8000;while(Date.now()<deadline){if(predicate())return;await new Promise(r=>setTimeout(r,25));}throw new Error('Frontend stopped polling the fixture');};
  const click=selector=>evaluate(`document.querySelector(${JSON.stringify(selector)}).click()`);
  await evaluate('location.hash="#workspace"');
  await wait('document.querySelector("#browser-frame")?.naturalWidth===1280');
  assert.equal(await evaluate('document.querySelector("#browser-address").value'),'https://form.example/fixture');
  assert.equal(await evaluate('document.querySelector("[data-tab=form] .tab-agent-badge").textContent'),'Agent');
  assert.equal(await evaluate('document.querySelector("[data-tab=form] .tab-observer-badge").textContent'),'View');
  assert.equal(await evaluate('document.querySelector("[data-tab=background] .tab-title").textContent'),'New tab');
  assert.equal(await evaluate('document.querySelector("#browser-error").hidden'),true);
  await click('[data-tab=background]');await wait('document.querySelector("[data-tab=background]").getAttribute("aria-pressed")==="true"');
  await click('#follow-agent');await wait('document.querySelector("[data-tab=form]").getAttribute("aria-pressed")==="true"');
  await click('[data-action=toggle-language]');await wait('document.documentElement.lang==="it" && !!document.querySelector("[data-tab=form] .tab-agent-badge")');
  assert.equal(await evaluate('document.querySelector("[data-tab=form] .tab-agent-badge").textContent'),'Agente');
  await click('[data-action=toggle-language]');await wait('document.documentElement.lang==="en" && !!document.querySelector("#prompt")');
  await evaluate('document.querySelector("#prompt").value="Continue using the disposable fixture password.";document.querySelector("#prompt-form").requestSubmit()');
  await wait('document.querySelector("#agent-status").textContent==="Waiting for model"');
  const lease={owner,epoch};
  await evaluate('window.__tabNode=document.querySelector("[data-tab=form]");window.__imageLoads=0;document.querySelector("#browser-frame").addEventListener("load",()=>window.__imageLoads++);document.querySelector("#viewport-select").value="desktop-fullhd";document.querySelector("#viewport-select").dispatchEvent(new Event("change",{bubbles:true}))');
  await wait('document.querySelector("#viewport-select").disabled');
  await wait('document.querySelector("#browser-frame").naturalWidth===1920 && !document.querySelector("#viewport-select").disabled');
  assert.deepEqual({owner,epoch},lease,'Resolution must not take control or cancel the pending provider');
  assert.equal(runs.at(-1).status,'running');
  assert.equal(await evaluate('document.querySelector("#follow-agent").disabled'),true);
  assert.equal(await evaluate('document.querySelector("#browser-error").hidden'),true);
  assert.equal(mutations.filter(m=>m.path!=='/api/core/v1/runs'&&m.path!=='/api/core/v1/browser/viewport').length,0,'Observation and resolution do not send input, takeover or stop');
  const requestsBefore=tabsRequests;await wait('window.__tabNode===document.querySelector("[data-tab=form]")');
  await waitServer(()=>tabsRequests>requestsBefore);
  assert.equal(await evaluate('window.__tabNode===document.querySelector("[data-tab=form]")'),true,'Unchanged tab polling preserves DOM and focus');
  const loads=await evaluate('window.__imageLoads'),viewsBefore=viewRequests;
  await waitServer(()=>viewRequests>viewsBefore+1);
  assert.equal(await evaluate('window.__imageLoads'),loads,'Identical frames do not decode/reload the image');
  failedViews=1;await click('#refresh-view');await wait('!document.querySelector("#browser-error").hidden');
  assert.equal(await evaluate('document.querySelector("#browser-error").classList.contains("reconnecting")'),true);
  assert.equal(await evaluate('document.querySelector("#browser-frame").hidden'),false,'Transient failure keeps the last valid page visible');
  await wait('document.querySelector("#browser-error").hidden');
  runs.at(-1).status='completed';runs.at(-1).text='Fixture field completed and verified.';owner='human';epoch++;
  await wait('document.querySelector("#messages").textContent.includes("Fixture field completed and verified")');
  assert.doesNotMatch(await evaluate('document.querySelector("#messages").textContent'),/Task interrupted|is not a function/);
  unselected=true;await wait('document.querySelectorAll("[data-tab]").length===1 && !document.querySelector(".tab-agent-badge")');
  assert.equal(await evaluate('document.querySelector("[data-tab=form]").getAttribute("aria-pressed")'),'true','The only owned page stays observable before an agent selects its target');
  assert.equal(await evaluate('document.querySelector("#browser-frame").hidden'),false);
});
