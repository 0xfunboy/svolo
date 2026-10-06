import { DatabaseSync } from 'node:sqlite';
import { randomBytes, createHash, createCipheriv, createDecipheriv, scrypt, timingSafeEqual } from 'node:crypto';
import { promisify } from 'node:util';
import { mkdirSync, readFileSync, statSync, chmodSync } from 'node:fs';
import { join } from 'node:path';

const derive = promisify(scrypt);
export const hashToken = value => createHash('sha256').update(value).digest('hex');
export async function hashPassword(password) {
  if (typeof password !== 'string' || password.length < 10 || password.length > 256) throw Object.assign(new Error('La password deve contenere da 10 a 256 caratteri.'),{status:400});
  const salt = randomBytes(16);
  const key = await derive(password, salt, 64, { N: 32768, r: 8, p: 1, maxmem: 64 * 1024 * 1024 });
  return `scrypt$32768$8$1$${salt.toString('hex')}$${key.toString('hex')}`;
}
export async function verifyPassword(password, encoded) {
  if (typeof password !== 'string' || password.length > 256 || !encoded) return false;
  const [algorithm,n,r,p,salt,key] = encoded.split('$');
  if (algorithm !== 'scrypt' || Number(n) !== 32768 || Number(r) !== 8 || Number(p) !== 1) return false;
  if(!/^[a-f0-9]{32}$/.test(salt??'')||!/^[a-f0-9]{128}$/.test(key??''))return false;
  const derived = await derive(password, Buffer.from(salt,'hex'), 64, { N: Number(n), r: Number(r), p: Number(p), maxmem:64*1024*1024 });
  const expected=Buffer.from(key,'hex');
  return expected.length === derived.length && timingSafeEqual(expected,derived);
}

export function openDatabase(dataDir, keyFile) {
  mkdirSync(dataDir, { recursive:true, mode:0o700 });
  if ((statSync(keyFile).mode & 0o077) !== 0) throw new Error('La chiave del database deve avere permessi 0600.');
  const encodedKey=readFileSync(keyFile,'utf8').trim();
  if(!/^[a-f0-9]{64}$/i.test(encodedKey))throw new Error('Chiave del database non valida.');
  const key = Buffer.from(encodedKey,'hex');
  if (key.length !== 32) throw new Error('Chiave del database non valida.');
  const dbPath=join(dataDir,'svolo.sqlite');
  const db=new DatabaseSync(dbPath);
  chmodSync(dbPath,0o600);
  db.exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;
    CREATE TABLE IF NOT EXISTS users(id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE COLLATE NOCASE, password_hash TEXT NOT NULL, role TEXT NOT NULL CHECK(role IN ('admin','user')), disabled INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL);
    CREATE TABLE IF NOT EXISTS web_sessions(token_hash TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, csrf TEXT NOT NULL, expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL);
    CREATE TABLE IF NOT EXISTS user_preferences(user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, provider_id TEXT, model TEXT);
    CREATE TABLE IF NOT EXISTS mcp_servers(user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,id TEXT NOT NULL, config_json TEXT NOT NULL, credential_enc TEXT, PRIMARY KEY(user_id,id));
    CREATE TABLE IF NOT EXISTS user_run_prompts(user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,run_id TEXT NOT NULL,session_id TEXT NOT NULL,prompt_enc TEXT NOT NULL,PRIMARY KEY(user_id,run_id));
    CREATE TABLE IF NOT EXISTS chat_preferences(user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,session_id TEXT NOT NULL,pinned INTEGER NOT NULL DEFAULT 0 CHECK(pinned IN (0,1)),PRIMARY KEY(user_id,session_id));
    CREATE TABLE IF NOT EXISTS app_metadata(key TEXT PRIMARY KEY,value TEXT NOT NULL);
    CREATE TABLE IF NOT EXISTS audit_log(id INTEGER PRIMARY KEY AUTOINCREMENT,user_id TEXT,action TEXT NOT NULL,created_at TEXT NOT NULL);
  `);
  const encrypt = (plaintext, context='') => {
    const nonce=randomBytes(12),cipher=createCipheriv('aes-256-gcm',key,nonce);
    cipher.setAAD(Buffer.from(context));
    const ciphertext=Buffer.concat([cipher.update(String(plaintext),'utf8'),cipher.final()]);
    return Buffer.concat([nonce,cipher.getAuthTag(),ciphertext]).toString('base64');
  };
  const decrypt = (encoded, context='') => {
    const raw=Buffer.from(encoded,'base64');
    if(raw.length<28) throw new Error('Credenziale cifrata non valida.');
    const decipher=createDecipheriv('aes-256-gcm',key,raw.subarray(0,12));
    decipher.setAAD(Buffer.from(context));decipher.setAuthTag(raw.subarray(12,28));
    return Buffer.concat([decipher.update(raw.subarray(28)),decipher.final()]).toString('utf8');
  };
  // Explicit, atomic migration of the first development gateway's MCP/core
  // ciphertext to tenant-bound authenticated encryption. Provider credentials
  // and prompts already use the tenant context and are not rewritten.
  if(!db.prepare("SELECT value FROM app_metadata WHERE key='tenant-crypto-v1'").get()){
    db.exec('BEGIN IMMEDIATE');
    try{
      for(const row of db.prepare('SELECT user_id,id,credential_enc FROM mcp_servers WHERE credential_enc IS NOT NULL').all())db.prepare('UPDATE mcp_servers SET credential_enc=? WHERE user_id=? AND id=?').run(encrypt(decrypt(row.credential_enc),row.user_id),row.user_id,row.id);
      for(const row of db.prepare("SELECT key,value FROM app_metadata WHERE key LIKE 'core-key:%'").all()){const userId=row.key.slice(9);if(!/^[a-f0-9]{32}$/.test(userId))throw new Error('Identità chiave core non valida.');db.prepare('UPDATE app_metadata SET value=? WHERE key=?').run(encrypt(decrypt(row.value),userId),row.key);}
      db.prepare("INSERT INTO app_metadata(key,value) VALUES('tenant-crypto-v1','complete')").run();db.exec('COMMIT');
    }catch(error){db.exec('ROLLBACK');db.close();throw error;}
  }
  return { db,encrypt,decrypt };
}

export function safeUser(row) { return { id:row.id, username:row.username, role:row.role, disabled:!!row.disabled }; }
export function audit(db,userId,action) {
  db.prepare('INSERT INTO audit_log(user_id,action,created_at) VALUES(?,?,?)').run(userId ?? null,action,new Date().toISOString());
  db.exec('DELETE FROM audit_log WHERE id < (SELECT coalesce(max(id),0)-10000 FROM audit_log)');
}
