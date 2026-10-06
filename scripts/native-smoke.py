#!/usr/bin/env python3
"""Linux X11 native provider against a real GTK fixture, including Esc.
This does not test AT-SPI semantic operations, Wayland or Windows. No user
application is touched; an isolated Xvfb is started and only fixture PIDs owned
by this script are stopped. Requires Xvfb, GTK3, X11 and XTest libraries.
"""
import argparse,base64,ctypes as C,ctypes.util,json,os,pathlib,queue,shutil,subprocess as sp,tempfile,threading,time

def main():
    parser=argparse.ArgumentParser();parser.add_argument('--evidence',default=None);args=parser.parse_args()
    root=pathlib.Path(__file__).resolve().parent.parent;checks=[];notifications=[];owned=[]
    evidence=pathlib.Path(args.evidence).resolve() if args.evidence else None
    if evidence:evidence.mkdir(parents=True,exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='native-ui-fixture-') as temp:
        temp=pathlib.Path(temp);fixture_file=temp/'fixture.py';shutil.copyfile(root/'scripts/fixtures/native-gtk.py',fixture_file)
        try:
            x=sp.Popen(['Xvfb','-displayfd','1','-screen','0','1024x768x24','-nolisten','tcp'],stdout=sp.PIPE,stderr=sp.DEVNULL,text=True);owned.append(x)
            display=':'+x.stdout.readline().strip()
            env={**os.environ,'DISPLAY':display,'XDG_SESSION_TYPE':'x11','NO_AT_BRIDGE':'1'}
            fixture=sp.Popen(['/usr/bin/python3',str(fixture_file),str(temp/'result.json')],env=env,stdout=sp.DEVNULL,stderr=sp.DEVNULL);owned.append(fixture);time.sleep(.6)
            token='a'*64
            helper=sp.Popen(['/usr/bin/python3','-I','-u',str(root/'core/internal/computer/native/linux.py')],env={**env,'SVOLO_NATIVE_TOKEN':token,'SVOLO_NATIVE_FOREGROUND':'1'},stdin=sp.PIPE,stdout=sp.PIPE,stderr=sp.PIPE,text=True);owned.append(helper)
            incoming=queue.Queue()
            def reader():
                for line in helper.stdout:incoming.put(json.loads(line))
                incoming.put({'closed':True})
            threading.Thread(target=reader,daemon=True).start();seq=0
            def call(method,params=None,auth=token):
                nonlocal seq
                seq+=1;helper.stdin.write(json.dumps({'id':seq,'token':auth,'method':method,'params':params or {}})+'\n');helper.stdin.flush()
                while True:
                    r=incoming.get(timeout=10)
                    if 'method' in r:notifications.append(r);continue
                    if r.get('closed'):raise RuntimeError('Native helper exited: '+helper.stderr.read())
                    assert r['id']==seq,r;return r
            def ok(method,params=None):
                r=call(method,params);assert 'result' in r,(method,r);return r['result']
            assert 'error' in call('hello',auth='wrong');checks.append('private-protocol-authentication')
            caps=ok('hello');apps=ok('list_apps')['apps'];app=next(a for a in apps if a['pid']==fixture.pid);win=app['windows'][0]['id'];aid=app['id'];checks.append('enumerate-real-GTK-process-and-window')
            xl=C.CDLL(ctypes.util.find_library('X11'));xl.XOpenDisplay.argtypes=[C.c_char_p];xl.XOpenDisplay.restype=C.c_void_p;d=xl.XOpenDisplay(display.encode());assert d
            xl.XSetInputFocus.argtypes=[C.c_void_p,C.c_ulong,C.c_int,C.c_ulong];xl.XSync.argtypes=[C.c_void_p,C.c_int];xl.XCloseDisplay.argtypes=[C.c_void_p]
            # A user action in the fixture, not an action of the provider.
            xl.XSetInputFocus(d,win,1,0);xl.XSync(d,0)
            assert 'error' in call('click',{'app':aid,'x':20,'y':30});checks.append('unapproved-input-refused')
            ok('overlay_show',{'app':aid,'session':'native-fixture','session_label':'Native acceptance fixture'})
            shot=ok('screenshot',{'app':aid});checks.append('owned-window-only-screenshot')
            if evidence:(evidence/'native-before.png').write_bytes(base64.b64decode(shot['png']))
            for char in 'abc':ok('press_key',{'app':aid,'key':char})
            ok('click',{'app':aid,'x':250,'y':150});time.sleep(.15)
            assert json.loads((temp/'result.json').read_text())=={'confirmed':True,'text':'abc'};checks.append('real-keyboard-and-click-effect-verified')
            shot=ok('screenshot',{'app':aid})
            if evidence:(evidence/'native-after.png').write_bytes(base64.b64decode(shot['png']))
            assert 'error' in call('click',{'app':aid,'x':10000,'y':50});checks.append('out-of-window-input-refused')
            xt=C.CDLL(ctypes.util.find_library('Xtst'));xt.XTestFakeKeyEvent.argtypes=[C.c_void_p,C.c_uint,C.c_int,C.c_ulong]
            xl.XStringToKeysym.argtypes=[C.c_char_p];xl.XStringToKeysym.restype=C.c_ulong;xl.XKeysymToKeycode.argtypes=[C.c_void_p,C.c_ulong];xl.XKeysymToKeycode.restype=C.c_uint
            esc=xl.XKeysymToKeycode(d,xl.XStringToKeysym(b'Escape'));xt.XTestFakeKeyEvent(d,esc,1,0);xl.XSync(d,0);time.sleep(.25);xt.XTestFakeKeyEvent(d,esc,0,0);xl.XSync(d,0)
            ok('hello');assert any(n.get('method')=='cancelled' and n.get('params',{}).get('session')=='native-fixture' for n in notifications),notifications
            assert 'error' in call('click',{'app':aid,'x':250,'y':150});checks.append('global-Esc-revokes-grant-and-notifies-session')
            ok('overlay_show',{'app':aid,'session':'native-fixture','session_label':'Second explicit grant'})
            ok('overlay_hide',{'app':aid});assert 'error' in call('click',{'app':aid,'x':250,'y':150});checks.append('explicit-release-revokes-grant')
            ok('shutdown');xl.XCloseDisplay(d)
            result={'status':'pass','platform':'linux-x11-Xvfb','application':'real GTK3 fixture','checks':checks,'unqualified':['AT-SPI semantic operations','Wayland','Windows','macOS'],'capabilities':caps}
            if evidence:(evidence/'native-smoke.json').write_text(json.dumps(result,indent=2)+'\n')
            print(json.dumps(result,indent=2))
        finally:
            for p in reversed(owned):
                if p.poll() is None:
                    p.terminate()
                    try:p.wait(timeout=3)
                    except sp.TimeoutExpired:p.kill();p.wait()
if __name__=='__main__':main()
