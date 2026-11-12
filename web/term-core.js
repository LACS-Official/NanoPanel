/* NanoPanel 内置终端 —— 零依赖 ANSI/VT100 终端模拟器
 * 支持：UTF-8 与东亚宽字符、SGR（16/256/RGB 色）、光标定位与编辑、
 * 滚动区域、备用屏幕、鼠标报告、括号粘贴、虚拟滚动历史、选区复制。
 */
'use strict';
(function (global) {
  function esc(s) {
    return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;')
      .replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  // ANSI 16 基调色板（深色终端观感，兼容常见 CLI 工具）
  const PALETTE = [
    '#1c2128', '#ff5c57', '#5ac87a', '#f3d17a', '#5aa9f8', '#c397d8', '#56c7d6', '#d7dae0',
    '#5c6370', '#ff807b', '#8ce0a5', '#ffe3a0', '#8cc4ff', '#dcb6f0', '#84e2ee', '#f2f4f8'
  ];
  function ansi256(n) {
    if (n < 16) return PALETTE[n];
    if (n < 232) {
      const i = n - 16, r = Math.floor(i / 36), g = Math.floor((i % 36) / 6), b = i % 6;
      const cv = v => (v === 0 ? 0 : 55 + v * 40);
      return 'rgb(' + cv(r) + ',' + cv(g) + ',' + cv(b) + ')';
    }
    const v = 8 + (n - 232) * 10;
    return 'rgb(' + v + ',' + v + ',' + v + ')';
  }

  function isWideChar(ch) {
    const cp = ch.codePointAt(0);
    if (cp < 0x1100) return false;
    return (cp <= 0x115f) || cp === 0x2329 || cp === 0x232a ||
      (cp >= 0x2e80 && cp <= 0xa4cf && cp !== 0x303f) ||
      (cp >= 0xac00 && cp <= 0xd7a3) ||
      (cp >= 0xf900 && cp <= 0xfaff) ||
      (cp >= 0xfe10 && cp <= 0xfe19) ||
      (cp >= 0xfe30 && cp <= 0xfe6f) ||
      (cp >= 0xff00 && cp <= 0xff60) ||
      (cp >= 0xffe0 && cp <= 0xffe6) ||
      (cp >= 0x1f300 && cp <= 0x1faff) ||
      (cp >= 0x20000 && cp <= 0x3fffd);
  }

  const F_BOLD = 1, F_DIM = 2, F_ITALIC = 4, F_UNDER = 8, F_BLINK = 16,
    F_INVERSE = 32, F_HIDDEN = 64, F_STRIKE = 128;

  function blankCell(attr) {
    return { ch: ' ', w: 1, fg: attr ? attr.fg : null, bg: attr ? attr.bg : null, flags: attr ? attr.flags : 0 };
  }
  function makeLine(cols, attr) {
    const line = new Array(cols);
    for (let i = 0; i < cols; i++) line[i] = blankCell(attr);
    return line;
  }
  function cloneLine(line) {
    return line.map(c => ({ ch: c.ch, w: c.w, fg: c.fg, bg: c.bg, flags: c.flags }));
  }

  class Terminal {
    constructor(container, opts) {
      this.opts = opts || {};
      this.onData = this.opts.onData || function () {};
      this.onResize = this.opts.onResize || function () {};
      this.onTitle = this.opts.onTitle || function () {};
      this.onBell = this.opts.onBell || function () {};
      this.cols = 80; this.rows = 24;
      this.fontSize = this.opts.fontSize || 13;
      this.charW = 8; this.lineH = 18;
      this.maxHistory = 2000;
      this.scrollback = [];
      this.lines = [];
      this.cursorX = 0; this.cursorY = 0;
      this.attr = { fg: null, bg: null, flags: 0 };
      this.savedCursor = null;
      this.altBuffer = null;
      this.scrollTop = 0; this.scrollBottom = 0;
      this.modes = {
        appCursor: false, appKeypad: false, wrap: true, cursorVisible: true,
        bracketedPaste: false, mouseButton: false, mouseMotion: false, mouseSGR: false,
        altScreen: false, origin: false, insert: false
      };
      this.title = '';
      this.state = 'ground';
      this.csiBuf = '';
      this.oscBuf = '';
      this.charsetSkip = 0;
      this.utf8 = [];
      this.utf8Need = 0;
      this.decoder = new TextDecoder('utf-8');
      this.pendingRender = false;
      this.autoScroll = true;
      this.composing = false;
      this.destroyed = false;
      this.wrapPending = false;
      this._buildDOM(container);
      this.resize(this.cols, this.rows);
      this._bindInput();
      this._observe();
    }

    _buildDOM(root) {
      this.root = root;
      root.classList.add('term-surface');
      root.style.fontSize = this.fontSize + 'px';
      this.viewport = document.createElement('div');
      this.viewport.className = 'term-viewport';
      this.spacer = document.createElement('div');
      this.spacer.className = 'term-spacer';
      this.rowsEl = document.createElement('div');
      this.rowsEl.className = 'term-rows';
      this.caret = document.createElement('div');
      this.caret.className = 'term-caret';
      this.measure = document.createElement('span');
      this.measure.className = 'term-measure';
      this.measure.textContent = 'MMMMMMMMMM';
      this.input = document.createElement('textarea');
      this.input.className = 'term-input';
      this.input.setAttribute('autocomplete', 'off');
      this.input.setAttribute('autocapitalize', 'off');
      this.input.setAttribute('autocorrect', 'off');
      this.input.setAttribute('spellcheck', 'false');
      this.input.setAttribute('aria-label', '终端输入');
      this.viewport.appendChild(this.spacer);
      this.viewport.appendChild(this.rowsEl);
      this.viewport.appendChild(this.measure);
      root.appendChild(this.viewport);
      root.appendChild(this.caret);
      root.appendChild(this.input);
      this.viewport.addEventListener('scroll', () => {
        const dist = this.viewport.scrollHeight - this.viewport.scrollTop - this.viewport.clientHeight;
        this.autoScroll = dist < this.lineH * 1.5;
        this.scheduleRender();
      });
    }

    _measure() {
      const r = this.measure.getBoundingClientRect();
      if (r.width > 0) this.charW = r.width / 10;
      if (r.height > 0) this.lineH = r.height;
      // 兜底：等宽字体一般高约为字号的 1.35 倍，宽约 0.6 倍
      if (!this.charW || !isFinite(this.charW)) this.charW = this.fontSize * 0.6;
      if (!this.lineH || !isFinite(this.lineH)) this.lineH = Math.round(this.fontSize * 1.38);
    }

    setFontSize(px) {
      this.fontSize = Math.max(10, Math.min(24, px));
      this.root.style.fontSize = this.fontSize + 'px';
      // 字体变化后需等布局刷新再测量
      requestAnimationFrame(() => { this._measure(); this.fit(); });
    }

    _observe() {
      if (typeof ResizeObserver === 'undefined') return;
      this.ro = new ResizeObserver(() => this.fit());
      this.ro.observe(this.viewport);
    }

    // 按容器实际尺寸计算行列数并通知后端
    fit() {
      if (this.destroyed) return;
      this._measure();
      const w = this.viewport.clientWidth - 12, h = this.viewport.clientHeight;
      if (w <= 0 || h <= 0) return;
      const cols = Math.max(20, Math.min(500, Math.floor(w / this.charW)));
      const rows = Math.max(5, Math.min(200, Math.floor(h / this.lineH)));
      if (cols !== this.cols || rows !== this.rows) {
        this.resize(cols, rows);
        this.onResize(cols, rows);
      }
    }

    resize(cols, rows) {
      this.cols = cols; this.rows = rows;
      if (this.lines.length === 0) {
        for (let i = 0; i < rows; i++) this.lines.push(makeLine(cols, this.attr));
      } else {
        while (this.lines.length < rows) this.lines.push(makeLine(cols, this.attr));
        if (this.lines.length > rows) {
          const overflow = this.lines.splice(0, this.lines.length - rows);
          overflow.forEach(l => this._pushHistory(l));
        }
        for (let i = 0; i < this.lines.length; i++) {
          const l = this.lines[i];
          while (l.length < cols) l.push(blankCell(this.attr));
          if (l.length > cols) l.length = cols;
        }
      }
      this.scrollTop = 0; this.scrollBottom = rows - 1;
      this.cursorX = Math.min(this.cursorX, cols - 1);
      this.cursorY = Math.min(this.cursorY, rows - 1);
      this.wrapPending = false;
      this.scheduleRender();
    }

    _pushHistory(line) {
      if (this.modes.altScreen) return;
      this.scrollback.push(cloneLine(line));
      if (this.scrollback.length > this.maxHistory) this.scrollback.shift();
    }

    totalLines() { return this.scrollback.length + this.rows; }
    getLine(i) {
      return i < this.scrollback.length ? this.scrollback[i] : this.lines[i - this.scrollback.length];
    }

    clear() {
      this.scrollback = [];
      this.lines = [];
      this.cursorX = this.cursorY = 0;
      this.wrapPending = false;
      for (let i = 0; i < this.rows; i++) this.lines.push(makeLine(this.cols, this.attr));
      this.viewport.scrollTop = 0;
      this.autoScroll = true;
      this.scheduleRender();
    }

    // ---------- 渲染 ----------
    scheduleRender() {
      if (this.pendingRender || this.destroyed) return;
      this.pendingRender = true;
      requestAnimationFrame(() => { this.pendingRender = false; this.render(); });
    }

    _color(v, isBg) {
      if (v == null) return isBg ? '' : 'var(--term-fg)';
      if (typeof v === 'string') return v;
      if (typeof v === 'number') {
        let n = v;
        if (n >= 90 && n <= 97) n = n - 90 + 8;
        else if (n >= 100 && n <= 107) n = n - 100 + 8;
        return ansi256(n);
      }
      return isBg ? '' : 'var(--term-fg)';
    }

    _cellStyle(c) {
      let fg = c.fg, bg = c.bg;
      const f = c.flags;
      if (f & F_INVERSE) { const t = fg; fg = bg; bg = t; }
      let s = '';
      if (fg != null) s += 'color:' + this._color(fg, false) + ';';
      if (bg != null) s += 'background:' + this._color(bg, true) + ';';
      if (f & F_BOLD) s += 'font-weight:600;';
      if (f & F_DIM) s += 'opacity:.65;';
      if (f & F_ITALIC) s += 'font-style:italic;';
      if (f & F_UNDER) s += 'text-decoration:underline;';
      if ((f & F_STRIKE) && (f & F_UNDER)) s += 'text-decoration:underline line-through;';
      else if (f & F_STRIKE) s += 'text-decoration:line-through;';
      if (f & F_BLINK) s += 'animation:termBlink 1s steps(2) infinite;';
      if (f & F_HIDDEN) s += 'color:transparent;';
      return s;
    }

    _renderLine(line) {
      let html = '', style = null, buf = '';
      for (let i = 0; i < line.length; i++) {
        const c = line[i];
        if (!c || c.w === 0) continue;
        const st = this._cellStyle(c);
        if (st !== style) {
          if (buf) html += '<span style="' + style + '">' + esc(buf) + '</span>';
          buf = ''; style = st;
        }
        buf += c.ch || ' ';
      }
      if (buf) html += '<span style="' + style + '">' + esc(buf) + '</span>';
      return html || '&nbsp;';
    }

    render() {
      if (this.destroyed) return;
      const total = this.totalLines();
      const height = total * this.lineH;
      this.spacer.style.height = height + 'px';
      if (this.autoScroll) {
        const target = Math.max(0, height - this.viewport.clientHeight);
        if (Math.abs(this.viewport.scrollTop - target) > 1) this.viewport.scrollTop = target;
      }
      let first = Math.floor(this.viewport.scrollTop / this.lineH);
      first = Math.max(0, Math.min(Math.max(0, total - this.rows), first));
      const count = Math.min(this.rows + 1, total - first);
      let html = '';
      for (let i = 0; i < count; i++) {
        const line = this.getLine(first + i);
        html += '<div class="term-row">' + (line ? this._renderLine(line) : '') + '</div>';
      }
      this.rowsEl.style.transform = 'translateY(' + (first * this.lineH) + 'px)';
      this.rowsEl.style.width = Math.max(this.cols * this.charW, 10) + 'px';
      this.rowsEl.innerHTML = html;
      const cy = this.cursorY - (first - this.scrollback.length);
      const visible = this.modes.cursorVisible && cy >= 0 && cy < this.rows + 1;
      this.caret.style.display = visible ? 'block' : 'none';
      if (visible) {
        this.caret.style.left = (this.cursorX * this.charW) + 'px';
        this.caret.style.top = (cy * this.lineH) + 'px';
        this.caret.style.width = this.charW + 'px';
        this.caret.style.height = this.lineH + 'px';
      }
    }

    // ---------- 屏幕操作 ----------
    _blankCell() { return blankCell(this.attr); }

    lineFeed() {
      this.wrapPending = false;
      if (this.cursorY === this.scrollBottom) this.scrollUp(1);
      else if (this.cursorY < this.rows - 1) this.cursorY++;
    }
    reverseIndex() {
      if (this.cursorY === this.scrollTop) this.scrollDown(1);
      else if (this.cursorY > 0) this.cursorY--;
    }
    scrollUp(n) {
      for (let i = 0; i < n; i++) {
        const top = this.scrollTop, bottom = this.scrollBottom;
        const removed = this.lines.splice(top, 1)[0];
        this._pushHistory(removed);
        this.lines.splice(bottom, 0, makeLine(this.cols, this.attr));
      }
    }
    scrollDown(n) {
      for (let i = 0; i < n; i++) {
        this.lines.splice(this.scrollBottom, 1);
        this.lines.splice(this.scrollTop, 0, makeLine(this.cols, this.attr));
      }
    }

    printChar(ch) {
      const w = isWideChar(ch) ? 2 : 1;
      // VT100 的 wrap-pending：写满最后一列后光标停在末列，
      // 直到下一个可打印字符到达才真正换行。
      if (this.wrapPending) {
        this.cursorX = 0;
        this.lineFeed();
        this.wrapPending = false;
      }
      if (this.cursorX + w > this.cols) {
        if (this.modes.wrap) {
          this.cursorX = 0;
          this.lineFeed();
        } else {
          this.cursorX = Math.max(0, this.cols - w);
        }
      }
      const line = this.lines[this.cursorY];
      if (!line) return;
      if (this.modes.insert) {
        for (let i = this.cols - 1; i > this.cursorX; i--) line[i] = line[i - 1];
      }
      // 覆盖位置若原本是宽字符的另一半，先清掉相邻占位
      if (line[this.cursorX] && line[this.cursorX].w === 0 && this.cursorX > 0) {
        line[this.cursorX - 1] = this._blankCell();
      }
      if (this.cursorX + w <= this.cols && line[this.cursorX + 1] && line[this.cursorX + 1].w === 0) {
        line[this.cursorX + 1] = this._blankCell();
      }
      line[this.cursorX] = { ch, w, fg: this.attr.fg, bg: this.attr.bg, flags: this.attr.flags };
      if (w === 2 && this.cursorX + 1 < this.cols) {
        line[this.cursorX + 1] = { ch: '', w: 0, fg: this.attr.fg, bg: this.attr.bg, flags: this.attr.flags };
      }
      this.cursorX += w;
      if (this.cursorX >= this.cols) {
        this.cursorX = this.cols - 1;
        this.wrapPending = this.modes.wrap;
      }
    }

    tab() {
      this.wrapPending = false;
      const next = (Math.floor(this.cursorX / 8) + 1) * 8;
      this.cursorX = Math.min(next, this.cols - 1);
    }

    eraseInDisplay(mode) {
      const blank = () => this._blankCell();
      if (mode === 2) {
        for (let y = 0; y < this.rows; y++) this.lines[y] = makeLine(this.cols, this.attr);
      } else if (mode === 0) {
        const l = this.lines[this.cursorY];
        for (let x = this.cursorX; x < this.cols; x++) l[x] = blank();
        for (let y = this.cursorY + 1; y < this.rows; y++) this.lines[y] = makeLine(this.cols, this.attr);
      } else if (mode === 1) {
        const l = this.lines[this.cursorY];
        for (let x = 0; x <= this.cursorX && x < this.cols; x++) l[x] = blank();
        for (let y = 0; y < this.cursorY; y++) this.lines[y] = makeLine(this.cols, this.attr);
      } else if (mode === 3) {
        this.scrollback = [];
        this.viewport.scrollTop = 0;
      }
    }
    eraseInLine(mode) {
      const l = this.lines[this.cursorY];
      if (!l) return;
      const from = mode === 0 ? this.cursorX : 0;
      const to = mode === 1 ? Math.min(this.cursorX, this.cols - 1) : this.cols - 1;
      for (let x = from; x <= to && x < this.cols; x++) l[x] = this._blankCell();
    }
    deleteChars(n) {
      const l = this.lines[this.cursorY];
      for (let i = 0; i < n; i++) {
        l.splice(this.cursorX, 1);
        l.push(this._blankCell());
      }
    }
    insertBlanks(n) {
      const l = this.lines[this.cursorY];
      for (let i = 0; i < n; i++) {
        l.pop();
        l.splice(this.cursorX, 0, this._blankCell());
      }
    }
    insertLines(n) {
      if (this.cursorY < this.scrollTop || this.cursorY > this.scrollBottom) return;
      for (let i = 0; i < n; i++) {
        this.lines.splice(this.scrollBottom, 1);
        this.lines.splice(this.cursorY, 0, makeLine(this.cols, this.attr));
      }
      this.cursorX = 0;
    }
    deleteLines(n) {
      if (this.cursorY < this.scrollTop || this.cursorY > this.scrollBottom) return;
      for (let i = 0; i < n; i++) {
        this.lines.splice(this.cursorY, 1);
        this.lines.splice(this.scrollBottom, 0, makeLine(this.cols, this.attr));
      }
      this.cursorX = 0;
    }

    setSGR(params) {
      const p = params.length ? params : [0];
      for (let i = 0; i < p.length; i++) {
        const v = p[i];
        if (v === 0) { this.attr = { fg: null, bg: null, flags: 0 }; continue; }
        if (v === 1) this.attr.flags |= F_BOLD;
        else if (v === 2) this.attr.flags |= F_DIM;
        else if (v === 3) this.attr.flags |= F_ITALIC;
        else if (v === 4) this.attr.flags |= F_UNDER;
        else if (v === 5) this.attr.flags |= F_BLINK;
        else if (v === 7) this.attr.flags |= F_INVERSE;
        else if (v === 8) this.attr.flags |= F_HIDDEN;
        else if (v === 9) this.attr.flags |= F_STRIKE;
        else if (v === 21 || v === 22) this.attr.flags &= ~(F_BOLD | F_DIM);
        else if (v === 23) this.attr.flags &= ~F_ITALIC;
        else if (v === 24) this.attr.flags &= ~F_UNDER;
        else if (v === 25) this.attr.flags &= ~F_BLINK;
        else if (v === 27) this.attr.flags &= ~F_INVERSE;
        else if (v === 28) this.attr.flags &= ~F_HIDDEN;
        else if (v === 29) this.attr.flags &= ~F_STRIKE;
        else if (v >= 30 && v <= 37) this.attr.fg = v - 30;
        else if (v === 39) this.attr.fg = null;
        else if (v >= 40 && v <= 47) this.attr.bg = v - 40;
        else if (v === 49) this.attr.bg = null;
        else if (v >= 90 && v <= 97) this.attr.fg = v - 90 + 8;
        else if (v >= 100 && v <= 107) this.attr.bg = v - 100 + 8;
        else if (v === 38 || v === 48) {
          const isFg = v === 38;
          const kind = p[i + 1];
          if (kind === 5 && p[i + 2] != null) {
            const c = ansi256(Math.max(0, Math.min(255, p[i + 2] | 0)));
            if (isFg) this.attr.fg = c; else this.attr.bg = c;
            i += 2;
          } else if (kind === 2 && p[i + 4] != null) {
            const c = 'rgb(' + (p[i + 2] | 0) + ',' + (p[i + 3] | 0) + ',' + (p[i + 4] | 0) + ')';
            if (isFg) this.attr.fg = c; else this.attr.bg = c;
            i += 4;
          }
        }
      }
    }

    _switchAltScreen(on) {
      if (on && !this.modes.altScreen) {
        this.altBuffer = { lines: this.lines, x: this.cursorX, y: this.cursorY, top: this.scrollTop, bottom: this.scrollBottom };
        this.lines = [];
        for (let i = 0; i < this.rows; i++) this.lines.push(makeLine(this.cols, this.attr));
        this.cursorX = this.cursorY = 0;
        this.modes.altScreen = true;
      } else if (!on && this.modes.altScreen) {
        this.lines = this.altBuffer.lines;
        this.cursorX = this.altBuffer.x;
        this.cursorY = this.altBuffer.y;
        this.scrollTop = this.altBuffer.top;
        this.scrollBottom = this.altBuffer.bottom;
        this.altBuffer = null;
        this.modes.altScreen = false;
      }
      this.wrapPending = false;
    }

    csi(raw, final) {
      let s = raw, priv = false;
      if (s.charAt(0) === '?') { priv = true; s = s.slice(1); }
      const nums = s ? s.split(';').map(x => x === '' ? 0 : (parseInt(x, 10) || 0)) : [];
      const arg = (i, d) => (nums[i] == null || nums[i] === 0 ? d : nums[i]);
      if (priv && (final === 'h' || final === 'l')) {
        const on = final === 'h';
        for (const n of nums) {
          if (n === 1) this.modes.appCursor = on;
          else if (n === 7) this.modes.wrap = on;
          else if (n === 12 || n === 25) this.modes.cursorVisible = on;
          else if (n === 66) this.modes.appKeypad = on;
          else if (n === 6) this.modes.origin = on;
          else if (n === 2004) this.modes.bracketedPaste = on;
          else if (n === 1000 || n === 1002 || n === 1003) this.modes.mouseButton = on;
          else if (n === 1005 || n === 1006 || n === 1015) this.modes.mouseSGR = on;
          else if (n === 1049 || n === 47 || n === 1047) this._switchAltScreen(on);
        }
        this.scheduleRender();
        return;
      }
      switch (final) {
        case 'm': this.setSGR(nums); break;
        case 'A': this.cursorY = Math.max(0, this.cursorY - arg(0, 1)); break;
        case 'B': this.cursorY = Math.min(this.rows - 1, this.cursorY + arg(0, 1)); break;
        case 'C': this.cursorX = Math.min(this.cols - 1, this.cursorX + arg(0, 1)); break;
        case 'D': this.cursorX = Math.max(0, this.cursorX - arg(0, 1)); break;
        case 'E': this.cursorY = Math.min(this.rows - 1, this.cursorY + arg(0, 1)); this.cursorX = 0; break;
        case 'F': this.cursorY = Math.max(0, this.cursorY - arg(0, 1)); this.cursorX = 0; break;
        case 'G': case '`': this.cursorX = Math.max(0, Math.min(this.cols - 1, arg(0, 1) - 1)); break;
        case 'd': this.cursorY = Math.max(0, Math.min(this.rows - 1, arg(0, 1) - 1)); break;
        case 'H': case 'f': {
          let y = arg(0, 1) - 1, x = arg(1, 1) - 1;
          if (this.modes.origin) y += this.scrollTop;
          this.cursorY = Math.max(0, Math.min(this.rows - 1, y));
          this.cursorX = Math.max(0, Math.min(this.cols - 1, x));
          break;
        }
        case 'J': this.eraseInDisplay(arg(0, 0)); break;
        case 'K': this.eraseInLine(arg(0, 0)); break;
        case 'L': this.insertLines(arg(0, 1)); break;
        case 'M': this.deleteLines(arg(0, 1)); break;
        case 'P': this.deleteChars(arg(0, 1)); break;
        case '@': this.insertBlanks(arg(0, 1)); break;
        case 'X': {
          const l = this.lines[this.cursorY], n = arg(0, 1);
          for (let i = this.cursorX; i < this.cursorX + n && i < this.cols; i++) l[i] = this._blankCell();
          break;
        }
        case 'S': this.scrollUp(arg(0, 1)); break;
        case 'T': this.scrollDown(arg(0, 1)); break;
        case 'r': {
          const top = arg(0, 1) - 1, bottom = (nums.length > 1 ? nums[1] : this.rows) - 1;
          if (top < bottom && bottom < this.rows) { this.scrollTop = top; this.scrollBottom = bottom; }
          this.cursorX = 0;
          this.cursorY = this.modes.origin ? this.scrollTop : 0;
          break;
        }
        case 's': this.savedCursor = { x: this.cursorX, y: this.cursorY, attr: Object.assign({}, this.attr) }; break;
        case 'u': if (this.savedCursor) { this.cursorX = this.savedCursor.x; this.cursorY = this.savedCursor.y; this.attr = this.savedCursor.attr; } break;
        case 'h': if (nums[0] === 4) this.modes.insert = true; break;
        case 'l': if (nums[0] === 4) this.modes.insert = false; break;
        case 'n':
          if (nums[0] === 6) this.onData('\x1b[' + (this.cursorY + 1) + ';' + (this.cursorX + 1) + 'R');
          else if (nums[0] === 5) this.onData('\x1b[0n');
          break;
        case 'c': this.onData('\x1b[?1;2c'); break;
        case 't': if (nums[0] === 18) this.onData('\x1b[8;' + this.rows + ';' + this.cols + 't'); break;
        default: break;
      }
      // 除 SGR / 模式设置外，CSI 指令都会改变光标或屏幕，解除 wrap-pending。
      if (final !== 'm') this.wrapPending = false;
      this.scheduleRender();
    }

    osc(buf) {
      const idx = buf.indexOf(';');
      if (idx < 0) return;
      const kind = buf.slice(0, idx), value = buf.slice(idx + 1);
      if (kind === '0' || kind === '2') { this.title = value; this.onTitle(value); }
    }

    // ---------- 字节流解析 ----------
    write(bytes) {
      const data = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
      for (let i = 0; i < data.length; i++) {
        const b = data[i];
        if (this.utf8Need > 0) {
          this.utf8.push(b);
          this.utf8Need--;
          if (this.utf8Need === 0) {
            let ch = '';
            try { ch = this.decoder.decode(new Uint8Array(this.utf8)); } catch (e) { ch = '\ufffd'; }
            this.utf8 = [];
            this._groundChar(ch);
          }
          continue;
        }
        if (b >= 0xc0 && b < 0xf8) {
          this.utf8 = [b];
          this.utf8Need = b < 0xe0 ? 1 : (b < 0xf0 ? 2 : 3);
          continue;
        }
        this._groundChar(String.fromCharCode(b));
      }
      this.scheduleRender();
    }

    _groundChar(ch) {
      const code = ch.charCodeAt(0);
      switch (this.state) {
        case 'esc':
          this._esc(ch, code); return;
        case 'csi':
          if (code >= 0x40 && code <= 0x7e) { this.state = 'ground'; this.csi(this.csiBuf, ch); }
          else if (this.csiBuf.length < 128) this.csiBuf += ch;
          else this.state = 'ground';
          return;
        case 'osc':
          if (code === 0x07) { this.state = 'ground'; this.osc(this.oscBuf); }
          else if (code === 0x1b) { this.state = 'osc-esc'; }
          else if (this.oscBuf.length < 1024) this.oscBuf += ch;
          return;
        case 'osc-esc':
          this.state = ch === '\\' ? 'ground' : 'osc';
          return;
        case 'charset':
          this.charsetSkip--;
          if (this.charsetSkip <= 0) this.state = 'ground';
          return;
        case 'dcs':
          if (code === 0x1b) this.state = 'ground';
          return;
      }
      if (code === 0x1b) { this.state = 'esc'; return; }
      if (code === 0x0d) { this.cursorX = 0; this.wrapPending = false; return; }
      if (code === 0x0a || code === 0x0b || code === 0x0c) { this.lineFeed(); return; }
      if (code === 0x08) { this.cursorX = Math.max(0, this.cursorX - 1); this.wrapPending = false; return; }
      if (code === 0x09) { this.tab(); return; }
      if (code === 0x07) { this.onBell(); return; }
      if (code < 0x20 || code === 0x7f) return;
      this.printChar(ch);
    }

    _esc(ch, code) {
      switch (ch) {
        case '[': this.state = 'csi'; this.csiBuf = ''; return;
        case ']': this.state = 'osc'; this.oscBuf = ''; return;
        case 'P': case 'X': case '^': case '_': this.state = 'dcs'; return;
        case '(': case ')': case '*': case '+': case '-': case '.': case '/':
          this.state = 'charset'; this.charsetSkip = 1; return;
        case '7': this.savedCursor = { x: this.cursorX, y: this.cursorY, attr: Object.assign({}, this.attr) }; break;
        case '8': if (this.savedCursor) { this.cursorX = this.savedCursor.x; this.cursorY = this.savedCursor.y; this.attr = this.savedCursor.attr; } break;
        case 'M': this.reverseIndex(); return;
        case 'D': this.lineFeed(); return;
        case 'E': this.cursorX = 0; this.lineFeed(); return;
        case 'c':
          this.lines = []; this.scrollback = []; this.cursorX = this.cursorY = 0;
          this.attr = { fg: null, bg: null, flags: 0 };
          this.modes.altScreen = false;
          for (let i = 0; i < this.rows; i++) this.lines.push(makeLine(this.cols, this.attr));
          this.scrollTop = 0; this.scrollBottom = this.rows - 1;
          return;
        case '=': this.modes.appKeypad = true; break;
        case '>': this.modes.appKeypad = false; break;
        default: break;
      }
      this.state = 'ground';
    }

    // ---------- 输入 ----------
    send(str) { this.onData(str); }

    _bindInput() {
      const el = this.input;
      el.addEventListener('compositionstart', () => { this.composing = true; });
      el.addEventListener('compositionend', () => {
        this.composing = false;
        if (el.value) { this.send(el.value); el.value = ''; }
      });
      el.addEventListener('input', () => {
        if (this.composing || !el.value) return;
        this.send(el.value);
        el.value = '';
      });
      el.addEventListener('paste', (ev) => {
        ev.preventDefault();
        const text = (ev.clipboardData || window.clipboardData).getData('text');
        if (!text) return;
        if (this.opts.onPaste) this.opts.onPaste(text, (t) => this.send(t));
        else this.send(text);
      });
      el.addEventListener('keydown', (ev) => this._keydown(ev));
      this.root.addEventListener('mousedown', (ev) => {
        if (ev.button === 0) this._mouse(ev, 'down');
      });
      this.root.addEventListener('mouseup', (ev) => {
        this._mouse(ev, 'up');
        // 有选区时保留选区供复制，否则把键盘焦点交回隐藏输入框
        const sel = window.getSelection();
        if (ev.button === 0 && !(sel && sel.toString())) setTimeout(() => this.focus(), 0);
      });
      this.root.addEventListener('wheel', (ev) => { this._wheel(ev); }, { passive: true });
      this.root.addEventListener('copy', (ev) => {
        const sel = window.getSelection();
        const text = sel ? sel.toString() : '';
        if (text && ev.clipboardData) {
          ev.clipboardData.setData('text/plain', text);
          ev.preventDefault();
        }
      });
    }

    _keydown(ev) {
      const key = ev.key;
      let seq = null;
      const ctrl = ev.ctrlKey, alt = ev.altKey, shift = ev.shiftKey, meta = ev.metaKey;
      if (ctrl && shift && key.toLowerCase() === 'c') { ev.preventDefault(); this.copySelection(); return; }
      if (ctrl && shift && key.toLowerCase() === 'v') { return; } // 交给 paste 事件
      if (meta && key.toLowerCase() === 'v') return;

      if (ctrl && !alt && key.length === 1) {
        const c = key.toUpperCase().charCodeAt(0);
        if (c >= 65 && c <= 90) { seq = String.fromCharCode(c - 64); }
        else if (key === '[') seq = '\x1b';
        else if (key === '\\') seq = '\x1c';
        else if (key === ']') seq = '\x1d';
        else if (key === '^') seq = '\x1e';
        else if (key === '_') seq = '\x1f';
        else if (key === '?') seq = '\x7f';
        else if (key === '@') seq = '\x00';
      } else if (alt && key.length === 1) {
        seq = '\x1b' + key;
      } else {
        const ac = this.modes.appCursor ? 'O' : '[';
        switch (key) {
          case 'Enter': seq = this.modes.appKeypad ? '\x1bOM' : '\r'; break;
          case 'Backspace': seq = ctrl ? '\x08' : '\x7f'; break;
          case 'Tab': seq = shift ? '\x1b[Z' : '\t'; break;
          case 'Escape': seq = '\x1b'; break;
          case 'ArrowUp': seq = ac + 'A'; break;
          case 'ArrowDown': seq = ac + 'B'; break;
          case 'ArrowRight': seq = ac + 'C'; break;
          case 'ArrowLeft': seq = ac + 'D'; break;
          case 'Home': seq = this.modes.appCursor ? '\x1bOH' : '\x1b[H'; break;
          case 'End': seq = this.modes.appCursor ? '\x1bOF' : '\x1b[F'; break;
          case 'Insert': seq = '\x1b[2~'; break;
          case 'Delete': seq = '\x1b[3~'; break;
          case 'PageUp': seq = '\x1b[5~'; break;
          case 'PageDown': seq = '\x1b[6~'; break;
          case 'F1': seq = '\x1bOP'; break;
          case 'F2': seq = '\x1bOQ'; break;
          case 'F3': seq = '\x1bOR'; break;
          case 'F4': seq = '\x1bOS'; break;
          case 'F5': seq = '\x1b[15~'; break;
          case 'F6': seq = '\x1b[17~'; break;
          case 'F7': seq = '\x1b[18~'; break;
          case 'F8': seq = '\x1b[19~'; break;
          case 'F9': seq = '\x1b[20~'; break;
          case 'F10': seq = '\x1b[21~'; break;
          case 'F11': seq = '\x1b[23~'; break;
          case 'F12': seq = '\x1b[24~'; break;
          default: break;
        }
        if (seq && seq.charAt(0) === ac) seq = '\x1b' + seq;
      }
      if (seq !== null) { ev.preventDefault(); this.send(seq); }
    }

    _mouse(ev, kind) {
      if (!this.modes.mouseButton || ev.shiftKey) return;
      const rect = this.viewport.getBoundingClientRect();
      const col = Math.max(1, Math.min(this.cols, Math.floor((ev.clientX - rect.left) / this.charW) + 1));
      const row = Math.max(1, Math.min(this.rows, Math.floor((ev.clientY - rect.top) / this.lineH) + 1));
      if (!this.modes.mouseSGR) return;
      const b = ev.button === 1 ? 1 : (ev.button === 2 ? 2 : 0);
      const suffix = kind === 'down' ? 'M' : 'm';
      this.send('\x1b[<' + b + ';' + col + ';' + row + suffix);
    }
    _wheel(ev) {
      if (!this.modes.mouseButton || ev.shiftKey) return;
      const rect = this.viewport.getBoundingClientRect();
      const col = Math.max(1, Math.min(this.cols, Math.floor((ev.clientX - rect.left) / this.charW) + 1));
      const row = Math.max(1, Math.min(this.rows, Math.floor((ev.clientY - rect.top) / this.lineH) + 1));
      const b = ev.deltaY < 0 ? 64 : 65;
      if (this.modes.mouseSGR) this.send('\x1b[<' + b + ';' + col + ';' + row + 'M');
    }

    copySelection() {
      const sel = window.getSelection();
      const text = sel ? sel.toString() : '';
      if (text && navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).catch(() => {});
      }
      return text;
    }
    focus() { if (!this.destroyed) this.input.focus({ preventScroll: true }); }
    destroy() {
      this.destroyed = true;
      if (this.ro) this.ro.disconnect();
      this.root.innerHTML = '';
    }
  }

  global.NPTermCore = { Terminal: Terminal, ansi256: ansi256 };
})(window);
