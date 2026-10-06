# API locale

Il server ascolta sul loopback. Tutti gli endpoint `/v1/` richiedono `Authorization:
Bearer <token>`. Un token limitato alla sessione può accedere al server MCP ma non ai
controlli amministrativi. Non esporre il daemon direttamente a Internet.

## Convenzioni

Input e output sono JSON salvo stream, trasferimenti e artefatti. Le richieste di
modifica vanno inviate con il metodo previsto; campi sconosciuti sono rifiutati nei
contratti tipizzati. Gli errori sono risposte HTTP non riuscite con un messaggio; non
considerare un timeout una prova che un'azione esterna non sia avvenuta.

## Endpoint principali

| Endpoint | Uso |
| --- | --- |
| `/v1/health` | Versione, protocollo e stato del backend. |
| `/v1/config`, `/v1/sessions` | Configurazione dell'host e sessioni. |
| `/v1/tools`, `/v1/tools/call` | Discovery ed esecuzione esplicita degli strumenti. |
| `/v1/control`, `/v1/runs`, `/v1/runs/stop` | Proprietà del controllo e attività agente. |
| `/v1/approvals`, `/v1/tokens` | Decisioni umane e credenziali MCP limitate. |
| `/v1/events`, `/v1/events/stream`, `/v1/events/cursor` | Eventi e recupero del cursore. |
| `/v1/view`, `/v1/input`, `/v1/browser/tabs` | Vista della pagina, input e tab della sessione. |
| `POST /v1/browser/viewport` | Ridimensiona una scheda propria senza prendere il controllo o cancellare l'agente. |
| `/v1/artifacts`, `/v1/artifact` | Elenco e contenuto degli artefatti. |
| `/v1/hosts/*`, `/v1/remote` | Gestione SSH e richieste al daemon selezionato. |
| `/v1/transfers/*` | Staging, chunk, commit e download verificati. |
| `/v1/vault/*`, `/v1/credentials` | Sblocco/blocco e gestione dei riferimenti ai segreti. |
| `/v1/projects/*`, `/v1/atp/*`, `/v1/git` | Stato di progetto, workflow e Git. |
| `/v1/runtime/*`, `/v1/extensions/browser` | Runtime RPC e adapter del browser desktop. |
| `/v1/computer/*` | Policy e capacità delle applicazioni native. |
| `/v1/mcp`, `/v1/mcp/refresh` | Server MCP e collegamento dei server esterni. |
| `/v1/bridge/*` | Canale privato del desktop; non è uno strumento del modello. |

Per forme delle richieste consultare i gestori in `core/internal/server`. I pattern
`/*` raggruppano endpoint espliciti, non autorizzano URL arbitrari. Non usare i dettagli
del bridge come API pubblica per le estensioni.

`GET /v1/browser/tabs?session=…` restituisce le schede possedute dalla sessione
come `[{id,url,title,type,active}]`. `active` identifica la destinazione logica
selezionata dal core e non dipende dall'ordine dei target Chrome.
`GET /v1/control?session=…` restituisce `{owner,epoch,active,tab?}`: qui `active`
indica un'operazione agente in corso e `tab` la sua destinazione selezionata.
`GET /v1/view?session=…&tab=…` osserva la scheda indicata senza cambiarla come
destinazione, prendere il controllo o accodarsi a un'attesa agente. Gli input e
le navigazioni devono usare l'ID della scheda a cui si riferisce l'azione.

Il browser gestito abilita `CDPScreenshotNewSurface` e acquisisce gli screenshot
con `fromSurface:true`. Questa funzione di [Chromium](https://chromium.googlesource.com/chromium/src/+/refs/heads/main/content/common/features.cc)
richiede una nuova superficie del compositore senza attendere `ForceRedraw`,
che può bloccarsi quando una scheda statica in secondo piano non presenta nuovi
frame. La cattura mantiene la scheda nascosta, senza attivarla. La prova nativa
[`TestChromiumAgedBackgroundObservation`](../../../core/internal/browser/screenshot_chromium_test.go)
lascia B nascosta per sei secondi, poi verifica sei acquisizioni consecutive con
i pixel di B, focus su A e controllo invariato, usando Chromium headless con il
sandbox standard.

I riferimenti `@documento:elemento` sono legati sia all'epoch di controllo sia
alla scheda dello snapshot. Un ID `tab` incompatibile produce
`target_tab_mismatch`; takeover o documento sostituito rendono i riferimenti
obsoleti. Senza selezione valida, una sessione con più schede produce
`tab_ambiguous` invece di scegliere la prima. Lo harness del modello richiede
un `tab` esplicito per gli strumenti che operano su una pagina.

`POST /v1/browser/viewport` accetta solo `{session,tab,width,height,dpr,mobile,touch}`.
La sessione deve essere configurata e `tab` deve indicare esplicitamente una sua
scheda. Larghezza e altezza sono interi da 100 a 8192 pixel CSS; DPR va da 0.1 a 5.
Campi sconosciuti, schede estranee e dimensioni mancanti sono rifiutati. La modifica
si accoda all'azione in corso, con attesa cancellabile e limite di 20 secondi.
Non ferma l'agente, non cambia l'epoch del controllo e non seleziona un'altra scheda.
Gli snapshot della pagina ridimensionata vengono invalidati: servono riferimenti
aggiornati per l'azione successiva. Dopo un timeout, acquisire nuovamente la vista;
non è prova che la modifica non sia avvenuta. Input, navigazione e altri strumenti
manuali richiedono ancora il controllo umano. Il gateway web applica autenticazione,
CSRF e isolamento per utente. Questo endpoint configura la vista e non è uno
strumento del modello o una scorciatoia per acquisire il controllo.

## Chiamata manuale

```json
{"session":"workspace-1","name":"read-page","arguments":{}}
```

La forma effettiva del comando CLI è disponibile con `svolo-core call -h`.
Consultare `/v1/tools` per includere anche i tool MCP collegati, oppure `svolo-core
catalog` per descrivere i soli strumenti incorporati senza aprire dati o processi.

## Catalogo incorporato

La tabella seguente è derivata da [tools.json](../../../schemas/tools.json). `readOnly` è una
classificazione della politica; non è un'affermazione sull'affidabilità dei dati letti.

| Operazione | Sola lettura | Scopo |
| --- | --- | --- |
| `project-board` | no | Project-scoped Kanban, implemented in Go. action=get or apply with a validated board operation. No access to another project. |
| `project-laments` | no | Project-scoped lament reports, fixes and resolution, implemented in Go. action=get or apply; bounded report retention and explicit reopening. |
| `computer-use` | no | Native application control, off by default. Requires a separate per-app human approval. Select list_apps/get_app_state/screenshot/click/drag/scroll/type_text/press_key/set_value/select_text/perform_secondary_action/paste/end. Foreground actions need an explicit setting and an already focused app. |
| `pi-kanban` | no | Kanban operations for a connected desktop runtime session. Requires an existing pi session in Electron; native domain policy remains enforced. |
| `pi-atp` | no | Pause or resume the workflow associated with a connected desktop runtime session. |
| `pi-lament` | no | Issue-report operations for a connected desktop runtime session. Requires an existing pi session in Electron. |
| `pi-computer` | no | Desktop runtime Computer Use adapter. Preserves pi approvals; macOS uses Swift, Windows/Linux use the Go-supervised native provider. Native capabilities and limitations are reported at runtime. |
| `state` | sì | Read active tab URL, title and loading state. |
| `navigate` | no | Navigate the owned tab to http(s). Verify page state afterwards. |
| `open-browser` | no | Open a managed browser tab in this session. |
| `close-browser` | no | Close only this session's managed browser. |
| `open-tab` | no | Create another tab in the same browser session. |
| `tabs` | sì | List only the browser targets owned by this session. |
| `switch-tab` | no | Activate a tab by exact id, title or URL; ambiguous matches fail. |
| `close-tab` | no | Close the current owned tab. |
| `tab-history` | no | Go back or forward in tab history. |
| `open-in-new-tab` | no | Open an element's link or image URL in another tab. |
| `click` | no | Dispatch a click on a visible, unambiguous target. This is not a task-success assertion. |
| `type-text` | no | Type text into the current focus or an explicit target. |
| `fill` | no | Replace the text of a visible editable target. |
| `press-key` | no | Send a key, optionally Ctrl/Alt/Shift/Meta modifiers joined with +. |
| `select` | no | Choose a native select option by exact value or label. |
| `check` | no | Check a checkbox and verify its state. |
| `dialog` | no | Accept or dismiss a JavaScript dialog. |
| `drag` | no | Drag between two explicit page element targets. |
| `wait` | sì | Bounded wait for text, css, visible, url, title, hidden or image-ready. |
| `wait-for` | sì | Wait for an explicit condition for at most 60 seconds. |
| `assert-url` | sì | Verify the current URL; exact match by default. |
| `assert-title` | sì | Verify the document title. |
| `assert-visible` | sì | Verify element visibility and return geometry. |
| `assert-text` | sì | Verify a target's text, exact or contains. |
| `assert-image-ready` | sì | Verify visible image completion and nonzero natural dimensions. |
| `snapshot-interactive` | sì | Read semantic interactive elements with snapshot-scoped refs. Refresh refs after takeover or page changes. |
| `find-interactive` | sì | Search interactive elements by semantic label. |
| `read-page` | sì | Read title, URL, headings and bounded main text. |
| `inspect-inputs` | sì | Inspect visible form controls. Password values are redacted. |
| `inspect-elements` | sì | Inspect visible CSS matches, bounded to 500. |
| `element-info` | sì | Inspect state, attributes, HTML, text and geometry of one target. |
| `scroll` | no | Scroll up/down/top/bottom or to a target. |
| `query-selector` | sì | Inspect CSS matching elements. |
| `get-element` | sì | Read one target's current DOM data. |
| `accessibility-tree` | sì | Read non-ignored accessibility nodes with a hard output bound. |
| `inspect-links` | sì | Read visible links and resolved URLs. |
| `inspect-images` | sì | Read image sources and load state. |
| `upload` | no | Set a file input. File must belong to the session's registered workspace on the browser host. |
| `highlight` | no | Draw a non-interactive box highlight over a target. |
| `clear-highlight` | no | Remove the current owned highlight. |
| `evaluate-js` | no | Evaluate JavaScript in the owned page. Requires explicit approval; never runs in application UI. |
| `inject-js` | no | Run script now; optionally install it for future documents of this tab. |
| `screenshot` | sì | Capture page pixels, optionally save an immutable hashed artifact. |
| `viewport` | no | Set viewport dimensions, device scale, mobile/touch and optional user agent. |
| `inspect-network` | no | Start/stop/show bounded redacted network metadata capture. Does not capture request bodies or secrets. |
| `console` | sì | Show the console events collected after network/console capture was started. |
| `downloads` | sì | Enable managed downloads for this session and list completed files. |
| `wait-download` | sì | Wait for a non-temporary stable download newer than a timestamp, then register a copy as an artifact. |
| `verify-artifact` | sì | Verify the immutable artifact bytes, size and SHA-256. Does not invent semantic proof. |
| `record-browser` | no | Start or stop bounded page recordings (sampled continuous or action steps); ffmpeg is required for MP4 encoding. |
| `profile-import` | no | Copy a CLOSED Chromium profile into a CLOSED managed session. Credentials may not be portable between OS users. |
| `browser-task` | no | Run one browser operation in an isolated owned task session and close it unless persist is set. |
| `call-routine` | no | Run or resume a bounded persisted JSON graph. Human suspension returns a resume id; uncertain actions never replay automatically. |
| `hitl` | no | Request human intervention in the integrated approval UI. |
| `workspace-list` | sì | List files in the explicitly selected workspace. |
| `workspace-read` | sì | Read a regular file confined to the workspace; symlinks are refused. |
| `workspace-write` | no | Atomically write a workspace file after approval. |
| `workspace-exec` | no | Run an explicit program and argument vector inside a trusted workspace. Requires allowExec and approval; not an OS sandbox. |
| `browser-schema` | sì | Discover the argument schema of a named browser operation. |
| `browser-operation` | no | Execute a discovered browser primitive through the same control/policy path. |
