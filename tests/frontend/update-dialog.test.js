/* 版本更新弹窗运行时回归
 * 根因：后端 ok(w, info) 已把数据放进响应的 data 字段，前端 call() 也返回了 data.data，
 * 但 checkUpdate()/autoUpdateCheck() 又多取了一层 .data，导致 u 恒为 {}：
 *   1) 「关于」页永远显示“已是最新版本”
 *   2) 进入首页的自动更新弹窗永远不弹出
 * 本测试用真实抓取到的 /update/check 响应，执行前端函数验证解析与弹窗链路。 */
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

// 按大括号配对提取一个顶层函数源码（跳过字符串/模板串内的括号）
function extractFn(name) {
  const m = js.match(new RegExp('(?:async\\s+)?function\\s+' + name + '\\s*\\('));
  if (!m) throw new Error('未找到函数 ' + name);
  const start = m.index;
  let i = js.indexOf('{', start + m[0].length - 1);
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
    else if (c === '}') {
      depth--;
      if (depth === 0) return js.slice(start, i + 1);
    }
  }
  throw new Error('函数大括号未闭合: ' + name);
}

// 极简 DOM mock：getElementById 返回可记录状态的通用元素
function makeDom() {
  const nodes = {};
  const mk = () => ({
    textContent: '', innerHTML: '', checked: false, href: '', style: {}, dataset: {},
    classList: {
      _hidden: true,
      add(c) { if (c === 'hidden') this._hidden = true; },
      remove(c) { if (c === 'hidden') this._hidden = false; },
      contains(c) { return c === 'hidden' ? this._hidden : false; }
    }
  });
  return {
    nodes,
    document: { getElementById: (id) => (nodes[id] = nodes[id] || mk()) }
  };
}

// 用户抓包得到的真实响应；call() 已解包 data，所以这里直接返回 data 内容
const payload = {
  current_version: 'v0.1.7',
  latest_version: '0.1.8',
  has_update: true,
  release_url: 'https://github.com/LACS-Official/NanoPanel/releases/tag/v0.1.8',
  status: 'ok',
  message: '发现新版本 v0.1.8',
  checked_at: '2026-11-19T13:10:17Z'
};
const callMock = async () => JSON.parse(JSON.stringify(payload));

(async () => {
  // async 检查需单独 await，用 acheck 收集结果
  const asyncResults = [];
  async function acheck(name, fn) {
    try { await fn(); asyncResults.push(true); console.log('  PASS  ' + name); }
    catch (e) { asyncResults.push(false); console.error('  FAIL  ' + name + ' -> ' + e.message); }
  }

  await acheck('checkUpdate 正确识别新版本（不再误报“已是最新”）', async () => {
    const { document, nodes } = makeDom();
    const fn = new Function('call', 'document', 'fmtVersion', 'esc', 'npVersion',
      extractFn('checkUpdate') + '\nreturn checkUpdate;')(
      callMock, document, (v) => v, (v) => String(v), 'v0.1.7');
    await fn();
    const htmlOut = nodes.updateStatus.innerHTML;
    assert(htmlOut.includes('发现新版本'), '状态栏未显示“发现新版本”，实际: ' + htmlOut);
    assert(htmlOut.includes('0.1.8'), '状态栏未显示新版本号，实际: ' + htmlOut);
  });

  await acheck('autoUpdateCheck 触发更新弹窗', async () => {
    let shown = null;
    const { document } = makeDom();
    const fn = new Function('call', 'showUpdateDialog', 'localStorage', 'setTimeout',
      'let autoUpdateChecked = false;\n' + extractFn('autoUpdateCheck') + '\nreturn autoUpdateCheck;')(
      callMock,
      (u) => { shown = u; },
      { getItem() { return null; }, setItem() {} },
      () => 0  // 不真正调度 15s 超时 timer，避免测试进程悬挂
    );
    await fn();
    assert(shown, '未调用 showUpdateDialog');
    assert.strictEqual(shown.has_update, true);
    assert.strictEqual(shown.latest_version, '0.1.8');
  });

  await acheck('showUpdateDialog 填充版本并显示弹窗', async () => {
    const { document, nodes } = makeDom();
    const fn = new Function('document', 'fmtVersion', 'npVersion',
      extractFn('showUpdateDialog') + '\nreturn showUpdateDialog;')(
      document, (v) => v, 'v0.1.7');
    fn(payload);
    assert.strictEqual(nodes.updCurrent.textContent, 'v0.1.7');
    assert.strictEqual(nodes.updLatest.textContent, '0.1.8');
    assert.strictEqual(nodes.updLatest.dataset.ver, '0.1.8');
    assert.strictEqual(nodes.updateMask.classList.contains('hidden'), false, '弹窗未显示');
  });

  check('弹窗为强制升级：无“稍后再说”/“不再提示”/遮罩关闭', () => {
    const mask = html.match(/<div class="modal-mask[^"]*" id="updateMask"[^>]*>/);
    assert(mask, '未找到 updateMask');
    assert(!/data-act/.test(mask[0]), '遮罩仍可点击关闭');
    const start = html.indexOf('id="updateMask"');
    const block = html.slice(start, html.indexOf('<script src="chart.js"', start));
    assert(!block.includes('updSkip'), '仍存在“不再提示该版本”');
    assert(!block.includes('closeUpdateDlg'), '仍存在“稍后再说”');
    assert(block.includes('applyUpdateFromDlg()'), '缺少“立即升级”按钮');
  });

  check('前端不再错误地多取一层 .data', () => {
    assert(!/const u = \(d && d\.data\)/.test(js), 'autoUpdateCheck 仍取 d.data');
    assert(!/const u = d\.data/.test(js), 'checkUpdate 仍取 d.data');
  });

  const passed = results.filter(Boolean).length + asyncResults.filter(Boolean).length;
  const total = results.length + asyncResults.length;
  console.log(`\n${passed}/${total} checks passed`);
  process.exit(passed === total ? 0 : 1);
})();
