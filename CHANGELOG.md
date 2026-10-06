# Material Changes

## Web Update — 2026-10-06

- Paginated the assistant's Markdown responses, including in the history and during streaming, with contained tables/code and secure links.
- Added six resolutions: two for desktop, tablet, and mobile; selector synchronized with actual capture.

- Added default dark theme on landing, login, workspace, settings, and modals, with persistent choice.
- Separated the observed tab from the operational target: automatic agent view, free selection of tabs, and visible progress.
- Eliminated the second empty startup tab; concurrent observations without interrupting the agent.
- Bound references and actions to explicit tabs; fixed the immediate transition to human control.
- Reinforced form assistance, with current context, tool scope, outcome verifications, and resumption after an interruption.
- Verified a local module with real Gemrouter, two tabs, and no external registration.

## 0.3.1-dev — 2026-10-05

- Rewrote product, development, configuration, and security documentation.
- Applied the license provided by the owner, with separate component notices.
- Standardized application identifiers, internal references, and packaging configuration.
- Replaced all application assets with the approved Svolo graphic system.
- Added generated catalog, product checks, and asset integrity controls.
- Removed working and presentation materials not belonging to the current distribution.
- Fixed branding errors in tests and an update controller load dependent on missing metadata.
- Replaced embedded provider logos with text badges of the product.
- Fixed Windows ATP path recognition and native control names.
- Centralized the identity of Go protocols and added packaging checks against incompatible targets.
- Replaced the old publishing path with read-only planning and disabled publishing.
- Added TypeScript contract tests, negative audits, and complete librarian cycle on demonstration plans.

The checks actually performed are in `docs/verification.json`.

## Web chat management and localization

- Added per-user chat rename, pin/unpin and deletion, including browser/history cleanup.
- Added a desktop control to collapse and expand the right-hand chat column.
- Added EN/IT interface selection with English as the default and retained unsent drafts.
- Translated documentation and README into English; preserved Italian under `docs/it/`.
- Added a public, read-only Git endpoint served through Cloudflare Tunnel, with private state kept outside source.
