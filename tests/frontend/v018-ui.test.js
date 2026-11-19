/* v0.1.8 功能静态回归：CSV 导出、免打扰、weekly/once、TOTP、远程备份、只读 token */
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
const main = read('main.go');
const cron = read('internal/api/cron.go');

// 所有 data-act / data-keydown 动作都必须在白名单中注册
check('全部前端动作已注册白名单', () => {
  const acts = new Set();
  for (const m of html.matchAll(/data-(act|keydown)="([^"]*)"/g)) acts.add(m[2]);
  const block = js.slice(js.indexOf('const INLINE_ACTIONS'), js.indexOf('function runInlineAction'));
  const missing = [...acts].filter(a => !block.includes('"' + a + '"'));
  assert.strictEqual(missing.length, 0, '未注册动作: ' + missing.join(', '));
});

check('#1 CSV 导出按钮、函数与后端路由', () => {
  assert(html.includes('exportMetricsCSV()'), '缺少导出按钮');
  assert(/function exportMetricsCSV\(/.test(js), '缺少导出函数');
  assert(/metrics\/export/.test(js) && /metrics\/export/.test(main), '缺少导出 API 路由');
});

check('#2 免打扰时段 UI 与字段', () => {
  for (const id of ['quietEnabled', 'quietStart', 'quietEnd', 'quietExempt']) {
    assert(html.includes('id="' + id + '"'), '缺少 ' + id);
  }
  assert(/quiet_enabled/.test(js), '前端未提交 quiet_enabled');
  assert(/InQuietHours/.test(read('internal/config/config.go')), '后端缺少时段判断');
});

check('#3 weekly / once 调度表达式', () => {
  assert(/weekly:W:HH:MM/.test(cron) && /once:YYYY-MM-DDTHH:MM/.test(cron), '缺少表达式说明');
  assert(/case strings.HasPrefix\(sc, "weekly:"\)/.test(cron), 'validSchedule 缺少 weekly');
  assert(/case strings.HasPrefix\(sc, "once:"\)/.test(cron), 'cronDue 缺少 once');
  assert(html.includes('cronCustom'), '缺少 once 自定义输入框');
  assert(/__custom__/.test(js), '缺少自定义调度选项');
});

check('#4 TOTP 两步验证登录与设置', () => {
  assert(html.includes('id="mfaStep"') && html.includes('id="mfaCode"'), '缺少登录第二步 UI');
  assert(html.includes('id="mfaSecret"') && html.includes('id="mfaRecoveryCodes"'), '缺少设置 UI');
  assert(/login\/verify/.test(js) && /login\/verify/.test(main), '缺少验证路由');
  assert(/settings\/mfa\/enable/.test(main), '缺少 MFA 设置路由');
  assert(fs.existsSync(path.join(root, 'internal/totp/totp.go')), '缺少 totp 包');
  assert(/function doMfaVerify\(/.test(js), '缺少验证函数');
});

check('#5 远程备份 UI 与后端', () => {
  for (const id of ['rbType', 'rbUrl', 'rbBucket', 'rbUser', 'rbPass', 'rbRetain', 'rbSchedule']) {
    assert(html.includes('id="' + id + '"'), '缺少 ' + id);
  }
  assert(/remote-backup/.test(js) && /remote-backup/.test(main), '缺少远程备份路由');
  assert(/func s3Put\(/.test(read('internal/api/remote_backup.go')), '缺少 S3 上传');
  assert(/func webdavPut\(/.test(read('internal/api/remote_backup.go')), '缺少 WebDAV 上传');
  assert(/StartRemoteBackup/.test(main), '未启动定时备份');
});

check('#6 只读 API Token', () => {
  assert(html.includes('createReadonlyToken()'), '缺少生成按钮');
  assert(/tokens\/readonly/.test(js) && /tokens\/readonly/.test(main), '缺少 token 路由');
  const auth = read('internal/auth/auth.go');
  assert(/IssueReadonly/.test(auth) && /ScopeRO/.test(auth), 'auth 缺少只读 scope');
  assert(/Scope\(tok\) == auth.ScopeRO/.test(read('internal/api/server.go')), '中间件未拦截只读写操作');
});

check('新后端路由均已注册', () => {
  for (const p of ['metrics/export', 'login/verify', 'settings/remote-backup', 'tokens/readonly', 'settings/mfa']) {
    assert(main.includes(p), '路由未注册: ' + p);
  }
});

console.log(`\n${results.filter(Boolean).length}/${results.length} checks passed`);
process.exit(results.every(Boolean) ? 0 : 1);
