// Native control authority. A local takeover cannot be overwritten by a delayed
// response to a previously issued agent request. All epochs are server-issued.
export class ControlAuthority {
 private states = new Map<string,{owner:string; epoch:number; generation:number; pending:boolean}>();
 private state(id:string) { let s=this.states.get(id);if(!s){s={owner:'human',epoch:0,generation:0,pending:false};this.states.set(id,s);}return s; }
 accept(id:string,owner:string,epoch:number):boolean {
  const s=this.state(id); if(s.pending||epoch<s.epoch)return false;
  if(s.epoch!==epoch||s.owner!==owner)s.generation++;
  s.owner=owner;s.epoch=epoch;return true;
 }
 takeover(id:string):number {const s=this.state(id);s.owner='human';s.pending=true;return ++s.generation;}
 acknowledge(id:string,nonce:number,epoch:number):boolean {const s=this.state(id);if(s.generation!==nonce)return false;s.pending=false;s.owner='human';s.epoch=Math.max(s.epoch,epoch);return true;}
 lease(id:string,epoch:number,actor:string):()=>void {
  const s=this.state(id);if(!this.accept(id,actor,epoch))throw new Error('Human takeover pending; native input refused');
  const generation=s.generation;
  return ()=>{const now=this.state(id);if(now.generation!==generation||now.pending||now.epoch!==epoch||(actor==='agent'&&now.owner!=='agent'))throw new Error('Native control lease revoked; action not replayed');};
 }
 revokeAll():void { for(const [id] of this.states)this.takeover(id); }
}
