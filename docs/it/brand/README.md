# Identità Svolo

**You set the direction. Svolo gets the work done.**

Il simbolo combina due ali orientate nella stessa direzione e un punto separato:
azione delegata e controllo umano nello stesso ambiente. Il prodotto si presenta come
workspace con browser e agenti, non come una mascotte o un modello specifico.

## Asset

`mark.svg` è la ricostruzione vettoriale del simbolo approvato. `wordmark.svg` aggiunge
il nome con un carattere di sistema; nessun file di font viene distribuito. `icons`
contiene PNG a dimensioni reali, ICO multi-risoluzione, ICNS, versioni light/dark e favicon.
Il simbolo rimane senza testo nelle dimensioni piccole.

`tokens.json` definisce graphite, green, teal, mist e white. Per testo su superfici
chiare usare teal, non verde luminoso a basso contrasto. Non alterare proporzioni, punto
o distanza tra le ali. Evitare ombre nei favicon e non usare l'intera tavola come icona.

## Immagini di presentazione

I quattro PNG in `sources` sono gli artwork approvati. Le versioni WebP servono alla
documentazione; **sono concept di prodotto, non catture dell'applicazione eseguita**.
I fondali applicativi sono ritagli senza testi e variazioni tonali del paesaggio approvato.
Non sono fotografie attribuite a un luogo reale.

## Rigenerazione

```bash
python3 -m pip install -r scripts/requirements-brand.txt
python3 scripts/build-brand.py
```

Eseguire i comandi dalla radice. `assets.json` registra file, dimensioni e SHA-256.
Le copie per desktop e console vengono aggiornate nello stesso passaggio.

I provider vengono identificati nella GUI da badge testuali nel tema Svolo, senza
SVG di marchi esterni incorporati. Il nome del provider rimane una descrizione
dell’integrazione, non un’affermazione di sponsorizzazione.
