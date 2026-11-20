/* 升级实时日志弹窗回归
 * 1) 点击升级必须强制弹出日志窗口（完成前没有关闭入口）；
 * 2) 日志通过 /update/apply/stream 的 SSE 实时输出，而非旧的同步 POST；
 * 3) 探测结果/下载节点/进度/重启要驱动界面状态；
 * 4) 完成后显示操作按钮并定时刷新。 */
const fs = require('fs');
const path = require('path');
const assert = require('assert');

const root = path.join(__dirname, '..', '..');
const read = (p) => fs.readFileSync(path.join(root, p), 'utf8');
const js = read('web/app.js');
const html = read('web/index.html');
const results = [];
function check(name, fn) {
  try { fn(); results.push(true); console.log('  PASS  ' + name); }
  catch (e) { results.push(false); console.error('  FAIL  ' + name + ' -> ' + e.message); }
}
async function checkAsync(name, fn) {
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

const events = [
  { event: 'status', phase: 'checking' },
  { event: 'log', line: '[15:00:00] 正在探测最新版本...' },
  { event: 'log', line: '[15:00:01] 下载节点探测结果（目标 nanopanel-linux-amd64.tar.gz）：' },
  { event: 'log', line: '[15:00:01]   ✓ ghfast 182 ms  https://ghfast.top/https://github.com' },
  { event: 'log', line: '[15:00:02] 正在从 ghfast 下载 nanopanel-linux-amd64.tar.gz（8.4 MB）...' },
  { event: 'log', line: '[15:00:02]   进度 1.2 MB / 8.4 MB (14%)' },
  { event: 'log', line: '[15:00:03] 正在校验新二进制...' },
  { event: 'log', line: '[15:00:04] 正在重启面板服务...' },
  { event: 'done', ok: true, status: 'updating', latest: '0.2.1' }
];
const sse = events.map((e) => 'data: ' + JSON.stringify(e) + '\n\n').join('');
let fetchArgs = null;
const fetchMock = async (url, opts) => {
  fetchArgs = { url, opts };
  return {
    status: 200,
    headers: { get: () => 'text/event-stream; charset=utf-8' },
    body: {
      getReader: () => {
        let sent = false;
        return {
          read: async () => {
            if (sent) return { done: true };
            sent = true;
            return { done: false, value: Buffer.from(sse, 'utf8') };
          }
        };
      }
    }
  };
};

const FN_NAMES = ['applyUpdate', 'updLogAppend', 'updLogState', 'openUpdateLog',
  'closeUpdateLogDlg', 'finishUpdateLog', 'trackUpdateState'];
function buildApply(dom, timers) {
  const body = FN_NAMES.map(extractFn).join('\n');
  return new Function(
    'fetch', 'document', 'token', 'API', 'fmtVersion', 'confirm', 'setTimeout', 'location', 'toast', 'upgrading', 'npLatestVer',
    body + '\nreturn applyUpdate;'
  )(fetchMock, dom.document, 'tk', '/api/v1', (v) => v, () => true,
    (_fn, ms) => timers.push(ms), { reload: () => {} }, () => {}, false, '');
}

console.log('\n=== 升级实时日志 ===');

check('日志弹窗存在，且初始没有关闭按钮（强制展示）', () => {
  const m = html.match(/<div class="modal-mask hidden" id="updLogMask">[\s\S]*?<\/div>\s*<\/div>\s*<\/div>/);
  assert(m, '缺少 updLogMask 弹窗');
  const block = m[0];
  assert(block.includes('id="updLogOutput"'), '缺少日志输出区');
  assert(block.includes('id="updLogState"'), '缺少状态标签');
  assert(block.includes('id="updLogNode"'), '缺少当前节点显示');
  assert(/id="updLogActions"[^>]*style="display:none"/.test(block), '操作按钮初始必须隐藏');
  assert(!/data-act="if\(event\.target===this\)/.test(block), '遮罩不可点击关闭');
});

check('applyUpdate 使用 SSE 流式接口并注册了日志动作', () => {
  assert(/fetch\(API\+'\/update\/apply\/stream'/.test(js), '升级未走 SSE 接口');
  assert(!/call\('POST','\/update\/apply'/.test(js), '仍在使用旧的同步升级接口');
  assert(/"closeUpdateLogDlg\(\)":/.test(js) && /"reloadPage\(\)":/.test(js), '动作未注册白名单');
});

(async () => {
  await checkAsync('运行时：日志实时输出、节点与阶段正确显示、完成后定时刷新', async () => {
    const dom = makeDom();
    const timers = [];
    await buildApply(dom, timers)(true);
    const out = dom.nodes.updLogOutput.textContent;
    assert(out.includes('正在探测最新版本'), '缺少版本检查日志');
    assert(out.includes('下载节点探测结果'), '缺少节点探测日志');
    assert(out.includes('正在从 ghfast 下载'), '缺少下载节点日志');
    assert(out.includes('进度 1.2 MB / 8.4 MB (14%)'), '缺少下载进度日志');
    assert(out.includes('正在重启面板服务'), '缺少重启日志');
    assert.strictEqual(dom.nodes.updLogState.textContent, '升级完成 · 即将重启', '最终状态不正确');
    assert(dom.nodes.updLogNode.textContent.includes('ghfast'), '未显示当前下载节点');
    assert.strictEqual(dom.nodes.updLogActions.style.display, '', '完成后应显示操作按钮');
    assert(timers.includes(6000), '完成后应定时刷新页面');
    assert(fetchArgs.url.includes('/update/apply/stream'), '请求地址不正确');
    assert.strictEqual(fetchArgs.opts.method, 'POST', 'SSE 升级请求应为 POST');
  });

  await checkAsync('失败时日志仍完整展示且显示失败状态', async () => {
    const failEvents = [
      { event: 'log', line: '[15:00:00] 开始检查新版本（当前 v0.2.0）' },
      { event: 'log', line: '[15:00:01] 所有下载节点均失败，请检查网络或稍后重试' },
      { event: 'done', ok: false, status: 'failed', msg: '所有下载节点均失败' }
    ];
    const failSSE = failEvents.map((e) => 'data: ' + JSON.stringify(e) + '\n\n').join('');
    const dom = makeDom();
    const timers = [];
    const body = FN_NAMES.map(extractFn).join('\n');
    const applyUpdate = new Function(
      'fetch', 'document', 'token', 'API', 'fmtVersion', 'confirm', 'setTimeout', 'location', 'toast', 'upgrading', 'npLatestVer',
      body + '\nreturn applyUpdate;'
    )(async () => ({
      status: 200,
      headers: { get: () => 'text/event-stream; charset=utf-8' },
      body: { getReader: () => {
        let sent = false;
        return { read: async () => {
          if (sent) return { done: true };
          sent = true;
          return { done: false, value: Buffer.from(failSSE, 'utf8') };
        }};
      }}
    }), dom.document, 'tk', '/api/v1', (v) => v, () => true,
      () => {}, { reload: () => {} }, () => {}, false, '');
    await applyUpdate(true);
    assert(dom.nodes.updLogOutput.textContent.includes('所有下载节点均失败'), '失败日志未展示');
    assert.strictEqual(dom.nodes.updLogState.textContent, '升级失败', '失败状态未显示');
    assert.strictEqual(dom.nodes.updLogActions.style.display, '', '失败后应允许关闭');
    assert.strictEqual(timers.length, 0, '失败时不应自动刷新');
  });

  const passed = results.filter(Boolean).length;
  console.log('\n' + passed + '/' + results.length + ' passed');
  if (passed !== results.length) process.exit(1);
})();
