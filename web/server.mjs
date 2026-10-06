import http from 'node:http';
import { randomBytes, timingSafeEqual } from 'node:crypto';
import { readFileSync, existsSync, statSync, createReadStream, mkdirSync } from 'node:fs';
import { join, resolve, dirname, extname, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { Readable, pipeline } from 'node:stream';
import { openDatabase, hashPassword, verifyPassword, hashToken, safeUser, audit } from './database.mjs';
import { createEgressProxy, validatePublicURL } from './egress.mjs';
import { CorePool } from './cores.mjs';
import { createProviderService } from './providers.mjs';

const projectRoot=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const cookieName='__Host-svolo';
const readRoutes=new Set(['/v1/health','/v1/config','/v1/control','/v1/tools','/v1/browser/tabs','/v1/view','/v1/runs','/v1/approvals','/v1/events','/v1/events/cursor','/v1/artifacts','/v1/artifact','/v1/projects/board','/v1/projects/laments','/v1/transfers/status','/v1/transfers/chunk']);
const writeRoutes=new Set(['/v1/control','/v1/tools/call','/v1/input','/v1/runs','/v1/runs/stop','/v1/approvals','/v1/projects/board','/v1/projects/laments','/v1/mcp/refresh','/v1/transfers','/v1/transfers/chunk','/v1/transfers/commit','/v1/transfers/abort','/v1/transfers/download']);
const deniedTools=new Set(['workspace-exec','computer-use','profile-import','call-routine','pi-computer','pi-kanban','pi-atp','pi-lament']);
const uuid=()=>randomBytes(16).toString('hex');
function error(message,status=400){return Object.assign(new Error(message),{status});}
function json(res,status,value){res.writeHead(status,{'Content-Type':'application/json; charset=utf-8'});res.end(JSON.stringify(value));}
function constantEquals(a,b){const x=Buffer.from(String(a??'')),y=Buffer.from(String(b??''));return x.length===y.length&&timingSafeEqual(x,y);}
async function body(req,max=16*1024*1024){
  if(!/^application\/json(?:;|$)/i.test(req.headers['content-type']??''))throw error('È richiesto un corpo JSON.',415);
  const chunks=[];let size=0;for await(const chunk of req){size+=chunk.length;if(size>max)throw error('Richiesta troppo grande.',413);chunks.push(chunk);}
  try{const value=JSON.parse(Buffer.concat(chunks).toString('utf8'));if(!value||typeof value!=='object'||Array.isArray(value))throw 0;return value;}catch{throw error('JSON non valido.');}
}
function staticFile(req,res,root,relative){
  const path=resolve(root,relative);
  if(path!==root&&!path.startsWith(root+sep))throw error('Risorsa non trovata',404);
  if(!existsSync(path)||!statSync(path).isFile())throw error('Risorsa non trovata',404);
  const types={'.html':'text/html; charset=utf-8','.js':'text/javascript; charset=utf-8','.mjs':'text/javascript; charset=utf-8','.css':'text/css; charset=utf-8','.svg':'image/svg+xml','.webp':'image/webp','.png':'image/png','.ico':'image/x-icon','.json':'application/json'};
  const cache=relative.endsWith('.html')?'no-store':/\.(?:js|mjs|css)$/.test(relative)?'no-cache':'public, max-age=3600';
  res.writeHead(200,{'Content-Type':types[extname(path)]??'application/octet-stream','Content-Length':statSync(path).size,'Cache-Control':cache});
  if(req.method==='HEAD')res.end();else pipeline(createReadStream(path),res,()=>{});
}

export async function createApp(options={}) {
  const host=options.host??process.env.SVOLO_WEB_HOST??'127.0.0.1';
  if(host!=='127.0.0.1')throw new Error('Il gateway deve ascoltare sul loopback.');
  const port=Number(options.port??process.env.SVOLO_WEB_PORT??7340);
  const dataDir=options.dataDir??process.env.SVOLO_WEB_DATA;
  const keyFile=options.keyFile??process.env.SVOLO_WEB_KEY_FILE;
  const publicOrigin=options.publicOrigin??process.env.SVOLO_PUBLIC_ORIGIN??'https://svolo.eeess.cyou';
  if(!dataDir||!keyFile||new URL(publicOrigin).protocol!=='https:')throw new Error('Configura dati, chiave e origine HTTPS prima dell’avvio.');
  const {db,encrypt,decrypt}=openDatabase(dataDir,keyFile);
  const providers=options.providers??createProviderService({db,encrypt,decrypt,userStateDir:id=>join(dataDir,'users',id),workspaceRoot:join(dataDir,'users')});
  const bootstrapFile=options.bootstrapFile??process.env.SVOLO_WEB_ADMIN_FILE??join(dirname(keyFile),'web-admin.json');
  if(!db.prepare('SELECT id FROM users LIMIT 1').get()){
    if(!existsSync(bootstrapFile))throw new Error('Configura il primo amministratore con web/bootstrap.mjs.');
    const seed=JSON.parse(readFileSync(bootstrapFile,'utf8'));
    if(!/^[a-zA-Z0-9_.-]{3,32}$/.test(seed.username)||!seed.passwordHash?.startsWith('scrypt$'))throw new Error('Bootstrap amministratore non valido.');
    const id=uuid();db.prepare('INSERT INTO users(id,username,password_hash,role,created_at) VALUES(?,?,?,?,?)').run(id,seed.username,seed.passwordHash,'admin',new Date().toISOString());
    audit(db,id,'admin.created');
  }
  const importFile=process.env.SVOLO_INITIAL_IMPORT_FILE;
  if(importFile&&existsSync(importFile)&&!db.prepare("SELECT value FROM app_metadata WHERE key='initial-import'").get()){
    const admin=db.prepare("SELECT id FROM users WHERE role='admin' ORDER BY created_at LIMIT 1").get();
    const importOptions=JSON.parse(readFileSync(importFile,'utf8'));
    const result=await providers.importAdmin(admin.id,importOptions);
    db.prepare("INSERT INTO app_metadata(key,value) VALUES('initial-import',?)").run(JSON.stringify({at:new Date().toISOString(),result}));
    const list=await providers.list(admin.id);const candidate=Array.isArray(list)?list.find(p=>p.id==='gemrouter'):(list.providers??[]).find(p=>p.id==='gemrouter');
    if(candidate)db.prepare('INSERT OR REPLACE INTO user_preferences(user_id,provider_id,model) VALUES(?,?,?)').run(admin.id,candidate.id,candidate.model??candidate.models?.[0]?.id);
  }
  const proxy=await createEgressProxy();
  const cores=options.cores??new CorePool({dataDir,binary:process.env.SVOLO_CORE_BIN??join(projectRoot,'build/svolo-core-web'),chromium:process.env.SVOLO_CHROMIUM??'/usr/bin/chromium',proxyURL:proxy.url,providers,db,encrypt,decrypt,gatewayURL:`http://${host}:${port}`});
  const attempts=new Map();let pendingLogins=0;
  const cleanup=setInterval(()=>{const now=Date.now();db.prepare('DELETE FROM web_sessions WHERE expires_at<?').run(now);for(const[key,entry]of attempts)if(now-entry.at>15*60000)attempts.delete(key);},60000);cleanup.unref();
  const prefs=userId=>db.prepare('SELECT provider_id,model FROM user_preferences WHERE user_id=?').get(userId)??{};
  async function providerList(userId){
    const result=await providers.list(userId),list=Array.isArray(result)?result:(result.providers??[]),preference=prefs(userId);
    return {providers:await Promise.all(list.map(async p=>({...p,name:p.name??p.label,models:p.models?.map(m=>({...m,name:m.name??m.label})),quota:await providers.quota(userId,p.id).catch(()=>({status:'unavailable',message:'Il provider non espone la quota residua.'}))}))),defaultProvider:preference.provider_id??list.find(p=>p.configured)?.id??'',defaultModel:preference.model??list.find(p=>p.id===preference.provider_id)?.model??list[0]?.model??''};
  }
  function session(req){
    const encoded=(req.headers.cookie??'').split(';').map(s=>s.trim()).find(s=>s.startsWith(cookieName+'='))?.slice(cookieName.length+1);
    if(!encoded||!/^[a-f0-9]{64}$/.test(encoded))return null;
    const row=db.prepare('SELECT s.csrf,s.token_hash,u.id,u.username,u.role,u.disabled FROM web_sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>? AND u.disabled=0').get(hashToken(encoded),Date.now());
    return row?{user:safeUser(row),csrf:row.csrf,tokenHash:row.token_hash}:null;
  }
  function rate(req,username){
    const ip=req.headers.host===new URL(publicOrigin).host?(req.headers['cf-connecting-ip']??req.socket.remoteAddress):req.socket.remoteAddress;
    for(const key of ['ip:'+ip,'name:'+username.toLowerCase()]){
      const record=attempts.get(key)??{count:0,at:Date.now()};
      if(Date.now()-record.at>15*60000){record.count=0;record.at=Date.now();}
      record.count++;attempts.set(key,record);if(record.count>20)throw error('Troppi tentativi di accesso. Riprova tra 15 minuti.',429);
    }
    if(attempts.size>5000)attempts.delete(attempts.keys().next().value);
  }
  const server=http.createServer(async(req,res)=>{
    const requestId=uuid();
    res.setHeader('X-Request-ID',requestId);res.setHeader('X-Content-Type-Options','nosniff');res.setHeader('Referrer-Policy','same-origin');res.setHeader('X-Frame-Options','DENY');res.setHeader('Cache-Control','no-store');
    res.setHeader('Content-Security-Policy',"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self'; connect-src 'self'; frame-src 'none'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'");
    res.setHeader('Permissions-Policy','camera=(), microphone=(), geolocation=()');
    if(req.headers.host===new URL(publicOrigin).host)res.setHeader('Strict-Transport-Security','max-age=31536000; includeSubDomains');
    try{
      const validHosts=[new URL(publicOrigin).host,`${host}:${port}`];
      if(!validHosts.includes(req.headers.host))throw error('Host non autorizzato.',403);
      const origin=req.headers.origin;
      const expectedOrigin=req.headers.host===new URL(publicOrigin).host?publicOrigin:`http://${host}:${port}`;
      if(origin&&origin!==expectedOrigin)throw error('Origine non autorizzata.',403);
      const url=new URL(req.url,`http://${host}:${port}`),path=decodeURIComponent(url.pathname);
      if(path.startsWith('/internal/provider/')){
        if(req.socket.remoteAddress!=='127.0.0.1'||req.headers.host!==`${host}:${port}`||req.method!=='POST')throw error('Accesso non consentito.',403);
        const match=path.match(/^\/internal\/provider\/([a-f0-9]{32})\/([A-Za-z0-9_.-]+)\/v1\/(chat\/completions|responses)$/);
        if(!match||!cores.broker(match[1],(req.headers.authorization??'').replace(/^Bearer /,'')))throw error('Autenticazione richiesta.',401);
        const payload=await body(req);
        const controller=new AbortController();res.once('close',()=>controller.abort());
        const result=await providers.handleCompletion(match[1],match[2],payload,{signal:controller.signal});
        if(result instanceof Response){res.writeHead(result.status,{'Content-Type':result.headers.get('content-type')??'application/json'});if(result.body)pipeline(Readable.fromWeb(result.body),res,()=>{});else res.end();}
        else json(res,200,result);
        return;
      }
      if(path==='/api/health'&&req.method==='GET'){json(res,200,{ok:true,service:'svolo-web',version:'0.3.1-web',authentication:'session',productionQualified:false});return;}
      if(path==='/api/login'&&req.method==='POST'){
        const input=await body(req,4096);if(typeof input.username!=='string'||typeof input.password!=='string'||input.username.length>32||input.password.length>256)throw error('Credenziali non valide.',401);
        rate(req,input.username);if(pendingLogins>=4)throw error('Accessi occupati. Riprova.',429);
        pendingLogins++;
        let user,valid;
        try{user=db.prepare('SELECT * FROM users WHERE username=? COLLATE NOCASE').get(input.username);const fallback=db.prepare('SELECT password_hash FROM users LIMIT 1').get();valid=await verifyPassword(input.password,user?.password_hash??fallback.password_hash);}finally{pendingLogins--;}
        if(!valid||!user||user.disabled){audit(db,null,'login.failed');throw error('Nome utente o password non validi.',401);}
        const token=randomBytes(32).toString('hex'),csrf=randomBytes(32).toString('hex'),now=Date.now();
        db.prepare('INSERT INTO web_sessions(token_hash,user_id,csrf,expires_at,created_at) VALUES(?,?,?,?,?)').run(hashToken(token),user.id,csrf,now+12*3600000,now);
        db.prepare('DELETE FROM web_sessions WHERE user_id=? AND token_hash NOT IN (SELECT token_hash FROM web_sessions WHERE user_id=? ORDER BY created_at DESC LIMIT 10)').run(user.id,user.id);
        res.setHeader('Set-Cookie',`${cookieName}=${token}; Path=/; HttpOnly; Secure; SameSite=Lax; Max-Age=43200`);audit(db,user.id,'login.success');json(res,200,{user:safeUser(user),csrf});return;
      }
      if(path.startsWith('/api/')){
        const auth=session(req);if(!auth)throw error('Accedi per continuare.',401);
        if(!['GET','HEAD'].includes(req.method)&&!constantEquals(req.headers['x-csrf-token'],auth.csrf))throw error('Sessione scaduta o richiesta non autorizzata.',403);
        const user=auth.user;
        if(path==='/api/me'&&req.method==='GET'){json(res,200,{user,csrf:auth.csrf});return;}
        if(path==='/api/logout'&&req.method==='POST'){db.prepare('DELETE FROM web_sessions WHERE token_hash=?').run(auth.tokenHash);res.setHeader('Set-Cookie',`${cookieName}=; Path=/; HttpOnly; Secure; SameSite=Lax; Max-Age=0`);json(res,200,{ok:true});return;}
        if(path==='/api/providers'){
          if(req.method==='GET'){json(res,200,await providerList(user.id));return;}
          if(req.method==='POST'){
            const input=await body(req,128*1024),p=input.provider??input;
            p.label=p.label??p.name;
            if(p.kind==='chat-completions')p.kind='openai-compatible';
            if((await providers.list(user.id)).length>=32&&!(await providers.list(user.id)).some(existing=>existing.id===p.id))throw error('Massimo 32 provider per utente.');
            if(p.baseUrl??p.baseURL){
              const endpoint=p.baseUrl??p.baseURL;
              if(typeof endpoint!=='string')throw error('Endpoint non valido.');
              const previous=db.prepare('SELECT kind,base_url,metadata_json FROM svolo_web_providers WHERE user_id=? AND id=?').get(user.id,p.id);
              const importedLAN=user.role==='admin'&&previous?.kind==='gemrouter'&&JSON.parse(previous.metadata_json).imported===true&&endpoint.replace(/\/$/,'')===previous.base_url;
              if(!importedLAN)await validatePublicURL(endpoint);
            }
            await providers.save(user.id,p);const core=cores.cores.get(user.id);if(core)await cores.sync(core);audit(db,user.id,'provider.saved');json(res,200,await providerList(user.id));return;
          }
          if(req.method==='DELETE'){
            const id=url.searchParams.get('id');await providers.remove(user.id,id);const preference=prefs(user.id);if(preference.provider_id===id)db.prepare('DELETE FROM user_preferences WHERE user_id=?').run(user.id);const core=cores.cores.get(user.id);if(core)await cores.sync(core);audit(db,user.id,'provider.removed');json(res,200,await providerList(user.id));return;
          }
        }
        if(path==='/api/providers/select'&&req.method==='POST'){
          const p=await body(req,4096);await providers.selectModel(user.id,p.providerId,p.model);
          db.prepare('INSERT OR REPLACE INTO user_preferences(user_id,provider_id,model) VALUES(?,?,?)').run(user.id,p.providerId,p.model);
          const core=cores.cores.get(user.id);if(core)await cores.sync(core);json(res,200,await providerList(user.id));return;
        }
        const oauth=path.match(/^\/api\/providers\/([A-Za-z0-9_.-]+)\/oauth\/(start|callback)$/);
        if(oauth&&req.method==='POST'){
          const p=await body(req,8192),handler=oauth[2]==='start'?providers.oauthStart:providers.oauthComplete;
          if(!handler)throw error('Usa la credenziale OAuth del tuo account nella configurazione provider; accesso guidato non disponibile per questo provider.',501);
          json(res,200,await handler(user.id,oauth[1],p));return;
        }
        const oauthStatus=path.match(/^\/api\/providers\/([A-Za-z0-9_.-]+)\/oauth\/status$/);
        if(oauthStatus&&req.method==='GET'){const status=await providers.oauthLoginStatus(user.id,url.searchParams.get('loginId'));if(status.providerId!==oauthStatus[1])throw error('Login non trovato.',404);if(status.status==='completed'){const core=cores.cores.get(user.id);if(core)await cores.sync(core);}json(res,200,status);return;}
        const providerModels=path.match(/^\/api\/providers\/([A-Za-z0-9_.-]+)\/models$/);
        if(providerModels&&req.method==='POST'){await body(req,4096);await providers.models(user.id,providerModels[1],{refresh:true});json(res,200,await providerList(user.id));return;}
        if(path==='/api/mcp'){
          const mcpList=()=>({servers:db.prepare('SELECT config_json,credential_enc FROM mcp_servers WHERE user_id=?').all(user.id).map(row=>({...JSON.parse(row.config_json),configured:!!row.credential_enc}))});
          if(req.method==='GET'){json(res,200,mcpList());return;}
          if(req.method==='POST'){
            const input=await body(req,32768),s=input.server??input;
            if(!/^[A-Za-z0-9_-]{1,64}$/.test(s.id??''))throw error('Identificativo MCP non valido.');
            // No arbitrary host processes can be configured through the public app.
            if(s.command)throw error('Sul servizio web usa MCP via HTTPS; i processi locali restano nell’app desktop.');
            await validatePublicURL(s.url);
            const count=db.prepare('SELECT count(*) AS n FROM mcp_servers WHERE user_id=?').get(user.id).n;if(count>=16&&!db.prepare('SELECT id FROM mcp_servers WHERE user_id=? AND id=?').get(user.id,s.id))throw error('Massimo 16 server MCP per utente.');
            const previous=db.prepare('SELECT credential_enc FROM mcp_servers WHERE user_id=? AND id=?').get(user.id,s.id);
            const config={id:s.id,url:s.url,enabled:s.enabled!==false};
            db.prepare('INSERT OR REPLACE INTO mcp_servers(user_id,id,config_json,credential_enc) VALUES(?,?,?,?)').run(user.id,s.id,JSON.stringify(config),s.token?encrypt(s.token,user.id):previous?.credential_enc??null);
            const core=await cores.get(user);await cores.sync(core);await cores.request(core,'/v1/mcp/refresh','POST',{});audit(db,user.id,'mcp.saved');json(res,200,mcpList());return;
          }
          if(req.method==='DELETE'){db.prepare('DELETE FROM mcp_servers WHERE user_id=? AND id=?').run(user.id,url.searchParams.get('id'));const core=cores.cores.get(user.id);if(core){await cores.sync(core);await cores.request(core,'/v1/mcp/refresh','POST',{});}json(res,200,mcpList());return;}
        }
        if(path==='/api/users'){
          if(user.role!=='admin')throw error('Accesso amministratore richiesto.',403);
          if(req.method==='GET'){json(res,200,{users:db.prepare('SELECT id,username,role,disabled FROM users ORDER BY created_at').all().map(safeUser)});return;}
          if(req.method==='POST'){
            const p=await body(req,4096);if(!/^[A-Za-z0-9_.-]{3,32}$/.test(p.username??'')||!['admin','user'].includes(p.role??'user'))throw error('Nome utente o ruolo non valido.');
            if(db.prepare('SELECT id FROM users WHERE username=? COLLATE NOCASE').get(p.username))throw error('Nome utente già presente.',409);
            if(db.prepare('SELECT count(*) AS n FROM users').get().n>=100)throw error('Limite utenti raggiunto.');
            const passwordHash=await hashPassword(p.password),id=uuid();db.prepare('INSERT INTO users(id,username,password_hash,role,created_at) VALUES(?,?,?,?,?)').run(id,p.username,passwordHash,p.role??'user',new Date().toISOString());audit(db,user.id,'user.created');json(res,201,{user:{id,username:p.username,role:p.role??'user'}});return;
          }
        }
        if(path==='/api/sessions'){
          const core=await cores.get(user);
          const decorate=s=>({...s,pinned:!!db.prepare('SELECT pinned FROM chat_preferences WHERE user_id=? AND session_id=?').get(user.id,s.id)?.pinned});
          if(req.method==='GET'){json(res,200,(await cores.request(core,'/v1/sessions')).map(decorate).sort((a,b)=>Number(b.pinned)-Number(a.pinned)));return;}
          const operation=async()=>{
            const sessions=await cores.request(core,'/v1/sessions');
            if(req.method==='POST'){
              const p=await body(req,4096),name=String(p.name??'New chat').trim();
              if(!name||name.length>100)throw error('Chat names must contain 1 to 100 characters.');
              if(sessions.length>=32)throw error('Maximum 32 chats per user.');
              const id='chat-'+uuid().slice(0,20);
              json(res,201,decorate(await cores.request(core,'/v1/sessions','POST',{id,name,workspace:'/workspace',allowExec:false})));return;
            }
            const id=url.searchParams.get('id'),existing=sessions.find(s=>s.id===id);
            if(!existing)throw error('Chat not found.',404);
            if(req.method==='PATCH'){
              const p=await body(req,4096);
              if(!Object.keys(p).length||Object.keys(p).some(k=>!['name','pinned'].includes(k)))throw error('Specify a chat name or pin state.');
              if(p.name!==undefined&&(typeof p.name!=='string'||!p.name.trim()||p.name.trim().length>100))throw error('Chat names must contain 1 to 100 characters.');
              if(p.pinned!==undefined&&typeof p.pinned!=='boolean')throw error('Pin state must be a boolean.');
              let updated=existing;
              if(p.name!==undefined)updated=await cores.request(core,'/v1/sessions','POST',{...existing,name:p.name.trim()});
              if(p.pinned!==undefined)db.prepare('INSERT INTO chat_preferences(user_id,session_id,pinned) VALUES(?,?,?) ON CONFLICT(user_id,session_id) DO UPDATE SET pinned=excluded.pinned').run(user.id,id,Number(p.pinned));
              audit(db,user.id,'chat.updated');json(res,200,decorate(updated));return;
            }
            if(req.method==='DELETE'){
              if((await cores.request(core,'/v1/runs')).some(r=>r.session===id&&['running','waiting_approval'].includes(r.status)))throw error('Stop the agent before deleting this chat.',409);
              await cores.request(core,'/v1/sessions?session='+encodeURIComponent(id),'DELETE');
              db.prepare('DELETE FROM user_run_prompts WHERE user_id=? AND session_id=?').run(user.id,id);
              db.prepare('DELETE FROM chat_preferences WHERE user_id=? AND session_id=?').run(user.id,id);
              audit(db,user.id,'chat.deleted');json(res,200,{deleted:true,id});return;
            }
            throw error('Method not allowed.',405);
          };
          await (cores.withConfigLock?cores.withConfigLock(core,operation):operation());return;
        }
        if(path.startsWith('/api/core/')){
          const corePath=path.slice('/api/core'.length),reading=req.method==='GET';
          if(!(reading?readRoutes:writeRoutes).has(corePath)||(!reading&&req.method!=='POST'))throw error('Operazione non disponibile nel servizio web.',403);
          const core=await cores.get(user);let payload=reading?undefined:await body(req);
          if(corePath==='/v1/tools/call'&&deniedTools.has(payload.name))throw error('Operazione non disponibile nel servizio web.',403);
          if(corePath==='/v1/runs'&&!reading){
            if((await cores.request(core,'/v1/runs')).filter(run=>run.status==='running').length>=3)throw error('Sono già attive tre operazioni.',429);
            const preference=prefs(user.id);payload.provider=payload.provider||preference.provider_id;payload.autonomy=payload.autonomy==='browser'?'browser':'ask';payload.maxSteps=Math.min(50,Math.max(1,Number(payload.maxSteps)||20));payload.continue=payload.continue!==false;
            await cores.sync(core);
            const safeTools=(await cores.request(core,'/v1/tools')).map(tool=>tool.name).filter(name=>!deniedTools.has(name));
            if(Array.isArray(payload.allowedTools)&&payload.allowedTools.length){if(payload.allowedTools.some(name=>!safeTools.includes(name)))throw error('Strumento non disponibile nel servizio web.',403);}
            else payload.allowedTools=safeTools;
            // Approval preferences cannot widen the catalog or the executor's scope.
            if(payload.toolScope!==undefined&&(!Array.isArray(payload.toolScope)||payload.toolScope.some(name=>!safeTools.includes(name))))throw error('Ambito strumenti non disponibile nel servizio web.',403);
            payload.toolScope=payload.toolScope??safeTools;
          }
          if(corePath==='/v1/artifact'&&reading){
            const response=await fetch(core.url+corePath+url.search,{headers:{Authorization:`Bearer ${core.token}`},signal:AbortSignal.timeout(30000)});
            if(!response.ok)throw error('Artefatto non disponibile.',404);res.writeHead(200,{'Content-Type':'application/octet-stream','Content-Disposition':response.headers.get('content-disposition')??'attachment'});pipeline(Readable.fromWeb(response.body),res,()=>{});return;
          }
          const forwarding=new AbortController();
          res.once('close',()=>forwarding.abort());
          let result=await cores.request(core,corePath+url.search,req.method,payload,forwarding.signal);
          if(corePath==='/v1/runs'){
            if(reading)result=result.map(run=>{const saved=db.prepare('SELECT prompt_enc FROM user_run_prompts WHERE user_id=? AND run_id=?').get(user.id,run.id);return saved?{...run,prompt:decrypt(saved.prompt_enc,user.id)}:run;});
            else if(result.id)db.prepare('INSERT OR REPLACE INTO user_run_prompts(user_id,run_id,session_id,prompt_enc) VALUES(?,?,?,?)').run(user.id,result.id,payload.session,encrypt(payload.prompt,user.id));
          }
          if(corePath==='/v1/config')result={...result,providers:result.providers.map(({apiKeyEnv,apiKeyRef,...p})=>p),mcpServers:result.mcpServers.map(({tokenEnv,tokenRef,...s})=>s)};
          if(corePath==='/v1/tools')result=result.filter(tool=>!deniedTools.has(tool.name));
          json(res,200,result);return;
        }
        throw error('Risorsa non trovata.',404);
      }
      if(!['GET','HEAD'].includes(req.method))throw error('Metodo non consentito.',405);
      if(path.startsWith('/brand/')){staticFile(req,res,join(projectRoot,'brand'),path.slice(7));return;}
      const publicDir=join(projectRoot,'web/public');
      if(['/', '/login','/app','/settings'].includes(path)){staticFile(req,res,publicDir,'index.html');return;}
      staticFile(req,res,publicDir,path.slice(1));
    }catch(err){
      if(res.destroyed)return;
      if(res.headersSent){res.end();return;}
      const status=Number(err.status)||500;
      if(status===500)console.error(JSON.stringify({event:'request.failed',requestId,type:err.name??'Error'}));
      json(res,status,{error:status===500?'Operazione non riuscita. Riprova o contatta l’amministratore.':err.message,requestId});
    }
  });
  server.requestTimeout=30000;server.headersTimeout=10000;server.keepAliveTimeout=5000;server.maxHeadersCount=100;
  server.on('clientError',(_,socket)=>socket.end('HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n'));
  await new Promise((resolve,reject)=>{server.once('error',reject);server.listen(port,host,resolve);});
  return {server,db,cores,providers,dataDir,async close(){clearInterval(cleanup);await cores.close();proxy.close();server.closeAllConnections();await new Promise(resolve=>server.close(resolve));db.close();}};
}

if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url)){
  process.umask(0o077);
  const app=await createApp();
  console.log(JSON.stringify({event:'ready',service:'svolo-web',host:process.env.SVOLO_WEB_HOST??'127.0.0.1',port:Number(process.env.SVOLO_WEB_PORT??7340),authentication:'session'}));
  let closing=false;for(const signal of ['SIGTERM','SIGINT'])process.on(signal,async()=>{if(closing)return;closing=true;await app.close();process.exit(0);});
}
