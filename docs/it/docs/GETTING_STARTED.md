# Primo avvio

## Prerequisiti

Il core usa esclusivamente la libreria standard di Go. La versione minima è dichiarata
in `core/go.mod`. Il desktop richiede Node.js, il package manager dichiarato in
`desktop/package.json` e le dipendenze del lockfile. Chromium/Chrome, OpenSSH, Git,
GitHub CLI, ffmpeg e il runtime pi sono programmi esterni: installare solo quelli
necessari alle funzioni che si intendono usare.

`doctor` elenca i prerequisiti rilevati senza installare programmi:

```bash
cd core
go run ./cmd/svolo-core doctor
```

## Linux e macOS: console del core

Dalla radice del repository:

```bash
mkdir -p build
cd core
go build -trimpath -o ../build/svolo-core ./cmd/svolo-core
../build/svolo-core serve --data "$HOME/.local/share/svolo/core" --listen 127.0.0.1:7331
```

L'output iniziale contiene `url`, `tokenFile`, `data` e `version`. Aprire l'URL locale
e leggere il token dal percorso indicato. Per scegliere il browser aggiungere
`--chromium /percorso/assoluto/del/browser`. `--headless` elimina la finestra nativa,
non trasforma il viewer della pagina in un desktop remoto completo.

Conservare la sandbox del browser. Il flag `--no-sandbox-test` è riservato a test
isolati ed è ulteriormente protetto da una variabile di ambiente; non è una soluzione
per l'installazione normale.

## Windows: console del core

In PowerShell, dalla radice del repository:

```powershell
New-Item -ItemType Directory -Force build | Out-Null
Set-Location core
go build -trimpath -o ../build/svolo-core.exe ./cmd/svolo-core
../build/svolo-core.exe serve --data "$env:LOCALAPPDATA\Svolo\core" --listen 127.0.0.1:7331
```

Se il browser non viene rilevato, indicarne l'eseguibile con `--chromium`.
Il controllo delle applicazioni native richiede una sessione utente interattiva,
non semplicemente un processo avviato come servizio.

## Desktop

Dalla directory `desktop`:

```bash
corepack enable
pnpm install --frozen-lockfile
pnpm typecheck
pnpm test
pnpm dev
```

`pnpm dev` costruisce il core e avvia la finestra di sviluppo. `pnpm build` costruisce
il core e i bundle desktop. Non eseguire `pnpm start` prima dell'installazione delle
dipendenze. `SVOLO_CORE_BIN` seleziona un core già compilato; `SVOLO_PI_BIN` seleziona
il runtime esterno, senza rinominarlo o incorporarne un eseguibile nel repository.

## Prima sessione

Nella console creare una sessione con un ID univoco. Il workspace è facoltativo per
la navigazione, ma deve essere un percorso assoluto dell'host selezionato per usare i
file. Lasciare l'esecuzione dei programmi disattivata finché non è necessaria.

Configurare endpoint, modello e riferimento della credenziale nella pagina
Configuration. I file in [examples](../examples/README.md) sono modelli di
configurazione, non credenziali o identificativi di modello già validi.

Provare inizialmente una pagina senza account e un workspace di prova. Arrestare il
core con Ctrl+C; non spegnere la macchina per terminare l'applicazione.
