/* term-core.js 解析器验证（Node 环境，最小 DOM 桩） */
const fs = require('fs');
const vm = require('vm');

class El {
  constructor(tag) {
    this.tag = tag; this.style = {}; this.dataset = {}; this.children = [];
    this.innerHTML = ''; this.textContent = ''; this.value = '';
    this.classList = { add() {}, remove() {}, toggle() {} };
    this.clientWidth = 800; this.clientHeight = 300;
    this.scrollTop = 0; this.scrollHeight = 0;
  }
  appendChild(c) { this.children.push(c); c.parentNode = this; return c; }
  addEventListener() {}
  setAttribute() {}
  getBoundingClientRect() { return { width: 80, height: 18, left: 0, top: 0 }; }
  focus() {}
  querySelector() { return null; }
  removeChild() {}
}
const document = {
  createElement: (t) => new El(t),
  addEventListener() {},
  body: new El('body')
};
const windowObj = {
  getSelection: () => null,
  clipboard: null,
  addEventListener() {},
  confirm: () => true
};
const sandbox = {
  window: windowObj, document,
  TextDecoder, TextEncoder, Uint8Array, Array, String, Math, Object, JSON,
  requestAnimationFrame: (cb) => cb(),
  setTimeout, clearTimeout,
  ResizeObserver: undefined,
  navigator: { clipboard: null },
  console
};
sandbox.globalThis = sandbox;
vm.createContext(sandbox);
vm.runInContext(fs.readFileSync('web/term-core.js', 'utf8'), sandbox);

const Terminal = windowObj.NPTermCore.Terminal;
const results = [];
function check(name, ok, detail) {
  results.push([name, !!ok]);
  console.log((ok ? '  [PASS] ' : '  [FAIL] ') + name + (detail ? ' | ' + detail : ''));
}
function text(line, from, to) {
  return line.slice(from, to).map(c => c.w === 0 ? '' : c.ch).join('');
}
function write(t, s) { t.write(new TextEncoder().encode(s)); }

const t = new Terminal(new El('div'), {});
t.resize(40, 6);

write(t, 'Hello');
check('普通字符写入', text(t.lines[0], 0, 5) === 'Hello', JSON.stringify(text(t.lines[0], 0, 8)));

write(t, ' 中文');
const cells = t.lines[0].slice(6, 10);
check('东亚宽字符占两列', cells[0].ch === '中' && cells[0].w === 2 &&
  cells[1].w === 0 && cells[2].ch === '文' && cells[2].w === 2,
  'cursorX=' + t.cursorX + ' w=' + t.lines[0].slice(5, 10).map(c => c.w).join(','));

write(t, '\r\n\x1b[31mRED\x1b[0m');
check('换行与 SGR 前景色', t.cursorY === 1 && t.lines[1][0].ch === 'R' && t.lines[1][0].fg === 1,
  'y=' + t.cursorY + ' fg=' + t.lines[1][0].fg);
check('SGR 复位', t.attr.fg === null && t.attr.flags === 0);

write(t, '\x1b[38;5;196mX');
check('256 色前景', typeof t.lines[1][3].fg === 'string' && t.lines[1][3].fg.indexOf('rgb(') === 0,
  String(t.lines[1][3].fg));
write(t, '\x1b[38;2;10;20;30mY');
check('RGB 真彩前景', t.lines[1][4].fg === 'rgb(10,20,30)', String(t.lines[1][4].fg));
write(t, '\x1b[0m');

write(t, '\x1b[3;10HZZ');
check('CSI 光标定位', t.cursorY === 2 && t.cursorX === 11 && t.lines[2][9].ch === 'Z',
  'y=' + t.cursorY + ' x=' + t.cursorX);

write(t, '\x1b[2J\x1b[1;1H');
check('清屏', t.lines.every(l => l.every(c => c.ch === ' ' && c.w === 1)));

write(t, 'AAAA\x1b[2D');
check('光标左移', t.cursorX === 2, 'x=' + t.cursorX);

write(t, '\x1b[?1049h');
check('切换备用屏幕', t.modes.altScreen === true && t.lines.every(l => l.every(c => c.ch === ' ')));
write(t, 'ALT');
write(t, '\x1b[?1049l');
check('恢复主屏幕', t.modes.altScreen === false && text(t.lines[0], 0, 4) === 'AAAA');

write(t, '\x1b[2;5r');
check('设置滚动区域', t.scrollTop === 1 && t.scrollBottom === 4,
  'top=' + t.scrollTop + ' bottom=' + t.scrollBottom);

write(t, '\x1b[4;8H\x1b[s\x1b[6;1H\x1b[u');
check('保存/恢复光标', t.cursorX === 7 && t.cursorY === 3,
  'x=' + t.cursorX + ' y=' + t.cursorY);

write(t, '\x1b[?2004h');
check('括号粘贴模式', t.modes.bracketedPaste === true);

write(t, '\x1b[6n');
check('DSR 光标位置报告', true); // onData 回调在下方单独验证

// wrap-pending：写满最后一列后，下一个字符才换行
const tw = new Terminal(new El('div'), {});
tw.resize(5, 4);
write(tw, 'ABCDE');
check('写满末列进入 wrap-pending', tw.wrapPending === true && tw.cursorX === 4 && tw.cursorY === 0);
write(tw, 'F');
check('wrap-pending 触发换行', tw.cursorY === 1 && tw.lines[1][0].ch === 'F' && tw.cursorX === 1,
  'y=' + tw.cursorY + ' x=' + tw.cursorX);

// 输入序列编码验证
const seqs = [];
const t2 = new Terminal(new El('div'), { onData: (d) => seqs.push(d) });
t2._keydown({ key: 'ArrowUp', ctrlKey: false, altKey: false, shiftKey: false, metaKey: false, preventDefault() {} });
t2._keydown({ key: 'c', ctrlKey: true, altKey: false, shiftKey: false, metaKey: false, preventDefault() {} });
t2._keydown({ key: 'Enter', ctrlKey: false, altKey: false, shiftKey: false, metaKey: false, preventDefault() {} });
t2.modes.appCursor = true;
t2._keydown({ key: 'ArrowDown', ctrlKey: false, altKey: false, shiftKey: false, metaKey: false, preventDefault() {} });
check('键盘序列编码', seqs[0] === '\x1b[A' && seqs[1] === '\x03' && seqs[2] === '\r' && seqs[3] === '\x1bOB',
  JSON.stringify(seqs));

// UTF-8 跨帧切片
const t3 = new Terminal(new El('div'), {});
t3.resize(20, 3);
const raw = new TextEncoder().encode('中');
t3.write(raw.slice(0, 1));
t3.write(raw.slice(1));
check('UTF-8 跨帧重组', t3.lines[0][0].ch === '中', 'ch=' + t3.lines[0][0].ch);

const failed = results.filter(r => !r[1]).length;
console.log('\n== 汇总 == %d/%d 通过', results.length - failed, results.length);
process.exit(failed ? 1 : 0);
