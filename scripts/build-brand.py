#!/usr/bin/env python3
"""Build the Svolo icon family and ambient assets from the approved brand sources.

No network, fonts, generated UI screenshots, or external executable is required.
Install the packages in requirements-brand.txt to regenerate the committed assets.
"""
from __future__ import annotations
import hashlib
import io
import json
from pathlib import Path
import shutil
import sys
try:
    import cairosvg
    from PIL import Image, ImageEnhance, ImageFilter, ImageOps
except ImportError as exc:
    raise SystemExit("Install scripts/requirements-brand.txt before rebuilding assets.") from exc

ROOT = Path(__file__).resolve().parents[1]
BRAND = ROOT / "brand"
GENERATED = BRAND / "icons"
GENERATED.mkdir(parents=True, exist_ok=True)

# Geometric reconstruction of the two-wing-and-dot mark in the approved identity sheet.
UPPER = "M 11 126 C 27 79 47 64 88 51 L 207 12 C 193 53 171 74 132 88 Z"
LOWER = "M 56 175 C 79 130 97 112 132 99 L 195 73 C 180 117 151 141 111 154 Z"
DEFS = '''<defs><linearGradient id="upper" x1="0" y1="1" x2="1" y2="0"><stop stop-color="#064b5c"/><stop offset=".6" stop-color="#05c98c"/><stop offset="1" stop-color="#0aff81"/></linearGradient><linearGradient id="lower" x1="0" y1="1" x2="1" y2="0"><stop stop-color="#10212e"/><stop offset="1" stop-color="#059f7b"/></linearGradient></defs>'''

def symbol(dark: bool = False, mono: str | None = None) -> str:
    lower = '#54d7b1' if dark else 'url(#lower)'
    dot = '#f5fbfa' if dark else '#10212e'
    return f'<g transform="translate(0 33)"><path d="{UPPER}" fill="{mono or "url(#upper)"}"/><path d="{LOWER}" fill="{mono or lower}"/><circle cx="231" cy="43" r="13" fill="{mono or dot}"/></g>'

def svg(content: str, view: str = "0 0 256 256", label: str = "Svolo") -> str:
    return f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="{view}" role="img" aria-label="{label}">{DEFS}{content}</svg>\n'

def write(name: str, data: str) -> Path:
    p = ROOT / name; p.parent.mkdir(parents=True, exist_ok=True); p.write_text(data, encoding="utf-8"); return p

def render(source: str, size: int) -> Image.Image:
    data = cairosvg.svg2png(bytestring=source.encode(), output_width=size, output_height=size)
    return Image.open(io.BytesIO(data)).convert("RGBA")

mark = svg(symbol())
mark_dark = svg(symbol(True))
write('brand/mark.svg', mark)
write('brand/mark-dark.svg', mark_dark)
write('core/internal/server/web/mark-dark.svg', mark_dark)
write('brand/mark-mono.svg', svg(symbol(mono='currentColor')))
wordmark = svg(f'{symbol()}<text x="278" y="180" fill="#10212e" font-family="system-ui,-apple-system,Segoe UI,sans-serif" font-size="160" font-weight="650" letter-spacing="-6">Svolo</text>', '0 0 730 256')
write('brand/wordmark.svg', wordmark)
for theme in ('dark', 'light'):
    background = '#10212e' if theme == 'dark' else '#f5f8f9'
    source = svg(f'<rect width="256" height="256" rx="52" fill="{background}"/><g transform="translate(22 22) scale(.828125)">{symbol(theme=="dark")}</g>')
    write(f'brand/icons/app-{theme}.svg', source)
    for size in (16, 24, 32, 48, 64, 128, 180, 192, 256, 512, 1024):
        render(source, size).save(GENERATED / f'app-{theme}-{size}.png')
    base = render(source, 1024)
    base.save(GENERATED / f'app-{theme}.ico', sizes=[(n,n) for n in (16,24,32,48,64,128,256)])
    if theme == 'dark': base.save(GENERATED / 'app.icns')

# Icon source is transparent. The tiny output is a real raster at each stated size.
for size in (16,24,32,48,64,128,256,512):
    render(mark, size).save(GENERATED / f'mark-{size}.png')
render(mark,256).save(GENERATED/'favicon.ico',sizes=[(n,n) for n in (16,24,32,48,64)])
# Dark tabs receive a light dot and a brighter lower wing, without a raster background.
favicon = mark.replace('</defs>', '</defs><style>@media(prefers-color-scheme:dark){circle{fill:#f5fbfa}path:nth-of-type(2){fill:#54d7b1}}</style>')
write('brand/icons/favicon.svg',favicon)

for dest in ('desktop/resources/icon.svg','desktop/src/renderer/src/assets/svolo-mark.svg','core/internal/server/web/mark.svg'):
    write(dest,mark)
for dest in ('desktop/resources/icon.png','desktop/build/icon.png'):
    p=ROOT/dest;p.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(GENERATED/'app-dark-512.png',p)
for src,dest in [('app-dark.ico','desktop/build/icon.ico'),('app.icns','desktop/build/icon.icns')]:
    shutil.copyfile(GENERATED/src,ROOT/dest)

# The approved artwork is presentation material, not evidence of a running build.
for source in (BRAND/'sources').glob('*.png'):
    image=Image.open(source).convert('RGB')
    image.save(BRAND/(source.stem+'.webp'),quality=88,method=6)

# Crops deliberately exclude all labels, UI windows and logotypes in the artwork.
landscape=Image.open(BRAND/'sources/product-hero.png').convert('RGB').crop((174,590,642,851))
landscape.save(BRAND/'landscape.webp',quality=92,method=6)
variations = {
    'sky': (1.0, .80, .8), 'stars': (.88,.55,2.3), 'peak': (1.08,.7,1.2),
    'pines': (.92,.90,.4), 'shadow': (.90,.45,2.8), 'ink': (1.02,.08,3.0),
    'fresco': (1.08,.45,3.5),
}
wallpapers=ROOT/'desktop/src/renderer/src/assets/wallpapers';(wallpapers/'thumbs').mkdir(parents=True,exist_ok=True)
for name,(brightness,saturation,blur) in variations.items():
    image=ImageOps.fit(landscape,(1600,900),method=Image.Resampling.LANCZOS)
    image=ImageEnhance.Color(image).enhance(saturation)
    image=ImageEnhance.Brightness(image).enhance(brightness).filter(ImageFilter.GaussianBlur(blur))
    for theme in ('day','dusk'):
        tint='#f1f6f7' if theme=='day' else '#0f172a'
        value=Image.blend(image,Image.new('RGB',image.size,tint),.43 if theme=='day' else .52)
        value.save(wallpapers/f'{name}-{theme}.webp',quality=86,method=6)
        ImageOps.fit(value,(480,270),method=Image.Resampling.LANCZOS).save(wallpapers/'thumbs'/f'{name}-{theme}.webp',quality=83,method=6)

for directory in (ROOT/'core/internal/server/web',ROOT/'desktop/src/renderer/public'):
    directory.mkdir(parents=True,exist_ok=True)
    for src,dst in [('favicon.ico','favicon.ico'),('favicon.svg','favicon.svg'),('app-light-180.png','apple-touch-icon.png'),('app-dark-192.png','app-192.png'),('app-dark-512.png','app-512.png')]:
        shutil.copyfile(GENERATED/src,directory/dst)
    mask=svg(f'<rect width="256" height="256" fill="#10212e"/><g transform="translate(42 42) scale(.67)">{symbol(True)}</g>')
    render(mask,512).save(directory/'app-maskable-512.png')
    manifest={'name':'Svolo','short_name':'Svolo','start_url':'/','display':'standalone','background_color':'#0f172a','theme_color':'#0f172a','icons':[{'src':'app-192.png','sizes':'192x192','type':'image/png'},{'src':'app-512.png','sizes':'512x512','type':'image/png'},{'src':'app-maskable-512.png','sizes':'512x512','type':'image/png','purpose':'maskable'}]}
    (directory/'site.webmanifest').write_text(json.dumps(manifest,indent=2)+'\n')

# Inline the trusted mark in the shared console; do not fetch it through remote page content.
p=ROOT/'core/internal/server/web/client.js'
text=p.read_text();old='<span class="ac-mark">S</span>'
inline=mark.strip().replace('role="img"','class="ac-mark" role="img"')
if old in text:text=text.replace(old,inline)
else:
    import re
    text=re.sub(r'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256" class="ac-mark".*?</svg>',lambda _:inline,text,count=1)
p.write_text(text)
# Metadata describes this product's assets, not a history of another project.
tokens={'graphite':'#0f172a','ink':'#10212e','green':'#00e676','teal':'#007b53','mist':'#e8ecef','white':'#ffffff','systemFont':'system-ui, -apple-system, Segoe UI, sans-serif'}
write('brand/tokens.json',json.dumps(tokens,indent=2)+'\n')
asset_files=[]
for directory in (BRAND,ROOT/'desktop/src/renderer/src/assets/wallpapers'):
    for p in sorted(directory.rglob('*')):
        if p.is_file() and p.name!='assets.json' and p.suffix.lower() in ('.png','.svg','.webp','.ico','.icns'):
            entry={'path':p.relative_to(ROOT).as_posix(),'sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'bytes':p.stat().st_size}
            if p.suffix!='.svg':
                im=Image.open(p);entry['width'],entry['height']=im.size
            asset_files.append(entry)
write('brand/assets.json',json.dumps({'version':1,'product':'Svolo','presentationImagesAreScreenshots':False,'landscapeCrop':[174,590,642,851],'assets':asset_files},indent=2)+'\n')
print(f'Built {len(asset_files)} brand assets plus application copies. No external requests.')
