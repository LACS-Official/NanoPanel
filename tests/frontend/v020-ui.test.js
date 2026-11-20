/* v0.2.0 UI 改造回归：温度渲染、CPU 字段、概览/关于双列布局、镜像源 UI、深色对比度
 * 温度渲染部分为运行时测试：用 mock DOM 喂入含 HTML 的字符串，验证不会原样输出标签。 */
const fs = require('fs');
const path = require('path');
const assert = require('assert');

const root = path.join(__dirname, '..', '..');
const read = (p) => fs.readFileSync(path.join(root, p), 'utf8');
const html = read('web/index.html');
const js = read('web/app.js');
const css = read('web/style.css');
const chart = read('web/chart.js');
const results = [];
function check(name, fn) {
  try { fn(); results.push(true); console.log('  PASS  ' + name); }
  catch (e) { results.push(false); console.error('  FAIL  ' + name + ' -> ' + e.message); }
}

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
    else if (c === '}') { depth--; if (depth === 0) return js.slice(start, i + 1); }
  }
  throw new Error('函数大括号未闭合: ' + name);
}

// ---- 温度运行时渲染 ----
check('tempView 输出数值+单位，不输出原始 HTML 标签', () => {
  const fn = new Function('esc', extractFn('tempView') + '\nreturn tempView;')((v) => String(v));
  const out = fn({ celsius: 38.4, status: 'ok' });
  assert(/38/.test(out) && /℃/.test(out), '应含数值与单位: ' + out);
  assert(!/<b |style=/.test(out), '不应输出原始标签: ' + out);
  assert(/tv-ok/.test(out), '正常状态应有 tv-ok 类');
});

check('tempView 异常读数显示“读数异常”而非具体温度', () => {
  const fn = new Function('esc', extractFn('tempView') + '\nreturn tempView;')((v) => String(v));
  const out = fn({ celsius: -50, status: 'suspicious' });
  assert(/读数异常/.test(out), '应标记异常: ' + out);
  assert(!/-50/.test(out), '不应显示异常数值: ' + out);
});

check('tempView 高温状态分级正确', () => {
  const fn = new Function('esc', extractFn('tempView') + '\nreturn tempView;')((v) => String(v));
  assert(/tv-crit/.test(fn({ celsius: 98, status: 'critical' })), 'critical 状态');
  assert(/tv-hot/.test(fn({ celsius: 85, status: 'hot' })), 'hot 状态');
  assert(/tv-warm/.test(fn({ celsius: 70, status: 'warm' })), 'warm 状态');
});

// ---- 静态结构 ----
check('概览为双卡片布局且顺序正确', () => {
  assert(html.includes('class="overview-grid"'), '缺少 overview-grid');
  assert(/\.overview-grid\s*\{[^}]*grid-template-columns:repeat\(2/.test(css), 'CSS 未定义两列');
  const ov = html.slice(html.indexOf('id="view-overview"'), html.indexOf('id="view-processes"'));
  const order = ['ov-sys', 'ov-res', 'ov-trend', 'ov-stat'].map(c => ov.indexOf(c));
  assert(order.every((v, i) => v >= 0 && (i === 0 || v > order[i - 1])), '卡片顺序应为 系统/资源/趋势/概览: ' + order);
});

check('系统信息含架构/核心线程/负载字段，温度独立成区', () => {
  assert(/metric\('架构'/.test(js), '缺少架构字段');
  assert(/metric\('核心 \/ 线程'/.test(js), '缺少核心/线程字段');
  assert(/metric\('负载 \(1\/5\/15\)'/.test(js), '缺少负载字段');
  assert(!/核心数 \/ 负载/.test(js), '旧的误导性标签未清除');
  assert(html.includes('id="tempReadings"'), '缺少独立温度区');
});

check('关于页为两列设计', () => {
  assert(html.includes('class="about-grid"'), '缺少 about-grid');
  assert(/\.about-grid\s*\{[^}]*grid-template-columns:repeat\(2/.test(css), 'CSS 未定义两列');
});

check('镜像源选择与测试结果 UI 齐备', () => {
  for (const id of ['mirrorSource', 'mirrorTestResult']) {
    assert(html.includes('id="' + id + '"'), '缺少 ' + id);
  }
  assert(html.includes('selectMirror()') && html.includes('testMirrors()'), '缺少按钮动作');
  assert(/function testMirrors\(/.test(js) && /function selectMirror\(/.test(js), '缺少前端函数');
  assert(/"selectMirror\(\)"/.test(js) && /"testMirrors\(\)"/.test(js), '动作未注册白名单');
  assert(/mirror-cur|cur-tag/.test(css) && /st-ok/.test(css), '测试结果样式缺失');
  // 亚毫秒响应时延迟不能显示为空（否则出现"状态可用但延迟 —"的自相矛盾）
  assert(/r\.ok && r\.latency_ms/.test(js), '延迟读取的字段名与后端 latency_ms 不一致');
  assert(!/r\.latency\b/.test(js.replace('latency_ms', '')), '仍在读取错误的 r.latency 字段');
  const apiGo = read('internal/api/mirror_source.go');
  assert(/Microseconds\(\)/.test(apiGo), '后端未按亚毫秒精度计时');
});

check('后端镜像源 API 三端点已注册', () => {
  const main = read('main.go');
  for (const p of ['settings/mirror/candidates', 'settings/mirror/test', 'settings/mirror/select']) {
    assert(main.includes(p), '缺少路由: ' + p);
  }
  const api = read('internal/api/mirror_source.go');
  assert(/findMirrorCandidate/.test(api), '缺少白名单校验');
  assert(/NP_MIRROR_CONF/.test(api), '缺少可测试的配置路径覆盖');
});

check('深色模式对比度：变量与图表配色覆盖', () => {
  // 两个深色块都应覆盖图表色（浅色 block + 2 个深色 block 共出现 3 次 --chart-cpu 定义）
  const occurrences = (css.match(/--chart-cpu:/g) || []).length;
  assert(occurrences >= 3, '图表色变量应在 :root 与两个深色块中定义，实际 ' + occurrences);
  assert(/--chart-cpu:#4da3ff/.test(css), '深色图表色未提亮');
  assert(/html\[data-theme="dark"\]\s+\.btn\.gray/.test(css), '深色按钮对比度未覆盖');
  assert(/--bg:#111113/.test(css), '深色背景未加深');
  assert(/resolveColors/.test(chart), '图表未读取主题色');
});

const passed = results.filter(Boolean).length;
console.log(`\n${passed}/${results.length} checks passed`);
process.exit(passed === results.length ? 0 : 1);
