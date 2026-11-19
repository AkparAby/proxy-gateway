// Only run on a disposable macOS CI runner: this uses its local gateway ports.
import {spawn, execFile} from 'node:child_process';
import {promisify} from 'node:util';
import fs from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import {createHash} from 'node:crypto';
const exec = promisify(execFile), sleep = ms => new Promise(r => setTimeout(r,ms));
if (process.platform !== 'darwin' || process.arch !== 'arm64' || process.env.CI !== 'true') throw Error('Requires disposable Apple Silicon CI runner');
const version = process.argv[2], name = `proxy-gateway-${version}-macos-arm64`;
const temp = await fs.mkdtemp(path.join(os.tmpdir(),'gateway-smoke-'));
await exec('/usr/bin/ditto',['-x','-k',path.resolve('dist',version,name+'.zip'),temp]);
const stage = path.join(temp,name), app = path.join(stage,'Proxy Gateway.app');
const manager = path.join(app,'Contents/Resources/proxy-gateway');
const mihomo = path.join(app,'Contents/Resources/mihomo');
const data = path.join(os.homedir(),'Library/Application Support/Proxy Gateway');
const children = [];
let token;
async function status() { return fetch('http://127.0.0.1:17880/api/status').then(r=>r.json()); }
async function waitFor(fn,label,ms=60000) {
  const end=Date.now()+ms;
  while(Date.now()<end){try{if(await fn()){console.log('PASS',label);return}}catch{} await sleep(300)}
  throw Error('Timeout: '+label);
}
try {
  const manifest = JSON.parse(await fs.readFile(path.join(stage,'release-manifest.json'),'utf8'));
  for(const [file,hash] of Object.entries(manifest.files)) {
    const b=await fs.readFile(path.join(stage,file));
    if(createHash('sha256').update(b).digest('hex')!==hash) throw Error('Manifest mismatch: '+file);
    if(/(^|\/)(config\.json|.*\.log|integrations-state\.json|selection\.json)$/.test(file)) throw Error('Local data in archive');
  }
  for(const file of [manager,mihomo,path.join(app,'Contents/MacOS/ProxyGateway'),path.join(stage,'Restore-Proxy.command')]) {
    if(!((await fs.stat(file)).mode & 0o111)) throw Error('Executable bit lost: '+file);
  }
  await exec('/usr/bin/codesign',['--verify','--deep','--strict',app]);
  console.log('PASS extracted ZIP permissions, file hashes and app signature');
  for(const port of [17891,7892]) {
    const dir=path.join(temp,String(port)); await fs.mkdir(dir);
    const cfg=path.join(dir,'config.json');
    await fs.writeFile(cfg,JSON.stringify({'mixed-port':port,'bind-address':'127.0.0.1','allow-lan':false,mode:'direct','log-level':'error',dns:{enable:false}}));
    const child=spawn(mihomo,['-d',dir,'-f',cfg],{stdio:'inherit'}); children.push(child);
    child.on('error',e=>console.error(e));
  }
  // LaunchServices opens the actual .app as Finder does, not only the raw binary.
  await exec('/usr/bin/open',['-n',app]);
  await waitFor(async()=>{const s=await status();token=s.token;return s.platform==='darwin'&&s.version===version&&s.config.outbounds.every(o=>s.health[o.id]?.available)},'native .app launch, default data path and two healthy upstreams');
  const s=await status();
  if(JSON.stringify(s.integrationOptions.map(x=>x[0]))!==JSON.stringify(['system','git','autostart'])) throw Error('Wrong Mac integration options');
  await fs.access(path.join(data,'config.json'));
  const pid = async()=>(await exec('/usr/sbin/lsof',['-t','-iTCP:17880','-sTCP:LISTEN'])).stdout.trim();
  const originalPID=await pid();
  await exec('/usr/bin/open',['-n',app]); await sleep(1500);
  await exec(manager,['--background']);
  if(await pid()!==originalPID) throw Error('Duplicate manager');
  console.log('PASS reopen app and background launch reuse existing manager');
  await exec('node',['scripts/verify-failover.mjs'],{timeout:240000}).then(r=>console.log(r.stdout));
  await exec(manager,['--restore']);
  console.log('PASS emergency restoration while manager is running');
  // Runtime writes must not alter the signed .app.
  await exec('/usr/bin/codesign',['--verify','--deep','--strict',app]);
  console.log('PASS app remains sealed after runtime and failover tests');
} finally {
  if(token) await fetch('http://127.0.0.1:17880/api/stop',{method:'POST',headers:{'Content-Type':'application/json','X-Gateway-Token':token},body:'{}'}).catch(()=>{});
  await sleep(1500);
  for(const child of children) child.kill('SIGTERM');
  // Logs are CI diagnostics only; never copied into a release package.
  for(const file of ['gateway.log','launcher.log','mihomo.log']) {
    try{console.log(`--- ${file} ---\n`+(await fs.readFile(path.join(data,'runtime',file),'utf8')).slice(-6000))}catch{}
  }
}
