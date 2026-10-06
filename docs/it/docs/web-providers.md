# Provider nel gateway web multiutente

`web/providers.mjs` mantiene configurazione, modello selezionato e credenziali in
SQLite. Ogni chiave primaria è `(user_id, id)`: due utenti possono usare lo stesso
nome provider senza condividere account, chiavi o preferenze. Il gateway autentica
gli utenti e autorizza l'importazione iniziale dell'amministratore. Il core riceve
solo la credenziale interna `web-broker`; le richieste ai provider passano dal
gateway.

## Contratto del modulo

Il gateway richiede Node.js 24 o successivo. `web/package.json` fissa
`@earendil-works/pi-coding-agent` alla versione 1.0.3 e pnpm alla versione 10.34.3;
`web/pnpm-lock.yaml` fissa anche le dipendenze transitive. Installazione e controlli:

```sh
cd web
corepack pnpm install --frozen-lockfile --ignore-scripts
corepack pnpm test
corepack pnpm start
```

Gli script lifecycle delle dipendenze rimangono disabilitati. Il percorso
`ModelRuntime` usato dal gateway è JavaScript e il suo import/catalogo offline
è stato verificato con questa installazione, senza compilare binding opzionali.
Il runtime predefinito appartiene all'applicazione:
`web/node_modules/@earendil-works/pi-coding-agent/dist/core/model-runtime.js`.
`SVOLO_PI_MODULE` oppure `piModulePath` possono indicare un modulo alternativo
esplicito. Nessuna directory OAuth dell'utente del sistema viene usata come
dipendenza runtime. La CLI Codex delle quote conserva invece il binario stabile
configurabile con `SVOLO_CODEX_BIN`/`codexBin`.

```js
const providers = createProviderService({
  db,                 // node:sqlite DatabaseSync
  encrypt, decrypt,   // string -> string; secondo argomento opzionale userId
  userStateDir,       // root dei dati oppure function(userId) -> directory
  workspaceRoot,
  // Optional deployment paths, independent of source credential files:
  piModulePath, codexBin,
});
```

Il modulo crea `svolo_web_providers`; `credential_enc` contiene solo il risultato
del cifratore fornito dal gateway. Modello, catalogo e metadati non contengono
access token, refresh token o chiavi API. La chiave master e la sua protezione
appartengono al gateway.

| Metodo | Risultato e uso |
| --- | --- |
| `list(userId)`, `get(userId,id)` | Configurazioni pubbliche, senza credenziali. |
| `save(userId,input)` | Salva un provider; omettere `apiKey` mantiene la chiave, valore vuoto la scollega. Tipi: `openai-compatible`, `gemrouter`, `codex-oauth`, `google-api-key`, `antigravity-oauth`. |
| `selectModel(userId,id,model)` | Cambia solo modello e capacità di visione; non modifica la credenziale cifrata. |
| `remove(userId,id)` | Elimina esclusivamente il provider dell'utente. |
| `models(userId,id,{refresh})` | Catalogo salvato oppure aggiornato; restituisce `{models,source,selectedModel}`. |
| `importAdmin(userId,{gemrouterEnv,codexAuth})` | Importazione autorizzata una tantum; restituisce `{imported,unavailable,providers}`. |
| `quota(userId,id)` | Quote lette dal provider oppure stato `unavailable` con motivazione; cache di almeno 60 secondi. |
| `coreProviders(userId,{proxyBaseUrl})` | `proxyBaseUrl` è già `/internal/provider/${userId}`; ciascun core usa `${proxyBaseUrl}/${providerId}/v1`. |
| `handleCompletion(userId,id,payload,{signal})` | `Response` per OpenAI-compatible/SSE; oggetto ChatCompletion per OAuth con `stream:false`. |
| `beginOAuthLogin(userId,id)`, alias `oauthStart` | Avvia device login Codex e restituisce `loginId`. |
| `oauthLoginStatus(userId,loginId)` | Stato e codice/URL device; visibili solo all'utente che ha avviato il login. |
| `cancelOAuthLogin(userId,loginId)` | Annulla il login. |

`oauthComplete`/`oAuthComplete` sono un controllo dello stato del device login, non
un endpoint che accetta token dal browser. Il client avvia il login, interroga lo
stato e mostra `event.userCode` e `event.verificationUri`. Il provider completa
il login tramite polling; il server salva la credenziale risultante in SQLite.

Il gateway valida gli endpoint HTTPS pubblici per tutti gli utenti. Solo
l'importatore dell'amministratore può usare il Gemrouter LAN autorizzato. Le
eccezioni LAN sono vincolate a `privateOrigin`, salvato solo dal backend di importazione e preservato quando cambia l'endpoint; una nuova origine usa DNS pubblico con connessione IP vincolata.
Le richieste disabilitano i redirect, così la chiave non viene inoltrata a un'altra
origine. L'eventuale `quotaURL` deve appartenere alla stessa origine del provider.

## Account e importazione iniziale

L'importatore legge solo `LLM_BASE_URL`, `LLM_MODEL` e `LLM_API_KEY` dal file `.env`
autorizzato. Il modello predefinito è il valore reale di `LLM_MODEL`. Una risposta
`GET /v1/models` aggiorna il catalogo senza effettuare un turno modello. La
configurazione osservata su questo host usa Gemrouter LAN e `gemini-3.8-flash`;
nessuna chiave viene riportata nei risultati o nei log.

Codex `auth.json` viene convertito nella forma canonica pi:
`{type:'oauth',access,refresh,expires,accountId,idToken?}`. Token e account ID
rimangono dentro la credenziale cifrata. L'importazione ripetuta non sovrascrive
un account già presente né i token ruotati. I file origine non sono salvati come
puntatori e non sono letti durante le richieste successive.
I path dell'importazione sono opzioni esplicite dell'amministratore; il modulo
non contiene path predefiniti verso `.env` o auth file esterni al servizio.

La presenza di `.gemini/antigravity-ide` o di un token del server remoto IDE non
dimostra la disponibilità di un OAuth Antigravity. Su questo host non è stato
trovato un export OAuth compatibile: nessun `oauth_creds.json`, auth pi vuoto e
nessun `state.vscdb` nelle posizioni standard verificate. Il provider Antigravity
appare come **non collegato**, con una motivazione esplicita. Un OAuth Google
generico non viene rinominato Antigravity. La versione pi installata, 1.0.3,
include Codex OAuth ma non un adapter Antigravity/Gemini CLI OAuth.
Durante la discovery iniziale erano presenti solo i launcher del server IDE
remoto `agy-ide`/`antigravity-ide`. È stata poi preparata la CLI ufficiale `agy`
1.2.17 nella toolchain locale, verificando SHA512 contro il manifest pubblicato
dall'installer Google. `agy --version` e `agy --help` funzionano; la lettura
`agy models` richiede autenticazione e non restituisce modelli.
Il record pubblico include istruzioni e documentazione ufficiale per questo
stato. L'[Antigravity Agent via Gemini
API](https://ai.google.dev/gemini-api/docs/antigravity-agent) usa una chiave Gemini
API e un sandbox gestito Google; non equivale all'OAuth o all'abbonamento IDE.

Per completare il login CLI su questo host, l'utente può avviare in un terminale
SSH interattivo:

```sh
AGY_CLI_DISABLE_AUTO_UPDATE=true agy
```

La [procedura ufficiale di installazione e
autenticazione](https://www.antigravity.google/docs/cli/install/) descrive il
passaggio URL nel browser, il codice da riportare nel terminale e lo storage
keyring Linux. Il codice di autorizzazione non va inviato in chat o nei log.
Dopo l'accesso, `/usage` legge le quote native. Questo login abilita la CLI del
proprio account; non collega automaticamente l'account al DB Svolo multiutente.
Il passaggio al DB richiede un adapter/export supportato e la separazione delle
credenziali per ogni utente web. Le interfacce documentate verificate non
stabiliscono tale contratto di esportazione. Svolo conserva quindi lo stato
Antigravity non collegato, anche se un login CLI viene completato separatamente.

Gli utenti nuovi configurano una propria chiave Gemini API oppure effettuano il
device login Codex da Svolo. Il device login richiede l'abilitazione prevista
dall'account o dal workspace ChatGPT. Vedi la [documentazione ufficiale OpenAI
sull'autenticazione](https://learn.chatgpt.com/docs/auth).

## Esecuzione e limiti

OpenAI-compatible e gli endpoint con function calling nativo passano al provider
il corpo Chat Completions e inoltrano la risposta JSON o SSE. L'endpoint Gemrouter
LAN importato su questo host ha risposto HTTP 400 con `Tool calling is not
supported on this router surface` quando riceve il catalogo nativo; una richiesta
chat minima allo stesso modello restituisce `SVOLO_OK`. La sua configurazione
persistente usa quindi `toolCalling: 'text'`, esposto anche nella configurazione
pubblica: **gli strumenti usano un protocollo testuale, non function calling
nativo**.

Questo bridge trasmette il catalogo come istruzioni JSON nel messaggio system e
richiede un oggetto completo `{"svolo_tool_calls":[{"name":"…","arguments":{}}]}`
oppure una risposta finale testuale. Non invia i campi nativi `tools`,
`tool_choice` e `parallel_tool_calls` a questo router. Le chiamate precedenti sono
rappresentate nel protocollo; i risultati tool sono marcati come dati non
attendibili. Il parser converte solo un envelope JSON completo, controlla nomi
presenti nel catalogo, argomenti object, tipi, proprietà, campi richiesti e limiti
numerici/di lunghezza dello schema e genera nuovi ID. L'adapter valida questo
subset JSON Schema e rifiuta chiamate con vincoli che non può validare (ad esempio
`$ref`, `pattern`, `not`); gli adapter nativi non hanno questa restrizione del
bridge. Una visita JSON separata limita la profondità e rifiuta chiavi prototype
anche nelle proprietà libere annidate. JSON dentro una citazione o un testo più ampio non viene
eseguito. Le richieste validate tornano al normale percorso di autorizzazione ed
esecuzione del core. Il modello può non rispettare questo protocollo e i risultati
di pagine/tool possono contenere prompt injection: la validazione strutturale non
sostituisce le autorizzazioni del core. La prova browser live deve quindi
verificare separatamente questa capacità del modello.
Su questo host, il 2026-10-05, la prova reale tramite gateway e core Go è terminata
in due step: `gemini-3.8-flash` ha richiesto lo stato browser e ha riportato
`Example Domain` / `https://example.com/` dai dati ricevuti. Il risultato è in
`svolo-installation/web-gemrouter-agent-browser-live.json`. Questo verifica il
percorso strumento/risultato/risposta per quella prova; non qualifica tutte le
operazioni browser o tutti i modelli del router.

Per il bridge testuale il gateway acquisisce una risposta completa e produce poi
Chat Completions/SSE; non mostra testo parziale durante quel turno. Il bridge è
limitato al Gemrouter importato dall'amministratore. Un cambio endpoint ripristina
il percorso nativo, salvo configurazione esplicita valida.

Il bridge pi converte testo, immagini inline,
definizioni tool, chiamate tool e risultati tool. Le chiamate OAuth usano
`ModelRuntime` con un `CredentialStore` SQLite dedicato al singolo utente e
provider. Le modifiche/refresh sono serializzati nel processo gateway e ogni
token ruotato viene salvato cifrato. Non usare più processi gateway sullo stesso
DB senza aggiungere un lock distribuito per il refresh.

Le risposte OAuth sono accumulate da pi e poi tradotte in ChatCompletion; quando
il core richiede streaming, il bridge emette SSE valido dopo il completamento
del turno. Questo percorso supporta i tool, ma non mostra testo parziale durante
l'esecuzione OAuth. Il catalogo Codex del runtime è un catalogo client installato,
non una prova di accesso dell'account a ogni modello. L'accesso effettivo è
verificato dalla richiesta al modello, come chiarisce la [documentazione
OpenAI per Codex app-server](https://developers.openai.com/siwc/token-sharing-open-source/codex-app-server).

Le quote Codex sono lette con `account/rateLimits/read`, protocollo ufficiale
[Codex App Server](https://learn.chatgpt.com/docs/app-server). Il processo usa un
`CODEX_HOME` temporaneo privato sotto la directory dell'utente Svolo, `auth.json`
con permessi `0600`, e il binario Codex stabile. Il gateway acquisisce il lock
credenziale, materializza l'account dal DB, acquisisce le finestre quota, salva
eventuali token ruotati e rimuove la directory temporanea. Non legge il Codex home
dell'utente del sistema e non effettua inferenza per controllare le quote.
Configurare `SVOLO_CODEX_BIN` oppure l'opzione `codexBin`; su questo host il
binario stabile è `.local/lib/svolo-toolchain/codex/codex` (0.160.0).

Per Gemrouter/OpenAI-compatible, Svolo espone un endpoint quote soltanto se
configurato esplicitamente, oppure gli ultimi header rate-limit ricevuti da una
vera risposta provider. `models/list` non viene scambiato per un endpoint quote.
Se il residuo non è disponibile, Svolo mostra `unavailable` e la ragione. Per
Gemini API i limiti sono del progetto; la documentazione indirizza a [Google AI
Studio per i limiti effettivi](https://ai.google.dev/gemini-api/docs/rate-limits).
Le [quote Antigravity CLI](https://antigravity.google/docs/cli/commands/usage)
sono separate; Svolo non le simula quando l'account/adapter non è disponibile.

Gli errori upstream riportati dal bridge sono sintetici: non vengono copiati
corpi di errore o stderr OAuth che potrebbero contenere segreti. Tutti i payload
pubblici sono privi di credenziali. Per verifiche locali senza spendere turni
provider: `node --test web/providers.test.mjs`. Le prove con SDK e upstream
simulati non qualificano una chiamata live; questa va verificata separatamente.
