import { mountCoreConsole } from './client.js';
const root=document.getElementById('app');
root.querySelector('form').addEventListener('submit',async event=>{
 event.preventDefault();const token=root.querySelector('#token').value;
 const request=async(path,method='GET',body)=>{
  if(!path.startsWith('/v1/'))throw new Error('Invalid API path');
  const response=await fetch(path,{method,headers:{Authorization:'Bearer '+token,'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body),redirect:'error'});
  if(response.status===204)return null;const value=await response.json();if(!response.ok)throw new Error(value.error||`HTTP ${response.status}`);return value;
 };
 try{await request('/v1/health');mountCoreConsole(root,request,{downloadArtifact:async(host,session,id,name)=>{
  const r=await fetch(`${host?'/v1/remote-artifact?host='+encodeURIComponent(host)+'&':'/v1/artifact?'}session=${encodeURIComponent(session)}&id=${encodeURIComponent(id)}`,{headers:{Authorization:'Bearer '+token},redirect:'error'});if(!r.ok)throw new Error((await r.json()).error);const url=URL.createObjectURL(await r.blob());const a=document.createElement('a');a.href=url;a.download=name;a.click();setTimeout(()=>URL.revokeObjectURL(url),30000);
 }});}catch(e){root.querySelector('.error').textContent=e.message;}
});
