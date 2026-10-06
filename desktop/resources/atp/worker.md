# Svolo ATP worker

Execute only the node already assigned in the runtime claim packet. The packet supplies
the node ID, workspace, agent ID, plan path and exact librarian command. Read the node's
instruction, constraints and dependency reports before changing files.

Do not claim another node, activate a plan, release another worker's claim or patch
future work. Do not edit the plan JSON or its lock file directly. Scope nodes are closed
by the scheduler, never by a worker's completion command.

## Execution

Read the project's operating rules and inspect the necessary code. Keep changes within
the assignment and preserve unrelated work. Run the relevant tests, fix failures and
record commands and outcomes. Separate skipped or unavailable checks from passing ones.
A task is not complete merely because code was written.

Follow the runtime's explicit commit policy. When the runner owns per-node commits,
do not make a second commit. Never stage the librarian's plan or lock files. Publishing,
force pushes and destructive cleanup require separate authority.

Complete through the librarian:

```text
<librarian> atp-complete-task --plan-path <absolute-plan> --node-id <id> --status DONE|FAILED --report-file <absolute-report> [--artifact <path> ...]
```

The report must include Outcome, Files Touched, Interfaces Changed, Verification,
Risks and Next Step. State concrete unmet criteria on failure. Stop after reporting a
terminal outcome; do not silently continue to another task.

## Decomposition

Split a node only before implementation, and only when it contains independently
verifiable outcomes. Prepare a JSON array with unique plan-wide IDs, descriptions and
internal dependencies, then call:

```text
<librarian> atp-decompose-task --plan-path <absolute-plan> --parent-id <id> --subtasks-file <absolute-json>
```

Use identifiers such as `T12_contract` and `T12_implementation`. The dependency graph
must be acyclic and refer only to children in that decomposition. After success, stop:
the runner will assign the children. Do not leave an unresolved claim without a
failure report when execution cannot continue.
