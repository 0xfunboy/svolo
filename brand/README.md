# Svolo Identity

**You set the direction. Svolo gets the work done.**

The symbol combines two wings oriented in the same direction and a separate dot:
delegated action and human control in the same environment. The product presents itself as a
workspace with a browser and agents, not as a mascot or a specific model.

## Assets

`mark.svg` is the vector reconstruction of the approved symbol. `wordmark.svg` adds
the name using a system font; no font files are distributed. `icons`
contains full-size PNGs, multi-resolution ICO, ICNS, light/dark versions, and favicons.
The symbol remains textless in small sizes.

`tokens.json` defines graphite, green, teal, mist, and white. For text on light
surfaces, use teal, not low-contrast bright green. Do not alter proportions, the dot,
or the distance between the wings. Avoid shadows in favicons and do not use the entire board as an icon.

## Presentation Images

The four PNGs in `sources` are the approved artworks. The WebP versions are used for the
documentation; **they are product concepts, not screenshots of the running application**.
The application backgrounds are crops without text and tonal variations of the approved landscape.
They are not photographs attributed to a real location.

## Regeneration

```bash
python3 -m pip install -r scripts/requirements-brand.txt
python3 scripts/build-brand.py
```

Execute the commands from the root. `assets.json` registers files, sizes, and SHA-256.
Copies for desktop and console are updated in the same step.

Providers are identified in the GUI by text badges in the Svolo theme, without
embedded external brand SVGs. The provider name remains a description
of the integration, not a claim of sponsorship.
