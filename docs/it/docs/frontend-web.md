# Frontend Svolo Web

Il client in `web/public` è un’applicazione vanilla HTML/CSS/JavaScript a moduli,
servita dal gateway `web/server.mjs`. Non richiede una build JavaScript. La landing
pubblica, il login e il workspace funzionano con lo stesso server; i provider,
le sessioni e i server MCP arrivano dalle API autenticate, non da dati dimostrativi.

## Identità e schermate

Il frontend riprende il simbolo, i colori e il paesaggio approvati in `brand`.
Graphite `#0f172a`, ink `#10212e`, green `#00e676`, teal `#007b53`, mist
`#e8ecef` e bianco rimangono i riferimenti. Il tema predefinito è scuro:
graphite/ink per le superfici, mist per il testo, verde e mint per gli accenti.
Il tema chiaro usa bianco, mist e mint, con teal per il testo accentato.
`product-hero.webp` nella landing è etichettato come **concept**, non come
screenshot dell’applicazione. Le immagini del viewer sono invece catture reali
della sessione browser fornite dal core.

Percorsi pubblici: `/` e `/login`. `/app` apre il workspace e `/settings` apre
le impostazioni. La navigazione interna usa gli hash `#workspace`,
`#settings/providers`, `#settings/mcp`, `#settings/users` e `#settings/account`.
La sezione utenti è visibile soltanto agli amministratori. Non esiste signup
pubblico: l’amministratore crea gli accessi.
Il primo ingresso nel workspace crea una chat vuota: l’utente può scrivere subito
senza configurare prima il nome di una sessione. Questo non avvia il modello.

Su desktop il browser è a sinistra e la chat a destra, come nel riferimento di prodotto. A larghezze inferiori a 901 px
il pulsante Browser alterna le due viste; sotto 620 px la sidebar diventa
un menu. I moduli includono etichette dei campi, stato dei pulsanti, messaggi
di errore, focus visibile e riduzione delle animazioni secondo le preferenze.

Il pulsante del tema è disponibile nella landing, nel login e nella barra del
workspace/impostazioni, anche su mobile. La scelta viene salvata in
`localStorage['svolo:theme']`; `theme-init.js` la applica prima di caricare il CSS
per evitare un lampo del tema opposto. Se lo storage non è disponibile, il tema
scuro rimane utilizzabile. Il tema copre form, modali, tabelle, errori, toast,
stati delle schede e superfici della pagina. Artwork di prodotto e screenshot
delle pagine esterne conservano i propri colori originali.

## Moduli e contratto API

- `api.js`: richieste JSON same-origin con cookie, CSRF e timeout; escaping
  HTML dei valori dinamici; rappresentazione delle quote disponibili.
- `icons.js`: icone SVG applicative senza marchi esterni.
- `views.js`: schermate e componenti presentazionali.
- `app.js`: autenticazione, routing, form, polling degli eventi, approvazioni
  e interazioni nel browser.
- `styles.css`: identità visiva e layout responsive.
- `theme-init.js`: tema prima del primo rendering, senza credenziali nello storage.
- `markdown.js`: risposte GFM dell’assistente con sanitizzazione finale.
- `viewports.js`: catalogo condiviso delle sei dimensioni del browser.

Le URL degli asset JavaScript/CSS e tutti gli import del grafo ESM usano lo stesso
parametro di versione: un client con vecchi asset in cache non mescola il nuovo
DOM con moduli precedenti. Il server può inoltre rivalidare gli asset applicativi.

Il client legge `/api/me`, `/api/providers`, `/api/sessions`, `/api/mcp` e,
per gli amministratori, `/api/users`. `/api/me` e il login restituiscono
`{user,csrf}`. Le mutazioni hanno `X-CSRF-Token`; il login è gestito dal
gateway prima della sessione autenticata. Le credenziali dei provider sono
campi di form temporanei, inviati al server e mai salvati in localStorage.
Il client non conosce il token amministrativo del core.

Provider API: salvataggio `{provider:{id,label,kind,baseURL,model,models,apiKey?}}`,
selezione `{providerId,model}` e aggiornamento del catalogo con
`POST /api/providers/:id/models`. I tipi del servizio sono `gemrouter`,
`openai-compatible`, `codex-oauth`, `google-api-key`, `antigravity-oauth`;
i nuovi collegamenti con chiave API usano gli adapter realmente supportati.
Il tipo OAuth non viene trasformato in un collegamento con chiave API.
Lasciare una chiave vuota in modifica omette `apiKey` e conserva la chiave
precedente. Il servizio supporta la disconnessione con DELETE.

Il login Codex usa `POST /api/providers/codex/oauth/start` e interroga
`GET /api/providers/:id/oauth/status?loginId=…` ogni 1,5 secondi. Il client
mostra `event.userCode` e `event.verificationUri`; non richiede token incollati
manualmente. Un provider Codex già importato mantiene il proprio identificativo.
Il completamento viene dichiarato soltanto dopo lo stato del servizio.
Antigravity non collegato non viene presentato come OAuth funzionante;
la UI propone la configurazione Google Gemini con credenziali reali.

Quote: residui numerici diretti, header rate-limit osservati e finestre Codex
`primary`/`secondary` vengono mostrati quando presenti. La percentuale
disponibile Codex è `100 - usedPercent`; la durata viene dal dato del provider.
In assenza di dati il client mostra «Quota non disponibile dal provider»,
senza barre o valori inventati. La lettura delle quote non avvia un turno modello.

MCP: form esclusivamente HTTPS, anche per gli amministratori, coerentemente
con il servizio web. Salvataggio `{server:{id,name,url,token?}}`. I comandi
locali non sono esposti dal frontend. Il client distingue «Configurato» da
«Collegato»: la sola presenza del record non dimostra una connessione attiva.

## Chat, eventi e browser

Il client invia `POST /api/core/v1/runs` con sessione, provider, prompt,
`autonomy:'ask'`, `maxSteps:20` e `continue:true`. Il modello arriva dalla
selezione salvata nel servizio provider. Ogni 1,1 secondi legge runs,
approvals, control ed events per la sessione corrente. `message.delta`
aggiorna la risposta in corso; gli esiti finali e gli errori arrivano dai runs.
Il gateway aggiunge `run.prompt` per ricostruire le domande salvate al ricaricamento:
il core elimina `history` dalla risposta List. I prompt appena inviati restano
anche in memoria per la visualizzazione immediata.

Le risposte dell’assistente vengono renderizzate come GitHub Flavored Markdown,
sia durante lo streaming sia dallo storico: titoli, grassetto, enfasi, liste,
checklist statiche, citazioni, tabelle, link e codice. Il testo memorizzato resta
invariato; non vengono applicate euristiche per riscrivere i delimitatori.
I messaggi dell’utente restano testo letterale nel relativo `pre`.
Tabelle e blocchi di codice hanno scorrimento orizzontale nel proprio contenitore,
senza allargare la conversazione su mobile; il contenitore delle tabelle è
accessibile da tastiera. Il codice mantiene indentazione e caratteri letterali.

`renderMarkdown(text)` è sincrona. Usa [Marked](https://marked.js.org/) **18.1.0**
per il parsing GFM e [DOMPurify](https://github.com/cure53/DOMPurify) **3.4.16**
per la sanitizzazione finale con una lista limitata di tag/attributi HTML.
Le versioni esatte sono fissate nel package/lock web e verificate sul registry
npm ufficiale; ESM, source map e licenze originali sono vendorizzati localmente
in `web/public/vendor`, senza CDN. Marked da solo non sanitizza l’HTML.
L’HTML grezzo delle risposte viene escapato come testo prima della sanitizzazione;
script, handler, stili, frame, form e media non sono tag ammessi.
Le immagini Markdown diventano collegamenti espliciti, senza download automatici.
I link hanno `target="_blank"` e `rel="noopener noreferrer"`; la destinazione
decodificata viene validata per consentire soltanto HTTP, HTTPS, mailto e percorsi
relativi che risolvono su uno di questi protocolli. Un protocollo non ammesso
rimane testo. Se parser o sanitizzatore non sono utilizzabili, il fallback è
testo escapato, mai HTML grezzo eseguibile.

Le approvazioni mostrano strumento e argomenti e inviano una decisione esplicita
a `/api/core/v1/approvals`. Non vengono approvate automaticamente. Il pulsante
di controllo usa `/api/core/v1/control`; i clic e l’input umano passano da
`/api/core/v1/input`, che prende il controllo tramite il core.

Il browser usa screenshot da `/api/core/v1/view`, senza iframe di siti esterni.
La vista si aggiorna ogni 800 ms quando visibile; una sola richiesta immagine
può essere in corso. Le coordinate vengono convertite dalle dimensioni
visualizzate al viewport CSS restituito dal server. Sono disponibili clic,
testo, tasti, rotella, schede, navigazione, viewport desktop/tablet/mobile
e download della cattura. Le operazioni manuali usano `/api/core/v1/tools/call`.
Un viewport mobile cambia dimensioni e input; non promette un user agent
di dispositivo che il core non abbia confermato.

Il selettore usa sei preset dal catalogo condiviso, tutti con DPR 1:

| Gruppo | Dimensioni CSS | Mobile | Touch |
| --- | --- | --- | --- |
| Desktop | 1280 × 800 | No | No |
| Desktop | 1920 × 1080 | No | No |
| Tablet | 768 × 1024 | No | Sì |
| Tablet | 1536 × 2048 | No | Sì |
| Mobile | 393 × 852 | Sì | Sì |
| Mobile | 1080 × 1920 | Sì | Sì |

Il preset selezionato viene sincronizzato alle dimensioni effettive del PNG;
non viene dedotto dalla dimensione della finestra del frontend. Le coordinate
dell’input restano riferite al viewport del browser della sessione.

Per impostazione predefinita la vista segue la scheda con `active:true` di
`GET /api/core/v1/browser/tabs`, il target logico effettivo del core. Selezionare
una scheda nella UI cambia soltanto la vista locale con `GET /v1/view?tab=ID`:
non invia `switch-tab`, non passa il controllo e non altera il target dell’agente.
I badge «Agente» e «Vista» distinguono il target del core dalla scheda osservata.
«Segui agente» ripristina il seguito automatico; la chiusura di una scheda
osservata fa tornare la vista al target del core.

Le mutazioni di pagina usano l’ID esplicito della scheda osservata. Quando
l’agente possiede il controllo, il frontend riprende prima il controllo umano,
poi invia input/navigazione. Il pulsante di controllo applica immediatamente
la risposta del POST; `epoch` impedisce a un polling precedente di sovrascrivere
lo stato più recente. Un cambio di scheda svuota la vecchia cattura finché
arriva quella corretta, evitando input sul contenuto precedente. La richiesta
della cattura precedente viene annullata quando si sceglie un’altra scheda:
il nuovo viewer non deve attendere il timeout della scheda lasciata.

Gli eventi `tool.started`/`tool.finished` alimentano un indicatore dell’azione
con etichette come «Lettura pagina» o «Compilazione campo». L’esito viene
associato a `(runId,callId)` e alla scheda del tool; l’indicatore non mostra
argomenti, testo digitato, password o risultati del tool. È un riepilogo
dell’attività osservata, non una dichiarazione del risultato desiderato.

Le interruzioni, compreso `cancelled`, hanno una spiegazione italiana e invitano
a intervenire o chiedere di continuare. Il limite di passaggi viene indicato
come tale. I messaggi specifici degli errori del provider restano leggibili,
con lunghezza limitata; il client non mostra l’URL interno del broker per
spiegare un’interruzione. I dettagli tecnici rimangono nei log del server.

La navigazione abortisce le richieste e ferma i timer della schermata precedente.
Testo dei modelli, errori, nomi e argomenti sono sempre escapati: non diventano
HTML eseguibile. Le immagini accettano soltanto PNG/JPEG/WebP base64. Gli
errori del browser rimangono nella sua vista e non indicano falsamente che la
chat o il core si siano disconnessi.

## Verifiche effettuate

Controllo sintattico Node dei moduli. Smoke del client nel Chromium di
Electron 44.5.1 con Xvfb, contro il gateway locale reale sulla porta 7340:
landing, login, cookie autenticato, sessione, 25 modelli importati, provider,
quote Codex, modali, MCP HTTPS e pagina amministrativa utenti. Viewer reale
del browser verificato su desktop e mobile. Nessun errore JavaScript e nessun
overflow orizzontale alle larghezze 1440 e 390 px.

Le catture `frontend-web-*.png` e il risultato `frontend-web-smoke.json` sono
in `/home/your-user/svolo-installation`. Queste sono catture runtime del client,
distinte dagli artwork brand. Nessun turno modello è stato creato dal test del
frontend; eventuali attività presenti nelle immagini provengono dalle prove
del gateway. Il device login interattivo richiede l’accesso reale dell’utente;
la presenza del form non equivale al completamento di un nuovo login OAuth.

La verifica del tema ha controllato 13 catture desktop/mobile e 80 superfici
visibili (75 scure e 5 del tema chiaro), comprese landing, login, provider, MCP, utenti, account e modale:
tutte le superfici campionate del tema scuro erano scure, con contrasto
testo/sfondo calcolato almeno 4,5:1. Toggle e persistenza dopo reload verificati;
nessun overflow o errore JavaScript. Sono controlli campionati, non una
certificazione completa di accessibilità. Report `frontend-dark-smoke.json`
e catture `frontend-dark-*.png` nella directory delle verifiche.

La prova `frontend-viewer-contract-smoke.json` usa il browser reale e API
simulate per verificare seguito automatico, selezione locale senza POST,
badge distinti, ritorno al seguito, controllo immediato con poll obsoleto,
azioni associate al call ID, input sulla scheda osservata e assenza di
argomenti sensibili. Lo scroll differito viene scartato se nel frattempo cambia
la scheda osservata. Verifica anche le spiegazioni italiane di interruzione
e limite passaggi, conservando un errore specifico del provider. Non chiama
modelli reali e non sostituisce le prove di navigazione/tool del core reale.

La prova `frontend-viewer-real-smoke.json`, contro il gateway e il core reali,
ha osservato le due schede del form di verifica: cattura agente A, pin locale B,
ritorno ad A e viewer mobile. Target, proprietario ed epoch sono rimasti
invariati, con zero mutazioni API, zero errori JavaScript e nessun overflow.
Le catture `frontend-agent-*.png` mostrano i form runtime del test, non artwork.
La prova iniziale registrava catture B scadute al limite di 8 secondi,
recuperate dal polling successivo. L’indagine successiva ha riprodotto il blocco
nella cattura Chromium di schede statiche nascoste e abilitato
`CDPScreenshotNewSurface` con `fromSurface:true` nel core. Dopo il deploy,
`browser-aged-runtime-final.json` verifica tre prime catture B dopo 6,5 secondi
di inattività ciascuna: 13–14 ms, immagine B corretta, target A e controllo
invariati. Le regressioni native verificano anche che il focus fisico resti su A.

La prova congiunta `frontend-live-operative-watch.json` ha osservato un turno
Gemrouter reale avviato dal harness del gateway su due form locali autorizzati.
La sessione partiva dalla scheda B; il viewer ha seguito automaticamente A
quando il core l’ha scelta per compilare il form. Durante il turno sono stati
registrati 206 campioni con A operativa e visibile e 7 immagini distinte di A;
nessun campione del turno aveva il viewer vuoto. Il primo campione con target
agente A mostrava già A. Le catture `frontend-live-operative-A-1.png` fino a
`frontend-live-operative-A-5.png` mostrano la compilazione, con indicatore
dell’attività e tema scuro. Zero mutazioni API dal watcher, errori JavaScript
e overflow. Il retarget finale a B è dovuto alla verifica manuale del harness
dopo il turno; la cattura finale segue quel target. Questa prova dimostra la
visibilità dell’azione nella UI; non misura il focus nativo di Chrome durante
l’intero turno e rimane distinta dalle regressioni native del core.

Una seconda prova con API simulate e il browser reale ha verificato invio
del prompt, payload del runner, visualizzazione della domanda persistita,
streaming, escaping di un frammento HTML ostile, consenso esplicito,
coordinate dell’input umano e CSRF su ogni mutazione. Tutte le verifiche sono
passate, senza errori JavaScript. Il risultato è in
`frontend-contract-smoke.json`: è una simulazione dei contratti UI, distinta
dalla verifica del gateway e del browser effettivi.

La prova mirata `frontend-markdown-smoke.json` usa il renderer reale nel browser
Electron con API simulate: GFM storico, prompt utente letterale, delimitatori
Markdown incompleti nello streaming e ricostruzione dopo reload. Verifica
13 payload ostili, link con entità, assenza di esecuzione e download esterni,
tabelle/codice lunghi contenuti a 1440 e 390 px nei due temi. Tutti i sei preset
UI inviano dimensioni/flag/tab/CSRF corretti; le fixture PNG hanno le dimensioni
reali dichiarate e il selettore le riconosce. Questi controlli non chiamano
provider reali e restano distinti dalle prove native del core sui sei viewport.
Le catture `frontend-markdown-*.png` sono immagini del client con dati simulati.
La prova separata `markdown-real-history.json` verifica una risposta già salvata
dal gateway reale, con 7 righe tabella, 3 titoli e 15 elementi in grassetto:
desktop, mobile e reload, senza turno modello aggiuntivo o mutazioni API.

La prova finale `frontend-viewer-after-capture-fix.json`, dopo il deploy del core,
ha verificato A → osservazione B → Segui agente A, su desktop e mobile. B è
rimasta senza catture per sette secondi: la prima vista è arrivata in 15 ms.
Le 19 catture hanno impiegato 13–26 ms, senza timeout, errori JavaScript, overflow
o mutazioni API; target, proprietario ed epoch invariati.

## Lingua e colonne

Il pulsante EN/IT passa direttamente tra le due lingue. Il comando nella chat chiude la colonna destra; il pulsante flottante sul browser la riapre. La barra sinistra si comprime lasciando logo, nuova chat, impostazioni e profilo. Lingua e preferenze delle colonne vengono ricordate nel browser. Le bozze restano durante questi cambiamenti; il controllo del browser e l'attività dell'agente non cambiano. Su mobile resta disponibile la navigazione completa tramite il menu.
