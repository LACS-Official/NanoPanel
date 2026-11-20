/* 升级版本号显示回归
 * 后端 latest_version 为 "0.2.2"（无 v），前端 fmtVersion 已统一补 v。
 * 早期 openUpdateLog 与 done 事件里又手动拼了一次 'v'，导致标题显示 "vv0.2.2"。
 * 本测试锁定：无论来源带不带 v，标题都只显示一个 v。 */
const fs = require('fs');
const path = require('path');
const assert = require('assert');

const root = path.join(__dirname, '..', '..');
const js = fs.readFileSync(path.join(root, 'web/app.js'), 'utf8');
const results = [];
function check(name, fn) {
  try { fn(); results.push(true); console.log('  PASS  ' + name); }
  catch (e) { results.push(false); console.error('  FAIL  ' + name + ' -> ' + e.message); }
}
async function checkAsync(name, fn) {
  try { await fn(); results.push(true); console.log('  PASS  ' + name); }
  catch (e) { results.push(false); console.error('  FAIL  ' + name + ' -> ' + e.message); }
}

// 与 app.js 中 fmtVersion 保持一致的真实实现（而非 identity 桩）
const fmtVersion = (s) => {
  const raw = String(s || '').replace(/^v+/i, '').trim();
  return raw ? 'v' + raw : '';
};

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
  const mk = () => ({
    textContent: '', innerHTML: '', style: {}, dataset: {},
    classList: {
      hidden: true,
      add(c) { if (c === 'hidden') this.hidden = true; },
      remove(c) { if (c === 'hidden') this.hidden = false; },
      contains(c) { return c === 'hidden' ? this.hidden : false; }
    }
  });
  return { nodes, document: { getElementById: (id) => (nodes[id] = nodes[id] || mk()) } };
}

const FN_NAMES = ['applyUpdate', 'updLogAppend', 'updLogState', 'openUpdateLog',
  'closeUpdateLogDlg', 'finishUpdateLog', 'trackUpdateState'];

function buildApply(dom, timers, sse, latest) {
  const body = FN_NAMES.map(extractFn).join('\n');
  const fetchMock = async () => ({
    status: 200,
    headers: { get: () => 'text/event-stream; charset=utf-8' },
    body: { getReader: () => {
      let sent = false;
      return { read: async () => {
        if (sent) return { done: true };
        sent = true;
        return { done: false, value: Buffer.from(sse, 'utf8') };
      }};
    }}
  });
  return new Function(
    'fetch', 'document', 'token', 'API', 'fmtVersion', 'confirm', 'setTimeout',
    'location', 'toast', 'upgrading', 'npLatestVer',
    body + '\nreturn applyUpdate;'
  )(fetchMock, dom.document, 'tk', '/api/v1', fmtVersion, () => true,
    (_fn, ms) => timers.push(ms), { reload: () => {} }, () => {}, false, latest || '');
}

console.log('\n=== 升级版本号显示（vv 前缀回归） ===');

check('源码中不再手动叠加 v 前缀', () => {
  assert(!js.includes("'→ v'+fmtVersion("), '仍存在手动 v 前缀拼接，会产生 vv');
  assert(js.includes("'→ '+fmtVersion("), '缺少 fmtVersion 归一化调用');
});

check('openUpdateLog 对带/不带 v 的版本都只显示一个 v', () => {
  const dom = makeDom();
  const body = ['openUpdateLog', 'updLogState'].map(extractFn).join('\n');
  const open = new Function('document', 'fmtVersion', body + '\nreturn openUpdateLog;')(
    dom.document, fmtVersion);
  open('0.2.2');
  assert.strictEqual(dom.nodes.updLogVer.textContent, '→ v0.2.2');
  open('v0.2.2');
  assert.strictEqual(dom.nodes.updLogVer.textContent, '→ v0.2.2');
  assert(!dom.nodes.updLogVer.textContent.includes('vv'), '出现 vv 双前缀');
});

(async () => {
  await checkAsync('done 事件回传的裸版本号显示为 → v0.2.2（无 vv）', async () => {
    const sse = 'data: ' + JSON.stringify({
      event: 'done', ok: true, status: 'updating', latest: '0.2.2'
    }) + '\n\n';
    const dom = makeDom();
    const timers = [];
    await buildApply(dom, timers, sse, '0.2.2')(true);
    assert.strictEqual(dom.nodes.updLogVer.textContent, '→ v0.2.2');
    assert(!dom.nodes.updLogVer.textContent.includes('vv'), 'done 事件产生 vv 双前缀');
  });

  await checkAsync('done 事件回传带 v 的版本号同样不出现 vv', async () => {
    const sse = 'data: ' + JSON.stringify({
      event: 'done', ok: true, status: 'updating', latest: 'v0.2.2'
    }) + '\n\n';
    const dom = makeDom();
    const timers = [];
    await buildApply(dom, timers, sse, 'v0.2.2')(true);
    assert.strictEqual(dom.nodes.updLogVer.textContent, '→ v0.2.2');
  });

  const passed = results.filter(Boolean).length;
  console.log('\n' + passed + '/' + results.length + ' passed');
  if (passed !== results.length) process.exit(1);
})();
