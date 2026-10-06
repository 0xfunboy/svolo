import { ComputerError, ComputerErrorCode, type ComputerMethod, type ComputerMethods, type ComputerNotification, type HelloResult } from '../../shared/computer';
import type { CoreHost } from '../core-host';

/** Same contract as the Swift service, backed by the Go-owned Windows/Linux
 * provider. The existing ComputerAgent remains the per-app approval gate. */
export class PortableComputerService {
  constructor(private readonly core: CoreHost) {}
  get installedApp(): string { return this.core.nativeDirectory(); }
  onNotification(listener: (notification: ComputerNotification) => void): () => void {
    return this.core.onNativeNotification(listener);
  }
  async call<M extends ComputerMethod>(method: M, params: ComputerMethods[M][0], _timeoutMs?: number): Promise<ComputerMethods[M][1]> {
    const reply = await this.core.nativeCall(method, params) as { result?: ComputerMethods[M][1]; error?: { code: number; message: string } };
    if (reply.error) throw new ComputerError(reply.error.code, reply.error.message);
    if (!Object.prototype.hasOwnProperty.call(reply, 'result')) throw new ComputerError(ComputerErrorCode.helperCrashed, 'Native provider returned no result.');
    return reply.result as ComputerMethods[M][1];
  }
  info(): Promise<HelloResult> { return this.call('hello', { token: '', protocol: 1 }); }
  async stop(): Promise<void> { await this.call('shutdown', {}).catch(() => undefined); }
}
