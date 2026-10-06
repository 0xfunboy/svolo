#!/usr/bin/env python3
"""Native Linux provider: AT-SPI2 semantic actions, scoped X11 capture/input.
No external Python packages. Never activates another application. Coordinate/
keyboard input additionally requires explicit foreground mode and the target
already focused. Wayland visual/input requires a portal provider (reported as
unavailable rather than silently bypassing compositor consent).
"""
import base64, ctypes as C, ctypes.util, difflib, hashlib, hmac, json, os, select, struct, sys, time, zlib
P=C.c_void_p; I=C.c_int; U=C.c_uint; L=C.c_ulong; S=C.c_char_p
class Failure(Exception):
    def __init__(self,code,message): self.code,self.message=code,message

def fail(message,code=-32005): raise Failure(code,message)
def lib(name): return C.CDLL(ctypes.util.find_library(name) or name)
def fn(lib,name,ret,*args):
    f=getattr(lib,name);f.restype=ret;f.argtypes=list(args);return f
class Rect(C.Structure): _fields_=[('x',I),('y',I),('width',I),('height',I)]
class XAttributes(C.Structure):
    _fields_=[('x',I),('y',I),('width',I),('height',I),('border_width',I),('depth',I),('visual',P),('root',L),('class_',I),('bit_gravity',I),('win_gravity',I),('backing_store',I),('backing_planes',L),('backing_pixel',L),('save_under',I),('colormap',L),('map_installed',I),('map_state',I),('all_event_masks',C.c_long),('your_event_mask',C.c_long),('do_not_propagate_mask',C.c_long),('override_redirect',I),('screen',P)]
class XImage(C.Structure):
    _fields_=[('width',I),('height',I),('xoffset',I),('format',I),('data',P),('byte_order',I),('bitmap_unit',I),('bitmap_bit_order',I),('bitmap_pad',I),('depth',I),('bytes_per_line',I),('bits_per_pixel',I),('red_mask',L),('green_mask',L),('blue_mask',L)]

def png_rgb(width,height,rows):
    def chunk(k,b):return struct.pack('!I',len(b))+k+b+struct.pack('!I',zlib.crc32(k+b)&0xffffffff)
    return b'\x89PNG\r\n\x1a\n'+chunk(b'IHDR',struct.pack('!2I5B',width,height,8,2,0,0,0))+chunk(b'IDAT',zlib.compress(b''.join(b'\0'+r for r in rows),3))+chunk(b'IEND',b'')

class X11:
    def __init__(self):
        self.x=lib('X11');self.error=False
        self.errorcb=C.CFUNCTYPE(I,P,P)(self.on_error);fn(self.x,'XSetErrorHandler',P,P)(C.cast(self.errorcb,P))
        self.d=fn(self.x,'XOpenDisplay',P,S)(None)
        if not self.d: fail('No usable X11 display; use the interactive user session',-32001)
        self.root=fn(self.x,'XDefaultRootWindow',L,P)(self.d)
        for n,ret,args in [('XInternAtom',L,[P,S,I]),('XGetWindowProperty',I,[P,L,L,C.c_long,C.c_long,I,L,C.POINTER(L),C.POINTER(I),C.POINTER(L),C.POINTER(L),C.POINTER(P)]),('XFree',I,[P]),('XQueryTree',I,[P,L,C.POINTER(L),C.POINTER(L),C.POINTER(P),C.POINTER(U)]),('XGetWindowAttributes',I,[P,L,C.POINTER(XAttributes)]),('XGetInputFocus',I,[P,C.POINTER(L),C.POINTER(I)]),('XQueryKeymap',I,[P,P]),('XStringToKeysym',L,[S]),('XKeysymToKeycode',U,[P,L]),('XSync',I,[P,I]),('XGetImage',P,[P,L,I,I,U,U,L,I]),('XGetPixel',L,[P,I,I]),('XDestroyImage',I,[P]),('XTranslateCoordinates',I,[P,L,L,I,I,C.POINTER(I),C.POINTER(I),C.POINTER(L)]),('XFreePixmap',I,[P,L])]:fn(self.x,n,ret,*args)
        self.xt=lib('Xtst');fn(self.xt,'XTestFakeMotionEvent',I,P,I,I,I,L);fn(self.xt,'XTestFakeButtonEvent',I,P,U,I,L);fn(self.xt,'XTestFakeKeyEvent',I,P,U,I,L)
        self.composite=lib('Xcomposite');fn(self.composite,'XCompositeNameWindowPixmap',L,P,L)
    def on_error(self,display,event):
        self.error=True
        raw=C.string_at(event,35);self.last_error={"error":raw[32],"request":raw[33],"minor":raw[34]}
        return 0
    def prop(self,w,name):
        atom=self.x.XInternAtom(self.d,name.encode(),1)
        if not atom:return []
        typ=L();fmt=I();n=L();remain=L();data=P()
        status=self.x.XGetWindowProperty(self.d,w,atom,0,65536,0,0,C.byref(typ),C.byref(fmt),C.byref(n),C.byref(remain),C.byref(data))
        if status or not data:return []
        try:
            if fmt.value==32:return list(C.cast(data,C.POINTER(L))[:n.value])
            if fmt.value==8:return C.string_at(data,n.value).decode('utf8','replace').rstrip('\0')
            return []
        finally:self.x.XFree(data)
    def children(self,w):
        root=L();parent=L();data=P();n=U()
        if not self.x.XQueryTree(self.d,w,C.byref(root),C.byref(parent),C.byref(data),C.byref(n)):return []
        try:return list(C.cast(data,C.POINTER(L))[:n.value]) if data else []
        finally:
            if data:self.x.XFree(data)
    def windows(self):
        ids=self.prop(self.root,'_NET_CLIENT_LIST_STACKING') or self.children(self.root);out=[]
        for w in ids:
            a=XAttributes()
            if not self.x.XGetWindowAttributes(self.d,w,C.byref(a)) or a.map_state!=2:continue
            pid=self.prop(w,'_NET_WM_PID');title=self.prop(w,'_NET_WM_NAME') or self.prop(w,'WM_NAME') or ''
            if not pid:
                for child in self.children(w):
                    p=self.prop(child,'_NET_WM_PID')
                    if p:pid=p;w=child;break
            if pid:out.append({'id':int(w),'pid':int(pid[0]),'title':str(title)[:2048]})
        return out
    def focused(self,w):
        active=self.prop(self.root,'_NET_ACTIVE_WINDOW')
        if active:return active[0]==w
        focus=L();revert=I();self.x.XGetInputFocus(self.d,C.byref(focus),C.byref(revert))
        return focus.value==w or focus.value in self.children(w)
    def geom(self,w):
        a=XAttributes()
        if not self.x.XGetWindowAttributes(self.d,w,C.byref(a)) or a.map_state!=2:fail('Window is not visible',-32003)
        x=I();y=I();child=L();self.x.XTranslateCoordinates(self.d,w,self.root,0,0,C.byref(x),C.byref(y),C.byref(child))
        return x.value,y.value,a.width,a.height
    def shot(self,w):
        # GTK installs its own global X error handler. Trap errors for our
        # connection while probing compositor pixmaps, then restore GTK's.
        previous=self.x.XSetErrorHandler(C.cast(self.errorcb,P))
        try:return self._shot(w)
        finally:self.x.XSetErrorHandler(previous)
    def _shot(self,w):
        _,_,width,height=self.geom(w)
        if width<1 or height<1 or width*height>12000000:fail('Window image exceeds pixel budget')
        self.error=False;pix=self.composite.XCompositeNameWindowPixmap(self.d,w);self.x.XSync(self.d,0)
        composited=bool(pix and not self.error)
        if not composited:
            if not self.focused(w):fail('Uncomposited background capture unavailable; focus the target manually',-32011)
            # Do not expose unrelated overlapping windows in the foreground fallback.
            ordered=self.windows();pos=next((i for i,v in enumerate(ordered) if v['id']==w),None)
            if pos is None:fail('Cannot verify capture isolation',-32011)
            x,y,ww,hh=self.geom(w)
            for other in ordered[pos+1:]:
                ox,oy,ow,oh=self.geom(other['id'])
                if x<ox+ow and ox<x+ww and y<oy+oh and oy<y+hh:fail('Another window overlaps target capture',-32011)
        self.error=False;image=self.x.XGetImage(self.d,pix if composited else w,0,0,width,height,L(-1).value,2);self.x.XSync(self.d,0)
        if not image or self.error:
            if composited:self.x.XFreePixmap(self.d,pix)
            fail('Scoped window capture failed: '+str(getattr(self,'last_error',{})))
        try:
            im=C.cast(image,C.POINTER(XImage)).contents;raw=C.string_at(im.data,im.bytes_per_line*height);rows=[]
            if im.bits_per_pixel==32 and im.byte_order==0 and (im.red_mask,im.green_mask,im.blue_mask)==(0xff0000,0xff00,0xff):
                for yy in range(height):
                    line=raw[yy*im.bytes_per_line:yy*im.bytes_per_line+width*4];rgb=bytearray(width*3);rgb[0::3]=line[2::4];rgb[1::3]=line[1::4];rgb[2::3]=line[0::4];rows.append(bytes(rgb))
            else:
                for yy in range(height):
                    row=bytearray()
                    for xx in range(width):
                        v=self.x.XGetPixel(image,xx,yy)
                        for mask in (im.red_mask,im.green_mask,im.blue_mask):
                            shift=(mask&-mask).bit_length()-1;row.append(((v&mask)>>shift)*255//(mask>>shift))
                    rows.append(bytes(row))
            return {'png':base64.b64encode(png_rgb(width,height,rows)).decode(),'width':width,'height':height,'scale':1}
        finally:
            self.x.XDestroyImage(image)
            if composited:self.x.XFreePixmap(self.d,pix)
    def escape(self):
        keys=C.create_string_buffer(32);self.x.XQueryKeymap(self.d,keys);k=self.x.XKeysymToKeycode(self.d,self.x.XStringToKeysym(b'Escape'));return bool(keys.raw[k//8]&(1<<(k%8)))
    def key(self,key,pressed):
        k=self.x.XKeysymToKeycode(self.d,self.x.XStringToKeysym(key.encode()))
        if not k:fail('Key is not mapped on this keyboard')
        self.xt.XTestFakeKeyEvent(self.d,k,int(pressed),0)
    def sync(self):self.x.XSync(self.d,0)

class Atspi:
    def __init__(self):
        # Probe the session accessibility bus before libatspi: its lazy desktop
        # getter aborts the entire process if the registry cannot be reached.
        gio=lib('gio-2.0');gl=lib('glib-2.0');go=lib('gobject-2.0')
        fn(gio,'g_bus_get_sync',P,I,P,P);fn(gio,'g_dbus_connection_call_sync',P,P,S,S,S,S,P,P,I,I,P,P)
        fn(gl,'g_variant_get_child_value',P,P,C.c_size_t);fn(gl,'g_variant_get_string',S,P,P);fn(gl,'g_variant_unref',None,P);fn(go,'g_object_unref',None,P)
        if not os.environ.get('AT_SPI_BUS_ADDRESS'):
            conn=gio.g_bus_get_sync(2,None,None)
            if not conn:fail('No user session D-Bus connection for accessibility',-32001)
            response=gio.g_dbus_connection_call_sync(conn,b'org.a11y.Bus',b'/org/a11y/bus',b'org.a11y.Bus',b'GetAddress',None,None,0,1500,None,None)
            go.g_object_unref(conn)
            if not response:fail('AT-SPI bus service unavailable; install at-spi2-core and run inside the graphical user session',-32001)
            child=gl.g_variant_get_child_value(response,0);address=gl.g_variant_get_string(child,None)
            if address:os.environ['AT_SPI_BUS_ADDRESS']=address.decode()
            gl.g_variant_unref(child);gl.g_variant_unref(response)
            if not address:fail('Empty AT-SPI bus address',-32001)
        self.a=lib('atspi');self.g=go;self.gl=gl
        fn(self.g,'g_object_unref',None,P);fn(self.gl,'g_free',None,P);fn(self.a,'atspi_init',I)();fn(self.a,'atspi_set_timeout',None,I,I)(500,1500)
        for name,ret,args in [
          ('get_desktop',P,[I]),('accessible_get_child_count',I,[P,P]),('accessible_get_child_at_index',P,[P,I,P]),('accessible_get_name',P,[P,P]),('accessible_get_role_name',P,[P,P]),('accessible_get_process_id',U,[P,P]),('accessible_get_component_iface',P,[P]),('component_get_extents',P,[P,I,P]),('accessible_get_action_iface',P,[P]),('action_get_n_actions',I,[P,P]),('action_get_action_name',P,[P,I,P]),('action_do_action',I,[P,I,P]),('accessible_get_editable_text_iface',P,[P]),('editable_text_set_text_contents',I,[P,S,P]),('editable_text_insert_text',I,[P,I,S,I,P]),('accessible_get_text_iface',P,[P]),('text_get_text',P,[P,I,I,P]),('text_get_caret_offset',I,[P,P]),('text_set_caret_offset',I,[P,I,P]),('text_get_n_selections',I,[P,P]),('text_add_selection',I,[P,I,I,P]),('text_set_selection',I,[P,I,I,I,P]),('accessible_get_state_set',P,[P]),('state_set_contains',I,[P,I]),('state_type_get_type',L,[])]:fn(self.a,'atspi_'+name,ret,*args)
        self.refs={};self.revision=0;self.previous={};self.state_enums={}
        class EV(C.Structure):_fields_=[('value',I),('name',S),('nick',S)]
        fn(self.g,'g_type_class_ref',P,L);fn(self.g,'g_type_class_unref',None,P);fn(self.g,'g_enum_get_value_by_nick',C.POINTER(EV),P,S)
        klass=self.g.g_type_class_ref(self.a.atspi_state_type_get_type())
        for name in ['focused','editable','sensitive','enabled','showing']:
            v=self.g.g_enum_get_value_by_nick(klass,name.encode());self.state_enums[name]=v.contents.value if v else -1
        self.g.g_type_class_unref(klass)
    def text(self,p):
        if not p:return ''
        try:return C.string_at(p).decode('utf8','replace')[:20000]
        finally:self.gl.g_free(p)
    def name(self,p):return self.text(self.a.atspi_accessible_get_name(p,None))
    def role(self,p):return self.text(self.a.atspi_accessible_get_role_name(p,None))
    def children(self,p):
        out=[]
        for i in range(min(1200,max(0,self.a.atspi_accessible_get_child_count(p,None)))):
            c=self.a.atspi_accessible_get_child_at_index(p,i,None)
            if c:out.append(c)
        return out
    def applications(self):
        desktop=self.a.atspi_get_desktop(0)
        if not desktop:return []
        children=self.children(desktop);self.g.g_object_unref(desktop);out=[]
        for p in children:
            pid=int(self.a.atspi_accessible_get_process_id(p,None));out.append((pid,self.name(p),p))
        return out
    def state(self,p,name):
        states=self.a.atspi_accessible_get_state_set(p)
        if not states:return False
        try:return bool(self.a.atspi_state_set_contains(states,self.state_enums.get(name,-1)))
        finally:self.g.g_object_unref(states)
    def snapshot(self,pid,window_title,app,disable_diff):
        # Drop all prior references, including other apps: indices never silently alias.
        for rec in self.refs.values():self.g.g_object_unref(rec['ptr'])
        self.refs={};self.revision+=1;roots=[]
        for p,name,obj in self.applications():
            if p==pid:roots.append(obj)
            else:self.g.g_object_unref(obj)
        lines=[];stack=[(o,0) for o in roots];deadline=time.monotonic()+15
        while stack:
            obj,depth=stack.pop()
            if len(self.refs)>=1000 or time.monotonic()>deadline or depth>24:
                self.g.g_object_unref(obj);continue
            idx=self.revision*10000+len(self.refs)+1;role=self.role(obj);name=self.name(obj);secret='password' in role.lower()
            comp=self.a.atspi_accessible_get_component_iface(obj);bounds=None
            if comp:
                rect=self.a.atspi_component_get_extents(comp,0,None)
                if rect:
                    r=C.cast(rect,C.POINTER(Rect)).contents;bounds=[r.x,r.y,r.width,r.height];self.gl.g_free(rect)
            self.refs[idx]={'ptr':obj,'pid':pid,'app':app,'secret':secret,'role':role,'name':name,'bounds':bounds}
            value='';text=self.a.atspi_accessible_get_text_iface(obj)
            if text and not secret:value=self.text(self.a.atspi_text_get_text(text,0,2000,None))
            line='  '*depth+'[%d] %s %s'%(idx,role,json.dumps('[redacted]' if secret else name,ensure_ascii=False))
            if value and value!=name:line+=' value='+json.dumps(value[:2000],ensure_ascii=False)
            if self.state(obj,'focused'):line+=' focused'
            lines.append(line);stack.extend((ch,depth+1) for ch in reversed(self.children(obj)))
        full='\n'.join(lines) or 'No accessible elements exposed. Use visual input only after explicitly enabling foreground control.'
        prev=self.previous.get(app);self.previous[app]=full
        # Changing index epochs makes a full snapshot safer than a misleading tiny diff.
        return full,len(self.refs),self.revision
    def ref(self,index,pid):
        r=self.refs.get(int(index))
        if not r or r['pid']!=pid:fail('Stale element: obtain a fresh application snapshot',-32004)
        if r['secret']:fail('Password controls are never read or modified',-32007)
        return r
    def focused(self,pid):
        matches=[r for r in self.refs.values() if r['pid']==pid and self.state(r['ptr'],'focused') and not r['secret']]
        if len(matches)!=1:fail('No unique focused accessible control; get_app_state or set_value explicitly',-32004)
        return matches[0]
    def invoke(self,r,action=None):
        a=self.a.atspi_accessible_get_action_iface(r['ptr'])
        if not a:fail('Element has no semantic action; foreground coordinates require explicit permission',-32011)
        n=self.a.atspi_action_get_n_actions(a,None);matches=[]
        for i in range(n):
            name=self.text(self.a.atspi_action_get_action_name(a,i,None))
            if action is None or name.lower()==str(action).lower():matches.append(i)
        if not matches or (action is None and len(matches)!=1):fail('Ambiguous/missing semantic action; select it explicitly')
        if not self.a.atspi_action_do_action(a,matches[0],None):fail('Application refused semantic action')
    def set_value(self,r,text):
        edit=self.a.atspi_accessible_get_editable_text_iface(r['ptr'])
        if not edit or not self.a.atspi_editable_text_set_text_contents(edit,text.encode(),None):fail('Control does not support setting text in background',-32011)
    def insert(self,r,text):
        edit=self.a.atspi_accessible_get_editable_text_iface(r['ptr']);txt=self.a.atspi_accessible_get_text_iface(r['ptr'])
        if not edit or not txt:fail('Focused control does not support background text input',-32011)
        if self.a.atspi_text_get_n_selections(txt,None)>0:fail('A selection is active; use set_value to avoid ambiguous replacement')
        pos=self.a.atspi_text_get_caret_offset(txt,None)
        if not self.a.atspi_editable_text_insert_text(edit,pos,text.encode(),len(text),None):fail('Application refused text insertion')
    def select(self,r,p):
        txt=self.a.atspi_accessible_get_text_iface(r['ptr'])
        if not txt:fail('Control has no text interface')
        text=self.text(self.a.atspi_text_get_text(txt,0,-1,None));needle=str(p['text']);prefix=str(p.get('prefix',''));suffix=str(p.get('suffix',''));find=prefix+needle+suffix
        start=text.find(find)
        if start<0 or text.find(find,start+1)>=0:fail('Text match missing or ambiguous')
        start+=len(prefix);end=start+len(needle);kind=p.get('selection_type','text')
        if kind=='cursor_before':ok=self.a.atspi_text_set_caret_offset(txt,start,None)
        elif kind=='cursor_after':ok=self.a.atspi_text_set_caret_offset(txt,end,None)
        elif kind=='text':
            ok=self.a.atspi_text_set_selection(txt,0,start,end,None) if self.a.atspi_text_get_n_selections(txt,None) else self.a.atspi_text_add_selection(txt,start,end,None)
        else:fail('Invalid selection type',-32008)
        if not ok:fail('Application refused text selection')

DENY={'xterm','uxterm','konsole','gnome-terminal','gnome-terminal-server','kitty','alacritty','wezterm','ghostty','tilix','terminator','xfce4-terminal','mate-terminal','lxterminal','qterminal','foot','bash','sh','zsh','fish','sudo','su','pkexec','gksu','polkit-gnome-authentication-agent-1','gnome-keyring-daemon','seahorse','keepassxc','gdm','sddm','lightdm','login','svolo-core','svolo'}
class Provider:
    def __init__(self):
        self.a=None;self.x=None;self.a_error='';self.x_error='';self.grants={};self.overlay=None;self.escape_last=False
        try:self.a=Atspi()
        except Exception as e:self.a_error=str(e)
        if os.environ.get('XDG_SESSION_TYPE')!='wayland':
            try:self.x=X11()
            except Exception as e:self.x_error=str(e)
        else:self.x_error='Wayland capture/input requires the consented portal provider; semantic AT-SPI remains available'
        self.own={os.getpid(),os.getppid()}
        for p in os.environ.get('SVOLO_OWNER_PIDS','').split(','):
            if p.isdigit():self.own.add(int(p))
    def apps(self):
        out={};windows=self.x.windows() if self.x else []
        bypid={}
        for w in windows:bypid.setdefault(w['pid'],[]).append({'id':w['id'],'title':w['title']})
        names={}
        if self.a:
            for pid,name,obj in self.a.applications():names[pid]=name;self.a.g.g_object_unref(obj)
        for pid in set(names)|set(bypid):
            try:exe=os.path.realpath('/proc/%d/exe'%pid);base=os.path.basename(exe);cmd=open('/proc/%d/cmdline'%pid,'rb').read(4096)
            except OSError:continue
            if pid in self.own or base.lower() in DENY or b'svolo' in cmd.lower() or b'svolo' in cmd.lower():continue
            aid='linux.'+hashlib.sha256(exe.encode()).hexdigest()[:24]
            # Multiple instances remain explicit by PID instead of picking an arbitrary app.
            out[pid]={'id':aid,'bundleId':aid,'displayName':names.get(pid,base),'pid':pid,'isRunning':True,'windows':bypid.get(pid,[])}
        return list(out.values())
    def resolve(self,ref):
        pid=ref.get('pid') if isinstance(ref,dict) else None;query=ref.get('bundleId','') if isinstance(ref,dict) else str(ref)
        hits=[v for v in self.apps() if query.lower() in (v['id'].lower(),v['displayName'].lower()) and (not pid or pid==v['pid'])]
        if not hits:fail('Application not running, inaccessible, or denied. Choose an application from list_apps.',-32002)
        if len(hits)!=1:fail('Several app instances match; use bundleId plus pid',-32008)
        return hits[0]
    def window(self,app,p):
        ws=app['windows'];chosen=p.get('window_id')
        if chosen:
            hit=next((w for w in ws if w['id']==int(chosen)),None)
            if not hit:fail('Window is not owned by this application',-32003)
            return hit
        if self.x:
            for w in ws:
                if self.x.focused(w['id']):return w
        if len(ws)==1:return ws[0]
        if not ws:return {'id':0,'title':app['displayName']}
        fail('Multiple windows: choose window_id explicitly',-32003)
    def check(self,app):
        key=app['id'];grant=self.grants.get(key)
        if not grant or grant['pid']!=app['pid']:fail('Application has no active user-approved session',-32001)
        if time.monotonic()-grant['last']>300:self.grants.pop(key,None);fail('Application grant expired',-32001)
        grant['last']=time.monotonic()
    def foreground(self,app,w):
        if os.environ.get('SVOLO_NATIVE_FOREGROUND')!='1':fail('Foreground input is disabled. Explicitly enable it in Computer settings.',-32011)
        if not self.x or not w or not self.x.focused(w):fail('Target must already be foreground; the agent never activates it',-32011)
        if self.x.escape():self.cancel();fail('Stopped by Esc',-32006)
    def pump(self):
        if self.overlay:self.overlay.pump()
        if self.x:
            pressed=self.x.escape()
            if pressed and not self.escape_last:self.cancel()
            self.escape_last=pressed
    def cancel(self):
        for app,g in list(self.grants.items()):emit({'method':'cancelled','params':{'app':app,'session':g['session'],'reason':'esc'}})
        self.grants.clear()
        if self.overlay:self.overlay.hide()
    def call(self,method,p):
        if method not in ('hello', 'ping', 'permissions', 'request_permissions', 'open_settings', 'list_apps', 'shutdown', 'overlay_hide', 'resolve_app', 'overlay_show', 'screenshot', 'get_app_state', 'set_value', 'select_text', 'perform_secondary_action', 'type_text', 'paste', 'press_key', 'drag', 'click', 'scroll'):fail('Unknown native method',-32601)
        if method=='hello':return {'helperVersion':2,'protocol':1,'os':'linux','arch':os.uname().machine,'pid':os.getpid(),'permissions':self.call('permissions',{})}
        if method=='ping':return {'ok':True}
        if method in ('permissions','request_permissions'):return {'accessibility':bool(self.a),'screenRecording':bool(self.x),'foregroundEnabled':os.environ.get('SVOLO_NATIVE_FOREGROUND')=='1','note':self.x_error or self.a_error}
        if method=='open_settings':return {'note':'Enable accessibility in the desktop settings. Foreground input requires a separate explicit application grant.'}
        if method=='list_apps':return {'apps':self.apps()}
        if method=='shutdown':self.cancel();return {}
        if method=='overlay_hide':
            ref=p.get('app');query=ref.get('bundleId') if isinstance(ref,dict) else ref
            if query:self.grants.pop(query,None)
            else:self.grants.clear()
            if self.overlay:self.overlay.hide()
            return {}
        app=self.resolve(p.get('app',''));aid=app['id'];pid=app['pid']
        if method=='resolve_app':return {'bundleId':aid,'displayName':app['displayName'],'pid':pid}
        if method=='overlay_show':
            if not p.get('session'):fail('Explicit session required',-32008)
            existing=self.grants.get(aid)
            if existing and existing['session']!=p['session']:fail('Application already owned by another session',-32001)
            if not self.overlay:self.overlay=Overlay()
            self.overlay.show('svolo: '+str(p.get('session_label','Agent'))[:80]+' • '+app['displayName']+' • Esc to stop')
            self.grants[aid]={'pid':pid,'session':p['session'],'last':time.monotonic()};return {'visible':True}
        self.check(app);window=self.window(app,p);w=window['id']
        if method=='screenshot':
            if not self.x or not w:fail(self.x_error or 'No scoped screenshot provider',-32011)
            return self.x.shot(w)
        if method=='get_app_state':
            text,count,rev=self.a.snapshot(pid,window['title'],aid,p.get('disable_diff')) if self.a else ('No AT-SPI provider. '+self.a_error,0,0)
            return {'text':text,'mode':'full','bundleId':aid,'pid':pid,'windowId':w,'focusedWindowTitle':window['title'],'revision':rev,'elementCount':count}
        if method in ('set_value','select_text','perform_secondary_action') or (method=='click' and 'element_index' in p):
            if not self.a:fail('AT-SPI accessibility unavailable',-32001)
            r=self.a.ref(p['element_index'],pid)
            if method=='set_value':self.a.set_value(r,str(p['value']))
            elif method=='select_text':self.a.select(r,p)
            else:self.a.invoke(r,p.get('action') if method=='perform_secondary_action' else None)
            return {'method':'AT-SPI','settled':True}
        if method in ('type_text','paste'):
            if p.get('format','text') not in ('text','md'):fail('Rich HTML paste is not supported by the AT-SPI text provider',-32011)
            if not self.a:fail('AT-SPI accessibility unavailable',-32001)
            self.a.insert(self.a.focused(pid),str(p['text']));return {'method':'AT-SPI','settled':True}
        self.foreground(app,w);x,y,width,height=self.x.geom(w)
        def point(px,py):
            px,py=float(px),float(py)
            if not(0<=px<width and 0<=py<height):fail('Coordinates outside the selected application window',-32008)
            return x+int(px),y+int(py)
        if method=='press_key':
            aliases={'ctrl':'Control_L','control':'Control_L','alt':'Alt_L','shift':'Shift_L','meta':'Super_L','super':'Super_L','enter':'Return','escape':'Escape','esc':'Escape','backspace':'BackSpace','space':'space','tab':'Tab','delete':'Delete','up':'Up','down':'Down','left':'Left','right':'Right','home':'Home','end':'End','pageup':'Prior','pagedown':'Next'}
            parts=str(p['key']).split('+');pressed=[]
            if len(parts)>5:fail('Too many key modifiers',-32008)
            try:
                for part in parts:
                    key=aliases.get(part.lower(),part);self.x.key(key,True);pressed.append(key)
            finally:
                for key in reversed(pressed):self.x.key(key,False)
        elif method in ('click','scroll'):
            px,py=point(p.get('x',width/2),p.get('y',height/2));self.x.xt.XTestFakeMotionEvent(self.x.d,-1,px,py,0)
            if method=='click':button={'left':1,'middle':2,'right':3}.get(p.get('mouse_button','left'));count=int(p.get('click_count',1))
            else:button={'up':4,'down':5,'left':6,'right':7}.get(p.get('direction'));count=min(60,max(1,int(p.get('pages',1))*6))
            if not button or count<1 or count>60:fail('Invalid mouse action',-32008)
            for _ in range(count):self.x.xt.XTestFakeButtonEvent(self.x.d,button,1,0);self.x.xt.XTestFakeButtonEvent(self.x.d,button,0,0)
        elif method=='drag':
            fx,fy=point(p['from_x'],p['from_y']);tx,ty=point(p['to_x'],p['to_y']);self.x.xt.XTestFakeMotionEvent(self.x.d,-1,fx,fy,0);self.x.xt.XTestFakeButtonEvent(self.x.d,1,1,0)
            try:
                for i in range(1,13):
                    self.foreground(app,w);self.x.xt.XTestFakeMotionEvent(self.x.d,-1,int(fx+(tx-fx)*i/12),int(fy+(ty-fy)*i/12),0);self.x.sync();time.sleep(.01)
            finally:self.x.xt.XTestFakeButtonEvent(self.x.d,1,0,0)
        else:fail('Unknown native method',-32601)
        self.x.sync();return {'method':'X11 foreground (explicit consent)','settled':True}

class Overlay:
    """A no-focus GTK status window. Its creation is required before a grant."""
    def __init__(self):
        self.g=lib('gtk-3');fn(self.g,'gtk_init_check',I,P,P)
        if not self.g.gtk_init_check(None,None):fail('Cannot display the mandatory control indicator',-32001)
        for n,r,a in [('gtk_window_new',P,[I]),('gtk_label_new',P,[S]),('gtk_container_add',None,[P,P]),('gtk_window_set_title',None,[P,S]),('gtk_window_set_decorated',None,[P,I]),('gtk_window_set_keep_above',None,[P,I]),('gtk_window_set_accept_focus',None,[P,I]),('gtk_window_set_focus_on_map',None,[P,I]),('gtk_widget_show_all',None,[P]),('gtk_widget_hide',None,[P]),('gtk_label_set_text',None,[P,S]),('gtk_events_pending',I,[]),('gtk_main_iteration_do',I,[I]),('gtk_window_move',None,[P,I,I]),('gtk_container_set_border_width',None,[P,U])]:fn(self.g,n,r,*a)
        self.window=self.g.gtk_window_new(0);self.label=self.g.gtk_label_new(b'svolo');self.g.gtk_container_add(self.window,self.label);self.g.gtk_window_set_title(self.window,b'svolo control indicator');self.g.gtk_window_set_decorated(self.window,0);self.g.gtk_window_set_keep_above(self.window,1);self.g.gtk_window_set_accept_focus(self.window,0);self.g.gtk_window_set_focus_on_map(self.window,0);self.g.gtk_container_set_border_width(self.window,10);self.g.gtk_window_move(self.window,0,0)
    def show(self,label):self.g.gtk_label_set_text(self.label,label.encode());self.g.gtk_widget_show_all(self.window);self.pump()
    def hide(self):self.g.gtk_widget_hide(self.window);self.pump()
    def pump(self):
        for _ in range(100):
            if not self.g.gtk_events_pending():break
            self.g.gtk_main_iteration_do(0)

def emit(value):print(json.dumps(value,ensure_ascii=True,separators=(',',':')),flush=True)
def main():
    token=os.environ.pop('SVOLO_NATIVE_TOKEN','')
    if len(token)!=64:return 2
    provider=None;buf=b''
    while True:
        if provider:provider.pump()
        ready,_,_=select.select([sys.stdin.buffer],[],[],.05)
        if not ready:continue
        chunk=os.read(sys.stdin.fileno(),65536)
        if not chunk:break
        buf+=chunk
        if len(buf)>2<<20:return 2
        while b'\n' in buf:
            line,buf=buf.split(b'\n',1);request={}
            try:
                request=json.loads(line)
                if not hmac.compare_digest(str(request.get('token','')),token):fail('Unauthorized native caller',-32600)
                if not isinstance(request.get('params',{}),dict):fail('Parameters must be an object',-32008)
                if provider is None:provider=Provider()
                method=request['method'];result=provider.call(method,request.get('params',{}));emit({'jsonrpc':'2.0','id':request['id'],'result':result})
                if method=='shutdown':return 0
            except Failure as e:emit({'jsonrpc':'2.0','id':request.get('id',0),'error':{'code':e.code,'message':e.message}})
            except Exception as e:emit({'jsonrpc':'2.0','id':request.get('id',0),'error':{'code':-32005,'message':str(e)[:1000]}})
    if provider:provider.cancel()
    return 0
if __name__=='__main__':sys.exit(main())
