# Provider in the multi-user web gateway

`web/providers.mjs` maintains the configuration, selected model, and credentials in SQLite. Each primary key is `(user_id, id)`: two users can use the same provider name without sharing accounts, keys, or preferences. The gateway authenticates users and authorizes the initial import by the administrator. The core receives only the internal credential `web-broker`; requests to the providers pass through the gateway.

## Module contract

The gateway requires Node.js 24 or later. `web/package.json` pins `@earendil-works/pi-coding-agent` to version 1.0.3 and pnpm to version 10.34.3; `web/pnpm-lock.yaml` also pins transitive dependencies. Installation and checks:

```sh
cd web
corepack pnpm install --frozen-lockfile --ignore-scripts
corepack pnpm test
corepack pnpm start
```

Dependency lifecycle scripts remain disabled. The path `ModelRuntime` used by the gateway is JavaScript and its offline import/catalog has been verified with this installation, without compiling optional bindings. The default runtime belongs to the application: `web/node_modules/@earendil-works/pi-coding-agent/dist/core/model-runtime.js`. `SVOLO_PI_MODULE` or `piModulePath` can indicate an explicit alternative module. No OAuth directory of the system user is used as a runtime dependency. The quota Codex CLI instead retains the stable binary configurable via `SVOLO_CODEX_BIN`/`codexBin`.

```js
const providers = createProviderService({
  db,                 // node:sqlite DatabaseSync
  encrypt, decrypt,   // string -> string; secondo argomento opzionale userId
  userStateDir,       // root dei dati oppure function(userId) -> directory
  workspaceRoot,
  // Optional deployment paths, independent of source credential files:
  piModulePath, codexBin,
});
```

The module creates `svolo_web_providers`; `credential_enc` contains only the result of the encryptor provided by the gateway. Model, catalog, and metadata do not contain access tokens, refresh tokens, or API keys. The master key and its protection belong to the gateway.

| Method | Result and use |
| --- | --- |
| `list(userId)`, `get(userId,id)` | Public configurations, without credentials. |
| `save(userId,input)` | Saves a provider; omitting `apiKey` keeps the key, an empty value disconnects it. Types: `openai-compatible`, `gemrouter`, `codex-oauth`, `google-api-key`, `antigravity-oauth`. |
| `selectModel(userId,id,model)` | Only changes model and vision capabilities; does not modify the encrypted credential. |
| `remove(userId,id)` | Deletes exclusively the user's provider. |
| `models(userId,id,{refresh})` | Catalog saved or updated; returns `{models,source,selectedModel}`. |
| `importAdmin(userId,{gemrouterEnv,codexAuth})` | Authorized one-time import; returns `{imported,unavailable,providers}`. |
| `quota(userId,id)` | Quotas read from the provider or status `unavailable` with reason; cache of at least 60 seconds. |
| `coreProviders(userId,{proxyBaseUrl})` | `proxyBaseUrl` is already `/internal/provider/${userId}`; each core uses `${proxyBaseUrl}/${providerId}/v1`. |
| `handleCompletion(userId,id,payload,{signal})` | `Response` for OpenAI-compatible/SSE; ChatCompletion object for OAuth with `stream:false`. |
| `beginOAuthLogin(userId,id)`, alias `oauthStart` | Starts Codex device login and returns `loginId`. |
| `oauthLoginStatus(userId,loginId)` | Device status and code/URL; visible only to the user who started the login. |
| `cancelOAuthLogin(userId,loginId)` | Cancels the login. |

`oauthComplete`/`oAuthComplete` are a status check of the device login, not an endpoint that accepts tokens from the browser. The client starts the login, queries the status, and displays `event.userCode` and `event.verificationUri`. The provider completes the login via polling; the server saves the resulting credential in SQLite.

The gateway validates public HTTPS endpoints for all users. Only the administrator's importer can use the authorized LAN Gemrouter. LAN exceptions are bound to `privateOrigin`, saved only by the import backend and preserved when the endpoint changes; a new origin uses public DNS with a bound IP connection. Requests disable redirects, so the key is not forwarded to another origin. Any `quotaURL` must belong to the same origin as the provider.

## Accounts and initial import

The importer reads only `LLM_BASE_URL`, `LLM_MODEL`, and `LLM_API_KEY` from the authorized `.env` file. The default model is the real value of `LLM_MODEL`. A `GET /v1/models` response updates the catalog without performing a model turn. The configuration observed on this host uses LAN Gemrouter and `gemini-3.8-flash`; no keys are reported in the results or logs.

Codex `auth.json` is converted into the canonical pi form: `{type:'oauth',access,refresh,expires,accountId,idToken?}`. Tokens and account IDs remain inside the encrypted credential. Repeated import does not overwrite an already existing account nor rotated tokens. Source files are not saved as pointers and are not read during subsequent requests. Import paths are explicit administrator options; the module does not contain default paths to `.env` or auth files external to the service.

The presence of `.gemini/antigravity-ide` or a remote IDE server token does not demonstrate the availability of an Antigravity OAuth. No compatible OAuth export was found on this host: no `oauth_creds.json`, empty auth pi, and no `state.vscdb` in the verified standard locations. The Antigravity provider appears as **not connected**, with an explicit reason. A generic Google OAuth is not renamed to Antigravity. The installed pi version, 1.0.3, includes Codex OAuth but not an Antigravity/Gemini CLI OAuth adapter. During initial discovery, only the launchers of the remote IDE server `agy-ide`/`antigravity-ide` were present. The official CLI `agy` 1.2.17 was then prepared in the local toolchain, verifying SHA512 against the manifest published by the Google installer. `agy --version` and `agy --help` work; reading `agy models` requires authentication and does not return models. The public record includes instructions and official documentation for this state. The [Antigravity Agent via Gemini API](https://ai.google.dev/gemini-api/docs/antigravity-agent) uses a Gemini API key and a Google-managed sandbox; it is not equivalent to OAuth or the IDE subscription.

To complete the CLI login on this host, the user can start in an interactive SSH terminal:

```sh
AGY_CLI_DISABLE_AUTO_UPDATE=true agy
```

The [official installation and authentication procedure](https://www.antigravity.google/docs/cli/install/) describes the URL step in the browser, the code to be copied back to the terminal, and the Linux keyring storage. The authorization code must not be sent in chat or logs. After logging in, `/usage` reads native quotas. This login enables the CLI for one's own account; it does not automatically link the account to the multi-user Svolo DB. The transition to the DB requires a supported adapter/export and the separation of credentials for each web user. The verified documented interfaces do not establish such an export contract. Svolo therefore maintains the disconnected Antigravity state, even if a CLI login is completed separately.

New users configure their own Gemini API key or perform the Codex device login from Svolo. Device login requires enablement provided by the ChatGPT account or workspace. See the [official OpenAI documentation on authentication](https://learn.chatgpt.com/docs/auth).

## Execution and limits

The core retries explicit HTTP 429, 500, 502, 503 and 504 responses at most twice
(three requests total), retaining the same model, history and tool results.
Default waits are one and two seconds. `Retry-After` seconds or HTTP dates are
honored when the wait fits within 30 seconds; a longer requested wait ends the
run rather than retry early. A completion has a total 180-second deadline.
Stop cancels both requests and backoff. Transport failures, authentication errors,
partial streams and previously completed tools are not automatically replayed.
Retries do not consume extra agent steps or change the selected provider.

`provider.retry` events carry only run ID, attempt, maximum attempts, HTTP status
and wait duration. Exhausted HTTP failures persist a sanitized `providerError`
object, never the upstream error body, which could echo credentials or prompts.
The web chat shows a translated explanation and **Continue task** for the latest
transient failure when provider, model, selected documents and reviewed profile
still match. Continuing is an explicit user action: it retains history, observes
the current page and preserves an unsent draft. It does not resend an old click
or submission automatically. Old stored HTTP errors also receive friendly labels.

OpenAI-compatible and endpoints with native function calling pass the Chat Completions body to the provider and forward the JSON or SSE response. The LAN Gemrouter endpoint imported on this host responded with HTTP 400 with `Tool calling is not supported on this router surface` when receiving the native catalog; a minimal chat request to the same model returns `SVOLO_OK`. Its persistent configuration therefore uses `toolCalling: 'text'`, which is also exposed in the public configuration: **tools use a textual protocol, not native function calling**.

This bridge transmits the catalog as JSON instructions in the system message and requires a complete `{"svolo_tool_calls":[{"name":"…","arguments":{}}]}` object or a final textual response. It does not send the native fields `tools`, `tool_choice`, and `parallel_tool_calls` to this router. Previous calls are represented in the protocol; tool results are marked as untrusted data. The parser converts only a complete JSON envelope, checks names present in the catalog, object arguments, types, properties, required fields, and numeric/length limits of the schema and generates new IDs. The adapter validates this JSON Schema subset and rejects calls with constraints it cannot validate (e.g., `$ref`, `pattern`, `not`); native adapters do not have this bridge restriction. A separate JSON visitation limits depth and rejects prototype keys even in nested free properties. JSON inside a quotation or larger text is not executed. Validated requests return to the normal core authorization and execution path. The model may not respect this protocol and page/tool results may contain prompt injections: structural validation does not replace core authorizations. The live browser test must therefore verify this capability of the model separately. On this host, on 2026-10-05, the actual test via gateway and Go core completed in two steps: `gemini-3.8-flash` requested the browser status and reported `Example Domain` / `https://example.com/` from the received data. The result is in `svolo-installation/web-gemrouter-agent-browser-live.json`. This verifies the tool/result/response path for that test; it does not qualify all browser operations or all models of the router.

For the textual bridge, the gateway acquires a complete response and then produces Chat Completions/SSE; it does not show partial text during that turn. The bridge is limited to the Gemrouter imported by the administrator. An endpoint change restores the native path, unless a valid explicit configuration is in place.

The pi bridge converts text, inline images, tool definitions, tool calls, and tool results. OAuth calls use `ModelRuntime` with an SQLite `CredentialStore` dedicated to the single user and provider. Modifications/refreshes are serialized in the gateway process and each rotated token is saved encrypted. Do not use multiple gateway processes on the same DB without adding a distributed lock for the refresh.

OAuth responses are accumulated by pi and then translated into ChatCompletion; when the core requests streaming, the bridge emits valid SSE after the completion of the turn. This path supports tools, but does not show partial text during OAuth execution. The runtime Codex catalog is an installed client catalog, not proof of the account's access to every model. Actual access is verified by the request to the model, as clarified by the [OpenAI documentation for Codex app-server](https://developers.openai.com/siwc/token-sharing-open-source/codex-app-server).

Codex quotas are read with `account/rateLimits/read`, the official [Codex App Server protocol](https://learn.chatgpt.com/docs/app-server). The process uses a private temporary `CODEX_HOME` under the Svolo user directory, `auth.json` with `0600` permissions, and the stable Codex binary. The gateway acquires the credential lock, materializes the account from the DB, acquires the quota windows, saves any rotated tokens, and removes the temporary directory. It does not read the system user's Codex home and does not perform inference to check quotas. Configure `SVOLO_CODEX_BIN` or the `codexBin` option; on this host, the stable binary is `.local/lib/svolo-toolchain/codex/codex` (0.160.0).

For Gemrouter/OpenAI-compatible, Svolo exposes a quota endpoint only if explicitly configured, or the last rate-limit headers received from a real provider response. `models/list` is not mistaken for a quota endpoint. If the remaining balance is not available, Svolo shows `unavailable` and the reason. For Gemini API, the limits belong to the project; the documentation points to [Google AI Studio for actual limits](https://ai.google.dev/gemini-api/docs/rate-limits). The [Antigravity CLI quotas](https://antigravity.google/docs/cli/commands/usage) are separate; Svolo does not simulate them when the account/adapter is unavailable.

Upstream errors reported by the bridge are synthetic: no error bodies or OAuth stderr that might contain secrets are copied. All public payloads are free of credentials. For local checks without spending provider turns: `node --test web/providers.test.mjs`. Tests with SDKs and simulated upstreams do not qualify a live call; this must be verified separately.
