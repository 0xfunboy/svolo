// Page-scoped semantic observations and element operations for Svolo.
// Runs only in an explicitly registered, untrusted browser page, never in application UI.
(function(op, a) {
  const all = (selector, root = document, seen = new Set()) => {
    if (seen.has(root) || seen.size > 100) return [];
    seen.add(root);
    let out = Array.from(root.querySelectorAll(selector));
    for (const e of root.querySelectorAll('*')) {
      if (e.shadowRoot) out.push(...all(selector, e.shadowRoot, seen));
      if (e.tagName === 'IFRAME') { try { if (e.contentDocument) out.push(...all(selector, e.contentDocument, seen)); } catch (_) {} }
    }
    return out.slice(0, 10000);
  };
  const visible = e => {
    if (!e || !e.isConnected) return false;
    const s = e.ownerDocument.defaultView.getComputedStyle(e), r = e.getBoundingClientRect();
    return s.display !== 'none' && s.visibility !== 'hidden' && s.opacity !== '0' && r.width > 0 && r.height > 0;
  };
  const text = e => (e?.innerText ?? e?.textContent ?? '').replace(/\s+/g,' ').trim();
  const label = e => e.getAttribute('aria-label') || (e.labels && [...e.labels].map(text).join(' ')) || e.getAttribute('placeholder') || e.getAttribute('alt') || text(e) || e.getAttribute('title') || e.name || '';
  const rect = e => {
    const r = e.getBoundingClientRect(); let x=r.x,y=r.y,w=e.ownerDocument.defaultView;
    while(w && w!==window) {try {const f=w.frameElement;if(!f)break;const r=f.getBoundingClientRect();x+=r.x;y+=r.y;w=w.parent;}catch(_){break;}}
    return {x,y,width:r.width,height:r.height};
  };
  const find = target => {
    if (typeof target !== 'string' || !target) throw new Error('target_required');
    if (target.startsWith('@')) {
      const s=globalThis.__svoloBrowserV1;if(!s||!target.startsWith('@'+s.id+':'))throw new Error('target_stale: take a new snapshot');
      const e=s.refs.get(target);if(!e?.isConnected)throw new Error('target_stale: element removed');return e;
    }
    let found;
    if(target.startsWith('css:'))found=all(target.slice(4));
    else if(target.startsWith('role:')){const q=target.slice(5),i=q.indexOf('|'),role=i<0?q:q.slice(0,i),name=i<0?'':q.slice(i+1);const tags={button:'button,input[type="submit"],input[type="button"]',link:'a[href]',textbox:'input:not([type]),input[type="text"],textarea',checkbox:'input[type="checkbox"]'};found=all(`[role="${CSS.escape(role)}"]${tags[role]?','+tags[role]:''}`).filter(e=>!name||label(e)===name);}
    else {const q=target.replace(/^text:/,'');found=all('button,a,input,select,textarea,[role],[aria-label],label,h1,h2,h3,p,span,div,img').filter(e=>label(e)===q).filter(e=>!Array.from(e.children).some(c=>label(c)===q));}
    found=found.filter(visible);if(found.length!==1)throw new Error(found.length?'target_ambiguous: use a precise selector or snapshot ref':'target_not_found');return found[0];
  };
  const safeHTML = e => { const c=e.cloneNode(true); for (const n of [c,...c.querySelectorAll('input[type="password"]')]) if(n.type==='password')n.removeAttribute('value'); return c.outerHTML.slice(0,12000); };
  const info = e => ({tag:e.tagName.toLowerCase(),text:text(e).slice(0,6000),label:label(e).slice(0,500),html:safeHTML(e),visible:visible(e),box:rect(e),type:e.type,name:e.name,disabled:!!e.disabled,checked:!!e.checked,value:e.type==='password'?'[redacted]':e.value,attributes:Object.fromEntries([...e.attributes].filter(x=>!/^on/i.test(x.name)&&!['value'].includes(x.name)).map(x=>[x.name,x.value.slice(0,500)]))});
  const requireVisible=e=>{if(!visible(e))throw new Error('target_not_visible');return e};
  switch(op){
    case 'state':return {url:location.href,title:document.title,readyState:document.readyState};
    case 'snapshot-interactive':case 'find-interactive': {
      const s={id:Date.now().toString(36)+Math.random().toString(36).slice(2,9),refs:new Map()};globalThis.__svoloBrowserV1=s;
      let elements=all('a[href],button,input,textarea,select,summary,[role="button"],[role="link"],[role="checkbox"],[contenteditable="true"],[tabindex]').filter(visible);
      if(a.query)elements=elements.filter(e=>label(e).toLowerCase().includes(String(a.query).toLowerCase()));
      const limit=Math.max(1,Math.min(500,a.limit||100)),offset=Math.max(0,a.offset||0);
      return {url:location.href,title:document.title,document:s.id,total:elements.length,elements:elements.slice(offset,offset+limit).map((e,i)=>{const ref='@'+s.id+':'+(i+offset+1);s.refs.set(ref,e);return {ref,tag:e.tagName.toLowerCase(),role:e.getAttribute('role'),label:label(e).slice(0,300),type:e.type,disabled:!!e.disabled,checked:!!e.checked,box:rect(e)}})};
    }
    case 'read-page':return {url:location.href,title:document.title,text:text(document.querySelector('main,article,[role="main"]')||document.body).slice(0,60000),headings:all('h1,h2,h3').map(e=>({level:e.tagName,text:text(e)})).slice(0,100)};
    case 'inspect-inputs':return all('input,select,textarea,[contenteditable="true"]').filter(visible).map(info).slice(0,200);
    case 'inspect-elements':case 'query-selector':return all(a.css||'*').filter(e=>op==='query-selector'||visible(e)).slice(0,Math.min(a.limit||100,500)).map(info);
    case 'element-info':case 'get-element':return info(find(a.target));
    case 'inspect-links':return all('a[href]').filter(visible).slice(0,1000).map(e=>({text:label(e).slice(0,500),url:e.href,box:rect(e)}));
    case 'inspect-images':return all('img').slice(0,500).map(e=>({alt:e.alt,src:e.currentSrc||e.src,complete:e.complete,naturalWidth:e.naturalWidth,naturalHeight:e.naturalHeight,visible:visible(e),box:rect(e)}));
    case 'locate':{const e=requireVisible(find(a.target));e.scrollIntoView({block:'center',inline:'center',behavior:'instant'});if(e.disabled)throw new Error('target_disabled');const r=rect(e);return {x:r.x+r.width/2,y:r.y+r.height/2,box:r};}
    case 'focus':{const e=requireVisible(find(a.target));e.scrollIntoView({block:'center',behavior:'instant'});e.focus();if(e.disabled||e.readOnly)throw new Error('target_not_editable');if(a.clear){if(e.isContentEditable){e.textContent='';}else if('value'in e){const proto=e.tagName==='TEXTAREA'?e.ownerDocument.defaultView.HTMLTextAreaElement.prototype:e.ownerDocument.defaultView.HTMLInputElement.prototype;const set=Object.getOwnPropertyDescriptor(proto,'value')?.set;if(set)set.call(e,'');else e.value='';}else throw new Error('target_not_editable');e.dispatchEvent(new Event('input',{bubbles:true}));}return {focused:true};}
    case 'select':{const e=requireVisible(find(a.target));if(e.tagName!=='SELECT')throw new Error('target_not_select');const opts=[...e.options].filter(o=>o.value===a.value||o.text===a.value);if(opts.length!==1)throw new Error('option_not_found_or_ambiguous');e.value=opts[0].value;e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));return {value:e.value};}
    case 'check-state':{const e=requireVisible(find(a.target));if(e.type!=='checkbox'&&e.type!=='radio'&&!['checkbox','radio','switch'].includes(e.getAttribute('role')))throw new Error('target_not_checkable');return {checked:!!e.checked||e.getAttribute('aria-checked')==='true'};}
    case 'scroll':{const d=a.destination||'down';if(['up','down'].includes(d))window.scrollBy(0,(d==='up'?-1:1)*innerHeight*.8);else if(d==='top')window.scrollTo(0,0);else if(d==='bottom')window.scrollTo(0,document.documentElement.scrollHeight);else find(d).scrollIntoView({block:'center',behavior:'instant'});return {x:scrollX,y:scrollY};}
    case 'href':{const e=find(a.target);const url=e.href||e.currentSrc||e.src;if(!url)throw new Error('target_has_no_url');return url;}
    case 'assert-visible':return info(requireVisible(find(a.target)));
    case 'assert-image-ready':{const e=requireVisible(find(a.target));if(e.tagName!=='IMG'||!e.complete||!e.naturalWidth||!e.naturalHeight)throw new Error('condition_failed: image not ready');return {...info(e),complete:true,naturalWidth:e.naturalWidth,naturalHeight:e.naturalHeight};}
    case 'assert-text':{const e=find(a.target),actual=text(e),expected=String(a.text??a.expected??'');if(a.match==='exact'?actual!==expected:!actual.includes(expected))throw new Error('condition_failed: text mismatch');return {actual,expected,verified:true};}
    case 'assert-url':case 'assert-title':{const actual=op==='assert-url'?location.href:document.title,expected=String(a.expected??'');if(a.match==='contains'?!actual.includes(expected):actual!==expected)throw new Error('condition_failed: '+actual);return {actual,expected,verified:true};}
    case 'condition': {
      const c=a.condition,v=String(a.value??a.target??'');
      if(c==='url')return location.href.includes(v);if(c==='title')return document.title.includes(v);if(c==='text')return text(document.body).includes(v);
      let e;try{e=find(v.startsWith('css:')||v.startsWith('@')||v.startsWith('text:')?v:'css:'+v)}catch(err){if(String(err).includes('target_stale'))throw err;return c==='hidden'||c==='gone'}
      if(c==='hidden'||c==='gone')return !visible(e);if(c==='image-ready')return visible(e)&&e.complete&&e.naturalWidth>0&&e.naturalHeight>0;
      if(c==='exists'||c==='css')return !!e;if(c==='visible')return visible(e);throw new Error('unsupported_condition');
    }
    case 'highlight': {document.getElementById('__svolo_highlight')?.remove();const e=find(a.target),r=rect(e);const h=document.createElement('div');h.id='__svolo_highlight';Object.assign(h.style,{position:'fixed',left:r.x+'px',top:r.y+'px',width:r.width+'px',height:r.height+'px',outline:'3px solid #ccff00',pointerEvents:'none',zIndex:'2147483647',boxSizing:'border-box'});h.textContent=String(a.label||'');document.documentElement.appendChild(h);return {highlighted:true,box:r};}
    case 'clear-highlight':document.getElementById('__svolo_highlight')?.remove();return {cleared:true};
    case 'upload-object':return find(a.target);
    default:throw new Error('unknown_page_operation: '+op);
  }
})
