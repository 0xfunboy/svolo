# Linux Web Service Security

This document describes the protections and limits actually implemented
in the `web` gateway, the core pool and the Linux deployment. The product remains
a development revision with `productionQualified:false`. Web service verification
does not qualify Windows/macOS/Linux desktop packages,
nor does it replace the requirements of [RELEASE.md](RELEASE.md).

## Service Boundary

The gateway listens exclusively on the loopback, typically port 7340.
Public HTTPS terminates at the proxy/tunnel path documented in
[WEB_DEPLOYMENT.md](WEB_DEPLOYMENT.md). The independent local core on
port 7331 and the desktop are not targets of this gateway.
The gateway allows configured Host and Origin and an explicit core API list;
it does not expose the core admin token, vault, daemon stop, or SSH management
to the browser. The public API is described in [WEB_API.md](WEB_API.md).

Page traffic, model responses, and MCP results remain
untrusted inputs. Text cannot grant approval. The
frontend escapes dynamic values and displays consent request arguments;
the CSP excludes external scripts, iframes, objects,
and framing of the site. These measures do not guarantee immunity
to prompt injection or browser vulnerabilities.

## Identity and Authorizations

Passwords are scrypt hashes with a random salt, parameters N=32768, r=8, p=1,
and constant-time comparison. The session cookie contains 256 random bits;
SQLite stores its SHA-256. Cookies `HttpOnly`, `Secure`, `SameSite=Lax`,
prefix `__Host-`, a 12-hour duration, and distinct CSRF protect the web session.
Authenticated mutations require `X-CSRF-Token`; login precedes this
session and is protected by Origin/Host checks and rate-limiting.

Login limits attempts per IP and username to 20 in 15 minutes, with four
concurrent password verifications and a maximum of ten retained sessions per
account. Attempt counters are in memory and reset upon restart.
Logout deletes the session from the DB. A disabled user is not
authenticated; the current UI/API allows administrators to create
logins, but presents no public password reset or disabling endpoints.

Each request derives the user from the session, not from an ID supplied in the
body. Providers, preferences, MCP, and prompts have per-user keys.
Administrators create accounts; their provider list does not become that
of other users. Credentials from the first import belong
only to the selected administrator.

## Encryption and Sensitive Data

SQLite is not an entirely encrypted database with SQLCipher. Provider/MCP
credentials, core vault keys, and prompts registered by the gateway
are encrypted in their respective fields using AES-256-GCM, a random nonce, and an
authentication tag. Providers, MCP, core keys, and prompts bind the cryptographic
context to the user ID. The master key is a 32-byte file represented by
64 hexadecimal characters, external to the data directory, with 0600 permissions.
The DB and private directories have restrictive permissions; the gateway starts
the process with a umask of 0077.

Usernames, password hashes, provider metadata, configurations, preferences,
identifiers, and audits are not fully encrypted. Furthermore, the core
stores conversations, events, and results in its own data directories;
this history may include prompts and plaintext even when the
corresponding gateway field is encrypted. Chromium profiles, cookies,
downloads, and workspace files are sensitive. Therefore, protect the entire
data directory and backups, not just the credential fields.

The browser does not receive saved API keys or access/refresh tokens. The gateway
sends only an internal broker secret to the core; the actual provider is
called by the web service. OAuth refresh is serialized per user
and provider within the gateway process, saving rotated tokens in the encrypted DB.
This serialization is not a distributed lock: do not run multiple concurrent
gateways on the same DB without additional coordination.

The initial import is explicit and one-time, controlled by the marker
`initial-import` in the DB. It reads only the authorized fields of the
Gemrouter file and the Codex credential specified by the administrator. After
importing, the service uses its own encrypted copy and does not depend
on external OAuth directories for subsequent requests. Temporary directories
used for Codex quota checks are created privately, then removed;
any rotations are recorded in the DB. Copying an
Antigravity IDE file does not convert it into a supported OAuth credential.

## Core Isolation

Each user receives a distinct core and directories `users/<id>/core`,
`workspace`, and `home`. The launcher uses Bubblewrap (`bwrap`) with user,
PID, IPC, and UTS namespaces, capability shedding, `/usr` and read-only
mounted application components, private `/tmp`, and writeable directories
restricted to that user's data/home/workspace. The system home, original
OAuth credentials, and other users' directories are not
mounted in the namespace. Processes terminate with their owning namespace.

Chromium uses its own sandbox; the web deployment does not enable `--no-sandbox`.
Required are Linux with usable user namespaces, Bubblewrap, `flock`,
`prlimit`, Node.js with `node:sqlite`, a compiled Go core, and compatible
Chromium. The current web implementation is Linux: do not extrapolate
these guarantees to desktop or Windows/macOS services.

The host network is **shared**: no separate network namespace is created.
Chromium is configured for the gateway's egress proxy, with no loopback bypass,
and with QUIC disabled. The proxy validates DNS, connects to the resolved
numeric address, and rejects private/loopback/link-local destinations and
unallowed ports. HTTP is restricted to ports 80/8080, CONNECT to 443/8443.
This is a policy for traffic routed by the proxy, not a kernel firewall
forcing every process to use it.

Ordinary provider endpoints undergo HTTPS validation and fetch execution
which locks the DNS address for the single connection. The private exception
of the imported Gemrouter is an explicit decision by the administrator.
MCP in the gateway accepts only public HTTPS and rejects local processes.
Browser egress and external client configurations have separate boundaries:
do not infer a universal guarantee for every connection of any adapter or
external SDK from the Chromium proxy.

The web service denies workspace execution, Computer Use, profile import,
routines, and more administrative tools both in manual calls
and in the allowed runner catalog. Web sessions have `allowExec:false`
and a workspace fixed in the namespace. These restrictions do not eliminate
the risks of a core/browser vulnerability or an authorized MCP service
performing operations in its own environment.

## Ownership, Shutdown and Recovery

The launcher acquires an exclusive, non-blocking `flock` on
`core/.web-owner.lock`, maintained for the entire lifetime of the PID namespace.
Only after this lock does `core-launch.sh` remove singleton traces of the
web core profiles (`SingletonLock`, `SingletonSocket`, `SingletonCookie`,
`DevToolsActivePort`). The previous namespace must be terminated
before the new owner acquires the lock. Recovery is limited
to private web directories; it does not alter the conservative management
of standalone/desktop profiles. Do not manually remove profile locks
while the owner is still active.

Graceful shutdown first requests the core to close CDP/Chromium and dump
the store; after the shutdown budget, the pool can terminate the process group.
Restarting the core marks previously running tasks as interrupted,
without automatically repeating actions or approvals. An action already sent
to an external site may have taken effect before the interruption: verify
the outcome before resuming work.

The pool allows eight cores and can stop a core unused for 15 minutes
when it finds no runs with state `running` or `waiting_approval`. Endpoints enforce limits
for users, sessions, providers, MCP, and requests; these are not complete
CPU, memory, or provider cost consumption quotas per user. The systemd deployment
adds aggregate limits for the entire application group, described in
[WEB_DEPLOYMENT.md](WEB_DEPLOYMENT.md); the launcher limits open
file descriptors to 4096. Do not claim individual resource fairness or
isolation based on these aggregate limits.

## Consistent Backup

The data directory and the master key form a restorable set,
but must be stored in separate locations. For the current deployment,
the directory is `/home/your-user/.local/share/svolo/web` and the key is
`/home/your-user/.config/svolo/web-master.key`; these are configurable.
Do not create backups in the workspace directory accessible to the agent.

1. Record code version, core binary, runtime, and configuration paths,
without printing keys, cookies, or environment dumps.
2. Stop the tunnel first, then the gateway using the documented units.
Wait for the service cores and browsers to shut down; verify that
no web owner retains locks.
3. Copy the entire data directory, preserving structure, ownership, and permissions,
including SQLite, any WAL/SHM files, core data, profiles,
workspaces, and user homes. Copying only `svolo.sqlite` from an
active process may lose transactions present in the WAL.
4. Protect the full backup with encryption and access control.
Encryption of individual fields in the DB does not protect profiles or plaintext
history. Separately store a verified copy of the master key
with 0600 permissions, accessible only to the recovery operator.
5. Keep service configurations and tunnel credentials in a separate,
private archive. Do not include them in the source distribution.
Restart gateway and tunnel after verifying copy consistency.

A coordinated online backup command between SQLite and the core stores
is not implemented. Therefore, the described procedure uses a controlled
shutdown. Copying the DB alone without the master key does not recover
encrypted credentials/prompts or core vault keys; losing the master
key can make this data unrecoverable.

## Recovery

Restore first in a private, compatible Linux environment with the tunnel
stopped. Keep a copy of the current state before replacing it.
Restore the data directory and the corresponding master key, maintaining
ownership of the service user, private directories, and 0600 permissions
for the key/DB. Restore binaries and unit paths compatible with
that version; do not attempt a DB migration without verification.

Revoke restored web sessions by deleting `web_sessions` records
from the DB while the gateway is stopped: otherwise, a still-valid cookie
present in the backup could become usable again. Revocation requires a new login;
it does not delete users, providers, or work sessions. External API/OAuth tokens
may have expired or rotated after the backup: verify them and reconnect
the account when necessary, without overwriting external OAuth files.

Start only the gateway, verify health, authentication, data separation
between two users, session access, Chromium startup, and quotas.
Verify that no run is automatically repeated and that lock recovery
applies exclusively to restored web profiles. Re-expose
the tunnel after these checks. This document describes a procedure;
it is not a statement of already-tested disaster recovery.

## Verifications and Qualification

Gateway unit tests/simulations verify authentication, CSRF,
record separation, encryption, and API restrictions; simulated SDKs/upstreams
do not certify real providers or MCPs. Local runtime testing
of the frontend verified browsers, captures, sessions, and layouts, and deployment
tests verify the service/tunnel actually installed.
Updated outcomes belong to the deployment verification reports.

Codex can expose real quota windows; Gemrouter with no observed quota
endpoints/headers remains `unavailable`. Antigravity with no supported credential
remains unconnected. A textual response test does not
certify a complete cycle of tool calls and approvals; these must be
verified separately for each adapter/model. The publication
of this service and the described tests do not automatically
promote the product to a production-qualified release.

## Attachments and task memory

The gateway encrypts original document bytes, OCR/text, task procedures, versions,
proposals and run-context bindings with owner/resource-bound AES-256-GCM fields.
Selected-file and same-chat authorization applies to both HTTP and internal MCP.
CSRF protects edits; optimistic revisions and mandatory user review prevent silent
learning updates. Pattern checks reject common personal identifiers and credential
URLs; they do not detect every name/address and do not replace human review.

Decoding uses a separate, network-isolated Bubblewrap process without user home or
credentials, capped at two concurrent readers, one upload per user, 1 GiB address
space, 25 CPU seconds per command, bounded output and a 120-second file budget.
Originals are capped at 20 MiB each, 100 files/200 MiB per user and seven-day retention.
Uploads materialize only selected files in a private workspace directory, reject
symlinks/hardlinks and verify existing file hashes. Core guards require selected
paths, authorized HTTPS origins and fresh human approval; the origin is checked
again after approval. Active/waiting tasks retain needed copies; polling or a
periodic sweep removes inactive copies.

These encrypted fields do not encrypt the entire SQLite file, core history or
Chromium data. Attachment expiry does not erase prior conversation/provider text.
Only checked files are available to the selected provider, subject to its retention
policy. [Task profiles](TASK_PROFILES.md) describe the user workflow and boundaries.
