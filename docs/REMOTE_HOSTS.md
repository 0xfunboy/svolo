# Remote hosts

## Operating model

Each host runs a Svolo daemon with its own data and authentication. OpenSSH resolves aliases,
keys, port, and intermediate hosts from the user's configuration. The client does not
automatically accept unknown host keys and does not enable SSH agent forwarding.

Before connecting a host, verify it with the OpenSSH client and set up authorized
non-interactive access. Do not disable host identity checking to make
a test pass. The resolution command uses `ssh -G`; it is not a connection test.

## Connecting

Add a local ID, an SSH alias, the remote port, and a reference to the daemon
credential. Secrets must not be placed directly in the host configuration.
Daemon installation is a separate and explicit operation that transfers a
compatible binary, verifies its SHA-256, and starts the user service.

The local core must know an artifact directory with `--artifacts-dir`.
Cross-compilation scripts produce the binaries for that directory, but only a
test on the target system demonstrates its correct startup.

An open tunnel is not sufficient: the connection is considered ready after the
authenticated response from the daemon and the protocol check. Explicit
disconnection takes precedence over an already scheduled automatic reconnection.

## Local and remote resources

The managed browser, workspace, and model must remain available to continue
a task when the client closes. A browser embedded in the desktop window cannot
survive the termination of that process. Native windows require a
graphical session; SSH alone does not create an interactive desktop.

Paths are interpreted on the host that owns them. Attachments are transferred
explicitly. Replaying events after a disconnection does not authorize the automatic
replay of external actions with uncertain outcomes.

## Qualification

Verify aliases with ProxyJump, changed host key, interrupted connection, reconnect,
daemon restart, file transfer, and absence of cross-session contamination. The
[current verification](verification.json) specifies which tests were performed in
preparing this distribution; a simulation is not a test on two real hosts.
