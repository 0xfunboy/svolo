# Browser Assistance and Form Filling

The runner in `core/internal/agent/runner.go` guides agents within the shared browser
authorized by the user. The assistant can help create accounts and fill out forms:
it does not apply a general refusal to registrations or to aliases and personas chosen
explicitly by the user. It uses the provided or approved data and asks only for
missing mandatory information, after reading the actual form. It does not invent
personal data, email addresses, or identities without a mandate, and does not attribute
terms of service rules to the site without relevant evidence. An old refusal
by the assistant preserved in the conversation does not become a policy.

CAPTCHAs and access verifications require user contribution. The assistant
can ask for the code received by the user or for manual completion in the browser,
then resume the authorized procedure. It does not bypass controls and does not propose ways
to evade provider restrictions. A confirmation of human intervention does not prove
that the page is ready: the state must be read again.

## Current Context and Explicit Tabs

`Manager.BrowserContext` is an optional browser observation callback. The
server links it to `Engine.Observe(ctx, session, "tabs", nil)`. Before each request
to the provider, the runner acquires only the metadata of the tabs belonging to the
session: `id`, `url`, `title`, `type`, and `active`. It does not make a hidden call
to `Execute`, does not change the selection, and does not assume that the first tab is the
chosen one. Observation is limited to 64 tabs; title, URL, and type have size
limits. Queries, fragments, and credentials entered in the URLs are removed.

The model receives this metadata as temporary data explicitly marked untrusted.
They are not inserted into the system instructions nor accumulated in the persistent
conversation history. The event `browser.context` records the observation
with the task ID and step number. If the callback is missing, fails, or the scope
excludes `tabs`, the assistant uses the available tools to observe the browser;
no metadata or targets are invented.

For tools operating on an existing page, the catalog intended for the
model requires `arguments.tab`, containing the exact ID of an authorized tab.
The runner rejects calls lacking this field before asking for approval
or executing the tool. `switch-tab` also requires `query` equal to that ID.
`tabs`, `open-tab`, and `open-browser` do not require an already existing tab. The
API and CLI forms remain distinct from the reinforced catalog of the model.

A snapshot produces valid references only for its tab and its
document. After navigation, document change, or control handoff,
the assistant must acquire a new one. For a form, the expected path is:
observe fields and targets, enter the authorized values, follow the requested
submission and approval flow, and then verify the result in the same tab.
The response of a click indicates the sending of the action, not the success of the registration.

The events `tool.started` and `tool.finished` include task ID,
call ID, tool name, and tab ID when present. They do not include
field values, passwords, or form arguments. The tool outcome
remains in the core history according to the existing contract.

## Scope, Approvals, and Resumption

`RunRequest.ToolScope` limits the capabilities of the single task. When it is absent,
the catalog maintains existing compatibility; an empty array excludes all
tools. When present, only the specified names are exposed and the runner
rejects others before approvals and execution. Even the primitives
called via `browser-operation` and `browser-task` must belong to the
scope. `AllowedTools` retains the previous meaning of pre-approval:
it does not expand `ToolScope` and does not allow bypassing it. Wrappers that launch
autonomous workflows require a contract from their executor; the web gateway excludes
`call-routine` and other privileged tools from its allowed surface.

With `continue:true`, the runner resumes the history of the last terminal task
of the same session, provider, model, and protocol, even if deleted, failed,
or interrupted by restart. It preserves the goal and the user's data. A series of
tool calls with missing results is removed along with its
partial results and associated images, avoiding unanswered call IDs
in the provider protocol. The continuity note signals the uncertain outcome:
previous actions are not automatically replayed and the model must
verify the current page before proceeding.

Stop immediately returns control to the user. The session remains busy until
the completion of the previous execution, the persistence of the final state, and the
release of its control: a new task cannot lose its own control
due to the late closure of the previous one. A result arriving after
cancellation does not transform the task into a successful completion.
A Stop request referring to a historical or already terminal task is rejected;
it cannot take control away from a subsequent task. The control handoff
initiated by Stop must also complete before the session becomes reusable.

## Verification

```bash
cd core
GOFLAGS=-buildvcs=false go test -race -count=1 ./internal/agent
```

`runner_harness_test.go` uses HTTP providers and simulated browsers, temporary databases,
and dummy test data. It verifies the contracts in the Responses and Chat
Completions protocols, updated and non-elevated-to-policy context, scope, explicit selection,
user-provided values, missing email query, manual intervention and
subsequent verification, continuity after interruption, and concurrency in control.
These tests do not register real accounts and do not qualify the terms, forms, or
OAuth flows of external services.

`server/viewport_test.go` uses a simulated provider to verify a missing-credential
question followed by a user answer, with viewport resizing during inference and
no cancellation. `browser/viewport_chromium_test.go` separately verifies resizing,
preserved form values, stale-reference recovery, fixture password entry and tab
focus in real Chromium (`SVOLO_E2E=1`). The native web test in
`web/browser-state.test.mjs` exercises active-tab translation, both languages,
resizing without takeover, stable tab/image updates and transient capture recovery
against simulated HTTP endpoints. These checks never submit a real registration.

## Selected documents and reusable task knowledge

The web gateway adds a reviewed profile snapshot and bounded selected-document
excerpts as untrusted data. The internal MCP offers `read_document` in bounded
chunks and `propose_knowledge`; these require the current user's active run and
its persisted binding. The latter creates a pending proposal, not trusted memory.
Only user review creates a new profile revision. Profiles contain user-chosen goals
and instructions separately from reviewed knowledge; there are no built-in task
procedures. Learning updates the bound profile’s knowledge only. Proposals show
the target profile and base revision for review. Personal case values and credentials
must remain outside reusable procedures. New document selections/profile revisions
start fresh model context; procedure history remains persistent in the gateway DB.

For these runs, the gateway excludes arbitrary JavaScript, workspace operations,
browser wrappers and external MCP tools. Server-produced `uploadFiles`,
`uploadOrigins` and `taskOrigins` constrain the core. An explicit empty portal-origin
list denies task browser actions; configured origins are exact HTTPS destinations.
Uploads are always approved individually, with the destination exposed and checked
again after approval. These guards constrain the agent; an authenticated user's
manual browser controls remain available. General final-submission consent is a
harness instruction, not a universal semantic detector of every site's submit button.
See [task profiles](TASK_PROFILES.md).
