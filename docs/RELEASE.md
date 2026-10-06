# Release Requirements

## Status

This revision is a development distribution. `productionQualified` is `false` in the
product manifest. The publish command refuses to proceed; an update server is not
configured. A local build or passing Go tests does not authorize
changing this status on its own.

## Conditions to satisfy

| Scope | Required verification before a stable release |
| --- | --- |
| Core | Reproducible build, vet, race detector, negative tests, recovery after crash, and resource limits. |
| Desktop | Full typecheck, renderer/main/preload tests, build, and trial of the installed package. |
| Browser | Same session for human input and agent, popups/dialogs, downloads/uploads, authentication, and coordinated control. |
| Host | Real OpenSSH connections, reconnection, daemon update, credentials, and transfers on separate hosts. |
| Models/MCP | Real providers and authorized MCP servers; streaming, tools, errors, and images, not just simulations. |
| Platforms | Windows, macOS, Linux X11/Wayland on declared targets; native permissions and emergency stop. |
| Distribution | Tested installers, publisher signature, notarization where required, verified update channel, and rollback. |
| Security | Independent review of execution surfaces, sensitive data, and dependencies; extended tests. |

Development packages use Svolo identity and icons but do not claim to be signed
for distribution. The ad-hoc macOS configuration is not a Developer ID signature.

## Packaging commands

From `desktop`, after verifications. Each command requires the target operating system
and a corresponding x64/ARM64 Node runtime matching the included core:

```bash
pnpm dist:linux
pnpm dist:windows
pnpm dist
```

`dist` prepares the macOS package and requires native tools. Cross-compilation
of the core is separate from desktop packaging: the preliminary check rejects an unmatched host or GOOS/GOARCH variables.
`build/core/build-info.json` logs the actual target. Do not include keys or certificates in the source.

Before publishing, record the input hash, code revision, dependencies,
environment, and test outcomes. The data must refer to the actual distributed
package and include any check that was not executed.
