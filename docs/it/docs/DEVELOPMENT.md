# Sviluppo e verifiche

## Struttura

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

`product.json` definisce identità e versione. `scripts/check-product.py` ne verifica
la corrispondenza con desktop, core, packaging, licenza e catalogo. Un cambio di nome
non si applica ai moduli con un replace indiscriminato: aggiornare produttori,
consumatori, test, identificativi nativi e asset.

## Core

```bash
cd core
gofmt -l .
go vet ./...
go test -race ./...
go build ./...
go run ./cmd/svolo-core catalog
```

Le prove browser sono opt-in con `SVOLO_E2E=1`, richiedono un browser installato e
possono creare profili temporanei. Non usare directory o credenziali personali per i
test. Le prove native richiedono il sistema grafico previsto dal relativo helper.

## Desktop

```bash
cd desktop
pnpm install --frozen-lockfile
pnpm typecheck
pnpm test
pnpm build
```

La verifica `scripts/syntax-check.cjs` controlla solo parsing e trasposizione TypeScript:
non sostituisce `pnpm typecheck`, i test Vitest o una build Electron. Se le dipendenze
non sono installabili, il risultato va registrato come non eseguito, non come PASS.

Le fixture `core/internal/project/testdata/contracts.json` si rigenerano con
`node scripts/project-contracts.cjs` dalla radice; devono riflettere i riduttori correnti.
Il catalogo si aggiorna con `python3 scripts/update-catalog.py`.

## Asset

```bash
python3 -m pip install -r scripts/requirements-brand.txt
python3 scripts/build-brand.py
python3 scripts/check-product.py
```

La rigenerazione non scarica font o artwork. Le immagini di presentazione non possono
essere utilizzate come prova di funzionamento. Per gli screenshot di test creare una
sessione isolata e indicare il software effettivamente eseguito.

## Distribuzione sorgente

Il repository non include `node_modules`, cache, profili utenti o eseguibili precedenti.
Il manifest SHA-256 controlla i file consegnati; non è una firma del publisher e non
dimostra sicurezza o parità funzionale. Non aggiungere evidenze storiche come se fossero
state prodotte dalla revisione corrente.

## Contratti senza dipendenze desktop

`node --test scripts/test-source-contracts.cjs` usa TypeScript locale (o globale)
per caricare moduli puri e il runner Node per verificarne gli output. Controlla i
percorsi Windows/POSIX, i piani, i badge provider e i prompt di login. Non sostituisce
Vitest, il typecheck del desktop o la build Electron.

## Integrità della distribuzione e piani

```bash
python3 -B scripts/check-product.py
python3 -B scripts/test-product-check.py
python3 -B scripts/test-task-plans.py
```

Il primo controllo verifica identità, JSON, collegamenti, import locali, icone e hash.
I test negativi verificano che un’incoerenza venga davvero rifiutata. Il test dei piani
usa directory temporanee e il librarian reale, senza eseguire un modello.
