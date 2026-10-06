# Web deployment on svolo.eeess.cyou

This installation uses a dedicated Node.js gateway and a dedicated Cloudflare tunnel.
The application maintains state `0.3.1-dev`: the deployment does not change the product
qualification defined in [RELEASE.md](RELEASE.md).

## Traffic path

```text
https://svolo.eeess.cyou
    → tunnel Cloudflare "svolo"
    → http://127.0.0.1:7340
    → /home/your-user/svolo/web/server.mjs
```

The tunnel ID is `<tunnel-id>`. The DNS record was
created using the Cloudflare certificate already present on the server. The local
configuration allows this hostname and returns 404 for other hostnames. The local
core on port 7331 is not a destination of this tunnel.

## Installed files and process contract

| Item | Path or value |
| --- | --- |
| Gateway | `/home/your-user/svolo/web/server.mjs` |
| Node.js | `/home/your-user/.nvm/versions/node/v24.18.0/bin/node` |
| Working directory | `/home/your-user/svolo` |
| Private state | `/home/your-user/.local/share/svolo/web` (permissions 700) |
| Gateway key | `/home/your-user/.config/svolo/web-master.key` (permissions 600; managed by the gateway) |
| Application unit | `/home/your-user/.config/systemd/user/svolo-web.service` |
| Tunnel unit | `/home/your-user/.config/systemd/user/svolo-cloudflared.service` |
| Tunnel configuration | `/home/your-user/.cloudflared/config-svolo.yml` (permissions 600) |
| Tunnel credential | `/home/your-user/.cloudflared/<tunnel-id>.json` (permissions 600) |
| Cloudflare management certificate | `/home/your-user/.cloudflared/cert-eeess.pem` |
| Tunnel metrics | `http://127.0.0.1:7341/metrics` |

The application unit provides these variables:

```text
NODE_ENV=production
SVOLO_WEB_HOST=127.0.0.1
SVOLO_WEB_PORT=7340
SVOLO_WEB_DATA=/home/your-user/.local/share/svolo/web
SVOLO_PUBLIC_ORIGIN=https://svolo.eeess.cyou
SVOLO_WEB_KEY_FILE=/home/your-user/.config/svolo/web-master.key
SVOLO_CORE_BIN=/home/your-user/svolo/build/svolo-core-web
SVOLO_CHROMIUM=/home/your-user/.local/bin/chromium
SVOLO_PI_BIN=/home/your-user/.local/bin/pi
```

The data directory contains the persistent state of the gateway. The service has
write access to that directory and to the repository for application
operations. It uses `UMask=0077`, `NoNewPrivileges=true`, a private
temporary directory, read-only system filesystem, and read-only home, with the
indicated write exceptions. The gateway separately enforces filesystem and
process isolation for user cores via `bwrap`; the host network
remains shared and the browser uses the egress proxy that rejects private addresses.
The gateway also limits the APIs and tools exposed to users of the service.

The application service sets `MemoryHigh=2G`, `MemoryMax=4G`, `CPUQuota=200%`
and `TasksMax=1024` for the entire process group, including browsers and child cores.
These limits are aggregated for the application; they do not constitute individual per-user
quotas. Starting cores uses `/usr/bin/prlimit` to limit open file descriptors
to 4096. Limits on virtual memory and the number of processes per UID are not
used to avoid interference with V8 and with other services belonging to the same user.

The gateway uses the `@earendil-works/pi-coding-agent` SDK version `1.0.3`, pinned in
`web/package.json` and `web/pnpm-lock.yaml` and installed in the application's
`web/node_modules`. To install web dependencies, use Node.js 24 or higher
and pnpm 10.34.3:

```bash
cd /home/your-user/svolo/web
corepack pnpm install --frozen-lockfile --ignore-scripts
```

Installation with a frozen lockfile and without dependency scripts has been
verified; local SDK import and the 63 web tests (54 unit/mock tests and 9 browser checks including sub-tests) passed. The gateway
loads this copy of the SDK without depending on the previous copy in the external
toolchain. The `pi` CLI, Chromium, and the web core remain selected via the explicit
paths indicated in the unit; their operation within the service has been verified.

## Startup and persistence

Both services use `Restart=on-failure` with a five-second wait. The tunnel
requires the gateway to start via `Wants` and is ordered after it.
`Linger=yes` is already active for user `<os-user>`: the systemd user manager remains
available without an active session and is started after a system reboot.

Before the first startup, verify the application, authentication, and isolation
locally. Enable and start the services only after this verification:

```bash
systemctl --user daemon-reload
systemctl --user enable --now svolo-web.service
# Verify the local gateway before starting the tunnel.
systemctl --user enable --now svolo-cloudflared.service
```

Unit configuration does not start processes on its own. As of the check on
October 5, 2026, `svolo-web.service` and `svolo-cloudflared.service` are both
enabled and active; the public domain reaches the gateway on port 7340.
The tunnel has four active connections and `Linger=yes` is confirmed. The actual
status is checked with:

```bash
systemctl --user is-enabled svolo-web.service svolo-cloudflared.service
systemctl --user is-active svolo-web.service svolo-cloudflared.service
loginctl show-user your-user -p Linger
systemctl --user status svolo-web.service svolo-cloudflared.service
journalctl --user -u svolo-web.service -u svolo-cloudflared.service --since today
```

A forced termination test of only the gateway using `SIGKILL` verified an
automatic restart in 7129 ms, with sessions, providers, agent results, and browser
still usable. Application restart was also verified; the entire
server was not restarted. The report is in
`/home/your-user/svolo-installation/web-crash-recovery.json`.

To stop the public service:

```bash
systemctl --user stop svolo-cloudflared.service
systemctl --user stop svolo-web.service
```

To also prevent startup on the next reboot, use `disable --now` instead of
`stop`. The services `air3`, `goonerbot`, and the local core service on port 7331
remain separate from this configuration.

## Infrastructure checks

```bash
systemd-analyze --user verify ~/.config/systemd/user/svolo-web.service ~/.config/systemd/user/svolo-cloudflared.service
cloudflared tunnel --config ~/.cloudflared/config-svolo.yml ingress validate
cloudflared tunnel --config ~/.cloudflared/config-svolo.yml ingress rule https://svolo.eeess.cyou
node --test web/security.test.mjs web/providers.test.mjs
```

The final web suite comprises 53 passed tests: 39 security and 14 provider tests.
The security tests use temporary databases and credentials as well as mock providers.
They verify cookies, session expiration, CSRF, data isolation between users,
API restrictions, authenticated encryption, and egress traffic destinations.
Local HTTP tests use a mock core and do not replace testing real
cores and Chromium under the systemd service, nor public HTTPS verification.

Public verification on October 5, 2026 yielded HTTP 200 for the start page
and `/api/health`, HTTP 401 for APIs without a session, and HTTP 403 for forged origins
or hostnames. The TLS chain and name `svolo.eeess.cyou` are valid;
the observed certificate is issued by Google Trust Services, covers `*.eeess.cyou`,
and expires on November 13, 2026. Public tests performed no login and
published no passwords. Details are stored locally in
`/home/your-user/svolo-installation/public-http-checks.json` and
`/home/your-user/svolo-installation/public-tls-certificate.txt`.

19 checks were also run with two users on the real systemd gateway and
separate `bwrap` cores: empty initial providers and sessions for the new account,
confined workspaces, separated contents at the same path, rejection of
the other user's sessions and execution requests. Chromium displayed
the proxy rejection page when attempting to access the gateway on loopback.
The temporary account, its sessions, and its dedicated directory were removed
after stopping its supervisor; test files in the administrator workspace
were removed. The `flock` test on the original file `.owner.lock` confirmed
the lock while the core was running and release after shutdown, prior to
removal of the temporary directory. These tests did not call models. The report is in
`/home/your-user/svolo-installation/native-isolation-results.json`.

Cloudflare documents the [locally managed tunnel](https://developers.cloudflare.com/tunnel/features/locally-managed-tunnels/create-local-tunnel/)
and [ingress rule validation](https://developers.cloudflare.com/tunnel/features/locally-managed-tunnels/configuration-file/).
This deployment uses user systemd units, with credentials outside the repository.
Keep the gateway key, tunnel credentials, and Cloudflare certificate in
their respective private paths; their contents do not belong in the source
distribution or verification logs.

## Document-reading dependencies

The web attachment reader requires `poppler-utils` (`pdfinfo`, `pdftotext`,
`pdftoppm`, `pdfimages`), Tesseract with English and Italian language data,
Bubblewrap and `prlimit`. System packages are preferred. For a private unpacked
Tesseract installation, set `SVOLO_OCR_PREFIX` to a directory containing
`usr/bin/tesseract`, `usr/lib/x86_64-linux-gnu` and
`usr/share/tesseract-ocr/5/tessdata`. The reader mounts that directory read-only
inside its isolated decoder. This deployment uses such a private prefix, outside
the repository, set in the user service unit. `GET /api/documents/capabilities`
reports decoder availability. Restart the gateway after changing this environment.

Run `node --test web/*.test.mjs` on Node.js 24 with native browser/OCR dependencies
present. Native OCR and renderer checks are distinct from mock gateway tests;
skipped dependencies do not count as native passes. The current task-profile
checks also exercise a real sandboxed Go core and its internal MCP with a fixture
provider, without paid inference or real customer documents. See
[current verification](verification.json) and [task profiles](TASK_PROFILES.md).
