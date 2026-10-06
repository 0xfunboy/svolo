# Esempi di configurazione

`config.example.json` è una configurazione iniziale senza provider, host o segreti.
`provider.example.json` è una singola voce da personalizzare e aggiungere a `providers`:
endpoint e modello sono segnaposto espliciti, non servizi già configurati.

Gli host remoti usano un alias OpenSSH già verificato e il token del loro daemon risolto
tramite ambiente o vault. I file di esempio non contengono credenziali reali.

Per le routine copiare `inspect-and-confirm.routine.json` nella directory `routines`
della cartella dati privata, con nome `inspect-and-confirm.json`. Creare una sessione,
aprire la pagina e invocare `call-routine` con `{"name":"inspect-and-confirm"}`.
Una sospensione umana restituisce un ID di ripresa; riprendere con `{"resume":"ID"}`
nella stessa sessione. Non confondere queste routine con i piani `.atp.json`.

[Configurazione](../docs/CONFIGURATION.md) · [Host remoti](../docs/REMOTE_HOSTS.md)

`workspace-check.atp.json` è un piano dimostrativo in stato DRAFT. Non esegue lavoro
finché non viene attivato esplicitamente. Il relativo contratto di authoring è
[task-plan.schema.json](../../../schemas/task-plan.schema.json); il librarian verifica
anche le dipendenze e le transizioni di stato.
