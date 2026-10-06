import {loadTasks,loadTaskChat,bindTaskUI,learningCards} from './tasks-ui.js?v=20261006-13';
import { t, language, setLanguage, locale } from './i18n.js?v=20261006-13';
import { api, core, post, setCsrf, ApiError, escapeHtml as e, array } from './api.js?v=20261006-13';
import { icon } from './icons.js?v=20261006-13';
import * as view from './views.js?v=20261006-13';
import { VIEWPORT_PRESETS, viewportArguments } from './viewports.js?v=20261006-13';
import { renderMarkdown } from './markdown.js?v=20261006-13';

const app = document.getElementById('app');
const modal = document.getElementById('modal');
const $ = selector => document.querySelector(selector);
let savedCollapsed=false, savedSidebarCollapsed=false;
try{savedCollapsed=localStorage.getItem('svolo:chat-collapsed')==='true';savedSidebarCollapsed=localStorage.getItem('svolo:sidebar-collapsed')==='true';}catch{}
const drafts=new Map();
const state = { taskProfiles:[],taskProfile:'',attachments:[],selectedDocuments:new Map(),uploading:false,chatCollapsed:savedCollapsed, sidebarCollapsed:savedSidebarCollapsed, user:null, providers:[], sessions:[], mcp:[], users:[], provider:'', model:'', session:'', tab:'', agentTab:'', tabs:[], followAgent:true, activity:null, runs:[], approvals:[], after:0, owner:'human', controlEpoch:0, controlBusy:false, browserView:null, browserVisible:false, liveText:new Map(), submitted:new Map(), busy:false, page:'', health:false };
let generation = 0, timer, frameTimer, abort, toastTimer, polling = false, framing = false, viewPending = false, viewAbort, takeover;
const statusLabels = { running:t("Al lavoro"), waiting_approval:t("In attesa del tuo consenso"), completed:'Completato', finished:'Completato', failed:t("Attività non riuscita"), stopped:'Interrotto', interrupted:'Interrotto', cancelled:'Interrotto' };
const activityLabels = {
  navigate:t("Apertura pagina"),'open-browser':t("Apertura browser"),'open-tab':t("Apertura scheda"),'open-in-new-tab':t("Apertura scheda"),'close-tab':t("Chiusura scheda"),'switch-tab':t("Cambio scheda"),
  state:t("Lettura pagina"),tabs:t("Lettura schede"),'read-page':t("Lettura pagina"),'snapshot-interactive':t("Lettura pagina"),'find-interactive':t("Ricerca nella pagina"),'accessibility-tree':t("Lettura pagina"),
  click:t("Interazione sulla pagina"),drag:t("Interazione sulla pagina"),'type-text':t("Compilazione campo"),fill:t("Compilazione campo"),select:t("Selezione campo"),check:t("Selezione campo"),'press-key':t("Interazione sulla pagina"),
  'inspect-inputs':t("Verifica campi"),'inspect-elements':t("Verifica elementi"),'element-info':t("Verifica elemento"),'query-selector':t("Ricerca elemento"),'get-element':t("Lettura elemento"),'inspect-links':t("Lettura collegamenti"),'inspect-images':t("Verifica immagini"),
  'assert-url':t("Verifica pagina"),'assert-title':t("Verifica pagina"),'assert-visible':t("Verifica pagina"),'assert-text':t("Verifica contenuto"),'assert-image-ready':t("Verifica immagine"),'wait-for':t("Attesa della pagina"),wait:t("Attesa"),
  scroll:t("Scorrimento pagina"),screenshot:t("Cattura pagina"),viewport:t("Adattamento vista"),upload:t("Caricamento file"),downloads:t("Verifica download"),'wait-download':t("Attesa download"),'verify-artifact':t("Verifica file"),
  'evaluate-js':t("Verifica della pagina"),'inject-js':t("Interazione sulla pagina"),'browser-task':t("Attività sul browser"),'browser-operation':t("Attività sul browser"),'inspect-network':t("Verifica connessione"),console:t("Verifica pagina"),
  'workspace-list':t("Lettura file"),'workspace-read':t("Lettura file"),'workspace-write':t("Scrittura file"),
};
function runErrorText(run) {
  if(!run.error)return '';
  const text=String(run.error);
  if(['cancelled','stopped','interrupted'].includes(run.status)||/context cancel(?:ed|led)/i.test(text))return t("Attività interrotta. Puoi intervenire nel browser e poi chiedere di continuare.");
  if(/step budget exhausted/i.test(text))return t("L’agente ha raggiunto il limite di passaggi. Puoi chiedergli di continuare dal punto raggiunto.");
  if(/context deadline exceeded/i.test(text))return t("Il provider non ha risposto in tempo. Riprova o scegli un altro modello.");
  const cleaned=text.replace(/(?:Post|Get) "https?:\/\/127\.0\.0\.1:\d+\/internal\/provider\/[^"\s]+"\s*:?\s*/g,'').trim();
  return cleaned.length>600?cleaned.slice(0,600)+'…':cleaned;
}

function tell(message, bad = false) {
  const toast = $('#toast'); toast.textContent = t(message); toast.classList.toggle('error', bad); toast.hidden = false;
  clearTimeout(toastTimer); toastTimer = setTimeout(() => { toast.hidden = true; }, bad ? 9000 : 4500);
}
function error(failure) {
  if (failure.name === 'AbortError') return;
  if (failure instanceof ApiError && failure.status === 401 && state.user) {
    stopScreen(); state.user = null; setCsrf(''); location.hash = '#login'; tell(t("La sessione è scaduta. Accedi di nuovo."), true); return;
  }
  tell(failure instanceof Error ? failure.message : String(failure), true);
}
function bind(selector, event, fn) {
  const element = $(selector); if (!element) return;
  element.addEventListener(event, ev => { try { Promise.resolve(fn(ev)).catch(error); } catch (failure) { error(failure); } });
}
function stopScreen() { clearInterval(timer); clearInterval(frameTimer); abort?.abort(); viewAbort?.abort(); viewAbort=undefined; abort = new AbortController(); polling = false; framing = false; viewPending = false; }
function signal() { return { signal:abort.signal }; }
function me(result) { if (!result?.user) throw new Error(t("Risposta di accesso non valida.")); state.user = result.user; setCsrf(result.csrf); }
const requireSession = () => { if (!state.session) throw new Error(t("Crea prima una sessione di lavoro.")); return state.session; };
const mutations = (path, body) => core(path, { method:'POST', body, timeout:25000, ...signal() });

function toggleTheme() {
  const theme = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
  document.documentElement.dataset.theme = theme;
  document.documentElement.style.colorScheme = theme;
  $('meta[name="theme-color"]').content = theme === 'dark' ? '#0c141d' : '#f8fafb';
  try { localStorage.setItem('svolo:theme', theme); } catch { /* The choice still applies to this page. */ }
  document.querySelectorAll('[data-action="toggle-theme"]').forEach(button => {
    button.setAttribute('aria-label', theme === 'dark' ? t("Attiva tema chiaro") : t("Attiva tema scuro"));
    button.setAttribute('aria-pressed', String(theme === 'dark'));
    button.title = theme === 'dark' ? t("Tema chiaro") : t("Tema scuro");
    button.innerHTML = icon(theme === 'dark' ? 'sun' : 'moon', 17);
  });
}
document.addEventListener('toggle',ev=>{
  const menu=ev.target;if(!menu.matches?.('.session-menu')||!menu.open)return;
  const box=menu.querySelector('summary').getBoundingClientRect(),actions=menu.querySelector('.session-actions');
  actions.style.left=Math.max(8,Math.min(window.innerWidth-155,box.right-145))+'px';
  actions.style.top=Math.max(8,Math.min(window.innerHeight-actions.offsetHeight-8,box.bottom+4))+'px';
},{capture:true});
document.addEventListener('click',ev=>{if(!ev.target.closest('.session-menu'))document.querySelectorAll('.session-menu[open]').forEach(menu=>menu.open=false);});
let languageSwitching = false;
document.addEventListener('click',async ev=>{
  const button=ev.target.closest('[data-action="toggle-language"]');
  if(!button||languageSwitching)return;
  languageSwitching=true;button.disabled=true;
  const inputs=[...document.querySelectorAll('input,textarea,select')].map(input=>({id:input.id,name:input.name,value:input.value,type:input.type}));
  setLanguage(language()==='en'?'it':'en');
  try{await route();for(const item of inputs){if(item.id==='prompt')continue;const input=item.id?document.getElementById(item.id):document.querySelector(`[name="${CSS.escape(item.name)}"]`);if(input&&input.type===item.type&&!['hidden','submit','button'].includes(item.type))input.value=item.value;}}catch(failure){error(failure);}finally{languageSwitching=false;const current=document.querySelector('[data-action=toggle-language]');if(current)current.disabled=false;}
});
document.addEventListener('click', ev => { if (ev.target.closest('[data-action="toggle-theme"]')) toggleTheme(); });

function resetBrowser() {
  state.tab='';state.agentTab='';state.tabs=[];state.followAgent=true;state.browserView=null;
  state.activity=null;
  state.controlEpoch=0;state.owner='human';state.controlBusy=false;
}
function applyControl(control) {
  if (!control || !['human','agent'].includes(control.owner)) return false;
  const epoch=Number(control.epoch);
  if (!Number.isFinite(epoch) || epoch<state.controlEpoch) return false;
  state.controlEpoch=epoch;state.owner=control.owner;
  renderControl();return true;
}
async function takeHumanControl() {
  if(state.owner!=='agent')return;
  if(takeover)return takeover;
  const sid=requireSession(),stamp=generation;
  const operation=(async()=>{
    state.controlBusy=true;renderControl();
    try{
      const control=await mutations('/v1/control',{session:sid,owner:'human'});
      if(stamp!==generation||sid!==state.session)throw new DOMException(t("Sessione cambiata"),'AbortError');
      if(!applyControl(control)||state.owner!=='human')throw new Error(t("Non è stato possibile riprendere il controllo. Riprova."));
    }finally{if(stamp===generation){state.controlBusy=false;renderControl();}}
  })();
  takeover=operation;
  try{await operation;}finally{if(takeover===operation)takeover=undefined;}
}

async function loadWorkspaceData() {
  await loadTasks(state,signal());
  const [providers, sessions] = await Promise.all([api('/api/providers', signal()), api('/api/sessions', signal())]);
  state.providers = array(providers?.providers).map(p=>({...p,models:array(p.models).map(m=>typeof m==='string'?{id:m,name:m}:m)}));
  state.sessions = array(sessions?.sessions || sessions);
  state.provider = providers?.defaultProvider || state.provider;
  state.model = providers?.defaultModel || state.model;
  const selected = state.providers.find(p=>p.id===state.provider && p.configured) || state.providers.find(p=>p.configured);
  if (selected) { state.provider=selected.id; if(!selected.models.some(m=>m.id===state.model))state.model=selected.models[0]?.id||''; }
  else { state.provider=''; state.model=''; }
  if (!state.sessions.some(s=>s.id===state.session)) {state.session=state.sessions[0]?.id||'';resetBrowser();}
}
async function route() {
  if($('#prompt'))drafts.set(state.session,$('#prompt').value);
  const stamp = ++generation; stopScreen(); modal.close();
  const hash = location.hash.slice(1); state.page = hash;
  if (!hash || ['possibilita','controllo','inizia'].includes(hash)) {
    document.title=t("Svolo — Il lavoro prende una nuova direzione"); app.innerHTML=view.landing(!!state.user);
    if(hash)requestAnimationFrame(()=>document.getElementById(hash)?.scrollIntoView()); return;
  }
  if (hash==='login') { if(state.user){location.hash='#workspace';return;} renderLogin(); return; }
  if (!state.user) { location.hash='#login'; return; }
  app.innerHTML=`<div class="boot"><img src="/brand/mark.svg" alt="" width="52" height="52"><p>${e(t("Apriamo il tuo workspace…"))}</p></div>`;
  try {
    await loadWorkspaceData(); if(stamp!==generation)return;
    const settings=hash.startsWith('settings');
    if(!settings&&!state.sessions.length){
      const created=await post('/api/sessions',{name:t("Nuova chat")},signal());
      if(stamp!==generation)return;
      state.sessions=[created];if($('#prompt'))drafts.set(state.session,$('#prompt').value);state.session=created.id;if($('#prompt'))$('#prompt').value='';state.after=0;resetBrowser();
    }
    if(!settings)await loadTaskChat(state,signal());if(stamp!==generation)return;
    document.title=settings?t("Impostazioni — Svolo"):t("Workspace — Svolo"); app.innerHTML=view.shell(state,settings);
    bindShell(); void health();
    if(settings)await renderSettings(hash.split('/')[1]||'providers',stamp);
    else { bindChat(); $('#prompt').value=drafts.get(state.session)||''; await poll(); if(stamp!==generation)return; timer=setInterval(()=>void poll(),1100); frameTimer=setInterval(()=>{if(!document.hidden && browserOnScreen())void refreshView(false);},800); }
  } catch(failure) { if(failure.name==='AbortError')return; if(stamp!==generation)return; app.innerHTML=view.shell(state,true);bindShell();$('#settings-content').innerHTML=`<div class="empty-list"><p>${e(t(failure.message))}</p><button class="btn secondary" id="retry-route">${e(t("Riprova"))}</button></div>`;bind('#retry-route','click',route);error(failure); }
}
function renderLogin() {
  document.title=t("Accedi — Svolo"); app.innerHTML=view.login();
  bind('#toggle-password','click',()=>{const input=$('[name="password"]');input.type=input.type==='password'?'text':'password';$('#toggle-password').setAttribute('aria-label',input.type==='password'?t("Mostra password"):t("Nascondi password"));});
  bind('#login-form','submit',async ev=>{
    ev.preventDefault(); const form=ev.currentTarget, button=form.querySelector('[type="submit"]');button.disabled=true;$('#login-error').hidden=true;
    try { const data=new FormData(form); me(await post('/api/login',{username:data.get('username').trim(),password:data.get('password')}));form.reset();location.hash='#workspace'; }
    catch(failure){$('#login-error').textContent=t(failure.message);$('#login-error').hidden=false;} finally{button.disabled=false;}
  });
}
function bindShell() {
  const languageButton=document.querySelector('[data-action=toggle-language]');if(languageButton)languageButton.disabled=languageSwitching;
  bind('#toggle-sidebar','click',toggleSidebar);
  bind('#mobile-menu','click',()=>$('.workspace').classList.toggle('sidebar-open'));
  bind('#new-session','click',newSession);
  bind('#session-list','click',ev=>{const action=ev.target.closest('[data-chat-action]');if(action){ev.preventDefault();return chatAction(action.dataset.chatAction,action.dataset.chatId);}const button=ev.target.closest('[data-session]');if(button)selectSession(button.dataset.session);});
  bind('.workspace','click',ev=>{if(ev.target.closest('.side-nav,.profile-button'))$('.workspace')?.classList.remove('sidebar-open');});
}
async function health() {
  try {const data=await core('/v1/health',{...signal(),timeout:8000});state.health=data?.ok===true;}
  catch{state.health=false;}
  const label=$('#service-health');if(!label)return;label.classList.toggle('ready',state.health);label.innerHTML=`<span class="dot"></span>${state.health?t("Core connesso"):t("Core non disponibile")}`;
}
function showModal(title, description, contents, submit) {
  modal.innerHTML=`<div class="modal-heading"><div><h2>${e(title)}</h2><p>${e(description)}</p></div><button class="icon-button" id="modal-close" aria-label="${e(t("Chiudi"))}">${icon('close',16)}</button></div><form id="modal-form">${contents}<div id="modal-error" class="form-error" role="alert" hidden></div><div class="modal-footer"><button class="btn secondary" id="modal-cancel" type="button">${e(t("Annulla"))}</button><button class="btn primary" type="submit">${e(t("Salva"))} ${icon('check',15)}</button></div></form>`;
  bind('#modal-close','click',()=>modal.close());bind('#modal-cancel','click',()=>modal.close());
  bind('#modal-form','submit',async ev=>{ev.preventDefault();const button=ev.currentTarget.querySelector('[type="submit"]');button.disabled=true;$('#modal-error').hidden=true;try{await submit(new FormData(ev.currentTarget));modal.close();}catch(failure){$('#modal-error').textContent=failure.message;$('#modal-error').hidden=false;}finally{button.disabled=false;}});
  modal.showModal();
}
function newSession() {
  showModal(t("Una nuova direzione."),t("Dai un nome alla tua sessione di lavoro."),`<label class="form-field">${e(t("Nome della sessione"))}<input name="name" required maxlength="100" placeholder="${e(t("Ad esempio: ricerca per il prossimo progetto"))}" autocomplete="off"></label>`,async data=>{
    const created=await post('/api/sessions',{name:data.get('name').trim()},signal());if($('#prompt'))drafts.set(state.session,$('#prompt').value);state.session=created.id;if($('#prompt'))$('#prompt').value='';state.after=0;resetBrowser();state.liveText.clear();location.hash='#workspace';if(state.page==='workspace')await route();tell(t("La nuova sessione è pronta."));
  });
}
async function selectSession(id) {
  if($('#prompt'))drafts.set(state.session,$('#prompt').value);
  if(!state.sessions.some(s=>s.id===id))return;
  state.session=id; if($('#prompt'))$('#prompt').value=drafts.get(id)||'';resetBrowser();state.after=0;state.liveText.clear();state.runs=[];state.approvals=[];
  if(location.hash!=='#workspace')location.hash='#workspace';else await route();
}
async function chatAction(action,id) {
  const session=state.sessions.find(s=>s.id===id);if(!session)return;
  document.querySelectorAll('.session-menu[open]').forEach(menu=>menu.open=false);
  const path='/api/sessions?id='+encodeURIComponent(id);
  if(action==='pin'){
    await api(path,{method:'PATCH',body:{pinned:!session.pinned},...signal()});
    session.pinned=!session.pinned;$('#session-list').innerHTML=view.sessions(state);return;
  }
  if(action==='rename'){
    showModal(t('Rename chat'),t('Choose a name for this conversation.'),`<label class="form-field">${e(t("Chat name"))}<input name="name" required maxlength="100" value="${e(session.name)}" autocomplete="off"></label>`,async data=>{
      const updated=await api(path,{method:'PATCH',body:{name:data.get('name').trim()},...signal()});
      Object.assign(session,updated);$('#session-list').innerHTML=view.sessions(state);
      if(state.session===id&&!state.page.startsWith('settings'))$('.breadcrumbs strong').textContent=updated.name;
      tell(t('Chat renamed.'));
    });return;
  }
  if(action==='delete'){
    showModal(t('Delete chat?'),t('This removes its conversation history, browser profile and saved screenshots. Your workspace files are kept.'),`<p class="delete-chat-name">${e(session.name)}</p>`,async()=>{
      await api(path,{method:'DELETE',...signal()});
      state.sessions=state.sessions.filter(s=>s.id!==id);drafts.delete(id);
      state.liveText.clear();state.submitted.clear();
      if(state.session===id){state.session=state.sessions[0]?.id||'';if($('#prompt'))$('#prompt').value=drafts.get(state.session)||'';state.after=0;resetBrowser();state.runs=[];state.approvals=[];await route();}
      else $('#session-list').innerHTML=view.sessions(state);
      tell(t('Chat deleted.'));
    });
    const button=$('#modal-form [type="submit"]');button.textContent=t('Delete');button.className=t("btn danger");
  }
}
function toggleSidebar() {
  state.sidebarCollapsed=!state.sidebarCollapsed;
  $('.workspace').classList.toggle('sidebar-collapsed',state.sidebarCollapsed);
  const button=$('#toggle-sidebar'),label=t(state.sidebarCollapsed?'Expand sidebar':'Collapse sidebar');
  button.setAttribute('aria-expanded',String(!state.sidebarCollapsed));button.setAttribute('aria-label',label);button.title=label;
  document.querySelectorAll('.session-menu[open]').forEach(menu=>menu.open=false);
  try{localStorage.setItem('svolo:sidebar-collapsed',String(state.sidebarCollapsed));}catch{}
}
function toggleChat() {
  state.chatCollapsed=!state.chatCollapsed;
  $('#chat-layout').classList.toggle('chat-collapsed',state.chatCollapsed);
  $('#chat-panel').inert=state.chatCollapsed&&window.innerWidth>900;
  $('#toggle-chat').setAttribute('aria-expanded',String(!state.chatCollapsed));
  $('#expand-chat').hidden=!state.chatCollapsed;
  (state.chatCollapsed?$('#expand-chat'):$('#toggle-chat')).focus();
  try{localStorage.setItem('svolo:chat-collapsed',String(state.chatCollapsed));}catch{}
}
function bindChat() {
  bindTasks();
  bind('#toggle-chat','click',toggleChat);
  bind('#expand-chat','click',toggleChat);
  bind('#prompt-form','submit',sendPrompt);
  bind('#prompt','keydown',ev=>{if(ev.key==='Enter'&&!ev.shiftKey&&!ev.isComposing){ev.preventDefault();$('#prompt-form').requestSubmit();}});
  bind('#model-select','change',ev=>chooseModel(JSON.parse(ev.target.value)));
  bind('#messages','click',ev=>{const button=ev.target.closest('[data-prompt]');if(button){$('#prompt').value=button.dataset.prompt;$('#prompt').focus();}});
  bind('#control-button','click',async()=>{
    if(state.controlBusy)return;
    const sid=requireSession(),stamp=generation;state.controlBusy=true;renderControl();
    try{const control=await mutations('/v1/control',{session:sid,owner:state.owner==='agent'?'human':'agent'});if(stamp!==generation||sid!==state.session)return;if(!applyControl(control))throw new Error(t("Stato del controllo non valido. Riprova."));tell(state.owner==='agent'?t("Il controllo è passato all’agente."):t("Hai ripreso il controllo del browser."));}
    finally{if(stamp===generation){state.controlBusy=false;renderControl();}}
  });
  bind('#toggle-browser','click',()=>{state.browserVisible=!state.browserVisible;$('#chat-layout').classList.toggle('show-browser',state.browserVisible);$('#toggle-browser').innerHTML=`${icon(state.browserVisible?'chat':'globe',15)} ${state.browserVisible?'Chat':'Browser'}`;if(state.browserVisible)void refreshView(false);});
  bind('#address-form','submit',async ev=>{ev.preventDefault();await navigateBrowser(false);});
  bind('#new-tab','click',()=>navigateBrowser(true));
  bind('#refresh-view','click',()=>refreshView(true));
  bind('#viewport-select','change',async ev=>{const select=ev.currentTarget,args=viewportArguments(select.value);select.disabled=true;try{await manual('viewport',args);await refreshView(true);}finally{if(select.isConnected)select.disabled=false;}});
  bind('#browser-tabs','click',async ev=>{const button=ev.target.closest('[data-tab]');if(!button||!state.tabs.some(tab=>tab.id===button.dataset.tab))return;state.followAgent=false;setViewedTab(button.dataset.tab);renderTabs();await refreshView(true);});
  bind('#follow-agent','click',async()=>{state.followAgent=true;await refreshTabs();await refreshView(true);});
  bind('#browser-input','submit',async ev=>{ev.preventDefault();const text=$('#browser-text').value;if(!text)return;await input({kind:'text',text});$('#browser-text').value='';});
  bind('#browser-enter','click',()=>input({kind:'key',key:'Enter'}));
  bind('#browser-frame','click',async ev=>{
    if(!state.browserView)return;const rect=ev.currentTarget.getBoundingClientRect(),viewport=state.browserView.viewport;
    await input({kind:'click',x:(ev.clientX-rect.left)/rect.width*(viewport?.width||ev.currentTarget.naturalWidth),y:(ev.clientY-rect.top)/rect.height*(viewport?.height||ev.currentTarget.naturalHeight)});
  });
  bind('#browser-frame','keydown',ev=>{
    if(!state.browserView||ev.isComposing)return;ev.preventDefault();const modifiers=[ev.ctrlKey?'Ctrl':null,ev.altKey?'Alt':null,ev.metaKey?'Meta':null,ev.shiftKey&&ev.key.length>1?'Shift':null].filter(Boolean);
    return ev.key.length===1&&!modifiers.length?input({kind:'text',text:ev.key}):input({kind:'key',key:[...modifiers,ev.key===' '?'Space':ev.key].join('+')});
  });
  let wheelTimer, wheelY = 0, wheelX = 0;
  const frame=$('#browser-frame');
  frame.addEventListener('load',()=>{
    const select=$('#viewport-select');if(!select||!frame.naturalWidth)return;
    const preset=VIEWPORT_PRESETS.find(value=>value.width===frame.naturalWidth&&value.height===frame.naturalHeight);
    let custom=select.querySelector('[data-custom-viewport]');
    if(preset){custom?.remove();select.value=preset.id;}
    else{if(!custom){custom=document.createElement('option');custom.value='';custom.disabled=true;custom.dataset.customViewport='true';select.prepend(custom);}custom.textContent=`${e(t("Personalizzata ·"))} ${frame.naturalWidth} ${e(t("×"))} ${frame.naturalHeight}`;select.value='';}
  },{signal:abort.signal});
  frame.addEventListener('wheel',ev=>{
    if(!state.browserView)return;ev.preventDefault();wheelY+=ev.deltaY;wheelX+=ev.deltaX;clearTimeout(wheelTimer);
    const rect=frame.getBoundingClientRect(),viewport=state.browserView.viewport,x=(ev.clientX-rect.left)/rect.width*(viewport?.width||frame.naturalWidth),y=(ev.clientY-rect.top)/rect.height*(viewport?.height||frame.naturalHeight);
    const stamp=generation,observedTab=state.tab;
    wheelTimer=setTimeout(()=>{const deltaY=wheelY,deltaX=wheelX;wheelY=0;wheelX=0;if(stamp!==generation||observedTab!==state.tab)return;void input({kind:'wheel',x,y,deltaY,deltaX}).catch(error);},100);
  },{passive:false,signal:abort.signal});
  bind('#save-screenshot','click',async()=>{const shot=await manual('screenshot',{save:true});if(!validImage(shot))throw new Error(t("Immagine non disponibile."));const bytes=Uint8Array.from(atob(shot.data),c=>c.charCodeAt(0));const url=URL.createObjectURL(new Blob([bytes],{type:shot.mimeType}));const link=document.createElement('a');link.href=url;link.download='svolo-browser.png';link.click();setTimeout(()=>URL.revokeObjectURL(url),1000);tell(t("L’immagine della pagina è stata salvata."));});
  bind('#approvals','click',async ev=>{const button=ev.target.closest('[data-approval]');if(!button)return;button.disabled=true;try{await mutations('/v1/approvals',{id:button.dataset.approval,approved:button.dataset.answer==='yes'});await poll();}finally{if(button.isConnected)button.disabled=false;}});
}
async function chooseModel([provider,model]) {
  const p=state.providers.find(p=>p.id===provider&&p.configured);if(!p||!p.models.some(m=>m.id===model))throw new Error(t("Questo modello non è disponibile."));
  await post('/api/providers/select',{providerId:provider,model},signal());state.provider=provider;state.model=model;
  if($('#model-select'))$('#model-select').innerHTML=view.modelOptions(state);
  if(state.page.startsWith('settings'))await renderSettings('providers',generation);
  tell(`${p.name||p.id} · ${model} ${e(t("selezionato."))}`);
}
async function sendPrompt(ev) {
  ev.preventDefault();if(state.busy||state.uploading)return;
  const prompt=$('#prompt').value.trim();if(!prompt)return;
  const provider=state.providers.find(p=>p.id===state.provider&&p.configured);if(!provider)throw new Error(t("Collega un provider nelle impostazioni per avviare l’agente."));
  requireSession();state.busy=true;$('#send-button').disabled=true;
  try {const run=await mutations('/v1/runs',{session:state.session,provider:state.provider,prompt,attachments:[...(state.selectedDocuments.get(state.session)||[])],autonomy:'ask',maxSteps:20,continue:true});state.submitted.set(run.id,prompt);state.liveText.set(run.id,'');$('#prompt').value='';drafts.delete(state.session);await poll();}
  finally{state.busy=false;if($('#send-button'))$('#send-button').disabled=false;}
}
async function poll() {
  if(polling || state.page!=='workspace' || !state.session || !state.user)return;
  polling=true;const sid=state.session, stamp=generation;
  try {
    const [runs, approvals, control, events] = await Promise.all([core('/v1/runs',{...signal(),timeout:10000}),core('/v1/approvals',{...signal(),timeout:10000}),core('/v1/control?session='+encodeURIComponent(sid),{...signal(),timeout:10000}),core(`/v1/events?session=${encodeURIComponent(sid)}&after=${state.after}`,{...signal(),timeout:10000})]);
    if(stamp!==generation||state.session!==sid)return;
    state.runs=array(runs).filter(r=>r.session===sid).sort((a,b)=>String(a.started).localeCompare(String(b.started)));
    state.approvals=array(approvals).filter(a=>a.session===sid);applyControl(control);
    for(const event of array(events)){
      state.after=Math.max(state.after,Number(event.seq)||0);
      if(event.type==='message.delta' && event.data?.runId)state.liveText.set(event.data.runId,(state.liveText.get(event.data.runId)||'')+(event.data.text||''));
      if(event.type==='tool.started' && event.data?.runId){const text=state.liveText.get(event.data.runId);if(text&&!text.endsWith('\n\n'))state.liveText.set(event.data.runId,text+'\n\n');}
      if(event.type==='run.started')state.activity=null;
      if(event.type==='tool.started'&&event.data?.runId&&event.data?.callId)state.activity={runId:event.data.runId,callId:event.data.callId,name:event.data.name,tab:event.data.tab||'',status:'running'};
      if(event.type==='tool.finished'&&state.activity?.runId===event.data?.runId&&state.activity?.callId===event.data?.callId)state.activity={...state.activity,status:event.data.ok?'completed':'failed'};
    }
    const activityRun=state.activity&&state.runs.find(run=>run.id===state.activity.runId);
    if(state.activity?.status==='running'&&activityRun&&!['running','waiting_approval'].includes(activityRun.status))state.activity={...state.activity,status:['stopped','interrupted','cancelled'].includes(activityRun.status)?'interrupted':activityRun.status==='failed'?'failed':'completed'};
    renderConversation();renderApprovals();renderControl();renderActivity();
    const learning=await api('/api/task-profiles/proposals?session='+encodeURIComponent(sid),signal());if(stamp===generation&&$('#learning-proposals'))$('#learning-proposals').innerHTML=learningCards(array(learning.proposals),state.taskProfiles);
    const activeTask=state.runs.some(r=>['running','waiting_approval'].includes(r.status));if($('#learn-task'))$('#learn-task').disabled=!state.taskProfile||state.busy||activeTask||state.uploading;if($('#attach-file'))$('#attach-file').disabled=state.busy||activeTask||state.uploading;if($('#task-select'))$('#task-select').disabled=state.busy||activeTask;
    try{await refreshTabs();}catch(failure){if(failure.name==='AbortError')return;const note=$('#browser-error');if(note){note.textContent=t("Il browser non è disponibile. Riprova ad aprire la pagina tra un momento.");note.title=failure.message;note.hidden=false;}}
    state.health=true;const health=$('#service-health');if(health){health.classList.add('ready');health.innerHTML=`<span class="dot"></span>${e(t("Core connesso"))}`;}
  }catch(failure){if(failure.name==='AbortError')return;const label=$('#agent-status');if(label)label.textContent=t("Connessione interrotta");if(state.health){state.health=false;error(failure);}}
  finally{if(stamp===generation)polling=false;}
}
function renderConversation() {
  const area=$('#messages'),scroll=$('#chat-scroll');if(!area||!scroll)return;
  const nearEnd=scroll.scrollHeight-scroll.scrollTop-scroll.clientHeight<100;
  if(!state.runs.length){if(!area.querySelector('.chat-welcome'))area.innerHTML=view.welcome(state);$('#agent-status').textContent=t("Pronto quando vuoi");$('#agent-status').classList.remove('active');$('#send-button').disabled=state.busy;return;}
  const markup=state.runs.slice(-50).map(run=>{
    const prompt=run.prompt||state.submitted.get(run.id)||[...array(run.history)].reverse().find(t=>t.role==='user')?.text||'';
    const active=['running','waiting_approval'].includes(run.status), text=active?(state.liveText.get(run.id)||run.text||''):(run.text||state.liveText.get(run.id)||'');
    const started=new Date(run.started), time=Number.isNaN(started.getTime())?'':new Intl.DateTimeFormat(locale(),{hour:'2-digit',minute:'2-digit',timeZone:'Europe/Rome'}).format(started),failureText=runErrorText(run);
    return `${prompt?`<article class="message user"><div class="message-head"><span class="avatar">${e(state.user.username.slice(0,2).toUpperCase())}</span>${e(t("Tu"))}<time>${e(time)}</time></div><pre class="message-body">${e(prompt)}</pre></article>`:''}<article class="message assistant"><div class="message-head"><img src="/brand/mark.svg" alt="" width="26" height="26">Svolo<span class="muted" style="font-size:9px;font-weight:400">${e(run.model||run.provider)}</span></div>${text?`<div class="message-body markdown-body">${renderMarkdown(text)}</div>`:''}<div class="run-state ${active?'live':''} ${run.error?'error':''}">${e(t(failureText||statusLabels[run.status]||run.status))}${!run.error&&run.steps?` · ${e(run.steps)} ${run.steps===1?t("passaggio"):t("passaggi")}`:''}</div></article>`;
  }).join('');
  if(area.dataset.markup!==markup){area.innerHTML=markup;area.dataset.markup=markup;if(nearEnd)scroll.scrollTop=scroll.scrollHeight;}
  const running=state.runs.findLast(r=>['running','waiting_approval'].includes(r.status));
  $('#agent-status').textContent=running?(running.status==='running'&&state.activity?.runId===running.id&&state.activity.status==='running'?(t(activityLabels[state.activity.name])||t("Uso di uno strumento")):(t(statusLabels[running.status])||t("Al lavoro"))):t("Pronto quando vuoi");$('#agent-status').classList.toggle('active',!!running);
  $('#send-button').disabled=state.busy||!!running;
}
function renderApprovals() {
  const area=$('#approvals');if(!area)return;const key=state.approvals.map(a=>a.id).join(',');if(area.dataset.key===key)return;area.dataset.key=key;
  area.innerHTML=state.approvals.map(a=>`<article class="approval-card"><strong>${e(t("Il tuo consenso per il prossimo passo."))}</strong><span>${e(a.tool)}</span><pre>${e(JSON.stringify(a.arguments,null,2))}</pre><div class="approval-actions"><button class="btn secondary" data-approval="${e(a.id)}" data-answer="no">${e(t("Nega"))}</button><button class="btn primary" data-approval="${e(a.id)}" data-answer="yes">${e(t("Approva questa azione"))} ${icon('check',13)}</button></div></article>`).join('');
}
function renderControl() { const button=$('#control-button');if(button){button.disabled=state.controlBusy;button.innerHTML=`${icon(state.owner==='agent'?'stop':'shield',15)} ${state.owner==='agent'?t("Riprendi il controllo"):t("Hai il controllo")}`;button.title=state.owner==='agent'?t("Interrompi e riprendi il controllo"):t("Passa il controllo all’agente");button.setAttribute('aria-label',button.title);} }
function renderActivity() {
  const label=$('#browser-activity');if(!label)return;
  const activity=state.activity;label.hidden=!activity;if(!activity)return;
  const running=activity.status==='running', outcome=running?'':activity.status==='completed'?t(" · Completata"):activity.status==='interrupted'?t(" · Interrotta"):t(" · Non riuscita");
  label.classList.toggle('active',running);label.classList.toggle('error',['failed','interrupted'].includes(activity.status));label.dataset.tab=activity.tab;label.dataset.status=activity.status;
  label.innerHTML=`${icon(running?'spark':activity.status==='completed'?'check':'stop',13)} <span>${e(t(activityLabels[activity.name])||t("Uso di uno strumento"))}${activity.tab?` · ${activity.tab===state.agentTab?t("Scheda agente"):t("Altra scheda")}`:''}${outcome}</span>`;
}
async function manual(name,args={}) {
  const sid=requireSession(),tab=state.tab,stamp=generation;
  if(['navigate','open-tab','viewport'].includes(name)){await takeHumanControl();state.followAgent=false;renderTabs();}
  if(stamp!==generation||sid!==state.session)throw new DOMException(t("Sessione cambiata"),'AbortError');
  return mutations('/v1/tools/call',{session:sid,name,arguments:{...args,...(tab?{tab}:{})}});
}
async function input(args) {
  const sid=requireSession(),tab=state.tab,stamp=generation;
  if(!tab||!state.browserView)throw new Error(t("Attendi la vista della scheda prima di interagire."));
  await takeHumanControl();if(stamp!==generation||sid!==state.session)return;
  state.followAgent=false;renderTabs();
  await mutations('/v1/input',{session:sid,arguments:{...args,tab}});
  const control=await core('/v1/control?session='+encodeURIComponent(sid),{...signal(),timeout:8000});
  if(stamp!==generation||sid!==state.session)return;applyControl(control);await refreshTabs();await refreshView(true);
}
function validImage(image) { return image && ['image/png','image/jpeg','image/webp'].includes(image.mimeType) && typeof image.data==='string' && /^[A-Za-z0-9+/=\r\n]+$/.test(image.data); }
function browserOnScreen() { return window.innerWidth>900 || state.browserVisible; }
async function refreshView(explicit = false) {
  if(!state.session || state.page!=='workspace' || !state.tab)return;
  if(framing){viewPending=true;return;}
  framing=true;const sid=state.session,tab=state.tab,stamp=generation,controller=new AbortController();viewAbort=controller;
  try {
    const image=await core(`/v1/view?session=${encodeURIComponent(sid)}${tab?'&tab='+encodeURIComponent(tab):''}`,{signal:AbortSignal.any([abort.signal,controller.signal]),timeout:8000});
    if(stamp!==generation||sid!==state.session||tab!==state.tab)return;if(!validImage(image))throw new Error(t("La vista del browser non è disponibile."));
    const capturedTab=typeof image.tab==='string'?image.tab:image.tab?.id;
    if(capturedTab&&capturedTab!==tab)throw new Error(t("La cattura appartiene a una scheda diversa. Aggiorna la vista."));
    state.browserView=image;const frame=$('#browser-frame');if(!frame)return;frame.src=`data:${image.mimeType};base64,${image.data}`;frame.hidden=false;$('#browser-placeholder').hidden=true;$('#browser-error').hidden=true;$('#view-status').textContent=t("Vista aggiornata");
  }catch(failure){if(failure.name==='AbortError')return;if(explicit){const note=$('#browser-error');if(note){note.textContent=failure.message;note.hidden=false;}}}
  finally{if(viewAbort===controller)viewAbort=undefined;if(stamp===generation){framing=false;if(viewPending){viewPending=false;void refreshView(false);}}}
}
function setViewedTab(id) {
  if(state.tab===id)return;
  state.tab=id;state.browserView=null;
  if(framing){viewPending=true;viewAbort?.abort();}
  const frame=$('#browser-frame');if(frame){frame.hidden=true;frame.removeAttribute('src');}
  const placeholder=$('#browser-placeholder');if(placeholder)placeholder.hidden=false;
  if($('#view-status'))$('#view-status').textContent=id?t("Aggiornamento vista…"):t("Nessuna scheda aperta");
}
function renderTabs() {
  const area=$('#browser-tabs');if(!area)return;
  area.innerHTML=state.tabs.map(t=>`<button type="button" class="browser-tab ${t.id===state.tab?'active':''} ${t.id===state.agentTab?'agent-active':''}" data-tab="${e(t.id)}" aria-pressed="${t.id===state.tab}" title="${e(t.url||t.title||t.id)}"><span class="tab-title">${e(t.title||t.url||t("Nuova scheda"))}</span>${t.id===state.agentTab?`<span class="tab-agent-badge">${e(t("Agente"))}</span>`:''}${t.id===state.tab?`<span class="tab-observer-badge">${e(t("Vista"))}</span>`:''}</button>`).join('');
  const selected=state.tabs.find(t=>t.id===state.tab),input=$('#browser-address');if(input&&document.activeElement!==input)input.value=selected?.url||'';
  const follow=$('#follow-agent');if(follow){follow.disabled=state.followAgent;follow.setAttribute('aria-pressed',String(state.followAgent));follow.title=state.followAgent?t("La vista segue automaticamente la scheda dell’agente"):t("Torna a seguire la scheda dell’agente");}
  const label=$('#viewer-follow-label');if(label)label.innerHTML=`${icon(state.followAgent?'spark':'eye',13)} ${state.followAgent?t("Segui la scheda dell’agente"):state.tab===state.agentTab?t("Vista selezionata"):t("Osservi un’altra scheda")}`;
  renderActivity();
}
async function refreshTabs() {
  const sid=state.session,stamp=generation;
  const tabs=array(await core('/v1/browser/tabs?session='+encodeURIComponent(sid),{...signal(),timeout:8000}));if(stamp!==generation||sid!==state.session)return;
  state.tabs=tabs;state.agentTab=tabs.find(t=>t.active)?.id||'';
  if(!state.followAgent&&!tabs.some(t=>t.id===state.tab))state.followAgent=true;
  const before=state.tab;if(state.followAgent)setViewedTab(state.agentTab);
  renderTabs();if(before!==state.tab&&browserOnScreen())void refreshView(false);
}
async function navigateBrowser(newTab) {
  let url=$('#browser-address').value.trim();if(!url)throw new Error(t("Inserisci l’indirizzo della pagina da aprire."));if(!/^https?:\/\//i.test(url))url='https://'+url;
  const parsed=new URL(url);if(!['http:','https:'].includes(parsed.protocol))throw new Error(t("Usa un indirizzo HTTP o HTTPS."));
  $('#view-status').textContent=t("Apertura pagina…");
  try{const result=await manual(newTab?'open-tab':'navigate',{url:parsed.href});if(newTab && result?.id)setViewedTab(result.id);await refreshTabs();await refreshView(true);}
  catch(failure){$('#view-status').textContent=t("Apertura non riuscita");throw failure;}
}

async function renderSettings(page,stamp) {
  const area=$('#settings-content');if(!area)return;
  if(!['providers','mcp','tasks','users','account'].includes(page))page='providers';if(page==='users'&&state.user.role!=='admin')page='account';
  if(page==='mcp'){const result=await api('/api/mcp',signal());state.mcp=array(result?.servers||result);}
  if(page==='users'){const result=await api('/api/users',signal());state.users=array(result?.users||result);}
  if(stamp!==generation)return;area.innerHTML=view.settingsHeader(state,page)+view[page](state);
  bindTasks();
  bind('#add-provider','click',()=>providerForm());bind('#connect-codex','click',()=>codexLogin(state.providers.find(p=>p.kind==='codex-oauth')?.id||'codex'));bind('#add-mcp','click',()=>mcpForm());bind('#add-user','click',userForm);
  bind('#logout','click',async()=>{await post('/api/logout',{},signal());stopScreen();state.user=null;state.sessions=[];state.provider='';state.model='';state.liveText.clear();state.submitted.clear();drafts.clear();state.taskProfiles=[];state.attachments=[];state.selectedDocuments.clear();state.taskProfile='';state.session='';resetBrowser();setCsrf('');location.hash='#login';});
  bind('.provider-list','click',ev=>{const edit=ev.target.closest('[data-edit-provider]'),model=ev.target.closest('[data-model]');if(edit)providerForm(state.providers.find(p=>p.id===edit.dataset.editProvider));if(model)return chooseModel(JSON.parse(model.dataset.model));});
  bind('.mcp-list','click',ev=>{const edit=ev.target.closest('[data-edit-mcp]');if(edit)mcpForm(state.mcp.find(s=>s.id===edit.dataset.editMcp));});
}
function providerForm(provider = {}) {
  if(provider.kind==='codex-oauth'){codexLogin(provider.id);return;}
  if(provider.kind==='antigravity-oauth'){
    showModal(t("Antigravity non collegato."),t("Il login OAuth non è disponibile in questo workspace."),`<p class="muted" style="font-size:12px">${e(t("Puoi collegare un account Google Gemini con una chiave API del tuo progetto."))}</p>`,async()=>{modal.close();queueMicrotask(()=>providerForm({kind:'google-api-key',name:t("Google Gemini")}));});
    $('#modal-form [type="submit"]').textContent=t("Configura Google Gemini");return;
  }
  const editing=!!provider.id,kind=provider.kind||'openai-compatible';
  const options=[['openai-compatible',t("API compatibile OpenAI")],['google-api-key','Google Gemini API'],...(kind==='gemrouter'?[['gemrouter','Gemrouter']]:[])];
  showModal(editing?t("La tua connessione."):t("Collega un provider."),t("Scegli come vuoi lavorare con il tuo agente."),`<div class="form-grid"><label class="form-field">${e(t("Nome"))}<input name="name" required value="${e(provider.name||'')}" placeholder="${e(t("Il nome del provider"))}" maxlength="80"></label><label class="form-field">${e(t("Identificativo"))}<input name="id" required value="${e(provider.id||'')}" placeholder="${e(t("es. il-mio-provider"))}" pattern="[A-Za-z0-9_-]+" maxlength="80" ${editing?'readonly':''}></label><label class="form-field full">${e(t("Tipo di connessione"))}<select name="kind" ${editing?'disabled':''}>${options.map(([id,label])=>`<option value="${id}" ${id===kind?'selected':''}>${label}</option>`).join('')}</select>${editing?`<input type="hidden" name="kind" value="${e(kind)}">`:''}</label><label class="form-field full">${e(t("Endpoint API"))}<input name="baseUrl" type="url" required value="${e(provider.baseUrl||provider.baseURL||(kind==='google-api-key'?'https://generativelanguage.googleapis.com/v1beta':''))}" placeholder="https://…/v1"><small>${e(t("L’indirizzo HTTPS del servizio che vuoi collegare."))}</small></label><label class="form-field full">${e(t("Chiave API"))}<input name="apiKey" type="password" autocomplete="new-password" placeholder="${editing&&provider.configured?t("Lascia vuoto per conservare la chiave attuale"):t("La chiave del tuo provider")}"><small>${e(t("La chiave viene inviata al server e non viene mostrata nelle impostazioni."))}</small></label><label class="form-field full">${e(t("Modelli disponibili"))}<textarea name="models" required rows="3" placeholder="${e(t("Un identificativo di modello per riga"))}">${e(array(provider.models).map(m=>m.id).join('\n'))}</textarea><small>${e(t("Inserisci gli identificativi esatti disponibili per il tuo account."))}</small></label></div>${editing?`<div class="connection-actions"><button class="btn secondary" type="button" id="refresh-models">${e(t("Aggiorna modelli"))}</button><button class="btn danger" type="button" id="remove-provider">${e(t("Scollega provider"))}</button></div>`:''}`,async data=>{
    const ids=[...new Set(data.get('models').split(/[\n,]/).map(m=>m.trim()).filter(Boolean))];if(!ids.length)throw new Error(t("Inserisci almeno un modello."));
    const endpoint=new URL(data.get('baseUrl'));if(endpoint.protocol!=='https:' && !(editing && state.user.role==='admin' && endpoint.protocol==='http:'))throw new Error(t("L’endpoint del provider deve usare HTTPS."));
    const payload={id:data.get('id').trim(),label:data.get('name').trim(),kind:data.get('kind'),baseURL:endpoint.href.replace(/\/$/,''),models:ids.map(id=>({id,label:id})),model:ids.includes(provider.model||state.model)?(provider.model||state.model):ids[0],vision:provider.vision||false,stream:true};if(data.get('apiKey'))payload.apiKey=data.get('apiKey');
    await post('/api/providers',{provider:payload},signal());await loadWorkspaceData();await renderSettings('providers',generation);tell(t("La connessione è stata salvata."));
  });
  bind('select[name="kind"]','change',ev=>{if(ev.target.value==='google-api-key')$('#modal input[name="baseUrl"]').value='https://generativelanguage.googleapis.com/v1beta';});
  bind('#refresh-models','click',async()=>{await post('/api/providers/'+encodeURIComponent(provider.id)+'/models',{},signal());await loadWorkspaceData();modal.close();await renderSettings('providers',generation);tell(t("Il catalogo modelli è stato aggiornato."));});
  bind('#remove-provider','click',async()=>{await api('/api/providers?id='+encodeURIComponent(provider.id),{method:'DELETE',...signal()});modal.close();await loadWorkspaceData();await renderSettings('providers',generation);tell(t("Il provider è stato scollegato."));});
}
function mcpForm(server = {}) {
  showModal(server.id?t("Il tuo server MCP."):t("Nuovi strumenti per Svolo."),t("Collega un server che sei autorizzato a usare."),`<div class="form-grid"><label class="form-field">${e(t("Nome"))}<input name="name" required maxlength="80" value="${e(server.name||'')}" placeholder="${e(t("Il nome del server"))}"></label><label class="form-field">${e(t("Identificativo"))}<input name="id" required pattern="[A-Za-z0-9_-]+" maxlength="64" value="${e(server.id||'')}" placeholder="${e(t("es. ricerca-web"))}" ${server.id?'readonly':''}></label><label class="form-field full">${e(t("Endpoint HTTPS"))}<input name="url" type="url" required value="${e(server.url||'')}" placeholder="https://…/mcp"><small>${e(t("Sono supportati endpoint HTTPS remoti autorizzati per il tuo account."))}</small></label><label class="form-field full">${e(t("Credenziale del server (facoltativa)"))}<input name="token" type="password" autocomplete="new-password" placeholder="${server.id?t("Lascia vuoto per conservare la credenziale attuale"):t("Credenziale di accesso")}"><small>${e(t("La credenziale viene conservata sul server."))}</small></label></div>${server.id?`<button type="button" class="btn danger" id="remove-mcp">${e(t("Scollega server"))}</button>`:''}`,async data=>{
    const endpoint=data.get('url').trim();if(new URL(endpoint).protocol!=='https:')throw new Error(t("Il server remoto deve usare HTTPS."));
    const payload={id:data.get('id').trim(),name:data.get('name').trim(),url:endpoint};if(data.get('token'))payload.token=data.get('token');
    await post('/api/mcp',{server:payload},signal());await renderSettings('mcp',generation);tell(t("La configurazione MCP è stata salvata."));
  });
  bind('#remove-mcp','click',async()=>{await api('/api/mcp?id='+encodeURIComponent(server.id),{method:'DELETE',...signal()});modal.close();await renderSettings('mcp',generation);tell(t("Il server MCP è stato scollegato."));});
}
async function codexLogin(id='codex') {
  modal.close();
  modal.innerHTML=`<div class="modal-heading"><div><h2>${e(t("Collega il tuo account Codex."))}</h2><p>${e(t("Completa l’accesso nel sito del provider."))}</p></div><button class="icon-button" id="oauth-close" aria-label="${e(t("Chiudi"))}">${icon('close',16)}</button></div><div id="oauth-status" class="oauth-status"><p class="muted">${e(t("Prepariamo un accesso sicuro…"))}</p></div><div class="modal-footer"><button class="btn secondary" id="oauth-cancel">${e(t("Chiudi"))}</button></div>`;
  bind('#oauth-close','click',()=>modal.close());bind('#oauth-cancel','click',()=>modal.close());modal.showModal();
  const controller=new AbortController();let oauthTimer,checking=false,finished=false;
  modal.addEventListener('close',()=>{clearInterval(oauthTimer);controller.abort();},{once:true});
  const callOptions={signal:controller.signal,timeout:15000};
  try {
    const login=await post('/api/providers/'+encodeURIComponent(id)+'/oauth/start',{},callOptions);
    const check=async()=>{
      if(checking||finished||controller.signal.aborted)return;checking=true;
      try {
        const status=await api('/api/providers/'+encodeURIComponent(id)+'/oauth/status?loginId='+encodeURIComponent(login.loginId),callOptions);
        const area=$('#oauth-status');if(!area)return;
        if(status.status==='completed'||status.status==='connected'){
          finished=true;clearInterval(oauthTimer);area.innerHTML=`<div class="settings-note">${icon('check',22)}<p>${e(t("Il tuo account Codex è collegato. Puoi scegliere il modello nelle impostazioni."))}</p></div>`;
          await loadWorkspaceData();await renderSettings('providers',generation);tell(t("Codex è collegato al tuo workspace."));return;
        }
        if(['failed','cancelled'].includes(status.status)){finished=true;clearInterval(oauthTimer);area.innerHTML=`<div class="form-error">${e(status.reason||status.message||t("Accesso non completato. Riprova."))}</div>`;return;}
        const code=status.event?.userCode||status.code,url=status.event?.verificationUri||status.url;
        if(code&&url){const parsed=new URL(url);if(parsed.protocol!=='https:')throw new Error(t("Link di accesso non valido."));area.innerHTML=`<p style="font-size:13px">${e(t("Apri il sito del provider e inserisci questo codice:"))}</p><div class="device-code">${e(code)}</div><a class="btn primary" href="${e(parsed.href)}" target="_blank" rel="noopener noreferrer">${e(t("Apri il sito del provider"))} ${icon('arrow',16)}</a><p class="muted" style="font-size:11px;margin-top:20px">${e(t("Tieni questa finestra aperta. La connessione verrà aggiornata dopo il tuo accesso."))}</p>`;}
      } catch(failure){if(failure.name==='AbortError')return;clearInterval(oauthTimer);const area=$('#oauth-status');if(area)area.innerHTML=`<div class="form-error">${e(failure.message)}</div>`;}
      finally{checking=false;}
    };
    await check();if(!finished&&!controller.signal.aborted)oauthTimer=setInterval(()=>void check(),1500);
  }catch(failure){if(failure.name==='AbortError')return;$('#oauth-status').innerHTML=`<div class="form-error">${e(failure.message)}</div>`;}
}
function userForm() {
  showModal(t("Una nuova persona nel team."),t("Crea un accesso al workspace."),`<label class="form-field">${e(t("Nome utente"))}<input name="username" autocomplete="off" required minlength="3" maxlength="32" pattern="[A-Za-z0-9_.-]+" placeholder="nome.utente"></label><label class="form-field">Password<input name="password" type="password" autocomplete="new-password" required minlength="12" placeholder="${e(t("Almeno 12 caratteri"))}"></label><label class="form-field">Ruolo<select name="role"><option value="user">${e(t("Utente"))}</option><option value="admin">${e(t("Amministratore"))}</option></select><small>${e(t("Gli amministratori possono creare nuovi accessi."))}</small></label>`,async data=>{await post('/api/users',{username:data.get('username').trim(),password:data.get('password'),role:data.get('role')},signal());await renderSettings('users',generation);tell(t("Il nuovo accesso è stato creato."));});
}

window.addEventListener('hashchange',()=>void route());
window.addEventListener('beforeunload',stopScreen);
window.addEventListener('resize',()=>{if($('#chat-panel'))$('#chat-panel').inert=state.chatCollapsed&&window.innerWidth>900;if(state.page==='workspace'&&browserOnScreen())void refreshView(false);});
if(!location.hash && ['/app','/login','/settings'].includes(location.pathname))history.replaceState(null,'',location.pathname+({ '/app':'#workspace','/login':'#login','/settings':'#settings/providers' }[location.pathname]));
try { me(await api('/api/me')); } catch(failure) { if(failure.status!==401 && failure.status!==403)tell(failure.message,true); }
await route();

function bindTasks(){bindTaskUI({state,bind,showModal,signal,tell,error,refresh:async()=>{if(state.page.startsWith('settings'))await renderSettings('tasks',generation);else await route();},learn:async()=>{if(state.busy||state.uploading||!state.taskProfile||state.runs.some(r=>['running','waiting_approval'].includes(r.status)))return;const draft=$('#prompt').value;$('#prompt').value=t('Extract only reusable procedures and field mappings confirmed by tool results in this chat. Use only the selected task profile. Do not include personal case data or credentials, or change the profile goal or instructions. Propose the complete updated knowledge with the task-memory tool; do not claim it is saved until I approve.');await sendPrompt({preventDefault(){}});if($('#prompt'))$('#prompt').value=draft;drafts.set(state.session,draft);}});}
