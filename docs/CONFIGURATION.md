# Configuration

## Two explicit levels

The core stores `config.json` in its data directory. The desktop also has
presentation preferences and settings of the external runtime. Configuring a model
in the core does not silently change the default model of the pi runtime.

The core configuration has five fields: `version` (1), `providers`, `mcpServers`,
`hosts`, `sessions`. The server validates the entire object before saving it. A session
belongs to its host; a Windows path is not reinterpreted as a Linux path.

## Providers

| Field | Meaning |
| --- | --- |
| `id` | Unique local identifier. |
| `kind` | `responses` or `chat-completions`. |
| `baseURL` | Base endpoint, without queries, credentials in the URL or fragment. |
| `model` | ID actually available on the chosen service. |
| `apiKeyEnv` / `apiKeyRef` | Variable name or vault reference; not both. |
| `vision` / `stream` | Features to be enabled only after a test on the model. |
| `maxOutputTokens` | Budget from 128 to 65536, further limitable by the provider. |
| `reasoningEffort` | Optional parameter, depending on the provider's capabilities. |
| `allowInsecureHTTP` | Explicit exception for HTTP outside of the loopback. |

The software does not choose credentials or models on behalf of the user. A local service
without authentication does not require a dummy key. Outside the loopback use HTTPS;
enabling an HTTP exception makes the data visible to the underlying transport.

## External MCP

Each entry uses `command` with `args`, or `url`, never both. `enabled` enables the
server in the configuration, but startup occurs with the MCP refresh. `envKeys` lists the
variables to pass to a stdio process. `tokenEnv` or `tokenRef` resolve an HTTP
credential. MCP servers are trusted code or services chosen by the user, not sandboxed
extensions just because they speak MCP.

## Sessions

A session has `id`, `name`, `workspace` and `allowExec`. The optional workspace must
be absolute and belong to the host. `allowExec: false` is the starting choice:
activating it allows execution requests subject to the core policy, it does not create
operating system isolation.

## Vault

For a host without desktop, set `SVOLO_VAULT_KEY` to an external 32-byte key
encoded in 64 hexadecimal characters. Do not write it in the repository, in `config.json`
or in the agent's workspace. Keep a separate backup of the key: losing it makes
the encrypted secrets irrecoverable. The vault does not encrypt browser profiles, cookies, conversations
and any project files.

The desktop can protect the key with the operating system mechanisms when
present. The behavior must be verified on the actual host; a key is not
considered protected simply because it is in a hidden folder.

## Examples and validation

[Initial configuration](../examples/config.example.json),
[providers](../examples/provider.example.json), [hosts](../examples/remote-host.example.json)
and [routines](../examples/inspect-and-confirm.routine.json).

The examples are newly defined project data; the fixtures in `testdata` are instead
test inputs and must not be copied to the application data directory.
