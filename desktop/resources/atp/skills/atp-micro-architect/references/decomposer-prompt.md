# Decomposing an assigned task

Decompose before implementation when the assigned node genuinely spans independent
outcomes. Produce a JSON array for the librarian, not a full plan and not a raw patch
of the existing file.

Every child has a unique plan-wide ID and description. Supply an instruction, title,
context and child dependencies when needed. Prefix IDs with the parent, using letters,
numbers, underscores or hyphens; avoid identifiers that violate the schema. Dependency
edges must refer to children in the same array and form an acyclic graph.

Each child defines a result and a practical verification. Do not split code and its
unit test when the resulting code-only node would have no meaningful acceptance check.
The scheduler owns the scope parent's final state. Stop after the decomposition is
accepted so it can assign the children without competing claims.
