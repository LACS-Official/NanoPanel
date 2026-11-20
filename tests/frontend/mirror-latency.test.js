/* 镜像源测速表格运行时回归
 * 根因：后端 mirrorTestResult 的 JSON 字段是 latency_ms，前端却读 r.latency，
 * 永远 undefined → 延迟列显示为空/横杠，但 ok=true 仍显示"可用"，自相矛盾。
 * 本测试用后端真实响应格式执行 testMirrors()，验证延迟数值确实渲染进表格。 */
const fs = require('fs');
const path = require('path');
const assert = require('assert');

const root = path.join(__dirname, '..', '..');
const read = (p) => fs.readFileSync(path.join(root, p), 'utf8');
const js = read('web/app.js');
const results = [];
function check(name, fn) {
  try { fn(); results.push(true); console.log('  PASS  ' + name); }
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

// 后端 /settings/mirror/test 的真实响应格式（data 已被 call() 解包）
const payload = {
  results: [
    { id: 'github', name: 'GitHub 官方', latency_ms: 614.8, ok: true, status: 'HTTP 200' },
    { id: 'ghfast', name: 'ghfast', latency_ms: 1033.94, ok: true, status: 'HTTP 200' },
    { id: 'ghproxy', name: 'ghproxy', latency_ms: 5086.19, ok: true, status: 'HTTP 200' },
    { id: 'gh-proxy', name: 'gh-proxy', latency_ms: 0, ok: false, status: '连接失败' }
  ]
};

async function run() {
  console.log('\n=== 镜像测速延迟渲染 ===');
  await check('testMirrors 用后端 latency_ms 字段渲染真实延迟数值', async () => {
    const nodes = {};
    const box = { innerHTML: '' };
    nodes.mirrorTestResult = box;
    const dom = { getElementById: (id) => nodes[id] || null };
    const testMirrors = new Function(
      'call', 'document', 'esc', 'mirrorCurrentId',
      extractFn('testMirrors') + '\nreturn testMirrors;'
    )(async () => payload, dom, (v) => String(v), 'ghfast');

    await testMirrors();

    assert(box.innerHTML.includes('614.8 ms'), '未渲染 GitHub 官方延迟 614.8 ms：' + box.innerHTML.slice(0, 300));
    assert(box.innerHTML.includes('1033.9 ms'), '未渲染 ghfast 延迟：' + box.innerHTML.slice(0, 300));
    assert(box.innerHTML.includes('5086.2 ms'), '未渲染 ghproxy 延迟：' + box.innerHTML.slice(0, 300));
    assert(box.innerHTML.includes('—'), '不可用源延迟应显示横杠');
    assert(!box.innerHTML.includes('<1 ms'), '不应出现兜底的 <1 ms');
    assert(!box.innerHTML.includes('undefined'), '渲染结果不应出现 undefined（字段名写错的典型症状）');
  });

  const passed = results.filter(Boolean).length;
  console.log('\n' + passed + '/' + results.length + ' passed');
  if (passed !== results.length) process.exit(1);
}
run();
