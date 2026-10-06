# Documenti e profili di compito riutilizzabili

Il client web Linux legge JPG/JPEG, PNG, PDF, TXT e CSV UTF-8. I profili conservano
procedure revisionate per singolo account, anche cambiando modello o riavviando
l'applicazione. È memoria operativa, non addestramento del modello. Questo flusso
non è ancora esposto nel client desktop.

## Primo contratto TIM

1. In **Impostazioni → Profili di compito**, crea il profilo iniziale per i contratti
   TIM. Per altre operazioni ripetitive crea un profilo personalizzato.
2. Modifica la procedura e aggiungi l'origine HTTPS esatta del portale autorizzato,
   per esempio `https://dealer.example`, senza percorso, parametri o credenziali.
   Aggiungi gli altri siti necessari al flusso osservato. Con l'elenco vuoto l'agente
   può leggere i documenti ma non compiere azioni nel browser per quel compito.
3. Seleziona il profilo sopra il messaggio della chat. Accedi personalmente al
   portale nel browser condiviso e gestisci OTP o CAPTCHA.
4. Allega preventivo, contratto firmato e documenti del cliente. Spunta soltanto i
   file della pratica corrente. Il tasto con l'occhio mostra il testo estratto;
   verifica i dati importanti sull'originale, scaricabile dalla stessa finestra.
5. Durante il primo caso verifica con l'assistente etichette, tipi di documento,
   codici offerta, validazioni e ricevuta del portale. Le istruzioni richiedono
   dati forniti, nessun consenso o firma inventati e approvazione prima dell'invio
   definitivo. Gli upload richiedono sempre approvazione esplicita nel core, con
   destinazione indicata, anche in autonomia browser.
6. Chiedi di ricordare i passi verificati oppure premi il tasto di apprendimento
   nella testata della chat. Rivedi la proposta, elimina informazioni personali,
   modifica la procedura se necessario e salvala esplicitamente.

Per il cliente successivo apri una nuova chat, seleziona lo stesso profilo e carica
i nuovi documenti. Il cambio di profilo, versione o selezione degli allegati crea
un nuovo contesto del modello al prossimo avvio, conservando la cronologia visibile.
Una nuova serie di upload sostituisce la selezione precedente; dopo un ricaricamento
gli allegati devono essere nuovamente selezionati.

## Memoria e revisioni

Conserva percorsi dei menu, corrispondenze fra campi, tipi di allegato, regole di
validazione ed eccezioni verificate. L'agente propone: non può approvare da solo.
Ogni salvataggio revisionato crea una versione; sono disponibili le ultime 20.
Ripristinare una versione richiede revisione e crea una nuova versione. Modifiche
concorrenti o proposte basate su versioni superate vengono respinte.

Non inserire nella procedura nomi, indirizzi, documenti, valori di consenso,
password, cookie o token. I controlli riconoscono diversi codici fiscali, IBAN,
email, telefoni e credenziali, inclusi URL con segreti. Non possono riconoscere
ogni nome o indirizzo: la revisione umana è obbligatoria.

## Lettura e limiti

- 20 MiB per file, massimo 8 file selezionati per esecuzione.
- 100 originali e 200 MiB per account; gli originali scadono dopo 7 giorni.
- Prime 20 pagine dei PDF; le letture incomplete sono indicate nell'interfaccia.
- Estrazione fino a 200.000 caratteri per file; estratti limitati nel prompt e
  lettura completa a blocchi tramite lo strumento privato dei documenti.
- OCR locale italiano/inglese per immagini e PDF scansionati o misti; immagini
  entro limiti specifici ai modelli con visione. Verifica sempre numeri e importi.
- TXT/CSV strettamente UTF-8; contenuti e formule trattati come dati, non eseguiti.

## Protezioni e conservazione

Procedure, versioni, proposte, originali e testo estratto sono campi cifrati
AES-256-GCM, vincolati all'utente nel database dell'app. Soltanto i file spuntati
sono disponibili al modello scelto: il fornitore riceve testo e immagini supportate
e applica le proprie regole di conservazione.

I decoder operano in un namespace Bubblewrap separato senza rete né accesso alla
home, con limiti di risorse. I temporanei privati vengono rimossi dopo la lettura.
Le copie per gli upload sono nel workspace privato, limitate ai file selezionati e
ai siti autorizzati; vengono eliminate dopo l'esecuzione tramite polling o controllo
periodico. Il sito viene ricontrollato anche dopo l'approvazione. I profili usano
strumenti browser e documenti/memoria privati, escludendo script arbitrari,
strumenti del workspace e MCP esterni. Il servizio MCP interno richiede una
credenziale broker su loopback, non il normale cookie del sito.

Il database SQLite non è interamente cifrato: cronologia del core, profili browser
e normali file del workspace restano dati privati su filesystem. Eliminare o far
scadere un allegato non cancella il testo già nella conversazione o presso il
fornitore. Eliminare la chat rimuove cronologia e relativi allegati locali. Proteggi
i backup completi e conserva separatamente la chiave principale.

Il profilo TIM iniziale deve imparare il vostro portale con il primo caso assistito:
non costituisce un'integrazione già verificata. I requisiti di qualificazione della
release restano invariati. Vedi [sicurezza web](WEB_SECURITY.md),
[API aggiornate in inglese](../../WEB_API.md) e
[guida completa in inglese](../../TASK_PROFILES.md).
