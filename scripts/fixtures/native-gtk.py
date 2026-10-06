import ctypes as C,ctypes.util,sys,json,os
P=C.c_void_p;I=C.c_int;S=C.c_char_p;g=C.CDLL(ctypes.util.find_library('gtk-3'));o=C.CDLL(ctypes.util.find_library('gobject-2.0'))
def fn(lib,n,r,*a):f=getattr(lib,n);f.restype=r;f.argtypes=list(a);return f
for n,r,a in [('gtk_init',None,[P,P]),('gtk_window_new',P,[I]),('gtk_window_set_title',None,[P,S]),('gtk_window_set_default_size',None,[P,I,I]),('gtk_window_move',None,[P,I,I]),('gtk_box_new',P,[I,I]),('gtk_container_add',None,[P,P]),('gtk_box_pack_start',None,[P,P,I,I,C.c_uint]),('gtk_entry_new',P,[]),('gtk_button_new_with_label',P,[S]),('gtk_label_new',P,[S]),('gtk_label_set_text',None,[P,S]),('gtk_entry_get_text',S,[P]),('gtk_widget_show_all',None,[P]),('gtk_widget_grab_focus',None,[P]),('gtk_main',None,[])]:fn(g,n,r,*a)
g.gtk_init(None,None);w=g.gtk_window_new(0);g.gtk_window_set_title(w,b'Native Fixture');g.gtk_window_set_default_size(w,500,300);g.gtk_window_move(w,120,100);box=g.gtk_box_new(1,12);g.gtk_container_add(w,box);entry=g.gtk_entry_new();button=g.gtk_button_new_with_label(b'Confirm');label=g.gtk_label_new(b'Pending')
for v in [entry,button,label]:g.gtk_box_pack_start(box,v,1,1,0)
def save(*a):
 text=g.gtk_entry_get_text(entry).decode();g.gtk_label_set_text(label,('Confirmed: '+text).encode());open(sys.argv[1],'w').write(json.dumps({'confirmed':True,'text':text}))
cb=C.CFUNCTYPE(None,P,P)(save);fn(o,'g_signal_connect_data',C.c_ulong,P,S,P,P,P,I)(button,b'clicked',C.cast(cb,P),None,None,0);g.gtk_widget_show_all(w);g.gtk_widget_grab_focus(entry);g.gtk_main()
