// Run against a disposable gateway or during a maintenance window. Restores
// the original listener even if a verification fails. Never stops upstreams.
import net from 'node:net';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
const run=promisify(execFile), base='http://127.0.0.1:17880';
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const state=()=>fetch(base+'/api/status').then(r=>r.json());
const initial=await state(), original=initial.config;
if(!original.proxyPort)throw Error('Manager does not support configurable ports');
async function post(c,ok=true){
 const response=await fetch(base+'/api/config',{method:'POST',headers:{'Content-Type':'application/json','X-Gateway-Token':initial.token},body:JSON.stringify(c)});
 const value=await response.json();if(response.ok!==ok)throw Error(JSON.stringify(value));
}
async function healthy(port){
 for(let n=0;n<120;n++){const s=await state();if(s.proxy===`127.0.0.1:${port}`&&s.selected!=='REJECT'&&s.health[s.selected]?.available)return;await sleep(250)}
 throw Error('Gateway not healthy at port '+port);
}
const occupied=net.createServer(s=>s.destroy());
await new Promise((resolve,reject)=>{occupied.once('error',reject);occupied.listen(0,'127.0.0.1',resolve)});
const newPort=occupied.address().port;
let changed=false;
try{
 for(const port of [80,65536,17880,17881,17882,original.outbounds[0].port,newPort])await post({...original,proxyPort:port},false);
 if(JSON.stringify((await state()).config)!==JSON.stringify(original))throw Error('Invalid save mutated configuration');
 console.log('PASS invalid, reserved, upstream and occupied ports rejected without mutation');
 await new Promise(resolve=>occupied.close(resolve));
 changed=true;await post({...original,proxyPort:newPort});await healthy(newPort);
 for(const scheme of ['http','socks5h']){
  const {stdout}=await run(process.platform==='win32'?'curl.exe':'curl',['--proxy',`${scheme}://127.0.0.1:${newPort}`,'--noproxy','','--max-time','15','--fail','-sS','-o',process.platform==='win32'?'NUL':'/dev/null','-w','%{http_code}','https://www.gstatic.com/generate_204']);
  if(stdout!=='204')throw Error('Proxy request failed: '+scheme);
 }
 const oldOpen=await new Promise(resolve=>{const s=net.connect(original.proxyPort,'127.0.0.1');s.setTimeout(1000);s.once('connect',()=>{s.destroy();resolve(true)});s.once('error',()=>resolve(false));s.once('timeout',()=>{s.destroy();resolve(false)})});
 if(oldOpen)throw Error('Old listener still active');
 console.log('PASS custom port serves HTTP and SOCKS5, old listener closed');
}finally{
 occupied.close();
 if(changed){await post(original);await healthy(original.proxyPort);console.log('PASS original gateway port and configuration restored')}
}
