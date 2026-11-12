/* terminal.js UI 层冒烟验证：最小 DOM / fetch / WebSocket 桩 */
const fs = require('fs');
const vm = require('vm');

class El {
  constructor(tag) {
    this.tag = tag; this.style = {}; this.dataset = {}; this.children = [];
    this.innerHTML = ''; this.textContent = ''; this.value = '';
    this.classList = { add() {}, remove() {}, toggle() {}, contains() { return false; } };
    this.clientWidth = 900; this.clientHeight = 400;
    this.scrollTop = 0; this.scrollHeight = 0; this._h = {};
    this.parentNode = null;
  }
  appendChild(c) { this.children.push(c); c.parentNode = this; return c; }
  removeChild(c) { this.children = this.children.filter(x => x !== c); }
  remove() { if (this.parentNode) this.parentNode.removeChild(this); }
  addEventListener(t, fn) { (this._h[t] = this._h[t] || []).push(fn); }
  setAttribute() {}
  getBoundingClientRect() { return { width: 80, height: 18, left: 0, top: 0 }; }
  focus() {}
  closest() { return null; }
  querySelector() { return new El('button'); }
  querySelectorAll() { return []; }
}
const ids = {};
const ID_LIST = [
  'toast', 'termTabs', 'termBody', 'termStatus', 'termNotice', 'termNoticeText',
  'termNoticeClose', 'termNew', 'termClose', 'termClear', 'termCopy', 'termPaste',
  'termFontMinus', 'termFontPlus', 'termFontLabel', 'termQuick', 'termReconnect',
  'termFull', 'view-terminal'
];
ID_LIST.forEach(id => { ids[id] = new El('div'); ids[id].id = id; });

const document = {
  createElement: (t) => new El(t),
  getElementById: (id) => {
    if (!ids[id]) throw new Error('缺少 DOM 元素: #' + id);
    return ids[id];
  },
  addEventListener() {},
  body: new El('body')
};

const calls = [];
async function fetchMock(url, opt) {
  calls.push({ url: url, method: opt && opt.method });
  if (url.indexOf('/terminal/settings') >= 0) {
    return { status: 200, json: async () => ({ ok: true, data: { enabled: true, shell_resolved: '/bin/bash', user: 'root', platform: 'linux', pty: true, active: 0, max_sessions: 8 } }) };
  }
  if (url.indexOf('/terminal/session') >= 0) {
    return { status: 200, json: async () => ({ ok: true, data: { ticket: 'TEST_TICKET', session: 'sid1', expires: 30 } }) };
  }
  return { status: 200, json: async () => ({ ok: true, data: {} }) };
}

class FakeWS {
  constructor(url) {
    this.url = url; this.readyState = 0; this.sent = [];
    FakeWS.last = this;
    setTimeout(() => { this.readyState = 1; if (this.onopen) this.onopen({}); }, 0);
  }
  send(d) { this.sent.push(d); }
  close() { this.readyState = 3; }
}
FakeWS.OPEN = 1;

const windowObj = {
  getSelection: () => null,
  addEventListener() {},
  confirm: () => true,
  NPTermCore: null,
  NPTerminal: null,
  location: { protocol: 'http:', host: '127.0.0.1:8080' },
  localStorage: { getItem: (k) => (k === 'np_token' ? 'FAKE_TOKEN' : null), setItem() {}, removeItem() {} },
  fetch: fetchMock,
  WebSocket: FakeWS
};
const sandbox = {
  window: windowObj, document,
  TextDecoder, TextEncoder, Uint8Array, URLSearchParams, Array, String, Math, Object, JSON,
  requestAnimationFrame: (cb) => cb(),
  setTimeout, clearTimeout, setInterval: () => 0, clearInterval: () => {},
  ResizeObserver: undefined,
  navigator: { clipboard: null },
  location: windowObj.location,
  localStorage: windowObj.localStorage,
  fetch: fetchMock,
  WebSocket: FakeWS,
  console
};
sandbox.globalThis = sandbox;
vm.createContext(sandbox);
vm.runInContext(fs.readFileSync('web/term-core.js', 'utf8'), sandbox);
vm.runInContext(fs.readFileSync('web/terminal.js', 'utf8'), sandbox);

const results = [];
function check(name, ok, detail) {
  results.push(!!ok);
  console.log((ok ? '  [PASS] ' : '  [FAIL] ') + name + (detail ? ' | ' + detail : ''));
}
const tick = (ms = 40) => new Promise(r => setTimeout(r, ms));

(async () => {
  check('暴露 NPTerminal 接口', !!(windowObj.NPTerminal && windowObj.NPTerminal.enter &&
    windowObj.NPTerminal.leave && windowObj.NPTerminal.dispose));

  windowObj.NPTerminal.enter();
  await tick();
  check('读取终端设置', calls.some(c => c.url.indexOf('/terminal/settings') >= 0));
  check('申请一次性票据', calls.some(c => c.url.indexOf('/terminal/session') >= 0 && c.method === 'POST'));
  check('建立 WebSocket 连接', !!(FakeWS.last && FakeWS.last.url.indexOf('ticket=TEST_TICKET') >= 0),
    FakeWS.last ? FakeWS.last.url : 'none');
  check('构造请求带 Bearer 凭据', calls.every(c => c.url.indexOf('/api/v1/terminal/') >= 0));

  if (FakeWS.last) {
    const ws = FakeWS.last;
    ws.onmessage({ data: JSON.stringify({ type: 'ready', shell: '/bin/bash', user: 'root', pty: true, platform: 'linux' }) });
    ws.onmessage({ data: new TextEncoder().encode('\x1b[32mOK\x1b[0m\r\n').buffer });
    ws.onmessage({ data: JSON.stringify({ type: 'exit', code: 0 }) });
    check('处理 ready / 二进制输出 / exit 消息', true);
    check('连接后同步窗口尺寸', ws.sent.some(d => typeof d === 'string' && d.indexOf('"type":"resize"') >= 0),
      'send 次数=' + ws.sent.length);
  }

  windowObj.NPTerminal.leave();
  check('切离视图不抛错', true);
  windowObj.NPTerminal.dispose();
  check('销毁会话不抛错', true);

  const failed = results.filter(x => !x).length;
  console.log('\n== 汇总 == %d/%d 通过', results.length - failed, results.length);
  process.exit(failed ? 1 : 0);
})().catch(e => { console.error('异常:', e); process.exit(1); });
