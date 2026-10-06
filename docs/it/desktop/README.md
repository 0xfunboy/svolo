# Desktop Svolo

Shell Electron, interfaccia React, browser integrato e collegamento al core Go.

```bash
corepack enable
pnpm install --frozen-lockfile
pnpm typecheck
pnpm test
pnpm dev
```

`pnpm build` costruisce core e bundle; gli installer richiedono la procedura di
[rilascio](../docs/RELEASE.md). La versione del package manager è in `package.json`.

[Guida completa](../README.md) · [Primo avvio](../docs/GETTING_STARTED.md) ·
[Architettura](../docs/ARCHITECTURE.md) · [Sicurezza](../docs/SECURITY.md)
