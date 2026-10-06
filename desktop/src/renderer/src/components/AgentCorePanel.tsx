import { useEffect, useRef } from 'react';
import { mountCoreConsole } from '../../../../../core/internal/server/web/client.js';
import '../../../../../core/internal/server/web/style.css';

/** The exact same operational console is bundled in Electron and embedded by
 * the Go daemon. This wrapper does not fetch a daemon token into React. */
export function AgentCorePanel({ session, cwd }: { session?: string; cwd?: string }) {
  const element = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!element.current) return;
    return mountCoreConsole(element.current, (path, method, body) => window.agentCore.request(path, method, body), {
      initialSession: session,
      initialWorkspace: cwd,
      pickWorkspace: () => window.studio.pickFolder(),
      openNativeBrowser: (id) => window.agentCore.showBrowser(id),
      downloadArtifact: (host, sid, id, name) => window.agentCore.saveArtifact(host, sid, id, name),
    });
  }, [session, cwd]);
  return <div ref={element} className="h-full min-h-0 min-w-0" />;
}
