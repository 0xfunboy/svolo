# First startup

## Prerequisites

The core exclusively uses the Go standard library. The minimum version is declared
in `core/go.mod`. The desktop requires Node.js, the package manager declared in
`desktop/package.json` and the lockfile dependencies. Chromium/Chrome, OpenSSH, Git,
GitHub CLI, ffmpeg and the pi runtime are external programs: install only those
necessary for the functions you intend to use.

`doctor` lists the detected prerequisites without installing programs:

```bash
cd core
go run ./cmd/svolo-core doctor
```

## Linux and macOS: core console

From the root of the repository:

```bash
mkdir -p build
cd core
go build -trimpath -o ../build/svolo-core ./cmd/svolo-core
../build/svolo-core serve --data "$HOME/.local/share/svolo/core" --listen 127.0.0.1:7331
```

The initial output contains `url`, `tokenFile`, `data` and `version`. Open the local URL
and read the token from the indicated path. To choose the browser, add
`--chromium /absolute/path/to/browser`. `--headless` eliminates the native window,
it does not transform the page viewer into a complete remote desktop.

Keep the browser sandbox. The flag `--no-sandbox-test` is reserved for isolated
tests and is further protected by an environment variable; it is not a solution
for normal installation.

## Windows: core console

In PowerShell, from the root of the repository:

```powershell
New-Item -ItemType Directory -Force build | Out-Null
Set-Location core
go build -trimpath -o ../build/svolo-core.exe ./cmd/svolo-core
../build/svolo-core.exe serve --data "$env:LOCALAPPDATA\Svolo\core" --listen 127.0.0.1:7331
```

If the browser is not detected, specify its executable with `--chromium`.
Control of native applications requires an interactive user session,
not simply a process started as a service.

## Desktop

From the `desktop` directory:

```bash
corepack enable
pnpm install --frozen-lockfile
pnpm typecheck
pnpm test
pnpm dev
```

`pnpm dev` builds the core and starts the development window. `pnpm build` builds
the core and the desktop bundles. Do not run `pnpm start` before installing the
dependencies. `SVOLO_CORE_BIN` selects an already compiled core; `SVOLO_PI_BIN` selects
the external runtime, without renaming it or embedding an executable in the repository.

## First session

In the console, create a session with a unique ID. The workspace is optional for
browsing, but must be an absolute path on the selected host to use files.
Leave program execution disabled until it is necessary.

Configure the endpoint, model and credential reference in the Configuration page.
The files in [examples]](../examples/README.md) are configuration
templates, not credentials or already valid model identifiers.

Initially try a page without an account and a test workspace. Stop the
core with Ctrl+C; do not shut down the machine to terminate the application.
