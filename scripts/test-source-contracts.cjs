#!/usr/bin/env node
/** Dependency-light tests for pure TypeScript contracts, not the Electron suite.
 * Transpiles only the explicitly imported local modules, then asserts their output
 * with Node's own test runner. No mock type checker or substitute browser is used.
 */
const assert = require('node:assert/strict');
const { test } = require('node:test');
const fs = require('node:fs');
const path = require('node:path');
const Module = require('node:module');
const { execFileSync } = require('node:child_process');
const root = path.resolve(__dirname, '..');
let ts;
try { ts = require(path.join(root,'desktop/node_modules/typescript')); }
catch { ts = require(path.join(execFileSync('npm',['root','-g'],{encoding:'utf8'}).trim(),'typescript')); }
const cache = new Map();
function source(relative) {
  let file = path.resolve(root,relative);
  if (!file.startsWith(root + path.sep)) throw new Error('Source path escapes repository');
  if (!path.extname(file)) file += '.ts';
  if (cache.has(file)) return cache.get(file).exports;
  const result = ts.transpileModule(fs.readFileSync(file,'utf8'), {
    compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS,esModuleInterop:true},
    fileName:file,reportDiagnostics:true,
  });
  if (result.diagnostics?.some(d=>d.category===ts.DiagnosticCategory.Error)) throw new Error('Syntax error in ' + file);
  const mod = new Module(file, module); mod.filename=file; mod.paths=Module._nodeModulePaths(path.dirname(file));
  mod.require = name => name.startsWith('.') ? source(path.relative(root,path.resolve(path.dirname(file),name))) : require(name);
  cache.set(file,mod); mod._compile(result.outputText,file); return mod.exports;
}
const atp = source('desktop/src/shared/atp.ts');
const login = source('desktop/src/renderer/src/lib/login.ts');
const badges = source('desktop/src/renderer/src/lib/provider-badges.ts');

test('task paths accept absolute POSIX and Windows paths, never relative or NUL paths', () => {
  for (const value of ['/work/plan.atp.json','C:/work/plan.atp.json','C:\\work\\plan.atp.json']) assert.equal(atp.isPlanPath(value),true,value);
  for (const value of [undefined,null,123,'relative.atp.json','C:plan.atp.json','/work/plan.json','/x\0.atp.json']) assert.equal(atp.isPlanPath(value),false,String(value));
});
test('plan names do not expose a Windows parent path', () => {
  assert.equal(atp.planName('/work/check.atp.json'),'check');
  assert.equal(atp.planName('C:\\work\\check.atp.json'),'check');
});
test('closed nodes and scopes are not counted as executable work', () => {
  const plan = atp.parsePlan('/work/t.atp.json',{meta:{project_name:'Contract',project_status:'PAUSED'},nodes:{
    A:{title:'A',instruction:'A',dependencies:[],status:'COMPLETED'},
    B:{title:'B',instruction:'B',dependencies:['A'],status:'CLAIMED'},
    C:{title:'C',instruction:'C',dependencies:[],status:'LOCKED',future_state:'SUPERSEDED'},
    S:{title:'S',instruction:'S',dependencies:[],status:'CLAIMED',type:'SCOPE',scope_children:['B']},
  }});
  assert.equal(plan.status,'PAUSED');
  assert.deepEqual(atp.planProgress(plan),{total:2,completed:1,failed:0,claimed:1,ready:0,locked:0});
  assert.deepEqual(atp.workingNodes(plan).map(n=>n.id),['B']);
});
test('claim packets remain intact and unexpected execution output is rejected', () => {
  const packet='TASK ASSIGNED: A - Inspect\nSTATUS: CLAIMED\nINSTRUCTION:\nRead only';
  assert.equal(atp.parseClaim(packet).packet,packet);
  assert.equal(atp.parseClaim('NO_TASKS_AVAILABLE: blocked').kind,'none');
  assert.throws(()=>atp.parseClaim('Traceback: execution failed'));
});
test('worker instructions retain scope, verification, and completion constraints', () => {
  const message=atp.workerMessage({project:'/w',plan:'/w/p.atp.json',librarian:'/tool/lib.py',branch:'main',claim:{kind:'assigned',node:'A',title:'Inspect',packet:'TASK ASSIGNED: A - Inspect'}});
  for (const text of ['claimed_node_id: A','Do not run atp-claim-task','Never leave the node CLAIMED','Verification','atp-complete-task']) assert.ok(message.includes(text),text);
});
test('provider badges are text-only product-themed labels', () => {
  for (const badge of Object.values(badges.BADGES)) {
    assert.match(badge.label,/^[A-Z]{2}$/);
    assert.equal(badge.color,'var(--accent)');
    assert.equal(badge.background,'var(--accent-soft)');
    assert.deepEqual(Object.keys(badge).sort(),['background','color','label']);
  }
  assert.equal(login.badgeFor('anthropic'),badges.BADGES.Anthropic);
  assert.equal(login.badgeFor('qwen-token-plan-intl'),badges.BADGES.Qwen);
  assert.equal(login.badgeFor('private-proxy'),undefined);
});
test('login prompts preserve concurrent questions and remove only the answered prompt', () => {
  const provider={id:'fixture',name:'Fixture'};
  let view=login.startLogin(provider,'oauth');
  for (const n of [1,2]) view=login.updateLogin(view,{kind:'prompt',prompt:{n,type:'text',message:'Question'}});
  assert.deepEqual(login.answered(view,1).prompts.map(p=>p.n),[2]);
  assert.deepEqual(login.updateLogin(view,{kind:'withdraw',n:2}).prompts.map(p=>p.n),[1]);
});
test('catalog, desktop, and core identify the same product version', () => {
  const json=p=>JSON.parse(fs.readFileSync(path.join(root,p),'utf8'));
  const p=json('product.json'),pkg=json('desktop/package.json'),catalog=json('schemas/tools.json');
  assert.equal(pkg.version,p.version);assert.equal(catalog.version,p.version);assert.equal(catalog.product,p.name);
  assert.equal(new Set(catalog.tools.map(t=>t.name)).size,catalog.tools.length);
  assert.equal(p.productionQualified,false);
});

test('release planning is read-only and supports the actual prerelease manifest', async () => {
  const {pathToFileURL}=require('node:url');
  const release=await import(pathToFileURL(path.join(root,'desktop/scripts/release.mjs')).href);
  const product=JSON.parse(fs.readFileSync(path.join(root,'product.json'),'utf8'));
  assert.ok(release.releaseNotes(fs.readFileSync(path.join(root,'CHANGELOG.md'),'utf8'),product.version));
  assert.equal(release.nextVersion('1.2.3-dev','1.2.3'),'1.2.3');
  assert.equal(release.nextVersion('1.2.3','patch'),'1.2.4');
  assert.throws(()=>release.nextVersion('1.2.3','1.2.2'));
  assert.throws(()=>release.nextVersion('1.2.3','1.2.4-beta'));
  assert.match(release.cutRelease('## Unreleased\n\nVerified.\n','1.2.4','2026-10-05'),/## 1\.2\.4/);
});
