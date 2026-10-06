import assert from 'node:assert/strict';
import { spawn, execFileSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { dirname, join, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const publicRoot = join(dirname(fileURLToPath(import.meta.url)), 'public');
function executable(name) {
  try { return execFileSync('which', [name], { encoding:'utf8', stdio:['ignore','pipe','ignore'] }).trim() || null; }
  catch { return null; }
}
function chromiumPath() {
  if (process.env.SVOLO_CHROMIUM) return existsSync(process.env.SVOLO_CHROMIUM) ? process.env.SVOLO_CHROMIUM : null;
  return executable('chromium');
}
const electronPath = process.env.SVOLO_TEST_ELECTRON || resolve(publicRoot, '../../desktop/node_modules/electron/dist/electron');
const xvfb = executable('xvfb-run');
const electron = existsSync(electronPath) && xvfb ? electronPath : null;
const chromium = chromiumPath();
export const browserAvailable = electron || chromium;
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));

async function connectCDP(url) {
  const socket = new WebSocket(url), pending = new Map();
  await new Promise((resolve, reject) => {
    socket.addEventListener('open', resolve, { once:true });
    socket.addEventListener('error', () => reject(new Error('Test Chromium CDP connection failed')), { once:true });
  });
  let sequence = 0;
  socket.addEventListener('message', event => {
    const message = JSON.parse(String(event.data)), request = pending.get(message.id);
    if (!request) return;
    pending.delete(message.id); clearTimeout(request.timer);
    message.error ? request.reject(new Error(message.error.message)) : request.resolve(message.result);
  });
  socket.addEventListener('close', () => {
    for (const request of pending.values()) { clearTimeout(request.timer); request.reject(new Error('Test Chromium CDP closed')); }
    pending.clear();
  });
  return {
    close:() => socket.close(),
    call(method, params = {}) {
      return new Promise((resolve, reject) => {
        const id = ++sequence;
        const timer = setTimeout(() => { pending.delete(id); reject(new Error(`Test Chromium timed out: ${method}`)); }, 8000);
        pending.set(id, { resolve,reject,timer });
        socket.send(JSON.stringify({ id,method,params }));
      });
    },
  };
}

export async function nativeBrowser(t, { handler, ready='globalThis.markdownReady===true' } = {}) {
  const profile = await mkdtemp(join(tmpdir(), 'svolo-markdown-test-'));
  let server, browser, client;
  t.after(async () => {
    client?.close();
    if (browser && browser.exitCode === null && browser.signalCode === null) {
      try { process.kill(-browser.pid, 'SIGTERM'); } catch {}
      await Promise.race([new Promise(resolve => browser.once('exit', resolve)), delay(1000)]);
      if (browser.exitCode === null && browser.signalCode === null) try { process.kill(-browser.pid, 'SIGKILL'); } catch {}
    }
    if (server) { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }
    await rm(profile, { recursive:true, force:true });
  });
  const requests = [];
  server = createServer(async (request, response) => {
    if(handler && await handler(request,response))return;
    const pathname = new URL(request.url, 'http://localhost').pathname;
    requests.push(pathname);
    if (pathname === '/') {
      response.writeHead(200, { 'Content-Type':'text/html; charset=utf-8' });
      // No CSP: sanitizer failures must be visible rather than masked by CSP.
      response.end('<!doctype html><meta name="viewport" content="width=device-width, initial-scale=1"><div id="output" class="message-body markdown-body"></div><script type="module">try { const { renderMarkdown } = await import("/markdown.js"); globalThis.renderMarkdown=renderMarkdown;globalThis.markdownReady=true; } catch(error) { globalThis.markdownLoadError=String(error); }</script>');
      return;
    }
    const target = resolve(publicRoot, '.' + pathname);
    if (!target.startsWith(publicRoot + sep)) { response.writeHead(404); response.end(); return; }
    try {
      const data = await readFile(target);
      response.writeHead(200, { 'Content-Type':pathname.endsWith('.js') || pathname.endsWith('.mjs') ? 'text/javascript; charset=utf-8' : 'text/plain; charset=utf-8' });
      response.end(data);
    } catch { response.writeHead(404); response.end('Fixture not found'); }
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const origin = `http://127.0.0.1:${server.address().port}`;
  let command = chromium, args = ['--headless=new','--remote-debugging-address=127.0.0.1','--remote-debugging-port=0',`--user-data-dir=${profile}`,'--no-first-run','--no-default-browser-check','--disable-background-networking','--disable-sync','--disable-component-update','--disable-dev-shm-usage',origin];
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => /^(PATH|HOME|USER|LOGNAME|LANG|LANGUAGE|TZ|DISPLAY|XAUTHORITY|XDG_RUNTIME_DIR|DBUS_SESSION_BUS_ADDRESS|TMPDIR|TMP|TEMP)$/.test(key) || key.startsWith('LC_')));
  if (electron) {
    const helper = join(profile, 'markdown-browser.cjs');
    await writeFile(helper, `const {app,BrowserWindow}=require('electron');app.setPath('userData',${JSON.stringify(profile)});app.commandLine.appendSwitch('user-data-dir',${JSON.stringify(profile)});app.commandLine.appendSwitch('remote-debugging-address','127.0.0.1');app.commandLine.appendSwitch('remote-debugging-port','0');app.commandLine.appendSwitch('disable-extensions');app.whenReady().then(()=>{const window=new BrowserWindow({width:1280,height:800,show:true,webPreferences:{sandbox:true,nodeIntegration:false,contextIsolation:true}});window.loadURL(${JSON.stringify(origin)});});process.on('SIGTERM',()=>app.quit());app.on('window-all-closed',()=>app.quit());`);
    command = xvfb; args = ['-a', electron, helper];
  }
  browser = spawn(command, args, { stdio:['ignore','ignore','pipe'], env, detached:true });
  let launchError = '', spawnError;
  browser.stderr.on('data', data => { launchError = (launchError + data.toString()).slice(-4096); });
  browser.on('error', error => { spawnError = error; });
  let port;
  const deadline = Date.now() + 10000;
  while (Date.now() < deadline) {
    if (spawnError) throw spawnError;
    if (browser.exitCode !== null || browser.signalCode !== null) throw new Error(`Test Chromium exited before startup: ${launchError}`);
    try { port = Number((await readFile(join(profile, 'DevToolsActivePort'), 'utf8')).split('\n')[0]); } catch {}
    port ||= Number(launchError.match(/DevTools listening on ws:\/\/127\.0\.0\.1:(\d+)\//)?.[1]);
    if (port) break;
    await delay(20);
  }
  assert.ok(port, 'Test Chromium should start with its standard sandbox');
  const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
  const target = targets.find(target => target.type === 'page');
  assert.ok(target, 'Test Chromium should expose its fixture page');
  client = await connectCDP(target.webSocketDebuggerUrl);
  const evaluate = async expression => {
    const result = await client.call('Runtime.evaluate', { expression, awaitPromise:true, returnByValue:true });
    assert.equal(result.exceptionDetails, undefined, result.exceptionDetails?.text);
    return result.result.value;
  };
  const moduleDeadline = Date.now() + 10000;
  while (Date.now() < moduleDeadline) { if (await evaluate(`(${ready}) || !!globalThis.markdownLoadError`)) break; await delay(20); }
  const readiness = await evaluate(`({ready:(${ready}),error:globalThis.markdownLoadError,url:location.href})`);
  assert.equal(readiness.ready, true, `Actual Markdown and sanitizer modules should load: ${JSON.stringify({ ...readiness, requests })}`);
  return { evaluate, requests, origin, client };
}
