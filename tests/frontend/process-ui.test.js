/* 进程管理「用途说明」列静态校验：防止表头 / 渲染 / 样式被误删 */
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

check('表头含「说明」列且位于「命令」之后', () => {
  assert(/<th>命令<\/th>\s*<th>说明<\/th>\s*<th>操作<\/th>/.test(html),
    '进程表头顺序应为 命令 → 说明 → 操作');
});

check('后端返回 desc / args 字段', () => {
  assert(/Desc\s+string\s+`json:"desc"`/.test(goProc), 'procInfo 缺少 desc 字段');
  assert(/Args\s+string\s+`json:"args,omitempty"`/.test(goProc), 'procInfo 缺少 args 字段');
  assert(/Desc:\s+describeProcess\(/.test(goProc), '列表未调用 describeProcess');
  assert(/ps", "-eo", "pid,user,rss,comm,args"/.test(goProc), 'ps 未取完整命令行 args');
});

check('后端内置用途字典与兜底说明', () => {
  assert(/var processDescriptions = map\[string\]string\{/.test(goDesc), '缺少进程说明字典');
  assert(/nanopanel/.test(goDesc) && /nginx/.test(goDesc) && /sshd/.test(goDesc),
    '字典未覆盖常见进程');
  assert(/unknownProcessDesc/.test(goDesc), '缺少未收录进程的兜底说明');
});

check('前端渲染说明列且错误行 colspan 同步为 6', () => {
  assert(/class="proc-desc"/.test(js), '未渲染说明列');
  assert(/p\.desc/.test(js) && /esc\(desc\)/.test(js), '未输出 desc 内容');
  assert(/procBody'\)\.innerHTML = '<tr><td colspan="6"/.test(js), '进程表异常行 colspan 应为 6');
  assert(!/procBody'\)\.innerHTML = '<tr><td colspan="5"/.test(js), '进程表仍残留旧的 colspan="5"');
  assert(/esc\(p\.args\)/.test(js), '命令列未利用 args 显示完整命令行');
});

check('说明列样式与悬停提示存在', () => {
  assert(/\.proc-desc\s*\{/.test(css), '缺少 .proc-desc 样式');
  assert(/\.proc-cmd\s*\{/.test(css), '缺少 .proc-cmd 样式');
  assert(/title="'\+esc\(desc\)/.test(js), '说明列缺少 title 悬停提示');
});

const failed = results.filter((x) => !x).length;
console.log('\n== 汇总 == %d/%d 通过', results.length - failed, results.length);
process.exit(failed ? 1 : 0);
