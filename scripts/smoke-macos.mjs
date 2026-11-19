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
const stage = path.join(temp,name), app = '/Applications/Proxy Gateway.app';
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
  const appFiles = Object.entries(manifest.files).filter(([file])=>file.startsWith('Proxy Gateway.app/'));
  async function verifyApp(directory) {
    for(const [file,hash] of appFiles) {
      const b=await fs.readFile(path.join(directory,file.slice('Proxy Gateway.app/'.length)));
      if(createHash('sha256').update(b).digest('hex')!==hash) throw Error('Installed payload mismatch: '+file);
    }
    await exec('/usr/bin/codesign',['--verify','--deep','--strict',directory]);
  }
  const dmg=path.resolve('dist',version,name+'.dmg'), pkg=path.resolve('dist',version,name+'.pkg');
  const mount=path.join(temp,'mounted'); await fs.mkdir(mount);
  await exec('/usr/bin/hdiutil',['verify',dmg]);
  await exec('/usr/bin/hdiutil',['attach','-readonly','-nobrowse','-mountpoint',mount,dmg]);
  try {
    if(await fs.readlink(path.join(mount,'Applications'))!=='/Applications') throw Error('DMG Applications shortcut missing');
    const copied=path.join(temp,'drag-copy','Proxy Gateway.app');
    await exec('/usr/bin/ditto',[path.join(mount,'Proxy Gateway.app'),copied]);
    await verifyApp(copied);
  } finally { await exec('/usr/bin/hdiutil',['detach',mount]); }
  console.log('PASS DMG verification, read-only mount, Applications shortcut and copied app');
  const expanded=path.join(temp,'expanded-pkg');
  await exec('/usr/sbin/pkgutil',['--expand',pkg,expanded]);
  const distribution=await fs.readFile(path.join(expanded,'Distribution'),'utf8');
  if(!/hostArchitectures="arm64"/.test(distribution)) throw Error('PKG architecture gate missing');
  await exec('/usr/bin/sudo',['/usr/sbin/installer','-pkg',pkg,'-target','/'],{timeout:120000});
  await verifyApp(app);
  const receipt=(await exec('/usr/sbin/pkgutil',['--pkg-info','io.github.AkparAby.proxy-gateway'])).stdout;
  if(!receipt.split('\n').includes('version: '+version.replace(/^v/,'').split('-')[0])) throw Error('PKG receipt version mismatch');
  // Install again with an existing user configuration: neither install may overwrite it.
  await fs.mkdir(data,{recursive:true});
  const preserved=JSON.stringify({schemaVersion:2,proxyPort:17890});
  await fs.writeFile(path.join(data,'config.json'),preserved);
  await exec('/usr/bin/sudo',['/usr/sbin/installer','-pkg',pkg,'-target','/'],{timeout:120000});
  if(await fs.readFile(path.join(data,'config.json'),'utf8')!==preserved) throw Error('PKG upgrade overwrote user settings');
  await verifyApp(app);
  console.log('PASS PKG installation into /Applications, receipt, upgrade and settings preservation');
  for(const file of [manager,mihomo,path.join(app,'Contents/MacOS/ProxyGateway'),path.join(stage,'Restore-Proxy.command')]) {
    if(!((await fs.stat(file)).mode & 0o111)) throw Error('Executable bit lost: '+file);
  }
  await exec('/usr/bin/codesign',['--verify','--deep','--strict',app]);
  console.log('PASS extracted ZIP permissions, file hashes and app signature');
  // Probe the required system capability independently of our application.
  // Standard virtualized Mac runners may have no accelerated NSOpenGL format.
  const probeSource=path.join(temp,'opengl-probe.m'), probe=path.join(temp,'opengl-probe');
  await fs.writeFile(probeSource,'#import <AppKit/AppKit.h>\n#include <stdio.h>\nint main(void) { @autoreleasepool { NSOpenGLPixelFormatAttribute a[]={NSOpenGLPFAAccelerated,0}; NSOpenGLPixelFormat *p=[[NSOpenGLPixelFormat alloc] initWithAttributes:a]; puts(p ? "available" : "unavailable"); return 0; } }\n');
  await exec('/usr/bin/xcrun',['clang',probeSource,'-framework','AppKit','-framework','OpenGL','-o',probe]);
  const nativeGUI=(await exec(probe,[])).stdout.trim()==='available';
  await fs.mkdir('runtime',{recursive:true});
  const report={version,installers:{pkg:'passed',dmg:'passed',upgrade:'passed'},nativeGUI:nativeGUI?'pending':'skipped',reason:nativeGUI?'':'Runner has no accelerated NSOpenGL pixel format',backend:'pending'};
  await fs.writeFile('runtime/macos-validation.json',JSON.stringify(report,null,2));
  for(const port of [17891,7892]) {
    const dir=path.join(temp,String(port)); await fs.mkdir(dir);
    const cfg=path.join(dir,'config.json');
    await fs.writeFile(cfg,JSON.stringify({'mixed-port':port,'bind-address':'127.0.0.1','allow-lan':false,mode:'direct','log-level':'error',dns:{enable:false}}));
    const child=spawn(mihomo,['-d',dir,'-f',cfg],{stdio:'inherit'}); children.push(child);
    child.on('error',e=>console.error(e));
  }
  if(nativeGUI){
    // Keep native GUI startup stderr attached to CI for early crash diagnostics.
    // The subsequent hidden launch and reopen exercise Finder's LaunchServices.
    const desktop=spawn(path.join(app,'Contents/MacOS/ProxyGateway'),['--smoke-test',path.join(temp,'desktop.png')],{stdio:'inherit'});
    children.push(desktop);
    desktop.on('error',e=>console.error('Desktop startup:',e));
  }else{
    console.log('::warning::Native Fyne window/tray tests skipped: this runner has no accelerated NSOpenGL pixel format. Headless UI tests and native backend tests still run.');
    const backend=spawn(manager,['--background'],{stdio:'inherit'});children.push(backend);
    backend.on('error',e=>console.error(e));
  }
  await waitFor(async()=>{const s=await status();token=s.token;return s.platform==='darwin'&&s.version===version&&s.config.outbounds.every(o=>s.health[o.id]?.available)},'native backend, default data path and two healthy upstreams');
  const s=await status();
  // The installer must refuse to overwrite a running installed gateway.
  let refused=false;
  try { await exec('/usr/bin/sudo',['/usr/sbin/installer','-pkg',pkg,'-target','/'],{timeout:120000}); }
  catch { refused=true; }
  if(!refused) throw Error('PKG replaced a running gateway');
  await verifyApp(app);
  console.log('PASS PKG refuses upgrades while the installed gateway is running');
  if(JSON.stringify(s.integrationOptions.map(x=>x[0]))!==JSON.stringify(['system','git','autostart'])) throw Error('Wrong Mac integration options');
  await fs.access(path.join(data,'config.json'));
  const pid = async()=>(await exec('/usr/sbin/lsof',['-t','-iTCP:17880','-sTCP:LISTEN'])).stdout.trim();
  const originalPID=await pid();
  if(nativeGUI){
  await waitFor(async()=>{try{const result=JSON.parse(await fs.readFile(path.join(temp,'desktop.png.json'),'utf8'));return result.ready&&result.rendered&&!result.error&&result.version===version}catch{return false}},'Fyne native canvas, tray lifecycle and connected state');
  await fs.mkdir('runtime',{recursive:true});
  await fs.copyFile(path.join(temp,'desktop.png'),'runtime/mac-desktop-smoke.png');
  await sleep(1500);
  await exec('/usr/bin/open',['-n',app,'--args','--background']);
  await sleep(1500);
  const sessionPath=path.join(os.homedir(),'Library/Caches/Proxy Gateway/desktop-session.json');
  const session=JSON.parse(await fs.readFile(sessionPath,'utf8'));
  await exec('/usr/bin/open',['-n',app]); await sleep(1000);
  if(JSON.parse(await fs.readFile(sessionPath,'utf8')).pid!==session.pid) throw Error('Duplicate desktop instance');
  console.log('PASS hidden desktop activation keeps a single UI process');
  await exec('/usr/bin/open',['-n',app]); await sleep(1500);
  report.nativeGUI='passed';
  }
  await exec(manager,['--background']);
  if(await pid()!==originalPID) throw Error('Duplicate manager');
  console.log('PASS background launch reuses existing manager');
  await exec('node',['scripts/verify-port.mjs'],{timeout:180000}).then(r=>console.log(r.stdout));
  await exec('node',['scripts/verify-failover.mjs'],{timeout:240000}).then(r=>console.log(r.stdout));
  await exec(manager,['--restore']);
  await exec(path.join(app,'Contents/MacOS/ProxyGateway'),['--restore']);
  console.log('PASS emergency restoration while manager is running');
  // Runtime writes must not alter the signed .app.
  await exec('/usr/bin/codesign',['--verify','--deep','--strict',app]);
  console.log('PASS app remains sealed after runtime and failover tests');
  report.backend='passed';
  await fs.writeFile('runtime/macos-validation.json',JSON.stringify(report,null,2));
} finally {
  if(token) await fetch('http://127.0.0.1:17880/api/stop',{method:'POST',headers:{'Content-Type':'application/json','X-Gateway-Token':token},body:'{}'}).catch(()=>{});
  await sleep(1500);
  for(const child of children) child.kill('SIGTERM');
  // Logs are CI diagnostics only; never copied into a release package.
  for(const file of ['desktop.log','gateway.log','launcher.log','mihomo.log']) {
    try{console.log(`--- ${file} ---\n`+(await fs.readFile(path.join(data,'runtime',file),'utf8')).slice(-6000))}catch{}
  }
}
