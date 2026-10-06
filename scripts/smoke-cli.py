#!/usr/bin/env python3
"""Exercise the built daemon, authenticated manual CLI and stdio MCP proxy.
No external model, website, SSH host or browser policy changes are involved.
"""
import argparse, json, os, pathlib, subprocess, tempfile, time, urllib.request, threading, queue

def main():
    parser=argparse.ArgumentParser();parser.add_argument('--binary',required=True);args=parser.parse_args()
    binary=str(pathlib.Path(args.binary).resolve());checks=[]
    with tempfile.TemporaryDirectory(prefix='svolo-cli-smoke-') as root:
        root=pathlib.Path(root);data=root/'data';ready=root/'ready.json';work=root/'workspace';work.mkdir()
        proc=subprocess.Popen([binary,'serve','--data',str(data),'--listen','127.0.0.1:0','--ready-file',str(ready)],stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
        try:
            deadline=time.monotonic()+8
            while not ready.exists():
                if proc.poll() is not None:raise RuntimeError(proc.stderr.read())
                if time.monotonic()>deadline:raise TimeoutError('daemon readiness')
                time.sleep(.025)
            info=json.loads(ready.read_text());token=(data/'auth.token').read_text().strip()
            def api(path,method='GET',body=None):
                payload=None if body is None else json.dumps(body).encode()
                req=urllib.request.Request(info['url']+path,data=payload,method=method,headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'})
                with urllib.request.urlopen(req,timeout=5) as r:return json.load(r)
            assert api('/v1/health')['productionQualified'] is False;checks.append('daemon-ready-and-authenticated-health')
            api('/v1/sessions','POST',{'id':'smoke','name':'smoke','workspace':str(work),'allowExec':False})
            (work/'hello.txt').write_text('CLI fixture',encoding='utf8')
            result=subprocess.run([binary,'call','--url',info['url'],'--token-file',str(data/'auth.token'),'--session','smoke','--tool','workspace-read','--args','{"path":"hello.txt"}'],capture_output=True,text=True,timeout=8,check=True)
            assert 'CLI fixture' in result.stdout;checks.append('manual-cli-workspace-read')
            minted=api('/v1/tokens','POST',{'session':'smoke','label':'CLI fixture'});scoped=root/'client.token';scoped.write_text(minted['token']);os.chmod(scoped,0o600)
            messages=[{'jsonrpc':'2.0','id':1,'method':'initialize','params':{'protocolVersion':'2025-11-25','capabilities':{},'clientInfo':{'name':'smoke','version':'1'}}},{'jsonrpc':'2.0','id':2,'method':'tools/list','params':{}}]
            proxy=subprocess.Popen([binary,'mcp','--url',info['url'],'--token-file',str(scoped)],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
            received=queue.Queue()
            def read_replies():
                for line in proxy.stdout:
                    if line.strip():received.put(json.loads(line))
            thread=threading.Thread(target=read_replies,daemon=True);thread.start();replies=[]
            try:
                # MCP clients keep stdin open until pending operations complete.
                # EOF intentionally cancels active operations; do not simulate a
                # completed round trip by closing the client prematurely.
                for message in messages:
                    proxy.stdin.write(json.dumps(message)+'\n');proxy.stdin.flush()
                    replies.append(received.get(timeout=8))
                proxy.stdin.close();proxy.wait(timeout=8)
                assert proxy.returncode==0,proxy.stderr.read()
                assert len(replies)==2 and any(x.get('result',{}).get('tools') for x in replies),replies
            finally:
                if proxy.poll() is None:proxy.kill();proxy.wait()
                thread.join(timeout=2)
            checks.append('scoped-stdio-MCP-through-live-HTTP-daemon')
            proc.terminate();stdout,stderr=proc.communicate(timeout=10)
            assert token not in stdout+stderr and minted['token'] not in stdout+stderr
            assert proc.returncode==0,(proc.returncode,stderr)
            checks.append('graceful-stop-and-no-credential-logs')
        finally:
            if proc.poll() is None:proc.kill();proc.communicate()
    print(json.dumps({'status':'pass','checks':checks,'productionQualified':False},indent=2))
if __name__=='__main__':main()
