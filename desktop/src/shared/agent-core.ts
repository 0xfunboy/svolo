/** Typed desktop-to-core contract; UI callers never receive daemon credentials. */
export const CORE_IPC = {
  request: 'agent-core:request',
  status: 'agent-core:status',
  showBrowser: 'agent-core:show-browser',
  saveArtifact: 'agent-core:save-artifact',
} as const;
export type CoreMethod = 'GET' | 'POST' | 'PUT' | 'DELETE';
export interface AgentCoreApi {
  request(path: string, method: CoreMethod, body?: unknown): Promise<any>;
  status(): Promise<{ running: boolean; error?: string; browser?: string }>;
  showBrowser(session: string): Promise<void>;
  saveArtifact(host: string, session: string, id: string, name: string): Promise<void>;
}
