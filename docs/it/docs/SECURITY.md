# Sicurezza e trattamento dei dati

## Confine di fiducia

Pagina web, risposta del modello, contenuto MCP e file del workspace sono input non
attendibili. Nessuno di questi input può concedere permessi amministrativi. Le approvazioni
sono controlli dell'applicazione, non istruzioni nel prompt. Non c'è una garanzia di
immunità alla prompt injection.

Il core ascolta solo su un IP esplicito di loopback e richiede autenticazione per le
API. I token MCP limitati a una sessione non hanno i privilegi dell'admin token. Il
renderer non riceve accesso generale a Node.js e le pagine esterne non sono target
amministrativi del controller browser.

## Esecuzione dei programmi

`workspace-exec` è disabilitato finché la sessione non autorizza l'esecuzione. L'ambiente
dei processi viene minimizzato, i percorsi sono confinati dove l'operazione lo prevede
e i programmi hanno budget temporali. **Queste misure non sono una sandbox del sistema
operativo.** Eseguire codice non attendibile in una macchina o contenitore isolato.

La sandbox di Chromium deve rimanere attiva nell'uso normale. Il flag di test che la
disabilita richiede un'abilitazione separata e non va inserito negli script di produzione.

## Segreti

Il vault usa credenziali referenziate, non valori nei file di configurazione. Tenere la
chiave del vault fuori dalla directory dati protetta e separare il suo backup. File,
registrazioni, conversazioni e profili possono contenere informazioni sensibili anche
se le credenziali del provider sono cifrate.

Non caricare token, directory dei profili, dump dell'ambiente o chiavi private nei report
di errore. Le variabili private del core non vengono inoltrate intenzionalmente ai tool;
le variabili ammesse esplicitamente per un server esterno richiedono la stessa attenzione.

## Input e controllo remoto

Fermare il controller blocca nuove azioni, non revoca transazioni già completate. Le
finestre native e il viewer remoto hanno limiti differenti: non attribuire alla pagina
le garanzie di un desktop isolato. Usare solo applicazioni autorizzate ed evitare terminali
e pannelli di gestione dei permessi nel controllo generale dell'agente.

## Risorse e aggiornamenti

Sono presenti limiti per singole operazioni e file. Il modulo delle quote aggregate
non copre ancora tutti i percorsi. Monitorare lo spazio disponibile e le registrazioni.
Non è configurato un publisher per gli aggiornamenti del desktop: la distribuzione
sorgente non scarica né sostituisce automaticamente l'applicazione.

## Segnalazioni

Prima di condividere una riproduzione eliminare segreti e dati personali. Usare il
contatto del titolare indicato nella [licenza](../../../LICENSE.md), senza pubblicare una
chiave o un exploit completo insieme a dati sensibili.
