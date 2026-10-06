import http from 'node:http';
import https from 'node:https';
import net from 'node:net';
import { lookup } from 'node:dns/promises';
import { Readable } from 'node:stream';

export function isPublicIP(address) {
  const ip=address.toLowerCase();
  if(net.isIP(ip) === 4) {
    const [a,b]=ip.split('.').map(Number);
    const c=ip.split('.').map(Number)[2];
    return !(a===0||a===10||a===127||a>=224||(a===169&&b===254)||(a===172&&b>=16&&b<=31)||(a===192&&(b===168||b===0||b===2))||(a===100&&b>=64&&b<=127)||(a===198&&(b===18||b===19||(b===51&&c===100)))||(a===203&&b===0&&c===113));
  }
  if(net.isIP(ip) === 6){const canonical=new URL('http://['+ip+']/').hostname;return /^\[[23]/.test(canonical) && !canonical.startsWith('[2001:db8:') && !canonical.startsWith('[2002:');}
  return false;
}
export async function publicAddress(hostname) {
  const host=hostname.replace(/^\[|\]$/g,'');
  if(host==='localhost'||host.endsWith('.localhost')||host.endsWith('.local')||host.endsWith('.internal')) throw Object.assign(new Error('Gli indirizzi privati non sono accessibili dal browser web.'),{status:403});
  const addresses=net.isIP(host)?[{address:host}]:await lookup(host,{all:true});
  if(!addresses.length || addresses.some(item=>!isPublicIP(item.address))) throw Object.assign(new Error('Gli indirizzi privati non sono accessibili dal browser web.'),{status:403});
  return addresses[0].address;
}
export async function validatePublicURL(raw) {
  const url=new URL(raw);
  if(url.protocol!=='https:' || url.username || url.password || url.hash || url.search) throw Object.assign(new Error('Inserisci un endpoint HTTPS pubblico senza credenziali o query nell’URL.'),{status:403});
  await publicAddress(url.hostname);
  return url;
}
// Resolve once for each outbound connection; connect to that numeric address
// while preserving the TLS hostname. User-configured DNS cannot rebind to LAN.
export async function secureProviderFetch(raw,options={},policy={}) {
  const url=new URL(raw);
  if(!['http:','https:'].includes(url.protocol)||url.username||url.password)throw new Error('Endpoint non consentito');
  if(!policy.allowPrivate&&url.protocol!=='https:')throw new Error('HTTPS richiesto');
  const address=policy.allowPrivate?undefined:await publicAddress(url.hostname);
  return new Promise((resolve,reject)=>{
    const client=url.protocol==='https:'?https:http;
    const request=client.request({hostname:address??url.hostname,servername:url.hostname.replace(/^\[|\]$/g,''),port:url.port||undefined,path:url.pathname+url.search,method:options.method??'GET',headers:{'Accept-Encoding':'identity',...options.headers,Host:url.host},signal:options.signal,timeout:120000},response=>{
      const headers=new Headers();for(const[key,value]of Object.entries(response.headers))if(value!==undefined)headers.set(key,Array.isArray(value)?value.join(', '):value);
      const noBody=[204,205,304].includes(response.statusCode);
      resolve(new Response(noBody?null:Readable.toWeb(response),{status:response.statusCode,headers}));
    });
    request.on('error',reject);request.on('timeout',()=>request.destroy(new Error('Provider timeout')));
    if(options.body!==undefined)request.write(options.body);request.end();
  });
}
export async function createEgressProxy() {
  const sockets=new Set();
  const server=http.createServer(async(req,res)=>{
    try {
      const url=new URL(req.url);
      if(url.protocol!=='http:' || url.username || url.password || !['80','8080',''].includes(url.port)) throw new Error('Protocollo o porta non consentiti.');
      const address=await publicAddress(url.hostname);
      const headers={...req.headers,host:url.host};
      delete headers['proxy-authorization'];delete headers['proxy-connection'];
      const upstream=http.request({hostname:address,port:url.port||80,path:url.pathname+url.search,method:req.method,headers,timeout:30000},response=>{res.writeHead(response.statusCode,response.headers);response.pipe(res);});
      upstream.on('timeout',()=>upstream.destroy());upstream.on('error',()=>{if(!res.headersSent)res.writeHead(502);res.end();});
      req.pipe(upstream);
    } catch {res.writeHead(403,{'Content-Type':'text/plain'});res.end('Destinazione non consentita');}
  });
  server.on('connection',socket=>{sockets.add(socket);socket.on('close',()=>sockets.delete(socket));});
  server.on('connect',async(req,client,head)=>{
    let remote;
    try {
      const url=new URL('https://'+req.url);
      if(url.username||url.password||url.pathname!=='/'||!['443','8443',''].includes(url.port)) throw new Error('Porta non consentita');
      const address=await publicAddress(url.hostname);
      remote=net.connect({host:address,port:Number(url.port||443)});
      remote.setTimeout(90000,()=>remote.destroy());client.setTimeout(90000,()=>client.destroy());
      remote.once('connect',()=>{client.write('HTTP/1.1 200 Connection Established\r\n\r\n');if(head.length)remote.write(head);remote.pipe(client);client.pipe(remote);});
      remote.on('error',()=>client.destroy());client.on('error',()=>remote.destroy());client.on('close',()=>remote.destroy());
    } catch {client.end('HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n');remote?.destroy();}
  });
  server.on('clientError',(_,socket)=>socket.destroy());
  await new Promise((resolve,reject)=>{server.once('error',reject);server.listen(0,'127.0.0.1',resolve);});
  return {url:`http://127.0.0.1:${server.address().port}`,close:()=>{for(const socket of sockets)socket.destroy();server.close();}};
}
