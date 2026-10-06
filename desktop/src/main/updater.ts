/** Source builds have no configured publisher channel.
 * This controller deliberately performs no network fetch, file deletion or app replacement.
 * Signed manifest verification belongs to the Go updates package; enabling desktop delivery
 * requires a configured publisher and release qualification, not a package homepage URL.
 */
import { app } from "electron";
import type { UpdateState } from "../shared/ipc";
export class Updater {
  constructor(_logFile: string, private readonly onState: (state: UpdateState) => void) {}
  get(): UpdateState { return { phase: "idle" }; }
  start(): void { /* No automatic channel configured. */ }
  async check(): Promise<UpdateState> { const state = this.get(); this.onState(state); return state; }
  async download(): Promise<void> { throw new Error("No verified Svolo release channel is configured."); }
  restart(): void { app.relaunch(); app.quit(); }
  installOnQuit(): void { /* Source distributions never replace the installed application. */ }
}
