import test from 'node:test';
import assert from 'node:assert/strict';
import dns from 'node:dns/promises';
import http from 'node:http';
import https from 'node:https';
import net from 'node:net';
import { syncBuiltinESMExports } from 'node:module';
import { randomBytes } from 'node:crypto';
import { mkdtempSync, writeFileSync, chmodSync, statSync, rmSync, mkdirSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { PassThrough } from 'node:stream';
import { isPublicIP, publicAddress, validatePublicURL, secureProviderFetch, createEgressProxy } from './egress.mjs';
import { hashToken, hashPassword, verifyPassword, openDatabase, safeUser, audit } from './database.mjs';
import { createApp } from './server.mjs';
import { createProviderService } from './providers.mjs';

// All credentials and databases in this file are disposable fixtures.
const requestHTTP = http.request;
const connectTCP = net.createConnection;

async function withDNS(lookup, run) {
  const original = dns.lookup;
  dns.lookup = lookup;
  syncBuiltinESMExports();
  try { return await run(); }
  finally { dns.lookup = original; syncBuiltinESMExports(); }
}

function databaseFixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'svolo-web-security-'));
  const keyFile = join(root, 'fixture.key');
  const dataDir = join(root, 'data');
  writeFileSync(keyFile, randomBytes(32).toString('hex'), { mode: 0o600 });
  const store = openDatabase(dataDir, keyFile);
  t.after(() => { store.db.close(); rmSync(root, { recursive: true, force: true }); });
  return { ...store, root, keyFile, dataDir };
}

function user(db, id, username, role = 'user') {
  db.prepare('INSERT INTO users(id,username,password_hash,role,created_at) VALUES(?,?,?,?,?)')
    .run(id, username, 'fixture-not-a-password-hash', role, new Date().toISOString());
  return db.prepare('SELECT * FROM users WHERE id=?').get(id);
}

function proxyRequest(proxyURL, target, headers = {}) {
  const proxy = new URL(proxyURL);
  return new Promise((resolve, reject) => {
    const req = requestHTTP({ hostname: proxy.hostname, port: proxy.port, method: 'GET', path: target, headers, timeout: 3000 }, res => {
      let body = '';
      res.setEncoding('utf8');
      res.on('data', chunk => { body += chunk; });
      res.on('end', () => resolve({ status: res.statusCode, body }));
    });
    req.on('timeout', () => req.destroy(new Error('Fixture proxy timeout')));
    req.on('error', reject);
    req.end();
  });
}

function proxyConnect(proxyURL, target, payload = '') {
  const proxy = new URL(proxyURL);
  return new Promise((resolve, reject) => {
    const socket = connectTCP({ host: proxy.hostname, port: Number(proxy.port) });
    let response = '';
    socket.setTimeout(3000, () => socket.destroy(new Error('Fixture CONNECT timeout')));
    socket.on('connect', () => socket.write(`CONNECT ${target} HTTP/1.1\r\nHost: ${target}\r\n\r\n${payload}`));
    socket.on('data', chunk => {
      response += chunk.toString();
      if ((response.startsWith('HTTP/1.1 403') && response.includes('\r\n\r\n')) || (payload && response.endsWith(payload))) {
        socket.destroy();
        resolve(response);
      }
    });
    socket.on('end', () => resolve(response));
    socket.on('error', reject);
  });
}

test('egress rejects loopback, LAN, metadata, carrier NAT and non-IP values', () => {
  for (const address of ['127.0.0.1', '127.255.255.255', '10.0.0.1', '172.16.0.1', '172.31.255.255', '192.168.1.1', '169.254.169.254', '100.64.0.1', '100.127.255.255', '0.0.0.0', '198.18.0.1', '224.0.0.1', '255.255.255.255', '::', '::1', 'fc00::1', 'fe80::1', '::ffff:127.0.0.1', '2002:7f00:1::', '2001:db8::1', 'not-an-ip']) {
    assert.equal(isPublicIP(address), false, address);
  }
  for (const address of ['1.1.1.1', '8.8.8.8', '172.15.1.1', '172.32.1.1', '100.63.1.1', '100.128.1.1', '2606:4700:4700::1111']) {
    assert.equal(isPublicIP(address), true, address);
  }
});

test('egress rejects documentation ranges and alternate IPv6 spelling', () => {
  for (const address of ['192.0.2.1', '198.51.100.1', '203.0.113.1', '2001:0db8::1']) {
    assert.equal(isPublicIP(address), false, address);
  }
});

test('DNS answers fail closed when any candidate is private or no addresses exist', async () => {
  await withDNS(async () => [{ address: '1.1.1.1' }, { address: '127.0.0.1' }], async () => {
    await assert.rejects(publicAddress('mixed.fixture.example'), /privati/);
  });
  await withDNS(async () => [], async () => {
    await assert.rejects(publicAddress('empty.fixture.example'), /privati/);
  });
  for (const host of ['localhost', 'service.localhost', 'printer.local', 'metadata.internal', '[::1]', '169.254.169.254']) {
    await assert.rejects(publicAddress(host), /privati/);
  }
});

test('provider endpoints require public HTTPS without embedded credentials, query or fragment', async () => {
  await withDNS(async () => [{ address: '1.1.1.1', family: 4 }], async () => {
    assert.equal((await validatePublicURL('https://provider.fixture.example/v1')).pathname, '/v1');
    for (const endpoint of ['http://provider.fixture.example/v1', 'https://dummy:dummy@provider.fixture.example/v1', 'https://provider.fixture.example/v1?token=dummy', 'https://provider.fixture.example/v1#fragment']) {
      await assert.rejects(validatePublicURL(endpoint), /HTTPS pubblico/);
    }
  });
  await assert.rejects(validatePublicURL('https://127.0.0.1/v1'), /privati/);
});

test('HTTP proxy denies localhost, host services, metadata and prohibited ports', async t => {
  const proxy = await createEgressProxy();
  t.after(() => proxy.close());
  for (const target of ['http://127.0.0.1/', 'http://localhost/', 'http://127.0.0.1:7331/v1/config', 'http://127.0.0.1:7340/', 'http://169.254.169.254/latest/meta-data/', 'http://10.0.0.1/', 'http://[::1]/', 'http://public.fixture.example:22/', 'https://public.fixture.example/', 'ftp://public.fixture.example/']) {
    assert.equal((await proxyRequest(proxy.url, target)).status, 403, target);
  }
});

test('HTTP proxy pins a single validated DNS answer and strips proxy credentials', async t => {
  const proxy = await createEgressProxy();
  t.after(() => proxy.close());
  let lookups = 0;
  let destination;
  const original = http.request;
  http.request = (options, callback) => {
    destination = options;
    const upstream = new PassThrough();
    const response = new PassThrough();
    response.statusCode = 200;
    response.headers = { 'content-type': 'text/plain' };
    queueMicrotask(() => { callback(response); response.end('fixture public page'); });
    return upstream;
  };
  try {
    await withDNS(async () => {
      lookups++;
      return [{ address: lookups === 1 ? '1.1.1.1' : '127.0.0.1', family: 4 }];
    }, async () => {
      const result = await proxyRequest(proxy.url, 'http://public.fixture.example/path?q=1', { 'Proxy-Authorization': 'Bearer dummy-fixture-only', 'Proxy-Connection': 'keep-alive', Host: 'wrong.fixture.example' });
      assert.equal(result.status, 200);
      assert.equal(result.body, 'fixture public page');
      assert.equal(lookups, 1);
      assert.equal(destination.hostname, '1.1.1.1');
      assert.equal(destination.headers.host, 'public.fixture.example');
      assert.equal(destination.path, '/path?q=1');
      assert.equal(destination.headers['proxy-authorization'], undefined);
      assert.equal(destination.headers['proxy-connection'], undefined);
    });
  } finally { http.request = original; }
});

test('CONNECT proxy denies loopback, metadata and ports outside HTTPS', async t => {
  const proxy = await createEgressProxy();
  t.after(() => proxy.close());
  for (const target of ['127.0.0.1:443', 'localhost:443', '169.254.169.254:443', '[::1]:443', 'public.fixture.example:22', '127.0.0.1:7340']) {
    assert.match(await proxyConnect(proxy.url, target), /^HTTP\/1\.1 403 /, target);
  }
});

test('CONNECT proxy pins DNS and preserves tunneled input without a second lookup', async t => {
  const proxy = await createEgressProxy();
  t.after(() => proxy.close());
  const original = net.connect;
  let destination;
  let lookups = 0;
  net.connect = options => {
    destination = options;
    const remote = new PassThrough();
    remote.setTimeout = () => remote;
    queueMicrotask(() => remote.emit('connect'));
    return remote;
  };
  try {
    await withDNS(async () => {
      lookups++;
      return [{ address: lookups === 1 ? '1.1.1.1' : '127.0.0.1', family: 4 }];
    }, async () => {
      const result = await proxyConnect(proxy.url, 'public.fixture.example:443', 'fixture-payload');
      assert.match(result, /^HTTP\/1\.1 200 Connection Established/);
      assert.ok(result.endsWith('fixture-payload'));
      assert.equal(lookups, 1);
      assert.deepEqual(destination, { host: '1.1.1.1', port: 443 });
    });
  } finally { net.connect = original; }
});

test('provider HTTP transport rejects private IPs and malformed ports before connecting', async () => {
  let connections = 0;
  const originalHTTPS = https.request, originalHTTP = http.request;
  https.request = http.request = () => { connections++; throw new Error('Unexpected fixture connection'); };
  try {
    for (const target of ['https://127.0.0.1/v1', 'https://169.254.169.254/v1', 'https://[::1]/v1', 'http://public.fixture.example/v1', 'https://public.fixture.example:65536/v1', 'https://public.fixture.example:-1/v1', 'https://dummy:dummy@public.fixture.example/v1']) {
      await assert.rejects(secureProviderFetch(target));
    }
    assert.equal(connections, 0);
  } finally { https.request = originalHTTPS; http.request = originalHTTP; }
});

test('provider transport pins each connection, preserves TLS hostname and blocks a later DNS rebind', async () => {
  const original = https.request;
  const connections = [];
  https.request = (options, callback) => {
    connections.push(options);
    const req = new PassThrough(), response = new PassThrough();
    response.statusCode = 200;
    response.headers = { 'content-type': 'application/json' };
    queueMicrotask(() => { callback(response); response.end('{"fixture":true}'); });
    return req;
  };
  try {
    let lookups = 0;
    await withDNS(async () => [{ address: ++lookups === 1 ? '1.1.1.1' : '127.0.0.1', family: 4 }], async () => {
      const result = await secureProviderFetch('https://provider.fixture.example/v1/models', { headers: { Authorization: 'Bearer dummy-fixture-only' } });
      assert.deepEqual(await result.json(), { fixture: true });
      assert.equal(lookups, 1);
      assert.equal(connections[0].hostname, '1.1.1.1');
      assert.equal(connections[0].servername, 'provider.fixture.example');
      assert.equal(connections[0].headers.Host, 'provider.fixture.example');
      await assert.rejects(secureProviderFetch('https://provider.fixture.example/v1/models'), /privati/);
      assert.equal(lookups, 2);
      assert.equal(connections.length, 1);
    });
  } finally { https.request = original; }
});

test('password hashing uses independent salts and rejects wrong credentials and invalid lengths', async () => {
  const password = 'Disposable-fixture-password';
  const first = await hashPassword(password);
  const second = await hashPassword(password);
  assert.notEqual(first, second);
  assert.ok(!first.includes(password));
  assert.equal(await verifyPassword(password, first), true);
  assert.equal(await verifyPassword('Wrong-disposable-fixture', first), false);
  assert.equal(await verifyPassword(null, first), false);
  assert.equal(await verifyPassword('x'.repeat(257), first), false);
  for (const invalid of [null, 'short', 'x'.repeat(257)]) await assert.rejects(hashPassword(invalid), /10 a 256/);
});

test('malformed password hashes fail closed without throwing', async () => {
  for (const encoded of ['', 'not-a-hash', 'scrypt$32768$8$1', 'scrypt$32768$8$1$not-hex$not-hex', 'scrypt$32768$8$1$' + '00'.repeat(16)]) {
    assert.equal(await verifyPassword('Disposable-fixture-password', encoded), false);
  }
});

test('database rejects group-readable and malformed encryption keys', t => {
  const fixture = databaseFixture(t);
  chmodSync(fixture.keyFile, 0o644);
  assert.throws(() => openDatabase(join(fixture.root, 'unsafe'), fixture.keyFile), /0600/);
  chmodSync(fixture.keyFile, 0o600);
  for (const key of ['ab'.repeat(31), 'not-a-key', 'ab'.repeat(32) + 'invalid-trailer']) {
    writeFileSync(fixture.keyFile, key);
    assert.throws(() => openDatabase(join(fixture.root, 'bad-key'), fixture.keyFile), /non valida/);
  }
});

test('AES-GCM credentials round-trip while wrong key, context and tampering fail', t => {
  const fixture = databaseFixture(t);
  const plaintext = 'dummy-credential-test-only-☃';
  const context = 'fixture-user:mcp-fixture';
  const encoded = fixture.encrypt(plaintext, context);
  assert.equal(fixture.decrypt(encoded, context), plaintext);
  assert.notEqual(encoded, fixture.encrypt(plaintext, context));
  assert.ok(!Buffer.from(encoded, 'base64').includes(Buffer.from(plaintext)));
  assert.throws(() => fixture.decrypt(encoded, 'another-user:mcp-fixture'));
  const altered = Buffer.from(encoded, 'base64');
  altered[altered.length - 1] ^= 1;
  assert.throws(() => fixture.decrypt(altered.toString('base64'), context));
  assert.throws(() => fixture.decrypt('short', context), /non valida/);
  const other = databaseFixture(t);
  assert.throws(() => other.decrypt(encoded, context));
  assert.equal(fixture.decrypt(fixture.encrypt('', context), context), '');
});

test('database usernames are unique regardless of ASCII case and relationships reject missing users', t => {
  const { db } = databaseFixture(t);
  user(db, 'a'.repeat(32), 'FixtureAlice');
  assert.throws(() => user(db, 'b'.repeat(32), 'fixturealice'), /UNIQUE/);
  assert.throws(() => db.prepare('INSERT INTO web_sessions VALUES(?,?,?,?,?)').run(hashToken('dummy'), 'missing', 'fixture-csrf', Date.now() + 60000, Date.now()), /FOREIGN KEY/);
  assert.throws(() => user(db, 'c'.repeat(32), 'FixtureInvalidRole', 'owner'), /CHECK/);
});

test('user-scoped MCP rows coexist and encrypted credentials survive database reopen', t => {
  const fixture = databaseFixture(t);
  user(fixture.db, 'a'.repeat(32), 'FixtureAlice');
  user(fixture.db, 'b'.repeat(32), 'FixtureBob');
  const encrypted = fixture.encrypt('dummy-mcp-credential', 'alice:mcp');
  const insert = fixture.db.prepare('INSERT INTO mcp_servers(user_id,id,config_json,credential_enc) VALUES(?,?,?,?)');
  insert.run('a'.repeat(32), 'same-server-id', '{"fixture":"alice"}', encrypted);
  insert.run('b'.repeat(32), 'same-server-id', '{"fixture":"bob"}', null);
  assert.throws(() => insert.run('a'.repeat(32), 'same-server-id', '{}', null), /UNIQUE/);
  assert.equal(fixture.db.prepare('SELECT config_json FROM mcp_servers WHERE user_id=?').get('b'.repeat(32)).config_json, '{"fixture":"bob"}');
  const reopened = openDatabase(fixture.dataDir, fixture.keyFile);
  try {
    assert.equal(reopened.decrypt(reopened.db.prepare('SELECT credential_enc FROM mcp_servers WHERE user_id=?').get('a'.repeat(32)).credential_enc, 'alice:mcp'), 'dummy-mcp-credential');
  } finally { reopened.db.close(); }
});

test('legacy core and MCP ciphertext migrate atomically to their user context', t => {
  const fixture=databaseFixture(t),id='a'.repeat(32);
  user(fixture.db,id,'MigrationFixture');
  fixture.db.prepare("DELETE FROM app_metadata WHERE key='tenant-crypto-v1'").run();
  fixture.db.prepare('INSERT INTO app_metadata(key,value) VALUES(?,?)').run('core-key:'+id,fixture.encrypt('dummy-core-key'));
  fixture.db.prepare('INSERT INTO mcp_servers(user_id,id,config_json,credential_enc) VALUES(?,?,?,?)').run(id,'legacy','{}',fixture.encrypt('dummy-mcp-key'));
  const reopened=openDatabase(fixture.dataDir,fixture.keyFile);
  try{
    const core=reopened.db.prepare('SELECT value FROM app_metadata WHERE key=?').get('core-key:'+id).value;
    const mcp=reopened.db.prepare('SELECT credential_enc FROM mcp_servers WHERE user_id=?').get(id).credential_enc;
    assert.equal(reopened.decrypt(core,id),'dummy-core-key');
    assert.equal(reopened.decrypt(mcp,id),'dummy-mcp-key');
    assert.throws(()=>reopened.decrypt(core));assert.throws(()=>reopened.decrypt(mcp,'b'.repeat(32)));
  }finally{reopened.db.close();}
  const again=openDatabase(fixture.dataDir,fixture.keyFile);
  try{assert.equal(again.decrypt(again.db.prepare('SELECT value FROM app_metadata WHERE key=?').get('core-key:'+id).value,id),'dummy-core-key');}finally{again.db.close();}
});

test('session storage hashes tokens and distinguishes stored expiry timestamps', t => {
  const fixture = databaseFixture(t);
  user(fixture.db, 'a'.repeat(32), 'FixtureAlice');
  const now = Date.now();
  const insert = fixture.db.prepare('INSERT INTO web_sessions VALUES(?,?,?,?,?)');
  insert.run(hashToken('expired-dummy-token'), 'a'.repeat(32), 'dummy-csrf-expired', now - 1, now - 1000);
  insert.run(hashToken('active-dummy-token'), 'a'.repeat(32), 'dummy-csrf-active', now + 60000, now);
  const rows = fixture.db.prepare('SELECT * FROM web_sessions').all();
  assert.ok(rows.every(row => /^[a-f0-9]{64}$/.test(row.token_hash)));
  assert.equal(rows.filter(row => row.expires_at > now).length, 1);
  assert.ok(!JSON.stringify(rows).includes('active-dummy-token'));
  assert.ok(!JSON.stringify(rows).includes('expired-dummy-token'));
  // HTTP enforcement of expiration is covered by the gateway integration tests.
});

test('safe users omit password hashes, database modes are private and audit records contain only the supplied event', t => {
  const fixture = databaseFixture(t);
  const row = user(fixture.db, 'a'.repeat(32), 'FixtureAlice');
  assert.deepEqual(safeUser(row), { id: row.id, username: row.username, role: 'user', disabled: false });
  assert.equal(statSync(join(fixture.dataDir, 'svolo.sqlite')).mode & 0o777, 0o600);
  assert.equal(statSync(fixture.dataDir).mode & 0o777, 0o700);
  audit(fixture.db, row.id, 'fixture.login');
  const event = fixture.db.prepare('SELECT user_id,action FROM audit_log').get();
  assert.equal(event.user_id, row.id);
  assert.equal(event.action, 'fixture.login');
});

async function gatewayFixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'svolo-gateway-security-'));
  const dataDir = join(root, 'data');
  const keyFile = join(root, 'fixture.key');
  const bootstrapFile = join(root, 'fixture-admin.json');
  const password = 'Disposable-gateway-fixture';
  writeFileSync(keyFile, randomBytes(32).toString('hex'), { mode: 0o600 });
  writeFileSync(bootstrapFile, JSON.stringify({ username: 'FixtureAdmin', passwordHash: await hashPassword(password) }), { mode: 0o600 });
  const store = openDatabase(dataDir, keyFile);
  const decoderRemnant=join(dataDir,'document-tmp','document-fixture');mkdirSync(decoderRemnant,{recursive:true,mode:0o700});writeFileSync(join(decoderRemnant,'input.txt'),'Disposable decoder crash remnant',{mode:0o600});
  let externalCalls = 0;
  const providers = createProviderService({ ...store, userStateDir: id => join(dataDir, 'users', id), fetchImpl: async () => { externalCalls++; throw new Error('External calls are forbidden in this fixture'); } });
  const calls = [];
  const cores = {
    cores: new Map(),
    async get(user) {
      let core = this.cores.get(user.id);
      if (!core) { core = { user, sessions: [], runs: [], brokerToken: randomBytes(32).toString('hex') }; this.cores.set(user.id, core); }
      return core;
    },
    async request(core, path, method = 'GET', payload) {
      calls.push({ userId: core.user.id, path, method, payload });
      const pathname = path.split('?')[0];
      if (pathname === '/v1/sessions' && method === 'GET') return core.sessions;
      if (pathname === '/v1/sessions' && method === 'POST') { const index=core.sessions.findIndex(s=>s.id===payload.id);if(index<0)core.sessions.push(payload);else core.sessions[index]=payload;return payload; }
      if (pathname === '/v1/sessions' && method === 'DELETE') { const id=new URL(path,'http://fixture').searchParams.get('session');core.sessions=core.sessions.filter(s=>s.id!==id);core.runs=core.runs.filter(r=>r.session!==id);return {deleted:true}; }
      if (pathname === '/v1/config') return { providers: [{ id: 'fixture', apiKeyEnv: 'HIDDEN_FIXTURE_ENV', apiKeyRef: 'hidden-fixture-ref', model: 'fixture-model' }], mcpServers: [{ id: 'fixture', tokenEnv: 'HIDDEN_FIXTURE_ENV', tokenRef: 'hidden-fixture-ref' }], sessions: core.sessions };
      if (pathname === '/v1/tools') return [{ name: 'state' }, { name: 'workspace-exec' }, { name: 'computer-use' }, { name: 'profile-import' }, { name: 'call-routine' }];
      if (pathname === '/v1/tools/call') {
        if (payload?.session && !core.sessions.some(session => session.id === payload.session)) throw Object.assign(new Error('Fixture session not found'), { status: 400 });
        return { userId: core.user.id, session: payload?.session };
      }
      if (pathname === '/v1/runs') {
        if (method === 'POST') { const run = { id: core.runs.length?'fixture-run-'+core.runs.length:'same-fixture-run-id', session: payload.session, status: 'completed', text: 'Fixture response' }; core.runs.push(run); return run; }
        return core.runs;
      }
      return { ok: true };
    },
    async sync() {},
    broker(userId, token) { const core = this.cores.get(userId); return core?.brokerToken === token ? core : null; },
    async close() {},
  };
  const publicOrigin = 'https://svolo.fixture.example';
  const importFile = process.env.SVOLO_INITIAL_IMPORT_FILE;
  delete process.env.SVOLO_INITIAL_IMPORT_FILE;
  let app;
  try { app = await createApp({ host: '127.0.0.1', port: 0, dataDir, keyFile, bootstrapFile, publicOrigin, providers, cores }); }
  finally { if (importFile !== undefined) process.env.SVOLO_INITIAL_IMPORT_FILE = importFile; }
  assert.equal(existsSync(decoderRemnant),false,'Startup clears private decoder remnants from an interrupted read');
  t.after(async () => { await app.close(); store.db.close(); rmSync(root, { recursive: true, force: true }); });
  const url = `http://127.0.0.1:${app.server.address().port}`;
  async function api(path, { auth, method = 'GET', input, raw, headers = {} } = {}) {
    return new Promise((resolve, reject) => {
      const req = requestHTTP(url + path, {
        method,
        headers: { Host: new URL(publicOrigin).host, ...(raw!==undefined?{'Content-Type':'application/octet-stream'}:input === undefined ? {} : { 'Content-Type': 'application/json' }), ...(auth ? { Cookie: auth.cookie, 'X-CSRF-Token': auth.csrf } : {}), ...headers },
        timeout: 5000,
      }, response => {
        let text = '';
        response.setEncoding('utf8');
        response.on('data', chunk => { text += chunk; });
        response.on('end', () => {
          let data;
          try { data = JSON.parse(text); } catch { data = text; }
          const responseHeaders = new Headers();
          for (const [key, value] of Object.entries(response.headers)) responseHeaders.set(key, Array.isArray(value) ? value.join(', ') : value);
          resolve({ status: response.statusCode, data, headers: responseHeaders });
        });
      });
      req.on('timeout', () => req.destroy(new Error('Fixture gateway timeout')));
      req.on('error', reject);
      req.end(raw??(input === undefined ? undefined : JSON.stringify(input)));
    });
  }
  async function login(username) {
    const result = await api('/api/login', { method: 'POST', input: { username, password } });
    assert.equal(result.status, 200);
    const setCookie = result.headers.get('set-cookie');
    assert.match(setCookie, /^__Host-svolo=[a-f0-9]{64};/);
    for (const flag of ['Path=/', 'HttpOnly', 'Secure', 'SameSite=Lax']) assert.ok(setCookie.includes(flag));
    return { user: result.data.user, csrf: result.data.csrf, cookie: setCookie.split(';')[0] };
  }
  const admin = await login('FixtureAdmin');
  for (const username of ['FixtureAlice', 'FixtureBob']) assert.equal((await api('/api/users', { auth: admin, method: 'POST', input: { username, password } })).status, 201);
  const alice = await login('FixtureAlice'), bob = await login('FixtureBob');
  return { app, api, login, admin, alice, bob, calls, cores, providers, keyFile, externalCalls: () => externalCalls };
}

test('gateway enforces login, CSRF, per-user state and forbidden operations over local HTTP', async t => {
  const fixture = await gatewayFixture(t);
  const { app, api, admin, alice, bob, calls } = fixture;

  await t.test('anonymous API access requires login while health and static entry are available', async () => {
    for (const path of ['/api/me', '/api/providers', '/api/mcp', '/api/sessions', '/api/users', '/api/core/v1/config']) assert.equal((await api(path)).status, 401, path);
    assert.equal((await api('/api/health')).status, 200);
    const entry = await api('/');
    assert.equal(entry.status, 200);
    assert.ok(entry.headers.get('content-security-policy').includes("frame-ancestors 'none'"));
    assert.equal(entry.headers.get('x-content-type-options'), 'nosniff');
  });

  await t.test('host and origin validation reject cross-origin requests', async () => {
    assert.equal((await api('/api/health', { headers: { Host: 'evil.fixture.example' } })).status, 403);
    assert.equal((await api('/api/me', { auth: alice, headers: { Origin: 'https://evil.fixture.example' } })).status, 403);
    assert.equal((await api('/api/me', { auth: alice, headers: { Origin: 'https://svolo.fixture.example' } })).status, 200);
  });

  await t.test('write operations reject missing or another user CSRF token', async () => {
    assert.equal((await api('/api/logout', { auth: alice, method: 'POST', headers: { 'X-CSRF-Token': '' } })).status, 403);
    assert.equal((await api('/api/logout', { auth: alice, method: 'POST', headers: { 'X-CSRF-Token': bob.csrf } })).status, 403);
    assert.equal((await api('/api/me', { auth: alice })).status, 200);
  });

  await t.test('GET and HEAD cannot perform logout, model selection or tool calls', async () => {
    for (const method of ['GET', 'HEAD']) {
      for (const path of ['/api/logout', '/api/providers/select']) assert.equal((await api(path, { auth: alice, method })).status, 404, `${method} ${path}`);
      assert.equal((await api('/api/core/v1/tools/call', { auth: alice, method })).status, 403);
    }
    assert.equal((await api('/api/me', { auth: alice })).status, 200);
  });

  await t.test('invalid cookies, bearer-only requests and invalid login payloads fail closed', async () => {
    for (const Cookie of ['__Host-svolo=not-a-token', '__Host-svolo=' + bob.csrf]) assert.equal((await api('/api/me', { headers: { Cookie } })).status, 401);
    assert.equal((await api('/api/me', { headers: { Authorization: 'Bearer ' + alice.cookie.split('=')[1] } })).status, 401);
    assert.equal((await api('/api/login', { method: 'POST', input: { username: 'FixtureAlice', password: 'Wrong-disposable-fixture' } })).status, 401);
    assert.equal((await api('/api/login', { method: 'POST', input: { username: 'UnknownFixture', password: 'Disposable-gateway-fixture' } })).status, 401);
    assert.equal((await api('/api/login', { method: 'POST', input: [], headers: { 'Content-Type': 'text/plain' } })).status, 415);
    assert.equal((await api('/api/login', { method: 'POST', input: { padding: 'x'.repeat(5000) } })).status, 413);
    const row = app.db.prepare('SELECT expires_at,created_at FROM web_sessions WHERE token_hash=?').get(hashToken(alice.cookie.split('=')[1]));
    assert.equal(row.expires_at - row.created_at, 12 * 3600000);
  });

  await t.test('ordinary users cannot manage users and administrator cannot create case duplicates', async () => {
    assert.equal((await api('/api/users', { auth: alice })).status, 403);
    assert.equal((await api('/api/users', { auth: bob, method: 'POST', input: { username: 'ElevatedFixture', password: 'Disposable-gateway-fixture', role: 'admin' } })).status, 403);
    assert.equal((await api('/api/users', { auth: admin, method: 'POST', input: { username: 'fixturealice', password: 'Disposable-gateway-fixture' } })).status, 409);
    const users = await api('/api/users', { auth: admin });
    assert.equal(users.status, 200);
    assert.ok(users.data.users.every(user => !('password_hash' in user)));
  });

  await t.test('provider configuration and preferences stay with their authenticated user', async () => {
    await withDNS(async () => [{ address: '1.1.1.1', family: 4 }], async () => {
      const save = await api('/api/providers', { auth: alice, method: 'POST', input: { id: 'fixture-provider', kind: 'openai-compatible', model: 'fixture-alice-model', baseURL: 'https://provider.fixture.example/v1', apiKey: 'dummy-api-key-fixture-only' } });
      assert.equal(save.status, 200);
      assert.ok(!JSON.stringify(save.data).includes('dummy-api-key-fixture-only'));
      assert.equal(save.data.providers[0].configured, true);
      assert.equal((await api('/api/providers', { auth: bob })).data.providers.length, 0);
      assert.equal((await api('/api/providers/select', { auth: bob, method: 'POST', input: { providerId: 'fixture-provider', model: 'fixture-alice-model' } })).status, 404);
      assert.equal((await api('/api/providers', { auth: bob, method: 'POST', input: { id: 'fixture-provider', kind: 'openai-compatible', model: 'fixture-bob-model', baseURL: 'https://provider.fixture.example/v1', apiKey: 'dummy-bob-key-fixture-only' } })).status, 200);
      assert.equal((await api('/api/providers', { auth: alice })).data.providers[0].model, 'fixture-alice-model');
      assert.equal((await api('/api/providers', { auth: bob })).data.providers[0].model, 'fixture-bob-model');
    });
  });

  await t.test('new provider destinations reject localhost and metadata for all roles', async () => {
    for (const baseURL of ['https://127.0.0.1/v1', 'https://169.254.169.254/v1', 'http://localhost/v1']) {
      for(const auth of [admin,alice]){
        const result = await api('/api/providers', { auth, method: 'POST', input: { id: 'blocked-fixture', model: 'fixture', baseURL } });
        assert.ok([400, 403].includes(result.status), `Expected client denial for ${baseURL}, received ${result.status}`);
      }
    }
  });

  await t.test('MCP disallows host processes for admin and users and rejects private destinations', async () => {
    for (const auth of [admin, alice, bob]) {
      assert.equal((await api('/api/mcp', { auth, method: 'POST', input: { id: 'fixture-stdio', command: '/bin/sh', args: ['-c', 'true'] } })).status, 400);
    }
    for (const url of ['https://127.0.0.1/mcp', 'https://169.254.169.254/mcp']) {
      const result = await api('/api/mcp', { auth: alice, method: 'POST', input: { id: 'fixture-private', url } });
      assert.ok([400, 403].includes(result.status), `Expected client denial, received ${result.status}`);
    }
  });

  await t.test('MCP credentials stay encrypted and the same server ID is user scoped', async () => {
    await withDNS(async () => [{ address: '1.1.1.1', family: 4 }], async () => {
      for (const [auth, suffix] of [[alice, 'alice'], [bob, 'bob']]) {
        const saved = await api('/api/mcp', { auth, method: 'POST', input: { id: 'shared-fixture-id', url: `https://mcp.fixture.example/${suffix}`, token: `dummy-${suffix}-mcp-secret` } });
        assert.equal(saved.status, 200);
        assert.ok(!JSON.stringify(saved.data).includes(`dummy-${suffix}-mcp-secret`));
      }
    });
    const listAlice = await api('/api/mcp', { auth: alice }), listBob = await api('/api/mcp', { auth: bob });
    assert.ok(listAlice.data.servers[0].url.endsWith('/alice'));
    assert.ok(listBob.data.servers[0].url.endsWith('/bob'));
    assert.equal(app.db.prepare('SELECT count(*) AS n FROM mcp_servers WHERE credential_enc LIKE ?').get('%dummy-%').n, 0);
    assert.equal((await api('/api/mcp?id=shared-fixture-id', { auth: bob, method: 'DELETE' })).status, 200);
    assert.equal((await api('/api/mcp', { auth: alice })).data.servers.length, 1);
  });

  let aliceSession;
  await t.test('chat sessions route to the authenticated core and ignore supplied workspace or execution permissions', async () => {
    const created = await api('/api/sessions', { auth: alice, method: 'POST', input: { id: 'victim-session', name: 'Fixture chat', workspace: '/private-workspace-fixture', allowExec: true } });
    assert.equal(created.status, 201);
    aliceSession = created.data.id;
    assert.notEqual(aliceSession, 'victim-session');
    assert.equal(created.data.workspace, '/workspace');
    assert.equal(created.data.allowExec, false);
    assert.equal((await api('/api/sessions', { auth: alice })).data.length, 1);
    assert.equal((await api('/api/sessions', { auth: bob })).data.length, 0);
    assert.equal((await api('/api/core/v1/tools/call', { auth: bob, method: 'POST', input: { name: 'state', session: aliceSession, arguments: {} } })).status, 400);
    assert.equal(calls.at(-1).userId, bob.user.id);
  });

  await t.test('chat questions survive reload and the same run ID cannot mix two users', async () => {
    const alicePrompt='Private Alice fixture question';
    const bobPrompt='Private Bob fixture question';
    const bobSession=(await api('/api/sessions',{auth:bob,method:'POST',input:{name:'Bob fixture chat'}})).data.id;
    for (const [auth,prompt,session] of [[alice,alicePrompt,aliceSession],[bob,bobPrompt,bobSession]]) {
      const created=await api('/api/core/v1/runs',{auth,method:'POST',input:{session,provider:'fixture-provider',prompt,maxSteps:1}});
      assert.equal(created.status,200);
    }
    const reauthenticated=await fixture.login('FixtureAlice');
    const aliceRuns=await api('/api/core/v1/runs',{auth:reauthenticated});
    const bobRuns=await api('/api/core/v1/runs',{auth:bob});
    assert.equal(aliceRuns.data[0].prompt,alicePrompt);
    assert.equal(bobRuns.data[0].prompt,bobPrompt);
    assert.ok(!JSON.stringify(aliceRuns.data).includes(bobPrompt));
    const encrypted=app.db.prepare('SELECT prompt_enc FROM user_run_prompts').all();
    assert.ok(encrypted.every(row=>!row.prompt_enc.includes('Private')));
    const reopened=openDatabase(app.dataDir,fixture.keyFile);
    try{assert.equal(reopened.decrypt(reopened.db.prepare('SELECT prompt_enc FROM user_run_prompts WHERE user_id=?').get(alice.user.id).prompt_enc,alice.user.id),alicePrompt);}finally{reopened.db.close();}
  });

  await t.test('attachments and reviewed task learning are tenant-bound across HTTP and internal MCP',async()=>{
    const chat=(await api('/api/sessions',{auth:alice,method:'POST',input:{name:'Document fixture'}})).data.id;
    const created=await api('/api/task-profiles',{auth:alice,method:'POST',input:{name:'Fixture workflow',goal:'Complete the requested workflow',instructions:'Use only supplied values.',knowledge:'Verify the current form.',reviewed:true,uploadOrigins:['https://portal.example']}});assert.equal(created.status,201);const profile=created.data;
    assert.equal((await api('/api/task-profiles?id='+profile.id,{auth:bob,method:'PATCH',input:{...profile,reviewed:true}})).status,404);
    assert.equal((await api('/api/task-selection?session='+chat,{auth:alice,method:'POST',input:{profileId:profile.id}})).status,200);
    assert.equal((await api('/api/attachments?session='+chat+'&name=fixture.txt',{auth:alice,method:'POST',raw:Buffer.from('PRIVATE_DOCUMENT_FIXTURE')})).status,201);
    const files=(await api('/api/attachments?session='+chat,{auth:alice})).data.attachments;assert.equal(files.length,1);const doc=files[0];
    assert.equal((await api('/api/attachments/text?id='+doc.id,{auth:bob})).status,404);
    assert.equal((await api('/api/attachments?session='+chat+'&name=bad.txt',{auth:alice,method:'POST',raw:Buffer.from('fixture'),headers:{'X-CSRF-Token':'wrong'}})).status,403);
    assert.equal((await api('/api/attachments?session='+chat+'&name=bad.html',{auth:alice,method:'POST',raw:Buffer.from('fixture')})).status,415);
    assert.equal((await api('/api/core/v1/runs',{auth:bob,method:'POST',input:{session:chat,prompt:'Cross-user',provider:'fixture',attachments:[doc.id]}})).status,404);
    const started=await api('/api/core/v1/runs',{auth:alice,method:'POST',input:{session:chat,prompt:'Read this selected document',provider:'fixture',attachments:[doc.id]}});assert.equal(started.status,200);assert.equal(started.data.attachments[0].id,doc.id);
    const forwarded=calls.findLast(c=>c.userId===alice.user.id&&c.path==='/v1/runs'&&c.method==='POST').payload;assert.match(forwarded.prompt,/PRIVATE_DOCUMENT_FIXTURE/);assert.deepEqual(forwarded.taskOrigins,['https://portal.example']);assert.equal(forwarded.uploadFiles.length,1);assert.ok(!forwarded.allowedTools.includes('upload'));assert.equal(forwarded.continue,false);
    const fetched=(await api('/api/core/v1/runs',{auth:alice})).data.find(r=>r.id===started.data.id);assert.equal(fetched.prompt,'Read this selected document');
    const core=fixture.cores.cores.get(alice.user.id),run=core.runs.find(r=>r.id===started.data.id);run.status='running';
    assert.equal((await api('/api/task-profiles?id='+profile.id,{auth:alice,method:'PATCH',input:{...profile,reviewed:true}})).status,409);
    assert.equal((await api('/api/task-profiles?id='+profile.id,{auth:alice,method:'DELETE'})).status,409);
    const internal=async(authToken,params,trusted=false)=>{return new Promise((resolve,reject)=>{const req=requestHTTP(`http://127.0.0.1:${app.server.address().port}`+'/internal/tasks/'+alice.user.id,{method:'POST',headers:{Host:trusted?`127.0.0.1:${app.server.address().port}`:new URL(fixture.publicOrigin??'https://svolo.fixture.example').host,Authorization:'Bearer '+authToken,'Content-Type':'application/json'}},res=>{let text='';res.on('data',b=>text+=b);res.on('end',()=>resolve({status:res.statusCode,data:JSON.parse(text)}));});req.on('error',reject);req.end(JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/call',params}));});};
    // Public Host cannot turn the internal broker into an authenticated browser API.
    assert.equal((await internal(core.brokerToken,{name:'propose_knowledge',arguments:{session:chat,knowledge:'General procedure',reason:'Fixture'}})).status,403);
    const proposed=await internal(core.brokerToken,{name:'propose_knowledge',arguments:{session:chat,knowledge:'Read the offer label, enter supplied values and verify the receipt.',reason:'Fixture verified steps'}},true);assert.equal(proposed.status,200);assert.equal(proposed.data.result.isError,undefined);const proposalId=JSON.parse(proposed.data.result.content[0].text).id;
    assert.equal((await api('/api/task-profiles',{auth:alice})).data.profiles.find(p=>p.id===profile.id).revision,1);
    assert.equal((await api('/api/task-profiles/proposals?id='+proposalId,{auth:bob,method:'POST',input:{approve:true,reviewed:true}})).status,404);
    assert.equal((await api('/api/task-profiles/proposals?id='+proposalId,{auth:alice,method:'POST',input:{approve:true}})).status,400);
    assert.equal((await api('/api/task-profiles/proposals?id='+proposalId,{auth:alice,method:'POST',input:{approve:true,reviewed:true}})).status,200);
    const selectedRead=await internal(core.brokerToken,{name:'read_document',arguments:{session:chat,id:doc.id}},true);assert.match(selectedRead.data.result.content[0].text,/PRIVATE_DOCUMENT_FIXTURE/);
    const wrongRead=await internal(core.brokerToken,{name:'read_document',arguments:{session:chat,id:'not-selected'}},true);assert.equal(wrongRead.data.result.isError,true);

    assert.equal((await api('/api/task-selection?session='+chat,{auth:alice,method:'POST',input:{profileId:''}})).status,409);
    assert.equal((await api('/api/attachments?session='+chat+'&id='+doc.id,{auth:alice,method:'DELETE'})).status,409);run.status='completed';
    assert.equal((await api('/api/task-profiles/proposals',{auth:alice})).data.proposals.length,0);
    assert.ok(app.db.prepare('SELECT data_enc FROM chat_attachments').all().every(r=>!r.data_enc.includes('PRIVATE_DOCUMENT_FIXTURE')));
    assert.equal((await api('/api/sessions?id='+chat,{auth:alice,method:'DELETE'})).status,200);assert.equal((await api('/api/attachments/text?id='+doc.id,{auth:alice})).status,404);
  });
  await t.test('privileged core routes and restricted tools are denied for every account role', async () => {
    for (const auth of [admin, alice, bob]) {
      for (const [method, path] of [['GET', '/v1/tokens'], ['GET', '/v1/credentials'], ['GET', '/v1/hosts'], ['POST', '/v1/config'], ['POST', '/v1/credentials'], ['POST', '/v1/runtime/start'], ['PUT', '/v1/config']]) {
        assert.equal((await api('/api/core' + path, { auth, method, ...(method === 'GET' ? {} : { input: {} }) })).status, 403, `${method} ${path}`);
      }
      for (const name of ['workspace-exec', 'computer-use', 'profile-import', 'call-routine', 'pi-computer', 'pi-kanban', 'pi-atp', 'pi-lament']) {
        assert.equal((await api('/api/core/v1/tools/call', { auth, method: 'POST', input: { name, session: aliceSession, arguments: {} } })).status, 403, name);
        assert.equal((await api('/api/core/v1/runs', { auth, method: 'POST', input: { session: aliceSession,provider:'fixture-provider',prompt:'Unexecuted fixture request',allowedTools:[name] } })).status,403,name+' agent');
      }
    }
    const config = await api('/api/core/v1/config', { auth: alice });
    assert.equal(config.status, 200);
    assert.ok(!JSON.stringify(config.data).includes('hidden-fixture-ref'));
    assert.ok(!JSON.stringify(config.data).includes('HIDDEN_FIXTURE_ENV'));
    const tools = await api('/api/core/v1/tools', { auth: alice });
    assert.deepEqual(tools.data, [{ name: 'state' }]);
    const rejectedScope=await api('/api/core/v1/runs',{auth:alice,method:'POST',input:{session:aliceSession,provider:'fixture-provider',prompt:'Scope fixture',toolScope:['workspace-exec']}});
    assert.equal(rejectedScope.status,403);
    const scopedRun=await api('/api/core/v1/runs',{auth:alice,method:'POST',input:{session:aliceSession,provider:'fixture-provider',prompt:'Scope fixture',toolScope:['state']}});
    assert.equal(scopedRun.status,200);
    const forwarded=calls.findLast(call=>call.path==='/v1/runs'&&call.method==='POST');
    assert.deepEqual(forwarded.payload.toolScope,['state']);
  });

  await t.test('a disconnected browser viewer cancels its upstream observation', async () => {
    const original=fixture.cores.request;
    let enteredResolve,cancelledResolve;
    const entered=new Promise(resolve=>enteredResolve=resolve);
    const cancelled=new Promise(resolve=>cancelledResolve=resolve);
    fixture.cores.request=async function(core,path,method,payload,signal){
      if(path.startsWith('/v1/view')){
        enteredResolve();
        return await new Promise((resolve,reject)=>signal.addEventListener('abort',()=>{cancelledResolve();reject(signal.reason);},{once:true}));
      }
      return original.call(this,core,path,method,payload,signal);
    };
    let req;
    try{
      req=requestHTTP(`http://127.0.0.1:${app.server.address().port}/api/core/v1/view?session=${aliceSession}`,{headers:{Host:`127.0.0.1:${app.server.address().port}`,Cookie:alice.cookie}},response=>response.resume());
      req.on('error',()=>{});req.end();
      await Promise.race([entered,new Promise((_,reject)=>setTimeout(()=>reject(new Error('Viewer did not enter core')),2000))]);
      req.destroy();
      await Promise.race([cancelled,new Promise((_,reject)=>setTimeout(()=>reject(new Error('Viewer upstream did not abort')),2000))]);
      assert.equal((await api('/api/health')).status,200);
    }finally{req?.destroy();fixture.cores.request=original;}
  });

  await t.test('internal provider broker refuses public-host access and ordinary session tokens', async () => {
    const path = `/internal/provider/${alice.user.id}/fixture-provider/v1/chat/completions`;
    assert.equal((await api(path, { auth: alice, method: 'POST', input: { model: 'fixture-alice-model', messages: [] } })).status, 403);
    assert.equal((await api(path, { method: 'POST', input: { model: 'fixture-alice-model', messages: [] }, headers: { Host: `127.0.0.1:${app.server.address().port}`, Authorization: 'Bearer ' + alice.cookie.split('=')[1] } })).status, 401);
  });

  await t.test('a disconnected provider stream closes only its request and keeps the gateway alive', async () => {
    const saved=fixture.providers.handleCompletion;
    fixture.providers.handleCompletion=async()=>new Response(new ReadableStream({start(controller){controller.enqueue(new TextEncoder().encode('data: fixture\n\n'));setTimeout(()=>controller.error(new Error('Disposable provider disconnected')),15);}}),{headers:{'Content-Type':'text/event-stream'}});
    try{
      const outcome=await new Promise((resolve,reject)=>{
        const path=`http://127.0.0.1:${app.server.address().port}/internal/provider/${alice.user.id}/fixture-provider/v1/chat/completions`;
        const req=requestHTTP(path,{method:'POST',headers:{Host:`127.0.0.1:${app.server.address().port}`,'Content-Type':'application/json',Authorization:'Bearer '+fixture.cores.cores.get(alice.user.id).brokerToken}},response=>{
          response.resume();response.once('aborted',()=>resolve('aborted'));response.once('error',()=>resolve('aborted'));response.once('end',()=>resolve('ended'));
        });
        req.setTimeout(2000,()=>{req.destroy();reject(new Error('Stream fixture timeout'));});req.once('error',()=>resolve('aborted'));req.end(JSON.stringify({model:'fixture-alice-model',messages:[{role:'user',content:'Disposable request'}]}));
      });
      assert.equal(outcome,'aborted');assert.equal((await api('/api/health')).status,200);
    }finally{fixture.providers.handleCompletion=saved;}
  });

  await t.test('static requests do not expose repository modules, private keys or path traversal', async () => {
    for (const path of ['/database.mjs', '/security.test.mjs', '/web-master.key', '/brand/%2e%2e%2fLICENSE.md']) assert.equal((await api(path)).status, 404, path);
  });

  await t.test('expired and disabled-user sessions stop authenticating immediately', async () => {
    app.db.prepare('UPDATE users SET disabled=1 WHERE id=?').run(bob.user.id);
    assert.equal((await api('/api/me', { auth: bob })).status, 401);
    app.db.prepare('UPDATE users SET disabled=0 WHERE id=?').run(bob.user.id);
    assert.equal((await api('/api/me', { auth: bob })).status, 200);
    app.db.prepare('UPDATE web_sessions SET expires_at=? WHERE token_hash=?').run(Date.now() - 1, hashToken(bob.cookie.split('=')[1]));
    assert.equal((await api('/api/me', { auth: bob })).status, 401);
  });

  await t.test('logout revokes only the current user session and clears the secure cookie', async () => {
    const result = await api('/api/logout', { auth: alice, method: 'POST' });
    assert.equal(result.status, 200);
    assert.ok(result.headers.get('set-cookie').includes('Max-Age=0'));
    assert.equal((await api('/api/me', { auth: alice })).status, 401);
    assert.equal((await api('/api/me', { auth: admin })).status, 200);
  });

  await t.test('repeated invalid logins are rate limited without exposing credential details', async () => {
    let limited = false;
    for (let attempt = 0; attempt < 21; attempt++) {
      const result = await api('/api/login', { method: 'POST', input: { username: 'UnknownRateFixture', password: 'Wrong-disposable-fixture' } });
      assert.ok([401, 429].includes(result.status));
      assert.ok(!JSON.stringify(result.data).includes('Wrong-disposable-fixture'));
      if (result.status === 429) { limited = true; break; }
    }
    assert.equal(limited, true);
  });

  assert.equal(fixture.externalCalls(), 0, 'Gateway security tests must never contact an external model or quota service');
});


test('chat rename, pin and delete enforce tenant ownership, CSRF and retained state', async t=>{
 const {app,api,alice,bob,cores}=await gatewayFixture(t);
 const created=await api('/api/sessions',{auth:alice,method:'POST',input:{name:'Original'}});const id=created.data.id;
 const url='/api/sessions?id='+encodeURIComponent(id);
 for(const method of ['PATCH','DELETE'])assert.equal((await api(url,{auth:bob,method,input:method==='PATCH'?{name:'Stolen'}:undefined})).status,404);
 assert.equal((await api(url,{auth:alice,method:'PATCH',input:{name:'Stolen'},headers:{'X-CSRF-Token':'invalid'}})).status,403);
 for(const input of [{name:''},{name:'x'.repeat(101)},{pinned:'true'},{workspace:'/private'},{}])assert.equal((await api(url,{auth:alice,method:'PATCH',input})).status,400);
 const updated=await api(url,{auth:alice,method:'PATCH',input:{name:' Renamed ',pinned:true}});assert.equal(updated.status,200);assert.equal(updated.data.name,'Renamed');assert.equal(updated.data.pinned,true);
 const other=await api('/api/sessions',{auth:alice,method:'POST',input:{name:'Other'}});
 assert.equal((await api('/api/sessions',{auth:alice})).data[0].id,id);
 assert.equal(app.db.prepare('SELECT pinned FROM chat_preferences WHERE user_id=? AND session_id=?').get(alice.user.id,id).pinned,1);
 const core=await cores.get(alice.user);core.runs.push({id:'delete-fixture',session:id,status:'running'});
 assert.equal((await api(url,{auth:alice,method:'DELETE'})).status,409);
 core.runs[0].status='completed';app.db.prepare('INSERT INTO user_run_prompts VALUES(?,?,?,?)').run(alice.user.id,'delete-fixture',id,'disposable-encrypted-fixture');
 assert.equal((await api(url,{auth:alice,method:'DELETE'})).status,200);
 assert.deepEqual((await api('/api/sessions',{auth:alice})).data.map(s=>s.id),[other.data.id]);
 assert.equal(app.db.prepare('SELECT count(*) AS n FROM user_run_prompts WHERE user_id=? AND session_id=?').get(alice.user.id,id).n,0);
 assert.equal(app.db.prepare('SELECT count(*) AS n FROM chat_preferences WHERE user_id=? AND session_id=?').get(alice.user.id,id).n,0);
 assert.equal((await api(url,{auth:alice,method:'DELETE'})).status,404);
});
