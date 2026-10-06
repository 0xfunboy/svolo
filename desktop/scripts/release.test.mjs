import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { nextVersion, cutRelease, releaseNotes } from './release.mjs';

describe('release planning',()=>{
  it('supports prerelease inputs and explicitly promoting their stable version',()=>{
    expect(nextVersion('1.2.3-dev','1.2.3')).toBe('1.2.3');
    expect(nextVersion('1.2.3-dev','minor')).toBe('1.3.0');
    expect(nextVersion('1.2.3','patch')).toBe('1.2.4');
  });
  it('rejects rollback, malformed versions, and prerelease targets',()=>{
    for (const target of ['1.2.2','1.2.3','bad','1.2','1.2.4-beta']) expect(()=>nextVersion('1.2.3',target)).toThrow();
  });
  it('extracts only the requested changelog section',()=>{
    expect(releaseNotes('## 1.2.3 - 2026-10-05\n\nA\n\n## 1.2.2\nB','v1.2.3')).toBe('A');
  });
  it('plans a changelog without mutating files',()=>{
    const text='# Changes\n\n## Unreleased\n\nA\n';
    expect(cutRelease(text,'1.2.3','2026-10-05')).toContain('## 1.2.3 - 2026-10-05\n\nA');
    expect(text).toBe('# Changes\n\n## Unreleased\n\nA\n');
  });
  it('refuses an empty, missing or duplicate section',()=>{
    expect(()=>cutRelease('## Unreleased\n','1.2.3','2026-10-05')).toThrow();
    expect(()=>cutRelease('# Changes','1.2.3','2026-10-05')).toThrow();
    expect(()=>cutRelease('## Unreleased\nA\n## 1.2.3\nB','1.2.3','2026-10-05')).toThrow();
  });
  it('reads the actual root product version and its material modification notice',()=>{
    const product=JSON.parse(readFileSync(new URL('../../product.json',import.meta.url),'utf8'));
    expect(releaseNotes(readFileSync(new URL('../../CHANGELOG.md',import.meta.url),'utf8'),product.version)).not.toBe('');
  });
});
