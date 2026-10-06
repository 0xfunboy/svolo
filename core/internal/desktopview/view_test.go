package desktopview

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"
)

func TestTickets(t *testing.T) {
	m := New()
	defer m.Close()
	for _, a := range []string{"0.0.0.0:5900", "example.com:5900", "127.0.0.1:0", "localhost:5900", "[::]:5900"} {
		t.Run(a, func(t *testing.T) {
			if m.Bind(Binding{Session: "s", Address: a, Dedicated: true}) == nil {
				t.Fatal("accepted unsafe endpoint")
			}
		})
	}
	if e := m.Bind(Binding{Session: "s", Address: "127.0.0.1:5900", Dedicated: true}); e != nil {
		t.Fatal(e)
	}
	x, e := m.Mint("s", false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Consume(x.Token); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Consume(x.Token); e == nil {
		t.Fatal("replayed ticket")
	}
	x, _ = m.Mint("s", true)
	m.mu.Lock()
	q := m.tickets[x.Token]
	q.Expires = time.Now().Add(-time.Second)
	m.tickets[x.Token] = q
	m.mu.Unlock()
	if _, e = m.Consume(x.Token); e == nil {
		t.Fatal("expired ticket accepted")
	}
}
func TestReadOnlyRFB(t *testing.T) {
	for _, tag := range []byte{4, 5} {
		t.Run(string(rune('0'+tag)), func(t *testing.T) {
			b := make([]byte, map[byte]int{4: 8, 5: 6}[tag])
			b[0] = tag
			var out bytes.Buffer
			e := Filter(bytes.NewReader(b), &out, func() bool { return false })
			if e == nil || out.Len() != 0 {
				t.Fatal("view-only input leaked", e)
			}
		})
	}
}
func TestLeaseCheckedEveryMessage(t *testing.T) {
	b := []byte{4, 1, 0, 0, 0, 0, 0, 65}
	input := append(append([]byte{}, b...), b...)
	var out bytes.Buffer
	n := 0
	e := Filter(bytes.NewReader(input), &out, func() bool { n++; return n == 1 })
	if e == nil || !bytes.Equal(out.Bytes(), b) || n != 2 {
		t.Fatal(e, n, out.Bytes())
	}
}
func TestRFBRejectUnsupported(t *testing.T) {
	for _, tag := range []byte{6, 150, 248, 251, 255} {
		t.Run(string(rune(tag)), func(t *testing.T) {
			var out bytes.Buffer
			if Filter(bytes.NewReader([]byte{tag}), &out, func() bool { return true }) == nil || out.Len() != 0 {
				t.Fatal("unsupported operation leaked")
			}
		})
	}
}
func TestRFBEncodingFilter(t *testing.T) {
	xs := []int32{0, 1, -223, -258, -308, -309, -312, -313, -1063131698}
	b := []byte{2, 0, 0, byte(len(xs))}
	for _, x := range xs {
		var v [4]byte
		binary.BigEndian.PutUint32(v[:], uint32(x))
		b = append(b, v[:]...)
	}
	var out bytes.Buffer
	e := Filter(bytes.NewReader(b), &out, func() bool { return true })
	if !errors.Is(e, io.EOF) || len(out.Bytes()) != 16 || out.Bytes()[3] != 3 {
		t.Fatal(e, out.Bytes())
	}
}
func TestRFBMalformed(t *testing.T) {
	for _, b := range [][]byte{{2, 0, 1, 1}, {2, 0, 0, 1}, {0}, {3, 0}, {4, 1}} {
		var out bytes.Buffer
		if Filter(bytes.NewReader(b), &out, func() bool { return true }) == nil {
			t.Fatal("truncated data accepted")
		}
	}
}
