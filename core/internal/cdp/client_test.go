package cdp

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func upgradeTest(w http.ResponseWriter, r *http.Request) (net.Conn, *bufio.Reader) {
	c, rw, _ := w.(http.Hijacker).Hijack()
	sum := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(sum[:]))
	rw.Flush()
	return c, rw.Reader
}
func readClient(r *bufio.Reader) ([]byte, error) {
	h := make([]byte, 2)
	if _, e := io.ReadFull(r, h); e != nil {
		return nil, e
	}
	n := int(h[1] & 127)
	if n == 126 {
		b := make([]byte, 2)
		io.ReadFull(r, b)
		n = int(binary.BigEndian.Uint16(b))
	}
	if n == 127 {
		return nil, fmt.Errorf("test message too big")
	}
	mask := make([]byte, 4)
	io.ReadFull(r, mask)
	b := make([]byte, n)
	_, e := io.ReadFull(r, b)
	for i := range b {
		b[i] ^= mask[i%4]
	}
	return b, e
}
func serverFrame(c net.Conn, op byte, b []byte) {
	h := []byte{op}
	if len(b) < 126 {
		h = append(h, byte(len(b)))
	} else {
		h = append(h, 126, byte(len(b)>>8), byte(len(b)))
	}
	c.Write(append(h, b...))
}
func TestConcurrentCallsAndEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, reader := upgradeTest(w, r)
		defer c.Close()
		for i := 0; i < 20; i++ {
			b, e := readClient(reader)
			if e != nil {
				return
			}
			var req map[string]any
			json.Unmarshal(b, &req)
			resp, _ := json.Marshal(map[string]any{"id": req["id"], "result": map[string]any{"ok": true}})
			serverFrame(c, 0x81, resp)
			serverFrame(c, 0x81, []byte(`{"method":"Page.test","params":{"x":1}}`))
		}
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()
	client, e := Connect(context.Background(), strings.Replace(server.URL, "http:", "ws:", 1))
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	events, stop := client.Subscribe(64)
	defer stop()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var out map[string]any
			if e := client.Call(ctx, "", "Test.call", map[string]any{}, &out); e != nil || out["ok"] != true {
				t.Errorf("%v %v", out, e)
			}
		}()
	}
	wg.Wait()
	select {
	case ev := <-events:
		if ev.Method != "Page.test" {
			t.Fatal(ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
}
func TestFragmentedMessageAndPing(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	s := &socket{conn: a, r: bufio.NewReader(a)}
	go func() {
		serverFrame(b, 0x01, []byte(`{"ok":`))
		serverFrame(b, 0x89, []byte("p"))
		readClient(bufio.NewReader(b))
		serverFrame(b, 0x80, []byte(`true}`))
	}()
	data, e := s.read()
	if e != nil || string(data) != `{"ok":true}` {
		t.Fatal(string(data), e)
	}
}
func TestRejectUpgradeAndSchemes(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }))
	defer s.Close()
	if c, e := Connect(context.Background(), strings.Replace(s.URL, "http:", "ws:", 1)); e == nil {
		c.Close()
		t.Fatal("accepted bad upgrade")
	}
	if c, e := Connect(context.Background(), "file:///foo"); e == nil {
		c.Close()
		t.Fatal("accepted file")
	}
}
func TestCancelledCall(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, rd := upgradeTest(w, r)
		defer c.Close()
		readClient(rd)
		time.Sleep(100 * time.Millisecond)
	}))
	defer s.Close()
	c, e := Connect(context.Background(), strings.Replace(s.URL, "http:", "ws:", 1))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if e = c.Call(ctx, "", "Test", nil, nil); e != context.DeadlineExceeded {
		t.Fatal(e)
	}
}
