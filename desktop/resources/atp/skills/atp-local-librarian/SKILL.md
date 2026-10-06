---
name: atp-local-librarian
description: Inspect and modify Svolo ATP plans through the local librarian, with explicit plan paths, ownership checks and versioned future patches.
---

# Local plan operations

The runtime supplies the executable and absolute plan path. Use them verbatim, without
shell-concatenating untrusted values. Read [command patterns](references/command-patterns.md)
for the supported operations. The executable's `--help` is authoritative for option names.

A plan is data with ownership and transitions, not a file to patch with string replacement.
Use the librarian for activation, claims, completion, decomposition and future patches.
An existing plan must be read before it is changed. Preserve reports and artifact paths.

Activation requires execution authority. Completion requires the exact claimed node.
Release requires the current owner and a reason. Future patches require a fresh graph
version and no conflicting claims. A lock or conflict is a reason to refresh state,
not a reason to delete the lock file.

Do not claim or release nodes when acting as a worker or orchestrator whose runtime
reserves those operations for the scheduler. Never mark an untested implementation DONE
when its acceptance checks have not passed.
