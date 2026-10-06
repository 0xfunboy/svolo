# Configurazione

## Due livelli espliciti

Il core conserva `config.json` nella propria directory dati. Il desktop ha anche
preferenze di presentazione e impostazioni del runtime esterno. Configurare un modello
nel core non cambia silenziosamente il modello predefinito del runtime pi.

La configurazione del core ha cinque campi: `version` (1), `providers`, `mcpServers`,
`hosts`, `sessions`. Il server valida l'intero oggetto prima di salvarlo. Una sessione
appartiene al suo host; un percorso Windows non viene reinterpretato come percorso Linux.

## Provider

| Campo | Significato |
| --- | --- |
| `id` | Identificativo locale univoco. |
| `kind` | `responses` oppure `chat-completions`. |
| `baseURL` | Endpoint di base, senza query, credenziali nell'URL o frammento. |
| `model` | ID effettivamente disponibile sul servizio scelto. |
| `apiKeyEnv` / `apiKeyRef` | Nome di variabile oppure riferimento del vault; non entrambi. |
| `vision` / `stream` | Funzioni da abilitare solo dopo una prova sul modello. |
| `maxOutputTokens` | Budget da 128 a 65536, ulteriormente limitabile dal provider. |
| `reasoningEffort` | Parametro facoltativo, dipendente dalle capacità del provider. |
| `allowInsecureHTTP` | Eccezione esplicita per HTTP fuori dal loopback. |

Il software non sceglie credenziali o modelli al posto dell'utente. Un servizio locale
senza autenticazione non richiede una chiave fittizia. Fuori dal loopback usare HTTPS;
abilitare un'eccezione HTTP rende i dati visibili al trasporto sottostante.

## MCP esterno

Ogni voce usa `command` con `args`, oppure `url`, mai entrambi. `enabled` abilita il
server nella configurazione, ma l'avvio avviene con il refresh MCP. `envKeys` elenca le
variabili da passare a un processo stdio. `tokenEnv` o `tokenRef` risolvono una credenziale
HTTP. I server MCP sono codice o servizi attendibili scelti dall'utente, non estensioni
sandboxed solo perché parlano MCP.

## Sessioni

Una sessione ha `id`, `name`, `workspace` e `allowExec`. Il workspace opzionale deve
essere assoluto e appartenere all'host. `allowExec: false` è la scelta di partenza:
attivarlo permette richieste di esecuzione soggette alla politica del core, non crea
un isolamento del sistema operativo.

## Vault

Per un host senza desktop impostare `SVOLO_VAULT_KEY` a una chiave esterna di 32 byte
codificata in 64 caratteri esadecimali. Non scriverla nel repository, in `config.json`
o nel workspace dell'agente. Conservare un backup separato della chiave: perderla rende
irrecuperabili i segreti cifrati. Il vault non cifra profili browser, cookie, conversazioni
e ogni file del progetto.

Il desktop può custodire la chiave con i meccanismi del sistema operativo quando
presenti. Il comportamento deve essere verificato sull'host effettivo; una chiave non
viene considerata protetta soltanto perché è in una cartella nascosta.

## Esempi e validazione

[Configurazione iniziale](../../../examples/config.example.json),
[provider](../../../examples/provider.example.json), [host](../../../examples/remote-host.example.json)
e [routine](../../../examples/inspect-and-confirm.routine.json).

Gli esempi sono dati di progetto appena definiti; le fixture in `testdata` sono invece
input di test e non devono essere copiate nella directory dati dell'applicazione.
