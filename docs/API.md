# Local API

The server listens on loopback. All endpoints `/v1/` require `Authorization: Bearer <token>`. A session-limited token can access the MCP server but not administrative controls. Do not expose the daemon directly to the Internet.

## Conventions

Input and output are JSON except for streams, transfers, and artifacts. Modification requests must be sent using the expected method; unknown fields are rejected in typed contracts. Errors are failed HTTP responses with a message; do not consider a timeout as proof that an external action did not occur.

## Main Endpoints

| Endpoint | Use |
| --- | --- |
| `/v1/health` | Version, protocol, and backend status. |
| `/v1/config`, `/v1/sessions` | Host configuration and sessions. |
| `/v1/tools`, `/v1/tools/call` | Discovery and explicit tool execution. |
| `/v1/control`, `/v1/runs`, `/v1/runs/stop` | Control properties and agent activity. |
| `/v1/approvals`, `/v1/tokens` | Human decisions and limited MCP credentials. |
| `/v1/events`, `/v1/events/stream`, `/v1/events/cursor` | Events and cursor recovery. |
| `/v1/view`, `/v1/input`, `/v1/browser/tabs` | Page view, input, and session tabs. |
| `/v1/artifacts`, `/v1/artifact` | List and content of artifacts. |
| `/v1/hosts/*`, `/v1/remote` | SSH management and requests to the selected daemon. |
| `/v1/transfers/*` | Staging, chunks, commits, and verified downloads. |
| `/v1/vault/*`, `/v1/credentials` | Unlocking/locking and secret reference management. |
| `/v1/projects/*`, `/v1/atp/*`, `/v1/git` | Project, workflow, and Git status. |
| `/v1/runtime/*`, `/v1/extensions/browser` | RPC runtime and desktop browser adapters. |
| `/v1/computer/*` | Policies and capabilities of native applications. |
| `/v1/mcp`, `/v1/mcp/refresh` | MCP servers and linking of external servers. |
| `/v1/bridge/*` | Desktop private channel; this is not a model tool. |

For request shapes, consult the handlers in `core/internal/server`. The patterns `/*` group explicit endpoints, they do not authorize arbitrary URLs. Do not use bridge details as a public API for extensions.

`GET /v1/browser/tabs?session=…` returns the tabs owned by the session as `[{id,url,title,type,active}]`. `active` identifies the logical target selected by the core and does not depend on the order of Chrome targets. `GET /v1/control?session=…` returns `{owner,epoch,active,tab?}`: here `active` indicates an agent operation in progress and `tab` its selected target. `GET /v1/view?session=…&tab=…` observes the indicated tab without changing it as a target, taking control, or queuing in an agent wait. Inputs and navigations must use the ID of the tab to which the action refers.

The managed browser enables `CDPScreenshotNewSurface` and captures screenshots with `fromSurface:true`. This [Chromium](https://chromium.googlesource.com/chromium/src/+/refs/heads/main/content/common/features.cc) feature requires a new compositor surface without waiting for `ForceRedraw`, which can hang when a static background tab does not present new frames. Capture keeps the tab hidden, without activating it. The native test [`TestChromiumAgedBackgroundObservation`](../core/internal/browser/screenshot_chromium_test.go) leaves B hidden for six seconds, then verifies six consecutive captures with B's pixels, focus on A, and control unchanged, using headless Chromium with the standard sandbox.

The `@documento:elemento` references are bound both to the control epoch and the snapshot tab. An incompatible `tab` ID produces `target_tab_mismatch`; a takeover or replaced document makes references obsolete. Without a valid selection, a session with multiple tabs produces `tab_ambiguous` instead of choosing the first. The model harness requires an explicit `tab` for tools operating on a page.

## Manual Call

```json
{"session":"workspace-1","name":"read-page","arguments":{}}
```

The actual CLI command format is available via `svolo-core call -h`. Consult `/v1/tools` to also include connected MCP tools, or `svolo-core catalog` to describe only built-in tools without opening data or processes.

## Built-in Catalog

The following table is derived from [tools.json](../schemas/tools.json). `readOnly` is a policy classification; it is not an assertion about the reliability of the read data.

| Operation | Read-only | Purpose |
| --- | --- | --- |
| `project-board` | no | Project-scoped Kanban, implemented in Go. action=get or apply with a validated board operation. No access to another project. |
| `project-laments` | no | Project-scoped lament reports, fixes and resolution, implemented in Go. action=get or apply; bounded report retention and explicit reopening. |
| `computer-use` | no | Native application control, off by default. Requires a separate per-app human approval. Select list_apps/get_app_state/screenshot/click/drag/scroll/type_text/press_key/set_value/select_text/perform_secondary_action/paste/end. Foreground actions need an explicit setting and an already focused app. |
| `pi-kanban` | no | Kanban operations for a connected desktop runtime session. Requires an existing pi session in Electron; native domain policy remains enforced. |
| `pi-atp` | no | Pause or resume the workflow associated with a connected desktop runtime session. |
| `pi-lament` | no | Issue-report operations for a connected desktop runtime session. Requires an existing pi session in Electron. |
| `pi-computer` | no | Desktop runtime Computer Use adapter. Preserves pi approvals; macOS uses Swift, Windows/Linux use the Go-supervised native provider. Native capabilities and limitations are reported at runtime. |
| `state` | yes | Read active tab URL, title and loading state. |
| `navigate` | no | Navigate the owned tab to http(s). Verify page state afterwards. |
| `open-browser` | no | Open a managed browser tab in this session. |
| `close-browser` | no | Close only this session's managed browser. |
| `open-tab` | no | Create another tab in the same browser session. |
| `tabs` | yes | List only the browser targets owned by this session. |
| `switch-tab` | no | Activate a tab by exact id, title or URL; ambiguous matches fail. |
| `close-tab` | no | Close the current owned tab. |
| `tab-history` | no | Go back or forward in tab history. |
| `open-in-new-tab` | no | Open an element's link or image URL in another tab. |
| `click` | no | Dispatch a click on a visible, unambiguous target. This is not a task-success assertion. |
| `type-text` | no | Type text into the current focus or an explicit target. |
| `fill` | no | Replace the text of a visible editable target. |
| `press-key` | no | Send a key, optionally Ctrl/Alt/Shift/Meta modifiers joined with +. |
| `select` | no | Choose a native select option by exact value or label. |
| `check` | no | Check a checkbox and verify its state. |
| `dialog` | no | Accept or dismiss a JavaScript dialog. |
| `drag` | no | Drag between two explicit page element targets. |
| `wait` | yes | Bounded wait for text, css, visible, url, title, hidden or image-ready. |
| `wait-for` | yes | Wait for an explicit condition for at most 60 seconds. |
| `assert-url` | yes | Verify the current URL; exact match by default. |
| `assert-title` | yes | Verify the document title. |
| `assert-visible` | yes | Verify element visibility and return geometry. |
| `assert-text` | yes | Verify a target's text, exact or contains. |
| `assert-image-ready` | yes | Verify visible image completion and nonzero natural dimensions. |
| `snapshot-interactive` | yes | Read semantic interactive elements with snapshot-scoped refs. Refresh refs after takeover or page changes. |
| `find-interactive` | yes | Search interactive elements by semantic label. |
| `read-page` | yes | Read title, URL, headings and bounded main text. |
| `inspect-inputs` | yes | Inspect visible form controls. Password values are redacted. |
| `inspect-elements` | yes | Inspect visible CSS matches, bounded to 500. |
| `element-info` | yes | Inspect state, attributes, HTML, text and geometry of one target. |
| `scroll` | no | Scroll up/down/top/bottom or to a target. |
| `query-selector` | yes | Inspect CSS matching elements. |
| `get-element` | yes | Read one target's current DOM data. |
| `accessibility-tree` | yes | Read non-ignored accessibility nodes with a hard output bound. |
| `inspect-links` | yes | Read visible links and resolved URLs. |
| `inspect-images` | yes | Read image sources and load state. |
| `upload` | no | Set a file input. File must belong to the session's registered workspace on the browser host. |
| `highlight` | no | Draw a non-interactive box highlight over a target. |
| `clear-highlight` | no | Remove the current owned highlight. |
| `evaluate-js` | no | Evaluate JavaScript in the owned page. Requires explicit approval; never runs in application UI. |
| `inject-js` | no | Run script now; optionally install it for future documents of this tab. |
| `screenshot` | yes | Capture page pixels, optionally save an immutable hashed artifact. |
| `viewport` | no | Set viewport dimensions, device scale, mobile/touch and optional user agent. |
| `inspect-network` | no | Start/stop/show bounded redacted network metadata capture. Does not capture request bodies or secrets. |
| `console` | yes | Show the console events collected after network/console capture was started. |
| `downloads` | yes | Enable managed downloads for this session and list completed files. |
| `wait-download` | yes | Wait for a non-temporary stable download newer than a timestamp, then register a copy as an artifact. |
| `verify-artifact` | yes | Verify the immutable artifact bytes, size and SHA-256. Does not invent semantic proof. |
| `record-browser` | no | Start or stop bounded page recordings (sampled continuous or action steps); ffmpeg is required for MP4 encoding. |
| `profile-import` | no | Copy a CLOSED Chromium profile into a CLOSED managed session. Credentials may not be portable between OS users. |
| `browser-task` | no | Run one browser operation in an isolated owned task session and close it unless persist is set. |
| `call-routine` | no | Run or resume a bounded persisted JSON graph. Human suspension returns a resume id; uncertain actions never replay automatically. |
| `hitl` | no | Request human intervention in the integrated approval UI. |
| `workspace-list` | yes | List files in the explicitly selected workspace. |
| `workspace-read` | yes | Read a regular file confined to the workspace; symlinks are refused. |
| `workspace-write` | no | Atomically write a workspace file after approval. |
| `workspace-exec` | no | Run an explicit program and argument vector inside a trusted workspace. Requires allowExec and approval; not an OS sandbox. |
| `browser-schema` | yes | Discover the argument schema of a named browser operation. |
| `browser-operation` | no | Execute a discovered browser primitive through the same control/policy path. |

## Delete a session

`DELETE /v1/sessions?session=ID` requires the administrative core credential and a registered, idle session. It removes persisted runs and retained journal entries for that session, closes its browser, removes its browser profile and artifacts, revokes scoped credentials, and removes its configuration entry. Shared workspace files are preserved. Other sessions and event sequence cursors remain intact. Running agents must finish cancellation cleanup before deletion.
