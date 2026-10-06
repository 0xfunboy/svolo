#!/usr/bin/env python3
"""Check source identity, documentation, contracts, and committed asset integrity.

This is deliberately separate from compilation and runtime qualification. Optional
--forbid-text arguments let a release owner enforce their own text policy without
putting retired product identifiers in the source tree.
"""
from __future__ import annotations
import argparse, hashlib, json, re, struct, sys
from pathlib import Path
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parents[1]
SKIP = {'.git', 'node_modules', '__pycache__', '.pytest_cache', '.venv', 'out', 'dist'}
CODE_EXTS = {'.ts', '.tsx', '.mts', '.js', '.mjs', '.cjs'}


def run(root: Path, forbidden: list[str]) -> dict:
    errors: list[str] = []
    counts = {'json': 0, 'markdownLinks': 0, 'sourceImports': 0, 'assets': 0, 'files': 0}
    def check(condition: bool, message: str) -> None:
        if not condition: errors.append(message)
    def read_json(relative: str):
        return json.loads((root / relative).read_text(encoding='utf-8'))
    try:
        product = read_json('product.json')
        package = read_json('desktop/package.json')
        check(product['name'] == 'Svolo' and product['slug'] == 'svolo', 'Unexpected product identity')
        check(package['version'] == product['version'], 'Desktop version differs from product.json')
        check(package['productName'] == product['name'] and package['name'] == product['slug'], 'Desktop product name mismatch')
        check(package['author'] == product['owner'], 'Desktop owner mismatch')
        check(package['license'] == 'SEE LICENSE IN ../LICENSE.md', 'Desktop license pointer mismatch')
        identity = (root / 'core/internal/identity/identity.go').read_text()
        check(f'const Version = "{product["version"]}"' in identity, 'Go version differs from product.json')
        check(f'const AppID = "{product["appId"]}"' in identity, 'Go application ID differs from product.json')
        builder = (root / 'desktop/electron-builder.yml').read_text()
        check(f'appId: {product["appId"]}' in builder and 'publish: null' in builder, 'Unexpected packaging identity or publishing channel')
        check((root / 'LICENSE.md').is_file() and (root / 'legal/desktop-components.txt').is_file(), 'Applicable license notice missing')
        tools = read_json('schemas/tools.json')
        check(tools['product'] == product['name'] and tools['version'] == product['version'], 'Catalog metadata is stale')
        names = [t['name'] for t in tools['tools']]
        check(len(names) == len(set(names)) and len(names) > 0, 'Duplicate or empty tool definitions')
        for tool in tools['tools']:
            check(bool(tool['description']) and tool['inputSchema'].get('additionalProperties') is False, f'Invalid tool definition: {tool["name"]}')
        canonical = (root / 'schemas/task-plan.schema.json').read_bytes()
        for path in (root/'desktop/resources/atp/skills').glob('*/references/atp-schema.json'):
            check(path.read_bytes() == canonical, f'Task schema copy differs: {path.relative_to(root)}')
    except (OSError, KeyError, ValueError, TypeError) as exc:
        errors.append(f'Metadata: {exc}')

    paths = [p for p in root.rglob('*') if p.is_file() and not (set(p.relative_to(root).parts) & SKIP)]
    for p in paths:
        relative = p.relative_to(root).as_posix(); counts['files'] += 1
        check(not p.is_symlink(), f'Source symlink is not permitted in the package: {relative}')
        check(p.resolve().is_relative_to(root.resolve()), f'Path escapes source root: {relative}')
        check(p.name not in {'.DS_Store', 'Thumbs.db', 'auth.token', 'vault.key'}, f'Private or workstation artifact: {relative}')
        check(not p.name.endswith(('.p12', '.pfx', '.pem', '.key')), f'Key/certificate artifact: {relative}')
        raw = p.read_bytes()
        try: text = raw.decode('utf-8')
        except UnicodeDecodeError: text = None
        if forbidden:
            for term in forbidden:
                if term.casefold() in relative.casefold() or (text is not None and term.casefold() in text.casefold()):
                    errors.append(f'Forbidden text in {relative}')
                    break
        if text is None: continue
        if p.suffix in {'.json', '.webmanifest'}:
            try: json.loads(text); counts['json'] += 1
            except ValueError as exc: errors.append(f'JSON {relative}: {exc}')
        if p.suffix == '.md':
            plain = re.sub(r'```.*?```', '', text, flags=re.S)
            links = re.findall(r'!?\[[^\]]*\]\(([^)]+)\)', plain)
            links += re.findall(r'(?:src|href)=["\']([^"\']+)["\']', plain)
            for raw_target in links:
                target = raw_target.split(' "', 1)[0].strip('<>')
                if re.match(r'^[a-zA-Z][\w+.-]*:', target) or target.startswith(('#', '//')): continue
                target = unquote(target.split('#', 1)[0].split('?', 1)[0])
                if not target: continue
                q = (p.parent / target).resolve(); counts['markdownLinks'] += 1
                check(q.is_relative_to(root.resolve()) and q.exists(), f'Broken local link: {relative} -> {target}')
        if p.suffix in CODE_EXTS:
            # Explicit static specifiers only; interpolated runtime paths are not static imports.
            specifiers = re.findall(r'(?:\bfrom\s*|\bimport\s*\(\s*|\brequire\s*\(\s*|\bimport\s*)["\'](\.[^"\']+)["\']', text)
            for specifier in specifiers:
                if '${' in specifier: continue
                base = p.parent / specifier.split('?',1)[0]
                candidates = [base] + [Path(str(base) + suffix) for suffix in ('.ts','.tsx','.js','.mts','.mjs','.json','.d.ts')]
                candidates += [base / ('index' + suffix) for suffix in ('.ts','.tsx','.js','.mts')]
                if base.suffix == '.js': candidates.extend([base.with_suffix('.ts'), base.with_suffix('.tsx')])
                counts['sourceImports'] += 1
                check(any(q.is_file() for q in candidates), f'Unresolved local import: {relative} -> {specifier}')

    try:
        manifest = read_json('brand/assets.json')
        check(manifest['presentationImagesAreScreenshots'] is False, 'Artwork cannot be recorded as runtime evidence')
        for entry in manifest['assets']:
            p = root / entry['path']; counts['assets'] += 1
            if not p.resolve().is_relative_to(root.resolve()) or not p.is_file():
                errors.append(f'Missing asset: {entry["path"]}'); continue
            data = p.read_bytes()
            check(len(data) == entry['bytes'] and hashlib.sha256(data).hexdigest() == entry['sha256'], f'Changed asset without manifest update: {entry["path"]}')
            if p.suffix == '.png' and len(data) >= 24:
                width, height = struct.unpack('>II', data[16:24])
                check((width,height) == (entry.get('width'),entry.get('height')), f'PNG dimensions mismatch: {entry["path"]}')
        for theme in ('dark','light'):
            for size in (16,24,32,48,64,128,180,192,256,512,1024):
                data = (root / f'brand/icons/app-{theme}-{size}.png').read_bytes()
                check(data[:8] == b'\x89PNG\r\n\x1a\n' and struct.unpack('>II', data[16:24]) == (size,size), f'Invalid icon: {theme}/{size}')
        for copy, master in {
            'desktop/build/icon.png':'brand/icons/app-dark-512.png',
            'desktop/resources/icon.png':'brand/icons/app-dark-512.png',
            'desktop/build/icon.ico':'brand/icons/app-dark.ico',
            'desktop/build/icon.icns':'brand/icons/app.icns',
            'core/internal/server/web/mark.svg':'brand/mark.svg',
            'core/internal/server/web/mark-dark.svg':'brand/mark-dark.svg',
            'desktop/src/renderer/src/assets/svolo-mark.svg':'brand/mark.svg',
        }.items():
            check((root/copy).read_bytes() == (root/master).read_bytes(), f'Asset copy differs: {copy}')
        for folder in ('core/internal/server/web','desktop/src/renderer/public'):
            for name in ('favicon.svg','favicon.ico'):
                check((root/folder/name).read_bytes() == (root/'brand/icons'/name).read_bytes(), f'Favicon copy differs: {folder}/{name}')
            site = read_json(f'{folder}/site.webmanifest')
            check(site['name']=='Svolo', f'Web application identity: {folder}')
            for icon in site['icons']:
                check((root/folder/icon['src'].lstrip('/')).is_file(), f'Missing PWA icon: {icon["src"]}')
    except (OSError, KeyError, ValueError, struct.error) as exc:
        errors.append(f'Assets: {exc}')
    return {'status':'pass' if not errors else 'fail', 'counts':counts, 'errors':sorted(set(errors)), 'compilation':False, 'runtimeQualification':False}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=ROOT)
    parser.add_argument('--forbid-text', action='append', default=[])
    args = parser.parse_args()
    result = run(args.root.resolve(), args.forbid_text)
    print(json.dumps(result, indent=2, ensure_ascii=False))
    sys.exit(0 if result['status']=='pass' else 1)

if __name__ == '__main__': main()
