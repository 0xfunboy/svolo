# Distribuzione web su svolo.eeess.cyou

Questa installazione usa un gateway Node.js dedicato e un tunnel Cloudflare dedicato.
L'applicazione mantiene lo stato `0.3.1-dev`: il deploy non cambia la qualificazione
del prodotto definita in [RELEASE.md](RELEASE.md).

## Percorso del traffico

```text
https://svolo.eeess.cyou
    → tunnel Cloudflare "svolo"
    → http://127.0.0.1:7340
    → /home/your-user/svolo/web/server.mjs
```

Il tunnel ha ID `<tunnel-id>`. Il record DNS è stato
creato con il certificato Cloudflare già presente sul server. La configurazione
locale ammette questo hostname e restituisce 404 per gli altri hostname. Il core
locale sulla porta 7331 non è una destinazione di questo tunnel.

## File installati e contratto del processo

| Elemento | Percorso o valore |
| --- | --- |
| Gateway | `/home/your-user/svolo/web/server.mjs` |
| Node.js | `/home/your-user/.nvm/versions/node/v24.18.0/bin/node` |
| Directory di lavoro | `/home/your-user/svolo` |
| Stato privato | `/home/your-user/.local/share/svolo/web` (permessi 700) |
| Chiave del gateway | `/home/your-user/.config/svolo/web-master.key` (permessi 600; gestita dal gateway) |
| Unità applicazione | `/home/your-user/.config/systemd/user/svolo-web.service` |
| Unità tunnel | `/home/your-user/.config/systemd/user/svolo-cloudflared.service` |
| Configurazione tunnel | `/home/your-user/.cloudflared/config-svolo.yml` (permessi 600) |
| Credenziale tunnel | `/home/your-user/.cloudflared/<tunnel-id>.json` (permessi 600) |
| Certificato di gestione Cloudflare | `/home/your-user/.cloudflared/cert-eeess.pem` |
| Metriche tunnel | `http://127.0.0.1:7341/metrics` |

La unit applicazione fornisce queste variabili:

```text
NODE_ENV=production
SVOLO_WEB_HOST=127.0.0.1
SVOLO_WEB_PORT=7340
SVOLO_WEB_DATA=/home/your-user/.local/share/svolo/web
SVOLO_PUBLIC_ORIGIN=https://svolo.eeess.cyou
SVOLO_WEB_KEY_FILE=/home/your-user/.config/svolo/web-master.key
SVOLO_CORE_BIN=/home/your-user/svolo/build/svolo-core-web
SVOLO_CHROMIUM=/home/your-user/.local/bin/chromium
SVOLO_PI_BIN=/home/your-user/.local/bin/pi
```

La directory dati contiene lo stato persistente del gateway. Il servizio dispone
di accesso in scrittura a quella directory e al repository per le operazioni
dell'applicazione. Usa `UMask=0077`, `NoNewPrivileges=true`, directory temporanea
privata, filesystem di sistema in sola lettura e home in sola lettura, con le
eccezioni di scrittura indicate. Il gateway applica separatamente l'isolamento
del filesystem e dei processi dei core degli utenti tramite `bwrap`; la rete host
rimane condivisa e il browser usa il proxy egress che rifiuta gli indirizzi privati.
Il gateway limita inoltre le API e gli strumenti esposti agli utenti del servizio.

Il servizio applicazione imposta `MemoryHigh=2G`, `MemoryMax=4G`, `CPUQuota=200%`
e `TasksMax=1024` per l'intero gruppo di processi, inclusi i browser e i core figli.
Questi limiti sono aggregati per l'applicazione; non costituiscono quote individuali
per utente. L'avvio dei core usa `/usr/bin/prlimit` per limitare i descrittori aperti
a 4096. I limiti di memoria virtuale e del numero di processi per UID non vengono
usati per evitare interferenze con V8 e con gli altri servizi dello stesso utente.

Il gateway usa l'SDK `@earendil-works/pi-coding-agent` versione `1.0.3`, fissato in
`web/package.json` e `web/pnpm-lock.yaml` e installato nei `web/node_modules`
dell'applicazione. Per installare le dipendenze web, usare Node.js 24 o superiore
e pnpm 10.34.3:

```bash
cd /home/your-user/svolo/web
corepack pnpm install --frozen-lockfile --ignore-scripts
```

L'installazione con lockfile congelato e senza script delle dipendenze è stata
verificata; l'importazione locale dell'SDK e i 63 test web (54 unitari/simulati e 9 verifiche browser incluse le sottoprove) sono passati. Il gateway
carica questa copia dell'SDK senza dipendere dalla precedente copia nella toolchain
esterna. La CLI `pi`, Chromium e il core web restano selezionati con i percorsi
espliciti indicati nella unit; il loro funzionamento nel servizio è stato verificato.

## Avvio e persistenza

I due servizi usano `Restart=on-failure` con attesa di cinque secondi. Il tunnel
richiede l'avvio del gateway tramite `Wants` e viene ordinato dopo di esso.
`Linger=yes` è già attivo per l'utente `<os-user>`: il gestore systemd utente rimane
disponibile senza sessione aperta ed è avviato dopo un riavvio del sistema.

Prima del primo avvio, verificare applicazione, autenticazione e isolamento
localmente. Abilitare e avviare i servizi soltanto dopo questa verifica:

```bash
systemctl --user daemon-reload
systemctl --user enable --now svolo-web.service
# Verificare il gateway locale prima di avviare il tunnel.
systemctl --user enable --now svolo-cloudflared.service
```

La configurazione delle unit non avvia processi da sola. Alla verifica del
5 ottobre 2026, `svolo-web.service` e `svolo-cloudflared.service` sono entrambi
abilitati e attivi; il dominio pubblico raggiunge il gateway sulla porta 7340.
Il tunnel ha quattro connessioni attive e `Linger=yes` è confermato. Lo stato
effettivo si controlla con:

```bash
systemctl --user is-enabled svolo-web.service svolo-cloudflared.service
systemctl --user is-active svolo-web.service svolo-cloudflared.service
loginctl show-user your-user -p Linger
systemctl --user status svolo-web.service svolo-cloudflared.service
journalctl --user -u svolo-web.service -u svolo-cloudflared.service --since today
```

La prova di arresto forzato del solo gateway con `SIGKILL` ha verificato il riavvio
automatico in 7129 ms, con sessioni, provider, risultati degli agenti e browser
ancora utilizzabili. È stato verificato anche il riavvio dell'applicazione; l'intero
server non è stato riavviato. Il report è in
`/home/your-user/svolo-installation/web-crash-recovery.json`.

Per arrestare il servizio pubblico:

```bash
systemctl --user stop svolo-cloudflared.service
systemctl --user stop svolo-web.service
```

Per impedire anche l'avvio al prossimo riavvio, usare `disable --now` al posto di
`stop`. I servizi `air3`, `goonerbot` e il servizio locale del core sulla porta 7331
rimangono separati da questa configurazione.

## Verifiche dell'infrastruttura

```bash
systemd-analyze --user verify ~/.config/systemd/user/svolo-web.service ~/.config/systemd/user/svolo-cloudflared.service
cloudflared tunnel --config ~/.cloudflared/config-svolo.yml ingress validate
cloudflared tunnel --config ~/.cloudflared/config-svolo.yml ingress rule https://svolo.eeess.cyou
node --test web/security.test.mjs web/providers.test.mjs
```

La suite web finale comprende 53 test superati: 39 di sicurezza e 14 dei provider.
I test di sicurezza usano database e credenziali temporanei e provider simulati.
Verificano cookie, scadenza delle sessioni, CSRF, isolamento dei dati tra utenti,
restrizioni delle API, cifratura autenticata e destinazioni del traffico egress.
Le prove HTTP locali usano un core simulato e non sostituiscono la verifica dei
core reali e di Chromium nel servizio systemd, né la verifica HTTPS pubblica.

La verifica pubblica del 5 ottobre 2026 ha ottenuto HTTP 200 per la pagina iniziale
e `/api/health`, HTTP 401 per le API senza sessione e HTTP 403 per origini o
hostname contraffatti. La catena TLS e il nome `svolo.eeess.cyou` sono validi;
il certificato osservato è emesso da Google Trust Services, copre `*.eeess.cyou`
e scade il 13 novembre 2026. Le prove pubbliche non hanno eseguito login né
pubblicato password. I dettagli sono conservati localmente in
`/home/your-user/svolo-installation/public-http-checks.json` e
`/home/your-user/svolo-installation/public-tls-certificate.txt`.

Sono state eseguite anche 19 verifiche con due utenti sul gateway systemd reale e
core `bwrap` distinti: provider e sessioni iniziali del nuovo account vuoti,
workspace confinati, contenuti separati nello stesso percorso, rifiuto delle
sessioni dell'altro utente e delle richieste di esecuzione. Chromium ha mostrato
la pagina di rifiuto del proxy provando ad accedere al gateway su loopback.
L'account temporaneo, le sue sessioni e la directory dedicata sono stati rimossi
dopo l'arresto del relativo supervisore; i file di prova del workspace amministratore
sono stati rimossi. La prova `flock` sul file originale `.owner.lock` ha confermato
il lock durante l'esecuzione del core e il rilascio dopo l'arresto, prima della
rimozione della directory temporanea. Queste prove non hanno chiamato modelli. Il report è in
`/home/your-user/svolo-installation/native-isolation-results.json`.

Cloudflare documenta il [tunnel gestito localmente](https://developers.cloudflare.com/tunnel/features/locally-managed-tunnels/create-local-tunnel/)
e la [validazione delle regole ingress](https://developers.cloudflare.com/tunnel/features/locally-managed-tunnels/configuration-file/).
Questo deploy usa unit systemd dell'utente, con credenziali fuori dal repository.
Conservare chiave del gateway, credenziali del tunnel e certificato Cloudflare nei
rispettivi percorsi privati; i loro contenuti non appartengono alla distribuzione
sorgente o ai log di verifica.
