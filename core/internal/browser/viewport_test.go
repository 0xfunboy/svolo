package browser

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

type viewportTransport struct {
	targetTransport
	entered, release chan struct{}
	once             sync.Once
	methods          []string
}

func (f *viewportTransport) Call(ctx context.Context, sid, tab, method string, args map[string]any, out any) error {
	if method == "Page.navigate" && f.entered != nil {
		f.once.Do(func() { close(f.entered) })
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.release:
		}
	}
	f.mu.Lock()
	f.methods = append(f.methods, method)
	f.mu.Unlock()
	return f.targetTransport.Call(ctx, sid, tab, method, args, out)
}

func TestViewportPreservesAgentLeaseAndTarget(t *testing.T) {
	f := &viewportTransport{targetTransport: targetTransport{targets: []Target{{ID: "a"}, {ID: "b"}}}}
	e := NewEngine(f, t.TempDir())
	c, _ := e.Control("resize", "agent")
	s, _ := e.get("resize")
	s.target = "b"
	s.refs["doc-a"] = reference{Epoch: c.Epoch, Tab: "a"}
	s.refs["doc-b"] = reference{Epoch: c.Epoch, Tab: "b"}
	before, _ := e.Control("resize", "")
	if _, err := e.ConfigureViewport(context.Background(), "resize", ViewportSettings{Tab: "a", Width: 1920, Height: 1080, DPR: 1}); err != nil {
		t.Fatal(err)
	}
	after, _ := e.Control("resize", "")
	if before != after {
		t.Fatalf("resize changed control or target: %v -> %v", before, after)
	}
	if len(f.calls) != 2 || f.calls[0] != "a" || f.calls[1] != "a" {
		t.Fatal("resize reached wrong tab", f.calls)
	}
	if _, ok := s.refs["doc-a"]; ok {
		t.Fatal("resized snapshot remained valid")
	}
	if _, ok := s.refs["doc-b"]; !ok {
		t.Fatal("other tab snapshot invalidated")
	}
	if _, err := e.Run(context.Background(), "resize", "agent", "fill", map[string]any{"tab": "a", "target": "@doc-a:1", "text": "fixture"}); err == nil || !strings.Contains(err.Error(), "target_stale") {
		t.Fatal("old geometry was accepted", err)
	}
	if _, err := e.Run(context.Background(), "resize", "agent", "navigate", map[string]any{"url": "about:blank"}); err != nil {
		t.Fatal(err)
	}
	if f.calls[len(f.calls)-1] != "b" {
		t.Fatal("resize redirected next agent action")
	}
}

func TestViewportQueuesWithoutCancellingAnAgentAction(t *testing.T) {
	f := &viewportTransport{targetTransport: targetTransport{targets: []Target{{ID: "a"}}}, entered: make(chan struct{}), release: make(chan struct{})}
	e := NewEngine(f, t.TempDir())
	e.Control("queue", "agent")
	action := make(chan error, 1)
	go func() {
		_, err := e.Run(context.Background(), "queue", "agent", "navigate", map[string]any{"tab": "a", "url": "about:blank"})
		action <- err
	}()
	select {
	case <-f.entered:
	case <-time.After(time.Second):
		t.Fatal("action did not enter transport")
	}
	before, _ := e.Control("queue", "")
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	_, err := e.ConfigureViewport(ctx, "queue", ViewportSettings{Tab: "a", Width: 1920, Height: 1080, DPR: 1})
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("queued resize ignored deadline", err)
	}
	after, _ := e.Control("queue", "")
	if after != before || !after.Active {
		t.Fatal("expired resize changed active lease", before, after)
	}
	resize := make(chan error, 1)
	go func() {
		_, err := e.ConfigureViewport(context.Background(), "queue", ViewportSettings{Tab: "a", Width: 1080, Height: 1920, DPR: 1, Mobile: true, Touch: true})
		resize <- err
	}()
	close(f.release)
	if err := <-action; err != nil {
		t.Fatal("resize cancelled agent", err)
	}
	if err := <-resize; err != nil {
		t.Fatal(err)
	}
	after, _ = e.Control("queue", "")
	if after.Owner != before.Owner || after.Epoch != before.Epoch || after.Tab != before.Tab {
		t.Fatal("queued resize changed lease", before, after)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Join(f.methods, ",") != "Page.navigate,Emulation.setDeviceMetricsOverride,Emulation.setTouchEmulationEnabled" {
		t.Fatal("geometry interleaved with agent action", f.methods)
	}
}

func TestViewportRejectsUnownedAndInvalidSettings(t *testing.T) {
	f := &viewportTransport{targetTransport: targetTransport{targets: []Target{{ID: "owned"}}}}
	e := NewEngine(f, t.TempDir())
	valid := ViewportSettings{Tab: "owned", Width: 1920, Height: 1080, DPR: 1}
	for _, change := range []func(*ViewportSettings){func(v *ViewportSettings) { v.Tab = "foreign" }, func(v *ViewportSettings) { v.Tab = "" }, func(v *ViewportSettings) { v.Width = 99 }, func(v *ViewportSettings) { v.Height = 8193 }, func(v *ViewportSettings) { v.DPR = math.NaN() }, func(v *ViewportSettings) { v.DPR = 6 }} {
		v := valid
		change(&v)
		if _, err := e.ConfigureViewport(context.Background(), "validation", v); err == nil {
			t.Fatal("invalid resize accepted", v)
		}
	}
	if len(f.calls) > 0 {
		t.Fatal("rejected configuration reached CDP")
	}
}
