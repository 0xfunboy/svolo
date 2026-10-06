# Svolo Desktop

Electron shell, React interface, embedded browser, and connection to the Go core.

```bash
corepack enable
pnpm install --frozen-lockfile
pnpm typecheck
pnpm test
pnpm dev
```

`pnpm build` builds core and bundles; installers require the
[release procedure](../docs/RELEASE.md). The package manager version is in `package.json`.

[Complete guide](../README.md) · [First run](../docs/GETTING_STARTED.md) ·
[Architecture](../docs/ARCHITECTURE.md) · [Security](../docs/SECURITY.md)
