/* NanoPanel 历史趋势图：零依赖 SVG 折线图。
   数据来自 GET /api/v1/metrics，颜色跟随面板浅色/深色主题。 */
(function () {
  'use strict';

  var SERIES = [
    { key: 'cpu', label: 'CPU', color: '#0a84ff', unit: '%' },
    { key: 'mem', label: '内存', color: '#30d158', unit: '%' },
    { key: 'disk', label: '磁盘', color: '#ff9f0a', unit: '%' },
    { key: 'temp', label: '温度', color: '#ff453a', unit: '℃' }
  ];
  var NET_COLOR = '#bf5af2';

  var hidden = {};
  var lastContainer = null;
  var lastPayload = null;

  function esc(v) {
    return String(v == null ? '' : v)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
  }

  function num(v) { return Number(v) || 0; }

  function fmtTime(ts, range) {
    var d = new Date(ts * 1000);
    var p = function (n) { return n < 10 ? '0' + n : '' + n; };
    if (range === '7d') return p(d.getMonth() + 1) + '-' + p(d.getDate());
    if (range === '24h') return p(d.getMonth() + 1) + '-' + p(d.getDate()) + ' ' + p(d.getHours()) + ':' + p(d.getMinutes());
    return p(d.getHours()) + ':' + p(d.getMinutes());
  }

  function fmtRate(bps) {
    if (bps >= 1048576) return (bps / 1048576).toFixed(2) + ' MB/s';
    if (bps >= 1024) return (bps / 1024).toFixed(1) + ' KB/s';
    return Math.round(bps) + ' B/s';
  }

  function netRate(points, i) {
    if (i <= 0) return 0;
    var dt = num(points[i].ts) - num(points[i - 1].ts);
    if (dt <= 0) return 0;
    var rx = num(points[i].net_rx) - num(points[i - 1].net_rx);
    var tx = num(points[i].net_tx) - num(points[i - 1].net_tx);
    // 计数器回绕（网卡重置 / 设备重启）时不显示虚假尖峰
    return ((rx < 0 ? 0 : rx) + (tx < 0 ? 0 : tx)) / dt;
  }

  function xAt(i, n, left, width) {
    return n <= 1 ? left + width / 2 : left + (i / (n - 1)) * width;
  }

  function yAt(v, top, height, max) {
    var c = Math.max(0, Math.min(max, num(v)));
    return top + (1 - c / max) * height;
  }

  function pathFor(points, key, left, top, width, height, max) {
    var d = '';
    for (var i = 0; i < points.length; i++) {
      d += (i === 0 ? 'M' : 'L') + xAt(i, points.length, left, width).toFixed(1) +
        ' ' + yAt(points[i][key], top, height, max).toFixed(1) + ' ';
    }
    return d.trim();
  }

  function gridSVG(left, top, width, height, max, ticks) {
    var out = '';
    for (var i = 0; i <= 4; i++) {
      var y = top + (i / 4) * height;
      var val = max * (1 - i / 4);
      out += '<line class="npc-grid" x1="' + left + '" y1="' + y.toFixed(1) +
        '" x2="' + (left + width) + '" y2="' + y.toFixed(1) + '"/>';
      out += '<text class="npc-axis" x="' + (left - 6) + '" y="' + (y + 4).toFixed(1) +
        '" text-anchor="end">' + esc(ticks(val)) + '</text>';
    }
    return out;
  }

  function rateMax(points) {
    var m = 0;
    for (var i = 1; i < points.length; i++) {
      var r = netRate(points, i);
      if (r > m) m = r;
    }
    return Math.max(m * 1.2, 1024); // 至少 1 KB/s，避免空图除零
  }

  function render(container, payload) {
    if (!container) return;
    lastContainer = container;
    lastPayload = payload;
    var points = (payload && payload.points) || [];
    var range = (payload && payload.range) || '1h';

    if (points.length < 2) {
      container.innerHTML = '<div class="npc-empty">正在收集数据…面板每 ' +
        esc((payload && payload.interval_sec) || 60) + ' 秒采样一次，稍后刷新即可看到趋势。</div>';
      return;
    }

    var hasTemp = points.some(function (p) { return num(p.temp) > 0; });
    var W = 800, H = 220, left = 40, right = 12, top = 12, bottom = 26;
    var w = W - left - right, h = H - top - bottom;
    var netTop = 12, netH = 96, netBottom = 24, netW = w;
    var maxNet = rateMax(points);

    var legend = '<div class="npc-legend">';
    SERIES.forEach(function (s) {
      if (s.key === 'temp' && !hasTemp) return;
      var last = points[points.length - 1][s.key];
      legend += '<button type="button" class="npc-leg' + (hidden[s.key] ? ' off' : '') +
        '" data-key="' + s.key + '"><i style="background:' + s.color + '"></i>' +
        esc(s.label) + ' <b>' + esc(num(last).toFixed(1)) + esc(s.unit) + '</b></button>';
    });
    legend += '<span class="npc-legend-tip">点击图例可隐藏曲线</span></div>';

    var svg = '<svg class="npc-svg" viewBox="0 0 ' + W + ' ' + H + '" role="img" aria-label="CPU、内存、磁盘与温度历史趋势">';
    svg += gridSVG(left, top, w, h, 100, function (v) { return Math.round(v) + '%'; });
    SERIES.forEach(function (s) {
      if (hidden[s.key] || (s.key === 'temp' && !hasTemp)) return;
      svg += '<path d="' + pathFor(points, s.key, left, top, w, h, 100) +
        '" fill="none" stroke="' + s.color + '" stroke-width="2" stroke-linejoin="round" stroke-linecap="round"/>';
    });
    // 时间轴 3 个刻度
    [0, Math.floor((points.length - 1) / 2), points.length - 1].forEach(function (i) {
      var x = xAt(i, points.length, left, w);
      svg += '<text class="npc-axis" x="' + x.toFixed(1) + '" y="' + (H - 6) +
        '" text-anchor="' + (i === 0 ? 'start' : (i === points.length - 1 ? 'end' : 'middle')) + '">' +
        esc(fmtTime(points[i].ts, range)) + '</text>';
    });
    svg += '<line class="npc-cross" id="npcCross" x1="0" y1="' + top + '" x2="0" y2="' + (top + h) + '" style="display:none"/>';
    svg += '<rect id="npcHover" x="' + left + '" y="' + top + '" width="' + w + '" height="' + h + '" fill="transparent"/>';
    svg += '</svg>';

    // 流量单独一张自适应量程的小图，避免与百分比混用量纲
    var netSVG = '<div class="npc-subtitle">网络流量（下行 + 上行）</div>';
    netSVG += '<svg class="npc-svg npc-net" viewBox="0 0 ' + W + ' ' + (netTop + netH + netBottom) + '" role="img" aria-label="网络流量历史趋势">';
    netSVG += gridSVG(left, netTop, netW, netH, maxNet, function (v) {
      return v >= 1048576 ? (v / 1048576).toFixed(1) + 'M' : Math.round(v / 1024) + 'K';
    });
    var netPath = '';
    for (var i = 0; i < points.length; i++) {
      netPath += (i === 0 ? 'M' : 'L') + xAt(i, points.length, left, netW).toFixed(1) +
        ' ' + yAt(netRate(points, i), netTop, netH, maxNet).toFixed(1) + ' ';
    }
    netSVG += '<path d="' + netPath.trim() + '" fill="none" stroke="' + NET_COLOR + '" stroke-width="2" stroke-linejoin="round"/>';
    netSVG += '<text class="npc-axis" x="' + (W - right) + '" y="' + (netTop + netH + 16) +
      '" text-anchor="end">峰值 ' + esc(fmtRate(maxNet / 1.2)) + '</text>';
    netSVG += '</svg>';

    container.innerHTML = legend + svg + netSVG + '<div class="npc-tip" id="npcTip"></div>';
    bindEvents(container, points, range, hasTemp, left, w, maxNet);
  }

  function bindEvents(container, points, range, hasTemp, left, w, maxNet) {
    container.querySelectorAll('.npc-leg').forEach(function (b) {
      b.addEventListener('click', function () {
        var k = b.dataset.key;
        hidden[k] = !hidden[k];
        render(lastContainer, lastPayload);
      });
    });

    var svg = container.querySelector('.npc-svg');
    var cross = container.querySelector('#npcCross');
    var tip = container.querySelector('#npcTip');
    if (!svg || !cross || !tip) return;
    var hover = container.querySelector('#npcHover');
    hover.addEventListener('mousemove', function (ev) {
      var rect = svg.getBoundingClientRect();
      var scale = 800 / rect.width;
      var vx = (ev.clientX - rect.left) * scale;
      var ratio = Math.max(0, Math.min(1, (vx - left) / w));
      var i = Math.round(ratio * (points.length - 1));
      var x = xAt(i, points.length, left, w);
      cross.setAttribute('x1', x); cross.setAttribute('x2', x);
      cross.style.display = '';
      var html = '<b>' + esc(new Date(points[i].ts * 1000).toLocaleString()) + '</b>';
      SERIES.forEach(function (s) {
        if (hidden[s.key] || (s.key === 'temp' && !hasTemp)) return;
        html += '<span><i style="background:' + s.color + '"></i>' + esc(s.label) +
          ' ' + esc(num(points[i][s.key]).toFixed(1)) + esc(s.unit) + '</span>';
      });
      html += '<span><i style="background:' + NET_COLOR + '"></i>流量 ' + esc(fmtRate(netRate(points, i))) + '</span>';
      tip.innerHTML = html;
      tip.style.display = 'block';
      // tooltip 跟随鼠标但不出容器右边界
      var cx = ev.clientX - container.getBoundingClientRect().left;
      tip.style.left = Math.min(cx + 12, Math.max(0, container.clientWidth - 190)) + 'px';
    });
    hover.addEventListener('mouseleave', function () {
      cross.style.display = 'none';
      tip.style.display = 'none';
    });
  }

  window.NPChart = { render: render };
})();
