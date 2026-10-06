// Package cdp contains a bounded, dependency-free Chrome DevTools client.
// It supports only the RFC 6455 features needed by CDP (no compression/extensions).
package cdp

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
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

const maxMessage = 32 << 20

type socket struct {
	conn    net.Conn
	r       *bufio.Reader
	writeMu sync.Mutex
}

func dial(ctx context.Context, endpoint string) (*socket, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	if (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" || u.User != nil {
		return nil, errors.New("invalid websocket endpoint")
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "wss" {
			port = "443"
		} else {
			port = "80"
		}
	}
	d := net.Dialer{Timeout: 10 * time.Second}
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return nil, err
	}
	good := false
	defer func() {
		if !good {
			c.Close()
		}
	}()
	if u.Scheme == "wss" {
		t := tls.Client(c, &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12})
		if err = t.HandshakeContext(ctx); err != nil {
			return nil, err
		}
		c = t
	}
	c.SetDeadline(time.Now().Add(15 * time.Second))
	nonce := make([]byte, 16)
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(nonce)
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	if _, err = fmt.Fprintf(c, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", path, u.Host, key); err != nil {
		return nil, err
	}
	r := bufio.NewReaderSize(c, 64<<10)
	resp, err := http.ReadResponse(r, &http.Request{Method: "GET"})
	if err != nil {
		return nil, err
	}
	hash := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	if resp.StatusCode != 101 || !strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") || resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(hash[:]) {
		return nil, fmt.Errorf("websocket upgrade rejected: %s", resp.Status)
	}
	c.SetDeadline(time.Time{})
	good = true
	return &socket{conn: c, r: r}, nil
}
func (s *socket) write(op byte, data []byte) error {
	if len(data) > maxMessage {
		return errors.New("websocket payload too large")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	header := []byte{0x80 | op}
	n := len(data)
	switch {
	case n < 126:
		header = append(header, 0x80|byte(n))
	case n <= 65535:
		header = append(header, 0xfe, byte(n>>8), byte(n))
	default:
		header = append(header, 0xff)
		v := make([]byte, 8)
		binary.BigEndian.PutUint64(v, uint64(n))
		header = append(header, v...)
	}
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	header = append(header, mask...)
	body := make([]byte, n)
	for i := range data {
		body[i] = data[i] ^ mask[i%4]
	}
	s.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	defer s.conn.SetWriteDeadline(time.Time{})
	for _, b := range [][]byte{header, body} {
		for len(b) > 0 {
			n, err := s.conn.Write(b)
			if err != nil {
				return err
			}
			b = b[n:]
		}
	}
	return nil
}
func (s *socket) read() ([]byte, error) {
	var message []byte
	var fragmented bool
	for {
		h := make([]byte, 2)
		if _, err := io.ReadFull(s.r, h); err != nil {
			return nil, err
		}
		if h[0]&0x70 != 0 || h[1]&0x80 != 0 {
			return nil, errors.New("unsupported websocket frame flags")
		}
		fin := h[0]&0x80 != 0
		op := h[0] & 15
		n := uint64(h[1] & 127)
		if n == 126 {
			b := make([]byte, 2)
			if _, e := io.ReadFull(s.r, b); e != nil {
				return nil, e
			}
			n = uint64(binary.BigEndian.Uint16(b))
		} else if n == 127 {
			b := make([]byte, 8)
			if _, e := io.ReadFull(s.r, b); e != nil {
				return nil, e
			}
			n = binary.BigEndian.Uint64(b)
		}
		if n > maxMessage || uint64(len(message))+n > maxMessage {
			return nil, errors.New("websocket message limit exceeded")
		}
		if op >= 8 && (!fin || n > 125) {
			return nil, errors.New("invalid control frame")
		}
		p := make([]byte, int(n))
		if _, e := io.ReadFull(s.r, p); e != nil {
			return nil, e
		}
		switch op {
		case 8:
			return nil, io.EOF
		case 9:
			if e := s.write(10, p); e != nil {
				return nil, e
			}
			continue
		case 10:
			continue
		case 1, 2:
			if fragmented {
				return nil, errors.New("unexpected websocket message")
			}
			fragmented = !fin
		case 0:
			if !fragmented {
				return nil, errors.New("unexpected continuation")
			}
		default:
			return nil, errors.New("unsupported websocket opcode")
		}
		message = append(message, p...)
		if fin {
			return message, nil
		}
	}
}
