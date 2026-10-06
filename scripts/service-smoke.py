#!/usr/bin/env python3
"""Lifecycle smoke for the actual detached Go binary, without reboot or services.
Tests process persistence, not a real OpenSSH or native user-service installation.
"""
import argparse, json, pathlib, socket, subprocess, tempfile, urllib.request, time

def main():
    parser=argparse.ArgumentParser();parser.add_argument('--binary',required=True);args=parser.parse_args()
    binary=str(pathlib.Path(args.binary).resolve());checks=[]
    with tempfile.TemporaryDirectory(prefix='svolo-service-smoke-') as temporary:
        root=pathlib.Path(temporary); data=root/'private';work=root/'workspace';work.mkdir()
        with socket.socket() as probe: probe.bind(('127.0.0.1',0));port=probe.getsockname()[1]
        def cli(*command,check=True):return subprocess.run([binary,'service',*command,'--data',str(data)],capture_output=True,text=True,timeout=30,check=check)
        def api(path,method='GET',body=None):
            info=json.loads((data/'service.json').read_text());token=(data/'auth.token').read_text().strip()
            r=urllib.request.Request(info['url']+path,method=method,data=None if body is None else json.dumps(body).encode(),headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'})
            with urllib.request.urlopen(r,timeout=4) as response:return json.load(response)
        try:
            installed=cli('install','--mode','process','--port',str(port));result=json.loads(installed.stdout)['result'];pid=result['info']['pid']
            token=(data/'auth.token').read_text().strip()
            assert token not in installed.stdout+installed.stderr
            assert result['survivesDisconnect'] and result['mode']=='process'
            time.sleep(.15);assert json.loads(cli('status').stdout)['protocol']==1;checks.append('detached-daemon-survives-installer-exit')
            repeat=json.loads(cli('install','--port',str(port)).stdout)['result'];assert repeat['mode']=='existing' and repeat['info']['pid']==pid;checks.append('healthy-daemon-retained-without-explicit-replace')
            rejected=subprocess.run([binary,'serve','--data',str(data),'--listen','127.0.0.1:0'],capture_output=True,text=True,timeout=5)
            assert rejected.returncode!=0 and 'already in use' in rejected.stderr.lower(),rejected.stderr
            assert json.loads(cli('status').stdout)['ok'];checks.append('second-writer-rejected-without-disrupting-live-daemon')
            api('/v1/sessions','POST',{'id':'persist','name':'Persistent fixture','workspace':str(work),'allowExec':False})
            api('/v1/projects/board','POST',{'op':{'type':'add','id':'abc001','title':'Survives restart','cwd':str(work)},'operationId':'persist-first','expectedRevision':0})
            cli('stop');assert cli('status',check=False).returncode!=0;checks.append('authenticated-stop-and-service-identity-cleanup')
            cli('install','--port',str(port));assert api('/v1/config')['sessions'][0]['id']=='persist'
            board=api('/v1/projects/board');assert board['revision']==1 and board['value']['cards'][0]['title']=='Survives restart'
            assert (data/'auth.token').read_text().strip()==token;checks.append('session-board-revision-and-credential-survive-restart')
            events=api('/v1/events');seq=[e['seq'] for e in events];assert seq==sorted(set(seq));checks.append('durable-event-sequence-after-restart')
            cli('stop');logs=(data/'daemon.log').read_text();assert token not in logs;checks.append('no-master-bearer-in-daemon-or-installer-output')
        finally:
            cli('stop',check=False)
    print(json.dumps({'status':'pass','checks':checks,'realSSH':False,'userServiceManager':False,'productionQualified':False},indent=2))
if __name__=='__main__': main()
