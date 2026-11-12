/* NanoPanel 内置终端 —— 会话管理与界面控制
 * 与后端 /api/v1/terminal/* 配合：Bearer 换一次性票据 → WebSocket → PTY 字节流。
 * 独立于 app.js，仅通过 window.NPTerminal 暴露 enter / leave / dispose。
 */
'use strict';
(function (global) {
  const API = '/api/v1';
  const MAX_SESSIONS = 8;
  const QUICK_COMMANDS = [
    ['系统信息', 'uname -a'],
    ['资源占用', 'top'],
    ['磁盘用量', 'df -h'],
    ['内存用量', 'free -h'],
    ['网络地址', 'ip -br addr'],
    ['监听端口', 'ss -tulnp'],
    ['面板服务', 'systemctl status nanopanel --no-pager'],
    ['面板日志', 'journalctl -u nanopanel -n 50 --no-pager'],
    ['内存进程 TOP20', 'ps aux --sort=-%mem | head -20'],
    ['目录占用', 'du -h --max-depth=1 /opt 2>/dev/null | sort -h']
  ];

  function esc(s) {
    return String(s == null ? '' : s).replace(/&/g, '&amp;').replace(/</g, '&lt;')
      .replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;');
  }
  function lsGet(k, d) { try { return localStorage.getItem(k) || d; } catch (e) { return d; } }
  function lsSet(k, v) { try { localStorage.setItem(k, v); } catch (e) { /* 隐私模式忽略 */ } }
  function token() { return lsGet('np_token', ''); }

  function toast(msg) {
    const t = document.getElementById('toast');
    if (!t) return;
    t.textContent = msg;
    t.classList.add('show');
    clearTimeout(t._tm);
    t._tm = setTimeout(() => t.classList.remove('show'), 2600);
  }

  async function api(method, path, body) {
    const opt = { method: method, headers: { 'Content-Type': 'application/json' } };
    if (token()) opt.headers.Authorization = 'Bearer ' + token();
    if (body !== undefined) opt.body = JSON.stringify(body);
    const res = await fetch(API + path, opt);
    if (res.status === 401) {
      toast('登录已过期，请重新登录');
      return null;
    }
    const data = await res.json().catch(() => ({ ok: false, err: '响应格式错误' }));
    if (!data.ok) throw new Error(data.err || '请求失败 (HTTP ' + res.status + ')');
    return data.data;
  }

  function copyText(text) {
    if (!text) return false;
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).catch(() => fallbackCopy(text));
    } else fallbackCopy(text);
    return true;
  }
  function fallbackCopy(text) {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.cssText = 'position:fixed;left:-9999px;top:0';
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand('copy'); } catch (e) { /* 忽略 */ }
    document.body.removeChild(ta);
  }

  const state = {
    ready: false,
    bound: false,
    entered: false,
    sessions: new Map(),
    activeId: '',
    counter: 0,
    fontSize: parseInt(lsGet('np_term_font', '13'), 10) || 13,
    settings: null,
    fullscreen: false
  };

  function el(id) { return document.getElementById(id); }

  // ---------- 会话 ----------
  function newSessionId() {
    state.counter += 1;
    return 's' + Date.now().toString(36) + state.counter.toString(36);
  }

  function createSession(opts) {
    const opts2 = opts || {};
    if (state.sessions.size >= MAX_SESSIONS) {
      toast('终端会话已达上限（' + MAX_SESSIONS + '）');
      return null;
    }
    const id = newSessionId();
    const surface = document.createElement('div');
    surface.className = 'term-session';
    surface.dataset.sid = id;
    const overlay = document.createElement('div');
    overlay.className = 'term-overlay hidden';
    overlay.innerHTML = '<div class="term-overlay-box"><div class="term-overlay-title"></div>' +
      '<div class="term-overlay-desc"></div><button class="btn sm" type="button">重新连接</button></div>';
    surface.appendChild(overlay);
    el('termBody').appendChild(surface);

    const sess = {
      id: id, surface: surface, overlay: overlay, ws: null, term: null,
      status: 'connecting', created: Date.now(), shell: '', user: '',
      cols: 80, rows: 24, pty: false, platform: '', retries: 0
    };
    const term = new global.NPTermCore.Terminal(surface, {
      fontSize: state.fontSize,
      onData: (data) => sendInput(sess, data),
      onResize: (cols, rows) => sendResize(sess, cols, rows),
      onTitle: (title) => { if (state.activeId === id) updateStatus(sess, title); },
      onPaste: (text, send) => {
        // 多行粘贴给出保护：避免误把多行文本直接提交执行
        if (text.indexOf('\n') >= 0 && !opts2.allowMultiPaste &&
          !window.confirm('即将粘贴多行内容（' + text.split('\n').length + ' 行），确认继续？')) return;
        if (term.modes.bracketedPaste) send('\x1b[200~' + text + '\x1b[201~');
        else send(text);
      }
    });
    sess.term = term;
    state.sessions.set(id, sess);
    overlay.querySelector('button').addEventListener('click', () => reconnect(sess));
    renderTabs();
    activate(id);
    connect(sess);
    return sess;
  }

  function destroySession(id) {
    const s = state.sessions.get(id);
    if (!s) return;
    try { if (s.ws) s.ws.close(1000, 'closed by user'); } catch (e) { /* 忽略 */ }
    if (s.ws) s.ws.onclose = null;
    if (s.term) s.term.destroy();
    if (s.surface && s.surface.parentNode) s.surface.parentNode.removeChild(s.surface);
    state.sessions.delete(id);
    if (state.activeId === id) {
      state.activeId = '';
      const next = state.sessions.keys().next();
      if (!next.done) activate(next.value);
      else { renderTabs(); updateStatus(null); }
    } else renderTabs();
  }

  function activate(id) {
    const s = state.sessions.get(id);
    if (!s) return;
    state.activeId = id;
    state.sessions.forEach((v, k) => v.surface.classList.toggle('active', k === id));
    renderTabs();
    requestAnimationFrame(() => {
      s.term.fit();
      s.term.focus();
      updateStatus(s);
    });
  }

  function activeSession() { return state.sessions.get(state.activeId); }

  function renderTabs() {
    const box = el('termTabs');
    if (!box) return;
    let html = '';
    state.sessions.forEach((s, id) => {
      const dot = s.status === 'open' ? 'ok' : (s.status === 'connecting' ? 'wait' : 'off');
      html += '<button class="term-tab' + (id === state.activeId ? ' active' : '') + '" data-sid="' + esc(id) + '">' +
        '<span class="term-dot ' + dot + '"></span><span class="term-tab-name">' +
        (s.title || ('会话 ' + (Array.from(state.sessions.keys()).indexOf(id) + 1))) +
        '</span><span class="term-tab-x" data-close="' + esc(id) + '" title="关闭会话">×</span></button>';
    });
    box.innerHTML = html;
  }

  function showOverlay(s, title, desc, btn) {
    if (!s || !s.overlay) return;
    s.overlay.querySelector('.term-overlay-title').textContent = title;
    s.overlay.querySelector('.term-overlay-desc').textContent = desc;
    const b = s.overlay.querySelector('button');
    b.textContent = btn || '重新连接';
    b.style.display = btn ? 'inline-flex' : 'none';
    s.overlay.classList.remove('hidden');
  }
  function hideOverlay(s) { if (s && s.overlay) s.overlay.classList.add('hidden'); }

  function reconnect(s) {
    if (!s || !state.sessions.has(s.id)) return;
    if (s.ws) {
      s.ws.onclose = null;
      try { s.ws.close(); } catch (e) { /* 忽略 */ }
      s.ws = null;
    }
    s.retries = 0;
    s.status = 'connecting';
    s.term.write(new TextEncoder().encode('\r\n\x1b[90m──── 重新连接 ────\x1b[0m\r\n'));
    connect(s);
  }

  // ---------- 连接 ----------
  async function connect(s) {
    if (!state.sessions.has(s.id)) return;
    s.status = 'connecting';
    showOverlay(s, '正在连接终端…', '正在建立安全会话', '');
    renderTabs();
    let ticketData;
    try {
      ticketData = await api('POST', '/terminal/session', { cols: s.cols, rows: s.rows });
    } catch (e) {
      s.status = 'error';
      showOverlay(s, '无法创建终端会话', e.message, '重试');
      renderTabs();
      return;
    }
    if (!ticketData) { s.status = 'error'; showOverlay(s, '登录已过期', '请重新登录后再打开终端', ''); renderTabs(); return; }
    if (!state.sessions.has(s.id)) return;

    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const qs = new URLSearchParams({
      ticket: ticketData.ticket, sid: s.id,
      cols: String(s.term.cols), rows: String(s.term.rows)
    });
    let ws;
    try { ws = new WebSocket(proto + '//' + location.host + API + '/terminal/ws?' + qs.toString()); }
    catch (e) {
      s.status = 'error';
      showOverlay(s, 'WebSocket 连接失败', e.message, '重试');
      renderTabs();
      return;
    }
    s.ws = ws;
    ws.binaryType = 'arraybuffer';
    ws.onopen = () => {
      s.status = 'open';
      s.retries = 0;
      hideOverlay(s);
      renderTabs();
      updateStatus(s);
      // 连接建立后同步一次前端实测尺寸，消除握手期间的窗口变化差异
      ws.send(JSON.stringify({ type: 'resize', cols: s.term.cols, rows: s.term.rows }));
      if (state.activeId === s.id) s.term.focus();
    };
    ws.onmessage = (ev) => {
      if (typeof ev.data === 'string') {
        let msg;
        try { msg = JSON.parse(ev.data); } catch (e) { return; }
        if (msg.type === 'ready') {
          s.shell = msg.shell || '';
          s.user = msg.user || '';
          s.pty = !!msg.pty;
          s.platform = msg.platform || '';
          updateStatus(s);
        } else if (msg.type === 'exit') {
          s.status = 'closed';
          showOverlay(s, '会话已结束', 'Shell 退出码 ' + msg.code + '，可重新连接开启新会话', '重新连接');
          renderTabs();
        } else if (msg.type === 'error') {
          toast(msg.message || '终端错误');
        }
        return;
      }
      if (ev.data instanceof ArrayBuffer) s.term.write(new Uint8Array(ev.data));
      else if (typeof Blob !== 'undefined' && ev.data instanceof Blob) {
        ev.data.arrayBuffer().then(b => s.term.write(new Uint8Array(b))).catch(() => {});
      }
    };
    ws.onerror = () => { /* 具体原因由 onclose 统一呈现 */ };
    ws.onclose = (ev) => {
      if (s.ws !== ws) return;
      s.ws = null;
      if (s.status === 'closed') return;
      s.status = 'error';
      const why = ev.code === 1000 ? '连接已关闭' : '连接中断（代码 ' + ev.code + '）';
      // 面板重启等短暂断线自动重连最多 3 次
      if (ev.code !== 1000 && s.retries < 3 && state.entered && state.sessions.has(s.id)) {
        s.retries += 1;
        showOverlay(s, '正在自动重连…', '第 ' + s.retries + ' / 3 次，' + why, '');
        renderTabs();
        setTimeout(() => {
          if (!state.sessions.has(s.id)) return;
          s.term.write(new TextEncoder().encode('\r\n\x1b[90m──── 自动重连 ────\x1b[0m\r\n'));
          connect(s);
        }, 1200 * s.retries);
      } else {
        showOverlay(s, why, '可重新连接开启一个新的 Shell 会话', '重新连接');
        renderTabs();
      }
      updateStatus(s);
    };
  }

  function wsReady(s) { return s && s.ws && s.ws.readyState === WebSocket.OPEN; }

  function sendInput(s, data) {
    if (!wsReady(s)) return;
    // 输入统一用二进制帧，避免 UTF-8 与 JSON 转义带来的歧义
    const bytes = new TextEncoder().encode(data);
    s.ws.send(bytes);
  }
  function sendResize(s, cols, rows) {
    if (!s) return;
    s.cols = cols; s.rows = rows;
    if (state.activeId === s.id) updateStatus(s);
    if (!wsReady(s)) return;
    s.ws.send(JSON.stringify({ type: 'resize', cols: cols, rows: rows }));
  }

  // ---------- 状态栏 ----------
  function updateStatus(s, title) {
    const bar = el('termStatus');
    if (!bar) return;
    if (!s) {
      bar.innerHTML = '<span class="term-st-item">无会话</span><span class="term-st-right">点击「新建会话」开始</span>';
      return;
    }
    if (title) s.title = title.length > 28 ? title.slice(0, 28) + '…' : title;
    renderTabs();
    const label = { open: '已连接', connecting: '连接中', closed: '已结束', error: '已断开' }[s.status] || s.status;
    const secs = Math.floor((Date.now() - s.created) / 1000);
    const dur = secs < 60 ? secs + ' 秒' : Math.floor(secs / 60) + ' 分钟';
    bar.innerHTML =
      '<span class="term-st-item"><span class="term-dot ' +
      (s.status === 'open' ? 'ok' : s.status === 'connecting' ? 'wait' : 'off') + '"></span>' + label + '</span>' +
      '<span class="term-st-item">' + esc(s.user || '—') + '@' + esc(s.shell || 'shell') + '</span>' +
      '<span class="term-st-item">' + s.cols + ' × ' + s.rows + '</span>' +
      (s.pty ? '' : '<span class="term-st-item term-st-warn">无 PTY（交互程序受限）</span>') +
      '<span class="term-st-right">' + esc(s.title || '') + ' · ' + dur + '</span>';
  }
  setInterval(() => {
    if (state.entered && state.activeId) updateStatus(activeSession());
  }, 15000);

  // ---------- 工具栏 ----------
  function bindToolbar() {
    el('termTabs').addEventListener('click', (ev) => {
      const close = ev.target.closest('[data-close]');
      if (close) { destroySession(close.dataset.close); return; }
      const tab = ev.target.closest('[data-sid]');
      if (tab) activate(tab.dataset.sid);
    });
    el('termNew').addEventListener('click', () => createSession());
    el('termClear').addEventListener('click', () => {
      const s = activeSession();
      if (s) { s.term.clear(); s.term.focus(); }
    });
    el('termCopy').addEventListener('click', () => {
      const s = activeSession();
      if (!s) return;
      const t = s.term.copySelection();
      toast(t ? '已复制选中的文本' : '请先用鼠标拖选终端文本');
      s.term.focus();
    });
    el('termPaste').addEventListener('click', async () => {
      const s = activeSession();
      if (!s) return;
      try {
        const text = await navigator.clipboard.readText();
        if (text) sendInput(s, s.term.modes.bracketedPaste ? '\x1b[200~' + text + '\x1b[201~' : text);
        else toast('剪贴板为空');
      } catch (e) {
        toast('浏览器未授权读取剪贴板，请按 Ctrl+V 粘贴');
      }
      s.term.focus();
    });
    el('termFontMinus').addEventListener('click', () => changeFont(-1));
    el('termFontPlus').addEventListener('click', () => changeFont(1));
    el('termFull').addEventListener('click', toggleFullscreen);
    el('termReconnect').addEventListener('click', () => {
      const s = activeSession();
      if (!s) { createSession(); return; }
      reconnect(s);
    });
    el('termClose').addEventListener('click', () => { if (state.activeId) destroySession(state.activeId); });

    const quick = el('termQuick');
    quick.innerHTML = '<option value="">常用命令…</option>' +
      QUICK_COMMANDS.map((c, i) => '<option value="' + i + '">' + esc(c[0]) + '</option>').join('');
    quick.addEventListener('change', () => {
      const idx = quick.value;
      quick.value = '';
      if (idx === '') return;
      const s = activeSession();
      if (!s || !wsReady(s)) { toast('终端尚未连接'); return; }
      sendInput(s, QUICK_COMMANDS[parseInt(idx, 10)][1]);
      s.term.focus();
    });

    el('termNoticeClose').addEventListener('click', () => {
      el('termNotice').classList.add('hidden');
      lsSet('np_term_notice', '1');
    });
    document.addEventListener('keydown', (ev) => {
      if (ev.key === 'Escape' && state.fullscreen) toggleFullscreen();
    });
  }

  function changeFont(delta) {
    state.fontSize = Math.max(10, Math.min(24, state.fontSize + delta));
    lsSet('np_term_font', String(state.fontSize));
    state.sessions.forEach(s => s.term.setFontSize(state.fontSize));
    el('termFontLabel').textContent = state.fontSize + 'px';
  }

  function toggleFullscreen() {
    const view = el('view-terminal');
    state.fullscreen = !state.fullscreen;
    view.classList.toggle('term-fullscreen', state.fullscreen);
    el('termFull').textContent = state.fullscreen ? '退出全屏' : '全屏';
    requestAnimationFrame(() => {
      const s = activeSession();
      if (s) { s.term.fit(); s.term.focus(); }
    });
  }

  // ---------- 生命周期 ----------
  async function loadSettings() {
    try { state.settings = await api('GET', '/terminal/settings'); }
    catch (e) { state.settings = null; }
    const notice = el('termNotice');
    if (notice) {
      const text = el('termNoticeText');
      if (state.settings && state.settings.enabled === false) {
        text.innerHTML = '<b>内置终端当前已被禁用。</b>可在「设置 → 安全 → 内置终端」重新启用。';
        notice.classList.remove('hidden');
      } else if (!lsGet('np_term_notice', '')) {
        notice.classList.remove('hidden');
      }
    }
  }

  function enter() {
    if (!state.bound) {
      state.bound = true;
      state.ready = true;
      bindToolbar();
      el('termFontLabel').textContent = state.fontSize + 'px';
      loadSettings();
    }
    state.entered = true;
    if (state.sessions.size === 0) createSession();
    else {
      const s = activeSession();
      if (s) requestAnimationFrame(() => { s.term.fit(); s.term.focus(); });
    }
  }
  function leave() {
    // 切换视图不断开会话，保留正在运行的命令与 shell 状态
    state.entered = false;
  }
  function dispose() {
    Array.from(state.sessions.keys()).forEach(destroySession);
    state.entered = false;
    state.activeId = '';
    if (state.fullscreen) toggleFullscreen();
    renderTabs();
    updateStatus(null);
  }

  global.NPTerminal = { enter: enter, leave: leave, dispose: dispose };
})(window);
