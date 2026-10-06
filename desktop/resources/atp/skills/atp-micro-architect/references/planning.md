# Planning contract

Return a JSON object that conforms to [atp-schema.json](atp-schema.json). The root contains
`meta` and `nodes`. Use `meta.project_name`, a supported `meta.version` and
`meta.project_status: "DRAFT"`. Omit dates not known from the environment.

Each node has a stable ID, title, instruction, dependencies and status. Its instruction
states the authorized scope, required inputs, expected outputs and checks that establish
completion. Context describes facts the next worker needs, not an invented prior result.
Choose narrow steps that can be validated independently; avoid splitting a single inseparable transaction into speculative substeps.

Root nodes are READY; nodes with unresolved dependencies are LOCKED. Every dependency
must refer to another node in the same plan, and the graph must be acyclic. Do not
pre-fill worker IDs, reports, artifacts, CLAIMED or COMPLETED states.

Changing an active plan requires a fresh read, pause coordination and a versioned
librarian patch. Completed work and its evidence remain immutable. Add explicit follow-up
work when a correction is required rather than silently rewriting history.
