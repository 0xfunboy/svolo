# Web Gateway API

The contract described here is implemented in `web/server.mjs`,
`web/providers.mjs`, and `web/cores.mjs`. The Linux gateway adds multi-user
authentication and core isolation to the `0.3.1-dev` product; it does not replace
the local contract documented in [API.md](API.md). Web publishing
does not change `productionQualified`, which remains `false`.

## Transport and authentication

The gateway listens on `127.0.0.1:7340` by default; the public
client uses HTTPS via the proxy/tunnel documented in
[WEB_DEPLOYMENT.md](WEB_DEPLOYMENT.md). Only the configured public `Host`
and the gateway loopback address are accepted. If present,
`Origin` must match the origin of the requested `Host`.

Login issues the `__Host-svolo` cookie with `Path=/`, `HttpOnly`, `Secure`,
`SameSite=Lax`, and a duration of 12 hours. No core token is sent from the browser.
`GET /api/me` and login return `{user,csrf}`; all authenticated
mutations require `X-CSRF-Token` matching the session value.
Login precedes the authenticated session and does not require this header.
Public user registration is not available.

Request bodies are JSON objects with `Content-Type: application/json`, except raw attachment uploads documented below.
Normal responses are JSON, except for artifact downloads. Gateway
errors have `{error,requestId}` and `X-Request-ID`; 500 internal
errors do not expose stacks or credentials. Codes used: 400 for invalid
input/operations, 401 for missing access, 403 for forbidden origin/CSRF/operations,
404 for missing resources, 409 for conflicts, 413/415 for payload too large
or incorrect format, 429 for rate limits, 503 for busy pool. Do not interpret
a client timeout as a cancellation of an action already sent to the core.

## Access and users

| Method and path | Request | Response / authorization |
| --- | --- | --- |
| `GET /api/health` | None | Public: `{ok:true,service:'svolo-web',version:'0.3.1-web',authentication:'session',productionQualified:false}`. Checks the gateway, not all providers or browsers. |
| `POST /api/login` | `{username,password}` | `{user,csrf}` and cookie. Username maximum 32 characters; password maximum 256. |
| `GET /api/me` | Cookie | `{user:{id,username,role,disabled},csrf}`. |
| `POST /api/logout` | `{}` and CSRF | `{ok:true}`; revokes the session and clears the cookie. |
| `GET /api/users` | Admin cookie | `{users:[{id,username,role,disabled}]}`. |
| `POST /api/users` | `{username,password,role}` | HTTP 201, `{user:{id,username,role}}`. Admin only. |

User creation: username from 3 to 32 characters `[A-Za-z0-9_.-]`, unique case-insensitively;
role `user` or `admin`; password from
10 to 256 characters. The frontend requires at least 12 characters. Current limit:
100 users. No public endpoints are implemented for role modification,
password reset, or user deletion.

## Providers, models, and quotas

| Method and path | Request | Response |
| --- | --- | --- |
| `GET /api/providers` | — | `{providers,defaultProvider,defaultModel}` of the authenticated user. |
| `POST /api/providers` | `{provider:{…}}` | Same structure as GET after saving. |
| `DELETE /api/providers?id=…` | CSRF | Updated list; also removes the preference if it references the provider. |
| `POST /api/providers/select` | `{providerId,model}` | Updated list; saves the choice and synchronizes the already started core. |
| `POST /api/providers/:id/models` | `{}` | Updated list after catalog refresh. |
| `POST /api/providers/:id/oauth/start` | `{}` | For Codex: `{loginId,status:'starting'}`. |
| `GET /api/providers/:id/oauth/status?loginId=…` | — | Login status belonging to the requested user and provider. |
| `POST /api/providers/:id/oauth/callback` | `{loginId}` | Reads the status; does not accept manual OAuth tokens. |

The canonical saving fields are `id`, `label`, `kind`, `baseURL`,
`model`, `models:[{id,label,vision?}]`, and, for an API key, `apiKey`.
The gateway accepts `name` as name and `baseUrl` as endpoint; the UI uses
the canonical service contract. Supported: `gemrouter`,
`openai-compatible`, `codex-oauth`, `google-api-key`, `antigravity-oauth`.
The core receives a Chat Completions adapter to the gateway broker;
its `kind` must not be confused with the type saved in the provider service.

Omitting `apiKey` keeps the current key; the service interprets an
explicitly empty/null key as credential removal. The web client
omits secret fields left empty during edits. OAuth connects
via the provider's login: public saving does not import raw OAuth
credentials. The catalog and `configured:true` indicate configuration and
presence of the credential, not verified access to every model.

The public list includes `id`, `label`/`name`, `kind`, `model`, `baseURL`,
`models`, `configured`, `authType`, `source`, `expiresAt`, `vision`,
`maxOutputTokens`, `toolCalling`, `updatedAt`, and any `issue`,
`toolCallingDescription`, `onboarding`, and `quota`;
it does not include API keys, access tokens, or refresh tokens. Maximum 32 providers
per user. Ordinary endpoints use public HTTPS; one-time administrative
import can authorize the configured LAN Gemrouter.

The imported Gemrouter can declare `toolCalling:'text'`: the broker sends
a text catalog and converts responses into the core tool protocol
only after validation of the name and arguments. It is an adaptation for
an endpoint lacking native function calling. Codex uses `toolCalling:'native'`.
Core approvals and restrictions apply to both modes.

Codex device login normally uses the identifier `codex`. The states
are `starting`, `pending`, `completed`, `failed`, `cancelled`. While pending,
`event` may contain `{type:'device_code',userCode,verificationUri,expiresInSeconds}`;
on error, `reason` may appear. The frontend displays the code and link, then
polls the status; the server saves the resulting credential. Pending
logins are in-memory, expire, and do not survive a gateway restart.

`quota` can be `unavailable` with `reason`, `source`, and `checkedAt`, or
`available` with Codex windows in `buckets`, data from an explicit endpoint
in `data`, or rate-limit headers observed in `headers`. The cache lasts at least
60 seconds. Reading does not perform inference. Do not invent remainders
when the service returns `unavailable`. Service and provider protocol
details: [web-providers.md](web-providers.md).

## MCP and sessions

| Method and path | Request | Response |
| --- | --- | --- |
| `GET /api/mcp` | — | `{servers:[{id,url,enabled,configured}]}`. |
| `POST /api/mcp` | `{server:{id,url,enabled?,token?}}` | Updated list after core synchronization and MCP refresh. |
| `DELETE /api/mcp?id=…` | CSRF | Updated list after removal. |
| `GET /api/sessions` | — | Array of the user's core sessions. |
| `POST /api/sessions` | `{name}` | HTTP 201, session created. |

Web MCP accepts only public HTTPS URLs without credentials, query, or fragment.
`command` is rejected even for administrators. MCP identifiers
use `[A-Za-z0-9_-]`, from 1 to 64 characters; maximum 16 servers per user.
Omitting `token` preserves the previous credential. `configured` indicates
the presence of the saved credential, not proof of an active connection.
The UI's `name` field is currently not retained by the gateway.

Sessions have a gateway-generated ID, name maximum 100 characters,
workspace `/workspace` in the user's namespace, and `allowExec:false`.
It is not possible to choose an arbitrary host path from this endpoint.
Maximum 32 sessions per user.

## Core authorized proxy

The prefix is `/api/core`, followed by the exact core path. Each request
is routed to the authenticated user's core: the client does not select a
core belonging to another account. The contracts of pass-through operations
remain those of the [core](API.md); the proxy authorizes these paths:

| Method | Allowed core paths |
| --- | --- |
| GET | `/v1/health`, `/v1/config`, `/v1/control`, `/v1/tools`, `/v1/browser/tabs`, `/v1/view`, `/v1/runs`, `/v1/approvals`, `/v1/events`, `/v1/events/cursor`, `/v1/artifacts`, `/v1/artifact`, `/v1/projects/board`, `/v1/projects/laments`, `/v1/transfers/status`, `/v1/transfers/chunk` |
| POST | `/v1/control`, `/v1/tools/call`, `/v1/input`, `/v1/runs`, `/v1/runs/stop`, `/v1/approvals`, `/v1/projects/board`, `/v1/projects/laments`, `/v1/mcp/refresh`, `/v1/transfers`, `/v1/transfers/chunk`, `/v1/transfers/commit`, `/v1/transfers/abort`, `/v1/transfers/download` |

PUT configuration, vault, token, daemon stop, SSH hosts, and other core routes
are not exposed. `/v1/config` GET removes references to provider/MCP credentials.
The tools `workspace-exec`, `computer-use`,
`profile-import`, `call-routine`, `pi-computer`, `pi-kanban`, `pi-atp`,
`pi-lament` are excluded from the web catalog, manual operations, and
the set allowed to the runner.

### Chat and events

`POST /api/core/v1/runs` accepts `{session,provider?,prompt,maxSteps?,autonomy?,continue?,allowedTools?,toolScope?}`.
The gateway applies the preferred provider if omitted, autonomy `ask` or
`browser`, from 1 to 50 steps, default continuation, and allowlisted tools.
An explicit `allowedTools` must be a subset of it:
this field pre-approves tools, it does not expand the catalog. `toolScope`
restricts the model's catalog and execution to the same subset;
if omitted, it includes the allowed web tools. An item outside the
web list is rejected, even if the client attempts to pre-approve it.
The gateway limit is three runs with status `running` per core; the core
also prevents multiple concurrent runs in the same session.

`GET /api/core/v1/runs` returns runs with ID, session, provider, model,
status, timestamps, steps, text, and any error. The gateway adds `prompt`
when the prompt was saved through this endpoint; the core strips
`history` from the list response. Prompts are persisted for
`(user_id,run_id)` and encrypted with the user's context.

Provider HTTP failures also include optional
`providerError:{status,attempts,retryable}`. The error message contains no raw
upstream response body. Transient HTTP failures receive at most three attempts
within one completion deadline; authentication, transport and partial-stream
errors are not automatically retried. `provider.retry` events contain
`{runId,attempt,maxAttempts,status,delayMs}` and no prompt or tool data. This is
inference recovery, not a replay of previously executed browser actions.

`GET /api/core/v1/events?session=…&after=…` returns an array of
`{seq,at,session,type,data}` events. The client keeps the last `seq` received.
`message.delta` contains `{runId,text}`; `tool.started`/`tool.finished`
include name, call ID, and, when present, tab ID;
the client displays the activity without exposing field values. `browser.context`
describes the tab observation used by the model in that step.
Events
`run.started`, outcomes `run.*`, and `approval.requested` describe the activity.
The frontend uses polling; `/v1/events/stream` is not in the current web list.

Approvals arrive from GET `/v1/approvals` and receive
`POST /v1/approvals {id,approved}`. Consent remains external to the model.
`POST /v1/control {session,owner:'human'|'agent'}` switches control;
`POST /v1/runs/stop {id}` interrupts a run. Interruption does not roll back
already completed external transactions.

### Browser and artifacts

`GET /v1/browser/tabs?session=…` observes tabs and returns `active:true`
for the operational target selected by the core; `GET /v1/control` includes `tab`.
`GET /v1/view?session=…&tab=…` returns `{mimeType,data,tab,viewport?}`,
with base64 image and CSS viewport. This is a capture of the actual session.
It is not an iframe, a browser duplicate, or a full remote desktop.
These reads do not select the operational target, do not change control
ownership, and do not wait for the end of a long agent operation.
The frontend follows the operational target by default; selecting
a tab only fixes the local view. "Follow agent" reenables tracking.

Human input uses `POST /v1/input {session,arguments:{kind,…,tab?}}`, with
`kind` `click` (x/y), `text` (text), `key` (key), or `wheel` (x/y/deltaX/deltaY).
The core interrupts the agent and takes human control prior to input.
Manual operations use `/v1/tools/call {session,name,arguments}`:
navigation, tabs, viewports, snapshots, and screenshots maintain the core
contracts. Mutations require human control during a run; after the
explicit switch, they may proceed even if an old canceled call
is still finishing. Read-only observations do not take control.
The input tab corresponds to the shown capture, with an explicit ID.
A successful dispatch is not proof of the desired outcome on the page.

The [assistant harness](AGENT_HARNESS.md) requires explicit tab IDs
for page tools, updated references, and outcome verification.
It also assists with filling out user-authorized registration forms.

`GET /v1/artifacts?session=…` lists verified files;
`GET /v1/artifact?session=…&id=…` is the proxy's binary exception, with
`Content-Type: application/octet-stream` and core download headers.

## Reserved surface and limits

`/internal/provider/:userId/:providerId/v1/...` is a loopback broker reserved
for the core, authenticated with an internal credential and bound to the user. It is not a
public API for frontends, MCPs, or external clients. The model path used
by the core is Chat Completions; the gateway does not allow an arbitrary HTTP proxy.

Body limits: login/users/selection/sessions 4 KiB, OAuth 8 KiB, MCP
32 KiB, providers 128 KiB, generic requests 16 MiB. Process resource
limits and actual capacity are described in
[WEB_SECURITY.md](WEB_SECURITY.md). A 200 health response, a model
catalog, or a text test do not certify the full live cycle of tools,
approvals, images, and external services.

## Chat management

All chat operations require an authenticated session. Mutations also require the current `X-CSRF-Token`. Chat IDs belong to the authenticated user's isolated core; another user's ID returns 404.

| Method | Route | Request / result |
| --- | --- | --- |
| GET | `/api/sessions` | Chats with a boolean `pinned`, sorted with pinned chats first. |
| POST | `/api/sessions` | `{name}` creates a chat; names contain 1–100 characters, with a maximum of 32 chats per user. |
| PATCH | `/api/sessions?id=CHAT_ID` | `{name?,pinned?}` updates a chat; the pin value must be boolean. |
| DELETE | `/api/sessions?id=CHAT_ID` | Removes its core history, browser profile, artifacts and gateway prompt/pin rows; workspace files are retained. |

Deletion returns 409 while an agent is running or waiting for approval. Stop it and retry once cleanup completes. The core also checks the busy state under the deletion lock. Core `DELETE /v1/sessions?session=ID` provides the corresponding authenticated local operation. Configuration synchronization and chat edits are serialized per user to prevent a concurrent provider update from resurrecting a deleted chat.

## Documents and reusable task profiles

All endpoints below require the current user's cookie; mutations also require CSRF.
Ownership is checked even for administrator accounts. Profiles are distinct from
Chromium profiles. Request fields and knowledge are bounded; see
[task profiles](TASK_PROFILES.md) for retention, OCR and privacy details.

| Method and path | Request | Response / behavior |
| --- | --- | --- |
| `GET /api/documents/capabilities` | — | Supported types, OCR/PDF availability, file/page limits |
| `GET /api/task-profiles` | — | `{profiles}` owned by this account |
| `POST /api/task-profiles` | `{name,goal?,instructions?,knowledge,uploadOrigins,reviewed:true}` | Profile with revision 1 |
| `PATCH /api/task-profiles?id=…` | `{name,goal?,instructions?,knowledge,uploadOrigins,revision,reviewed:true}` | New revision; stale revision returns 409 |
| `DELETE /api/task-profiles?id=…` | — | Deletes procedure, versions and proposals; active bindings return 409 |
| `GET /api/task-profiles/versions?id=…` | — | `{versions}`, latest 20 |
| `GET /api/task-selection?session=…` | — | `{profileId}` for an owned chat |
| `POST /api/task-selection?session=…` | `{profileId}`; empty string clears | Persists selection; active chat returns 409 |
| `GET /api/task-profiles/proposals?session=…` | Optional chat filter | `{proposals}` pending user review |
| `POST /api/task-profiles/proposals?id=…` | `{approve:false}` or `{approve:true,reviewed:true,knowledge?}` | Rejects or saves a reviewed revision; stale proposal returns 409 |
| `POST /api/attachments?session=…&name=…` | Raw `application/octet-stream`, max 20 MiB | Decoded document summary; active chat returns 409 |
| `GET /api/attachments?session=…` | — | `{attachments}` metadata; no raw bytes or text |
| `GET /api/attachments/text?id=…` | — | Owned extracted text and reading metadata |
| `GET /api/attachments/download?id=…` | — | Original, forced octet-stream download |
| `DELETE /api/attachments?session=…&id=…` | — | Removes owned attachment and upload copy; active chat returns 409 |

`uploadOrigins` contains exact authorized HTTPS portal origins, without credentials,
paths, query or fragment. It constrains task browser actions as well as file uploads.
Profiles are arbitrary user-defined tasks, with no built-in templates or automatic
creation/selection. A `template` property is rejected. Names are limited to 100
characters, goals to 2,000, instructions to 10,000 and knowledge to 40,000. Each
account can own up to 1,000 profiles. Older encrypted revisions explicitly expose
empty goals/instructions; no knowledge is rewritten. Learning updates knowledge
only, preserving the profile goal, instructions and authorized origins. Versions
include all fields; duplication is a reviewed POST using the chosen source fields,
with a new ID and revision 1. Profile IDs alone never authorize access.

Changing any profile fields while it is actively bound also returns 409.

`POST /api/core/v1/runs` additionally accepts `attachments:string[]`, at most eight
IDs belonging to the same account and chat. The gateway loads the selected profile,
constructs text/image context, narrows tool scope and supplies the core policy fields.
Provider vision capability determines image inclusion; unreadable files that fit
neither text nor vision are rejected. Client-supplied `images`, `uploadFiles`,
`uploadOrigins` and `taskOrigins` cannot override this context. The stored/returned
`prompt` is the user's original message, separate from the generated file/profile
context; run metadata includes attachment summaries and task revision.

The gateway reserves MCP ID prefix `svolo-task-memory`. Its internal JSON-RPC endpoint
`POST /internal/tasks/<user-id>` accepts only gateway-loopback Host and the owning
core's broker bearer credential. It is unavailable to public-host requests or
ordinary web cookies. Tools are `read_document(session,id,offset?)` and
`propose_knowledge(session,knowledge,reason)`, authorized against an active run's
profile/selected files. General user-configured MCP servers remain external HTTPS
services and are excluded from document/profile runs.
