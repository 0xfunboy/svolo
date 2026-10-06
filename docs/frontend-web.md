# Frontend Svolo Web

The client in `web/public` is a vanilla HTML/CSS/JavaScript modular application,
served by the `web/server.mjs` gateway. It does not require a JavaScript build. The public
landing page, login, and workspace all work with the same server; providers,
sessions, and MCP servers come from authenticated APIs, not demo data.

## Identity and Screens

The frontend mirrors the symbol, colors, and landscape approved in `brand`.
Graphite `#0f172a`, ink `#10212e`, green `#00e676`, teal `#007b53`, mist
`#e8ecef`, and white remain the reference points. The default theme is dark:
graphite/ink for surfaces, mist for text, green and mint for accents.
The light theme uses white, mist, and mint, with teal for accented text.
`product-hero.webp` on the landing page is labeled as a **concept**, not as an application
screenshot. The viewer images are instead real browser session captures provided
by the core.

Public paths: `/` and `/login`. `/app` opens the workspace and `/settings` opens
settings. Internal navigation uses the hashes `#workspace`,
`#settings/providers`, `#settings/mcp`, `#settings/users`, and `#settings/account`.
The users section is visible only to administrators. There is no public signup:
the administrator creates access accounts.
The first entry into the workspace creates an empty chat: the user can type
immediately without first configuring a session name. This does not start the model.

On desktop, the browser is on the left and the chat is on the right, as in the product reference. At widths below 901 px,
the Browser button toggles between the two views; below 620 px, the sidebar becomes
a menu. Modules include field labels, button states, error messages, visible focus,
and reduced animations according to preferences.

The theme button is available on the landing page, login, and workspace/settings bar,
even on mobile. The choice is saved in `localStorage['svolo:theme']`; `theme-init.js` applies it
before loading CSS to prevent a flash of the opposite theme. If storage is not available,
the dark theme remains usable. The theme covers forms, modals, tables, errors, toasts,
tab states, and page surfaces. Product artwork and external page screenshots
retain their original colors.

## Modules and API Contract

- `api.js`: same-origin JSON requests with cookies, CSRF, and timeout; HTML
escaping of dynamic values; representation of available quotas.
- `icons.js`: application SVG icons without external branding.
- `views.js`: screens and presentational components.
- `app.js`: authentication, routing, forms, event polling, approvals,
and browser interactions.
- `styles.css`: visual identity and responsive layout.
- `theme-init.js`: theme before first render, without credentials in storage.
- `markdown.js`: GFM assistant responses with final sanitization.
- `viewports.js`: shared catalog of the six browser dimensions.

JavaScript/CSS asset URLs and all ESM graph imports use the same version
parameter: a client with cached old assets does not mix the new DOM with previous
modules. The server can also revalidate application assets.

The client reads `/api/me`, `/api/providers`, `/api/sessions`, `/api/mcp`, and,
for administrators, `/api/users`. `/api/me` and login return
`{user,csrf}`. Mutations have `X-CSRF-Token`; login is handled by the
gateway before the authenticated session. Provider credentials are temporary form
fields, sent to the server and never saved in localStorage.
The client does not know the core administrator token.

Provider API: saving `{provider:{id,label,kind,baseURL,model,models,apiKey?}}`, selection
`{providerId,model}`, and catalog updates with `POST /api/providers/:id/models`.
Service types are `gemrouter`, `openai-compatible`,
`codex-oauth`, `google-api-key`, `antigravity-oauth`; new links with API keys use truly
supported adapters. The OAuth type is not transformed into an API key link.
Leaving a key empty during editing omits `apiKey` and preserves the
previous key. The service supports disconnection with DELETE.

Codex login uses `POST /api/providers/codex/oauth/start` and queries
`GET /api/providers/:id/oauth/status?loginId=…` every 1.5 seconds. The client displays
`event.userCode` and `event.verificationUri`; it does not require manually pasted
tokens. An already imported Codex provider keeps its own identifier.
Completion is declared only after the service state.
Antigravity not connected is not presented as a working OAuth; the UI
proposes Google Gemini configuration with real credentials.

Quotas: direct numerical residuals, observed rate-limit headers, and Codex
`primary`/`secondary` windows are shown when present. The available
Codex percentage is `100 - usedPercent`; the duration comes from the provider data.
In the absence of data, the client shows "Quota not available from provider",
without invented bars or values. Reading quotas does not start a model turn.

MCP: exclusively HTTPS forms, even for administrators, consistently with the
web service. Saving `{server:{id,name,url,token?}}`. Local commands are not
exposed by the frontend. The client distinguishes "Configured" from
"Connected": the mere presence of the record does not prove an active connection.

## Chat, Events, and Browser

The client sends `POST /api/core/v1/runs` with session, provider, prompt,
`autonomy:'ask'`, `maxSteps:20`, and `continue:true`. The model comes from
the selection saved in the provider service. Every 1.1 seconds, it reads runs,
approvals, control, and events for the current session. `message.delta` updates the
ongoing response; final outcomes and errors come from runs. The gateway adds
`run.prompt` to reconstruct saved questions upon reload: the core removes
`history` from the List response. Newly sent prompts also remain in memory
for immediate display.

Assistant responses are rendered as GitHub Flavored Markdown, both during streaming
and from history: headings, bold, emphasis, lists, static checklists, blockquotes,
tables, links, and code. The stored text remains unchanged; no heuristics are
applied to rewrite delimiters. User messages remain literal text in their
relative `pre`. Tables and code blocks have horizontal scrolling in their
own container without widening the conversation on mobile; the table container
is keyboard accessible. Code maintains indentation and literal characters.

`renderMarkdown(text)` is synchronous. It uses [Marked ](https://marked.js.org/) **18.1.0**
for GFM parsing and [DOMPurify ](https://github.com/cure53/DOMPurify) **3.4.16**
for final sanitization with a limited list of HTML tags/attributes.
Exact versions are pinned in the web package/lock and verified against the official
npm registry; ESM, source maps, and original licenses are vendored locally
in `web/public/vendor`, without CDNs. Marked alone does not sanitize HTML.
Raw response HTML is escaped as text before sanitization; scripts, handlers, styles,
frames, forms, and media are not allowed tags.
Markdown images become explicit links without automatic downloads.
Links have `target="_blank"` and `rel="noopener noreferrer"`; the decoded destination
is validated to allow only HTTP, HTTPS, mailto, and relative paths resolving to
one of these protocols. An unsupported protocol remains text. If the parser or
sanitizer is unusable, the fallback is escaped text, never executable raw HTML.

Approvals show the tool and arguments and send an explicit decision to
`/api/core/v1/approvals`. They are not approved automatically. The control button uses
`/api/core/v1/control`; human clicks and input pass through `/api/core/v1/input`,
which takes control via the core.

The browser uses screenshots from `/api/core/v1/view`, without external site iframes.
The view updates every 800 ms when visible; only one image request can be in
progress. Coordinates are converted from the displayed dimensions to the CSS
viewport returned by the server. Clicks, text, keys, wheel, tabs, navigation,
desktop/tablet/mobile viewports, and capture download are available. Manual
operations use `/api/core/v1/tools/call`. A mobile viewport changes dimensions and input;
it does not promise a device user agent that the core has not confirmed.

The selector uses six presets from the shared catalog, all with DPR 1:

| Group | CSS Dimensions | Mobile | Touch |
| --- | --- | --- | --- |
| Desktop | 1280 × 800 | No | No |
| Desktop | 1920 × 1080 | No | No |
| Tablet | 768 × 1024 | No | Yes |
| Tablet | 1536 × 2048 | No | Yes |
| Mobile | 393 × 852 | Yes | Yes |
| Mobile | 1080 × 1920 | Yes | Yes |

The selected preset is synchronized to the actual PNG dimensions; it is not deduced
from the frontend window size. Input coordinates remain referred to the session
browser viewport.

By default, the view follows the tab with `active:true` of
`GET /api/core/v1/browser/tabs`, the core's actual logical target. Selecting a tab in the UI
changes only the local view with `GET /v1/view?tab=ID`: it does not send `switch-tab`,
does not pass control, and does not alter the agent target. The "Agent" and "View"
badges distinguish the core target from the observed tab. "Follow agent" restores
automatic following; closing an observed tab returns the view to the core target.

Page mutations use the explicit ID of the observed tab. When the agent holds control,
the frontend first reclaims human control, then sends input/navigation. The control
button immediately applies the POST response; `epoch` prevents a previous
polling from overwriting the latest state. A tab switch clears the old capture
until the correct one arrives, avoiding input on the previous content. The request
for the previous capture is canceled when another tab is chosen: the new viewer
must not wait for the timeout of the abandoned tab.

`tool.started`/`tool.finished` events power an action indicator with labels such
as "Reading page" or "Filling field". The outcome is associated with
`(runId,callId)` and the tool tab; the indicator does not show arguments, typed text,
passwords, or tool results. It is a summary of observed activity, not a declaration
of the desired result.

Interruptions, including `cancelled`, have a localized explanation and invite
the user to intervene or ask to continue. The step limit is indicated as such.
Specific provider error messages remain readable, with limited length; the client
does not show the internal broker URL to explain an interruption. Technical details
remain in the server logs.

Navigation aborts requests and stops timers from the previous screen.
Model text, errors, names, and arguments are always escaped: they do not become
executable HTML. Images accept only base64 PNG/JPEG/WebP. Browser errors remain
in their view and do not falsely indicate that the chat or core has disconnected.

## Verifications Performed

Node syntax check of modules. Client smoke test in Chromium of Electron 44.5.1
with Xvfb, against the real local gateway on port 7340: landing page, login,
authenticated cookie, session, 25 imported models, providers, Codex quotas,
modals, HTTPS MCP, and user admin page. Real browser viewer verified on desktop
and mobile. No JavaScript errors and no horizontal overflow at widths 1440 and 390 px.

`frontend-web-*.png` captures and the `frontend-web-smoke.json` result are in `/home/your-user/svolo-installation`.
These are runtime client captures, distinct from brand artwork. No model turns
were created by the frontend test; any activity present in the images comes from
gateway testing. Interactive device login requires real user access; the presence
of the form does not equal the completion of a new OAuth login.

Theme verification checked 13 desktop/mobile captures and 80 visible surfaces
(75 dark and 5 light theme), including landing, login, providers, MCP, users,
account, and modal: all sampled dark theme surfaces were dark, with calculated
text/background contrast of at least 4.5:1. Toggle and persistence after reload
verified; no overflow or JavaScript errors. These are sampled checks, not a full
accessibility certification. Report `frontend-dark-smoke.json` and captures `frontend-dark-*.png`
in the verifications directory.

The `frontend-viewer-contract-smoke.json` test uses the real browser and simulated APIs to verify
automatic following, local selection without POST, distinct badges, return to follow,
immediate control with stale poll, actions associated with call ID, input on the
observed tab, and absence of sensitive arguments. Deferred scroll is discarded if
the observed tab changes in the meantime. It also verifies Italian interruption and
step limit explanations, preserving a specific provider error. It does not call
real models and does not replace real core navigation/tool tests.

The `frontend-viewer-real-smoke.json` test, against the real gateway and core, observed the two
verification form tabs: agent capture A, local pin B, return to A, and mobile viewer.
Target, owner, and epoch remained unchanged, with zero API mutations, zero JavaScript
errors, and no overflow. Captures `frontend-agent-*.png` show test runtime forms, not artwork.
The initial test recorded expired B captures at the 8-second limit, recovered by
subsequent polling. Subsequent investigation reproduced the freeze in Chromium capture
of hidden static tabs and enabled `CDPScreenshotNewSurface` with `fromSurface:true` in the core.
After deploy, `browser-aged-runtime-final.json` verifies three initial B captures after 6.5 seconds of
inactivity each: 13–14 ms, correct B image, target A and control unchanged.
Native regressions also verify that physical focus remains on A.

The joint `frontend-live-operative-watch.json` test observed a real Gemrouter turn initiated by the
gateway harness on two authorized local forms. The session started from tab B; the viewer
automatically followed A when the core chose it to fill out the form. During the turn,
206 samples were recorded with A operational and visible and 7 distinct images of A;
no turn samples had an empty viewer. The first sample with agent target A already showed A.
Captures `frontend-live-operative-A-1.png` through `frontend-live-operative-A-5.png` show the filling process, with activity
indicator and dark theme. Zero API mutations from the watcher, JavaScript errors,
and overflow. The final retarget to B is due to the harness manual verification
after the turn; the final capture follows that target. This test proves UI activity
visibility; it does not measure Chrome native focus throughout the entire turn and
remains distinct from core native regressions.

A second test with simulated APIs and the real browser verified prompt submission,
runner payload, display of persisted question, streaming, escaping of a hostile
HTML fragment, explicit consent, human input coordinates, and CSRF on every mutation.
All checks passed, without JavaScript errors. The result is in `frontend-contract-smoke.json`:
it is a UI contracts simulation, distinct from the verification of the actual
gateway and browser.

The targeted `frontend-markdown-smoke.json` test uses the real renderer in the Electron browser with
simulated APIs: historical GFM, literal user prompt, incomplete Markdown delimiters
during streaming, and reconstruction after reload. It verifies 13 hostile payloads,
links with entities, absence of execution and external downloads, long tables/code
contained at 1440 and 390 px in both themes. All six UI presets send correct
dimensions/flags/tabs/CSRF; PNG fixtures have declared real dimensions and the selector
recognizes them. These checks do not call real providers and remain distinct from
core native tests on the six viewports. Captures `frontend-markdown-*.png` are client images
with simulated data. The separate `markdown-real-history.json` test verifies a response already saved
by the real gateway, with 7 table rows, 3 headings, and 15 bold items: desktop,
mobile, and reload, without additional model turns or API mutations.

The final `frontend-viewer-after-capture-fix.json` test, after core deployment, verified A → observation B →
Follow agent A, on desktop and mobile. B remained without captures for seven seconds:
the first view arrived in 15 ms. The 19 captures took 13–26 ms, without timeouts,
JavaScript errors, overflow, or API mutations; target, owner, and epoch unchanged.

## Language and chat controls

`i18n.js` provides the UI dictionary and formatting locale. English is the default; a direct EN/IT toggle button is present on landing, login, workspace and settings. `localStorage['svolo:language']` stores only the language choice, applied before first paint by `theme-init.js`. Model responses, user messages, chat names, provider names and external page content retain their original language. Dates and quotas use the selected formatting locale.

Each sidebar chat has a keyboard-accessible actions menu: rename, pin/unpin and delete. Pins and names persist through the authenticated chat API and remain isolated by user. Deletion asks for confirmation and rejects a busy agent; workspace files are preserved. The menu is positioned outside the scrolling sidebar list so its buttons remain visible.

The desktop chat header includes a collapse button with `aria-expanded`. The hidden conversation is inert; the browser expands into the available width. A small floating button over the browser reopens the chat and receives keyboard focus when the panel closes. On mobile the existing Browser/Chat switch remains usable. `localStorage['svolo:chat-collapsed']` stores the desktop preference.

The left sidebar has its own collapse button. The compact rail retains the logo, new-chat button, settings icon and user avatar, with accessible labels and tooltips. The session list returns when the sidebar expands. `localStorage['svolo:sidebar-collapsed']` stores this preference. On mobile the full navigation remains available in the menu. Both columns can be collapsed independently. These layout changes and the language toggle do not mutate browser control or agent state. Unsent drafts survive collapsing either column and switching chats or languages within the current page.

`web/frontend.test.mjs` exercises the actual client in sandboxed Electron/Chromium against fixture APIs, covering both languages, visible chat menus, pin/rename/delete requests with CSRF, retained model/user text, draft continuity, independent desktop column collapse, floating-button visibility, compact sidebar icons, mobile navigation and saved language/layout after reload. These UI tests are separate from the gateway/core deletion tests and do not invoke real model providers.

## Documents and task memory

The composer exposes file attachment, explicit document selection, extracted-text
preview and per-chat task selection. Settings include private task CRUD, reviewed
procedure edits and revision history. The chat header can request learning; pending
proposals are editable and require explicit review before saving. Upload/learning
controls follow active-run state. Labels use the existing EN/IT dictionary and
dark/light styles. See [task profiles](TASK_PROFILES.md) and [web API](WEB_API.md).
