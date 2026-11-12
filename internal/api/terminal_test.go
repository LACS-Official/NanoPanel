package api

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nanopanel/internal/config"
)

// fakeConn 是内存版 net.Conn，让帧编解码测试完全同步、无阻塞。
type fakeConn struct {
	in     *bytes.Buffer
	out    *bytes.Buffer
	closed bool
}

func (c *fakeConn) Read(p []byte) (int, error)       { return c.in.Read(p) }
func (c *fakeConn) Write(p []byte) (int, error)      { return c.out.Write(p) }
func (c *fakeConn) Close() error                     { c.closed = true; return nil }
func (c *fakeConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (c *fakeConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (c *fakeConn) SetDeadline(time.Time) error      { return nil }
func (c *fakeConn) SetReadDeadline(time.Time) error  { return nil }
func (c *fakeConn) SetWriteDeadline(time.Time) error { return nil }

func newTestWS(payload []byte) *wsConn {
	fc := &fakeConn{in: bytes.NewBuffer(payload), out: &bytes.Buffer{}}
	return &wsConn{
		conn: fc,
		rw:   bufio.NewReadWriter(bufio.NewReader(fc), bufio.NewWriter(fc)),
	}
}

// clientFrame 构造浏览器发来的掩码帧。
func clientFrame(op byte, payload []byte, fin bool) []byte {
	var b bytes.Buffer
	first := op
	if fin {
		first |= 0x80
	}
	b.WriteByte(first)
	n := len(payload)
	switch {
	case n < 126:
		b.WriteByte(0x80 | byte(n))
	case n <= 0xFFFF:
		b.WriteByte(0x80 | 126)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(n))
		b.Write(ext[:])
	default:
		b.WriteByte(0x80 | 127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		b.Write(ext[:])
	}
	mask := [4]byte{0x12, 0x34, 0x56, 0x78}
	b.Write(mask[:])
	for i, v := range payload {
		b.WriteByte(v ^ mask[i%4])
	}
	return b.Bytes()
}

// parseServerFrame 解析服务端写出的未掩码帧。
func parseServerFrame(b []byte) (byte, []byte, bool) {
	if len(b) < 2 {
		return 0, nil, false
	}
	op := b[0] & 0x0F
	fin := b[0]&0x80 != 0
	length := int(b[1] & 0x7F)
	off := 2
	switch length {
	case 126:
		length = int(binary.BigEndian.Uint16(b[2:4]))
		off = 4
	case 127:
		length = int(binary.BigEndian.Uint64(b[2:10]))
		off = 10
	}
	if len(b) < off+length {
		return op, nil, fin
	}
	return op, b[off : off+length], fin
}

func TestWSReadTextMessage(t *testing.T) {
	c := newTestWS(clientFrame(opText, []byte(`{"type":"resize","cols":120}`), true))
	op, data, err := c.readMessage()
	if err != nil {
		t.Fatalf("读取消息失败: %v", err)
	}
	if op != opText || string(data) != `{"type":"resize","cols":120}` {
		t.Fatalf("解析结果不符: op=%d data=%q", op, data)
	}
}

func TestWSReadFragmentedMessage(t *testing.T) {
	var raw bytes.Buffer
	raw.Write(clientFrame(opBinary, []byte("hello "), false))
	raw.Write(clientFrame(opContinuation, []byte("world"), true))
	c := newTestWS(raw.Bytes())
	op, data, err := c.readMessage()
	if err != nil {
		t.Fatalf("读取分片消息失败: %v", err)
	}
	if op != opBinary || string(data) != "hello world" {
		t.Fatalf("分片重组失败: op=%d data=%q", op, data)
	}
}

func TestWSReadPingAutoPong(t *testing.T) {
	var raw bytes.Buffer
	raw.Write(clientFrame(opPing, []byte("hi"), true))
	raw.Write(clientFrame(opText, []byte("next"), true))
	c := newTestWS(raw.Bytes())
	op, data, err := c.readMessage()
	if err != nil {
		t.Fatalf("读取消息失败: %v", err)
	}
	if op != opText || string(data) != "next" {
		t.Fatalf("ping 后应继续返回 text: op=%d data=%q", op, data)
	}
	fc := c.conn.(*fakeConn)
	gotOp, gotData, _ := parseServerFrame(fc.out.Bytes())
	if gotOp != opPong || string(gotData) != "hi" {
		t.Fatalf("未正确回应 pong: op=%d data=%q", gotOp, gotData)
	}
}

func TestWSRejectOversizeFrame(t *testing.T) {
	// 声明 2 MiB 长度但不提供载荷，解析器应在分配内存前拒绝。
	var raw bytes.Buffer
	raw.WriteByte(0x82) // FIN + binary
	raw.WriteByte(0x7F)
	var ext [8]byte
	binary.BigEndian.PutUint64(ext[:], uint64(2<<20))
	raw.Write(ext[:])
	c := newTestWS(raw.Bytes())
	if _, _, err := c.readMessage(); err == nil {
		t.Fatal("超大帧应当被拒绝")
	}
}

func TestWSWriteMessageFormat(t *testing.T) {
	c := newTestWS(nil)
	if err := c.writeMessage(opBinary, []byte("abc")); err != nil {
		t.Fatalf("写出消息失败: %v", err)
	}
	fc := c.conn.(*fakeConn)
	raw := fc.out.Bytes()
	if len(raw) < 2 || raw[1]&0x80 != 0 {
		t.Fatalf("服务端帧不应设置掩码位: %v", raw)
	}
	op, data, fin := parseServerFrame(raw)
	if op != opBinary || string(data) != "abc" || !fin {
		t.Fatalf("帧格式错误: op=%d data=%q fin=%v", op, data, fin)
	}
}

func TestWSOriginValidation(t *testing.T) {
	cases := []struct {
		origin, host string
		want         bool
	}{
		{"", "example.com:8080", true},
		{"http://example.com:8080", "example.com:8080", true},
		{"https://example.com:8080", "example.com:8080", true},
		{"http://evil.com", "example.com:8080", false},
		{"http://example.com", "example.com:8080", false},
		{"not-a-url", "example.com:8080", false},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(http.MethodGet, "http://"+tc.host+"/api/v1/terminal/ws", nil)
		r.Host = tc.host
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		if got := wsOriginAllowed(r); got != tc.want {
			t.Errorf("origin=%q host=%q 期望 %v 得到 %v", tc.origin, tc.host, tc.want, got)
		}
	}
}

func TestWSConnectSources(t *testing.T) {
	if got := wsConnectSources("192.168.10.7:8080"); !strings.Contains(got, "ws://192.168.10.7:8080") ||
		!strings.Contains(got, "wss://192.168.10.7:8080") {
		t.Fatalf("同源 ws 白名单生成错误: %q", got)
	}
	if got := wsConnectSources("[::1]:8080"); !strings.Contains(got, "ws://[::1]:8080") {
		t.Fatalf("IPv6 主机应被允许: %q", got)
	}
	for _, bad := range []string{"evil.com/ x", "a b", "x\r\ninjected:y", ""} {
		if got := wsConnectSources(bad); got != "" {
			t.Errorf("非法 Host %q 不应生成白名单: %q", bad, got)
		}
	}
}

func TestSecurityHeadersIncludeWS(t *testing.T) {
	s := &Server{cfg: config.Default()}
	h := s.SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest(http.MethodGet, "http://panel.local:8080/", nil)
	r.Host = "panel.local:8080"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "ws://panel.local:8080") || !strings.Contains(csp, "script-src 'self'") {
		t.Fatalf("CSP 未正确放行同源 WebSocket: %q", csp)
	}
}

func TestTerminalTicketIsOneTime(t *testing.T) {
	s := &Server{cfg: config.Default()}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/terminal/session", strings.NewReader(`{"cols":120,"rows":40}`))
	s.CreateTerminalSession(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("创建会话失败: HTTP %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			Ticket string `json:"ticket"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Data.Ticket == "" {
		t.Fatal("未返回票据")
	}
	if !s.consumeTerminalTicket(resp.Data.Ticket) {
		t.Fatal("首次消费票据应成功")
	}
	if s.consumeTerminalTicket(resp.Data.Ticket) {
		t.Fatal("票据不能被重复消费")
	}
	if s.consumeTerminalTicket("") {
		t.Fatal("空票据应被拒绝")
	}
}

func TestTerminalTicketExpires(t *testing.T) {
	s := &Server{cfg: config.Default(), termTickets: map[string]terminalTicket{}}
	s.termTickets["expired"] = terminalTicket{expires: time.Now().Add(-time.Second)}
	if s.consumeTerminalTicket("expired") {
		t.Fatal("过期票据应被拒绝")
	}
}

func TestTerminalSettingsDefaultEnabled(t *testing.T) {
	s := &Server{cfg: config.Default()}
	if !s.terminalEnabled() {
		t.Fatal("默认应启用内置终端")
	}
	off := false
	s.cfg.TerminalEnabled = &off
	if s.terminalEnabled() {
		t.Fatal("显式关闭后应禁用")
	}
}
