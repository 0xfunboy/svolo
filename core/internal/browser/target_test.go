package browser

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

type targetTransport struct {
	mu      sync.Mutex
	targets []Target
	calls   []string
}

func (f *targetTransport) List(context.Context, string) ([]Target, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Target(nil), f.targets...), nil
}
func (f *targetTransport) Create(context.Context, string, string) (Target, error) {
	panic("unexpected target creation")
}
func (f *targetTransport) Call(_ context.Context, _, target, _ string, _ map[string]any, out any) error {
	f.mu.Lock()
	f.calls = append(f.calls, target)
	f.mu.Unlock()
	if out != nil {
		return json.Unmarshal([]byte(`{"result":{"value":true}}`), out)
	}
	return nil
}
func (f *targetTransport) Activate(context.Context, string, string) error { return nil }
func (f *targetTransport) CloseTab(context.Context, string, string) error { return nil }
func (f *targetTransport) CloseSession(string) error                      { return nil }
func (f *targetTransport) Close() error                                   { return nil }

func TestTargetsFailClosedWithoutASelection(t *testing.T) {
	f := &targetTransport{targets: []Target{{ID: "second", Type: "page"}, {ID: "first", Type: "page"}}}
	e := NewEngine(f, t.TempDir())
	e.Control("multi", "agent")
	if _, err := e.Run(context.Background(), "multi", "agent", "fill", map[string]any{"target": "css:input", "text": "wrong"}); err == nil || !strings.Contains(err.Error(), "tab_ambiguous") {
		t.Fatalf("ambiguous action: %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatal("ambiguous action reached a page")
	}
	if _, err := e.Run(context.Background(), "multi", "agent", "navigate", map[string]any{"tab": "first", "url": "about:blank"}); err != nil {
		t.Fatal(err)
	}
	before, _ := e.Control("multi", "")
	if before.Tab != "first" {
		t.Fatal(before)
	}
	if _, err := e.Observe(context.Background(), "multi", "state", map[string]any{"tab": "second"}); err != nil {
		t.Fatal(err)
	}
	after, _ := e.Control("multi", "")
	if before != after {
		t.Fatalf("observation changed control: %v -> %v", before, after)
	}
	value, err := e.Observe(context.Background(), "multi", "tabs", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tab := range value.([]Target) {
		if tab.Active != (tab.ID == "first") {
			t.Fatal(tab)
		}
	}
	if _, err := e.Run(context.Background(), "multi", "agent", "navigate", map[string]any{"url": "about:blank"}); err != nil {
		t.Fatal(err)
	}
	if f.calls[len(f.calls)-1] != "first" {
		t.Fatal("observation retargeted the next action")
	}
}

func TestSnapshotReferencesBindToTheirOwnedTab(t *testing.T) {
	f := &targetTransport{targets: []Target{{ID: "a"}, {ID: "b"}}}
	e := NewEngine(f, t.TempDir())
	c, _ := e.Control("refs", "agent")
	s, _ := e.get("refs")
	s.mu.Lock()
	s.target = "b"
	s.refs["doc-a"] = reference{Epoch: c.Epoch, Tab: "a"}
	s.refs["doc-b"] = reference{Epoch: c.Epoch, Tab: "b"}
	s.mu.Unlock()
	if _, err := e.Run(context.Background(), "refs", "agent", "fill", map[string]any{"tab": "b", "target": "@doc-a:1", "text": "wrong"}); err == nil || !strings.Contains(err.Error(), "target_tab_mismatch") {
		t.Fatalf("cross-tab ref: %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatal("cross-tab reference reached CDP")
	}
	if _, err := e.Run(context.Background(), "refs", "agent", "drag", map[string]any{"source": "@doc-a:1", "destination": "@doc-b:1"}); err == nil || !strings.Contains(err.Error(), "target_tab_mismatch") {
		t.Fatalf("cross-tab drag: %v", err)
	}
	if _, err := e.Run(context.Background(), "refs", "agent", "press-key", map[string]any{"target": "@doc-a:1", "key": "Enter"}); err != nil {
		t.Fatal(err)
	}
	if f.calls[len(f.calls)-1] != "a" {
		t.Fatal("reference resolved to active tab instead of snapshot tab")
	}
	e.Control("refs", "human")
	e.Control("refs", "agent")
	if _, err := e.Run(context.Background(), "refs", "agent", "press-key", map[string]any{"target": "@doc-a:1", "key": "Enter"}); err == nil || !strings.Contains(err.Error(), "target_stale") {
		t.Fatalf("stale epoch: %v", err)
	}
}

func TestObservationDoesNotQueueBehindAnAgentAction(t *testing.T) {
	f := &fakeTransport{block: make(chan struct{}), started: make(chan struct{}, 1)}
	e := NewEngine(f, t.TempDir())
	e.Control("live", "agent")
	done := make(chan error, 1)
	go func() { _, err := e.Run(context.Background(), "live", "agent", "state", nil); done <- err }()
	<-f.started
	before, _ := e.Control("live", "")
	if !before.Active {
		t.Fatal("agent was not active")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := e.Observe(ctx, "live", "tabs", nil); err != nil {
		t.Fatal(err)
	}
	after, _ := e.Control("live", "")
	if before != after {
		t.Fatalf("observation changed lease: %v -> %v", before, after)
	}
	e.Control("live", "human")
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("takeover failed to cancel original operation")
		}
	case <-time.After(time.Second):
		t.Fatal("original cancellation was lost")
	}
}
