// Isolated browser smoke test. Install Playwright outside production dependencies,
// then set PLAYWRIGHT_MODULE and optionally PLAYWRIGHT_CHROMIUM to local paths.
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const {chromium} = require(process.env.PLAYWRIGHT_MODULE || 'playwright');

(async () => {
  const root = path.resolve(__dirname, '..');
  const output = path.join(root, 'dist', 'routing-validation');
  await fs.mkdir(output, {recursive:true});
  const browser = await chromium.launch({headless:true, ...(process.env.PLAYWRIGHT_CHROMIUM ? {executablePath:process.env.PLAYWRIGHT_CHROMIUM} : {})});
  try {
    const page = await browser.newPage({viewport:{width:1200,height:900}});
    const errors = []; page.on('pageerror', error => errors.push(error.message));
    let saved;
    const applications=[{id:'codex-desktop',name:'Codex 桌面版',available:true,kind:'desktop',processPaths:['C:\\Apps\\Codex.exe']},{id:'claude-cli',name:'Claude Code CLI',available:true,kind:'cli',processPaths:['C:\\Tools\\claude.exe']},{id:'claude-desktop',name:'Claude 桌面版',available:false,kind:'desktop',processPaths:[]}];
    let config = {
      schemaVersion:2, proxyPort:17890, mode:'auto', autoReturn:true, interval:10, timeout:3000, failures:3, recovery:60,
      outbounds:[{id:'a',name:'代理 A',type:'http',host:'127.0.0.1',port:7890},{id:'b',name:'代理 B',type:'http',host:'127.0.0.1',port:7891}],
      targets:['https://www.gstatic.com/generate_204'],directDomains:['localhost','local'],
      latencyPolicy:{enabled:false,rounds:3,minImprovement:20,cooldown:30},appRules:[],
    };
    await page.route('http://127.0.0.1:19779/**', async route => {
      const request = route.request(), pathname = new URL(request.url()).pathname;
      if (pathname === '/api/status') {
        await route.fulfill({json:{version:'UI fixture',config,applications,platform:'windows',integrations:{system:true,terminal:true},proxy:'127.0.0.1:17890',selected:'a',reason:'验证样例',token:'fixture',health:{a:{available:true,checked:true,lastOK:true,latency:100,smoothedLatency:110},b:{available:true,checked:true,lastOK:true,latency:60,smoothedLatency:70}},events:[],integrationOptions:[],appStatus:{}}});
      } else if (pathname === '/api/config') {
        saved = request.postDataJSON(); config = structuredClone(saved);
        await route.fulfill({json:{ok:true}});
      } else {
        const file = pathname === '/' ? 'index.html' : pathname.slice(1);
        const contentType = file.endsWith('.js') ? 'application/javascript' : file.endsWith('.css') ? 'text/css' : 'text/html';
        await route.fulfill({body:await fs.readFile(path.join(root,'web',file)),contentType});
      }
    });
    await page.goto('http://127.0.0.1:19779/');
    await page.waitForFunction(() => document.querySelector('#saved').textContent === '配置已保存');
    const field = name => page.locator(`[name="${name}"]`);
    assert.equal(await field('latencyRounds').isDisabled(),true);
    await field('latencyEnabled').check();
    assert.equal(await field('autoReturn').isDisabled(),true);
    assert.equal(await field('latencyRounds').isDisabled(),false);
    await field('latencyDifference').fill('35');
    await field('mode').selectOption('a');
    assert.equal(await field('latencyEnabled').isDisabled(),true);
    await field('mode').selectOption('auto');
    await page.locator('.check-label').scrollIntoViewIfNeeded();
    await page.screenshot({path:path.join(output,'web-latency-settings.png')});
    await page.locator('#add-codex').click();
    let rows = page.locator('#app-rule-editor fieldset');
    assert.equal(await rows.count(),1);
    assert.equal(await rows.nth(0).locator('[data-application]').inputValue(),'codex-desktop');
    assert.equal(await rows.nth(0).locator('input,textarea').count(),0,'automatic binding must not ask for paths or ports');
    await rows.nth(0).locator('[data-rule-outbound]').selectOption('b');
    await page.locator('#add-claude').click();
    assert.equal(await rows.count(),2);
    assert.equal(await rows.nth(1).locator('[data-application]').inputValue(),'claude-cli');
    // A referenced outbound cannot be silently removed.
    await page.locator('#outbound-editor fieldset').nth(1).locator('[data-action=delete]').click();
    assert.equal(await page.locator('#outbound-editor fieldset').count(),2);
    await page.getByRole('button',{name:'保存全部设置',exact:true}).click();
    await page.waitForFunction(() => document.querySelector('#saved').textContent === '配置已保存');
    assert.equal(saved.latencyPolicy.enabled,true);
    assert.equal(saved.latencyPolicy.minImprovement,35);
    assert.equal(saved.appRules[0].outboundId,'b');
    assert.equal(saved.appRules[0].applicationId,'codex-desktop');
    assert.equal(saved.appRules[0].match,'application');
    assert.equal(saved.appRules[1].applicationId,'claude-cli');
    // Status polling and unrelated outbound edits must not erase application drafts.
    await rows.nth(0).locator('[data-rule-outbound]').selectOption('a');
    await page.evaluate(() => refresh());
    assert.equal(await rows.nth(0).locator('[data-rule-outbound]').inputValue(),'a');
    await page.locator('#discard').click();
    assert.equal(await rows.nth(0).locator('[data-rule-outbound]').inputValue(),'b');
    await page.locator('#app-rule-editor').scrollIntoViewIfNeeded();
    await page.screenshot({path:path.join(output,'web-app-rules.png')});
    await page.setViewportSize({width:390,height:844});
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),true,'mobile overflow');
    await page.screenshot({path:path.join(output,'web-app-rules-mobile.png')});
    await page.locator('#add-advanced-rule').click();
    assert.equal(await rows.nth(2).locator('input[type=number]').count(),1,'advanced entry remains available');
    assert.deepEqual(errors,[]);
    console.log('PASS: latency controls, app editing, fixed bindings, save/discard, refresh isolation, mobile layout.');
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode=1; });
