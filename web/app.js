let state, loaded = false, busy = false, draft = [], noticeTimer;
const $ = selector => document.querySelector(selector);
const form = $('#settings');
let apps = [], appSignature = "";
function element(tag, text, className) {
  const node = document.createElement(tag);
  if (text !== undefined) node.textContent = text;
  if (className) node.className = className;
  return node;
}
function notice(text, error = false) {
  clearTimeout(noticeTimer);
  const node = $('#notice');
  node.hidden = false;
  node.textContent = text;
  node.classList.toggle('error', error);
  if (!error) noticeTimer = setTimeout(() => node.hidden = true, 6000);
}
function dirty() { $('#saved').textContent = '有未保存的修改'; }
async function api(path, body = {}) {
  if (!state) throw Error('请等待网关连接');
  const response = await fetch(path, {
    method: 'POST',
    headers: {'Content-Type': 'application/json', 'X-Gateway-Token': state.token},
    body: JSON.stringify(body),
  });
  const value = await response.json();
  if (!response.ok) throw Error(value.error || '操作失败');
  return value;
}
function syncModes(selected = form.elements.mode.value) {
  const select = form.elements.mode;
  select.replaceChildren(new Option('自动按优先级切换', 'auto'));
  for (const outbound of draft) select.add(new Option('固定：' + (outbound.name || '未命名出口'), outbound.id));
  select.value = draft.some(o => o.id === selected) ? selected : 'auto';
}
function editorField(outbound, key, label, type = 'text') {
  const wrapper = element('label', label);
  const input = key === 'type' ? element('select') : element('input');
  input.name = `${outbound.id}-${key}`;
  input.dataset.field = key;
  if (key === 'type') {
    input.add(new Option('HTTP', 'http'));
    input.add(new Option('SOCKS5', 'socks5'));
  } else {
    input.type = type;
    input.required = true;
    if (key === 'name') input.maxLength = 60;
    if (key === 'host') input.placeholder = '127.0.0.1 或 ::1';
    if (key === 'port') { input.min = 1; input.max = 65535; }
  }
  input.value = outbound[key];
  input.addEventListener('input', () => {
    outbound[key] = key === 'port' ? Number(input.value) : input.value;
    if (key === 'name') syncModes();
    dirty();
  });
  wrapper.append(input);
  return wrapper;
}
function renderEditor() {
  const container = $('#outbound-editor');
  container.replaceChildren();
  draft.forEach((outbound, index) => {
    const row = element('fieldset', undefined, 'outbound-row');
    row.dataset.outboundId = outbound.id;
    row.append(element('legend', `优先级 ${index + 1} · ${index === 0 ? '首选出口' : '备用出口'}`));
    const fields = element('div', undefined, 'outbound-fields');
    fields.append(editorField(outbound, 'name', '代理应用名称'), editorField(outbound, 'type', '协议'),
      editorField(outbound, 'host', '本机地址'), editorField(outbound, 'port', '端口', 'number'));
    const actions = element('div', undefined, 'outbound-actions');
    for (const [label, action, disabled] of [
      ['上移', 'up', index === 0], ['下移', 'down', index === draft.length - 1], ['删除', 'delete', draft.length === 1],
    ]) {
      const button = element('button', label, action === 'delete' ? 'danger' : undefined);
      button.type = 'button'; button.dataset.action = action; button.disabled = disabled;
      button.setAttribute('aria-label', `${label}第 ${index + 1} 个出口`);
      button.addEventListener('click', () => {
        if (busy) return;
        if (action === 'delete') {
          if (form.elements.mode.value === outbound.id) notice('已移除固定出口；保存后将使用自动模式。');
          draft.splice(index, 1);
        } else {
          const next = index + (action === 'up' ? -1 : 1);
          [draft[index], draft[next]] = [draft[next], draft[index]];
        }
        syncModes(); renderEditor(); dirty();
      });
      actions.append(button);
    }
    row.append(fields, actions); container.append(row);
  });
  $('#add-outbound').disabled = draft.length >= 16;
}
function fill(config) {
  draft = structuredClone(config.outbounds);
  for (const key of ['proxyPort', 'autoReturn', 'recovery', 'interval', 'timeout', 'failures']) form.elements[key].value = String(config[key]);
  form.elements.targets.value = config.targets.join('\n');
  form.elements.directDomains.value = config.directDomains.join('\n');
  syncModes(config.mode); renderEditor();
  $('#saved').textContent = '配置已保存';
}
function controls() {
  $('#config-fields').disabled = busy || !state;
  $('#check').disabled = busy || !state;
  $('#stop').disabled = busy || !state;
  const first = state?.config.outbounds[0]?.id;
  $('#return').disabled = busy || state?.config.mode !== 'auto' || !state?.health[first]?.available;
  for (const button of document.querySelectorAll('[data-app]')) button.disabled = busy || !state;
}
function display(status) {
  if (!Array.isArray(status.config.outbounds)) throw Error('版本不匹配，请升级网关后刷新页面');
  state = status;
  $('#proxy-address').textContent = status.proxy;
  renderIntegrations(status.integrationOptions || []);
  $('#connection').textContent = '网关运行中'; $('#connection').className = 'pill ok';
  $('#version').textContent = status.version || '';
  document.body.classList.remove('offline');
  const active = status.config.outbounds.find(o => o.id === status.selected);
  $('#selected').textContent = active?.name || '暂无可用出口';
  $('#reason').textContent = status.error || status.reason;
  const cards = $('#health-cards'); cards.replaceChildren();
  status.config.outbounds.forEach((outbound, index) => {
    const health = status.health[outbound.id] || {};
    const card = element('article', undefined, 'card');
    card.dataset.outboundId = outbound.id;
    card.classList.toggle('active', status.selected === outbound.id);
    const heading = element('div', undefined, 'row');
    const healthText = !health.checked ? '待检测' : health.available ? (health.lastOK ? '可用' : '观察中') : '不可用';
    heading.append(element('h3', outbound.name), element('span', healthText, 'pill ' + (health.available ? 'ok' : health.checked ? 'bad' : '')));
    const host = outbound.host.includes(':') ? `[${outbound.host}]` : outbound.host;
    card.append(heading, element('code', `${outbound.type}://${host}:${outbound.port}`),
      element('p', `优先级 ${index + 1} · ${index === 0 ? '首选' : '备用'} · ${health.lastOK ? `${health.latency} ms` : `连续失败 ${health.failures || 0} 次`}`),
      element('small', health.detail || '等待检测'));
    cards.append(card);
  });
  if (!loaded) { fill(status.config); loaded = true; }
  const events = $('#events'); events.replaceChildren();
  for (const item of status.events || []) {
    const row = element('li');
    row.append(element('time', new Date(item.time).toLocaleTimeString()), element('span', item.message));
    events.append(row);
  }
  for (const [key] of apps) {
    const button = $(`[data-app="${key}"]`);
    const enabled = Boolean(status.integrations?.[key]);
    button.textContent = enabled ? '停用并恢复' : '启用'; button.dataset.enabled = String(enabled);
  }
  controls();
}
async function refresh() {
  try {
    const response = await fetch('/api/status');
    if (!response.ok) throw Error('网关已离线');
    display(await response.json());
  } catch (error) {
    document.body.classList.add('offline');
    $('#connection').textContent = '网关已离线'; $('#connection').className = 'pill bad';
    if (error.message.includes('版本不匹配')) notice(error.message, true);
  }
}
async function act(fn) {
  if (busy || !state) return;
  busy = true; controls();
  try { await fn(); await refresh(); }
  catch (error) { notice(error.message, true); }
  finally { busy = false; controls(); }
}
function renderIntegrations(options) {
  const signature = JSON.stringify(options);
  if (signature === appSignature) return;
  appSignature = signature; apps = options;
  $('#integrations').replaceChildren();
for (const [key, title, description] of apps) {
  const row = element('div', undefined, 'integration'), text = element('div');
  text.append(element('strong', title), element('p', description));
  const button = element('button', '启用'); button.dataset.app = key; button.disabled = true;
  button.addEventListener('click', () => act(async () => {
    const result = await api('/api/integration', {name: key, enabled: button.dataset.enabled !== 'true'});
    notice(result.message || '已完成');
  }));
  row.append(text, button); $('#integrations').append(row);
}

}
$('#add-outbound').onclick = () => {
  if (busy || draft.length >= 16) return;
  let number = 1, port = 7890;
  while (draft.some(o => o.name === `代理 ${number}`)) number++;
  while (draft.some(o => o.port === port) || [17880, 17881, 17882, Number(form.elements.proxyPort.value)].includes(port)) port++;
  draft.push({id: crypto.randomUUID(), name: `代理 ${number}`, type: 'http', host: '127.0.0.1', port});
  syncModes(); renderEditor(); dirty();
  $('#outbound-editor').lastElementChild.querySelector('input').focus();
};
$('#discard').onclick = () => { if (!busy && state) fill(state.config); };
form.addEventListener('input', dirty);
form.addEventListener('submit', event => {
  event.preventDefault();
  act(async () => {
    const config = structuredClone(state.config);
    config.outbounds = draft.map(o => ({...o, name: o.name.trim(), host: o.host.trim()}));
    config.mode = form.elements.mode.value;
    config.autoReturn = form.elements.autoReturn.value === 'true';
    for (const key of ['proxyPort', 'recovery', 'interval', 'timeout', 'failures']) config[key] = Number(form.elements[key].value);
    for (const key of ['targets', 'directDomains']) config[key] = form.elements[key].value.split('\n').map(s => s.trim()).filter(Boolean);
    await api('/api/config', config);
    loaded = false;
    notice(config.proxyPort !== state.config.proxyPort ? '端口已更新，已同步启用的接入。请重开终端、重建容器，并修改手动代理地址。' : '设置已保存，正在重新检查所有出口。');
  });
});
$('#check').onclick = () => act(async () => { await api('/api/check'); notice('已开始检查所有出口。'); });
$('#return').onclick = () => act(async () => { await api('/api/return'); notice('已切回首选出口，继续自动故障切换。'); });
$('#stop').onclick = () => {
  if (confirm('恢复由网关管理的接入设置并停止网关？')) act(async () => notice((await api('/api/stop')).message));
};
refresh(); setInterval(refresh, 3000);
