// Compatibility facade: the Go daemon now owns process lifetime and RPC correlation.
// The facade never spawns pi and never logs raw commands, provider keys, or transcripts.
import { randomUUID } from 'node:crypto';
import type { CoreHost } from './core-host';
import type { ExtensionUiRequest, ExtensionUiResponse, RpcCommand, RpcResponse, SessionEvent } from '../shared/protocol';
import { log } from './log';
export interface PiProcessOptions { runtimeId?:string; session?: string; cwd: string; sessionPath?: string; tag: string; args?: string[]; env?: Record<string,string>; }
export interface PiExit { code: number|null; signal: string|null; error?: string; stderrTail: string; }
export interface PiProcessHandlers { onRecords(records:(SessionEvent|ExtensionUiRequest)[]):void; onExit(exit:PiExit):void; }
let broker: CoreHost | undefined;
export function configurePiRuntime(core: CoreHost): void { broker=core; }
export class PiProcess {
  private core: CoreHost;
  private started: Promise<{id:string;pid:number}>;
  private runtimeId=''; private processId?:number; private cursor=0; private closed=false; private exited=false;
  constructor(private options:PiProcessOptions,private handlers:PiProcessHandlers){
    if(!broker) throw new Error('Svolo Go runtime broker is not configured.');
    this.core=broker;
    const boot=options.runtimeId?this.core.request('/v1/runtime').then((states:{id:string;pid:number;cwd:string}[])=>{const state=states.find(s=>s.id===options.runtimeId);if(!state||state.cwd!==options.cwd)throw new Error('Cannot attach to a runtime from another workspace');return state;}):this.core.runtimeStart({session:options.session??randomUUID().replaceAll('-',''),cwd:options.cwd,sessionPath:options.sessionPath,args:options.args,env:options.env});
    this.started=boot.then(state=>{
      this.runtimeId=state.id;this.processId=state.pid;void this.watch();return state;
    });
    void this.started.catch(error=>this.finish({code:null,signal:null,error:String(error),stderrTail:''}));
  }
  get pid():number|undefined{return this.processId;}
  async send<T=unknown>(command:RpcCommand):Promise<RpcResponse<T>>{
    try{const state=await this.started;if(this.closed||this.exited)throw new Error('pi is not running');return await this.core.request('/v1/runtime/send','POST',{id:state.id,command}) as RpcResponse<T>;}
    catch(error){return {type:'response',command:command.type,success:false,error:String(error)} as RpcResponse<T>;}
  }
  respondUi(command:ExtensionUiResponse):void{void this.started.then(state=>this.core.request('/v1/runtime/ui','POST',{id:state.id,command})).catch(error=>log.warn(this.options.tag,String(error)));}
  async close():Promise<void>{this.closed=true;if(this.options.runtimeId){this.exited=true;return;}try{const state=await this.started;await this.core.request('/v1/runtime/stop','POST',{id:state.id});}catch(error){log.warn(this.options.tag,String(error));}}
  private async watch():Promise<void>{
    while(!this.exited){
      try{
        const result=await this.core.request(`/v1/runtime/events?id=${encodeURIComponent(this.runtimeId)}&after=${this.cursor}`) as {events:{sequence:number;kind:string;record:unknown}[];cursor:number;gap:boolean;state:{status:string;exitCode?:number;error?:string}};
        if(result.gap)throw new Error('Runtime event history has a gap. No actions will be replayed.');
        const records:(SessionEvent|ExtensionUiRequest)[]=[];
        for(const e of result.events){if(e.kind==='record')records.push(e.record as SessionEvent|ExtensionUiRequest);else if(e.kind==='exit'){if(records.length)this.handlers.onRecords(records.splice(0));this.finish(e.record as PiExit);}}
        if(records.length)this.handlers.onRecords(records);this.cursor=result.cursor;
        if(result.state.status==='exited'&&!this.exited)this.finish({code:result.state.exitCode??null,signal:null,error:result.state.error,stderrTail:''});
      }catch(error){this.finish({code:null,signal:null,error:`Runtime connection lost: ${String(error)}. Reconnect explicitly; no command replay.`,stderrTail:''});}
      if(!this.exited)await new Promise(resolve=>setTimeout(resolve,150));
    }
  }
  private finish(exit:PiExit):void{if(this.exited)return;this.exited=true;this.handlers.onExit(exit);}
}

export async function detachedRuntimeCommand(session:string,command:{type:string;[key:string]:unknown}):Promise<any>{if(!broker)throw new Error('Runtime broker unavailable');return broker.detachedCommand(session,command);}
