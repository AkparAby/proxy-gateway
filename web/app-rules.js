function syncAppOutbounds() {
  for (const select of document.querySelectorAll('[data-rule-outbound]')) {
    const rule = ruleDraft.find(a => a.id === select.dataset.ruleOutbound);
    select.replaceChildren(...draft.map(o => new Option(o.name || '未命名出口', o.id)));
    select.value = rule?.outboundId || '';
  }
}

function addAppRule(name, advanced = false) {
  if (busy || !draft.length || ruleDraft.length >= 32) return;
  let port = 17900;
  const used = [17880, 17881, 17882, Number(form.elements.proxyPort.value), ...draft.map(o => o.port), ...ruleDraft.filter(a => a.match === 'port').map(a => a.listenPort)];
  while (used.includes(port)) port++;
  const available = (state?.applications || []).filter(a => a.available);
  const app = available.find(a => !name || a.id.startsWith(name.toLowerCase()));
  if (!advanced && !app) { notice('尚未检测到支持的应用，请安装或打开应用后点击“刷新应用列表”。', true); return; }
  if (!advanced && ruleDraft.some(a => a.match === 'application' && a.applicationId === app.id)) {notice('此应用已有绑定，请编辑已有规则。',true);return;}
  ruleDraft.push({id:crypto.randomUUID(), name:advanced ? name : app.name, applicationId:advanced ? '' : app.id, enabled:true, outboundId:draft[0].id, match:advanced ? 'port' : 'application', processPaths:advanced ? [] : app.processPaths, listenPort:advanced ? port : 0});
  renderAppEditor(); dirty();
  $('#app-rule-editor').lastElementChild.querySelector('select, input').focus();
}

function renderAppEditor() {
  const editor = $('#app-rule-editor');
  editor.replaceChildren();
  for (const [index, rule] of ruleDraft.entries()) {
    const row = element('fieldset', undefined, 'outbound-row');
    row.append(element('legend', `应用规则 ${index + 1}`));
    const fields = element('div', undefined, 'grid');
    const field = (title, input) => { const label = element('label', title); label.append(input); fields.append(label); return input; };
    if (rule.match === 'application') {
      const application = field('选择应用', element('select')); application.dataset.application = rule.id;
      for (const app of state?.applications || []) application.append(new Option(`${app.name} · ${app.available ? '已安装' : '未检测到'}`, app.id));
      if (![...application.options].some(o => o.value === rule.applicationId)) application.append(new Option(`${rule.name} · 等待重新识别`,rule.applicationId));
      application.value = rule.applicationId;
      application.onchange = () => {
        const app = state.applications.find(a => a.id === application.value);
        if (ruleDraft.some(a => a.id !== rule.id && a.applicationId === app.id && a.match === 'application')) {application.value = rule.applicationId;notice('此应用已有绑定。',true);return;}
        rule.applicationId=app.id;rule.name=app.name;rule.processPaths=app.processPaths || [];dirty();
      };
    } else {
      const name = field('应用名称', element('input')); name.type = 'text'; name.value = rule.name; name.maxLength = 60; name.required = true;
      name.oninput = () => { rule.name = name.value.trim(); dirty(); };
    }
    const enabled = field('策略', element('select'));
    enabled.append(new Option('固定指定客户端', 'true'), new Option('跟随默认策略', 'false'));
    enabled.value = String(rule.enabled); enabled.onchange = () => { rule.enabled = enabled.value === 'true'; dirty(); };
    const outbound = field('代理客户端', element('select')); outbound.dataset.ruleOutbound = rule.id;
    outbound.onchange = () => { rule.outboundId = outbound.value; dirty(); };
    if (rule.match !== 'application') {
      const match = field('匹配方式', element('select'));
      match.append(new Option('专属代理入口', 'port'), new Option('完整程序路径', 'process'),new Option('直接选择应用','application')); match.value = rule.match;
      match.onchange = () => {
        if (match.value === 'application') {
          const app=(state?.applications || []).find(a => a.available && a.name.toLowerCase().startsWith(rule.name.toLowerCase().split(' ')[0])) || (state?.applications || []).find(a=>a.available);
          if (!app) {match.value=rule.match;notice('请先刷新应用列表。',true);return;}
          rule.applicationId=app.id;rule.name=app.name;rule.processPaths=app.processPaths;rule.listenPort=0;
        }
        rule.match = match.value; renderAppEditor(); dirty();
      };
    }
    if (rule.match === 'port') {
      const port = field('专属端口', element('input')); port.type = 'number'; port.min = 1024; port.max = 65535; port.value = rule.listenPort || 17900; port.required = true;
      if (!rule.listenPort) rule.listenPort = Number(port.value);
      port.oninput = () => { rule.listenPort = Number(port.value); dirty(); };
      const copy = field('保存后配置到应用', element('button', '复制专属 HTTP 地址')); copy.type = 'button';
      copy.onclick = async () => {
        try { await navigator.clipboard.writeText(`http://127.0.0.1:${rule.listenPort}`); notice('已复制；同端口支持 SOCKS5。修改后请先保存。'); }
        catch { notice(`专属入口：http://127.0.0.1:${rule.listenPort}`); }
      };
    } else if (rule.match === 'process') {
      const paths = field('完整程序路径（每行一个）', element('textarea')); paths.rows = 3; paths.value = (rule.processPaths || []).join('\n'); paths.required = true;
      paths.oninput = () => { rule.processPaths = paths.value.split('\n').map(p => p.trim()).filter(Boolean); dirty(); };
    }
    const remove = element('button', '删除规则', 'danger'); remove.type = 'button';
    remove.onclick = () => { ruleDraft.splice(index,1); renderAppEditor(); dirty(); };
    row.append(fields, element('p', rule.match === 'application' ? '保存后自动识别并配置系统代理和终端接入，无需填写 HTTP 地址或程序路径。已打开的应用或终端可能需要重启一次；其他接入应用跟随默认策略。' : '固定客户端不可用时不改道。专属入口只监听本机；本机和局域网目标保持直连。', 'hint'), remove);
    editor.append(row);
  }
  syncAppOutbounds();
  for (const id of ['#add-app-rule', '#add-codex', '#add-claude']) $(id).disabled = ruleDraft.length >= 32;
}

function renderAppStatus(status) {
  const container = $('#app-rule-status'); container.replaceChildren();
  if (status.appStatusError) container.append(element('p', status.appStatusError));
  if (status.applicationScanError) container.append(element('p', status.applicationScanError));
  for (const rule of status.config.appRules || []) {
    const outbound = status.config.outbounds.find(o => o.id === rule.outboundId);
    const health = status.health[rule.outboundId];
    let detail = '跟随默认策略';
    if (rule.enabled) {
      detail = `固定 → ${outbound?.name || '未知出口'} · ${!health?.checked ? '待检测' : health.available ? '可用' : '不可用，不改道'}`;
      const observed = status.appStatus?.[rule.id];
      detail += observed?.activeConnections ? ` · 当前命中 ${observed.activeConnections} 条连接 · 实际出口：${observed.actualOutbounds.join('、')}` : ' · 暂无活动连接';
      if (rule.match === 'application') {
        const app=(status.applications || []).find(a=>a.id===rule.applicationId);
        const access=rule.applicationId.endsWith('-cli') ? status.integrations?.terminal : status.integrations?.system && (status.platform!=='windows' || status.integrations?.terminal);
        if (!app?.available) detail='尚未识别此应用，请安装或打开应用后刷新列表';
        else if (!access) detail='尚未完成接入，保存规则时网关会自动配置';
        else if (observed?.activeConnections) detail=`已命中固定代理 · ${observed.activeConnections} 条连接 · 实际出口：${observed.actualOutbounds.join('、')}`;
        else detail='接入已设置，等待应用新连接；已打开的应用或终端请重启一次';
        if (health?.checked && !health.available) detail+=' · 固定出口不可用，不改道';
      }
    }
    const row = element('div', undefined, 'integration'); row.append(element('strong', rule.name), element('p', detail)); container.append(row);
  }
}
