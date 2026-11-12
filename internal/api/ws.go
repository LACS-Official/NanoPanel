package api

// 零外部依赖的 WebSocket 服务端实现（RFC 6455 子集）。
// 项目保持 Go 单二进制、无第三方模块，因此这里直接实现握手与帧编解码：
//   - 支持 text / binary / continuation / ping / pong / close 帧
//   - 客户端帧强制掩码，服务端帧不掩码
//   - 自动应答 ping、处理分片消息、限制单条消息大小
//   - Hijack 后清除 net.Conn 的 deadline，避免被 http.Server 的
//     ReadTimeout / WriteTimeout 在数分钟后掐断长连接

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	wsGUID         = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	wsMaxMessage   = 1 << 20 // 单条消息上限 1 MiB，防止恶意帧耗尽内存
	wsWriteTimeout = 10 * time.Second
)

const (
	opContinuation byte = 0x0
	opText         byte = 0x1
	opBinary       byte = 0x2
	opClose        byte = 0x8
	opPing         byte = 0x9
	opPong         byte = 0xA
)

var errWSClosed = errors.New("websocket 连接已关闭")

// wsConn 封装 Hijack 后的连接。读操作集中在单个 goroutine，
// 写操作可由读/输出泵/心跳三个 goroutine 并发调用，故用写锁保护。
type wsConn struct {
	conn      net.Conn
	rw        *bufio.ReadWriter
	writeMu   sync.Mutex
	closeOnce sync.Once
	closeErr  error
}

// wsConnectSources 为 CSP 生成精确同源的 ws/wss 白名单。
// 只接受合法的 host[:port] 字符，避免 Host 头注入 CSP 策略。
func wsConnectSources(host string) string {
	if host == "" || len(host) > 255 {
		return ""
	}
	for _, c := range host {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '-', c == ':', c == '[', c == ']':
		default:
			return ""
		}
	}
	return " ws://" + host + " wss://" + host
}

// wsOriginAllowed 校验 Origin，阻止跨站 WebSocket 劫持（CSWSH）。
// 浏览器发起的同源 WebSocket 携带 Origin；非浏览器客户端可不带。
func wsOriginAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// wsUpgrade 完成协议升级，返回可直接收发的连接。
func wsUpgrade(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") ||
		!strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
		return nil, errors.New("不是 WebSocket 升级请求")
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		return nil, errors.New("不支持的 WebSocket 版本")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(decoded) != 16 {
		return nil, errors.New("Sec-WebSocket-Key 无效")
	}
	if !wsOriginAllowed(r) {
		return nil, errors.New("不允许的 Origin")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("服务器不支持连接劫持")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, fmt.Errorf("连接劫持失败: %w", err)
	}
	// http.Server 的 ReadTimeout / WriteTimeout / IdleTimeout 已在底层
	// net.Conn 上设置过 deadline；Hijack 之后由其自行管理，必须清除。
	_ = conn.SetDeadline(time.Time{})

	sum := sha1.Sum([]byte(key + wsGUID))
	accept := base64.StdEncoding.EncodeToString(sum[:])
	_, err = fmt.Fprintf(rw,
		"HTTP/1.1 101 Switching Protocols\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Accept: %s\r\n\r\n", accept)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("写入握手响应失败: %w", err)
	}
	if err := rw.Flush(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("刷新握手响应失败: %w", err)
	}
	return &wsConn{conn: conn, rw: rw}, nil
}

// readFrame 读取单个帧，处理扩展长度与客户端掩码。
func (c *wsConn) readFrame() (fin bool, opcode byte, payload []byte, err error) {
	var head [2]byte
	if _, err = io.ReadFull(c.rw, head[:]); err != nil {
		return false, 0, nil, err
	}
	fin = head[0]&0x80 != 0
	if head[0]&0x70 != 0 {
		return false, 0, nil, errors.New("不支持的 RSV 扩展位")
	}
	opcode = head[0] & 0x0F
	masked := head[1]&0x80 != 0
	if !masked {
		return false, 0, nil, errors.New("客户端帧必须掩码")
	}
	length := uint64(head[1] & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(c.rw, ext[:]); err != nil {
			return false, 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(c.rw, ext[:]); err != nil {
			return false, 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext[:])
		if length&uint64(1<<63) != 0 {
			return false, 0, nil, errors.New("无效的长度字段")
		}
	}
	if opcode >= 0x8 && (length > 125 || !fin) {
		return false, 0, nil, errors.New("控制帧格式不合法")
	}
	if length > wsMaxMessage {
		return false, 0, nil, errors.New("帧长度超出限制")
	}
	var mask [4]byte
	if _, err = io.ReadFull(c.rw, mask[:]); err != nil {
		return false, 0, nil, err
	}
	payload = make([]byte, int(length))
	if _, err = io.ReadFull(c.rw, payload); err != nil {
		return false, 0, nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	return fin, opcode, payload, nil
}

// readMessage 组装一条完整消息，并自动处理控制帧。
func (c *wsConn) readMessage() (byte, []byte, error) {
	var fragOpcode byte
	var frag []byte
	for {
		fin, opcode, payload, err := c.readFrame()
		if err != nil {
			return 0, nil, err
		}
		switch opcode {
		case opPing:
			if err := c.writeControl(opPong, payload); err != nil {
				return 0, nil, err
			}
			continue
		case opPong:
			continue
		case opClose:
			_ = c.writeControl(opClose, nil)
			return 0, nil, errWSClosed
		case opContinuation:
			if fragOpcode == 0 {
				return 0, nil, errors.New("非法的 continuation 帧")
			}
			frag = append(frag, payload...)
			if len(frag) > wsMaxMessage {
				return 0, nil, errors.New("消息长度超出限制")
			}
			if fin {
				return fragOpcode, frag, nil
			}
		case opText, opBinary:
			if fin {
				return opcode, payload, nil
			}
			fragOpcode = opcode
			frag = append(frag, payload...)
		default:
			return 0, nil, fmt.Errorf("未知的操作码 %d", opcode)
		}
	}
}

// writeFrame 写单个未掩码帧（服务端不需要掩码）。
func (c *wsConn) writeFrame(opcode byte, payload []byte, fin bool) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout)); err != nil {
		return err
	}
	defer func() { _ = c.conn.SetWriteDeadline(time.Time{}) }()

	first := opcode
	if fin {
		first |= 0x80
	}
	if err := c.rw.WriteByte(first); err != nil {
		return err
	}
	n := len(payload)
	switch {
	case n < 126:
		if err := c.rw.WriteByte(byte(n)); err != nil {
			return err
		}
	case n <= 0xFFFF:
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(n))
		if err := c.rw.WriteByte(126); err != nil {
			return err
		}
		if _, err := c.rw.Write(ext[:]); err != nil {
			return err
		}
	default:
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		if err := c.rw.WriteByte(127); err != nil {
			return err
		}
		if _, err := c.rw.Write(ext[:]); err != nil {
			return err
		}
	}
	if n > 0 {
		if _, err := c.rw.Write(payload); err != nil {
			return err
		}
	}
	return c.rw.Flush()
}

func (c *wsConn) writeControl(opcode byte, payload []byte) error {
	return c.writeFrame(opcode, payload, true)
}

func (c *wsConn) writeMessage(opcode byte, payload []byte) error {
	return c.writeFrame(opcode, payload, true)
}

func (c *wsConn) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.conn.Close()
	})
	return c.closeErr
}
