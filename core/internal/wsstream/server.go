// Package wsstream is a bounded RFC 6455 server for binary byte streams. It does
// not enable compression, browser cookies, subprotocol auth or arbitrary origins.
package wsstream

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
	"strings"
	"sync"
	"time"
)

const MaxFrame = 4 << 20

type Conn struct {
	conn       net.Conn
	r          *bufio.Reader
	buf        []byte
	mu         sync.Mutex
	fragmented bool
	message    int
}

func Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if r.Method != "GET" || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || !contains(r.Header.Get("Connection"), "upgrade") || r.Header.Get("Sec-WebSocket-Version") != "13" {
		return nil, errors.New("WebSocket v13 upgrade required")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	nonce, e := base64.StdEncoding.DecodeString(key)
	if e != nil || len(nonce) != 16 {
		return nil, errors.New("invalid WebSocket key")
	}
	h, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("HTTP connection cannot be upgraded")
	}
	c, rw, e := h.Hijack()
	if e != nil {
		return nil, e
	}
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	_, e = fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\nCache-Control: no-store\r\n\r\n", base64.StdEncoding.EncodeToString(sum[:]))
	if e == nil {
		e = rw.Flush()
	}
	if e != nil {
		c.Close()
		return nil, e
	}
	return &Conn{conn: c, r: rw.Reader}, nil
}
func contains(s, x string) bool {
	for _, v := range strings.Split(s, ",") {
		if strings.EqualFold(strings.TrimSpace(v), x) {
			return true
		}
	}
	return false
}
func (c *Conn) Close() error { return c.conn.Close() }
func (c *Conn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(c.buf) == 0 {
		c.conn.SetReadDeadline(time.Now().Add(3 * time.Minute))
		var h [2]byte
		if _, e := io.ReadFull(c.r, h[:]); e != nil {
			return 0, e
		}
		fin, op := h[0]&128 != 0, h[0]&15
		if h[0]&0x70 != 0 || h[1]&128 == 0 {
			return 0, errors.New("invalid or unmasked WebSocket frame")
		}
		n := uint64(h[1] & 127)
		if n == 126 {
			var v [2]byte
			if _, e := io.ReadFull(c.r, v[:]); e != nil {
				return 0, e
			}
			n = uint64(binary.BigEndian.Uint16(v[:]))
			if n < 126 {
				return 0, errors.New("noncanonical frame length")
			}
		}
		if n == 127 {
			var v [8]byte
			if _, e := io.ReadFull(c.r, v[:]); e != nil {
				return 0, e
			}
			n = binary.BigEndian.Uint64(v[:])
			if n <= 65535 {
				return 0, errors.New("noncanonical frame length")
			}
		}
		if n > MaxFrame || op >= 8 && (!fin || n > 125) {
			return 0, errors.New("WebSocket frame budget exceeded")
		}
		var mask [4]byte
		if _, e := io.ReadFull(c.r, mask[:]); e != nil {
			return 0, e
		}
		b := make([]byte, int(n))
		if _, e := io.ReadFull(c.r, b); e != nil {
			return 0, e
		}
		for i := range b {
			b[i] ^= mask[i%4]
		}
		switch op {
		case 8:
			return 0, io.EOF
		case 9:
			if e := c.frame(10, b); e != nil {
				return 0, e
			}
			continue
		case 10:
			continue
		case 2:
			if c.fragmented {
				return 0, errors.New("unexpected data frame")
			}
			c.message = 0
		case 0:
			if !c.fragmented {
				return 0, errors.New("unexpected continuation")
			}
		default:
			return 0, errors.New("binary WebSocket messages required")
		}
		c.message += len(b)
		if c.message > MaxFrame {
			return 0, errors.New("WebSocket message budget exceeded")
		}
		c.fragmented = !fin
		c.buf = b
	}
	n := copy(p, c.buf)
	c.buf = c.buf[n:]
	return n, nil
}
func (c *Conn) frame(op byte, b []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(b) > MaxFrame {
		return errors.New("WebSocket frame budget exceeded")
	}
	h := []byte{0x80 | op}
	n := len(b)
	switch {
	case n < 126:
		h = append(h, byte(n))
	case n <= 65535:
		h = append(h, 126, byte(n>>8), byte(n))
	default:
		h = append(h, 127, 0, 0, 0, 0, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
	c.conn.SetWriteDeadline(time.Now().Add(20 * time.Second))
	for _, p := range [][]byte{h, b} {
		for len(p) > 0 {
			n, e := c.conn.Write(p)
			if e != nil {
				return e
			}
			if n == 0 {
				return io.ErrShortWrite
			}
			p = p[n:]
		}
	}
	return nil
}
func (c *Conn) Write(p []byte) (int, error) {
	n := 0
	for len(p) > 0 {
		k := min(len(p), 64<<10)
		if e := c.frame(2, p[:k]); e != nil {
			return n, e
		}
		n += k
		p = p[k:]
	}
	return n, nil
}
