package wsstream

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func frame(op byte, b []byte, mask bool) []byte {
	h := []byte{op}
	if len(b) < 126 {
		h = append(h, byte(len(b)))
	} else {
		h = append(h, 126, byte(len(b)>>8), byte(len(b)))
	}
	if mask {
		h[1] |= 128
		h = append(h, 1, 2, 3, 4)
		for i, v := range b {
			h = append(h, v^byte(i%4+1))
		}
	} else {
		h = append(h, b...)
	}
	return h
}
func connection(t *testing.T, data []byte) *Conn {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	go func() { b.Write(data); b.Close() }()
	return &Conn{conn: a, r: bufio.NewReader(a)}
}
func TestBinaryStream(t *testing.T) {
	raw := append(frame(2, []byte("hel"), true), frame(128, []byte("lo"), true)...)
	c := connection(t, raw)
	b, e := io.ReadAll(c)
	if e != nil || !bytes.Equal(b, []byte("hello")) {
		t.Fatal(e, string(b))
	}
}
func TestRejectBadFrames(t *testing.T) {
	huge := []byte{130, 255, 0, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint64(huge[2:], MaxFrame+1)
	for _, b := range [][]byte{frame(130, []byte("x"), false), frame(129, []byte("x"), true), frame(128, nil, true), frame(194, nil, true), huge} {
		c := connection(t, b)
		if _, e := io.ReadAll(c); e == nil {
			t.Fatal("invalid frame accepted", b)
		}
	}
}
func TestUpgradeValidation(t *testing.T) {
	for _, key := range []string{"", "short", "YWJjZA=="} {
		r := httptest.NewRequest("GET", "http://127.0.0.1", nil)
		r.Header = http.Header{"Upgrade": {"websocket"}, "Connection": {"Upgrade"}, "Sec-Websocket-Version": {"13"}, "Sec-Websocket-Key": {key}}
		if _, e := Upgrade(httptest.NewRecorder(), r); e == nil {
			t.Fatal("bad handshake accepted")
		}
	}
}
