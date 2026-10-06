# Development and verifications

## Structure

```text
brand/       Sorgenti grafici approvati, vettori, icone e manifest degli asset
core/        Servizio Go, trasporti, strumenti e test
desktop/     Shell Electron, renderer React, risorse e helper nativi
docs/        Documentazione dello stato implementato
examples/    Configurazioni e routine senza credenziali
legal/       Avvisi applicabili ai componenti
schemas/     Contratti generati o mantenuti insieme al codice
scripts/     Build, controlli e rigenerazione
```

`product.json` defines identity and version. `scripts/check-product.py` verifies
its correspondence with desktop, core, packaging, license, and catalog. A name change
does not apply to modules with an indiscriminate replace: update producers,
consumers, tests, native identifiers, and assets.

## Core

```bash
cd core
gofmt -l .
go vet ./...
go test -race ./...
go build ./...
go run ./cmd/svolo-core catalog
```

Browser tests are opt-in with `SVOLO_E2E=1`, require an installed browser, and
can create temporary profiles. Do not use personal directories or credentials for
testing. Native tests require the graphics system expected by the relative helper.

## Desktop

```bash
cd desktop
pnpm install --frozen-lockfile
pnpm typecheck
pnpm test
pnpm build
```

The `scripts/syntax-check.cjs` verification only checks TypeScript parsing and transposition:
it does not replace `pnpm typecheck`, Vitest tests, or an Electron build. If dependencies
cannot be installed, the result must be recorded as not executed, not as PASS.

The `core/internal/project/testdata/contracts.json` fixtures are regenerated with
`node scripts/project-contracts.cjs` from the root; they must reflect the current reducers.
The catalog is updated with `python3 scripts/update-catalog.py`.

## Assets

```bash
python3 -m pip install -r scripts/requirements-brand.txt
python3 scripts/build-brand.py
python3 scripts/check-product.py
```

Regeneration does not download fonts or artwork. Presentation images cannot
be used as proof of operation. For test screenshots, create an
isolated session and indicate the software actually executed.

## Source distribution

The repository does not include `node_modules`, caches, user profiles, or previous executables.
The SHA-256 manifest checks delivered files; it is not a publisher signature and does not
demonstrate security or functional parity. Do not add historical evidence as if it
had been produced by the current revision.

## Contracts without desktop dependencies

`node --test scripts/test-source-contracts.cjs` uses local (or global) TypeScript
to load pure modules and the Node runner to verify their outputs. It checks
Windows/POSIX paths, plans, provider badges, and login prompts. It does not replace
Vitest, the desktop typecheck, or the Electron build.

## Distribution integrity and plans

```bash
python3 -B scripts/check-product.py
python3 -B scripts/test-product-check.py
python3 -B scripts/test-task-plans.py
```

The first check verifies identity, JSON, links, local imports, icons, and hashes.
Negative tests verify that an inconsistency is indeed rejected. The plan test
uses temporary directories and the real librarian, without running a model.
