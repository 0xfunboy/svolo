//go:build !windows

package ssh

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This is a loopback process fixture, NOT a real SSH server. It tests lifecycle,
// protocol negotiation and reconnection without claiming SSH interoperability.
func TestTunnelPeerProcess(t *testing.T) {
	if os.Getenv("SVOLO_TEST_SSH_PEER") != "1" {
		return
	}
	args := os.Args
	bind := ""
	alias := args[len(args)-1]
	for i, a := range args {
		if a == "-L" && i+1 < len(args) {
			p := strings.Split(args[i+1], ":")
			bind = net.JoinHostPort(p[0], p[1])
		}
	}
	if bind == "" {
		os.Exit(2)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-credential" {
			w.WriteHeader(401)
			return
		}
		protocol := 1
		if alias == "incompatible" {
			protocol = 9
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "protocol": protocol})
	})
	mux.HandleFunc("/v1/echo", func(w http.ResponseWriter, r *http.Request) { io.Copy(w, r.Body) })
	if e := http.ListenAndServe(bind, mux); e != nil {
		os.Exit(3)
	}
	os.Exit(0)
}
func fixtureManager(t *testing.T) (*Manager, Host) {
	t.Helper()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "ssh-fixture")
	text := "#!/bin/sh\nSVOLO_TEST_SSH_PEER=1 exec " + shQuote(exe) + " -test.run=^TestTunnelPeerProcess$ -- \"$@\"\n"
	if e = os.WriteFile(path, []byte(text), 0700); e != nil {
		t.Fatal(e)
	}
	m := NewManager()
	m.Binary = path
	m.ResolveSecret = func(string) (string, error) { return "fixture-credential", nil }
	t.Cleanup(m.Close)
	return m, Host{ID: "dev", Alias: "fixture", RemotePort: 1234, TokenRef: "fixture-token", AutoReconnect: true}
}
func TestTunnelReadinessProtocolAndNoReplay(t *testing.T) {
	m, h := fixtureManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, e := m.Connect(ctx, h)
	if e != nil || !st.Connected || st.Protocol != 1 {
		t.Fatalf("%+v %v", st, e)
	}
	r, e := m.Do(ctx, h.ID, "POST", "/v1/echo", strings.NewReader("one action"))
	if e != nil {
		t.Fatal(e)
	}
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if string(b) != "one action" {
		t.Fatal(string(b))
	}
	m.mu.Lock()
	old := m.tunnels[h.ID]
	m.mu.Unlock()
	_ = old.cmd.Process.Kill()
	<-old.done
	deadline := time.Now().Add(6 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		m.mu.Lock()
		next := m.tunnels[h.ID]
		ready = next != nil && next != old && next.ready
		m.mu.Unlock()
		if ready {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("not reconnected: %+v", m.Status())
	}
	if e = m.Disconnect(h.ID); e != nil {
		t.Fatal(e)
	}
	time.Sleep(1100 * time.Millisecond)
	if len(m.Status()) != 0 {
		t.Fatal("explicit disconnect must cancel retries")
	}
	if _, e = m.Do(ctx, h.ID, "POST", "/v1/echo", strings.NewReader("must not replay")); e == nil {
		t.Fatal("disconnected request succeeded")
	}
}
func TestTunnelRejectsIncompatibleProtocol(t *testing.T) {
	m, h := fixtureManager(t)
	h.Alias = "incompatible"
	_, e := m.Connect(context.Background(), h)
	if e == nil || !strings.Contains(e.Error(), "protocol 9") {
		t.Fatal(e)
	}
	if len(m.Status()) != 0 {
		t.Fatal("failed negotiation retained tunnel")
	}
}
func TestBootstrapValidationAndQuotedInputs(t *testing.T) {
	h := Host{ID: "test", Alias: "test-box", RemotePort: 7331, TokenEnv: "TEMP_TOKEN"}
	for _, osName := range []string{"linux", "darwin", "windows"} {
		cmd, e := BootstrapCommand(h, Platform{osName, "amd64"}, strings.Repeat("a", 64), "process", false)
		if e != nil || cmd == "" {
			t.Fatal(osName, e)
		}
		if strings.Contains(cmd, "StrictHostKeyChecking=no") {
			t.Fatal(cmd)
		}
	}
	_, e := BootstrapCommand(h, Platform{"linux", "amd64"}, "$(touch bad)", "process", false)
	if e == nil {
		t.Fatal("invalid hash accepted")
	}
	for _, p := range []Platform{{"linux", "x86_64"}, {"darwin", "aarch64"}, {"windows", "AMD64"}} {
		if _, e = normalizePlatform(p.OS, p.Arch); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = normalizePlatform("plan9", "amd64"); e == nil {
		t.Fatal("unsupported OS")
	}
	args := CommandArgs(h.Alias, "uname -sm")
	if args[len(args)-2] != h.Alias || args[len(args)-1] != "uname -sm" {
		t.Fatal(args)
	}
	// Port boundaries are rejected before any command can be built.
	for _, n := range []int{0, -1, 65536} {
		h.RemotePort = n
		if Validate(h) == nil {
			t.Fatal(fmt.Sprint("accepted ", strconv.Itoa(n)))
		}
	}
}
