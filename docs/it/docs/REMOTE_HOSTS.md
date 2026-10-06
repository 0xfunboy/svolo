# Host remoti

## Modello operativo

Ogni host esegue un daemon Svolo con dati e autenticazione propri. OpenSSH risolve alias,
chiavi, porta e host intermedi dalla configurazione dell'utente. Il client non accetta
automaticamente chiavi host sconosciute e non attiva l'inoltro dell'agent SSH.

Prima di collegare un host, verificarlo con il client OpenSSH e predisporre un accesso
non interattivo autorizzato. Non disabilitare il controllo dell'identità dell'host per
far passare un test. Il comando di risoluzione usa `ssh -G`; non è un test di connessione.

## Collegamento

Aggiungere un ID locale, un alias SSH, la porta remota e un riferimento alla credenziale
del daemon. I segreti non vanno inseriti direttamente nella configurazione dell'host.
L'installazione del daemon è un'operazione separata ed esplicita che trasferisce un
binario compatibile, ne verifica SHA-256 e avvia il servizio utente.

Il core locale deve conoscere una directory degli artefatti con `--artifacts-dir`.
Gli script di cross-compilazione producono i binari per tale directory, ma solo una
prova sul sistema target ne dimostra il corretto avvio.

Un tunnel aperto non è sufficiente: la connessione viene considerata pronta dopo la
risposta autenticata del daemon e il controllo del protocollo. La disconnessione
esplicita prevale sulla riconnessione automatica già pianificata.

## Risorse locali e remote

Il browser gestito, il workspace e il modello devono rimanere disponibili per continuare
un'attività quando il client chiude. Un browser incorporato nella finestra desktop non
può sopravvivere alla terminazione di quel processo. Le finestre native richiedono una
sessione grafica; SSH da solo non crea un desktop interattivo.

I percorsi sono interpretati sull'host proprietario. Gli allegati vengono trasferiti
esplicitamente. Il replay degli eventi dopo una disconnessione non autorizza il replay
automatico di azioni esterne dal risultato incerto.

## Qualifica

Verificare alias con ProxyJump, chiave host cambiata, connessione interrotta, reconnect,
riavvio del daemon, scambio dei file e assenza di contaminazione tra sessioni. La
[verifica corrente](../../verification.json) specifica quali prove sono state eseguite nella
preparazione di questa distribuzione; una simulazione non è una prova su due host reali.
