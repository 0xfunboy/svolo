// Syntax/transpilation smoke check ONLY. This is not tsc typechecking or a Vite build.
const fs=require('node:fs'),path=require('node:path'),cp=require('node:child_process');
let ts;try{ts=require(path.resolve(__dirname,'../desktop/node_modules/typescript'));}catch{const global=cp.execFileSync('npm',['root','-g'],{encoding:'utf8'}).trim();ts=require(path.join(global,'typescript'));}
const root=path.resolve(__dirname,'..');const files=[];
function walk(dir){for(const d of fs.readdirSync(dir,{withFileTypes:true})){if(d.name==='node_modules'||d.name==='out'||d.name==='build')continue;const p=path.join(dir,d.name);if(d.isDirectory())walk(p);else if(/\.(tsx?|mts)$/.test(d.name)&&!d.name.endsWith('.d.ts'))files.push(p);}}
walk(path.join(root,'desktop/src'));walk(path.join(root,'desktop/resources'));let errors=0;
for(const file of files){const result=ts.transpileModule(fs.readFileSync(file,'utf8'),{fileName:file,compilerOptions:{target:ts.ScriptTarget.ESNext,module:ts.ModuleKind.ESNext,jsx:ts.JsxEmit.ReactJSX},reportDiagnostics:true});for(const d of result.diagnostics||[]){if(d.category===ts.DiagnosticCategory.Error){console.error(path.relative(root,file),ts.flattenDiagnosticMessageText(d.messageText,'\n'));errors++;}}}
console.log(JSON.stringify({check:'syntax-and-transpilation-only',typescript:ts.version,files:files.length,errors,fullTypecheck:false,desktopBuild:false},null,2));if(errors)process.exit(1);
