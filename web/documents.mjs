import {execFile} from 'node:child_process';
import {promisify,TextDecoder} from 'node:util';
import {mkdtemp,mkdir,writeFile,readFile,rm} from 'node:fs/promises';
import {existsSync} from 'node:fs';
import {join} from 'node:path';
const execute=promisify(execFile);
export const DOCUMENT_LIMITS=Object.freeze({fileBytes:20*1024*1024,userBytes:200*1024*1024,files:100,runFiles:8,pages:20,textChars:200000});
const failure=(message,status=400)=>Object.assign(new Error(message),{status});
export function documentType(name,bytes){
 if(typeof name!=='string'||name.length>160||name.startsWith('.')||/[\\/\x00-\x1f\x7f]/.test(name))throw failure('Invalid document filename.');
 const extension=name.toLowerCase().split('.').pop();const types={jpg:'image/jpeg',jpeg:'image/jpeg',png:'image/png',pdf:'application/pdf',txt:'text/plain',csv:'text/csv'};
 const mime=types[extension];if(!mime)throw failure('Supported files: JPG, PNG, PDF, TXT and CSV.',415);
 if(!bytes.length||bytes.length>DOCUMENT_LIMITS.fileBytes)throw failure('Files must contain 1 byte to 20 MiB.',413);
 if(mime==='image/png'&&!bytes.subarray(0,8).equals(Buffer.from([137,80,78,71,13,10,26,10])))throw failure('The file is not a valid PNG.');
 if(mime==='image/jpeg'&&!bytes.subarray(0,3).equals(Buffer.from([255,216,255])))throw failure('The file is not a valid JPEG.');
 if(mime==='application/pdf'&&!bytes.subarray(0,5).equals(Buffer.from('%PDF-')))throw failure('The file is not a valid PDF.');
 return {mime,extension};
}
export function createDocumentReader({tempRoot,ocrPrefix=process.env.SVOLO_OCR_PREFIX}={}){
 const systemOCR=existsSync('/usr/bin/tesseract');
 const hasOCR=systemOCR||Boolean(ocrPrefix&&existsSync(join(ocrPrefix,'usr/bin/tesseract')));
 const capabilities={types:['jpg','jpeg','png','pdf','txt','csv'],ocr:hasOCR,pdfText:['pdfinfo','pdftotext','pdfimages','pdftoppm'].every(name=>existsSync('/usr/bin/'+name)),maxFileBytes:DOCUMENT_LIMITS.fileBytes,maxPages:DOCUMENT_LIMITS.pages};
 let running=0;
 async function read(name,bytes){
  const {mime,extension}=documentType(name,bytes);
  if(mime.startsWith('text/')){let text;try{text=new TextDecoder('utf-8',{fatal:true}).decode(bytes)}catch{throw failure('TXT and CSV documents must use UTF-8.');}if(/[\x00-\x08\x0b\x0c\x0e-\x1f]/.test(text))throw failure('Binary data is not a text document.');return {mime,text:text.slice(0,DOCUMENT_LIMITS.textChars),method:'utf8',partial:text.length>DOCUMENT_LIMITS.textChars,pages:null,images:[]};}
  if(running>=2)throw failure('Document readers are busy. Try again shortly.',429);
  running++;let dir;const started=Date.now();
  try{
   await mkdir(tempRoot,{recursive:true,mode:0o700});dir=await mkdtemp(join(tempRoot,'document-'));const output=join(dir,'output');await mkdir(output,{mode:0o700});const input=join(dir,'input.'+extension);await writeFile(input,bytes,{mode:0o600});
   const run=async(program,args,timeout=30000)=>{
    timeout=Math.min(timeout,120000-(Date.now()-started));if(timeout<=0)throw failure('Document reading exceeded its time limit.');
    const sandbox=['--die-with-parent','--new-session','--unshare-all','--cap-drop','ALL','--clearenv','--setenv','PATH','/usr/bin:/bin','--setenv','LANG','C.UTF-8','--setenv','OMP_THREAD_LIMIT','1','--ro-bind','/usr','/usr','--symlink','usr/bin','/bin','--symlink','usr/lib','/lib','--symlink','usr/lib64','/lib64','--proc','/proc','--dev','/dev','--tmpfs','/tmp','--ro-bind',input,'/input.'+extension,'--bind',output,'/output'];
    if(ocrPrefix&&!systemOCR)sandbox.push('--ro-bind',ocrPrefix,'/opt/ocr','--setenv','LD_LIBRARY_PATH','/opt/ocr/usr/lib/x86_64-linux-gnu');
    try{return await execute('/usr/bin/prlimit',['--as=1073741824','--cpu=25','--fsize=67108864','--','/usr/bin/bwrap',...sandbox,program,...args],{timeout,maxBuffer:2*1024*1024,env:{PATH:'/usr/bin:/bin',LANG:'C.UTF-8'},killSignal:'SIGKILL'});}catch{throw failure('Document decoding failed or exceeded its resource limit. Check that the file is readable and unencrypted.');}
   };
   const ocr=async image=>{if(!hasOCR)throw failure('OCR is unavailable on this server.',503);const program=systemOCR?'/usr/bin/tesseract':'/opt/ocr/usr/bin/tesseract';const args=[image,'stdout','-l','eng+ita'];if(!systemOCR)args.push('--tessdata-dir','/opt/ocr/usr/share/tesseract-ocr/5/tessdata');return (await run(program,args)).stdout.trim();};
   if(mime.startsWith('image/')){const text=hasOCR?await ocr('/input.'+extension):'';return {mime,text:text.slice(0,DOCUMENT_LIMITS.textChars),method:hasOCR?'ocr':'vision',partial:text.length>DOCUMENT_LIMITS.textChars,pages:1,images:bytes.length<=8*1024*1024?[{mime,data:bytes.toString('base64')}]:[]};}
   if(!capabilities.pdfText)throw failure('PDF reading is unavailable on this server.',503);
   const info=(await run('/usr/bin/pdfinfo',['/input.pdf'])).stdout;const pages=Number(info.match(/^Pages:\s+(\d+)/m)?.[1]);if(!pages)throw failure('PDF page count is unavailable.');
   const count=Math.min(pages,DOCUMENT_LIMITS.pages);await run('/usr/bin/pdftotext',['-f','1','-l',String(count),'-layout','-enc','UTF-8','/input.pdf','/output/text.txt']);
   const raw=await readFile(join(output,'text.txt'),'utf8');const pageTexts=raw.split('\f');let text='',method='pdf-text';const images=[];
   const rasterPages=new Set((await run('/usr/bin/pdfimages',['-f','1','-l',String(count),'-list','/input.pdf'])).stdout.split('\n').map(line=>Number(line.match(/^\s*(\d+)\s+\d+\s+(?:image|mask|smask)\s/)?.[1])).filter(Boolean));
   // A text header can coexist with a scanned form: inspect raster pages too.
   for(let page=1;page<=count;page++){
    let value=pageTexts[page-1]??'';
    if(value.trim().length<30||rasterPages.has(page)){await run('/usr/bin/pdftoppm',['-f',String(page),'-l',String(page),'-scale-to','1500','-png','-singlefile','/input.pdf','/output/page']);const png=await readFile(join(output,'page.png'));if(hasOCR){const scanned=await ocr('/output/page.png');value=value.trim()?value+'\n[OCR]\n'+scanned:scanned;method='pdf-text+ocr';}if(images.length<4)images.push({mime:'image/png',data:png.toString('base64')});}
    text+=`\n[Page ${page}]\n${value}`;
   }
   return {mime,text:text.slice(0,DOCUMENT_LIMITS.textChars),method,partial:pages>count||text.length>DOCUMENT_LIMITS.textChars,pages,readPages:count,images};
  }finally{running--;if(dir)await rm(dir,{recursive:true,force:true});}
 }
 return {read,capabilities};
}
