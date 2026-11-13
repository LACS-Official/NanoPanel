/* 进程管理静态回归：说明列、CPU 列、搜索与图表资源引用 */
const fs = require('fs');
const path = require('path');
const assert = require('assert');

const root = path.join(__dirname, '..', '..');
const read = (p) => fs.readFileSync(path.join(root, p), 'utf8');
const results = [];
function check(name, fn) {
  try { fn(); results.push(true); console.log('  PASS  ' + name); }
  catch (e) { results.push(false); console.error('  FAIL  ' + name + ' -> ' + e.message); }
}

const html = read('web/index.html');
const js = read('web/app.js');
const css = read('web/style.css');
const goProc = read('internal/api/processes.go');
const goDesc = read('internal/api/process_desc.go');
const ci = read('.github/workflows/ci.yml');

check('表头含 CPU 与「说明」列且顺序正确', () => {
  assert(/<th>PID<\/th><th>用户<\/th><th>CPU<\/th><th>内存占用<\/th><th>命令<\/th><th>说明<\/th><th>操作<\/th>/.test(html),
    '进程表头应为 PID → 用户 → CPU → 内存 → 命令 → 说明 → 操作');
});

check('后端返回 desc / args / cpu_pct 字段', () => {
  assert(/Desc\s+string\s+`json:"desc"`/.test(goProc), 'procInfo 缺少 desc 字段');
  assert(/Args\s+string\s+`json:"args,omitempty"`/.test(goProc), 'procInfo 缺少 args 字段');
  assert(/CPUPct\s+float64\s+`json:"cpu_pct"`/.test(goProc), 'procInfo 缺少 cpu_pct 字段');
  assert(/Desc:\s+describeProcess\(/.test(goProc), '列表未调用 describeProcess');
  assert(/ps", "-eo", "pid,user,pcpu,rss,comm,args"/.test(goProc), 'ps 列顺序不正确');
});

check('后端内置用途字典与兜底说明', () => {
  assert(/var processDescriptions = map\[string\]string\{/.test(goDesc), '缺少进程说明字典');
  assert(/nanopanel/.test(goDesc) && /nginx/.test(goDesc) && /sshd/.test(goDesc),
    '字典未覆盖常见进程');
  assert(/unknownProcessDesc/.test(goDesc), '缺少未收录进程的兜底说明');
});

check('前端渲染说明列且错误行 colspan 同步为 7', () => {
  assert(/class="proc-desc"/.test(js), '未渲染说明列');
  assert(/p\.desc/.test(js) && /esc\(desc\)/.test(js), '未输出 desc 内容');
  assert(/procBody'\)\.innerHTML = '<tr><td colspan="7"/.test(js), '进程表异常行 colspan 应为 7');
  assert(!/colspan="6"/.test(js), '仍残留旧的 colspan="6"');
  assert(/esc\(p\.args\)/.test(js), '命令列未利用 args 显示完整命令行');
});

check('CPU 列与即时搜索已接线', () => {
  assert(/p\.cpu_pct/.test(js) && /toFixed\(1\)/.test(js), '未渲染 CPU 百分比');
  assert(/id="procSearch"/.test(html), '缺少进程搜索框');
  assert(/procSearchEl\.addEventListener\('input'/.test(js), '搜索框未绑定 input 事件');
  assert(/function renderProcesses\(/.test(js) && /procQuery/.test(js), '缺少过滤渲染逻辑');
  assert(/\[p\.pid, p\.user, p\.comm, p\.args, p\.desc\]/.test(js), '搜索范围应覆盖说明与命令行');
});

check('说明列样式与悬停提示存在', () => {
  assert(/\.proc-desc\s*\{/.test(css), '缺少 .proc-desc 样式');
  assert(/\.proc-cmd\s*\{/.test(css), '缺少 .proc-cmd 样式');
  assert(/title="'\+esc\(desc\)/.test(js), '说明列缺少 title 悬停提示');
});

check('趋势图资源与配置接口已接入', () => {
  assert(/<script src="chart\.js" defer><\/script>/.test(html), '未引入 chart.js');
  assert(/id="metricsChart"/.test(html) && /id="metricsRanges"/.test(html), '缺少趋势图容器');
  assert(/id="alertCpu"/.test(html) && /id="alertSustain"/.test(html), '缺少阈值告警设置项');
  assert(/\/metrics\?range=/.test(js) && /\/settings\/alerts/.test(js), '前端未调用新接口');
  assert(/setMetricsRange\('7d'\)/.test(html) && /setMetricsRange\('7d'\)/.test(js), '动作白名单不完整');
  assert(/node --check web\/chart\.js/.test(ci) && /chart\.test\.js/.test(ci), 'CI 未校验图表脚本');
  assert(/\.npc-svg\s*\{/.test(css), '缺少图表样式');
});

const failed = results.filter((x) => !x).length;
console.log('\n== 汇总 == %d/%d 通过', results.length - failed, results.length);
process.exit(failed ? 1 : 0);
