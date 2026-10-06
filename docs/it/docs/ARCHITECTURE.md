# Architettura

## Confini del sistema

Svolo è un'applicazione multiprocesso con un core Go e un desktop Electron/React.
Il core può funzionare senza il desktop attraverso la console HTTP autenticata.
Un host remoto esegue il proprio core; il client locale collega la sessione mediante
OpenSSH. Il modello è un servizio configurabile e non possiede lo stato del browser.

```text
Desktop React / Electron                Console locale
        | broker IPC                         | HTTP autenticato
        +---------------- core Go -----------+
                         |       |
                 sessioni/tool   runtime pi / workflow
                         |
                 trasporto browser
                 |               |
          Chromium gestito   adapter Electron
                         |
                  host remoti SSH
```

## Proprietà dello stato

`internal/store` gestisce scritture atomiche, lock della directory e registro eventi.
`internal/server` possiede configurazioni, autenticazione, sessioni e autorizzazioni.
`internal/agent` esegue i turni del modello e le richieste di strumenti. Le schermate
React non possono dichiarare conclusa un'azione che il backend non ha confermato.

`internal/browser` associa ogni sessione ai target autorizzati. Il trasporto gestito
avvia Chromium; il trasporto desktop inoltra le operazioni ai contenuti Electron
registrati. L'identità del documento e la versione del controllo invalidano i riferimenti
agli elementi dopo un cambio di pagina o un intervento umano.

## Esecuzione e workflow

`internal/piruntime` supervisiona processi RPC esterni. `internal/atp` supervisiona
l'esecuzione del grafo, i worker e il loro stato; le mutazioni ATP usano il librarian
Python distribuito nelle risorse del desktop. I riduttori di progetto e il repository
Go gestiscono Kanban e segnalazioni. Il renderer mantiene modelli di vista e interazioni;
non tutto il codice applicativo è scritto in Go.

`internal/gitops` e `internal/github` circoscrivono le operazioni al workspace e agli
account autorizzati. Non sono un'autorizzazione generale a pubblicare modifiche.
`internal/workflow` esegue routine JSON deterministiche con limiti di durata e passi.

## Modelli e strumenti

Gli adapter `responses` e `chat-completions` condividono un contratto di turni,
chiamate agli strumenti, immagini e delta testuali. La proprietà `vision` indica che
l'utente ha qualificato il modello per gli input immagine; non verifica automaticamente
le sue capacità. Un server che accetta la stessa forma HTTP può comunque avere limiti
differenti per strumenti, streaming e ragionamento.

Gli strumenti incorporati sono descritti in [schemas/tools.json](../../../schemas/tools.json).
La stessa definizione alimenta l'API e il catalogo. I server MCP esterni sono avviati o
collegati soltanto dopo configurazione e richiesta esplicita di refresh.

## Trasporti e isolamento

Il desktop non espone Node.js alle pagine navigate. Il renderer usa un broker con
operazioni ammesse; il canale di controllo del browser rimane distinto dal contenuto
non attendibile. Il server ascolta su un indirizzo IP di loopback esplicito.
Gli host remoti hanno directory dati, credenziali, processi e browser indipendenti.

## Componenti non collegati al percorso completo

`internal/desktopview` contiene il trasporto RFB/WebSocket, ma questa distribuzione non
lo presenta come viewer desktop completo integrato. `internal/updates` verifica manifest
firmati, ma non è configurato un canale di aggiornamento del desktop. `internal/quota`
non equivale a quote aggregate applicate a ogni percorso di scrittura.

CEF non è presente. La cross-compilazione del core non dimostra la disponibilità del
controllo nativo o degli installer su un sistema operativo. Questi confini sono riportati
anche in [capabilities.json](../../capabilities.json).
