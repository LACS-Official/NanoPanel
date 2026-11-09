'use strict';
const API = '/api/v1';
let token = localStorage.getItem('np_token') || '';
let overviewTimer = null;

/* ===== Logo（登录页 / 侧栏 / 关于 共用）===== */
const LOGO_SVG =
  '<svg viewBox="0 0 64 64" xmlns="http://www.w3.org/2000/svg" width="100%" height="100%">' +
  '<rect x="4" y="4" width="56" height="56" rx="14" fill="url(#npg)"/>' +
  '<path d="M21 45V19l22 26V19" fill="none" stroke="#fff" stroke-width="6.5" stroke-linecap="round" stroke-linejoin="round"/>' +
  '<circle cx="50" cy="14" r="4.5" fill="#34c759"/></svg>';
document.querySelectorAll('.np-logo').forEach(el=>{ el.innerHTML = LOGO_SVG; });

function esc(v){
  return String(v == null ? '' : v)
    .replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;')
    .replace(/"/g,'&quot;').replace(/'/g,'&#39;');
}

function toast(msg){
  const t = document.getElementById('toast');
  t.textContent = msg; t.classList.add('show');
  clearTimeout(t._tm);
  t._tm = setTimeout(()=>t.classList.remove('show'), 2600);
}

async function call(method, path, body){
  const opt = { method, headers:{'Content-Type':'application/json'} };
  if (token) opt.headers['Authorization'] = 'Bearer ' + token;
  if (body) opt.body = JSON.stringify(body);
  let res;
  try { res = await fetch(API + path, opt); }
  catch(e){ throw new Error('网络错误，连不上面板'); }
  if (res.status === 401){
    token=''; localStorage.removeItem('np_token'); showLogin();
    throw new Error('登录已过期');
  }
  let data;
  try { data = await res.json(); }
  catch(e){ throw new Error('响应格式错误 (HTTP ' + res.status + ')'); }
  if (!data.ok) throw new Error(data.err || '请求失败 (HTTP ' + res.status + ')');
  return data.data;
}

function showLogin(){
  document.getElementById('loginView').classList.remove('hidden');
  document.getElementById('mainView').classList.add('hidden');
  if (overviewTimer){ clearInterval(overviewTimer); overviewTimer=null; }
}
function showMain(){
  document.getElementById('loginView').classList.add('hidden');
  document.getElementById('mainView').classList.remove('hidden');
  const saved = localStorage.getItem('np_user');
  document.getElementById('sideUser').textContent = saved ? '已登录：' + saved : '';
  switchView('overview'); loadOverview(); loadServices(); loadProcesses(); loadApps();
  loadWebhook(); loadMonitorSettings();
  overviewTimer = setInterval(()=>{ if(currentView()==='overview') loadOverview(); }, 5000);
}

async function doLogin(){
  const user = document.getElementById('loginUser').value.trim();
  const pw = document.getElementById('loginPwd').value;
  if (!user || !pw) return toast('请输入用户名和密码');
  try {
    const d = await call('POST','/login',{username:user, password:pw});
    token = d.token;
    localStorage.setItem('np_token', token);
    localStorage.setItem('np_user', user);
    document.getElementById('loginPwd').value = '';
    showMain();
  } catch(e){ toast(e.message); }
}
async function doLogout(){
  try { await call('POST','/logout'); } catch(e){}
  token=''; localStorage.removeItem('np_token'); showLogin();
}

/* ===== 视图与二级页签 ===== */
const viewMeta = {
  overview:  { t:'概览',     s:'系统状态与资源占用一览' },
  processes: { t:'进程',     s:'按内存排序的进程快照' },
  logs:      { t:'日志中心', s:'系统日志与面板审计',
               tabs:[['system','系统日志'],['panel','面板日志']] },
  network:   { t:'网络',     s:'连通性测试' },
  disks:     { t:'磁盘',     s:'块设备挂载与用量管理' },
  files:     { t:'文件',     s:'受限目录内的文件管理' },
  apps:      { t:'应用商店', s:'应用、系统服务与下载源管理',
               tabs:[['all','全部'],['installed','已安装'],['upgradable','可升级'],['store','设置']] },
  settings:  { t:'设置',     s:'通知、安全与面板信息',
               tabs:[['notify','通知'],['security','安全'],['maintain','维护'],['system','系统'],['about','关于']] },
};
let curSub = {};
function currentView(){ const b=document.querySelector('.nav button.active'); return b?b.dataset.view:''; }
function switchView(v){
  document.querySelectorAll('.nav button').forEach(b=>b.classList.toggle('active',b.dataset.view===v));
  document.querySelectorAll('.view').forEach(x=>x.classList.toggle('active',x.id==='view-'+v));
  const m = viewMeta[v] || {};
  document.getElementById('pageTitle').textContent = m.t || v;
  document.getElementById('pageSub').textContent = m.s || '';
  renderSubtabs(v);
  if (v==='apps'){ loadApps(); if(!curSub.apps) setSub('apps','all'); }
  if (v==='logs' && !curSub.logs) setSub('logs','system');
  if (v==='settings' && !curSub.settings) setSub('settings','notify');
  if (v==='disks') loadDisks();
  if (v==='files' && !fmPath) loadFiles();
  startNetTimer(v==='network');
}
function renderSubtabs(v){
  const box = document.getElementById('pageSubtabs');
  const m = viewMeta[v];
  if (!m || !m.tabs){ box.innerHTML=''; box.classList.add('hidden'); return; }
  const cur = curSub[v] || m.tabs[0][0];
  box.classList.remove('hidden');
  box.innerHTML = m.tabs.map(t=>
    '<button data-sub="'+t[0]+'"'+(t[0]===cur?' class="active"':'')+'>'+t[1]+'</button>').join('');
}
function setSub(v,k){
  curSub[v] = k;
  document.querySelectorAll('#pageSubtabs button').forEach(b=>b.classList.toggle('active', b.dataset.sub===k));
  document.querySelectorAll('#view-'+v+' .subpane').forEach(p=>p.classList.toggle('hidden', p.dataset.sub!==k));
  if (v==='logs') (k==='system') ? loadLogs() : loadPanelLogs();
  if (v==='apps' && k==='store') loadMirrorSettings();
  if (v==='settings' && k==='system') loadTLS();
  if (v==='settings' && k==='maintain') loadCron();
}
document.getElementById('pageSubtabs').addEventListener('click', e=>{
  const b = e.target.closest('button[data-sub]');
  if (b) setSub(currentView(), b.dataset.sub);
});
document.getElementById('nav').addEventListener('click',e=>{
  if(e.target.closest('button[data-view]')) switchView(e.target.closest('button[data-view]').dataset.view);
});

function fmtUptime(sec){
  const d=Math.floor(sec/86400), h=Math.floor(sec%86400/3600), m=Math.floor(sec%3600/60);
  return d+'天 '+h+'时 '+m+'分';
}
function metric(k,v){ return '<div class="metric"><div class="k">'+esc(k)+'</div><div class="v">'+esc(v)+'</div></div>'; }

async function loadOverview(){
  try {
    const o = await call('GET','/overview');
    document.getElementById('sysMetrics').innerHTML =
      metric('主机名',o.hostname || '-')+
      metric('系统',o.os || '-')+
      metric('内核',o.kernel || '-')+
      metric('运行时间',fmtUptime(o.uptime_sec))+
      metric('CPU',o.cpu_model || '-')+
      metric('核心数 / 负载',o.cpu_cores+' 核 / '+o.load1)+
      (o.temps && o.temps.length ? metric('温度',
        o.temps.map(z=>esc(z.label)+' <b style="color:'+(z.celsius>=70?'#ff3b30':(z.celsius>=55?'#d38b1d':'#1a9e44'))+'">'+z.celsius+'℃</b>').join(' · ')) : '')+
      (o.cpu_freqs && o.cpu_freqs.length ? metric('CPU 频率',
        Math.round(Math.min(...o.cpu_freqs))+' - '+Math.round(Math.max(...o.cpu_freqs))+' MHz') : '');
    document.getElementById('resBars').innerHTML =
      bar('内存',o.mem_used_pct,(o.mem_total_mb-o.mem_avail_mb)+' / '+o.mem_total_mb+' MB')+
      bar('磁盘',o.disk_used_pct,(o.disk_total_mb-o.disk_avail_mb)+' / '+o.disk_total_mb+' MB');
  } catch(e){ /* 定时刷新静默失败，避免打扰 */ }
}
function bar(label,pct,detail){
  const w = Math.max(0, Math.min(100, Number(pct)||0));
  return '<div class="bar-row"><div class="lab"><span>'+esc(label)+'</span><span>'+esc(detail)+'（'+w+'%）</span></div>'+
    '<div class="bar-track"><div class="bar-fill" style="width:'+w+'%"></div></div></div>';
}

async function loadServices(){
  try {
    const list = await call('GET','/services');
    document.getElementById('svcList').innerHTML = list.map(s=>
      '<div class="svc-row"><div class="svc-name"><span class="dot '+esc(s.state)+'"></span>'+esc(s.label)+
      '<div class="lbl">'+esc(s.name)+' · '+esc(s.state)+'</div></div>'+
      '<div class="svc-actions">'+
      '<button class="btn sm green" data-svc="'+esc(s.name)+'" data-act="start">启动</button>'+
      '<button class="btn sm gray" data-svc="'+esc(s.name)+'" data-act="restart">重启</button>'+
      '<button class="btn sm danger" data-svc="'+esc(s.name)+'" data-act="stop">停止</button>'+
      '</div></div>').join('');
  } catch(e){ document.getElementById('svcList').textContent = e.message; }
}
document.getElementById('svcList').addEventListener('click', async e=>{
  const b = e.target.closest('button[data-svc]'); if(!b) return;
  try { await call('POST','/service/action?svc='+encodeURIComponent(b.dataset.svc)+'&action='+b.dataset.act);
    toast('操作成功'); loadServices();
  } catch(err){ toast(err.message); }
});

async function loadProcesses(){
  try {
    const list = await call('GET','/processes');
    document.getElementById('procBody').innerHTML = list.map(p=>
      '<tr><td>'+esc(p.pid)+'</td><td>'+esc(p.user)+'</td><td>'+esc(p.rss_kb)+'</td><td>'+esc(p.comm)+'</td>'+
      '<td><button class="btn sm danger" data-pid="'+esc(p.pid)+'">结束</button></td></tr>').join('');
  } catch(e){ document.getElementById('procBody').innerHTML = '<tr><td colspan="5">'+esc(e.message)+'</td></tr>'; }
}
document.getElementById('procBody').addEventListener('click', async e=>{
  const b = e.target.closest('button[data-pid]'); if(!b) return;
  const pid = b.dataset.pid;
  if(!confirm('确定结束进程 '+pid+'？')) return;
  try { await call('POST','/process/kill?pid='+encodeURIComponent(pid)+'&force=0');
    toast('已结束'); loadProcesses();
  } catch(err){ toast(err.message); }
});

// 单元下拉：服务白名单 + 已安装应用
async function refreshLogUnits(){
  const sel = document.getElementById('logUnit');
  const current = sel.value;
  let options = '<option value="">全部单元</option>';
  try {
    const svcs = await call('GET','/services');
    if (svcs.length) options += '<optgroup label="系统服务">'+
      svcs.map(s=>'<option value="'+esc(s.name)+'">'+esc(s.label||s.name)+'（'+esc(s.name)+'）</option>').join('')+'</optgroup>';
  } catch(e){ /* 服务列表加载失败时仍显示应用 */ }
  const units = (appData||[]).filter(a=>a.state==='installed' && a.unit);
  if (units.length) options += '<optgroup label="已安装应用">'+
    units.map(a=>'<option value="'+esc(a.unit)+'">'+esc(a.name)+'（'+esc(a.unit)+'）</option>').join('')+'</optgroup>';
  sel.innerHTML = options;
  if (current && sel.querySelector('option[value="'+CSS.escape(current)+'"]')) sel.value = current;
}

async function loadLogs(){
  const unit = document.getElementById('logUnit').value;
  const n = document.getElementById('logN').value;
  let path='/logs?n='+n;
  if(unit) path+='&unit='+encodeURIComponent(unit);
  document.getElementById('logOutput').textContent='加载中…';
  try {
    const d = await call('GET',path);
    document.getElementById('logOutput').textContent = d.logs || '（无日志）';
  } catch(e){ document.getElementById('logOutput').textContent = e.message; }
}

const catLabels = {login:'登录', service:'服务', process:'进程', app:'应用', settings:'设置', system:'系统', health:'健康检测', disk:'磁盘'};
async function loadPanelLogs(){
  try {
    const list = await call('GET','/panel-logs');
    document.getElementById('panelLogBody').innerHTML = list.length
      ? list.map(e=>'<tr><td>'+esc(e.time)+'</td><td>'+esc(catLabels[e.category]||e.category)+
        '</td><td>'+esc(e.action)+'</td><td>'+esc(e.detail||'-')+'</td><td>'+esc(e.ip)+'</td></tr>').join('')
      : '<tr><td colspan="5" class="empty-tip">暂无面板操作日志</td></tr>';
  } catch(e){ document.getElementById('panelLogBody').innerHTML = '<tr><td colspan="5">'+esc(e.message)+'</td></tr>'; }
}

// ===== Webhook 通知 =====
async function loadWebhook(){
  try {
    const d = await call('GET','/settings/webhook');
    document.getElementById('whUrl').value = d.url || '';
    document.getElementById('whFormat').value = d.format || 'generic';
    document.getElementById('whEvents').innerHTML = (d.event_options||[]).map(o=>
      '<label><input type="checkbox" value="'+esc(o.key)+'"'+
      ((d.events||[]).indexOf(o.key)>=0?' checked':'')+'>'+esc(o.label)+'</label>').join('');
  } catch(e){ /* 加载失败不打扰 */ }
}
function whSelectedEvents(){
  return Array.from(document.querySelectorAll('#whEvents input:checked')).map(x=>x.value);
}
async function saveWebhook(){
  const body = { url: document.getElementById('whUrl').value.trim(),
    format: document.getElementById('whFormat').value, events: whSelectedEvents() };
  try { const d = await call('POST','/settings/webhook', body); toast(d.msg || '已保存'); }
  catch(e){ toast(e.message); }
}
async function testWebhook(){
  try { const d = await call('POST','/settings/webhook/test'); toast(d.msg); }
  catch(e){ toast(e.message); }
}

// ===== 应用健康检测设置 =====
async function loadMonitorSettings(){
  try {
    const d = await call('GET','/settings/monitor');
    const sel = document.getElementById('monInterval');
    sel.innerHTML = (d.options||[]).map(o=>
      '<option value="'+esc(o.value)+'"'+(Number(o.value)===Number(d.interval_sec)?' selected':'')+'>'+esc(o.label)+'</option>').join('');
    document.getElementById('monHint').textContent = d.monitor_enabled
      ? '当前每 ' + d.interval_sec + ' 秒检测一次；保存后下一轮生效。'
      : '自动检测已关闭，可点「立即检测并告警」手动体检。';
    const th = document.getElementById('tempThreshold');
    if (th && document.activeElement !== th) th.value = d.temp_threshold_c || 0;
    const hint = document.getElementById('tempHint');
    if (hint) hint.textContent = d.temp_threshold_c > 0
      ? '当前阈值 ' + d.temp_threshold_c + '℃，超过后推送告警。'
      : '温度告警未开启。';
  } catch(e){ /* 静默 */ }
}
async function saveTempThreshold(){
  const v = Number(document.getElementById('tempThreshold').value || 0);
  if (v !== 0 && (v < 40 || v > 100)) { toast('阈值需为 40-100℃ 或 0（关闭）'); return; }
  try {
    const cur = Number(document.getElementById('monInterval').value);
    await call('POST','/settings/monitor',{interval_sec:cur, temp_threshold_c:v});
    toast('温度阈值已保存');
    loadMonitorSettings();
  } catch(e){ toast(e.message); }
}
async function saveMonitor(){
  const v = Number(document.getElementById('monInterval').value);
  const th = Number(document.getElementById('tempThreshold').value || 0);
  try { await call('POST','/settings/monitor',{interval_sec:v, temp_threshold_c:th}); toast('检测设置已保存'); loadMonitorSettings(); }
  catch(e){ toast(e.message); }
}

// ===== 备份恢复与定时任务 =====
async function downloadBackup(){
  try {
    const res = await fetch(API+'/backup', {headers:{'Authorization':'Bearer '+token}});
    if (!res.ok) throw new Error('备份失败 HTTP '+res.status);
    const blob = await res.blob();
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = 'nanopanel-backup.tar.gz';
    a.click();
    setTimeout(()=>URL.revokeObjectURL(a.href), 5000);
    toast('备份已下载');
  } catch(e){ toast(e.message); }
}

document.getElementById('restoreFile').addEventListener('change', async e=>{
  const f = e.target.files[0];
  e.target.value = '';
  if (!f) return;
  const applyCfg = document.getElementById('restoreCfg').checked;
  const fd = new FormData();
  fd.append('file', f);
  const hint = document.getElementById('restoreHint');
  hint.textContent = '正在恢复…';
  try {
    const res = await fetch(API+'/restore'+(applyCfg?'':'?apply_config=0'), {
      method:'POST', headers:{'Authorization':'Bearer '+token}, body: fd
    });
    const d = await res.json();
    if (!res.ok || !d.ok) throw new Error(d.err || '恢复失败');
    const r = d.data;
    hint.textContent = '已恢复应用：' + (r.apps_restored||[]).join('、') +
      (r.panel_applied ? '；面板配置已还原，面板正在自动重启' : (r.panel_staged ? '；面板配置已暂存到 '+r.panel_staged : ''));
    toast('恢复完成');
    if (r.panel_applied) setTimeout(()=>location.reload(), 5000);
  } catch(err){ hint.textContent = err.message; toast(err.message); }
});

let cronSchedLoaded = false;
async function loadCron(){
  try {
    const d = await call('GET','/cron');
    const sel = document.getElementById('cronSched');
    if (!cronSchedLoaded){
      sel.innerHTML = (d.schedules||[]).map(s=>'<option value="'+esc(s.value)+'">'+esc(s.label)+'</option>').join('');
      cronSchedLoaded = true;
    }
    const tb = document.getElementById('cronList');
    if (!d.tasks || !d.tasks.length){
      tb.innerHTML = '<tr><td colspan="5" class="empty-tip">暂无任务</td></tr>';
      return;
    }
    tb.innerHTML = d.tasks.map(t=>{
      const st = t.last_status==='ok' ? '<span style="color:#1a9e44">成功</span>'
        : (t.last_status==='fail' ? '<span style="color:#d70015">失败</span>' : '—');
      const last = t.last_run ? esc(t.last_run)+' '+st : '—';
      return '<tr><td>'+esc(t.name)+'</td>'+
        '<td>'+esc(cronDesc(t.schedule))+'</td>'+
        '<td style="max-width:260px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="'+esc(t.command)+'">'+esc(t.command)+'</td>'+
        '<td>'+last+'</td>'+
        '<td><span class="row-flex" style="gap:6px">'+
        '<button class="btn sm gray" data-cron-run="'+esc(t.id)+'">立即执行</button>'+
        '<button class="btn sm gray" data-cron-toggle="'+esc(t.id)+'" data-on="'+(t.enabled?1:0)+'">'+(t.enabled?'停用':'启用')+'</button>'+
        '<button class="btn sm danger" data-cron-del="'+esc(t.id)+'">删除</button>'+
        '</span></td></tr>';
    }).join('');
  } catch(e){ /* 静默 */ }
}
function cronDesc(sc){
  if (sc.startsWith('every:')) return '每 '+sc.slice(6,-1)+' 分钟';
  if (sc.startsWith('hourly:')) return '每小时第 '+sc.slice(7)+' 分';
  if (sc.startsWith('daily:')) return '每天 '+sc.slice(6);
  return sc;
}
async function saveCronTask(){
  const name = document.getElementById('cronName').value.trim();
  const schedule = document.getElementById('cronSched').value;
  const command = document.getElementById('cronCmd').value.trim();
  if (!name || !command){ toast('名称与命令不能为空'); return; }
  try {
    await call('POST','/cron',{name, schedule, command, enabled:true});
    toast('任务已添加');
    document.getElementById('cronName').value = '';
    document.getElementById('cronCmd').value = '';
    loadCron();
  } catch(e){ toast(e.message); }
}
document.getElementById('cronList').addEventListener('click', async e=>{
  const el = e.target.closest('[data-cron-run],[data-cron-del],[data-cron-toggle]');
  if (!el) return;
  if (el.dataset.cronRun){
    if (!confirm('立即执行该任务？')) return;
    try {
      const d = await call('POST','/cron/run',{id:el.dataset.cronRun});
      toast('执行结果：'+(d.status==='ok'?'成功':'失败'));
      loadCron();
    } catch(err){ toast(err.message); }
  }
  if (el.dataset.cronDel){
    if (!confirm('删除该任务？')) return;
    try { await call('POST','/cron/delete',{id:el.dataset.cronDel}); loadCron(); }
    catch(err){ toast(err.message); }
  }
  if (el.dataset.cronToggle){
    try { await call('POST','/cron',{id:el.dataset.cronToggle, enabled: el.dataset.on!=='1'}); loadCron(); }
    catch(err){ toast(err.message); }
  }
});

// ===== 应用清单远程更新 =====
async function updateManifest(){
  const hint = document.getElementById('manifestHint');
  hint.textContent = '正在从远程仓库拉取…';
  try {
    const d = await call('POST','/apps/update-manifest');
    hint.textContent = '';
    toast('清单已更新：' + d.count + ' 个应用');
    loadApps();
  } catch(e){ hint.textContent = ''; toast(e.message); }
}

// ===== HTTPS 设置 =====
async function loadTLS(){
  try {
    const d = await call('GET','/settings/tls');
    const on = !!d.enabled;
    const tag = document.getElementById('tlsState');
    tag.textContent = on ? '已开启' + (d.expires ? ' · 证书有效期至 ' + d.expires : '') : '未开启';
    tag.className = 'state-tag ' + (on ? 'st-ok' : 'st-off');
    document.getElementById('tlsEnableBtn').disabled = on;
    document.getElementById('tlsDisableBtn').disabled = !on;
    document.getElementById('tlsHint').textContent = on
      ? '当前以 HTTPS 提供服务。证书路径：' + d.cert_path
      : '当前以 HTTP 明文传输，建议在可信网络内开启 HTTPS。';
  } catch(e){ /* 静默 */ }
}
async function saveTLS(enable){
  if (enable && !confirm('开启后将自动生成自签证书并重启面板（约 5 秒），期间页面会短暂不可达。继续？')) return;
  if (!enable && !confirm('关闭 HTTPS 后面板将回到 HTTP 明文传输，重启面板后生效。继续？')) return;
  try {
    const d = await call('POST','/settings/tls',{enabled:!!enable});
    toast(d.msg || '已保存');
    if (enable) { setTimeout(()=>location.reload(), 4000); }
    else loadTLS();
  } catch(e){ toast(e.message); }
}
async function checkAppsNow(){
  try {
    const d = await call('POST','/apps/check?force=1');
    toast('检测完成：' + (d.changes||[]).length + ' 处掉线 / 恢复');
    loadApps(); loadPanelLogs();
  } catch(e){ toast(e.message); }
}

// ===== 应用商店 =====
const healthLabels = {running:'● 运行中', down:'● 已掉线', unknown:'○ 未监控', not_installed:'未安装'};
const healthCls = {running:'st-run', down:'st-down', unknown:'st-unknown', not_installed:'st-unknown'};
let appData = [];

function appUpgradable(a){
  return a.state==='installed' && a.installed_version && a.installed_version !== a.version;
}

function appCard(a, mode){
  const installed = a.state==='installed';
  const upgradable = appUpgradable(a);
  const cfgBtn = installed && a.config && a.config.length
    ? '<button class="btn sm gray" data-cfg="'+esc(a.id)+'">配置</button>' : '';
  const manageBtn = (a.id==='mihomo' && installed)
    ? '<button class="btn sm gray" data-manage="mihomo">管理面板</button>' : '';
  const upgBtn = upgradable && mode!=='upgradable'
    ? '<button class="btn sm green" data-id="'+esc(a.id)+'" data-act="install" data-label="升级">升级</button>' : '';
  let actionBtn;
  if (mode==='upgradable'){
    actionBtn = '<button class="btn sm green" data-id="'+esc(a.id)+'" data-act="install" data-label="升级">升级到 '+esc(a.version)+'</button>';
  } else {
    actionBtn = '<button class="btn sm '+(installed?'danger':'')+'" data-id="'+esc(a.id)+
      '" data-act="'+(installed?'remove':'install')+'">'+(installed?'卸载':'安装')+'</button>';
  }
  const verLine = mode==='upgradable'
    ? '<div class="ver">当前 '+esc(a.installed_version)+' → 最新 '+esc(a.version)+'</div>'
    : '<div class="ver">'+esc(a.version)+(upgradable?' · <span style="color:#d70015">有新版本</span>':'')+'</div>';
  const st = a.status || a.state;
  return '<div class="app-card"><div class="app-head"><div class="app-icon">'+esc((a.name||'?')[0])+'</div>'+
    '<div><div class="name">'+esc(a.name)+'</div>'+verLine+'</div></div>'+
    '<div class="app-desc">'+esc(a.description)+'</div>'+
    '<div class="app-foot"><span class="app-state '+(healthCls[st]||'st-unknown')+'">'+
    esc(healthLabels[st]||st)+'</span>'+
    '<span class="row-flex" style="gap:8px">'+manageBtn+cfgBtn+upgBtn+actionBtn+'</span></div></div>';
}

async function loadApps(){
  try {
    appData = await call('GET','/apps');
    const all = appData, inst = appData.filter(a=>a.state==='installed'), upg = appData.filter(appUpgradable);
    document.getElementById('allGrid').innerHTML = all.length
      ? all.map(a=>appCard(a,'all')).join('') : '<div class="empty-tip">应用清单为空</div>';
    document.getElementById('installedGrid').innerHTML = inst.length
      ? inst.map(a=>appCard(a,'installed')).join('') : '<div class="empty-tip">还没有安装任何应用</div>';
    document.getElementById('upgradableGrid').innerHTML = upg.length
      ? upg.map(a=>appCard(a,'upgradable')).join('') : '<div class="empty-tip">所有应用都是最新版本</div>';
    refreshLogUnits();
  } catch(e){
    document.getElementById('allGrid').textContent = e.message;
  }
}

document.getElementById('view-apps').addEventListener('click', async e=>{
  const manageBtn = e.target.closest('button[data-manage]');
  if (manageBtn){
    try {
      const st = await call('GET','/mihomo/status');
      if (st.installed && st.ui_enabled && st.port){
        window.open('http://' + location.hostname + ':' + st.port + '/ui', '_blank');
      } else {
        toast('metacubexd 未开启，请在该应用的「配置」里开启后重试');
      }
    } catch(err){ toast(err.message); }
    return;
  }
  const cfgBtn = e.target.closest('button[data-cfg]');
  if (cfgBtn) return openAppCfg(cfgBtn.dataset.cfg);
  const b = e.target.closest('button[data-id]');
  if (b) runAppAction(b.dataset.id, b.dataset.act, b.dataset.label);
});

// ===== 应用商店设置 =====
async function loadMirrorSettings(){
  try {
    const d = await call('GET','/settings/mirror');
    const box = document.getElementById('mirrorMetrics');
    box.innerHTML =
      metric('GitHub 下载源', d.GITHUB_MIRROR || 'https://github.com（默认）')+
      metric('apt 软件源', d.APT_MIRROR || '系统默认')+
      metric('测速延迟', d.GITHUB_MIRROR_LATENCY ? (Number(d.GITHUB_MIRROR_LATENCY).toFixed(2)+' s') : '-')+
      metric('选择时间', d.SELECTED_AT || '-');
  } catch(e){ /* 静默 */ }
}

// ===== 网卡流量图表 =====
const netHistory = {}; // name -> {rx:[], tx:[]}
let netTimer = null;
const NET_POINTS = 60;

function startNetTimer(active){
  if (netTimer){ clearInterval(netTimer); netTimer = null; }
  if (active){
    pollNetTraffic();
    netTimer = setInterval(pollNetTraffic, 2000);
  }
}

async function pollNetTraffic(){
  try {
    const list = await call('GET','/net/traffic');
    const container = document.getElementById('netTraffic');
    if (!container) return;
    container.innerHTML = (list||[]).map(renderNetCard).join('') ||
      '<div class="empty-tip">未读取到网卡信息</div>';
  } catch(e){ /* 静默 */ }
}

function fmtRate(bps){
  if (bps >= 1048576) return (bps/1048576).toFixed(1)+' MB/s';
  if (bps >= 1024) return (bps/1024).toFixed(1)+' KB/s';
  return Math.round(bps)+' B/s';
}
function fmtBytes(b){
  const u = ['B','KB','MB','GB','TB']; let i = 0;
  while (b >= 1024 && i < u.length-1){ b /= 1024; i++; }
  return b.toFixed(i?1:0)+' '+u[i];
}

// renderNetCard 画一块网卡：速率、累计、SVG 双折线（下蓝上红）。
function renderNetCard(n){
  let h = netHistory[n.name];
  if (!h) h = netHistory[n.name] = {rx:[], tx:[]};
  h.rx.push(n.rx_rate||0); h.tx.push(n.tx_rate||0);
  if (h.rx.length > NET_POINTS) { h.rx.shift(); h.tx.shift(); }
  const max = Math.max(1024, ...h.rx, ...h.tx);
  const W = 560, H = 80;
  const line = arr => arr.map((v,i)=>{
    const x = (i/Math.max(1,NET_POINTS-1))*W;
    const y = H - (v/max)*H;
    return (i?'L':'M')+x.toFixed(1)+' '+y.toFixed(1);
  }).join(' ');
  return '<div class="net-card" style="margin-bottom:16px">'+
    '<div class="row-flex" style="margin-bottom:6px"><b>'+esc(n.name)+'</b>'+
    '<span style="color:#0071e3">↓ '+fmtRate(n.rx_rate||0)+'</span>'+
    '<span style="color:#d70015">↑ '+fmtRate(n.tx_rate||0)+'</span>'+
    '<span style="color:var(--gray);font-size:12px">累计 ↓ '+fmtBytes(n.rx_bytes)+' / ↑ '+fmtBytes(n.tx_bytes)+'</span></div>'+
    '<svg viewBox="0 0 '+W+' '+H+'" width="100%" height="'+H+'" preserveAspectRatio="none" style="display:block;background:rgba(0,113,227,0.04);border-radius:8px">'+
    '<path d="'+line(h.rx)+'" fill="none" stroke="#0071e3" stroke-width="1.6"/>'+
    '<path d="'+line(h.tx)+'" fill="none" stroke="#d70015" stroke-width="1.6"/>'+
    '</svg></div>';
}

async function doPing(){
  const host = document.getElementById('pingHost').value.trim();
  if(!host) return toast('请输入目标');
  document.getElementById('pingOutput').textContent='测试中…';
  try {
    const d = await call('GET','/ping?host='+encodeURIComponent(host));
    document.getElementById('pingOutput').textContent = d.output;
  } catch(e){ document.getElementById('pingOutput').textContent = e.message; }
}

// ===== 应用安装 / 卸载 / 升级实时输出 =====
let actDone = false;
function closeActDlg(){ document.getElementById('actMask').classList.add('hidden'); }
function actAppend(line){
  const pre = document.getElementById('actOutput');
  pre.textContent += line + '\n';
  pre.scrollTop = pre.scrollHeight;
}
function finishAct(ok, label){
  if (actDone) return;
  actDone = true;
  const st = document.getElementById('actState');
  st.textContent = (ok ? '✔ ' : '✖ ') + label;
  st.style.color = ok ? '#1a9e44' : '#d70015';
  document.getElementById('actClose').disabled = false;
  toast(ok ? '操作完成' : '操作失败');
}
async function runAppAction(id, action, label){
  const app = (appData||[]).find(a=>a.id===id);
  const name = app ? app.name : id;
  const verb = label || (action==='install'?'安装':'卸载');
  actDone = false;
  document.getElementById('actTitle').textContent = verb + ' · ' + name;
  const st = document.getElementById('actState');
  st.textContent = '正在执行…'; st.style.color = 'var(--gray)';
  document.getElementById('actOutput').textContent = '';
  document.getElementById('actClose').disabled = true;
  document.getElementById('actMask').classList.remove('hidden');
  actAppend('[' + new Date().toLocaleTimeString() + '] 开始' + verb + ' ' + name);
  try {
    const res = await fetch(API+'/app/action/stream?id='+encodeURIComponent(id)+'&action='+action,
      { method:'POST', headers:{'Authorization':'Bearer '+token} });
    if (res.status === 401){ token=''; localStorage.removeItem('np_token'); showLogin(); return; }
    const ct = res.headers.get('Content-Type') || '';
    if (ct.indexOf('text/event-stream') < 0){
      let d = {}; try { d = await res.json(); } catch(e){}
      actAppend(d.err || ('HTTP ' + res.status));
      finishAct(false, '失败');
      return;
    }
    const reader = res.body.getReader();
    const dec = new TextDecoder();
    let buf = '';
    for(;;){
      const r = await reader.read();
      if (r.done) break;
      buf += dec.decode(r.value, {stream:true});
      let idx;
      while ((idx = buf.indexOf('\n\n')) >= 0){
        const chunk = buf.slice(0, idx); buf = buf.slice(idx+2);
        const line = chunk.split('\n').find(l=>l.indexOf('data:')===0);
        if (!line) continue;
        let m; try { m = JSON.parse(line.slice(5).trim()); } catch(e){ continue; }
        if (m.event === 'out') actAppend(m.line);
        else if (m.event === 'done'){
          if (m.ok) finishAct(true, '完成');
          else { if (m.err) actAppend('退出: ' + m.err); finishAct(false, '失败'); }
          if (m.state) actAppend('应用状态: ' + m.state);
          loadApps();
          return;
        }
      }
    }
    finishAct(false, '连接中断');
  } catch(e){
    actAppend(e.message);
    finishAct(false, '失败');
  }
}

// ===== 文件管理 =====
let fmPath = '';
let fmRoots = [];

async function loadFiles(path){
  if (path !== undefined) fmPath = path;
  try {
    const q = fmPath ? ('?path='+encodeURIComponent(fmPath)) : '';
    const d = await call('GET','/files'+q);
    if (!fmPath && d.roots){
      fmRoots = d.roots;
      // 默认进入第一个根
      if (fmRoots.length) return loadFiles(fmRoots[0]);
    }
    renderFmCrumb();
    const tb = document.getElementById('fmList');
    if (!d.entries || !d.entries.length){
      tb.innerHTML = '<tr><td colspan="4" class="empty-tip">空目录</td></tr>';
      return;
    }
    tb.innerHTML = d.entries.map(e=>{
      const p = fmPath.replace(/\/$/,'') + '/' + e.name;
      const icon = e.is_dir ? '📁' : (e.text ? '📄' : '📄');
      const name = e.is_dir
        ? '<a href="javascript:void(0)" data-fmgo="'+esc(p)+'" style="font-weight:600">'+icon+' '+esc(e.name)+'</a>'
        : '<span>'+icon+' '+esc(e.name)+'</span>';
      const acts = [];
      if (e.is_dir){
        acts.push('<button class="btn sm gray" data-fmgo="'+esc(p)+'">进入</button>');
      } else {
        acts.push('<button class="btn sm gray" data-fmdl="'+esc(p)+'">下载</button>');
        if (e.text) acts.push('<button class="btn sm gray" data-fmedit="'+esc(p)+'">编辑</button>');
      }
      acts.push('<button class="btn sm gray" data-fmren="'+esc(p)+'">重命名</button>');
      acts.push('<button class="btn sm danger" data-fmdel="'+esc(p)+'"'+(e.is_dir?' data-rec="1"':'')+'>删除</button>');
      return '<tr><td>'+name+'</td><td>'+(e.is_dir?'-':fmtBytes(e.size))+'</td><td>'+esc(e.mod_time)+'</td>'+
        '<td><span class="row-flex" style="gap:6px">'+acts.join('')+'</span></td></tr>';
    }).join('');
  } catch(e){
    document.getElementById('fmList').innerHTML =
      '<tr><td colspan="4" class="empty-tip">'+esc(e.message)+'</td></tr>';
  }
}

function renderFmCrumb(){
  const box = document.getElementById('fmCrumb');
  box.innerHTML = fmPath
    ? '<a href="javascript:void(0)" data-fmgo="'+esc(fmPath)+'">'+esc(fmPath)+'</a>'
    : '选择位置';
}

function fmGoUp(){
  if (!fmPath || !fmRoots.length) return;
  if (fmRoots.includes(fmPath)) return; // 已在根
  const parent = fmPath.replace(/\/[^/]*$/, '') || fmPath;
  loadFiles(parent);
}

function fmMkdir(){
  const name = prompt('新文件夹名称：');
  if (!name) return;
  call('POST','/files/mkdir',{path: fmPath.replace(/\/$/,'')+'/'+name})
    .then(()=>{ toast('已创建'); loadFiles(); })
    .catch(e=>toast(e.message));
}

async function fmDelete(p, recursive){
  const msg = recursive ? '递归删除该文件夹及其全部内容？此操作不可恢复！' : '删除该文件？此操作不可恢复！';
  if (!confirm(msg)) return;
  try {
    await call('POST','/files/delete',{path:p, recursive:!!recursive});
    toast('已删除'); loadFiles();
  } catch(e){ toast(e.message); }
}

async function fmRename(p){
  const cur = p.split('/').pop();
  const name = prompt('新名称：', cur);
  if (!name || name === cur) return;
  const to = p.slice(0, p.length - cur.length) + name;
  try {
    await call('POST','/files/rename',{path:p, to});
    toast('已重命名'); loadFiles();
  } catch(e){ toast(e.message); }
}

// 下载走 blob（请求需带 Authorization 头，无法用直链）
async function fmDownload(p){
  try {
    const res = await fetch(API+'/files/download?path='+encodeURIComponent(p), {
      headers:{'Authorization':'Bearer '+token}
    });
    if (!res.ok) throw new Error('下载失败 HTTP '+res.status);
    const blob = await res.blob();
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = p.split('/').pop();
    a.click();
    setTimeout(()=>URL.revokeObjectURL(a.href), 5000);
  } catch(e){ toast(e.message); }
}

let fmEditingPath = '';
async function fmEdit(p){
  try {
    const d = await call('GET','/files/read?path='+encodeURIComponent(p));
    fmEditingPath = d.path || p;
    document.getElementById('fmEditTitle').textContent = '编辑 ' + p.split('/').pop();
    document.getElementById('fmEditContent').value = d.content;
    document.getElementById('fmEditMask').classList.remove('hidden');
  } catch(e){ toast(e.message); }
}
function closeFmEdit(){ document.getElementById('fmEditMask').classList.add('hidden'); }
async function fmSaveEdit(){
  const content = document.getElementById('fmEditContent').value;
  try {
    await call('POST','/files/write',{path: fmEditingPath, content});
    toast('已保存'); closeFmEdit(); loadFiles();
  } catch(e){ toast(e.message); }
}

document.getElementById('fmList').addEventListener('click', e=>{
  const el = e.target.closest('[data-fmgo],[data-fmdel],[data-fmren],[data-fmdl],[data-fmedit]');
  if (!el) return;
  if (el.dataset.fmgo)   loadFiles(el.dataset.fmgo);
  if (el.dataset.fmdel)  fmDelete(el.dataset.fmdel, el.dataset.rec);
  if (el.dataset.fmren)  fmRename(el.dataset.fmren);
  if (el.dataset.fmdl)   fmDownload(el.dataset.fmdl);
  if (el.dataset.fmedit) fmEdit(el.dataset.fmedit);
});
document.getElementById('fmCrumb').addEventListener('click', e=>{
  const el = e.target.closest('[data-fmgo]');
  if (el) loadFiles(el.dataset.fmgo);
});
document.getElementById('fmUpload').addEventListener('change', async e=>{
  const files = [...e.target.files];
  e.target.value = '';
  if (!files.length || !fmPath) return;
  const fd = new FormData();
  files.forEach(f=>fd.append('files', f));
  try {
    const res = await fetch(API+'/files/upload?dir='+encodeURIComponent(fmPath), {
      method:'POST', headers:{'Authorization':'Bearer '+token}, body: fd
    });
    const d = await res.json();
    if (!res.ok || !d.ok) throw new Error(d.err || '上传失败');
    toast('已上传 ' + files.length + ' 个文件');
    loadFiles();
  } catch(err){ toast(err.message); }
});

// ===== 磁盘管理 =====
async function loadDisks(){
  const grid = document.getElementById('diskGrid'), mgrid = document.getElementById('mountGrid');
  grid.textContent = '加载中…';
  try {
    const d = await call('GET','/disks');
    document.getElementById('diskHint').textContent = d.lsblk ? '' : '（当前环境未发现 lsblk 块设备信息）';
    grid.innerHTML = (d.devices||[]).length ? d.devices.map(dev=>{
      const mounts = (dev.mounts||[]).map(m=>{
        const u = m.usage;
        return '<div class="mh-group"><div class="row-flex" style="justify-content:space-between">'+
          '<h3 style="font-size:13px">'+esc(m.mountpoint)+'</h3>'+
          '<button class="btn sm danger" data-umount="'+esc(m.mountpoint)+'">卸载</button></div>'+
          (u?bar('用量', u.used_pct, u.used_mb+' / '+u.total_mb+' MB'):'')+'</div>';
      }).join('');
      const stTag = (dev.mounts||[]).length ? '已挂载' : (dev.fstype==='swap' ? 'swap 分区' : '未挂载');
      return '<div class="app-card"><div class="app-head"><div class="app-icon">'+esc((dev.name||'?')[0])+'</div>'+
        '<div><div class="name">'+esc(dev.name)+(dev.model?' <span class="ver">'+esc(dev.model)+'</span>':'')+'</div>'+
        '<div class="ver">'+esc(dev.path)+' · '+esc(dev.size)+(dev.fstype?' · '+esc(dev.fstype):'')+'</div></div></div>'+
        (dev.removable?'<div class="app-desc" style="min-height:0">可移动设备</div>':'')+
        (mounts || '<div class="app-desc" style="min-height:0">'+stTag+'</div>')+
        '<div class="app-foot"><span class="state-tag">'+esc(stTag)+'</span>'+
        (dev.mountable?'<button class="btn sm" data-mountdev="'+esc(dev.path)+'" data-mountname="'+esc(dev.name)+'">挂载</button>':'')+
        '</div></div>';
    }).join('') : '<div class="empty-tip">未发现可管理的块设备</div>';
    mgrid.innerHTML = (d.mounts||[]).length ? d.mounts.map(m=>{
      const u = m.usage;
      return '<div class="app-card"><div class="app-head"><div class="app-icon">▪</div>'+
        '<div><div class="name">'+esc(m.mountpoint)+'</div>'+
        '<div class="ver">'+esc(m.device)+' · '+esc(m.fstype)+(m.read_only?' · 只读':'')+'</div></div></div>'+
        (u?bar('用量', u.used_pct, u.used_mb+' / '+u.total_mb+' MB'):'')+'</div>';
    }).join('') : '<div class="empty-tip">暂无</div>';
  } catch(e){ grid.textContent = e.message; }
}
let mountDevicePath = '';
function closeMountDlg(){ document.getElementById('mountMask').classList.add('hidden'); }
function openMountDlg(path, name){
  mountDevicePath = path;
  document.getElementById('mountTitle').textContent = '挂载 ' + name;
  document.getElementById('mntDevice').value = path;
  document.getElementById('mntPoint').value = '/mnt/' + name;
  document.getElementById('mntFstab').checked = false;
  document.getElementById('mountMask').classList.remove('hidden');
}
async function doMount(){
  const btn = document.getElementById('mntGo');
  btn.disabled = true;
  try {
    const d = await call('POST','/disks/mount',{device:mountDevicePath,
      mountpoint:document.getElementById('mntPoint').value.trim(),
      fstab:document.getElementById('mntFstab').checked});
    toast(d.msg || '已挂载'); closeMountDlg(); loadDisks();
  } catch(e){ toast(e.message); }
  btn.disabled = false;
}
document.getElementById('diskGrid').addEventListener('click', async e=>{
  const mb = e.target.closest('button[data-mountdev]');
  if (mb) return openMountDlg(mb.dataset.mountdev, mb.dataset.mountname);
  const ub = e.target.closest('button[data-umount]');
  if (!ub) return;
  if (!confirm('确定卸载 '+ub.dataset.umount+'？请先确保没有程序正在使用该挂载点。')) return;
  try { const d = await call('POST','/disks/umount',{target:ub.dataset.umount});
    toast(d.msg || '已卸载'); loadDisks();
  } catch(err){ toast(err.message); }
});

// ===== 应用配置弹窗 =====
let cfgAppId = '';
function closeAppCfg(){
  document.getElementById('appCfgMask').classList.add('hidden');
  document.getElementById('appCfgOutput').classList.add('hidden');
}
async function openAppCfg(id){
  try {
    const d = await call('GET','/apps/config?id='+encodeURIComponent(id));
    cfgAppId = id;
    document.getElementById('appCfgTitle').textContent = d.name + ' · 配置';
    document.getElementById('appCfgFields').innerHTML = d.fields.map(f=>{
      let input;
      if (f.type==='select'){
        input = '<select id="cf_'+esc(f.name)+'">'+f.options.map(o=>
          '<option value="'+esc(o)+'"'+(d.values[f.name]===o?' selected':'')+'>'+esc(o)+'</option>').join('')+'</select>';
      } else if (f.type==='textarea'){
        input = '<textarea id="cf_'+esc(f.name)+'" placeholder="'+esc(f.placeholder||'')+'">'+esc(d.values[f.name]||'')+'</textarea>';
      } else {
        input = '<input id="cf_'+esc(f.name)+'" type="'+(f.type==='password'?'password':'text')+
          '" value="'+esc(d.values[f.name]||'')+'" placeholder="'+esc(f.placeholder||'')+'">';
      }
      return '<div class="field"><div class="fl">'+esc(f.label)+'</div>'+input+
        (f.description?'<div class="fd">'+esc(f.description)+'</div>':'')+'</div>';
    }).join('');
    document.getElementById('appCfgOutput').classList.add('hidden');
    document.getElementById('appCfgMask').classList.remove('hidden');
  } catch(e){ toast(e.message); }
}
async function saveAppCfg(){
  const app = appData.find(a=>a.id===cfgAppId);
  if (!app) return;
  const values = {};
  app.config.forEach(f=>{
    const el = document.getElementById('cf_'+f.name);
    if (el) values[f.name] = el.value;
  });
  const btn = document.getElementById('appCfgSave');
  btn.disabled = true;
  try {
    const d = await call('POST','/apps/config?id='+encodeURIComponent(cfgAppId), {values});
    toast(d.msg || '已保存');
    if (d.output){
      const pre = document.getElementById('appCfgOutput');
      pre.textContent = d.output; pre.classList.remove('hidden');
    } else {
      closeAppCfg();
    }
    loadApps();
  } catch(e){ toast(e.message); }
  btn.disabled = false;
}

async function doChangePassword(){
  const oldP=document.getElementById('oldPwd').value, newP=document.getElementById('newPwd').value;
  if(!oldP||!newP) return toast('请填写完整');
  if(newP.length < 8) return toast('新密码至少 8 位');
  try {
    const d = await call('POST','/change-password',{old_password:oldP,new_password:newP});
    if(d && d.token){ token=d.token; localStorage.setItem('np_token', token); }
    toast('密码已修改');
    document.getElementById('oldPwd').value=''; document.getElementById('newPwd').value='';
  } catch(e){ toast(e.message); }
}
async function doReboot(){
  if(!confirm('确定重启系统？')) return;
  try { await call('POST','/reboot'); toast('系统正在重启'); } catch(e){ toast(e.message); }
}

async function loadHealth(){
  try {
    const res = await fetch(API+'/health');
    const d = await res.json();
    const h = d.data || {};
    const v = h.version ? 'v'+h.version : '';
    document.getElementById('appVersion').textContent = v;
    document.getElementById('aboutVer').textContent = v;
    document.getElementById('loginVersion').textContent = 'NanoPanel ' + v;
    if(h.platform && h.platform !== 'linux'){
      const n = document.getElementById('platformNotice');
      n.textContent = '当前运行在 '+h.platform+' 环境，服务/进程/日志等依赖 Linux 的功能不可用。';
      n.classList.remove('hidden');
    }
  } catch(e){ /* 健康检查失败不影响主流程 */ }
}

loadHealth();
const savedUser = localStorage.getItem('np_user');
if (savedUser) document.getElementById('loginUser').value = savedUser;
if (token) showMain(); else showLogin();

// ===== 主题切换 =====
// 初始：localStorage 优先，否则跟随系统；按钮在三种状态间切换（auto → dark → light）。
function applyTheme(){
  const saved = localStorage.getItem('np_theme') || 'auto';
  document.documentElement.removeAttribute('data-theme');
  if (saved === 'dark' || saved === 'light') {
    document.documentElement.setAttribute('data-theme', saved);
  }
  const btn = document.getElementById('themeBtn');
  if (btn) btn.textContent = saved === 'dark' ? '浅色' : (saved === 'light' ? '自动' : '深色');
}
function toggleTheme(){
  const saved = localStorage.getItem('np_theme') || 'auto';
  const next = saved === 'auto' ? 'dark' : (saved === 'dark' ? 'light' : 'auto');
  localStorage.setItem('np_theme', next);
  applyTheme();
}
applyTheme();
