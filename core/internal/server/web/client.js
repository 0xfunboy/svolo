/** Shared operational UI. Electron calls an allow-listed main-process IPC broker;
 * standalone mode calls the same API over authenticated loopback HTTP.
 * Model text, tool results and page content are ALWAYS rendered with textContent.
 */
export function mountCoreConsole(root, transport, options = {}) {
  root.classList.add('agent-core-console');
  root.innerHTML = `
    <header class="ac-top"><div><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256" class="ac-mark" role="img" aria-label="Svolo"><defs><linearGradient id="upper" x1="0" y1="1" x2="1" y2="0"><stop stop-color="#064b5c"/><stop offset=".6" stop-color="#05c98c"/><stop offset="1" stop-color="#0aff81"/></linearGradient><linearGradient id="lower" x1="0" y1="1" x2="1" y2="0"><stop stop-color="#10212e"/><stop offset="1" stop-color="#059f7b"/></linearGradient></defs><g transform="translate(0 33)"><path d="M 11 126 C 27 79 47 64 88 51 L 207 12 C 193 53 171 74 132 88 Z" fill="url(#upper)"/><path d="M 56 175 C 79 130 97 112 132 99 L 195 73 C 180 117 151 141 111 154 Z" fill="url(#lower)"/><circle cx="231" cy="43" r="13" fill="#10212e"/></g></svg><strong>Svolo <span>Workspace</span></strong><small>GO / CHROMIUM</small></div><span class="ac-health">Connecting…</span></header>
    <div class="ac-context"><label>Host<select class="ac-host"><option value="">Local</option></select></label><label>Workspace / session<select class="ac-session"></select></label><button class="ac-new">+ Session</button><span class="ac-owner">Human control</span><button class="ac-take ac-danger">Take control / Stop</button><button class="ac-release">Return to agent</button></div>
    <nav class="ac-nav"><button data-page="chat" class="ac-selected">Agent</button><button data-page="browser">Browser</button><button data-page="tools">Tools</button><button data-page="artifacts">Artifacts</button><button data-page="hosts">SSH hosts</button><button data-page="files">File transfers</button><button data-page="security">Credentials & desktop</button><button data-page="config">Configuration</button><button data-page="tokens">MCP access</button><button data-page="events">Events</button></nav>
    <div class="ac-notice" role="status"></div>
    <section class="ac-session-editor" hidden><h2>New session on selected host</h2><label>Session ID<input class="ac-session-id" placeholder="workspace-1"></label><label>Absolute workspace path on selected host<input class="ac-workspace-path" placeholder="Optional: /home/user/project or C:\\work\\project"></label><button class="ac-choose-folder">Choose local folder</button><button class="ac-session-save ac-primary">Create session</button><button class="ac-session-cancel">Cancel</button></section>
    <div class="ac-body">
      <section data-panel="chat"><div class="ac-agent-bar"><label>Model provider<select class="ac-provider"></select></label><label>Autonomy<select class="ac-autonomy"><option value="ask">Ask before changes</option><option value="browser">Browser actions; ask for code/files</option></select></label><label>Steps<input class="ac-steps" type="number" min="1" max="100" value="20"></label><label class="ac-check"><input class="ac-continue" type="checkbox" checked>Continue conversation</label></div><div class="ac-runs"></div><div class="ac-approvals"></div><div class="ac-composer"><textarea class="ac-prompt" rows="4" placeholder="Describe an outcome. The agent can use the same browser you use, plus authorized workspace and MCP tools."></textarea><div><label class="ac-image-label">Attach images<input class="ac-images" type="file" accept="image/png,image/jpeg,image/webp" multiple></label><span class="ac-image-count"></span><button class="ac-run ac-primary">Run agent</button></div></div><p class="ac-hint">Provider credentials use environment variables or encrypted vault references on the selected host. Page text and images do not grant permissions.</p></section>
      <section data-panel="browser" hidden><div class="ac-browser-bar"><input class="ac-url" placeholder="https://example.com"><button class="ac-go">Navigate</button><button class="ac-new-tab">New tab</button><button class="ac-native">Show native browser</button><button class="ac-refresh-view">Refresh</button><label class="ac-check"><input class="ac-live" type="checkbox">Live view</label></div><div class="ac-tabs"></div><div class="ac-browser-stage"><img class="ac-frame" alt="The actual owned browser session; no duplicate browser is opened." tabindex="0"><p class="ac-frame-note">Open a page or choose a browser tab. Remote view is sampled page pixels, not a complete remote desktop.</p></div><div class="ac-browser-bar"><input class="ac-input-text" placeholder="Type text into focused element"><button class="ac-type">Type</button><button class="ac-key" data-key="Tab">Tab</button><button class="ac-key" data-key="Enter">Enter</button><button class="ac-key" data-key="Escape">Escape</button><button class="ac-shot">Save screenshot</button></div><p class="ac-hint">Clicking or typing in the viewer takes human control. Watch mode does not interrupt the agent. The native Electron browser retains ordinary browsing, annotations and responsive controls.</p></section>
      <section data-panel="tools" hidden><div class="ac-grid"><label>Operation<select class="ac-tool"></select></label><button class="ac-invoke ac-primary">Execute as user</button></div><p class="ac-tool-description"></p><details><summary>Argument schema</summary><pre class="ac-schema"></pre></details><label>Arguments (JSON object)<textarea class="ac-args" rows="8">{}</textarea></label><pre class="ac-tool-output"></pre><p class="ac-hint">These are explicit manual operations. Workspace execution is disabled until allowExec is enabled for that session; it is not an OS sandbox.</p></section>
      <section data-panel="artifacts" hidden><h2>Verified artifacts</h2><div class="ac-artifacts"></div><p class="ac-hint">Files are checked against their recorded size and SHA-256 before download. Integrity is not a claim about semantic correctness.</p></section>
      <section data-panel="hosts" hidden><h2>Multi-host SSH</h2><p>SSH uses your existing OpenSSH configuration and verified host keys. Add an alias, then install its daemon explicitly. Closing this desktop does not ask the remote daemon to stop. Windows native input requires an interactive login.</p><div class="ac-grid"><label>Host ID<input class="ac-host-id" placeholder="development"></label><label>SSH config alias<input class="ac-host-alias" placeholder="dev-box"></label><label>Remote loopback port<input class="ac-host-port" type="number" value="7331" min="1" max="65535"></label><button class="ac-host-add">Add host</button></div><div class="ac-host-list"></div><label>Resolve SSH alias<input class="ac-alias" placeholder="development"></label><button class="ac-resolve">Show effective SSH configuration</button><pre class="ac-ssh-output"></pre></section>
      <section data-panel="config" hidden><h2>Host configuration</h2><p>This editor configures providers, workspaces and trusted MCP endpoints on the selected host. Use apiKeyEnv/tokenEnv or apiKeyRef/tokenRef references, never raw credentials. Saving does not launch MCP processes.</p><textarea class="ac-config" rows="22" spellcheck="false"></textarea><div class="ac-actions"><button class="ac-reload-config">Reload</button><button class="ac-example">Add provider example</button><button class="ac-save-config ac-primary">Validate & save</button><button class="ac-mcp-refresh">Connect configured MCP servers</button></div><pre class="ac-config-output"></pre></section>
      <section data-panel="files" hidden><h2>Verified, resumable workspace transfers</h2><p>Operations are bound to the host and session selected when they start. Maximum file size: 256 MiB. Existing files require their previous SHA-256; no silent overwrite.</p><label>Workspace-relative path<input class="ac-transfer-path" placeholder="src/example.txt"></label><label>Previous SHA-256 for explicit replacement (optional)<input class="ac-transfer-old" autocomplete="off"></label><label>Upload file<input class="ac-transfer-file" type="file"></label><label>Resume upload ID (optional)<input class="ac-transfer-id" placeholder="Paste an existing transfer ID"></label><div class="ac-actions"><button class="ac-upload">Upload / resume & verify</button><button class="ac-download">Download & verify</button><button class="ac-transfer-list">Refresh transfers</button></div><pre class="ac-transfer-output"></pre><div class="ac-transfers"></div></section>
      <section data-panel="security" hidden><h2>Credential vault</h2><p>The desktop uses OS-backed encryption for the vault key when available. Headless hosts require an externally held 64-character hexadecimal master key. Losing that key makes the encrypted credentials unrecoverable. Secret values are never returned by the list API.</p><button class="ac-vault-refresh">Refresh security state</button><pre class="ac-vault-status"></pre><label>External vault key<input class="ac-vault-key" type="password" autocomplete="off"></label><div class="ac-actions"><button class="ac-vault-unlock">Unlock</button><button class="ac-vault-lock">Lock</button></div><label>Credential reference<input class="ac-secret-ref" placeholder="provider-main"></label><label>Secret value<input class="ac-secret-value" type="password" autocomplete="off"></label><button class="ac-secret-save">Store encrypted</button><div class="ac-secret-list"></div><h2>Native desktop control</h2><p>Off by default. Every app needs separate approval. Foreground input additionally requires your explicit setting and an already focused app. Availability depends on the operating system and its accessibility permissions.</p><label class="ac-check"><input class="ac-computer-enabled" type="checkbox">Enable native Computer Use for the Go agent</label><label class="ac-check"><input class="ac-computer-foreground" type="checkbox">Allow foreground keyboard/mouse operations</label><button class="ac-computer-save">Apply desktop policy</button><button class="ac-computer-caps">Inspect native capabilities</button><pre class="ac-computer-output"></pre></section>
      <section data-panel="tokens" hidden><h2>Scoped MCP credentials</h2><p>Mint a tools-only credential for this session. It cannot edit configuration, approve itself or access other sessions. Mutations are approved here; return browser control to the agent before execution.</p><input class="ac-token-label" placeholder="Client label"><button class="ac-mint ac-primary">Create scoped token</button><pre class="ac-token-output"></pre><div class="ac-tokens"></div></section>
      <section data-panel="events" hidden><h2>Durable execution events</h2><p>Newest events for the selected workspace. An interrupted operation is not automatically replayed.</p><pre class="ac-events"></pre></section>
    </div>
    <footer><span>Development build · production qualification pending</span><span class="ac-footer-status">No model selected</span></footer>`;
  const $ = s => root.querySelector(s), $$ = s => [...root.querySelectorAll(s)];
  const state = { alive: true, host: '', session: options.initialSession || '', config: null, local: null, tools: [], page: 'chat', after: 0, events: [], view: null, images: [], busy: false, polling: false, tab: '', lastView: 0 };
  const disposers = [];
  const on = (selector, event, callback) => { for (const element of $$(selector)) { const wrapped = e => { try { Promise.resolve(callback(e)).catch(error); } catch (failure) { error(failure); } }; element.addEventListener(event, wrapped); disposers.push(() => element.removeEventListener(event, wrapped)); } };
  const tell = (message, bad = false) => { $('.ac-notice').textContent = message; $('.ac-notice').classList.toggle('ac-error', bad); };
  const error = e => { if (state.alive) tell(e instanceof Error ? e.message : String(e), true); };
  const api = (path, method = 'GET', body, local = false) => state.host && !local ? transport('/v1/remote', 'POST', { host: state.host, method, path, body }) : transport(path, method, body);
  const requireSession = () => { if (!state.session) throw new Error('Create or select a workspace session first.'); return state.session; };
  const node = (tag, text, className) => { const n = document.createElement(tag); if (text !== undefined) n.textContent = text; if (className) n.className = className; return n; };
  const button = (text, action, className) => { const b = node('button', text, className); b.addEventListener('click', () => Promise.resolve().then(action).catch(error)); return b; };
  const pretty = v => JSON.stringify(v, null, 2);
  const setOptions = (select, entries, selected) => { select.replaceChildren(...entries.map(([value, label]) => { const o = node('option', label); o.value = value; return o; })); if (entries.some(e => e[0] === selected)) select.value = selected; };
  const usePage = async page => {
    state.page = page; $$('[data-page]').forEach(b => b.classList.toggle('ac-selected', b.dataset.page === page)); $$('[data-panel]').forEach(p => p.hidden = p.dataset.panel !== page);
    if (page === 'security') await security(); if (page === 'files' && state.session) await transferList(); if (page === 'artifacts') await artifacts(); if (page === 'hosts') await hosts(); if (page === 'tokens') await tokens(); if (page === 'browser' && state.session) { await tabs(); await view(); }
  };
  async function load() {
    const [health, config, tools] = await Promise.all([api('/v1/health'), api('/v1/config'), api('/v1/tools')]);
    if (!state.alive) return;
    state.config = config; state.tools = tools;
    if (!state.host) { state.local = config; setOptions($('.ac-host'), [['', 'Local'], ...(config.hosts || []).map(h => [h.id, h.name || h.alias])], state.host); }
    const sessions = config.sessions || [];
    if (!sessions.some(x => x.id === state.session)) state.session = sessions[0]?.id || '';
    setOptions($('.ac-session'), sessions.map(x => [x.id, x.name || x.id]), state.session);
    setOptions($('.ac-provider'), (config.providers || []).map(x => [x.id, `${x.id} · ${x.model}${x.vision ? ' · vision' : ''}`]), $('.ac-provider').value);
    setOptions($('.ac-tool'), tools.map(x => [x.name, x.name]), $('.ac-tool').value || 'snapshot-interactive');
    $('.ac-config').value = pretty(config); $('.ac-health').textContent = `${health.browser} · ${health.version}`;
    $('.ac-native').hidden = !options.openNativeBrowser || !!state.host;
    $('.ac-footer-status').textContent = `${tools.length} tools · ${(config.providers || []).length} providers · ${state.host || 'local host'}`;
    schema(); await poll();
  }
  function schema() {
    const t = state.tools.find(x => x.name === $('.ac-tool').value); if (!t) return;
    $('.ac-schema').textContent = pretty(t.inputSchema); $('.ac-tool-description').textContent = t.description;
  }
  async function tool(name, argumentsValue = {}) {
    const value = await api('/v1/tools/call', 'POST', { session: requireSession(), name, arguments: argumentsValue });
    tell(`${name}: completed; inspect the result before treating the task as successful.`); return value;
  }
  async function poll() {
    if (!state.alive || state.polling || !state.config) return; state.polling = true;
    try {
      const requestedHost = state.host, requestedSession = state.session;
      const [runs, approvals] = await Promise.all([api('/v1/runs'), api('/v1/approvals')]);
      if (!state.alive || state.host !== requestedHost || state.session !== requestedSession) return;
      renderRuns(runs); renderApprovals(approvals);
      if (state.session) {
        const sid = encodeURIComponent(state.session);
        const [control, events] = await Promise.all([api(`/v1/control?session=${sid}`), api(`/v1/events?session=${sid}&after=${state.after}`)]);
        if (state.host !== requestedHost || state.session !== requestedSession) return;
        $('.ac-owner').textContent = `${control.owner === 'agent' ? 'Agent' : 'Human'} control · epoch ${control.epoch}`;
        $('.ac-owner').classList.toggle('ac-active', control.owner === 'agent');
        if (events.length) { state.after = events[events.length - 1].seq; state.events.push(...events); state.events = state.events.slice(-200); $('.ac-events').textContent = state.events.map(e => `${e.seq} ${e.at} ${e.type}\n${pretty(e.data)}`).join('\n\n'); }
        if (state.page === 'browser' && $('.ac-live').checked && Date.now() - state.lastView > 1200) await view();
      }
    } catch (e) { error(e); } finally { state.polling = false; }
  }
  function renderRuns(runs) {
    const relevant = runs.filter(r => r.session === state.session).sort((a,b) => a.started.localeCompare(b.started)).slice(-20);
    const area = $('.ac-runs'); area.replaceChildren();
    if (!relevant.length) { const empty = node('div', undefined, 'ac-empty'); empty.append(node('h2', 'One browser. Your agent. Your control.'), node('p', 'Select a model, register a workspace and delegate an outcome. Or open the Browser tab and navigate manually. The retained svolo interface is still available beside this panel.')); area.append(empty); }
    for (const r of relevant) { const card = node('article', undefined, 'ac-run-card'); const top = node('div', undefined, 'ac-run-meta'); top.append(node('strong', `${r.provider} / ${r.model || ''}`), node('span', `${r.status} · ${r.steps} steps`)); card.append(top); if (r.text) card.append(node('pre', r.text, 'ac-message')); if (r.error) card.append(node('pre', r.error, 'ac-error')); if (['running','waiting_approval'].includes(r.status)) card.append(button('Stop this run', () => api('/v1/runs/stop', 'POST', { id: r.id }), 'ac-danger')); area.append(card); }
  }
  function renderApprovals(approvals) {
    const relevant = approvals.filter(x => !state.session || x.session === state.session);
    const area = $('.ac-approvals'); const key = relevant.map(x => x.id).join(); if (area.dataset.key === key) return; area.dataset.key = key; area.replaceChildren();
    for (const a of relevant) { const card = node('article', undefined, 'ac-approval'); card.append(node('strong', `Permission requested: ${a.tool}`), node('p', `Session ${a.session} · ${a.runId}`), node('pre', pretty(a.arguments))); const actions = node('div', undefined, 'ac-actions'); const answer = async approved => { await api('/v1/approvals', 'POST', { id: a.id, approved }); area.dataset.key = ''; await poll(); }; actions.append(button('Deny', () => answer(false)), button('Approve this action', () => answer(true), 'ac-primary')); card.append(actions); area.append(card); }
  }
  async function tabs() {
    if (!state.session) return;
    // Observation does not change the control lease.
    const list = await api('/v1/browser/tabs?session='+encodeURIComponent(state.session)); const strip = $('.ac-tabs'); strip.replaceChildren();
    for (const t of list) strip.append(button(t.title || t.url || t.id, async () => { state.tab=t.id; await tool('switch-tab',{query:t.id}); await view(); }, t.id===state.tab?'ac-selected':''));
  }
  async function view() {
    const sid = requireSession(), host = state.host;
    const result = await api(`/v1/view?session=${encodeURIComponent(sid)}${state.tab?'&tab='+encodeURIComponent(state.tab):''}`);
    if (!state.alive || sid!==state.session || host!==state.host) return;
    state.view=result;state.lastView=Date.now();$('.ac-frame').src=`data:${result.mimeType};base64,${result.data}`;$('.ac-frame-note').hidden=true;
  }
  async function input(argumentsValue) { await api('/v1/input','POST',{session:requireSession(),arguments:{...argumentsValue,...(state.tab?{tab:state.tab}:{})}}); await view(); }
  async function artifacts() {
    const list = await api(`/v1/artifacts?session=${encodeURIComponent(requireSession())}`); const area=$('.ac-artifacts');area.replaceChildren();
    for (const a of list) { const card=node('article',undefined,'ac-run-card');card.append(node('strong',a.name),node('p',`${a.mimeType} · ${a.size} bytes · ${a.created}`),node('code',a.sha256));card.append(button('Verify',async()=>{$('.ac-tool-output').textContent=pretty(await tool('verify-artifact',{id:a.id}));tell(`Verified ${a.name}`);}));
      if(options.downloadArtifact) card.append(button('Download',()=>options.downloadArtifact(state.host,state.session,a.id,a.name)));
      area.append(card);
    }
    if(!list.length) area.append(node('p','No saved artifacts in this session.'));
  }
  async function hosts() {
    const local = await transport('/v1/config','GET'); state.local=local;
    const statuses = await transport('/v1/hosts/status','GET');const area=$('.ac-host-list');area.replaceChildren();
    setOptions($('.ac-host'),[['','Local'],...(local.hosts||[]).map(h=>[h.id,h.name||h.alias])],state.host);
    for(const h of local.hosts||[]){const status=statuses.find(x=>x.id===h.id);const card=node('article',undefined,'ac-run-card');card.append(node('strong',h.name||h.alias),node('p',`${h.alias} → loopback:${h.remotePort} · ${status?.state||(status?.connected?'connected':'disconnected')}`));if(status?.error)card.append(node('pre',status.error,'ac-error'));card.append(button(status?.connected?'Disconnect':'Connect',async()=>{await transport('/v1/hosts/'+(status?.connected?'disconnect':'connect'),'POST',{id:h.id});await hosts();}),button('Detect platform',async()=>{$('.ac-ssh-output').textContent=pretty(await transport('/v1/hosts/detect','POST',{id:h.id}));}),button('Install daemon',async()=>{if(!confirm(`Install a user-level daemon on SSH host ${h.alias}? This transfers an executable and stores the remote token in your unlocked local vault. Existing healthy daemons are retained.`))return;$('.ac-ssh-output').textContent=pretty(await transport('/v1/hosts/bootstrap','POST',{id:h.id,options:{mode:'process',replace:false}}));await hosts();}),button('Install login service',async()=>{if(!confirm(`Install a per-user login service on ${h.alias}? This changes the remote user's service configuration, not system services.`))return;$('.ac-ssh-output').textContent=pretty(await transport('/v1/hosts/bootstrap','POST',{id:h.id,options:{mode:'user-service',replace:false}}));await hosts();}));area.append(card);}
    if(!(local.hosts||[]).length)area.append(node('p','No SSH hosts configured. Add hosts to the LOCAL configuration; the example file documents every field.'));
  }
  async function tokens(){const list=await api('/v1/tokens');const area=$('.ac-tokens');area.replaceChildren();for(const t of list){const card=node('article',undefined,'ac-run-card');card.append(node('strong',t.label||t.id),node('p',`Session ${t.session} · ${t.created}`),button('Revoke',async()=>{await api('/v1/tokens?id='+encodeURIComponent(t.id),'DELETE');await tokens();}));area.append(card);}}

  // Long operations capture their endpoint. A host/tab change must never redirect
  // a later file chunk, credential mutation or download to a different machine.
  const bound = () => { const host=state.host; return (path,method='GET',body) => host ? transport('/v1/remote','POST',{host,method,path,body}) : transport(path,method,body); };
  const hex = bytes => [...new Uint8Array(bytes)].map(x=>x.toString(16).padStart(2,'0')).join('');
  const sha = async bytes => hex(await crypto.subtle.digest('SHA-256',bytes));
  const base64 = bytes => {let raw='';for(let i=0;i<bytes.length;i+=16384)raw+=String.fromCharCode(...bytes.subarray(i,i+16384));return btoa(raw);};
  async function transferList(){const sid=requireSession(),call=bound(),area=$('.ac-transfers');const rows=await call('/v1/transfers?session='+encodeURIComponent(sid));area.replaceChildren();for(const row of rows){const card=node('article',undefined,'ac-run-card');card.append(node('strong',row.path),node('p',`${row.id} · ${row.direction} · ${row.status} · ${row.offset}/${row.size}`),button('Abort / release staging',async()=>{await call('/v1/transfers/abort','POST',{session:sid,id:row.id});await transferList();}));area.append(card);}}
  async function security(){const call=bound();const [status,settings]=await Promise.all([call('/v1/vault/status'),call('/v1/computer/settings')]);$('.ac-vault-status').textContent=pretty(status);$('.ac-computer-enabled').checked=settings.enabled;$('.ac-computer-foreground').checked=settings.allowForeground;const area=$('.ac-secret-list');area.replaceChildren();if(status.unlocked){const secrets=await call('/v1/credentials');for(const item of secrets){const id=item.name;const card=node('article',undefined,'ac-run-card');card.append(node('span',id),button('Delete reference',async()=>{if(confirm(`Delete credential ${id}? Providers using it will no longer authenticate.`)){await call('/v1/credentials?name='+encodeURIComponent(id),'DELETE');await security();}}));area.append(card);}}}
  on('.ac-host-add','click',async()=>{const id=$('.ac-host-id').value.trim(),alias=$('.ac-host-alias').value.trim();if(!/^[A-Za-z0-9][A-Za-z0-9_.-]{0,100}$/.test(id))throw new Error('Invalid host ID.');const config=await transport('/v1/config','GET');if((config.hosts||[]).some(h=>h.id===id))throw new Error('Host ID already exists.');config.hosts=config.hosts||[];config.hosts.push({id,alias,name:alias,remotePort:Number($('.ac-host-port').value),tokenRef:'ssh-'+id,autoReconnect:true});await transport('/v1/config','PUT',config);await hosts();tell('Host saved. Unlock the local vault before installing its daemon.');});
  on('.ac-vault-refresh','click',security);
  on('.ac-vault-unlock','click',async()=>{const key=$('.ac-vault-key').value.trim();$('.ac-vault-key').value='';await api('/v1/vault/unlock','POST',{key});await security();});
  on('.ac-vault-lock','click',async()=>{await api('/v1/vault/lock','POST',{});await security();});
  on('.ac-secret-save','click',async()=>{const id=$('.ac-secret-ref').value.trim(),value=$('.ac-secret-value').value;$('.ac-secret-value').value='';await api('/v1/credentials','PUT',{name:id,value});await security();tell('Encrypted credential saved. Reference it through apiKeyRef or tokenRef in the selected host configuration.');});
  on('.ac-computer-save','click',async()=>{await api('/v1/computer/settings','PUT',{enabled:$('.ac-computer-enabled').checked,allowForeground:$('.ac-computer-foreground').checked});tell('Native desktop policy saved. Existing per-app approval gates remain active.');});
  on('.ac-computer-caps','click',async()=>{$('.ac-computer-output').textContent=pretty(await api('/v1/computer/capabilities'));});
  on('.ac-transfer-list','click',transferList);
  on('.ac-upload','click',async()=>{
    const sid=requireSession(),call=bound(),file=$('.ac-transfer-file').files[0],destination=$('.ac-transfer-path').value.trim(),resume=$('.ac-transfer-id').value.trim();
    if(!file||!destination)throw new Error('Select a file and a workspace-relative destination.');if(file.size>256*1024*1024)throw new Error('File exceeds 256 MiB.');
    $('.ac-upload').disabled=true;
    try{const digest=await sha(await file.arrayBuffer());let task=resume?await call(`/v1/transfers/status?session=${encodeURIComponent(sid)}&id=${encodeURIComponent(resume)}`):await call('/v1/transfers','POST',{session:sid,path:destination,size:file.size,sha256:digest,expectedOldSha256:$('.ac-transfer-old').value.trim()});
      if(task.sha256!==digest||task.size!==file.size||task.path!==destination)throw new Error('Resume file, path or checksum does not match the recorded transfer.');$('.ac-transfer-id').value=task.id;
      while(task.offset<file.size){const bytes=new Uint8Array(await file.slice(task.offset,task.offset+1048576).arrayBuffer());task=await call('/v1/transfers/chunk','POST',{session:sid,id:task.id,offset:task.offset,data:base64(bytes)});$('.ac-transfer-output').textContent=`${task.id}\nAcknowledged ${task.offset} of ${file.size} bytes`;}
      task=await call('/v1/transfers/commit','POST',{session:sid,id:task.id});$('.ac-transfer-output').textContent=pretty(task);tell('Upload committed after SHA-256 verification.');
    }finally{$('.ac-upload').disabled=false;}
  });
  on('.ac-download','click',async()=>{
    const sid=requireSession(),call=bound(),path=$('.ac-transfer-path').value.trim();if(!path)throw new Error('Enter a workspace-relative path.');$('.ac-download').disabled=true;
    let task;
    try{task=await call('/v1/transfers/download','POST',{session:sid,path});const parts=[];let offset=0;while(offset<task.size){const part=await call(`/v1/transfers/chunk?session=${encodeURIComponent(sid)}&id=${encodeURIComponent(task.id)}&offset=${offset}`);const bytes=Uint8Array.from(atob(part.data),c=>c.charCodeAt(0));if(!bytes.length||part.nextOffset!==offset+bytes.length||part.nextOffset>task.size)throw new Error('Invalid download chunk.');parts.push(bytes);offset=part.nextOffset;$('.ac-transfer-output').textContent=`${offset} of ${task.size} bytes`;}
      const blob=new Blob(parts);if(await sha(await blob.arrayBuffer())!==task.sha256)throw new Error('Download SHA-256 verification failed.');const url=URL.createObjectURL(blob),link=node('a');link.href=url;link.download=path.split(/[\\/]/).pop()||'download';root.append(link);link.click();link.remove();setTimeout(()=>URL.revokeObjectURL(url),60000);$('.ac-transfer-output').textContent=pretty({file:path,size:task.size,sha256:task.sha256,verified:true});
      await call('/v1/transfers/abort','POST',{session:sid,id:task.id});
    }finally{$('.ac-download').disabled=false;}
  });

  on('[data-page]','click',e=>usePage(e.currentTarget.dataset.page));
  on('.ac-new','click',()=>{$('.ac-session-editor').hidden=false;$('.ac-session-id').value='workspace-'+Date.now().toString(36);$('.ac-workspace-path').value='';$('.ac-choose-folder').hidden=!!state.host||!options.pickWorkspace;$('.ac-session-id').focus();});
  on('.ac-choose-folder','click',async()=>{if(!state.host&&options.pickWorkspace){const path=await options.pickWorkspace();if(path)$('.ac-workspace-path').value=path;}});
  on('.ac-session-cancel','click',()=>{$('.ac-session-editor').hidden=true;});
  on('.ac-session-save','click',async()=>{const id=$('.ac-session-id').value.trim();if(!/^[A-Za-z0-9_-]+$/.test(id))throw new Error('Use letters, numbers, _ or - for the session id.');await api('/v1/sessions','POST',{id,name:id,workspace:$('.ac-workspace-path').value.trim(),allowExec:false});state.session=id;state.after=0;state.events=[];$('.ac-session-editor').hidden=true;await load();tell('Session registered. Process execution remains disabled.');});
  on('.ac-session','change',async e=>{state.session=e.target.value;state.after=0;state.events=[];state.tab='';state.view=null;await poll();});
  on('.ac-host','change',async e=>{const id=e.target.value;if(id){tell('Connecting to selected SSH host…');try{await transport('/v1/hosts/connect','POST',{id});}catch(err){e.target.value=state.host;throw err;}}state.host=id;state.session='';state.after=0;state.events=[];state.tab='';state.view=null;await load();tell(`Selected ${id||'local host'}. Paths and providers now belong to this host.`);});
  on('.ac-take','click',async()=>{await api('/v1/control','POST',{session:requireSession(),owner:'human'});tell('Human control. Already-dispatched external actions cannot be undone.');await poll();});
  on('.ac-release','click',async()=>{await api('/v1/control','POST',{session:requireSession(),owner:'agent'});tell('Agent control granted. Interrupted runs are not silently replayed; submit a new instruction or resume a human-suspended routine.');await poll();});
  on('.ac-run','click',async()=>{if(state.busy)return;state.busy=true;$('.ac-run').disabled=true;try{const req={session:requireSession(),provider:$('.ac-provider').value,prompt:$('.ac-prompt').value,images:state.images,maxSteps:Number($('.ac-steps').value),autonomy:$('.ac-autonomy').value,continue:$('.ac-continue').checked};await api('/v1/runs','POST',req);$('.ac-prompt').value='';state.images=[];$('.ac-images').value='';$('.ac-image-count').textContent='';tell('Run started. Stop and approvals are handled outside the model.');await poll();}finally{state.busy=false;$('.ac-run').disabled=false;}});
  on('.ac-images','change',async e=>{const files=[...e.target.files];if(files.length>4)throw new Error('At most four images per turn.');state.images=[];for(const file of files){if(file.size>6*1024*1024)throw new Error('Image exceeds 6 MiB.');const data=await new Promise((resolve,reject)=>{const r=new FileReader();r.onload=()=>resolve(String(r.result).split(',')[1]);r.onerror=reject;r.readAsDataURL(file);});state.images.push({mimeType:file.type,data});}$('.ac-image-count').textContent=`${state.images.length} image(s)`;});
  on('.ac-tool','change',schema);
  on('.ac-invoke','click',async()=>{const a=JSON.parse($('.ac-args').value);if(!a||Array.isArray(a)||typeof a!=='object')throw new Error('Arguments must be an object.');$('.ac-tool-output').textContent=pretty(await tool($('.ac-tool').value,a));});
  on('.ac-reload-config','click',load);
  on('.ac-example','click',()=>{const c=JSON.parse($('.ac-config').value);c.providers=c.providers||[];c.providers.push({id:'provider-'+(c.providers.length+1),kind:'responses',baseUrl:'https://api.openai.com/v1',apiKeyEnv:'OPENAI_API_KEY',model:'REPLACE_WITH_YOUR_MODEL_ID',vision:true,stream:true,maxOutputTokens:4096});$('.ac-config').value=pretty(c);tell('Edit model and endpoint before saving. Export the named environment variable before starting the daemon.');});
  on('.ac-save-config','click',async()=>{const c=JSON.parse($('.ac-config').value);await api('/v1/config','PUT',c);await load();tell('Configuration validated and saved. No credentials are embedded in this file.');});
  on('.ac-mcp-refresh','click',async()=>{$('.ac-config-output').textContent=pretty(await api('/v1/mcp/refresh','POST',{}));await load();});
  on('.ac-resolve','click',async()=>{$('.ac-ssh-output').textContent=pretty(await transport('/v1/hosts/resolve','POST',{alias:$('.ac-alias').value}));});
  on('.ac-mint','click',async()=>{const result=await api('/v1/tokens','POST',{session:requireSession(),label:$('.ac-token-label').value});$('.ac-token-output').textContent=`Store this token in a private file. It is shown only now.\n${result.token}\n\nsvolo-core mcp --url http://127.0.0.1:7331 --token-file /private/client.token\n\nSession: ${result.session}`;await tokens();});
  on('.ac-go','click',async()=>{await tool('navigate',{url:$('.ac-url').value,...(state.tab?{tab:state.tab}:{})});await tabs();await view();});
  on('.ac-new-tab','click',async()=>{const t=await tool('open-tab',{url:$('.ac-url').value||'about:blank'});state.tab=t.id;await tabs();await view();});
  on('.ac-native','click',()=>options.openNativeBrowser?.(requireSession()));
  on('.ac-refresh-view','click',view);
  on('.ac-shot','click',async()=>{const result=await tool('screenshot',{save:true,...(state.tab?{tab:state.tab}:{})});tell(`Saved screenshot artifact ${result.artifact?.id}`);});
  on('.ac-type','click',async()=>{await input({kind:'text',text:$('.ac-input-text').value});$('.ac-input-text').value='';});
  on('.ac-key','click',e=>input({kind:'key',key:e.currentTarget.dataset.key}));
  on('.ac-frame','click',e=>{if(!state.view)return;const r=e.currentTarget.getBoundingClientRect(),v=state.view.viewport;const width=v?.width||e.currentTarget.naturalWidth,height=v?.height||e.currentTarget.naturalHeight;return input({kind:'click',x:(e.clientX-r.left)/r.width*width,y:(e.clientY-r.top)/r.height*height});});
  on('.ac-frame','keydown',e=>{if(!state.view||e.isComposing)return;e.preventDefault();const modifiers=[e.ctrlKey?'Ctrl':null,e.altKey?'Alt':null,e.metaKey?'Meta':null,e.shiftKey&&e.key.length>1?'Shift':null].filter(Boolean);if(e.key.length===1&&!modifiers.length)return input({kind:'text',text:e.key});return input({kind:'key',key:[...modifiers,e.key===' '?'Space':e.key].join('+')});});
  const timer=setInterval(()=>void poll(),1100);disposers.push(()=>clearInterval(timer));
  Promise.resolve().then(async()=>{
    // Optional desktop session follows the selected runtime chat, but does not enable
    // process execution or import credentials implicitly.
    if(options.initialSession){const config=await transport('/v1/config','GET');if(!(config.sessions||[]).some(x=>x.id===options.initialSession))await transport('/v1/sessions','POST',{id:options.initialSession,name:options.initialSession,workspace:options.initialWorkspace||'',allowExec:false});}
    await load();tell('Ready. Configure a provider for agent mode, or browse manually.');
  }).catch(error);
  return ()=>{state.alive=false;for(const dispose of disposers)dispose();state.images=[];root.replaceChildren();};
}
