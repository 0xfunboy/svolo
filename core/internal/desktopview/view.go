// Package desktopview brokers a full RFB desktop through one-use capabilities.
// It terminates the handshake and parses client messages, enforcing read-only
// access on the server. This is NOT a screenshot viewer or a second browser.
package desktopview

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"sync"
	"time"
)

type Binding struct {
	Session   string `json:"session"`
	Address   string `json:"address"`
	Dedicated bool   `json:"dedicated"`
}
type Ticket struct {
	Origin      string    `json:"origin"`
	Token       string    `json:"token"`
	Session     string    `json:"session"`
	Interactive bool      `json:"interactive"`
	Expires     time.Time `json:"expires"`
	address     string
}
type Manager struct {
	mu       sync.Mutex
	bindings map[string]Binding
	tickets  map[string]Ticket
	slots    chan struct{}
	closed   chan struct{}
	once     sync.Once
}

func New() *Manager {
	return &Manager{bindings: map[string]Binding{}, tickets: map[string]Ticket{}, slots: make(chan struct{}, 4), closed: make(chan struct{})}
}
func (m *Manager) Close() { m.once.Do(func() { close(m.closed) }) }
func Validate(b Binding) error {
	if b.Session == "" || !b.Dedicated {
		return errors.New("explicit dedicated desktop acknowledgement required")
	}
	h, p, e := net.SplitHostPort(b.Address)
	ip := net.ParseIP(h)
	port, pe := strconv.Atoi(p)
	if e != nil || pe != nil || ip == nil || !ip.IsLoopback() || port < 1 || port > 65535 {
		return errors.New("literal loopback RFB endpoint required; tunnel remote desktops over SSH")
	}
	return nil
}
func (m *Manager) Bind(b Binding) error {
	if e := Validate(b); e != nil {
		return e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.bindings) >= 64 {
		if _, ok := m.bindings[b.Session]; !ok {
			return errors.New("desktop binding budget exceeded")
		}
	}
	m.bindings[b.Session] = b
	for k, t := range m.tickets {
		if t.Session == b.Session {
			delete(m.tickets, k)
		}
	}
	return nil
}
func (m *Manager) Bindings() []Binding {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Binding{}
	for _, b := range m.bindings {
		out = append(out, b)
	}
	return out
}
func (m *Manager) Mint(sid string, interactive bool) (Ticket, error) {
	return m.MintForOrigin(sid, interactive, "")
}
func (m *Manager) MintForOrigin(sid string, interactive bool, origin string) (Ticket, error) {
	if origin != "" && !ValidOrigin(origin) {
		return Ticket{}, errors.New("loopback or native viewer origin required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for k, t := range m.tickets {
		if now.After(t.Expires) {
			delete(m.tickets, k)
		}
	}
	b, ok := m.bindings[sid]
	if !ok {
		return Ticket{}, errors.New("no dedicated desktop bound to this session")
	}
	if len(m.tickets) >= 128 {
		return Ticket{}, errors.New("viewer ticket budget exceeded")
	}
	v := make([]byte, 32)
	if _, e := rand.Read(v); e != nil {
		return Ticket{}, e
	}
	t := Ticket{Origin: origin, Token: hex.EncodeToString(v), Session: sid, Interactive: interactive, Expires: now.Add(time.Minute), address: b.Address}
	m.tickets[t.Token] = t
	return t, nil
}
func (m *Manager) Consume(token string) (Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tickets[token]
	delete(m.tickets, token)
	if !ok || time.Now().After(t.Expires) {
		return Ticket{}, errors.New("invalid, expired or consumed desktop ticket")
	}
	return t, nil
}

// Serve uses only RFB 3.7/3.8 None authentication at the explicitly registered
// LOOPBACK endpoint. Authentication and encryption of the remote hop are SSH's
// responsibility. Clipboard writes, desktop resize and vendor input are refused.
func (m *Manager) Serve(ctx context.Context, c io.ReadWriteCloser, t Ticket, lease func() (func() bool, error)) error {
	select {
	case m.slots <- struct{}{}:
		defer func() { <-m.slots }()
	default:
		return errors.New("desktop stream budget exceeded")
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Hour)
	defer cancel()
	defer c.Close()
	d := net.Dialer{Timeout: 5 * time.Second}
	up, e := d.DialContext(ctx, "tcp", t.address)
	if e != nil {
		return errors.New("dedicated RFB server is not reachable")
	}
	defer up.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-done:
			return
		case <-ctx.Done():
		case <-m.closed:
		}
		c.Close()
		up.Close()
	}()
	up.SetDeadline(time.Now().Add(20 * time.Second))
	if e = handshake(up, c); e != nil {
		return e
	}
	up.SetDeadline(time.Time{})
	allowed := func() bool { return false }
	if t.Interactive {
		allowed, e = lease()
		if e != nil {
			return e
		}
	}
	errs := make(chan error, 2)
	go func() { _, e := io.CopyBuffer(c, up, make([]byte, 64<<10)); errs <- e }()
	go func() { errs <- Filter(c, up, allowed) }()
	e = <-errs
	c.Close()
	up.Close()
	<-errs
	return e
}
func exact(r io.Reader, n int) ([]byte, error) {
	b := make([]byte, n)
	_, e := io.ReadFull(r, b)
	return b, e
}
func write(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, e := w.Write(b)
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
func handshake(up io.ReadWriter, c io.ReadWriter) error {
	v, e := exact(up, 12)
	if e != nil {
		return e
	}
	if string(v) != "RFB 003.008\n" && string(v) != "RFB 003.007\n" {
		return errors.New("RFB 3.7 or 3.8 required")
	}
	version := string(v)
	if e = write(up, v); e != nil {
		return e
	}
	b, e := exact(up, 1)
	if e != nil {
		return e
	}
	if b[0] == 0 {
		return errors.New("RFB security unavailable")
	}
	types, e := exact(up, int(b[0]))
	if e != nil {
		return e
	}
	found := false
	for _, k := range types {
		if k == 1 {
			found = true
		}
	}
	if !found {
		return errors.New("dedicated loopback RFB endpoint must use None authentication behind the Svolo broker")
	}
	if e = write(up, []byte{1}); e != nil {
		return e
	}
	if version == "RFB 003.008\n" {
		status, e := exact(up, 4)
		if e != nil {
			return e
		}
		if binary.BigEndian.Uint32(status) != 0 {
			return errors.New("RFB handshake rejected")
		}
	}
	if e = write(c, []byte("RFB 003.008\n")); e != nil {
		return e
	}
	cv, e := exact(c, 12)
	if e != nil {
		return e
	}
	if string(cv) != "RFB 003.008\n" {
		return errors.New("viewer must use RFB 3.8")
	}
	if e = write(c, []byte{1, 1}); e != nil {
		return e
	}
	s, e := exact(c, 1)
	if e != nil {
		return e
	}
	if s[0] != 1 {
		return errors.New("unexpected viewer security type")
	}
	if e = write(c, []byte{0, 0, 0, 0}); e != nil {
		return e
	}
	shared, e := exact(c, 1)
	if e != nil {
		return e
	}
	_ = shared
	if e = write(up, []byte{1}); e != nil {
		return e
	}
	init, e := exact(up, 24)
	if e != nil {
		return e
	}
	n := binary.BigEndian.Uint32(init[20:24])
	if n > 64<<10 {
		return errors.New("RFB desktop name exceeds budget")
	}
	name, e := exact(up, int(n))
	if e != nil {
		return e
	}
	if e = write(c, init); e != nil {
		return e
	}
	return write(c, name)
}
func Filter(r io.Reader, w io.Writer, allowed func() bool) error {
	for {
		tag, e := exact(r, 1)
		if e != nil {
			return e
		}
		var rest []byte
		switch tag[0] {
		case 0:
			rest, e = exact(r, 19)
		case 2:
			h, er := exact(r, 3)
			if er != nil {
				return er
			}
			n := int(binary.BigEndian.Uint16(h[1:]))
			if n > 256 {
				return errors.New("RFB encoding budget exceeded")
			}
			xs, er := exact(r, n*4)
			if er != nil {
				return er
			}
			keep := []byte{}
			for i := 0; i < n; i++ {
				v := int32(binary.BigEndian.Uint32(xs[i*4:]))
				if safeEncoding(v) {
					keep = append(keep, xs[i*4:i*4+4]...)
				}
			}
			out := []byte{2, 0, byte(len(keep) / 4 >> 8), byte(len(keep) / 4)}
			if er = write(w, append(out, keep...)); er != nil {
				return er
			}
			continue
		case 3:
			rest, e = exact(r, 9)
		case 4:
			rest, e = exact(r, 7)
			if e == nil && !allowed() {
				return errors.New("desktop input lease revoked or view-only")
			}
		case 5:
			rest, e = exact(r, 5)
			if e == nil && !allowed() {
				return errors.New("desktop input lease revoked or view-only")
			}
		case 6:
			return errors.New("clipboard writes are disabled by the desktop broker")
		default:
			return fmt.Errorf("RFB client operation %d is not authorized", tag[0])
		}
		if e != nil {
			return e
		}
		if e = write(w, append(tag, rest...)); e != nil {
			return e
		}
	}
}
func safeEncoding(x int32) bool {
	switch x {
	case 0, 1, 2, 5, 7, 16, 21, -260, -223, -224, -239, -307, 0x574d5664:
		return true
	}
	return x >= -32 && x <= -23 || x >= -256 && x <= -247
}

// A native Electron renderer has a file/null origin. It still needs an unguessable
// admin-minted capability. Arbitrary Internet origins are never registered.
func ValidOrigin(s string) bool {
	if s == "null" || s == "file://" {
		return true
	}
	u, e := url.Parse(s)
	return e == nil && u.Scheme == "http" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == "" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback())
}
func OriginMatches(t Ticket, origin string) bool {
	if origin == "" {
		return false
	}
	if t.Origin == "null" || t.Origin == "file://" {
		return origin == "null" || origin == "file://"
	}
	return origin == t.Origin
}
