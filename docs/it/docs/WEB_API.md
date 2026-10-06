# API del gateway web

Il contratto descritto qui è implementato in `web/server.mjs`,
`web/providers.mjs` e `web/cores.mjs`. Il gateway Linux aggiunge autenticazione
multiutente e isolamento dei core al prodotto `0.3.1-dev`; non sostituisce
il contratto locale documentato in [API.md](API.md). La pubblicazione web
non cambia `productionQualified`, che rimane `false`.

## Trasporto e autenticazione

Il gateway ascolta su `127.0.0.1:7340` per impostazione predefinita; il client
pubblico usa HTTPS tramite il proxy/tunnel documentato in
[WEB_DEPLOYMENT.md](WEB_DEPLOYMENT.md). Sono accettati soltanto il `Host`
pubblico configurato e l’indirizzo loopback del gateway. Se presente,
`Origin` deve coincidere con l’origine del `Host` richiesto.

Il login emette il cookie `__Host-svolo` con `Path=/`, `HttpOnly`, `Secure`,
`SameSite=Lax` e durata di 12 ore. Non si invia un token core dal browser.
`GET /api/me` e il login restituiscono `{user,csrf}`; tutte le mutazioni
autenticate richiedono `X-CSRF-Token` uguale al valore della sessione.
Il login precede la sessione autenticata e non richiede questo header.
Non è disponibile una registrazione pubblica degli utenti.

I corpi delle richieste sono oggetti JSON con `Content-Type: application/json`.
Le risposte normali sono JSON, salvo il download degli artefatti. Gli errori
del gateway hanno `{error,requestId}` e `X-Request-ID`; gli errori interni
500 non espongono stack o credenziali. Codici usati: 400 per input/operazioni
non validi, 401 per accesso mancante, 403 per origine/CSRF/operazioni vietate,
404 per risorse assenti, 409 per conflitti, 413/415 per corpo troppo grande
o formato errato, 429 per limiti, 503 per pool occupato. Non interpretare
un timeout del client come annullamento di un’azione già inviata al core.

## Accesso e utenti

| Metodo e percorso | Richiesta | Risposta / autorizzazione |
| --- | --- | --- |
| `GET /api/health` | Nessuna | Pubblica: `{ok:true,service:'svolo-web',version:'0.3.1-web',authentication:'session',productionQualified:false}`. Verifica il gateway, non tutti i provider o browser. |
| `POST /api/login` | `{username,password}` | `{user,csrf}` e cookie. Nome utente massimo 32 caratteri; password massimo 256. |
| `GET /api/me` | Cookie | `{user:{id,username,role,disabled},csrf}`. |
| `POST /api/logout` | `{}` e CSRF | `{ok:true}`; revoca la sessione e cancella il cookie. |
| `GET /api/users` | Cookie amministratore | `{users:[{id,username,role,disabled}]}`. |
| `POST /api/users` | `{username,password,role}` | HTTP 201, `{user:{id,username,role}}`. Solo amministratore. |

Creazione utenti: nome da 3 a 32 caratteri `[A-Za-z0-9_.-]`, univoco senza
distinzione fra maiuscole e minuscole; ruolo `user` o `admin`; password da
10 a 256 caratteri. Il frontend richiede almeno 12 caratteri. Limite attuale:
100 utenti. Non sono implementati endpoint pubblici di modifica del ruolo,
reset password o cancellazione utente.

## Provider, modelli e quote

| Metodo e percorso | Richiesta | Risposta |
| --- | --- | --- |
| `GET /api/providers` | — | `{providers,defaultProvider,defaultModel}` dell’utente autenticato. |
| `POST /api/providers` | `{provider:{…}}` | Stessa struttura del GET dopo il salvataggio. |
| `DELETE /api/providers?id=…` | CSRF | Lista aggiornata; rimuove anche la preferenza se riferita al provider. |
| `POST /api/providers/select` | `{providerId,model}` | Lista aggiornata; salva la scelta e sincronizza il core già avviato. |
| `POST /api/providers/:id/models` | `{}` | Lista aggiornata dopo il refresh del catalogo. |
| `POST /api/providers/:id/oauth/start` | `{}` | Per Codex: `{loginId,status:'starting'}`. |
| `GET /api/providers/:id/oauth/status?loginId=…` | — | Stato del login appartenente all’utente e al provider richiesti. |
| `POST /api/providers/:id/oauth/callback` | `{loginId}` | Legge lo stato; non accetta token OAuth manuali. |

I campi canonici di salvataggio sono `id`, `label`, `kind`, `baseURL`,
`model`, `models:[{id,label,vision?}]` e, per una chiave API, `apiKey`.
Il gateway accetta `name` come nome e `baseUrl` come endpoint; la UI usa
il contratto canonico del servizio. Sono supportati `gemrouter`,
`openai-compatible`, `codex-oauth`, `google-api-key`, `antigravity-oauth`.
Il core riceve un adapter Chat Completions verso il broker del gateway;
il suo `kind` non va confuso con il tipo salvato nel servizio provider.

Omettere `apiKey` conserva la chiave corrente; il servizio interpreta una
chiave esplicitamente vuota/null come rimozione della credenziale. Il client
web omette i campi segreti lasciati vuoti in modifica. OAuth si collega
tramite il login del provider: il salvataggio pubblico non importa credenziali
OAuth grezze. Il catalogo e `configured:true` indicano configurazione e
presenza della credenziale, non accesso accertato a ogni modello.

La lista pubblica include `id`, `label`/`name`, `kind`, `model`, `baseURL`,
`models`, `configured`, `authType`, `source`, `expiresAt`, `vision`,
`maxOutputTokens`, `toolCalling`, `updatedAt` ed eventuali `issue`,
`toolCallingDescription`, `onboarding` e `quota`;
non include API key, access token o refresh token. Massimo 32 provider
per utente. Gli endpoint ordinari usano HTTPS pubblico; l’importazione
amministrativa una tantum può autorizzare il Gemrouter LAN configurato.

Il Gemrouter importato può dichiarare `toolCalling:'text'`: il broker invia
un catalogo testuale e converte le risposte nel protocollo tool del core
soltanto dopo validazione del nome e degli argomenti. È un adattamento per
un endpoint privo di function calling nativo. Codex usa `toolCalling:'native'`.
Le approvazioni e le restrizioni core si applicano a entrambe le modalità.

Il login device Codex usa normalmente l’identificativo `codex`. Gli stati
sono `starting`, `pending`, `completed`, `failed`, `cancelled`. In attesa,
`event` può contenere `{type:'device_code',userCode,verificationUri,expiresInSeconds}`;
in errore può comparire `reason`. Il frontend mostra codice e link, poi
interroga lo stato; il server salva la credenziale risultante. I login
pendenti sono in memoria, scadono e non sopravvivono al riavvio del gateway.

`quota` può essere `unavailable` con `reason`, `source` e `checkedAt`, oppure
`available` con finestre Codex in `buckets`, dati di un endpoint esplicito
in `data` o header rate-limit osservati in `headers`. La cache dura almeno
60 secondi. La lettura non effettua un’inferenza. Non inventare residui
quando il servizio restituisce `unavailable`. Dettagli del servizio e
dei protocolli provider: [web-providers.md](web-providers.md).

## MCP e sessioni

| Metodo e percorso | Richiesta | Risposta |
| --- | --- | --- |
| `GET /api/mcp` | — | `{servers:[{id,url,enabled,configured}]}`. |
| `POST /api/mcp` | `{server:{id,url,enabled?,token?}}` | Lista aggiornata dopo sincronizzazione e refresh MCP del core. |
| `DELETE /api/mcp?id=…` | CSRF | Lista aggiornata dopo la rimozione. |
| `GET /api/sessions` | — | Array delle sessioni del core dell’utente. |
| `POST /api/sessions` | `{name}` | HTTP 201, sessione creata. |

MCP web accetta soltanto URL HTTPS pubblici senza credenziali, query o fragment.
`command` viene rifiutato anche per amministratori. Gli identificativi MCP
usano `[A-Za-z0-9_-]`, da 1 a 64 caratteri; massimo 16 server per utente.
Omettere `token` conserva la credenziale precedente. `configured` segnala
la presenza della credenziale salvata, non prova una connessione attiva.
Il campo `name` della UI non viene attualmente conservato dal gateway.

Le sessioni hanno un ID generato dal gateway, nome massimo 100 caratteri,
workspace `/workspace` nel namespace dell’utente e `allowExec:false`.
Non è possibile scegliere un percorso host arbitrario da questo endpoint.
Massimo 32 sessioni per utente.

## Proxy autorizzato del core

Il prefisso è `/api/core`, seguito dall’esatto percorso core. Ogni richiesta
è indirizzata al core dell’utente autenticato: il client non seleziona un
core appartenente a un altro account. I contratti delle operazioni passanti
restano quelli del [core](API.md); il proxy autorizza questi percorsi:

| Metodo | Percorsi core consentiti |
| --- | --- |
| GET | `/v1/health`, `/v1/config`, `/v1/control`, `/v1/tools`, `/v1/browser/tabs`, `/v1/view`, `/v1/runs`, `/v1/approvals`, `/v1/events`, `/v1/events/cursor`, `/v1/artifacts`, `/v1/artifact`, `/v1/projects/board`, `/v1/projects/laments`, `/v1/transfers/status`, `/v1/transfers/chunk` |
| POST | `/v1/control`, `/v1/tools/call`, `/v1/input`, `/v1/runs`, `/v1/runs/stop`, `/v1/approvals`, `/v1/projects/board`, `/v1/projects/laments`, `/v1/mcp/refresh`, `/v1/transfers`, `/v1/transfers/chunk`, `/v1/transfers/commit`, `/v1/transfers/abort`, `/v1/transfers/download` |

Configurazione PUT, vault, token, daemon stop, host SSH e altre route del
core non sono esposti. `/v1/config` GET elimina le referenze alle credenziali
dei provider/MCP. Gli strumenti `workspace-exec`, `computer-use`,
`profile-import`, `call-routine`, `pi-computer`, `pi-kanban`, `pi-atp`,
`pi-lament` sono esclusi dal catalogo web, dalle operazioni manuali e
dall’insieme ammesso al runner.

### Chat ed eventi

`POST /api/core/v1/runs` accetta `{session,provider?,prompt,maxSteps?,autonomy?,continue?,allowedTools?,toolScope?}`.
Il gateway applica il provider preferito se omesso, autonomia `ask` oppure
`browser`, da 1 a 50 passaggi, continuazione predefinita e strumenti della
lista consentita. Un `allowedTools` esplicito deve esserne un sottoinsieme:
questo campo preapprova gli strumenti, non amplia il catalogo. `toolScope`
restringe il catalogo del modello e l'esecuzione allo stesso sottoinsieme;
se omesso comprende gli strumenti web consentiti. Un elemento fuori dalla
lista web viene rifiutato, anche se il client tenta di preapprovarlo.
Il limite del gateway è tre run con stato `running` per core; il core
impedisce inoltre più run contemporanei nella stessa sessione.

`GET /api/core/v1/runs` restituisce i run con ID, sessione, provider, modello,
stato, orari, passaggi, testo ed eventuale errore. Il gateway aggiunge `prompt`
quando la domanda è stata salvata tramite questo endpoint; il core elimina
`history` dalla risposta della lista. I prompt sono persistiti per
`(user_id,run_id)` e cifrati con il contesto dell’utente.

`GET /api/core/v1/events?session=…&after=…` restituisce un array di eventi
`{seq,at,session,type,data}`. Il client conserva l’ultimo `seq` ricevuto.
`message.delta` contiene `{runId,text}`; `tool.started`/`tool.finished`
includono nome, ID della chiamata e, quando presente, ID della scheda;
il client mostra l'attività senza esporre i valori dei campi. `browser.context`
descrive l'osservazione delle schede usata dal modello in quel passaggio.
Gli eventi
`run.started`, gli esiti `run.*` e `approval.requested` descrivono l’attività.
Il frontend usa polling; `/v1/events/stream` non è nella lista web corrente.

Le approvazioni arrivano da GET `/v1/approvals` e ricevono
`POST /v1/approvals {id,approved}`. Il consenso rimane esterno al modello.
`POST /v1/control {session,owner:'human'|'agent'}` cambia il controllo;
`POST /v1/runs/stop {id}` interrompe un run. L’interruzione non annulla
transazioni esterne già completate.

### Browser e artefatti

`GET /v1/browser/tabs?session=…` osserva le schede e restituisce `active:true`
per il target operativo selezionato dal core; `GET /v1/control` include `tab`.
`GET /v1/view?session=…&tab=…` restituisce `{mimeType,data,tab,viewport?}`,
con immagine base64 e viewport CSS. Questa è una cattura della sessione
effettiva. Non è un iframe, un duplicato del browser o un desktop remoto completo.
Queste letture non selezionano il target operativo, non cambiano la proprietà
del controllo e non aspettano la fine di una lunga operazione dell'agente.
Il frontend segue il target operativo per impostazione predefinita; scegliere
una scheda fissa soltanto la vista locale. «Segui agente» riattiva l'inseguimento.

L’input umano usa `POST /v1/input {session,arguments:{kind,…,tab?}}`, con
`kind` `click` (x/y), `text` (text), `key` (key) o `wheel` (x/y/deltaX/deltaY).
Il core interrompe l’agente e prende il controllo umano prima dell’input.
Le operazioni manuali usano `/v1/tools/call {session,name,arguments}`:
navigazione, schede, viewport, snapshot e screenshot mantengono i contratti
del core. Le mutazioni richiedono il controllo umano durante un run; dopo il
passaggio esplicito possono procedere anche se una vecchia chiamata cancellata
sta ancora terminando. Le osservazioni in sola lettura non prendono il controllo.
La scheda degli input corrisponde alla cattura mostrata, con ID esplicito.
Un dispatch riuscito non è prova dell’esito desiderato sulla pagina.

L'[harness dell'assistente](AGENT_HARNESS.md) richiede ID di scheda espliciti
per gli strumenti di pagina, riferimenti aggiornati e verifica degli esiti.
Assiste anche la compilazione dei moduli di registrazione autorizzati dall'utente.

`GET /v1/artifacts?session=…` elenca i file verificati;
`GET /v1/artifact?session=…&id=…` è l’eccezione binaria del proxy, con
`Content-Type: application/octet-stream` e header di download del core.

## Superficie riservata e limiti

`/internal/provider/:userId/:providerId/v1/...` è un broker loopback riservato
al core, autenticato con credenziale interna e legato all’utente. Non è una
API pubblica per frontend, MCP o client esterni. Il percorso modello usato
dal core è Chat Completions; il gateway non permette un proxy HTTP arbitrario.

Limiti di corpo: login/utenti/selezione/sessioni 4 KiB, OAuth 8 KiB, MCP
32 KiB, provider 128 KiB, richieste generiche 16 MiB. I limiti di risorse
dei processi e la capacità effettiva sono descritti in
[WEB_SECURITY.md](WEB_SECURITY.md). Una risposta health 200, un catalogo
modelli o una prova di testo non certificano il ciclo live completo di tool,
approvazioni, immagini e servizi esterni.
