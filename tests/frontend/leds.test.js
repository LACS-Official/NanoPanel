/* 指示灯（LED）面板控制回归
 * 1) 设置→系统子页存在 LED 卡片，动作全部走 data-act 白名单（CSP 严格模式）；
 * 2) loadLEDs 渲染名称/亮度/触发器/状态/开关按钮，名称必须转义防注入；
 * 3) 单灯开关与批量开关的请求参数正确，批量关闭默认保留电源灯；
 * 4) 不支持的平台给出明确提示而非空白或报错。 */
const fs = require('fs');
const path = require('path');
const assert = require('assert');

const root = path.join(__dirname, '..', '..');
const read = (p) => fs.readFileSync(path.join(root, p), 'utf8');
const js = read('web/app.js');
const html = read('web/index.html');
const css = read('web/style.css');
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
    addEventListener() {},
    classList: { add() {}, remove() {}, contains() { return false; } }
  });
  return { nodes, document: { getElementById: (id) => (nodes[id] = nodes[id] || mk()) } };
}

// esc() 函数体含正则 /"/g，无法用简易大括号解析器安全提取；
// 与项目其它前端测试一致，改为作为参数注入（此处用与生产一致的真实实现，
// 以保留「名称转义防 XSS」断言的有效性）。
const escImpl = (v) => String(v == null ? '' : v)
  .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
const FN_NAMES = ['loadLEDs', 'setLED', 'setAllLEDs'];
function build(dom, callMock, toastMock) {
  const body = FN_NAMES.map(extractFn).join('\n');
  return new Function('document', 'call', 'toast', 'ledCache', 'esc',
    body + '\nreturn { loadLEDs, setLED, setAllLEDs };'
  )(dom.document, callMock, toastMock, [], escImpl);
}

const LEDS = [
  { name: 'power', brightness: 1, max_brightness: 1, trigger: 'none', writable: true, is_power: true },
  { name: 'blue:heartbeat', brightness: 255, max_brightness: 255, trigger: 'heartbeat', writable: true, is_power: false },
  { name: 'green-led', brightness: 0, max_brightness: 1, trigger: '', writable: true, is_power: false },
];

console.log('\n=== 指示灯（LED）控制 ===');

check('设置→系统含 LED 卡片与表格容器', () => {
  const m = html.match(/<h2>指示灯（LED）<\/h2>[\s\S]*?<div class="card">\s*<h2>系统操作<\/h2>/);
  assert(m, '缺少指示灯卡片');
  const block = m[0];
  assert(block.includes('id="ledBody"'), '缺少 LED 表格 tbody');
  assert(block.includes('id="ledHint"'), '缺少提示区');
  assert(block.includes('/sys/class/leds'), '缺少 sysfs 路径说明');
});

check('按钮走 data-act 白名单，且没有内联事件处理器', () => {
  for (const act of ['loadLEDs()', 'setAllLEDs(false)', 'setAllLEDs(true)']) {
    assert(html.includes('data-act="' + act + '"'), '缺少按钮动作: ' + act);
    assert(js.includes('"' + act + '":'), 'INLINE_ACTIONS 未注册: ' + act);
  }
  const card = html.slice(html.indexOf('<h2>指示灯（LED）</h2>'),
    html.indexOf('<h2>系统操作</h2>'));
  assert(!/on(click|change|keydown)=/.test(card), 'LED 卡片不应含内联事件处理器');
});

check('进入「设置 → 系统」子页时加载 LED 列表', () => {
  assert(/v==='settings' && k==='system'\)\{ loadTLS\(\); loadLEDs\(\); \}/.test(js),
    'setSub 未在 system 子页触发 loadLEDs');
});

check('样式表包含 LED 状态点定义', () => {
  assert(css.includes('.led-dot.on'), '缺少点亮状态样式');
  assert(css.includes('.led-power'), '缺少电源灯标记样式');
});

check('后端接口路径正确', () => {
  assert(js.includes("call('GET','/leds')"), '缺少列表请求');
  assert(js.includes("call('POST','/leds/set'"), '缺少单灯设置请求');
  assert(js.includes("call('POST','/leds/all'"), '缺少批量设置请求');
  assert(js.includes('data-led='), '缺少单灯开关的事件委托标记');
});

(async () => {
  await checkAsync('运行时：渲染全部 LED，亮/灭与开关文案正确', async () => {
    const dom = makeDom();
    const calls = [];
    const callMock = async (method, path) => {
      calls.push({ method, path });
      return { leds: LEDS, supported: true };
    };
    const api = build(dom, callMock, () => {});
    await api.loadLEDs();
    const out = dom.nodes.ledBody.innerHTML;
    assert(out.includes('power') && out.includes('blue:heartbeat') && out.includes('green-led'),
      '未渲染全部 LED 名称');
    assert(out.includes('电源灯'), '未标记电源灯');
    assert(out.includes('255 / 255'), '未显示亮度与最大亮度');
    assert(out.includes('heartbeat'), '未显示触发器');
    assert(out.includes('led-dot on'), '点亮的灯应有 on 状态点');
    assert(out.includes('data-on="false"'), '点亮的灯应显示「关闭」(on=false)');
    assert(out.includes('data-on="true"'), '熄灭的灯应显示「开启」(on=true)');
    assert.strictEqual(dom.nodes.ledHint.style.display, 'none', '有数据时应隐藏提示');
    assert.strictEqual(calls[0].path, '/leds');
  });

  await checkAsync('运行时：LED 名称经过转义，防止 HTML 注入', async () => {
    const dom = makeDom();
    const callMock = async () => ({ leds: [
      { name: '<img src=x onerror=alert(1)>', brightness: 1, max_brightness: 1, trigger: '', writable: true, is_power: false }
    ], supported: true });
    const api = build(dom, callMock, () => {});
    await api.loadLEDs();
    const out = dom.nodes.ledBody.innerHTML;
    assert(!out.includes('<img src=x'), 'LED 名称未转义，存在 XSS 风险');
    assert(out.includes('&lt;img'), '应转义为实体');
  });

  await checkAsync('运行时：单灯开关发送 name 与 on', async () => {
    const dom = makeDom();
    const calls = [];
    const callMock = async (method, path, body) => {
      calls.push({ method, path, body });
      return { leds: LEDS, supported: true };
    };
    const toasts = [];
    const api = build(dom, callMock, (m) => toasts.push(m));
    await api.setLED('blue:heartbeat', false);
    const set = calls.find(c => c.path === '/leds/set');
    assert(set, '未发起 /leds/set 请求');
    assert.strictEqual(set.method, 'POST');
    assert.deepStrictEqual(set.body, { name: 'blue:heartbeat', on: false });
    assert(toasts.some(t => t.includes('已关闭')), '缺少操作反馈');
  });

  await checkAsync('运行时：批量关闭保留电源灯，批量开启不保留', async () => {
    const dom = makeDom();
    const calls = [];
    const callMock = async (method, path, body) => {
      calls.push({ method, path, body });
      return { leds: LEDS, supported: true, changed: 2, skipped: 1, failed: [] };
    };
    const api = build(dom, callMock, () => {});
    await api.setAllLEDs(false);
    const off = calls.find(c => c.path === '/leds/all');
    assert(off, '未发起 /leds/all 请求');
    assert.deepStrictEqual(off.body, { on: false, keep_power: true }, '关闭时应保留电源灯');

    calls.length = 0;
    await api.setAllLEDs(true);
    const on = calls.find(c => c.path === '/leds/all');
    assert.deepStrictEqual(on.body, { on: true, keep_power: false }, '开启时不应跳过电源灯');
  });

  await checkAsync('运行时：不支持的平台显示明确提示', async () => {
    const dom = makeDom();
    const callMock = async () => ({ leds: [], supported: false, message: '当前系统未提供 /sys/class/leds' });
    const api = build(dom, callMock, () => {});
    await api.loadLEDs();
    assert.strictEqual(dom.nodes.ledBody.innerHTML, '', '不支持时表格应为空');
    assert(dom.nodes.ledHint.textContent.includes('/sys/class/leds'), '缺少不支持提示');
    assert.notStrictEqual(dom.nodes.ledHint.style.display, 'none', '提示应可见');
  });

  const passed = results.filter(Boolean).length;
  console.log('\n' + passed + '/' + results.length + ' passed');
  if (passed !== results.length) process.exit(1);
})();
