# Configuration examples

`config.example.json` is an initial configuration without providers, hosts, or secrets.
`provider.example.json` is a single entry to be customized and added to `providers`:
endpoint and model are explicit placeholders, not pre-configured services.

Remote hosts use an already verified OpenSSH alias and their daemon token resolved
via environment or vault. The example files do not contain real credentials.

For routines, copy `inspect-and-confirm.routine.json` into the `routines` directory
of the private data folder, with the name `inspect-and-confirm.json`. Create a session,
open the page, and invoke `call-routine` with `{"name":"inspect-and-confirm"}`.
A human suspension returns a resume ID; resume with `{"resume":"ID"}`
in the same session. Do not confuse these routines with `.atp.json` plans.

[Configuration](../docs/CONFIGURATION.md) · [Remote hosts](../docs/REMOTE_HOSTS.md)

`workspace-check.atp.json` is a demonstration plan in DRAFT state. It performs no work
until explicitly activated. Its authoring contract is
[task-plan.schema.json](../schemas/task-plan.schema.json); the librarian also checks
dependencies and state transitions.
