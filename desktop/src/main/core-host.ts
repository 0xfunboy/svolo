// The Electron main process owns this bridge. Application/approval webContents
// are NEVER placed in its target registry and no admin token enters the renderer.
import { randomBytes, createHash } from 'node:crypto';
import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { writeFile } from 'node:fs/promises';
import { basename, join } from 'node:path';
import { createInterface } from 'node:readline';
import { app, dialog, safeStorage, type WebContents, type DownloadItem } from 'electron';
import type { CoreMethod } from '../shared/agent-core';
import type { ComputerMethod, ComputerNotification } from '../shared/computer';
import type { AgentAction } from '../shared/browser';
import type { BrowserAgent } from './browser/agent';
import { cdp, withNativeLease } from './browser/cdp';
import { ControlAuthority } from './browser/control';
import type { BrowserManager, Tab } from './browser/manager';
import { log } from './log';

interface BridgeRequest {
  id: string; session: string; action: string; target?: string;
  method?: string; params?: Record<string, unknown>; deadline: string; actor?:string; epoch?:number;
}
type RpcCommandRecord={type:string;[key:string]:unknown};
interface Ready { type: 'ready'; url: string; tokenFile: string; data: string; browser: string; }
interface CoreSession { id: string; name: string; workspace?: string; allowExec: boolean; }
const SESSION = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$/;
// No arbitrary URL, CDP bridge access, credential minting on behalf of a page,
// or handler installation. Only the trusted app renderer reaches this broker.
const ROUTES = new Map<string, readonly CoreMethod[]>([
 ['/v1/projects/github',['POST']],['/v1/projects/atp',['POST']],['/v1/git',['POST']],['/v1/atp/runs',['GET']],['/v1/atp/start',['POST']],['/v1/atp/stop',['POST']],['/v1/atp/held',['GET']],
 ['/v1/runtime',['GET','POST']], ['/v1/runtime/send',['POST']], ['/v1/runtime/ui',['POST']], ['/v1/runtime/stop',['POST']], ['/v1/runtime/events',['GET']],
  ['/v1/projects/board',['GET','POST']], ['/v1/projects/laments',['GET','POST']],
  ['/v1/computer/settings',['GET','PUT']], ['/v1/computer/capabilities',['GET']], ['/v1/vault/status',['GET']], ['/v1/vault/unlock',['POST']], ['/v1/vault/lock',['POST']],
  ['/v1/credentials',['GET','PUT','DELETE']], ['/v1/events/cursor',['GET']],
  ['/v1/health',['GET']], ['/v1/config',['GET','PUT']], ['/v1/sessions',['GET','POST']],
  ['/v1/control',['GET','POST']], ['/v1/tools',['GET']], ['/v1/tools/call',['POST']],
  ['/v1/input',['POST']], ['/v1/view',['GET']], ['/v1/browser/tabs',['GET']], ['/v1/runs',['GET','POST']],
  ['/v1/runs/stop',['POST']], ['/v1/approvals',['GET','POST']], ['/v1/events',['GET']],
  ['/v1/artifacts',['GET']], ['/v1/tokens',['GET','POST','DELETE']], ['/v1/mcp/refresh',['POST']],
  ['/v1/transfers',['GET','POST']], ['/v1/transfers/status',['GET']], ['/v1/transfers/chunk',['GET','POST']],
  ['/v1/transfers/commit',['POST']], ['/v1/transfers/abort',['POST']], ['/v1/transfers/download',['POST']],
  ['/v1/hosts/bootstrap',['POST']], ['/v1/hosts/detect',['POST']], ['/v1/hosts/status',['GET']], ['/v1/hosts/connect',['POST']], ['/v1/hosts/disconnect',['POST']],
  ['/v1/hosts/resolve',['POST']], ['/v1/remote',['POST']],
]);

export class CoreHost {
  private process?: ChildProcessWithoutNullStreams;
  private startup?: Promise<void>;
  private ready?: Ready;
  private token = '';
  private error?: string;
  private closing = false;
  private readonly watched = new Set<number>();
  private readonly downloads = new Map<number, string>();
  private readonly downloadSessions = new WeakSet<Electron.Session>();
  private readonly runtimeSessions = new Map<string, Promise<void>>();
  private readonly owner = new Map<string, string>();
  private authority = new ControlAuthority();
  private eventQueue: { session: string; method: string; params: Record<string, unknown> }[] = [];
  private eventsSending = false;
  private polling = false;
  private nativeStream?: AbortController;
  private nativeStreamReady?: Promise<void>;
  private nativeCursor = 0;
  private readonly nativeOwners = new Map<string, string>();
  private readonly nativeListeners = new Set<(event: ComputerNotification) => void>();

  constructor(private browser: () => BrowserManager | undefined, private agent: () => BrowserAgent | undefined,
    private nativeInvoke?: (session: string, route: string, params: unknown) => Promise<unknown>) {}

  status() { return { running: !!this.ready && !this.closing, error: this.error, browser: this.ready?.browser }; }

  async start(): Promise<void> {
    if (this.closing) throw new Error('Agent core is shutting down.');
    if (!this.startup) this.startup = this.launch().catch(error => { this.error = String(error); this.startup = undefined; throw error; });
    return this.startup;
  }

  private binary(): string {
    const exe = process.platform === 'win32' ? 'svolo-core.exe' : 'svolo-core';
    const candidates = [process.env.SVOLO_CORE_BIN,
      join(process.resourcesPath, 'core', exe),
      join(app.getAppPath(), 'build', 'core', exe),
      join(app.getAppPath(), '..', 'dist', 'core', `${process.platform==='win32'?'windows':process.platform}-${process.arch==='x64'?'amd64':process.arch}`, exe),
    ].filter((p): p is string => !!p);
    const result = candidates.find(p => existsSync(p));
    if (!result) throw new Error('Go core binary missing. Run node scripts/build-core.mjs in desktop/ or set SVOLO_CORE_BIN. The core must be built for the selected platform before launch.');
    return result;
  }

  private async launch(): Promise<void> {
    await app.whenReady();
    const data = join(app.getPath('userData'), 'agent-core');
    mkdirSync(data, { recursive: true, mode: 0o700 });
    const env = {...process.env};
    env.SVOLO_ATP_LIBRARIAN=join(app.isPackaged?process.resourcesPath:app.getAppPath(),'resources','atp','skills','atp-local-librarian','scripts','atp_local_librarian.py');
    if(app.isPackaged)env.SVOLO_ATP_LIBRARIAN=join(process.resourcesPath,'atp','skills','atp-local-librarian','scripts','atp_local_librarian.py');
    // Do not silently fall back to Linux's hardcoded basic_text encryption.
    if (!env.SVOLO_VAULT_KEY && safeStorage.isEncryptionAvailable() &&
        (process.platform !== 'linux' || !['basic_text','unknown'].includes(safeStorage.getSelectedStorageBackend()))) {
      const keyPath=join(data, 'vault-key.os-encrypted');
      if(existsSync(keyPath)) env.SVOLO_VAULT_KEY=safeStorage.decryptString(readFileSync(keyPath));
      else {
        env.SVOLO_VAULT_KEY=randomBytes(32).toString('hex');
        writeFileSync(keyPath,safeStorage.encryptString(env.SVOLO_VAULT_KEY),{mode:0o600,flag:'wx'});
      }
    }
    const child = spawn(this.binary(), ['serve','--listen','127.0.0.1:0','--data',data,'--browser','electron'], { stdio: 'pipe', windowsHide: true, env });
    this.process = child;
    // Never log protocol stdout or token values. Only diagnostic stderr is logged.
    child.stderr.on('data', (chunk: Buffer) => log.warn('core', String(chunk).slice(0, 4000)));
    const lines = createInterface({ input: child.stdout });
    await new Promise<void>((resolve, reject) => {
      const timeout = setTimeout(() => { child.kill(); reject(new Error('Go core startup timed out.')); }, 15000);
      const failed = (error: Error) => { clearTimeout(timeout); reject(error); };
      child.once('error', failed);
      child.once('exit', () => { clearTimeout(timeout); if (!this.ready) reject(new Error('Go core exited before readiness.')); });
      lines.on('line', (line: string) => {
        if (this.ready) return;
        try {
          const ready = JSON.parse(line) as Ready;
          if (ready.type !== 'ready') return;
          const url = new URL(ready.url);
          if (url.protocol !== 'http:' || url.hostname !== '127.0.0.1' || ready.data !== data || ready.tokenFile !== join(data,'auth.token')) throw new Error('Invalid core readiness identity.');
          this.token = readFileSync(ready.tokenFile, 'utf8').trim();
          if (this.token.length < 32) throw new Error('Invalid core credential.');
          this.ready = ready; this.error = undefined;
          clearTimeout(timeout); child.off('error', failed); resolve();
        } catch (error) { failed(error instanceof Error ? error : new Error(String(error))); }
      });
    });
    child.on('exit', () => { this.authority.revokeAll();this.ready = undefined; this.token = ''; this.startup = undefined; this.error = 'Go core exited. No in-flight action will be replayed automatically.'; });
    void this.pollBridge();
  }

  private async raw(path: string, method: CoreMethod = 'GET', body?: unknown): Promise<any> {
    if (!this.ready || !this.token) throw new Error('Go core is not connected.');
    const response = await fetch(this.ready.url + path, {
      method, headers: { Authorization: `Bearer ${this.token}`, 'Content-Type':'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body), redirect:'error',
      signal: AbortSignal.timeout(path === '/v1/bridge/poll' ? 25000 : 360000),
    });
    if (response.status === 204) return null;
    const value = await response.json() as { error?: string };
    if (!response.ok) throw new Error(value.error || `Core HTTP ${response.status}`);
    return value;
  }

  async request(path: string, method: CoreMethod = 'GET', body?: unknown): Promise<any> {
    if (typeof path !== 'string' || !path.startsWith('/v1/') || /[\r\n#]/.test(path) || path.includes('..')) throw new Error('Invalid core API path.');
    const route = path.split('?')[0] || '';
    if (!ROUTES.get(route)?.includes(method)) throw new Error(`Core API route not permitted: ${method} ${route}`);
    await this.start();
    // Resolve wrapper requests before forwarding so the renderer cannot reach
    // remote internal bridges through a privileged generic proxy.
    if (route === '/v1/remote') {
      const p = body as { path?: string; method?: CoreMethod };
      if (!p || typeof p.path !== 'string' || p.path.startsWith('/v1/remote') || !ROUTES.get(p.path.split('?')[0] || '')?.includes(p.method || 'GET')) throw new Error('Remote control-plane route not permitted.');
    }
    const result = await this.raw(path, method, body);
    if (route === '/v1/control' && result?.owner) {
      const sid = method === 'POST' ? (body as {session:string}).session : new URLSearchParams(path.split('?')[1]).get('session');
      if(sid){this.owner.set(sid,result.owner);this.authority.accept(sid,result.owner,result.epoch);this.browser()?.protectSession(sid,result.owner==='agent');}
    }
    return result;
  }

  async workspaceSession(cwd:string,allowExec=false,identity?:string):Promise<string> {
    await this.start();const sid=identity??'project-'+createHash('sha256').update(cwd).digest('hex').slice(0,24);
    const sessions=await this.raw('/v1/sessions') as CoreSession[];const existing=sessions.find(s=>s.id===sid);
    if(existing?.workspace&&existing.workspace!==cwd)throw new Error('Workspace identity mismatch');
    if(!existing||(!existing.allowExec&&allowExec))await this.raw('/v1/sessions','POST',{id:sid,name:existing?.name??basename(cwd),workspace:cwd,allowExec:existing?.allowExec||allowExec});
    return sid;
  }
  async projectRequest(cwd:string,route:string,body:Record<string,unknown>,allowExec=false):Promise<any> {
    const session=await this.workspaceSession(cwd,allowExec);return this.request(route,'POST',{...body,session});
  }
  async detachedCommand(session:string,command:RpcCommandRecord):Promise<any> {
    const states=await this.request('/v1/runtime') as {id:string;session:string;status:string}[];
    const state=states.find(s=>s.session===session&&s.status==='running');if(!state)throw new Error('Host runtime is not running');
    return this.request('/v1/runtime/send','POST',{id:state.id,command});
  }
  async runtimeStart(options: { session: string; cwd: string; sessionPath?: string; args?: string[]; env?: Record<string,string> }): Promise<any> {
    await this.start();
    const sessions = await this.raw('/v1/sessions') as CoreSession[];
    const existing = sessions.find(s => s.id === options.session);
    if (existing && existing.workspace && existing.workspace !== options.cwd) throw new Error('Runtime workspace changed; open a new session.');
    if (!existing || !existing.workspace) await this.raw('/v1/sessions','POST', {id:options.session,name:options.session,workspace:options.cwd,allowExec:existing?.allowExec ?? false});
    return this.raw('/v1/runtime','POST',options);
  }

  async registerRuntimeSession(session: string, workspace = ''): Promise<void> {
    if (!SESSION.test(session)) throw new Error('Invalid pi session handle.');
    let registered = this.runtimeSessions.get(session);
    if (!registered) {
      registered = (async () => {
        await this.start();
        const sessions = await this.raw('/v1/sessions') as CoreSession[];
        if (!sessions.some(s => s.id === session)) {
          await this.raw('/v1/sessions','POST',{id:session,name:`pi ${session.slice(0,8)}`,workspace,allowExec:false});
          const lease=await this.raw('/v1/control','POST',{session,owner:'agent'}); this.owner.set(session,'agent');this.authority.accept(session,'agent',lease.epoch);this.browser()?.protectSession(session,true);
        }
      })();
      this.runtimeSessions.set(session, registered);
      void registered.catch(() => this.runtimeSessions.delete(session));
    }
    return registered;
  }

  async extensionAction(session: string, params: unknown): Promise<any> {
    await this.registerRuntimeSession(session);
    return this.raw('/v1/extensions/browser','POST',{session,arguments:params});
  }

  /** Real user input passes through this gate before another agent action. */
  async takeHuman(session?: string): Promise<void> {
    if (!session || !this.ready) return;
    const nonce=this.authority.takeover(session);this.owner.set(session,'human');
    const state=await this.raw('/v1/control','POST',{session,owner:'human'});
    if(this.authority.acknowledge(session,nonce,state.epoch))this.browser()?.protectSession(session,false);
  }

  watchTabs(): void {
    const manager = this.browser(); if (!manager) return;
    manager.setTakeoverHandler(sid=>this.takeHuman(sid));
    for (const tab of manager.tabs.values()) this.watch(tab);
  }

  private watch(tab: Tab): void {
    const wc = tab.view.webContents;
    if (this.watched.has(wc.id)) return;
    this.watched.add(wc.id);
    wc.once('destroyed', () => { this.watched.delete(wc.id); this.downloads.delete(wc.id); });
    wc.debugger.on('message', (_event, method, params) => {
      if (!tab.agent || !this.ready || !/^(Network\.(requestWillBeSent|responseReceived|loadingFailed)|Runtime\.consoleAPICalled)$/.test(method)) return;
      if (this.eventQueue.length < 256) this.eventQueue.push({session:tab.agent,method,params});
      void this.flushEvents();
    });
    // Physical input is intercepted by a separate trusted native view, not by
    // hooks on the page that could misclassify CDP-injected events.
    this.browser()?.setTakeoverHandler(sid=>this.takeHuman(sid));

  }

  private async flushEvents(): Promise<void> {
    if (this.eventsSending) return; this.eventsSending=true;
    try { while (this.eventQueue.length && this.ready && !this.closing) { const event=this.eventQueue.shift(); await this.raw('/v1/bridge/event','POST',event); } }
    catch { this.eventQueue=[]; }
    finally { this.eventsSending=false; }
  }

  private owned(sid: string, id?: string): Tab {
    const manager=this.browser();const tab=id?manager?.tabs.get(id):undefined;
    if (!tab || tab.agent!==sid || tab.view.webContents.isDestroyed()) throw new Error('Browser target is not owned by this session.');
    this.watch(tab);return tab;
  }

  private async pollBridge(): Promise<void> {
    if (this.polling) return;this.polling=true;
    try {
      while (this.ready && !this.closing) {
        const req=await this.raw('/v1/bridge/poll') as BridgeRequest|null;
        if (!req) continue;
        // The queue never replays a timed-out request. Check immediately before
        // dispatch as an additional boundary on the native side.
        if (Date.now()>Date.parse(req.deadline)) { await this.raw('/v1/bridge/result','POST',{id:req.id,error:'native dispatch deadline expired'});continue; }
        try {
          const authorized=await this.raw('/v1/bridge/authorize','POST',{id:req.id}) as BridgeRequest;
          if(authorized.session!==req.session||authorized.actor!==req.actor||authorized.epoch!==req.epoch)throw new Error('Bridge lease identity mismatch');
          let check=()=>{};
          if(req.actor&&req.epoch){check=this.authority.lease(req.session,req.epoch,req.actor);this.browser()?.protectSession(req.session,req.actor==='agent');}
          const guard=()=>{check();if(!this.ready||this.closing||Date.now()>Date.parse(req.deadline))throw new Error('Native bridge expired');};
          guard();const result=await withNativeLease(guard,()=>this.execute(req));await this.raw('/v1/bridge/result','POST',{id:req.id,result:result??null}); }
        catch (error) { await this.raw('/v1/bridge/result','POST',{id:req.id,error:error instanceof Error?error.message:String(error)}); }
      }
    } catch(error) { if(!this.closing)this.error=String(error); }
    finally { this.polling=false; }
  }

  private async execute(req: BridgeRequest): Promise<unknown> {
    if(!SESSION.test(req.session))throw new Error('Invalid session');
    const manager=this.browser();if(!manager)throw new Error('Native browser is not ready.');
    this.watchTabs();const p=req.params??{};
    const describe=(t:Tab)=>({id:t.id,url:t.view.webContents.getURL(),title:t.view.webContents.getTitle(),type:'page'});
    switch(req.action){
      case 'list':return [...manager.tabs.values()].filter(t=>t.agent===req.session).map(describe);
      case 'create':{
        const url=String(p.url||'about:blank');if(!/^(https?:\/\/|about:blank$)/i.test(url))throw new Error('Invalid managed navigation URL');
        const tab=manager.createTab(undefined,req.session);this.watch(tab);await manager.load(tab,url);return describe(tab);
      }
      case 'activate':manager.activate(this.owned(req.session,req.target).id);return {};
      case 'close':manager.closeTab(this.owned(req.session,req.target).id);return {};
      case 'close-session':for(const t of [...manager.tabs.values()])if(t.agent===req.session)manager.closeTab(t.id);return {};
      case 'extension-action':{
        const agent=this.agent();if(!agent)throw new Error('pi browser adapter unavailable.');
        const result=await agent.run(req.session,p as unknown as AgentAction);this.watchTabs();return result;
      }
      case 'native':{
        const route=String(p.route||'');if(!['/kanban','/atp','/lament','/computer'].includes(route)||!this.nativeInvoke)throw new Error('Native tool route refused');
        return this.nativeInvoke(req.session,route,p.arguments);
      }
      case 'cdp':{
        const tab=this.owned(req.session,req.target), wc=tab.view.webContents;const method=req.method||'';
        if(method==='Browser.setDownloadBehavior')return this.configureDownloads(tab,req.session);
        if(!/^(Page|DOM|Runtime|Input|Accessibility|Network|Emulation)\./.test(method))throw new Error('CDP domain not available on a scoped page target.');
        // Never attach to arbitrary webContents, browser UI, DevTools or targets
        // discovered by the model. Only this already-owned page receives CDP.
        if(method==='Emulation.setDeviceMetricsOverride'){
          await manager.setViewport(tab.id,{width:Number(p.width),height:Number(p.height),dpr:Number(p.deviceScaleFactor),mobile:p.mobile===true,source:'agent'});return {};
        }
        if(method==='Emulation.clearDeviceMetricsOverride'){await manager.setViewport(tab.id,undefined);return {};}
        const params={...p};
        if(method==='Input.dispatchMouseEvent'){
          const scale=tab.emulatedScale??1;
          if(typeof params.x==='number')params.x*=scale;if(typeof params.y==='number')params.y*=scale;
          if(tab.viewport?.touch && (params.type==='mousePressed'||params.type==='mouseReleased')){
            await cdp(wc,'Input.dispatchTouchEvent',{type:params.type==='mousePressed'?'touchStart':'touchEnd',touchPoints:params.type==='mousePressed'?[{x:params.x,y:params.y}]:[]});return {};
          }
        }
        if(method==='Page.captureScreenshot')await manager.ensureVisible(tab);
        return cdp(wc,method,params);
      }
      default:throw new Error(`Unknown native bridge action: ${req.action}`);
    }
  }

  private configureDownloads(tab: Tab, sid: string): object {
    if(!this.ready)throw new Error('Core not ready');
    const directory=join(this.ready.data,'downloads',sid);mkdirSync(directory,{recursive:true,mode:0o700});
    this.downloads.set(tab.view.webContents.id,directory);
    const session=tab.view.webContents.session;
    if(!this.downloadSessions.has(session)){
      this.downloadSessions.add(session);
      session.on('will-download',(_event,item:DownloadItem,wc:WebContents)=>{
        const dir=wc?this.downloads.get(wc.id):undefined;
        if(!dir)return; // Ordinary manual tabs retain native save-dialog behavior.
        const name=basename(item.getFilename()).replace(/[\\/:\x00]/g,'_')||'download';
        const path=join(dir,`${Date.now()}-${name}`);item.setSavePath(path);
      });
    }
    return {configured:true};
  }

  async showBrowser(sid: string): Promise<void> {
    if(!SESSION.test(sid))throw new Error('Invalid session');
    const manager=this.browser();if(!manager)throw new Error('Browser unavailable');
    let tab=[...manager.tabs.values()].find(t=>t.agent===sid);
    if(!tab){tab=manager.createTab(undefined,sid);await manager.load(tab,'about:blank');}
    this.watch(tab);await manager.ensureVisible(tab);
  }

  async saveArtifact(host: string, sid: string, id: string, name: string): Promise<void> {
    if(host&&!SESSION.test(host))throw new Error('Invalid host identity');
    if(!SESSION.test(sid)||!SESSION.test(id))throw new Error('Invalid artifact identity');
    await this.start();
    const response=await fetch(`${this.ready!.url}${host?'/v1/remote-artifact?host='+encodeURIComponent(host)+'&':'/v1/artifact?'}session=${encodeURIComponent(sid)}&id=${encodeURIComponent(id)}`,{headers:{Authorization:`Bearer ${this.token}`},redirect:'error',signal:AbortSignal.timeout(30000)});
    if(!response.ok)throw new Error(`Artifact validation failed: HTTP ${response.status}`);
    const bytes=Buffer.from(await response.arrayBuffer());if(bytes.length>64*1024*1024)throw new Error('Artifact exceeds save budget');
    const result=await dialog.showSaveDialog({defaultPath:basename(name)});if(result.canceled||!result.filePath)return;
    await writeFile(result.filePath,bytes,{mode:0o600});
  }

  nativeDirectory(): string { return join(this.ready?.data ?? app.getPath('userData'), 'native-computer'); }

  onNativeNotification(listener: (event: ComputerNotification) => void): () => void {
    this.nativeListeners.add(listener);
    return () => this.nativeListeners.delete(listener);
  }

  async nativeCall(method: ComputerMethod, params: unknown): Promise<unknown> {
    await this.start();
    if (!this.nativeStreamReady) {
      this.nativeStreamReady = (async () => {
        const cursor = await this.raw('/v1/events/cursor');
        this.nativeCursor = Number(cursor.last) || 0;
        this.nativeStream = new AbortController();
        // Connect the notification channel before granting desktop input.
        await new Promise<void>((resolve, reject) => {
          void this.readNativeEvents(this.nativeStream!.signal, resolve, reject);
        });
      })().catch(error => { this.nativeStreamReady = undefined; throw error; });
    }
    await this.nativeStreamReady;
    const result = await this.raw('/v1/computer/native', 'POST', { method, params });
    if (!result?.error && params && typeof params === 'object') {
      const p = params as { app?: string | { bundleId?: string }; session?: string };
      const id = typeof p.app === 'string' ? p.app : p.app?.bundleId;
      if (method === 'overlay_show' && id && p.session) this.nativeOwners.set(id, p.session);
      if (method === 'overlay_hide') { if (id) this.nativeOwners.delete(id); else this.nativeOwners.clear(); }
    }
    return result;
  }

  private async readNativeEvents(signal: AbortSignal, firstReady: () => void, firstError: (error: unknown) => void): Promise<void> {
    let connected = false;
    while (!signal.aborted && !this.closing && this.ready) {
      try {
        const response = await fetch(`${this.ready.url}/v1/events/stream?after=${this.nativeCursor}`, {
          headers: { Authorization: `Bearer ${this.token}` }, redirect: 'error', signal,
        });
        if (!response.ok || !response.body) throw new Error(`Native event stream: HTTP ${response.status}`);
        if (!connected) { connected = true; firstReady(); }
        const reader = response.body.getReader(); const decoder = new TextDecoder(); let pending = '';
        try {
          while (!signal.aborted) {
            const { value, done } = await reader.read(); if (done) break;
            pending += decoder.decode(value, { stream: true });
            if (pending.length > 8 * 1024 * 1024) throw new Error('Native event frame exceeds limit.');
            let end: number;
            while ((end = pending.indexOf('\n\n')) >= 0) {
              const frame = pending.slice(0, end); pending = pending.slice(end + 2);
              if (frame.startsWith('event: gap\n')) throw new Error('Native event history has a retention gap; revoke active grants.');
              const lines = frame.split('\n'); const id = lines.find(x => x.startsWith('id: '));
              const data = lines.filter(x => x.startsWith('data: ')).map(x => x.slice(6)).join('\n');
              if (data) {
                const event = JSON.parse(data) as { type?: string; data?: unknown };
                if (/^computer\.(cancelled|permissions_changed|app_gone)$/.test(event.type ?? '')) {
                  const notification = { method: event.type!.slice(9), params: event.data } as ComputerNotification;
                  if (notification.method === 'cancelled' || notification.method === 'app_gone') {
                    for (const [app, sid] of this.nativeOwners) if (app === notification.params.app || sid === notification.params.session) this.nativeOwners.delete(app);
                  }
                  for (const listener of this.nativeListeners) listener(notification);
                }
              }
              if (id) this.nativeCursor = Number(id.slice(4)) || this.nativeCursor;
            }
          }
        } finally { await reader.cancel().catch(() => undefined); reader.releaseLock(); }
        if (!signal.aborted) throw new Error('Native notification stream closed.');
      } catch (error) {
        if (!connected) { firstError(error); return; }
        if (signal.aborted || this.closing) return;
        // Losing the control channel must stop the runtime agent, not merely
        // hide its indicator. A new run is required after this notification.
        const sessions = new Set(this.nativeOwners.values());
        this.nativeOwners.clear();
        for (const session of sessions) for (const listener of this.nativeListeners) listener({ method: 'cancelled', params: { session, reason: 'connection_lost' } });
      }
      await new Promise<void>(resolve => {
        const done = () => { clearTimeout(timer); signal.removeEventListener('abort', done); resolve(); };
        const timer = setTimeout(done, 750); signal.addEventListener('abort', done, { once: true });
        if (signal.aborted) done();
      });
    }
  }

  async close(): Promise<void> {
    this.closing=true;this.eventQueue=[];this.nativeStream?.abort();this.nativeListeners.clear();
    const child=this.process;if(!child||child.exitCode!==null)return;
    await new Promise<void>(resolve=>{const timer=setTimeout(()=>{child.kill('SIGKILL');resolve();},10000);child.once('exit',()=>{clearTimeout(timer);resolve();});child.kill('SIGTERM');});
    this.token='';this.ready=undefined;
  }
}
