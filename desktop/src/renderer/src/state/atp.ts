// ATP UI projection. Scheduling and worker ownership are in Go, not this window. The runner works like atp-runner: per plan, claim the next node
// (main runs the librarian CLI), start a fresh worker chat with the claim packet (workerMessage), wait for its run,
// check the node, commit what the worker left, repeat until nothing is READY. Workers complete, fail or decompose
// their node themselves; nothing here judges them. ATP chats never show in the sidebar: the page opens them.
import { ATP_CONFIG, type AtpClaim, type AtpNode, type HostAtpRun, type AtpProjectPlans, type AtpSession, planName, workerMessage, workingNodes, nudgeMessage } from "../../../shared/atp";
import { taskModel } from "../../../shared/settings";
import { createStore, useStore } from "../lib/store";
import { activate, closeSession, command, interrupt, onSettle, remoteError, type Settled, startAtpChat, store as app, toast } from "./app";

/** One plan's run, while svolo runs it. */
export interface Runner {
  plan: string;
  /** The project the workers work in. */
  cwd: string;
  phase: "starting" | "claiming" | "working" | "nudging" | "committing" | "held" | "stopping";
  node?: string;
  title?: string;
  /** The current node's worker chat. */
  handle?: string;
  since: number;
  id?:string;runtime?:string;
}

/** Why a plan's run stopped by itself, or what it did last; until the plan starts again. */
export interface RunNote {
  level: "info" | "error";
  text: string;
  at: number;
}

export interface AtpState {
  /** The ATP page's project and its plans, as main pushes them. */
  project?: AtpProjectPlans;
  /** Plans an orchestrator paused (atp_pause). */
  held: string[];
  runners: Record<string, Runner>;
  notes: Record<string, RunNote>;
  /** Live orchestrator chats: by plan, or `new:<project>` for a plan the architect is still writing. */
  orchestrators: Record<string, string>;
}

export const atpStore = createStore<AtpState>({ held: [], runners: {}, notes: {}, orchestrators: {} });
export const useAtp = <S>(selector: (state: AtpState) => S): S => useStore(atpStore, selector);

const studio = () => window.studio;
const newPlanKey = (cwd: string) => `new:${cwd}`;

// ── Threads: which chats worked on what, kept in localStorage like the sidebar's pins ─────────────────────────

interface PlanThreads {
  orchestrator?: string;
  /** Node -> its worker chats' session files, oldest first (a node runs again after a stop). */
  workers: Record<string, string[]>;
}

const THREADS = "svolo:atp-threads";

function loadThreads(): Record<string, PlanThreads> {
  try {
    const saved = JSON.parse(localStorage.getItem(THREADS) ?? "null") as unknown;
    return saved && typeof saved === "object" ? (saved as Record<string, PlanThreads>) : {};
  } catch {
    return {};
  }
}

const threads = createStore<Record<string, PlanThreads>>(loadThreads());
export const useThreads = (plan: string | undefined): PlanThreads | undefined => useStore(threads, (state) => (plan ? state[plan] : undefined));

function saveThreads(key: string, update: (entry: PlanThreads) => PlanThreads): void {
  threads.set((state) => ({ ...state, [key]: update(state[key] ?? { workers: {} }) }));
  try {
    localStorage.setItem(THREADS, JSON.stringify(threads.get()));
  } catch {
    // storage unavailable: the page forgets the threads when svolo quits
  }
}

// ── Plans ────────────────────────────────────────────────────────────────────

let booted = false;
function boot(): void {
  if (booted) return;
  booted = true;
  studio().atp.onPlans((project) => {
    if (atpStore.get().project?.cwd !== project.cwd) return;
    atpStore.set((s) => ({ ...s, project }));
    adoptNewPlans(project);
  });
  studio().atp.onHeld((held) => {
    atpStore.set((s) => ({ ...s, held }));

  });
  void studio()
    .atp.held()
    .then((held) => atpStore.set((s) => ({ ...s, held })));
  void pollHostRuns();
  // Worker chats you were looking at when their node finished close once you look elsewhere.
  app.subscribe(() => {
    if (!finished.size) return;
    const { active, page } = app.get();
    for (const handle of finished) {
      if (handle === active && !page) continue;
      finished.delete(handle);
      if (app.get().sessions[handle]) void closeSession(handle, false);
    }
  });
}

/** Show a project's plans on the page (and watch them); null when the page closes. */
export async function watchProject(cwd: string | null): Promise<void> {
  boot();
  if (cwd === null) {
    atpStore.set((s) => ({ ...s, project: undefined }));
    void studio().atp.watch(null);
    return;
  }
  atpStore.set((s) => (s.project?.cwd === cwd ? s : { ...s, project: { cwd, plans: [] } }));
  try {
    const project = await studio().atp.watch(cwd);
    if (project && atpStore.get().project?.cwd === cwd) atpStore.set((s) => ({ ...s, project }));
  } catch (error) {
    toast(`Could not read the ATP plans: ${remoteError(error)}`, "error");
  }
}

// ── The runner ───────────────────────────────────────────────────────────────

// Renderer state is now only a projection of the host-owned workflow.
const finished = new Set<string>();
const shown = new Map<string,string>();
const lastRunUpdate = new Map<string,number>();
function patchRunner(plan:string,patch:Partial<Runner>):void{atpStore.set(s=>s.runners[plan]?({...s,runners:{...s.runners,[plan]:{...s.runners[plan]!,...patch}}}):s);}
function note(plan:string,level:RunNote['level'],text:string):void{atpStore.set(s=>({...s,notes:{...s.notes,[plan]:{level,text,at:Date.now()}}}));}
export async function startPlan(plan:string,cwd:string):Promise<void>{
 boot();if(atpStore.get().runners[plan])return;
 try{const run=await studio().atp.start(cwd,plan,taskModel(app.get().settings,'worker'));applyHostRun(run);}
 catch(error){note(plan,'error',remoteError(error));}
}
function applyHostRun(run:HostAtpRun):void{
 if((lastRunUpdate.get(run.id)??0)>=run.updated)return;lastRunUpdate.set(run.id,run.updated);
 const terminal=['finished','failed','stopped','interrupted'].includes(run.phase);
 if(terminal){const old=atpStore.get().runners[run.plan];if(old?.id===run.id){if(old.handle)done(old.handle);atpStore.set(s=>{const {[run.plan]:_,...runners}=s.runners;return {...s,runners};});}note(run.plan,run.error?'error':'info',run.error??run.message??run.phase);return;}
 let handle=run.runtime?shown.get(run.runtime):undefined;
 if(run.runtime&&run.sessionPath&&run.node&&!handle){handle=startAtpChat(run.cwd,{role:'worker',plan:run.plan,node:run.node},{runtimeId:run.runtime,resume:{path:run.sessionPath,title:run.title??run.node}});shown.set(run.runtime,handle);}
 const old=atpStore.get().runners[run.plan];if(old?.handle&&old.handle!==handle)done(old.handle);
 if(run.node&&run.sessionPath)saveThreads(run.plan,entry=>{const paths=entry.workers[run.node!]??[];return paths.includes(run.sessionPath!)?entry:{...entry,workers:{...entry.workers,[run.node!]:[...paths,run.sessionPath!]}};});
 atpStore.set(s=>({...s,runners:{...s.runners,[run.plan]:{id:run.id,plan:run.plan,cwd:run.cwd,phase:run.phase as Runner['phase'],node:run.node,title:run.title,handle,runtime:run.runtime,since:run.since}}}));
}
async function pollHostRuns():Promise<void>{
 try{const runs=await studio().atp.runs();for(const run of runs.sort((a,b)=>a.since-b.since))applyHostRun(run);}
 catch(error){/* A disconnected renderer never restarts a host action. */}
 finally{setTimeout(()=>void pollHostRuns(),700);}
}
function done(handle:string):void{const {active,page}=app.get();if(handle===active&&!page)finished.add(handle);else if(app.get().sessions[handle])void closeSession(handle,false);}

async function release(plan: string, node: string, reason: string): Promise<void> {
  try {
    await studio().atp.release(plan, node, ATP_CONFIG.agentId, reason);
  } catch (error) {
    toast(`Could not release ${node}: ${remoteError(error)}`, "error");
  }
}

/** Stop a plan's run: abort its worker, whose node goes back to READY. Start resumes the plan. */
export async function stopPlan(plan:string):Promise<void>{const runner=atpStore.get().runners[plan];if(!runner?.id)return;patchRunner(plan,{phase:'stopping'});try{await studio().atp.stop(runner.id);}catch(error){note(plan,'error',remoteError(error));}}

/** Give back a node svolo's worker still holds after a run that ended without it (a crash, a quit). */
export async function releaseInterrupted(plan: string, node: string): Promise<void> {
  await release(plan, node, "The user released the claim of an interrupted run in svolo.");
}

/** Let the runner claim nodes of a plan its orchestrator paused. */
export async function liftHold(plan: string): Promise<void> {
  try {
    await studio().atp.setHeld(plan, false);
  } catch (error) {
    toast(remoteError(error), "error");
  }
}

// ── Chats ────────────────────────────────────────────────────────────────────

/** Open a chat of the plan (a node's worker, its orchestrator) from its session file, as the active chat. */
export function openThread(cwd: string, path: string, title: string, atp: AtpSession): void {
  const live = Object.values(app.get().sessions).find((session) => session.sessionPath === path);
  if (live) return activate(live.handle);
  activate(startAtpChat(cwd, atp, { resume: { path, title } }));
}

/**
 * The plan's orchestrator chat, started (or resumed from its session file) when the page shows the plan. `plan`
 * undefined: a chat for a new plan, which the architect skills write.
 */
export function orchestrator(cwd: string, plan: string | undefined): string {
  boot();
  const key = plan ?? newPlanKey(cwd);
  const live = atpStore.get().orchestrators[key];
  if (live && app.get().sessions[live]) return live;
  const path = plan ? threads.get()[plan]?.orchestrator : undefined;
  const atp: AtpSession = { role: "orchestrator", plan };
  const handle = path
    ? startAtpChat(cwd, atp, { resume: { path, title: `${planName(plan as string)} orchestrator` } })
    : startAtpChat(cwd, atp, { setup: { model: taskModel(app.get().settings, "orchestrator") } });
  atpStore.set((s) => ({ ...s, orchestrators: { ...s.orchestrators, [key]: handle } }));
  // Remembered once it has a session file worth resuming: after its first run.
  const off = onSettle(handle, (how) => {
    if (how === "exited") return off();
    const sessionPath = app.get().sessions[handle]?.sessionPath;
    const owner = Object.entries(atpStore.get().orchestrators).find(([, other]) => other === handle)?.[0];
    if (sessionPath && owner && !owner.startsWith("new:")) saveThreads(owner, (entry) => ({ ...entry, orchestrator: sessionPath }));
  });
  return handle;
}

/** A new plan appeared while its project's new-plan chat ran: that chat (the architect) becomes its orchestrator. */
function adoptNewPlans(project: AtpProjectPlans): void {
  const key = newPlanKey(project.cwd);
  const handle = atpStore.get().orchestrators[key];
  const session = handle ? app.get().sessions[handle] : undefined;
  if (!handle || !session?.prompted) return;
  const known = new Set(Object.keys(atpStore.get().orchestrators));
  const adopted = project.plans.find((file) => file.plan && !known.has(file.path) && !threads.get()[file.path]?.orchestrator && file.modifiedAt >= (session.items[0]?.kind === "user" ? session.items[0].message.timestamp : 0));
  if (!adopted) return;
  atpStore.set((s) => {
    const { [key]: _new, ...rest } = s.orchestrators;
    return { ...s, orchestrators: { ...rest, [adopted.path]: handle } };
  });
  if (session.sessionPath) saveThreads(adopted.path, (entry) => ({ ...entry, orchestrator: session.sessionPath }));
}

/** Drop a new-plan chat (New ATP again, or you cancelled it), so the next one starts fresh. */
export function discardNewPlanChat(cwd: string): void {
  const key = newPlanKey(cwd);
  const handle = atpStore.get().orchestrators[key];
  atpStore.set((s) => {
    const { [key]: _gone, ...orchestrators } = s.orchestrators;
    return { ...s, orchestrators };
  });
  if (handle && app.get().sessions[handle]) void closeSession(handle, false);
}

/** The page closed: stop the orchestrators that are idle (a pi process each); busy ones once they finish. */
export function releaseOrchestrators(): void {
  for (const [key, handle] of Object.entries(atpStore.get().orchestrators)) {
    const session = app.get().sessions[handle];
    const drop = () => {
      if (app.get().page?.kind === "atp" || app.get().active === handle) return;
      atpStore.set((s) => {
        if (s.orchestrators[key] !== handle) return s;
        const { [key]: _gone, ...orchestrators } = s.orchestrators;
        return { ...s, orchestrators };
      });
      if (app.get().sessions[handle]) void closeSession(handle, false);
    };
    if (!session) drop();
    else if (session.running || session.compacting || session.dialogs.length) {
      const off = onSettle(handle, () => {
        off();
        drop();
      });
    } else drop();
  }
}
