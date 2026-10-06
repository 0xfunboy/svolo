# Sicurezza del servizio web Linux

Questo documento descrive le protezioni e i limiti realmente implementati
nel gateway `web`, nel pool dei core e nel deploy Linux. Il prodotto rimane
una revisione di sviluppo con `productionQualified:false`. La verifica
del servizio web non qualifica i pacchetti desktop Windows/macOS/Linux
né sostituisce i requisiti di [RELEASE.md](RELEASE.md).

## Confine dei servizi

Il gateway ascolta esclusivamente sul loopback, normalmente porta 7340.
HTTPS pubblico termina nel percorso proxy/tunnel documentato in
[WEB_DEPLOYMENT.md](WEB_DEPLOYMENT.md). Il core locale indipendente sulla
porta 7331 e il desktop non sono destinazioni di questo gateway.
Il gateway ammette Host e Origin configurati e una lista esplicita di API
core; non espone l’admin token core, il vault, il daemon stop o la gestione
SSH al browser. L’API pubblica è descritta in [WEB_API.md](WEB_API.md).

Il traffico di pagina, le risposte dei modelli e i risultati MCP rimangono
input non attendibili. Il testo non può concedere un’approvazione. Il
frontend effettua escaping dei valori dinamici e mostra gli argomenti
delle richieste di consenso; la CSP esclude script esterni, iframe, oggetti
e inserimento del sito in frame. Queste misure non garantiscono immunità
alla prompt injection o alle vulnerabilità del browser.

## Identità e autorizzazioni

Le password sono hash scrypt con salt casuale, parametri N=32768, r=8, p=1
e confronto costante. Il cookie di sessione contiene 256 bit casuali;
SQLite conserva il suo SHA-256. Cookie `HttpOnly`, `Secure`, `SameSite=Lax`,
prefisso `__Host-`, durata 12 ore e CSRF distinto proteggono la sessione web.
Le mutazioni autenticate richiedono `X-CSRF-Token`; il login precede questa
sessione ed è protetto da controlli Origin/Host e limiti di tentativi.

Il login limita tentativi per IP e nome utente a 20 in 15 minuti, con quattro
verifiche password concorrenti e massimo dieci sessioni conservate per
account. I contatori dei tentativi sono in memoria e si azzerano al riavvio.
Il logout cancella la sessione dal DB. Un utente disabilitato non viene
autenticato; la UI/API corrente permette agli amministratori di creare
accessi, ma non presenta endpoint pubblici di reset password o disabilitazione.

Ogni richiesta deriva l’utente dalla sessione, non da un ID fornito nel
corpo. Provider, preferenze, MCP e prompt hanno chiavi per utente. Gli
amministratori creano account; la loro lista provider non diventa quella
degli altri utenti. Le credenziali della prima importazione appartengono
soltanto all’amministratore selezionato.

## Cifratura e dati sensibili

SQLite non è un database interamente cifrato con SQLCipher. Le credenziali
provider/MCP, le chiavi dei vault core e i prompt registrati dal gateway
sono cifrati nei rispettivi campi con AES-256-GCM, nonce casuale e tag di
autenticazione. Provider, MCP, chiavi core e prompt associano il contesto crittografico
all’ID dell’utente. La master key è un file di 32 byte rappresentati da
64 caratteri esadecimali, esterno alla directory dati, con permessi 0600.
Il DB e le directory private hanno permessi restrittivi; il gateway avvia
il processo con umask 0077.

Nomi utente, hash password, metadati provider, configurazioni, preferenze,
identificativi e audit non sono cifrati integralmente. Inoltre il core
conserva conversazioni, eventi e risultati nelle proprie directory dati;
questo storico può includere prompt e testo in chiaro anche quando il
campo corrispondente del gateway è cifrato. Profili Chromium, cookie,
download e file workspace sono sensibili. Proteggere quindi l’intera
directory dati e i backup, non soltanto i campi credenziali.

Il browser non riceve le API key né access/refresh token salvati. Il gateway
invia al core soltanto un segreto broker interno; il provider effettivo è
chiamato dal servizio web. Il refresh OAuth viene serializzato per utente
e provider nel processo gateway, e salva i token ruotati nel DB cifrato.
Questa serializzazione non è un lock distribuito: non avviare più gateway
concorrenti sullo stesso DB senza un coordinamento ulteriore.

L’importazione iniziale è esplicita e una tantum, controllata dal marker
`initial-import` nel DB. Legge soltanto i campi autorizzati del file
Gemrouter e la credenziale Codex indicata dall’amministratore. Dopo
l’importazione il servizio usa la propria copia cifrata e non dipende
dalle directory OAuth esterne per le richieste successive. Le directory
temporanee usate per il controllo quote Codex vengono create privatamente,
poi rimosse; le eventuali rotazioni sono riportate nel DB. Copiare un
file IDE Antigravity non lo trasforma in credenziale OAuth supportata.

## Isolamento dei core

Ogni utente riceve un core distinto e directory `users/<id>/core`,
`workspace` e `home`. Il launcher usa Bubblewrap (`bwrap`) con namespace
utente, PID, IPC e UTS, rimozione delle capability, `/usr` e componenti
applicativi montati in sola lettura, `/tmp` privato e directory scrivibili
limitate ai dati/home/workspace di quell’utente. La home del sistema, le
credenziali OAuth originali e le directory degli altri utenti non sono
montate nel namespace. I processi terminano con il namespace proprietario.

Chromium usa la propria sandbox; il deploy web non abilita `--no-sandbox`.
Servono Linux con namespace utente utilizzabili, Bubblewrap, `flock`,
`prlimit`, Node.js con `node:sqlite`, un core Go compilato e Chromium
compatibile. L’implementazione web corrente è Linux: non estrapolare
queste garanzie al desktop o a servizi Windows/macOS.

La rete host è **condivisa**: non viene creato un namespace rete separato.
Chromium è configurato per il proxy egress del gateway, senza bypass
loopback, con QUIC disabilitato. Il proxy valida DNS, connette all’indirizzo
numerico risolto e rifiuta destinazioni private/loopback/link-local e
porte non ammesse. HTTP è limitato alle porte 80/8080, CONNECT a 443/8443.
Questa è una policy del traffico instradato dal proxy, non un firewall
kernel che costringe ogni processo a usarlo.

Gli endpoint provider ordinari passano dalla validazione HTTPS e dal fetch
che fissa l’indirizzo DNS per la singola connessione. L’eccezione privata
del Gemrouter importato è una decisione esplicita dell’amministratore.
MCP nel gateway accetta solo HTTPS pubblico e rifiuta i processi locali.
L’egress del browser e la configurazione dei client esterni hanno confini
distinti: non dedurre dal proxy Chromium una garanzia universale per ogni
connessione di qualunque adapter o SDK esterno.

Il servizio web nega esecuzione workspace, Computer Use, importazione
profili, routine e i tool pi amministrativi sia nelle chiamate manuali
sia nel catalogo ammesso al runner. Le sessioni web hanno `allowExec:false`
e un workspace fissato nel namespace. Queste restrizioni non eliminano
i rischi di una vulnerabilità del core/browser o di un servizio MCP
autorizzato che esegue operazioni nel proprio ambiente.

## Proprietà, arresto e recupero

Il launcher acquisisce un `flock` esclusivo non bloccante su
`core/.web-owner.lock`, mantenuto per l’intera vita del namespace PID.
Solo dopo questo lock `core-launch.sh` rimuove le tracce singleton dei
profili del core web (`SingletonLock`, `SingletonSocket`, `SingletonCookie`,
`DevToolsActivePort`). Il precedente namespace deve essere terminato
prima che il nuovo proprietario acquisisca il lock. La recovery è limitata
alle directory private web; non modifica la gestione conservativa dei
profili standalone/desktop. Non rimuovere manualmente lock di profili
mentre il proprietario è ancora attivo.

L’arresto ordinato chiede prima al core di chiudere CDP/Chromium e scaricare
lo store; dopo il budget di arresto il pool può terminare il gruppo processi.
Il riavvio del core marca i run precedentemente in corso come interrotti,
senza ripetere automaticamente azioni o approvazioni. Un’azione già inviata
a un sito esterno può avere avuto effetto prima dell’interruzione: verificarne
l’esito prima di riprendere il lavoro.

Il pool ammette otto core e può arrestare un core non richiesto da 15 minuti
quando non trova run con stato `running`. Gli endpoint impongono limiti
per utenti, sessioni, provider, MCP e richieste; non sono quote complete
di consumo CPU, memoria o costo provider per utente. Il deploy systemd
aggiunge limiti aggregati per l’intero gruppo applicazione, descritti in
[WEB_DEPLOYMENT.md](WEB_DEPLOYMENT.md); il launcher limita i descrittori
aperti a 4096. Non dichiarare equità o isolamento delle risorse individuali
sulla base di questi limiti aggregati.

## Backup coerente

La directory dati e la master key formano un insieme ripristinabile,
ma vanno conservate in destinazioni separate. Per il deploy corrente
la directory è `/home/your-user/.local/share/svolo/web` e la key è
`/home/your-user/.config/svolo/web-master.key`; sono configurabili.
Non creare backup nella directory workspace accessibile all’agente.

1. Registra versione del codice, binario core, runtime e percorsi della
   configurazione, senza stampare chiavi, cookie o dump dell’ambiente.
2. Arresta prima il tunnel e poi il gateway con le unit documentate.
   Attendi l’arresto dei core e dei browser del servizio; verifica che
   nessun proprietario web mantenga i lock.
3. Copia l’intera directory dati conservando struttura, proprietà e permessi,
   includendo SQLite, gli eventuali file WAL/SHM, dati dei core, profili,
   workspace e home degli utenti. Copiare soltanto `svolo.sqlite` da un
   processo attivo può perdere transazioni presenti nel WAL.
4. Proteggi il backup completo con cifratura e controllo degli accessi.
   La cifratura dei singoli campi nel DB non protegge i profili o lo storico
   in chiaro. Conserva separatamente una copia verificata della master key
   con permessi 0600, accessibile soltanto al responsabile del ripristino.
5. Conserva configurazioni del servizio e credenziali del tunnel in un
   archivio privato distinto. Non inserirle nella distribuzione sorgente.
   Riavvia gateway e tunnel dopo aver verificato la consistenza della copia.

Non è implementato un comando di backup online coordinato fra SQLite e
gli store dei core. Per questo il procedimento descritto usa un arresto
controllato. Una copia del solo DB senza la master key non recupera
credenziali/prompt cifrati né le chiavi dei vault core; perdere la master
key può rendere questi dati irrecuperabili.

## Ripristino

Ripristina prima in un ambiente Linux privato e compatibile, con tunnel
arrestato. Conserva una copia dello stato attuale prima di sostituirlo.
Ripristina la directory dati e la master key corrispondente, mantenendo
ownership dell’utente del servizio, directory private e permessi 0600
per key/DB. Ripristina i binari e i percorsi delle unit compatibili con
quella versione; non tentare una migrazione del DB senza una verifica.

Revoca le sessioni web ripristinate cancellando i record `web_sessions`
dal DB a gateway fermo: altrimenti un cookie ancora valido presente nel
backup potrebbe tornare utilizzabile. La revoca richiede un nuovo login;
non elimina utenti, provider o sessioni di lavoro. I token API/OAuth esterni
possono essere scaduti o ruotati dopo il backup: verificarli e ricollegare
l’account quando necessario, senza sovrascrivere file OAuth esterni.

Avvia soltanto il gateway, controlla health, autenticazione, separazione
dei dati di due utenti, accesso a una sessione, avvio Chromium e quote.
Verifica che nessun run venga ripetuto automaticamente e che la recovery
lock riguardi esclusivamente i profili web restaurati. Esponi nuovamente
il tunnel dopo questi controlli. Il documento descrive una procedura;
non è una dichiarazione di disaster recovery già provata.

## Verifiche e qualificazione

Test unitari/simulazioni del gateway verificano autenticazione, CSRF,
separazione dei record, cifratura e restrizioni delle API; gli SDK/upstream
simulati non certificano provider o MCP reali. Le prove runtime locali
del frontend hanno verificato browser, catture, sessioni e layout, e le
prove del deploy verificano il servizio/tunnel effettivamente installato.
Gli esiti aggiornati appartengono ai report di verifica del deploy.

Codex può esporre finestre quota reali; Gemrouter senza endpoint/header
quote osservati rimane `unavailable`. Antigravity senza credenziale
supportata rimane non collegato. Una prova di risposta testuale non
certifica un ciclo completo di chiamate tool e approvazioni; vanno
verificati separatamente per ciascun adapter/modello. La pubblicazione
di questo servizio e i test descritti non promuovono automaticamente
il prodotto a release qualificata per la produzione.
