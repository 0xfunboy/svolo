# Security and data processing

## Trust boundary

Web pages, model responses, MCP content, and workspace files are untrusted
inputs. None of these inputs can grant administrative permissions. Approvals
are application controls, not prompt instructions. There is no guarantee of
immunity to prompt injection.

The core listens only on an explicit loopback IP and requires API authentication.
Session-scoped MCP tokens do not have the privileges of the admin token. The
renderer does not receive general Node.js access, and external pages are not
administrative targets for the browser controller.

## Program execution

`workspace-exec` is disabled until the session authorizes execution. The process
environment is minimized, paths are confined where the operation expects it,
and programs have time budgets. **These measures are not an operating system
sandbox.** Run untrusted code in an isolated machine or container.

The Chromium sandbox must remain active during normal use. The test flag that
disables it requires separate enablement and must not be included in production scripts.

## Secrets

The vault uses referenced credentials, not values in configuration files. Keep the
vault key outside the protected data directory and separate its backup. Files,
recordings, conversations, and profiles may contain sensitive information even
if provider credentials are encrypted.

Do not upload tokens, profile directories, environment dumps, or private keys to
bug reports. Private core variables are not intentionally forwarded to tools;
variables explicitly allowed for an external server require the same care.

## Input and remote control

Stopping the controller blocks new actions; it does not roll back already completed transactions.
Native windows and the remote viewer have different limitations: do not attribute
the guarantees of an isolated desktop to the page. Use only authorized applications and
avoid terminals and permission management panels under the agent's general control.

## Resources and updates

Limits exist for individual operations and files. The aggregate quotas module
does not yet cover all paths. Monitor available space and recordings.
No desktop update publisher is configured: the source distribution
does not automatically download or replace the application.

## Reports

Before sharing a reproduction, remove secrets and personal data. Use the
maintainer contact specified in the [license](../LICENSE.md), without publishing a
key or a full exploit alongside sensitive data.
