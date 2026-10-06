import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { HostAtpRun } from "../../../shared/atp";
import type { RpcCommand } from "../../../shared/protocol";

vi.mock("../lib/layout", () => ({ loadSidebar: () => ({ width: 268, collapsed: false }), saveSidebar: vi.fn() }));

const PLAN = "/repo/big.atp.json";
const workerModel = { provider: "test-provider", id: "worker-model", thinking: "high" } as const;
let initialRun: HostAtpRun;
let hostRuns: HostAtpRun[];
let held: string[];
const heldListeners: ((plans: string[]) => void)[] = [];
const storage = new Map<string, string>();
let app: typeof import("./app");
let atp: typeof import("./atp");

const studio = {
  command: vi.fn(async (_handle: string, cmd: RpcCommand) => ({ type: "response", command: cmd.type, success: true, data: {} })),
  openSession: vi.fn(async (_request: { handle: string; cwd: string; sessionPath?: string; runtimeId?: string; atp?: unknown }) => ({ entries: [] })),
  closeSession: vi.fn(async () => undefined),
  listSessions: vi.fn(async () => []),
  atp: {
    start: vi.fn(async (_cwd: string, _plan: string, _model?: unknown) => structuredClone(initialRun)),
    stop: vi.fn(async (_id: string) => undefined),
    runs: vi.fn(async () => structuredClone(hostRuns)),
    held: vi.fn(async () => [...held]),
    setHeld: vi.fn(async (plan: string, value: boolean) => {
      held = value ? [...new Set([...held, plan])] : held.filter((other) => other !== plan);
      for (const listener of heldListeners) listener([...held]);
    }),
    onHeld: vi.fn((listener: (plans: string[]) => void) => {
      heldListeners.push(listener);
      return () => { heldListeners.splice(heldListeners.indexOf(listener), 1); };
    }),
    onPlans: vi.fn(() => () => undefined),
    watch: vi.fn(async () => null),
    // These mutations belong to the host scheduler, not the renderer projection.
    claim: vi.fn(),
    release: vi.fn(),
    commit: vi.fn(),
  },
};

const snapshot = (patch: Partial<HostAtpRun>): HostAtpRun => ({ ...initialRun, ...patch });
const poll = () => vi.advanceTimersByTimeAsync(700);
const savedThreads = () => JSON.parse(storage.get("svolo:atp-threads") ?? "{}");

beforeEach(async () => {
  // A reload creates a new projection and polling loop, just as a new window does.
  vi.resetModules();
  vi.useFakeTimers();
  vi.clearAllMocks();
  initialRun = { id: "host-run-1", session: "core-session", cwd: "/repo", plan: PLAN, phase: "starting", since: 100, updated: 100, completed: 0 };
  hostRuns = [];
  held = [];
  heldListeners.length = 0;
  storage.clear();
  vi.stubGlobal("window", { studio });
  vi.stubGlobal("localStorage", { getItem: (key: string) => storage.get(key) ?? null, setItem: (key: string, value: string) => { storage.set(key, value); } });
  vi.stubGlobal("requestAnimationFrame", () => 1);
  vi.stubGlobal("cancelAnimationFrame", () => undefined);
  app = await import("./app");
  atp = await import("./atp");
  app.store.set((state) => ({ ...state, settings: { ...state.settings, models: { worker: workerModel } } }));
});

afterEach(() => {
  // The host watcher deliberately schedules another poll forever. Exhausting
  // timers would test that loop, rather than a bounded workflow transition.
  vi.clearAllTimers();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("the host-owned ATP runner projection", () => {
  it("starts the host workflow with the selected worker model and does not start it again while running", async () => {
    await atp.startPlan(PLAN, "/repo");
    await atp.startPlan(PLAN, "/repo");

    expect(studio.atp.start).toHaveBeenCalledExactlyOnceWith("/repo", PLAN, workerModel);
    expect(atp.atpStore.get().runners[PLAN]).toMatchObject({ id: initialRun.id, cwd: "/repo", phase: "starting", since: 100 });
    expect(studio.openSession).not.toHaveBeenCalled();
    expect(studio.atp.claim).not.toHaveBeenCalled();
    expect(studio.atp.commit).not.toHaveBeenCalled();
  });

  it("attaches each existing host worker once, remembers node threads and closes finished chats after leaving them", async () => {
    await atp.startPlan(PLAN, "/repo");
    hostRuns = [snapshot({ phase: "working", node: "T1", title: "First", runtime: "runtime-1", sessionPath: "/atp-sessions/first.jsonl", updated: 200 })];
    await poll();
    const first = atp.atpStore.get().runners[PLAN]?.handle;
    expect(first).toBeTruthy();
    expect(app.store.get().active).toBeUndefined();
    expect(studio.openSession).toHaveBeenCalledWith({ handle: first, cwd: "/repo", sessionPath: "/atp-sessions/first.jsonl", runtimeId: "runtime-1", atp: { role: "worker", plan: PLAN, node: "T1" } });
    await poll();
    expect(studio.openSession).toHaveBeenCalledOnce();

    hostRuns = [snapshot({ phase: "working", node: "T2", title: "Second", runtime: "runtime-2", sessionPath: "/atp-sessions/second.jsonl", updated: 300, completed: 1 })];
    await poll();
    const second = atp.atpStore.get().runners[PLAN]?.handle;
    expect(second).toBeTruthy();
    expect(second).not.toBe(first);
    expect(studio.closeSession).toHaveBeenCalledWith(first);
    expect(savedThreads()[PLAN]?.workers).toEqual({ T1: ["/atp-sessions/first.jsonl"], T2: ["/atp-sessions/second.jsonl"] });

    app.store.set((state) => ({ ...state, active: second }));
    hostRuns = [snapshot({ phase: "finished", updated: 400, completed: 2, message: "Finished: every node is completed." })];
    await poll();
    expect(atp.atpStore.get().runners[PLAN]).toBeUndefined();
    expect(atp.atpStore.get().notes[PLAN]).toMatchObject({ level: "info", text: "Finished: every node is completed." });
    expect(app.store.get().sessions[second!]).toBeDefined();
    expect(studio.closeSession).toHaveBeenCalledOnce();
    app.store.set((state) => ({ ...state, page: { kind: "atp", cwd: "/repo" } }));
    await vi.advanceTimersByTimeAsync(250);
    expect(studio.closeSession).toHaveBeenLastCalledWith(second);
    expect(app.store.get().sessions[second!]).toBeUndefined();
    expect(studio.command.mock.calls.some(([, cmd]) => cmd.type === "prompt")).toBe(false);
  });

  it("asks the host to stop and retains the stopping state until a terminal host snapshot arrives", async () => {
    initialRun = snapshot({ phase: "working", node: "T1" });
    await atp.startPlan(PLAN, "/repo");
    await atp.stopPlan(PLAN);
    expect(studio.atp.stop).toHaveBeenCalledExactlyOnceWith(initialRun.id);
    expect(atp.atpStore.get().runners[PLAN]?.phase).toBe("stopping");
    expect(studio.atp.release).not.toHaveBeenCalled();
    expect(studio.atp.claim).not.toHaveBeenCalled();

    hostRuns = [snapshot({ phase: "stopped", updated: 200, message: "Stopped by the user." })];
    await poll();
    expect(atp.atpStore.get().runners[PLAN]).toBeUndefined();
    expect(atp.atpStore.get().notes[PLAN]?.text).toBe("Stopped by the user.");
    await atp.stopPlan(PLAN);
    expect(studio.atp.stop).toHaveBeenCalledOnce();
  });

  it("shows an orchestrator hold and delegates resume without locally claiming or prompting a worker", async () => {
    held = [PLAN];
    initialRun = snapshot({ phase: "held" });
    await atp.startPlan(PLAN, "/repo");
    expect(atp.atpStore.get().held).toEqual([PLAN]);
    expect(atp.atpStore.get().runners[PLAN]?.phase).toBe("held");
    await atp.liftHold(PLAN);
    expect(studio.atp.setHeld).toHaveBeenCalledExactlyOnceWith(PLAN, false);
    expect(atp.atpStore.get().held).toEqual([]);
    expect(atp.atpStore.get().runners[PLAN]?.phase).toBe("held");

    hostRuns = [snapshot({ phase: "claiming", updated: 200 })];
    await poll();
    expect(atp.atpStore.get().runners[PLAN]?.phase).toBe("claiming");
    expect(studio.atp.claim).not.toHaveBeenCalled();
    expect(studio.command).not.toHaveBeenCalled();
  });

  it("recovers an existing host run after reload, ignores stale snapshots and reports interruption without restarting it", async () => {
    hostRuns = [snapshot({ phase: "working", node: "T1", runtime: "existing-runtime", sessionPath: "/atp-sessions/resumed.jsonl", updated: 300 })];
    await atp.watchProject(null);
    expect(atp.atpStore.get().runners[PLAN]).toMatchObject({ id: initialRun.id, phase: "working", runtime: "existing-runtime" });
    expect(studio.openSession.mock.calls[0]?.[0]).toMatchObject({ runtimeId: "existing-runtime", sessionPath: "/atp-sessions/resumed.jsonl" });
    hostRuns = [snapshot({ phase: "starting", updated: 200 })];
    await poll();
    expect(atp.atpStore.get().runners[PLAN]?.phase).toBe("working");

    studio.atp.runs.mockRejectedValueOnce(new Error("Host disconnected"));
    await poll();
    expect(atp.atpStore.get().runners[PLAN]?.phase).toBe("working");
    hostRuns = [snapshot({ phase: "interrupted", updated: 400, error: "Core stopped while running T1." })];
    await poll();
    expect(atp.atpStore.get().runners[PLAN]).toBeUndefined();
    expect(atp.atpStore.get().notes[PLAN]).toMatchObject({ level: "error", text: "Core stopped while running T1." });
    expect(studio.closeSession).toHaveBeenCalledOnce();
    expect(studio.atp.start).not.toHaveBeenCalled();
    expect(studio.atp.release).not.toHaveBeenCalled();
    expect(studio.atp.commit).not.toHaveBeenCalled();
  });
});
