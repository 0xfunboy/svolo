# Librarian command patterns

Replace `<librarian>` with the runtime-supplied command and `<plan>` with its absolute
plan path. Use an argument vector or correct quoting. Store reports and patches in
workspace-safe or temporary files, not inside the plan's lock file.

```text
<librarian> atp-status-summary --plan-path <plan>
<librarian> atp-read-graph --plan-path <plan>
<librarian> atp-read-graph --plan-path <plan> --view-mode local --node-id <id>
<librarian> atp-activate-project --plan-path <plan> --actor-id <actor> --reason <reason>
<librarian> atp-claim-task --plan-path <plan> --agent-id <agent>
<librarian> atp-release-claim --plan-path <plan> --node-id <id> --agent-id <owner> --reason <reason>
<librarian> atp-complete-task --plan-path <plan> --node-id <id> --status DONE --report-file <report>
<librarian> atp-decompose-task --plan-path <plan> --parent-id <id> --subtasks-file <json>
<librarian> atp-apply-future-patch --plan-path <plan> --expected-graph-version <version> --patch-file <json> --reason <reason> --actor-id <actor>
```

DRAFT and PAUSED plans may be activated after approval; archived plans remain closed.
Claims are owned: releasing requires the same agent and the current CLAIMED state.
Use FAILED rather than DONE when terminal acceptance cannot be demonstrated.

A decomposition is a JSON array. Each child has a unique ID and description; add title,
instruction, context and dependencies where needed. Do not create cycles or references
to unknown children. The parent becomes a scope managed by the scheduler.

For a future patch first pause claims and read the full graph. Use only the supported
`add_nodes`, `update_nodes`, `close_nodes` and `rewire_edges` operations. Preserve completed
or running nodes. If the version changed, refresh and reconcile instead of forcing it.

A completion report states the result, changed files and interfaces, commands executed,
their results, unverified work and remaining risks. Attach only files that actually exist.
