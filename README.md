<p align="center"><img src="brand/wordmark.svg" width="410" alt="Svolo"></p>
<p align="center"><strong>You set the direction. Svolo gets the work done.</strong></p>

# Browser, agents, and workspace in the same environment

[Italian documentation](docs/it/index.md) · [Public repository](https://github.com/0xfunboy/svolo)

Svolo combines interactive browsing, agent activities, project files, and remote hosts into the same application. Control can pass from the agent to the user without opening a second copy of the page. The Go core manages tools, authorizations, and services; the Electron/React desktop provides tabs, chat, and workflows.

![Visual presentation of Svolo](brand/product-hero.webp)
*Approved product illustration: represents visual direction, not a screenshot of a running build.*

## Distribution status

**0.3.1-dev — source distribution for development and verification.** This is not a production-certified release. The implemented desktop browser uses Electron; a CEF host is not distributed. Experimental modules are not declared as available features merely because they appear in the source code.

The [current verification](docs/verification.json) distinguishes executed checks, results, and unexecuted verifications. The [release requirements](docs/RELEASE.md) remain binding.

## What is included

| Area | Project capabilities |
| --- | --- |
| Browser | Tabs, managed profiles, semantic reading, input, screenshots, responsive controls, network, downloads, and sampled recordings. |
| Agents | Configurable providers, images when supported by the model, streaming, tools with approval, interruption, and persistent state. |
| Workspace | Project-confined files, verified transfers, Kanban, issues, Git/worktree, and ATP planning. |
| Hosts | Local execution, multi-host configuration, and OpenSSH connections to remote daemons. |
| Integrations | MCP client for external tools, MCP server with session-restricted tokens, optional pi runtime for desktop. |
| Protections | Local authentication, credentials vault, separation of administrative controls, and native application permissions. |

The [tool catalog](schemas/tools.json) is generated from executable definitions. The [capabilities and limitations](docs/capabilities.json) describe which modules are connected to the application and which require further integration.

## Starting the core

Requires Go 1.23 or later. Browsing also requires Chromium/Chrome installed. Run as a regular user, not as system administrator.

```bash
mkdir -p build
cd core
go build -trimpath -o ../build/svolo-core ./cmd/svolo-core
../build/svolo-core serve --data "$HOME/.local/share/svolo/core" --listen 127.0.0.1:7331
```

Open `http://127.0.0.1:7331`. The process prints the path to the `auth.token` file: the token is read locally and entered into the login screen. Do not publish the token and do not place the data folder in a workspace accessible to the agent. Startup does not require an AI account; an agent session requires a configured provider.

For Windows, macOS, desktop, and native dependencies: [first run](docs/GETTING_STARTED.md).

## Starting the desktop

```bash
cd desktop
corepack enable
pnpm install --frozen-lockfile
pnpm typecheck
pnpm test
pnpm dev
```

Package manager version and dependencies are pinned in the manifest and lockfile. Installation requires access to the package registry. The `pi` runtime is external: the core console can be used without installing it.

## Linux web gateway

The service in `web/` provides HTTPS access with login, chat, integrated browser, provider/MCP settings, and user management. Each account uses a separate core inside Bubblewrap, with private profiles and workspaces. Gateway credentials and prompts are encrypted; backup must also include the core stores and keep the master key separately.

The gateway requires Node.js with `node:sqlite`, Chromium, Bubblewrap, and a compiled Go core. It listens on loopback and is exposed via an HTTPS proxy/tunnel: follow [web setup and verification](docs/WEB_DEPLOYMENT.md), [web API](docs/WEB_API.md), and [web security](docs/WEB_SECURITY.md). The Linux service is distinct from the desktop distribution and remains in development, with `productionQualified:false`; external providers require live checks of individual protocols and unavailable quotas are indicated as such.

Attach JPG, PNG, PDF, TXT and CSV documents in chat. Select a reusable task profile
to recall reviewed procedures, field mappings and exceptions. The TIM starter
supports a first supervised contract-entry case; learning proposals require review
before becoming a new private revision. See [documents and task profiles](docs/TASK_PROFILES.md).

## Documentation

[User guide](docs/USER_GUIDE.md) · [Architecture](docs/ARCHITECTURE.md) ·
[Configuration](docs/CONFIGURATION.md) · [API](docs/API.md) ·
[Remote hosts](docs/REMOTE_HOSTS.md) · [Security](docs/SECURITY.md) ·
[Development and testing](docs/DEVELOPMENT.md) · [Brand](brand/README.md)

[Web deployment](docs/WEB_DEPLOYMENT.md) · [Web API](docs/WEB_API.md) ·
[Web security](docs/WEB_SECURITY.md) · [Web frontend](docs/frontend-web.md) ·
[Web providers](docs/web-providers.md) · [Assistant harness](docs/AGENT_HARNESS.md)

## License

[0xfunboy Non-Commercial License](LICENSE.md), version 1.1: commercial use only with written authorization. Applicable exceptions for components are collected in [legal/NOTICE.md](legal/NOTICE.md), separately from the product documentation.

## Web chat controls and language

The chat menu offers **Rename**, **Pin / Unpin** and **Delete**. Pins and names are saved per user. Deletion removes the chat's history, browser profile and screenshots, while keeping workspace files. Stop an active agent before deleting its chat.

The top bar offers a direct **EN / IT** toggle, with English as the default. Collapse the right-hand column from inside the chat and reopen it with the floating button over the browser. The left sidebar also collapses into a compact rail with the logo, settings and user profile. Language and both column preferences persist in the current browser. Switching language preserves an unsent message and does not change browser control or provider login.

Clone the public source repository:

```bash
git clone https://github.com/0xfunboy/svolo.git
```

The public GitHub repository contains the application source. Runtime databases, browser profiles, API/OAuth credentials and tunnel configuration are excluded. See [repository publication](docs/REPOSITORY.md).
