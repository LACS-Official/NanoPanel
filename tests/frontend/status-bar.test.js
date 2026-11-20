/* 底部状态栏 + 分档通知 UI 回归 */
const fs = require('fs');
const path = require('path');
const assert = require('assert');

const root = path.join(__dirname, '..', '..');
const read = (p) => fs.readFileSync(path.join(root, p), 'utf8');
const js = read('web/app.js');
const html = read('web/index.html');
const css = read('web/style.css');
const results = [];
async function check(name, fn) {
  try { await fn(); results.push(true); console.log('  PASS  ' + name); }
  catch (e) { results.push(false); console.error('  FAIL  ' + name + ' -> ' + e.message); }
}

function extractFn(name) {
  const m = js.match(new RegExp('(?:async\\s+)?function\\s+' + name + '\\s*\\('));
  if (!m) throw new Error('未找到函数 ' + name);
  let i = js.indexOf('{', m.index + m[0].length - 1);
  let depth = 0, inStr = null, escaped = false;
  for (; i < js.length; i++) {
    const c = js[i];
    if (inStr) {
      if (escaped) { escaped = false; continue; }
      if (c === '\\') { escaped = true; continue; }
      if (c === inStr) inStr = null;
      continue;
    }
    if (c === '"' || c === "'" || c === '`') { inStr = c; continue; }
    if (c === '{') depth++;
    else if (c === '}') { depth--; if (depth === 0) return js.slice(m.index, i + 1); }
  }
  throw new Error('函数大括号未闭合: ' + name);
}

function makeDom() {
  const nodes = {};
  const mk = () => {
    const classes = new Set();
    return {
      textContent: '', style: {}, dataset: {},
      get className() { return Array.from(classes).join(' '); },
      set className(v) { classes.clear(); String(v).split(/\s+/).filter(Boolean).forEach(c => classes.add(c)); },
      classList: {
        add(c) { classes.add(c); },
        remove(c) { classes.delete(c); },
        toggle(c, force) {
          const shouldAdd = force === undefined ? !classes.has(c) : !!force;
          if (shouldAdd) classes.add(c); else classes.delete(c);
          return shouldAdd;
        },
        contains(c) { return classes.has(c); }
      }
    };
  };
  return { nodes, document: { getElementById: (id) => (nodes[id] = nodes[id] || mk()) } };
}

(async () => {
  console.log('\n=== 底部状态栏 ===');

  await check('状态栏与通知面板结构完整，分档过滤器齐全', () => {
    assert(html.includes('id="statusBar"'), '缺少状态栏');
    assert(html.includes('id="sbLatest"') && html.includes('id="sbUnread"'), '缺少最新通知/未读显示');
    assert(html.includes('id="notifPanel"') && html.includes('id="notifList"'), '缺少通知面板');
    for (const lv of ['all', 'danger', 'warn', 'success', 'info']) {
      assert(html.includes("filterNotif('" + lv + "')"), '缺少分档过滤器: ' + lv);
    }
  });

  await check('状态栏操作已注册 CSP 白名单', () => {
    for (const act of ['toggleNotifPanel()', 'markAllNotifRead()', 'clearNotifications()']) {
      assert(js.includes('"' + act + '":'), '缺少动作映射: ' + act);
    }
  });

  await check('状态栏样式存在且主视图预留底部空间', () => {
    assert(/#mainView \{ padding-bottom:44px/.test(css), '主视图未预留底部空间，内容会被状态栏遮挡');
    assert(/\.statusbar \{ position:fixed/.test(css), '状态栏未固定底部');
    for (const lv of ['info', 'success', 'warn', 'danger']) {
      assert(css.includes('.np-item.lv-' + lv), '缺少档位配色: ' + lv);
    }
  });

  await check('loadNotifications 用后端字段渲染最新通知、档位着色与未读数', async () => {
    const dom = makeDom();
    const backend = { items: [
      { id: 3, time: '11-20 16:00:00', level: 'danger', level_text: '告警', event: 'security', title: '登录失败', content: 'IP 1.2.3.4' }
    ], unread: 2 };
    const loadNotifications = new Function('call', 'document', 'LEVEL_ICON',
      'let notifOpen = false; let notifCache = []; function renderNotifList(){}' +
      extractFn('loadNotifications') + '\nreturn loadNotifications;'
    )(async () => backend, dom.document, { info: '●', success: '✔', warn: '▲', danger: '✖' });

    await loadNotifications();
    assert.strictEqual(dom.nodes.sbUnread.textContent, '2', '未读数未显示');
    assert(dom.nodes.sbUnread.classList.contains('hidden') === false, '未读徽标应可见');
    assert(dom.nodes.sbLatest.textContent.includes('登录失败'), '最新通知标题未显示');
    assert(dom.nodes.sbLatest.className.includes('lv-danger'), '最新通知应按档位着色');
    assert(dom.nodes.sbDot.classList.contains('has-unread'), '有未读时圆点应高亮');
  });

  const passed = results.filter(Boolean).length;
  console.log('\n' + passed + '/' + results.length + ' passed');
  if (passed !== results.length) process.exit(1);
})();
