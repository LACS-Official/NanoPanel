/* chart.js 渲染冒烟测试：最小 DOM 桩，验证 SVG 输出结构 */
const fs = require('fs');
const path = require('path');
const vm = require('vm');
const assert = require('assert');

const root = path.join(__dirname, '..', '..');
const code = fs.readFileSync(path.join(root, 'web', 'chart.js'), 'utf8');
const sandbox = { window: {}, document: {}, console };
vm.createContext(sandbox);
vm.runInContext(code, sandbox, { filename: 'chart.js' });
const NPChart = sandbox.window.NPChart;

const results = [];
function check(name, fn) {
  try { fn(); results.push(true); console.log('  PASS  ' + name); }
  catch (e) { results.push(false); console.error('  FAIL  ' + name + ' -> ' + e.message); }
}

function container() {
  return {
    innerHTML: '',
    clientWidth: 800,
    getBoundingClientRect: () => ({ left: 0, top: 0, width: 800, height: 220 }),
    querySelectorAll: () => [],
    querySelector: () => null,
    addEventListener: () => {}
  };
}

function samplePoints(n, withTemp) {
  const base = Math.floor(Date.now() / 1000) - n * 60;
  return Array.from({ length: n }, (_, i) => ({
    ts: base + i * 60,
    cpu: 20 + (i % 10), mem: 50 + (i % 5), disk: 70,
    temp: withTemp ? 40 + (i % 8) : 0,
    net_rx: 100000 + i * 2048, net_tx: 50000 + i * 1024
  }));
}

check('导出 NPChart.render', () => {
  assert.strictEqual(typeof NPChart.render, 'function');
});

check('数据不足时显示收集提示', () => {
  const el = container();
  NPChart.render(el, { points: [], range: '1h', interval_sec: 60 });
  assert(/正在收集数据/.test(el.innerHTML));
  assert(!/<svg/.test(el.innerHTML));
});

check('正常数据生成两张 SVG 与四条曲线', () => {
  const el = container();
  NPChart.render(el, { points: samplePoints(12, true), range: '1h', interval_sec: 60, disk_ok: true });
  const html = el.innerHTML;
  assert((html.match(/<svg/g) || []).length === 2, '应有主图与流量图两张 SVG');
  assert(/CPU/.test(html) && /内存/.test(html) && /磁盘/.test(html) && /温度/.test(html), '图例应含四条曲线');
  assert(/网络流量/.test(html), '应有流量子图');
  assert((html.match(/stroke="#0a84ff"/g) || []).length >= 1, 'CPU 曲线应绘制');
  assert(!/NaN|undefined|Infinity/.test(html), '输出不应包含 NaN / undefined / Infinity');
});

check('无温度数据时隐藏温度图例', () => {
  const el = container();
  NPChart.render(el, { points: samplePoints(5, false), range: '6h' });
  assert(!/data-key="temp"/.test(el.innerHTML), '热区不可用时不应出现温度图例');
  assert(!/stroke="#ff453a"/.test(el.innerHTML), '不应绘制温度曲线');
});

check('7 天窗口使用月-日时间刻度', () => {
  const el = container();
  NPChart.render(el, { points: samplePoints(20, false), range: '7d' });
  assert(/<text class="npc-axis"[^>]*>\d{2}-\d{2}<\/text>/.test(el.innerHTML), '7d 应显示 MM-DD 刻度');
  assert(!/NaN/.test(el.innerHTML));
});

check('流量计数器回绕不产生异常数值', () => {
  const pts = samplePoints(6, false);
  pts[3].net_rx = 1;      // 模拟设备重启导致计数归零
  pts[3].net_tx = 1;
  const el = container();
  NPChart.render(el, { points: pts, range: '1h' });
  assert(!/NaN|Infinity|undefined/.test(el.innerHTML), '回绕后路径坐标仍应合法');
  assert(/stroke="#bf5af2"/.test(el.innerHTML), '流量曲线应绘制');
});

const failed = results.filter((x) => !x).length;
console.log('\n== 汇总 == %d/%d 通过', results.length - failed, results.length);
process.exit(failed ? 1 : 0);
