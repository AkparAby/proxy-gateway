// Run only against a disposable gateway or during a maintenance window.
// Three local TCP relays simulate independent failures without stopping clients.
import net from 'node:net';
import fs from 'node:fs/promises';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
const run = promisify(execFile), base = 'http://127.0.0.1:17880';
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
const initial = await fetch(base + '/api/status').then(r => r.json());
const original = initial.config, token = initial.token, results = [], relays = [];
if (!Array.isArray(original.outbounds) || original.outbounds.length < 2) throw Error('Requires v0.2+ and at least two healthy upstreams.');
if (!original.outbounds.slice(0, 2).every(o => initial.health[o.id]?.available)) throw Error('The first two upstreams must be healthy before testing.');
async function post(path, body = {}) {
  const response = await fetch(base + path, {method: 'POST', headers: {'Content-Type': 'application/json', 'X-Gateway-Token': token}, body: JSON.stringify(body)});
  const data = await response.json(); if (!response.ok) throw Error(JSON.stringify(data)); return data;
}
async function state() { return fetch(base + '/api/status').then(r => r.json()); }
function pass(label) { console.log('PASS', label); results.push({test: label, result: 'passed'}); }
async function waitFor(test, label, ms = 25000) {
  const deadline = Date.now() + ms;
  while (Date.now() < deadline) { const s = await state(); if (test(s)) { pass(label); return s; } await sleep(300); }
  throw Error('Timeout: ' + label);
}
async function relay(target) {
  const control = {up: true, sockets: new Set()};
  control.server = net.createServer(socket => {
    if (!control.up) { socket.destroy(); return; }
    const upstream = net.connect(target.port, target.host);
    control.sockets.add(socket); control.sockets.add(upstream);
    socket.on('error', () => upstream.destroy()); upstream.on('error', () => socket.destroy());
    socket.on('close', () => { upstream.destroy(); control.sockets.delete(socket); });
    upstream.on('close', () => { socket.destroy(); control.sockets.delete(upstream); });
    socket.pipe(upstream); upstream.pipe(socket);
  });
  await new Promise((resolve, reject) => { control.server.once('error', reject); control.server.listen(0, '127.0.0.1', resolve); });
  control.port = control.server.address().port;
  control.set = up => { control.up = up; if (!up) for (const socket of control.sockets) socket.destroy(); };
  relays.push(control); return control;
}
async function https(proxy, succeeds = true) {
  let ok = false;
  try {
    const result = await run(process.platform === 'win32' ? 'curl.exe' : 'curl', ['--proxy', proxy, '--noproxy', '', '--max-time', '10', '--fail', '-sS', '-o', process.platform === 'win32' ? 'NUL' : '/dev/null', '-w', '%{http_code}', 'https://www.gstatic.com/generate_204']);
    ok = result.stdout === '204';
  } catch { }
  if (ok !== succeeds) throw Error(`Unexpected HTTPS result (${proxy}): ${ok}`);
}
let modified = false;
try {
  const a = await relay(original.outbounds[0]), b = await relay(original.outbounds[1]), c = await relay(original.outbounds[0]);
  const config = {...original, interval: 3, timeout: 1500, failures: 2, recovery: 5, mode: 'auto', autoReturn: true,
    outbounds: [a, b, c].map((r, index) => ({...original.outbounds[index === 1 ? 1 : 0], id: `test-${index + 1}`, name: `测试代理 ${index + 1}`, host: '127.0.0.1', port: r.port}))};
  const [first, second, third] = config.outbounds.map(o => o.id);
  modified = true; await post('/api/config', config);
  await waitFor(s => s.selected === first && config.outbounds.every(o => s.health[o.id]?.available), 'three generic exits healthy; first priority selected');
  a.set(false); await post('/api/check');
  await waitFor(s => s.health[first].failures === 1 && s.selected === first, 'single failed check retains current exit');
  await waitFor(s => s.selected === second, 'first exit failure selects second');
  b.set(false); await post('/api/check');
  await waitFor(s => s.selected === third, 'second exit failure selects third');
  await https('http://127.0.0.1:17890'); await https('socks5h://127.0.0.1:17890'); pass('HTTP and SOCKS5 work through third exit');
  b.set(true); await post('/api/check');
  await waitFor(s => s.health[second].lastOK && s.selected === third, 'third exit retained during recovery observation');
  await waitFor(s => s.selected === second, 'stable second priority recovered');
  a.set(true); await post('/api/check');
  await waitFor(s => s.selected === first, 'stable first priority recovered');
  config.autoReturn = false; await post('/api/config', config);
  await waitFor(s => s.selected === first && s.health[third].available, 'sticky test baseline');
  a.set(false); b.set(false); await post('/api/check');
  await waitFor(s => s.selected === third, 'sticky mode skips two failed exits');
  a.set(true); b.set(true); await post('/api/check');
  await waitFor(s => s.health[first].lastOK && s.health[second].lastOK, 'higher priorities recovered');
  await sleep(6500); if ((await state()).selected !== third) throw Error('Sticky mode returned automatically');
  pass('sticky mode retains third exit after recovery');
  await post('/api/return'); await waitFor(s => s.selected === first && s.config.mode === 'auto', 'manual return preserves automatic mode');
  config.mode = third; await post('/api/config', config);
  await waitFor(s => s.selected === third, 'any exit can be fixed');
  c.set(false); await post('/api/check');
  await waitFor(s => s.selected === 'REJECT' && s.health[first].available, 'fixed failed exit never falls back');
  config.mode = 'auto'; a.set(false); b.set(false); await post('/api/config', config);
  await waitFor(s => s.selected === 'REJECT' && config.outbounds.every(o => s.health[o.id]?.checked), 'all exits failed: REJECT');
  await https('http://127.0.0.1:17890', false); pass('all-down HTTPS fails closed');
  for (const r of relays) r.set(true);
  config.outbounds.reverse(); config.outbounds[0].name = 'DIRECT'; await post('/api/config', config);
  await waitFor(s => s.selected === third && s.config.outbounds[0].name === 'DIRECT', 'reorder and reserved display name are safe');
  config.outbounds.shift(); await post('/api/config', config);
  await waitFor(s => s.selected === second && !s.health[third], 'deleting current exit removes stale health and selects next');
} finally {
  try {
    for (const r of relays) r.set(true);
    if (modified) {
      await post('/api/config', original);
      await waitFor(s => original.outbounds.slice(0, 2).every(o => s.health[o.id]?.available), 'original configuration restored');
    }
  } finally {
    for (const r of relays) { r.set(false); r.server.close(); }
    const path = new URL('../runtime/failover-results.json', import.meta.url);
    await fs.mkdir(new URL('../runtime/', import.meta.url), {recursive: true});
    await fs.writeFile(path, JSON.stringify({time: new Date().toISOString(), results}, null, 2));
  }
}
