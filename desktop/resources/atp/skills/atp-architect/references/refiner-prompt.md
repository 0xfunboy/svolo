# Revising a plan

Read the graph, runtime ownership and current graph version. Identify exactly which
future outcomes the user wants changed. Preserve stable IDs, completed reports and
active claims. A textual request does not make claimed work disappear.

Pause through the application before submitting a future patch. Add or update only
permitted READY/LOCKED nodes, explicitly rewire their dependencies, and reject cycles.
Represent superseded future work with the supported close operation. Do not reset
completed nodes to READY or delete their artifacts to make the graph look current.

After a conflict, refresh and reconcile. Report the applied operations, remaining
blocked work and whether the plan remains paused. Resume only with continuing authority.
