package piruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRuntimeChild(t *testing.T) {
	if os.Getenv("SVOLO_TEST_RUNTIME") != "1" {
		return
	}
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		var p map[string]any
		_ = json.Unmarshal(s.Bytes(), &p)
		if p["type"] == "extension_ui_response" {
			fmt.Println(`{"type":"ui_done"}`)
			continue
		}
		if p["type"] == "silent" {
			continue
		}
		fmt.Println(`{"type":"agent_start"}`)
		if p["type"] == "bad" {
			fmt.Println("not JSON")
		}
		b, _ := json.Marshal(map[string]any{"type": "response", "id": p["id"], "success": true, "command": p["type"], "data": p})
		fmt.Println(string(b))
		fmt.Println(`{"type":"agent_end"}`)
	}
	os.Exit(0)
}
func testManager(t *testing.T) (*Manager, State) {
	t.Helper()
	m := New(nil, os.Args[0])
	m.Prefix = []string{"-test.run=TestRuntimeChild", "--"}
	s, e := m.Start(Options{Session: "test", CWD: t.TempDir(), Env: map[string]string{"SVOLO_TEST_RUNTIME": "1"}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Close)
	return m, s
}
func TestRuntimeRoundTrip(t *testing.T) {
	m, s := testManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, e := m.Send(ctx, s.ID, map[string]any{"type": "get_state", "id": "untrusted"})
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(r), `"success":true`) || strings.Contains(string(r), "untrusted") {
		t.Fatal(string(r))
	}
	if e = m.ReplyUI(s.ID, map[string]any{"type": "extension_ui_response", "id": "u1"}); e != nil {
		t.Fatal(e)
	}
	if e = m.Stop(ctx, s.ID); e != nil {
		t.Fatal(e)
	}
	events, e := m.Events(s.ID, 0)
	if e != nil || len(events.Events) < 3 || events.State.Status != "exited" {
		t.Fatal(events, e)
	}
	if _, e = m.Send(ctx, s.ID, map[string]any{"type": "prompt"}); e == nil {
		t.Fatal("replayed after exit")
	}
}
func TestRuntimeTimeoutNoReplay(t *testing.T) {
	m, s := testManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	_, e := m.Send(ctx, s.ID, map[string]any{"type": "silent"})
	if e == nil || !strings.Contains(e.Error(), "no automatic replay") {
		t.Fatal(e)
	}
}
func TestRuntimeValidation(t *testing.T) {
	m := New(nil, os.Args[0])
	defer m.Close()
	if _, e := m.Start(Options{Session: "../bad", CWD: t.TempDir()}); e == nil {
		t.Fatal("invalid session")
	}
	if _, e := m.Start(Options{Session: "ok", CWD: t.TempDir(), Env: map[string]string{"SVOLO_VAULT_KEY": "secret"}}); e == nil {
		t.Fatal("vault leaked")
	}
}
