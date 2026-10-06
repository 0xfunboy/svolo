# Documenti e profili di compito riutilizzabili

Il client web Linux legge JPG/JPEG, PNG, PDF, TXT e CSV UTF-8. I profili conservano
procedure revisionate per singolo account, anche cambiando modello o riavviando
l'applicazione. È memoria operativa, non addestramento del modello. Questo flusso
non è ancora esposto nel client desktop.

## Crea un profilo per la tua attività

Ogni account inizia senza profili. Non esistono attività aziendali predefinite o
profili assegnati automaticamente. Ogni utente può creare fino a 1.000 profili
privati per le proprie esigenze, indipendenti dal modello selezionato.

1. Apri **Impostazioni → Profili attività → Nuovo profilo attività**. Scegli nome,
   obiettivo e istruzioni: regole del flusso, controlli richiesti e risultato atteso.
   Le **Conoscenze verificate** possono iniziare vuote e raccolgono i passaggi
   osservati nelle esecuzioni e approvati dall'utente.
2. Per azioni nel browser aggiungi i siti HTTPS esatti autorizzati, per esempio
   `https://portal.example`, senza percorsi, parametri o credenziali. Lascia vuoto
   per attività sui documenti. Accedi personalmente ai siti e gestisci OTP o CAPTCHA.
   Il login del browser è separato dalla memoria. Rivedi tutti i campi e salva.
3. Seleziona il profilo sopra il messaggio della chat. Creare, duplicare o modificare
   un profilo non lo assegna automaticamente. Ogni esecuzione usa solo obiettivo,
   istruzioni e conoscenze del profilo selezionato.
4. Se necessario allega i documenti del caso corrente, spunta i file da usare e
   verifica il testo estratto e i dati incerti sull'originale. Chiedi all'assistente
   di svolgere l'attività e verifica i risultati osservati insieme a lui.
5. Correggi l'assistente durante il primo caso. Le modifiche esterne sensibili
   richiedono approvazione. Gli upload richiedono sempre approvazione esplicita
   nel core, con destinazione indicata, anche in autonomia browser.
6. Chiedi di ricordare i passi verificati o premi **Apprendi attività** nella chat.
   Rivedi la proposta completa, elimina dati personali e salva. Cambiano soltanto
   le conoscenze di questo profilo: obiettivo, istruzioni, altri profili e altri
   account restano separati.

Per il caso successivo apri una nuova chat e seleziona il profilo appropriato.
Il cambio di profilo, versione o selezione degli allegati crea un nuovo contesto
al prossimo avvio, conservando la cronologia visibile. Una nuova serie di upload
sostituisce la selezione precedente; dopo un ricaricamento riseleziona i file.

Cerca i profili per nome, obiettivo o istruzioni. **Duplica profilo** apre una copia
modificabile da rivedere e salvare, con ID indipendente e revisione 1. L'apprendimento
della copia non modifica l'originale e viceversa.

## Memoria e revisioni

Conserva percorsi dei menu, corrispondenze fra campi, tipi di allegato, regole di
validazione ed eccezioni verificate. L'agente propone: non può approvare da solo.
Ogni salvataggio revisionato crea una versione; sono disponibili le ultime 20.
Ripristinare una versione richiede revisione e crea una nuova versione. Il ripristino
comprende anche obiettivo, istruzioni e siti autorizzati. Obiettivo:
2.000 caratteri; istruzioni: 10.000; conoscenze: 40.000. Le revisioni precedenti
senza i nuovi campi espongono obiettivo e istruzioni vuoti fino alla modifica
dell’utente. Modifiche concorrenti o proposte basate su versioni superate vengono
respinte.

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

Ogni flusso specifico deve essere verificato durante un primo caso assistito.
I requisiti di qualificazione della release restano invariati. Vedi [sicurezza web](WEB_SECURITY.md),
[API aggiornate in inglese](../../WEB_API.md) e
[guida completa in inglese](../../TASK_PROFILES.md).
