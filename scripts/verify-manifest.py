#!/usr/bin/env python3
"""Verify delivered file bytes against MANIFEST.sha256; no external requests."""
from pathlib import Path
import hashlib,sys
root=Path(__file__).resolve().parents[1]
manifest=root/'MANIFEST.sha256'
if not manifest.exists():raise SystemExit('MANIFEST.sha256 not found (included in the delivery ZIP).')
errors=[];count=0
for line in manifest.read_text(encoding='utf8').splitlines():
    digest,name=line.split('  ',1);p=root/name
    if not p.resolve().is_relative_to(root.resolve()):raise SystemExit('Manifest path escapes root')
    count+=1
    if not p.is_file() or hashlib.sha256(p.read_bytes()).hexdigest()!=digest:errors.append(name)
if errors:print('\n'.join(errors));raise SystemExit(1)
print(f'PASS: {count} file hashes verified. This checks integrity, not production readiness.')
