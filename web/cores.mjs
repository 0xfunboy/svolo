import { spawn } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { mkdirSync, readFileSync, realpathSync, existsSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { createInterface } from 'node:readline';
import { fileURLToPath } from 'node:url';

const launchScript=fileURLToPath(new URL('./core-launch.sh',import.meta.url));

export class CorePool {
  constructor({dataDir,binary,chromium,proxyURL,providers,db,encrypt,decrypt,gatewayURL,maxCores=8}) {
    Object.assign(this,{dataDir,binary,chromium,proxyURL,providers,db,encrypt,decrypt,gatewayURL,maxCores});
    this.cores=new Map();this.starting=new Map();this.closed=false;
    this.timer=setInterval(()=>this.sweep(),60000);this.timer.unref();
  }
  stateDir(userId){if(!/^[a-f0-9]{32}$/.test(userId))throw new Error('Utente non valido');return join(this.dataDir,'users',userId);}
  async get(user) {
    if(this.closed)throw new Error('Applicazione in arresto');
    let core=this.cores.get(user.id);
    if(core){core.used=Date.now();return core;}
    if(this.starting.has(user.id))return this.starting.get(user.id);
    if(this.cores.size+this.starting.size>=this.maxCores)throw Object.assign(new Error('Tutti i browser disponibili sono occupati. Riprova tra poco.'),{status:503});
    const promise=this.launch(user).finally(()=>this.starting.delete(user.id));
    this.starting.set(user.id,promise);return promise;
  }
  async launch(user) {
    const root=this.stateDir(user.id),coreDir=join(root,'core'),workspace=join(root,'workspace'),home=join(root,'home');
    for(const path of [root,coreDir,workspace,home])mkdirSync(path,{recursive:true,mode:0o700});
    const chromiumDir=dirname(realpathSync(this.chromium));
    const vaultKey=randomBytes(32).toString('hex');
    // The key is durable in the app DB; it is never present in the workspace.
    const keyName='core-key:'+user.id;
    const existing=this.db.prepare('SELECT value FROM app_metadata WHERE key=?').get(keyName);
    let savedKey;
    if(existing)savedKey=this.decrypt(existing.value,user.id);else{savedKey=vaultKey;this.db.prepare('INSERT INTO app_metadata(key,value) VALUES(?,?)').run(keyName,this.encrypt(savedKey,user.id));}
    const args=['--die-with-parent','--new-session','--unshare-user','--unshare-pid','--unshare-ipc','--unshare-uts','--cap-drop','ALL','--ro-bind','/usr','/usr','--symlink','usr/bin','/bin','--symlink','usr/lib','/lib','--symlink','usr/lib64','/lib64','--proc','/proc','--dev','/dev','--tmpfs','/tmp','--dir','/etc'];
    for(const path of ['/etc/resolv.conf','/etc/hosts','/etc/nsswitch.conf','/etc/ssl/certs','/etc/fonts'])if(existsSync(path))args.push('--ro-bind',path,path);
    args.push('--ro-bind',this.binary,'/opt/svolo-core','--ro-bind',launchScript,'/opt/core-launch.sh','--ro-bind',chromiumDir,'/opt/chrome','--bind',coreDir,'/data','--bind',workspace,'/workspace','--bind',home,'/home/user','--chdir','/workspace','--setenv','HOME','/home/user','--setenv','PATH','/usr/bin:/bin','--setenv','HTTP_PROXY',this.proxyURL,'--setenv','HTTPS_PROXY',this.proxyURL,'--setenv','NO_PROXY','127.0.0.1','--','/bin/sh','/opt/core-launch.sh','--chromium-proxy',this.proxyURL);
    // flock is held for the complete PID namespace lifetime. Recovery inside
    // that namespace cannot remove a live web owner's browser lock.
    const child=spawn('/usr/bin/prlimit',['--nofile=4096:4096','--','/usr/bin/flock','--exclusive','--nonblock','--no-fork',join(coreDir,'.web-owner.lock'),'/usr/bin/bwrap',...args],{stdio:['ignore','pipe','pipe'],detached:true,env:{PATH:'/usr/bin:/bin',LANG:'C.UTF-8',USER:'svolo',LOGNAME:'svolo',SVOLO_VAULT_KEY:savedKey}});
    const core={user,child,coreDir,workspace,url:null,token:null,brokerToken:randomBytes(32).toString('hex'),used:Date.now(),lastSync:0,configVersion:0};
    let stderr='';child.stderr.on('data',chunk=>{stderr=(stderr+chunk.toString()).slice(-2000);});
    await new Promise((resolve,reject)=>{
      const lines=createInterface({input:child.stdout});
      const timer=setTimeout(()=>{child.kill('SIGTERM');reject(new Error('Avvio del workspace scaduto'));},15000);
      const fail=()=>{clearTimeout(timer);reject(new Error('Avvio del workspace non riuscito: '+stderr.replace(/[a-zA-Z0-9_-]{40,}/g,'[redacted]')));};
      child.once('error',fail);child.once('exit',fail);
      lines.on('line',line=>{try{const info=JSON.parse(line);if(info.type!=='ready')return;const url=new URL(info.url);if(url.hostname!=='127.0.0.1'||url.protocol!=='http:')throw new Error();core.url=info.url;core.token=readFileSync(join(coreDir,'auth.token'),'utf8').trim();clearTimeout(timer);child.off('exit',fail);child.off('error',fail);lines.close();resolve();}catch{fail();}});
    });
    child.on('exit',()=>{if(this.cores.get(user.id)===core)this.cores.delete(user.id);});
    this.cores.set(user.id,core);
    try{await this.sync(core);}catch(error){await this.stop(core);throw error;}
    return core;
  }
  async request(core,path,method='GET',body,signal) {
    const timeout=AbortSignal.timeout(360000);
    const response=await fetch(core.url+path,{method,headers:{Authorization:`Bearer ${core.token}`,'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body),redirect:'error',signal:signal?AbortSignal.any([signal,timeout]):timeout});
    const result=await response.json();if(!response.ok)throw Object.assign(new Error(result.error||'Operazione non riuscita'),{status:response.status===400?400:502});return result;
  }
  async withConfigLock(core, operation) {
    const previous=core.configTail??Promise.resolve();
    const next=previous.catch(()=>{}).then(operation);
    core.configTail=next;
    try{return await next;}finally{if(core.configTail===next)core.configTail=undefined;}
  }
  async sync(core) { return this.withConfigLock(core,()=>this.syncUnlocked(core)); }
  async syncUnlocked(core) {
    const config=await this.request(core,'/v1/config');
    const providers=await this.providers.coreProviders(core.user.id,{proxyBaseUrl:`${this.gatewayURL}/internal/provider/${core.user.id}`});
    for(const provider of providers){provider.apiKeyRef='web-broker';delete provider.apiKeyEnv;}
    await this.request(core,'/v1/credentials','PUT',{name:'web-broker',value:core.brokerToken});
    const mcp=this.db.prepare('SELECT config_json,credential_enc FROM mcp_servers WHERE user_id=?').all(core.user.id);
    const servers=[];
    for(const row of mcp){const config=JSON.parse(row.config_json);if(row.credential_enc){config.tokenRef='mcp-'+config.id;await this.request(core,'/v1/credentials','PUT',{name:config.tokenRef,value:this.decrypt(row.credential_enc,core.user.id)});}servers.push(config);}
    await this.request(core,'/v1/config','PUT',{version:1,providers,mcpServers:servers,hosts:[],sessions:config.sessions??[]});
    core.lastSync=Date.now();
  }
  broker(userId,token){const core=this.cores.get(userId);return core&&token?.length===64&&core.brokerToken===token?core:null;}
  async stop(core){
    if(core.stopping)return core.stopping;
    core.stopping=(async()=>{
      if(core.child.exitCode!==null||core.child.signalCode!==null){this.cores.delete(core.user.id);return;}
      const exited=new Promise(resolve=>{
        const timer=setTimeout(()=>{try{process.kill(-core.child.pid,'SIGKILL');}catch{}resolve();},12000);
        core.child.once('exit',()=>{clearTimeout(timer);resolve();});
      });
      // Ask the Go core to close CDP/Chromium and flush its store before bwrap
      // exits. Sending SIGTERM to bwrap first kills the entire PID namespace.
      try{await this.request(core,'/v1/daemon/stop','POST',{},AbortSignal.timeout(2000));}
      catch{core.child.kill('SIGTERM');}
      await exited;
      if(this.cores.get(core.user.id)===core)this.cores.delete(core.user.id);
    })();
    return core.stopping;
  }
  async sweep(){for(const core of this.cores.values())if(Date.now()-core.used>15*60*1000){try{const runs=await this.request(core,'/v1/runs');if(!runs.some(run=>run.status==='running'))await this.stop(core);}catch{await this.stop(core);}}}
  async close(){this.closed=true;clearInterval(this.timer);await Promise.allSettled([...this.starting.values()]);await Promise.all([...this.cores.values()].map(core=>this.stop(core)));}
}
