export type CoreTransport = (path: string, method: 'GET'|'POST'|'PUT'|'DELETE', body?: unknown) => Promise<any>;
export interface CoreConsoleOptions {
  initialSession?: string;
  initialWorkspace?: string;
  pickWorkspace?: () => Promise<string | null>;
  openNativeBrowser?: (session: string) => Promise<unknown>;
  downloadArtifact?: (host: string, session: string, id: string, name: string) => Promise<unknown>;
}
export function mountCoreConsole(root: HTMLElement, transport: CoreTransport, options?: CoreConsoleOptions): () => void;
