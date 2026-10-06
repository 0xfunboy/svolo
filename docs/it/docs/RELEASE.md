# Requisiti di rilascio

## Stato

Questa revisione è una distribuzione di sviluppo. `productionQualified` è `false` nel
manifest del prodotto. Il comando di pubblicazione rifiuta di procedere; non è configurato
un server di aggiornamenti. Una build locale o il passaggio dei test Go non autorizza
a cambiare questo stato da solo.

## Condizioni da soddisfare

| Ambito | Verifica richiesta prima di una release stabile |
| --- | --- |
| Core | Build riproducibile, vet, race detector, test negativi, ripristino dopo arresto e limiti di risorse. |
| Desktop | Typecheck completo, test renderer/main/preload, build e prova del pacchetto installato. |
| Browser | Stessa sessione per input umano e agente, popup/dialoghi, download/upload, autenticazione e controllo coordinato. |
| Host | Collegamenti OpenSSH reali, riconnessione, aggiornamento daemon, credenziali e trasferimenti su host separati. |
| Modelli/MCP | Provider reali e server MCP autorizzati; streaming, tool, errori e immagini, non soltanto simulazioni. |
| Piattaforme | Windows, macOS, Linux X11/Wayland nei target dichiarati; permessi nativi e stop d'emergenza. |
| Distribuzione | Installer testati, firma del publisher, notarizzazione ove necessaria, canale di update verificato e rollback. |
| Sicurezza | Revisione indipendente delle superfici di esecuzione, dati sensibili e dipendenze; prove prolungate. |

I pacchetti di sviluppo usano identità e icone Svolo ma non dichiarano di essere firmati
per la distribuzione. La configurazione macOS ad-hoc non è una firma Developer ID.

## Comandi di packaging

Da `desktop`, dopo le verifiche. Ogni comando richiede il sistema operativo di
destinazione e un runtime Node x64/ARM64 corrispondente al core incluso:

```bash
pnpm dist:linux
pnpm dist:windows
pnpm dist
```

`dist` prepara il pacchetto macOS e richiede gli strumenti nativi. La cross-compilazione
del core è separata dal packaging desktop: il controllo preliminare rifiuta un host o variabili GOOS/GOARCH non corrispondenti.
`build/core/build-info.json` registra il target effettivo. Non includere chiavi o certificati nella sorgente.

Prima di pubblicare, registrare l'hash dell'input, la revisione del codice, le dipendenze,
l'ambiente e gli esiti dei test. I dati devono riferirsi al pacchetto effettivamente
distribuito e includere ogni controllo non eseguito.
