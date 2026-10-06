#!/usr/bin/env node
/** Read-only release planning. Identity is owned by the root product manifest.
 * This tool never commits, tags, pushes, downloads, signs, or publishes a build.
 * Actual publication remains guarded by release-guard.mjs and docs/RELEASE.md.
 */
import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';

function parse(version) {
  const match = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([A-Za-z0-9.-]+))?$/.exec(version ?? '');
  if (!match) throw new Error(`Invalid semantic version: ${version}`);
  return { numbers: match.slice(1,4).map(Number), prerelease: match[4] };
}
export function nextVersion(current, bump) {
  const from = parse(current);
  if (['patch','minor','major'].includes(bump)) {
    const [major,minor,patch] = from.numbers;
    return bump==='major' ? `${major+1}.0.0` : bump==='minor' ? `${major}.${minor+1}.0` : `${major}.${minor}.${patch+1}`;
  }
  const version = String(bump ?? '').replace(/^v/,'');
  const to = parse(version);
  if (to.prerelease) throw new Error('A stable release cannot have a prerelease suffix');
  const order = to.numbers.findIndex((part,i)=>part!==from.numbers[i]);
  if (order<0 ? !from.prerelease : to.numbers[order]<from.numbers[order]) throw new Error(`${version} is not newer than ${current}`);
  return version;
}
export function releaseNotes(changelog, version) {
  const v = String(version).replace(/^v/,''); parse(v);
  const escaped = v.replace(/[.*+?^${}()|[\]\\]/g,'\\$&');
  const heading = new RegExp(`^## ${escaped}(?:[ \\t].*)?$`,'m').exec(changelog);
  if (!heading) throw new Error(`No release notes for ${v}`);
  const rest = changelog.slice(heading.index+heading[0].length);
  const next = rest.search(/^## /m);
  const notes = (next<0 ? rest : rest.slice(0,next)).trim();
  if (!notes) throw new Error(`Empty release notes for ${v}`);
  return notes;
}
export function cutRelease(changelog, version, date) {
  parse(version);
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) throw new Error('Date must be YYYY-MM-DD');
  const heading = /^## Unreleased[ \t]*$/m.exec(changelog);
  if (!heading) throw new Error('No Unreleased section');
  try { releaseNotes(changelog,version); throw new Error('Release version already exists'); }
  catch(error) { if (!error.message.startsWith('No release notes')) throw error; }
  const offset = heading.index+heading[0].length;
  const tail=changelog.slice(offset), end=tail.search(/^## /m);
  const entries=(end<0 ? tail : tail.slice(0,end)).trim();
  if (!entries) throw new Error('Unreleased section is empty');
  return `${changelog.slice(0,offset)}\n\n## ${version} - ${date}\n\n${entries}${end<0 ? '' : '\n\n'+tail.slice(end).trimEnd()}\n`;
}
function main(args) {
  const product=JSON.parse(readFileSync(new URL('../../product.json',import.meta.url),'utf8'));
  const changelog=readFileSync(new URL('../../CHANGELOG.md',import.meta.url),'utf8');
  if (args[0]==='notes') console.log(releaseNotes(changelog,args[1] ?? product.version));
  else if (args[0]==='plan') console.log(JSON.stringify({current:product.version,next:nextVersion(product.version,args[1]),writesFiles:false,publishingEnabled:false},null,2));
  else throw new Error('Usage: release.mjs notes [version] | plan <patch|minor|major|X.Y.Z>. No files are changed.');
}
if (process.argv[1] && import.meta.url===pathToFileURL(process.argv[1]).href) {
  try { main(process.argv.slice(2)); } catch(error) { console.error(error.message);process.exitCode=1; }
}
