# Assistenza nel browser e compilazione dei moduli

Il runner in `core/internal/agent/runner.go` guida gli agenti nel browser condiviso
autorizzato dall'utente. L'assistente può aiutare a creare account e compilare moduli:
non applica un rifiuto generale delle registrazioni o degli alias e personaggi scelti
esplicitamente dall'utente. Usa i dati forniti o approvati e chiede soltanto le
informazioni obbligatorie mancanti, dopo aver letto il modulo reale. Non inventa
dati personali, indirizzi email o identità senza mandato e non attribuisce al sito
regole dei termini di servizio senza evidenza pertinente. Un vecchio rifiuto
dell'assistente conservato nella conversazione non diventa una policy.

CAPTCHA e verifiche di accesso richiedono il contributo dell'utente. L'assistente
può chiedere il codice ricevuto dall'utente o il completamento manuale nel browser,
poi riprendere la procedura autorizzata. Non aggira i controlli e non propone modi
per eludere le restrizioni del provider. Una conferma di intervento umano non prova
che la pagina sia pronta: occorre leggere nuovamente lo stato.

## Contesto corrente e schede esplicite

`Manager.BrowserContext` è un callback facoltativo di osservazione del browser. Il
server lo collega a `Engine.Observe(ctx, session, "tabs", nil)`. Prima di ogni richiesta
al provider, il runner acquisisce solo i metadati delle schede appartenenti alla
sessione: `id`, `url`, `title`, `type` e `active`. Non effettua una chiamata nascosta
a `Execute`, non cambia la selezione e non assume che la prima scheda sia quella
scelta. L'osservazione è limitata a 64 schede; titolo, URL e tipo hanno limiti di
dimensione. Query, frammenti e credenziali inserite negli URL vengono rimossi.

Il modello riceve questi metadati come dati temporanei marcati esplicitamente non
attendibili. Non sono inseriti nelle istruzioni di sistema né accumulati nello storico
persistente della conversazione. L'evento `browser.context` registra l'osservazione
con ID dell'attività e numero di passo. Se il callback manca, fallisce o lo scope
esclude `tabs`, l'assistente usa gli strumenti disponibili per osservare il browser;
non vengono inventati metadati o target.

Per gli strumenti che operano su una pagina esistente, il catalogo destinato al
modello richiede `arguments.tab`, contenente l'ID esatto di una scheda autorizzata.
Il runner rifiuta le chiamate prive di questo campo prima di chiedere approvazione
o eseguire lo strumento. `switch-tab` richiede anche `query` uguale a quell'ID.
`tabs`, `open-tab` e `open-browser` non richiedono una scheda già esistente. Le
forme API e CLI restano distinte dal catalogo rafforzato del modello.

Uno snapshot produce riferimenti validi soltanto per la sua scheda e il suo
documento. Dopo navigazione, cambio di documento o passaggio del controllo,
l'assistente deve acquisirne uno nuovo. Per un modulo, il percorso atteso è:
osservare campi e target, inserire i valori autorizzati, seguire il flusso richiesto
di invio e approvazione, quindi verificare il risultato nella stessa scheda.
La risposta di un click indica l'invio dell'azione, non il successo della registrazione.

Gli eventi `tool.started` e `tool.finished` includono ID dell'attività, ID della
chiamata, nome dello strumento e ID della scheda quando presente. Non includono
i valori dei campi, le password o gli argomenti del modulo. L'esito dello strumento
resta nello storico del core secondo il contratto esistente.

## Scope, approvazioni e ripresa

`RunRequest.ToolScope` limita le capacità del singolo incarico. Quando è assente,
il catalogo mantiene la compatibilità esistente; un array vuoto esclude tutti gli
strumenti. Quando è presente, vengono esposti solo i nomi indicati e il runner
rifiuta gli altri prima delle approvazioni e dell'esecuzione. Anche le primitive
richiamate tramite `browser-operation` e `browser-task` devono appartenere allo
scope. `AllowedTools` conserva il significato precedente di preapprovazione:
non amplia `ToolScope` e non consente di aggirarlo. Wrapper che avviano workflow
autonomi richiedono un contratto del proprio esecutore; il gateway web esclude
`call-routine` e gli altri strumenti privilegiati dalla propria superficie ammessa.

Con `continue:true`, il runner riprende lo storico dell'ultima attività terminale
della stessa sessione, provider, modello e protocollo, anche se cancellata, fallita
o interrotta dal riavvio. Preserva l'obiettivo e i dati dell'utente. Una serie di
chiamate agli strumenti con risultati mancanti viene rimossa insieme ai suoi
risultati parziali e alle immagini associate, evitando ID di chiamata senza risposta
nel protocollo del provider. La nota di continuità segnala l'esito incerto: le
azioni precedenti non vengono riprodotte automaticamente e il modello deve
verificare la pagina corrente prima di proseguire.

Stop restituisce subito il controllo all'utente. La sessione rimane occupata fino
alla chiusura dell'esecuzione precedente, alla persistenza dello stato finale e al
rilascio del suo controllo: una nuova attività non può perdere il proprio controllo
a causa della chiusura tardiva della precedente. Un risultato arrivato dopo la
cancellazione non trasforma l'attività in un completamento riuscito.
Una richiesta Stop riferita a un'attività storica o già terminale viene rifiutata;
non può togliere il controllo a un'attività successiva. Anche il passaggio del
controllo avviato da Stop deve terminare prima che la sessione diventi riutilizzabile.

## Verifica

```bash
cd core
GOFLAGS=-buildvcs=false go test -race -count=1 ./internal/agent
```

`runner_harness_test.go` usa provider HTTP e browser simulati, database temporanei
e dati fittizi di test. Verifica i contratti nei protocolli Responses e Chat
Completions, contesto aggiornato e non elevato a policy, scope, selezione esplicita,
valori forniti dall'utente, domanda dell'email mancante, intervento manuale e
verifica successiva, continuità dopo interruzione e concorrenza nel controllo.
Questi test non registrano account reali e non qualificano i termini, i moduli o
i flussi OAuth dei servizi esterni.

`server/viewport_test.go` verifica con un provider simulato la richiesta di una
credenziale mancante e la risposta dell'utente, ridimensionando la pagina durante
l'inferenza senza cancellare il lavoro. `browser/viewport_chromium_test.go` verifica
separatamente geometria, valori del modulo conservati, recupero dai riferimenti
obsoleti, inserimento di una password fittizia e focus con Chromium reale
(`SVOLO_E2E=1`). `web/browser-state.test.mjs` usa un browser nativo e API HTTP
simulate per schede attive in entrambe le lingue, ridimensionamento senza takeover,
aggiornamenti stabili e recupero delle catture temporaneamente fallite. Nessuna
prova invia una registrazione reale.
