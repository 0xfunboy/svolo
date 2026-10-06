// One-time local bootstrap. Password input is read from stdin, never argv/logs.
import { hashPassword } from './database.mjs';
import { mkdirSync, writeFileSync, existsSync } from 'node:fs';
import { dirname } from 'node:path';
const path=process.env.SVOLO_WEB_ADMIN_FILE;
if(!path)throw new Error('Imposta SVOLO_WEB_ADMIN_FILE.');
if(existsSync(path))throw new Error('Il bootstrap esiste già.');
const username=process.env.SVOLO_ADMIN_USERNAME;
if(!/^[A-Za-z0-9_.-]{3,32}$/.test(username??''))throw new Error('Set SVOLO_ADMIN_USERNAME (3 to 32 characters).');
const chunks=[];for await(const chunk of process.stdin)chunks.push(chunk);
const password=Buffer.concat(chunks).toString('utf8').replace(/\r?\n$/,'');
const passwordHash=await hashPassword(password);
mkdirSync(dirname(path),{recursive:true,mode:0o700});
writeFileSync(path,JSON.stringify({username,passwordHash})+'\n',{mode:0o600,flag:'wx'});
console.log('Primo amministratore configurato.');
