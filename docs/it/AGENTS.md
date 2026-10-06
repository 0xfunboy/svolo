# Regole di lavoro

La fonte di verità è il codice corrente, non un resoconto precedente. Leggere il README
e i contratti del modulo prima di modificarlo. Conservare la licenza e gli avvisi applicabili.

Per ogni modifica identificare produttori e consumatori del contratto. Non creare alias
di migrazione impliciti, percorsi di update non configurati o credenziali di esempio
valide. Eseguire i test dell'area modificata e distinguere test unitari, simulazioni,
prove native e build dell'applicazione.

Aggiornare cataloghi, immagini e documentazione insieme al codice. I mockup di prodotto
non sono screenshot runtime. Non dichiarare un target compilato se si è verificata solo
la sintassi, e non promuovere una release senza i requisiti in `docs/RELEASE.md`.
