// Controlled TCP relays simulate failed upstreams without stopping either client.
import net from 'node:net';
import fs from 'node:fs/promises';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
const run=promisify(execFile),base='http://127.0.0.1:17880';
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const initial=await fetch(base+'/api/status').then(r=>r.json());
const original=initial.config,token=initial.token,results=[];
async function post(path,body={}){const r=await fetch(base+path,{method:'POST',headers:{'Content-Type':'application/json','X-Gateway-Token':token},body:JSON.stringify(body)});const b=await r.json();if(!r.ok)throw Error(JSON.stringify(b));return b}
async function state(){return fetch(base+'/api/status').then(r=>r.json())}
async function waitFor(test,label,ms=25000){const deadline=Date.now()+ms;while(Date.now()<deadline){const s=await state();if(test(s)){console.log('PASS',label);results.push({test:label,result:'passed'});return s}await sleep(500)}throw Error('Timeout: '+label)}
async function relay(port,target){const control={up:true,sockets:new Set()};control.server=net.createServer(socket=>{if(!control.up){socket.destroy();return}const upstream=net.connect(target,'127.0.0.1');control.sockets.add(socket);control.sockets.add(upstream);socket.on('error',()=>upstream.destroy());upstream.on('error',()=>socket.destroy());socket.on('close',()=>{upstream.destroy();control.sockets.delete(socket)});upstream.on('close',()=>{socket.destroy();control.sockets.delete(upstream)});socket.pipe(upstream);upstream.pipe(socket)});await new Promise((resolve,reject)=>{control.server.once('error',reject);control.server.listen(port,'127.0.0.1',resolve)});control.set=up=>{control.up=up;if(!up)for(const s of control.sockets)s.destroy()};return control}
const a=await relay(17892,original.speedcat.port),b=await relay(17893,original.clash.port);
const config={...original,interval:3,timeout:1500,failures:2,recovery:5,mode:'auto',autoReturn:true,speedcat:{...original.speedcat,port:17892},clash:{...original.clash,port:17893}};
try{
 await post('/api/config',config);await waitFor(s=>s.selected==='SpeedCat'&&s.health.Clash.available,'primary selected; both upstreams healthy');
 a.set(false);await post('/api/check');await waitFor(s=>s.health.SpeedCat.failures===1&&s.selected==='SpeedCat','one failed check does not switch');
 await waitFor(s=>s.selected==='Clash','primary failure switches to backup');
 const good=await run('curl.exe',['--proxy','http://127.0.0.1:17890','--max-time','10','-s','-o','NUL','-w','%{http_code}','https://www.gstatic.com/generate_204']);if(good.stdout!=='204')throw Error('Backup HTTP failed');results.push({test:'HTTPS works through backup',result:'passed'});console.log('PASS HTTPS works through backup');
 a.set(true);await post('/api/check');await waitFor(s=>s.health.SpeedCat.lastOK&&s.selected==='Clash','recovery observation retains backup');await waitFor(s=>s.selected==='SpeedCat','stable primary automatically restored');
 config.autoReturn=false;await post('/api/config',config);await waitFor(s=>s.selected==='SpeedCat'&&s.health.Clash.available,'sticky test baseline');a.set(false);await post('/api/check');await waitFor(s=>s.selected==='Clash','sticky mode still fails over');a.set(true);await post('/api/check');await waitFor(s=>s.health.SpeedCat.available,'primary healthy again');await sleep(7000);if((await state()).selected!=='Clash')throw Error('Sticky mode returned automatically');results.push({test:'sticky mode retains backup after recovery',result:'passed'});console.log('PASS sticky mode retains backup');
 await post('/api/return');await waitFor(s=>s.selected==='SpeedCat'&&s.config.mode==='auto','manual return preserves automatic failover');
 a.set(false);b.set(false);await post('/api/check');await waitFor(s=>s.selected==='REJECT','both failed: reject, never DIRECT');let rejected=false;try{await run('curl.exe',['--proxy','http://127.0.0.1:17890','--max-time','6','--fail','-s','-o','NUL','https://www.gstatic.com/generate_204'])}catch{rejected=true}if(!rejected)throw Error('Both-down request unexpectedly succeeded');results.push({test:'both-down HTTPS request fails closed',result:'passed'});console.log('PASS both-down request rejected');
}finally{
 a.set(true);b.set(true);await post('/api/config',original);await waitFor(s=>s.health.SpeedCat.available&&s.health.Clash.available,'original upstream configuration restored');await post('/api/return');for(const r of[a,b]){r.set(false);r.server.close()}
 await fs.writeFile(new URL('../runtime/failover-results.json',import.meta.url),JSON.stringify({time:new Date().toISOString(),results},null,2));
}
