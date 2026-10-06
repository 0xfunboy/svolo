// One CDP session per tab, shared by the manager (emulation) and the agent (input, screenshots).
import type { WebContents } from "electron";
import { AsyncLocalStorage } from "node:async_hooks";
const lease = new AsyncLocalStorage<()=>void>();
export function assertNativeAction():void { lease.getStore()?.(); }
export function withNativeLease<T>(guard:()=>void, action:()=>Promise<T>):Promise<T> { return lease.run(guard,action); }

/** Send a CDP command to a tab, attaching the debugger on first use. */
export async function cdp(wc: WebContents, method: string, params: Record<string, unknown> = {}): Promise<unknown> {
  assertNativeAction();
  if (!wc.debugger.isAttached()) wc.debugger.attach("1.3");
  return wc.debugger.sendCommand(method, params);
}
