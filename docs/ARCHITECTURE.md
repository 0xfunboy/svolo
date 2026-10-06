# Architecture

## System boundaries

Svolo is a multi-process application with a Go core and an Electron/React desktop.
The core can run without the desktop through the authenticated HTTP console.
A remote host executes its own core; the local client connects the session via
OpenSSH. The model is a configurable service and does not own the browser state.

```text
Desktop React / Electron                Console locale
        | broker IPC                         | HTTP autenticato
        +---------------- core Go -----------+
                         |       |
                 sessioni/tool   runtime pi / workflow
                         |
                 trasporto browser
                 |               |
          Chromium gestito   adapter Electron
                         |
                  host remoti SSH
```

## State properties

`internal/store` manages atomic writes, directory locks, and the event log.
`internal/server` owns configurations, authentication, sessions, and authorizations.
`internal/agent` executes model turns and tool requests. React
screens cannot declare an action completed if the backend has not confirmed it.

`internal/browser` associates each session with authorized targets. Managed transport
starts Chromium; desktop transport forwards operations to registered Electron
contents. Document identity and control version invalidate element
references after a page change or human intervention.

## Execution and workflow

`internal/piruntime` supervises external RPC processes. `internal/atp` supervises
graph execution, workers, and their status; ATP mutations use the Python
librarian bundled in the desktop resources. Project reducers and the Go
repository manage Kanbans and issues. The renderer maintains view models and interactions;
not all application code is written in Go.

`internal/gitops` and `internal/github` restrict operations to the workspace and
authorized accounts. They do not constitute general authorization to publish changes.
`internal/workflow` executes deterministic JSON routines with duration and step limits.

## Models and tools

The `responses` and `chat-completions` adapters share a contract for turns,
tool calls, images, and text deltas. The `vision` property indicates that
the user has qualified the model for image inputs; it does not automatically verify
its capabilities. A server accepting the same HTTP format may still have different
limits for tools, streaming, and reasoning.

Built-in tools are described in [schemas/tools.json](../schemas/tools.json).
The same definition powers the API and the catalog. External MCP servers are launched or
connected only after configuration and an explicit refresh request.

## Transports and isolation

The desktop does not expose Node.js to navigated pages. The renderer uses a broker with
permitted operations; the browser control channel remains distinct from untrusted
content. The server listens on an explicit loopback IP address.
Remote hosts have independent data directories, credentials, processes, and browsers.

## Components not connected to the complete path

`internal/desktopview` contains the RFB/WebSocket transport, but this distribution does not
present it as a full integrated desktop viewer. `internal/updates` verifies signed
manifests, but a desktop update channel is not configured. `internal/quota`
is not equivalent to aggregate quotas applied to every write path.

CEF is not present. Cross-compilation of the core does not demonstrate the availability of
native control or installers on an operating system. These boundaries are also reported
in [capabilities.json](capabilities.json).
