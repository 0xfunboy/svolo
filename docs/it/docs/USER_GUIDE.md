# Lavorare con Svolo

## Navigazione e controllo

Il desktop offre un browser interattivo e una vista dell'attività. La console del core
mostra il browser posseduto dalla sessione mediante immagini della pagina. La vista
remota non apre una seconda pagina allo stesso URL, ma non include tutte le superfici
native di un desktop remoto.

Usare **Take control / Stop** prima di intervenire. Gli input nel viewer prendono il
controllo umano; **Return to agent** permette una nuova osservazione e la ripresa.
Il controllo automatico di tutti gli input nella finestra browser nativa richiede ancora
qualifica completa. Fermare l'agente non annulla un invio già eseguito su un sito.

Nel workspace web, selezionare un'altra scheda cambia solo la vista. Anche il
cambio di risoluzione mantiene l'agente in esecuzione e la sua scheda operativa.
Il selettore si disabilita durante il ridimensionamento: attendere la nuova immagine
prima di cliccare sulla pagina. **In attesa del modello** indica che il browser
attende la risposta del provider AI selezionato. Una cattura in ritardo mantiene
l'ultima immagine valida e riprova automaticamente; non significa che il browser
sia stato chiuso.

## Attività dell'agente

Selezionare host, sessione e provider, scrivere un risultato desiderato e mantenere
inizialmente **Ask before changes**. Un'immagine allegata viene trasmessa al modello
selezionato: non caricare informazioni che quel servizio non può ricevere. Non includere
nel prompt chiavi API o l'admin token del core.

Gli esiti delle chiamate e le richieste di approvazione sono separati dalla risposta
testuale. Verificare il risultato di operazioni importanti sulla pagina o nell'artefatto,
non soltanto attraverso la frase conclusiva del modello.

## Progetti e workflow

Kanban organizza schede e attività. Le segnalazioni raccolgono problemi, ripetizioni e
risoluzioni. ATP rappresenta dipendenze e stato dei nodi; avviare un piano autorizza
l'esecuzione nel workspace selezionato, non una pubblicazione remota indiscriminata.
I worker ricevono un nodo e i report delle dipendenze. I nodi interrotti non vengono
considerati completati.

Git e GitHub usano il repository e l'account selezionati. Conservare separati i cambiamenti
personali; un worktree dedicato riduce le interferenze ma non sostituisce una revisione.

## File e artefatti

I trasferimenti sono legati a host e sessione. Il percorso di destinazione è relativo
al workspace. Un trasferimento riprendibile ha un ID e un offset; il checksum finale
verifica i byte, non il contenuto semantico del documento. Non sovrascrivere un file
esistente senza indicarne esplicitamente la versione richiesta dall'API.

Gli screenshot salvati, i download registrati e le registrazioni compaiono come artefatti.
Una registrazione MP4 richiede ffmpeg. Non registrare schermate sensibili senza necessità.

## Controllo delle applicazioni

Computer Use è disattivato inizialmente. Ogni applicazione richiede un'autorizzazione
separata; l'input in primo piano ha un'ulteriore abilitazione. Disponibilità e semantica
dipendono dal sistema operativo, dai permessi e dal server grafico. Verificare le
capabilities restituite dal backend prima di affidare un'attività a questa funzione.

## Aspetto

I temi light/dark/system e la rotazione degli sfondi restano selezionabili. Tutti gli
sfondi distribuiti sono variazioni del paesaggio Svolo approvato, senza immagini di gesti o mascotte. L'opzione **None** disattiva il fondale.
