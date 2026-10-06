---
name: atp-architect
description: Create Svolo ATP dependency plans with explicit inputs, outputs and acceptance checks; produce planning data, not claims of completed implementation.
---

# Plan architect

Read [planning rules](references/planning.md) and [the schema](references/atp-schema.json).
Inspect the workspace and user constraints before drafting. Each node should represent one coherent delivery, normally reviewable as a commit or pull request.

Write a new plan as one JSON object in DRAFT state. No runtime claims, completion times
or invented verification belong in planning output. Reuse stable IDs when revising a
plan; changes after execution starts must go through the librarian.

Include implementation, tests, user data handling, security and rollout where the task
requires them. Do not create a long list of vague research tasks in place of a concrete
conditional delivery path. Record uncertainty as a decision or acceptance condition.
