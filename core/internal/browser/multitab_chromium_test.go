package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"
)

func TestChromiumMultiTabObservation(t *testing.T) {
	if os.Getenv("SVOLO_E2E") != "1" {
		t.Skip("set SVOLO_E2E=1 to run real Chromium multi-tab acceptance")
	}
	m := NewManaged(ManagedOptions{Root: t.TempDir(), Headless: true})
	defer m.Close()
	e := NewEngine(m, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const sid = "multitab"
	run := func(actor, name string, args map[string]any) any {
		t.Helper()
		value, err := e.Run(ctx, sid, actor, name, args)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return value
	}
	first := run("human", "open-browser", map[string]any{"url": "about:blank"}).(Target)
	tabs, err := m.List(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) != 1 || tabs[0].ID != first.ID {
		t.Fatalf("first open left an extra startup blank: %#v", tabs)
	}
	setDocument := func(tab, title string) {
		t.Helper()
		var tree struct {
			FrameTree struct {
				Frame struct {
					ID string `json:"id"`
				} `json:"frame"`
			} `json:"frameTree"`
		}
		if err := m.Call(ctx, sid, tab, "Page.getFrameTree", map[string]any{}, &tree); err != nil {
			t.Fatal(err)
		}
		color := "#ff0000"
		if title == "Second form" {
			color = "#0000ff"
		}
		html := `<!doctype html><title>` + title + `</title><style>html{background:` + color + `}</style><label>Name <input aria-label="Name" id="name"></label><p id="result"></p>`
		if err := m.Call(ctx, sid, tab, "Page.setDocumentContent", map[string]any{"frameId": tree.FrameTree.Frame.ID, "html": html}, nil); err != nil {
			t.Fatal(err)
		}
	}
	setDocument(first.ID, "First form")
	second := run("human", "open-tab", map[string]any{"url": "about:blank"}).(Target)
	if second.ID == first.ID {
		t.Fatal("second explicit tab reused a user page")
	}
	setDocument(second.ID, "Second form")
	tabs, err = m.List(ctx, sid)
	if err != nil || len(tabs) != 2 {
		t.Fatalf("two pages: %#v %v", tabs, err)
	}
	e.Control(sid, "agent")
	state := run("agent", "state", map[string]any{"tab": first.ID}).(map[string]any)
	if state["tab"] != first.ID || state["title"] != "First form" {
		t.Fatal(state)
	}
	snapshot := run("agent", "snapshot-interactive", map[string]any{"tab": first.ID}).(map[string]any)
	var ref string
	for _, raw := range snapshot["elements"].([]any) {
		node := raw.(map[string]any)
		if node["label"] == "Name" {
			ref = node["ref"].(string)
		}
	}
	if ref == "" {
		t.Fatal("missing form reference")
	}
	// Logical targeting alone does not bring a page to the front in Chrome.
	// Explicitly activate A, then observe the physically background page B.
	run("agent", "switch-tab", map[string]any{"tab": first.ID})
	visibility := func(tab string) string {
		t.Helper()
		var result struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		if err := m.Call(ctx, sid, tab, "Runtime.evaluate", map[string]any{"expression": "document.visibilityState + ':' + document.hasFocus()", "returnByValue": true}, &result); err != nil {
			t.Fatal(err)
		}
		return result.Result.Value
	}
	firstVisibility, secondVisibility := visibility(first.ID), visibility(second.ID)
	if firstVisibility != "visible:true" || secondVisibility != "hidden:false" {
		t.Fatalf("fixture did not activate A above background B: %s / %s", firstVisibility, secondVisibility)
	}
	before, _ := e.Control(sid, "")
	backgroundCtx, cancelBackground := context.WithTimeout(ctx, 3*time.Second)
	backgroundStarted := time.Now()
	view, err := e.Observe(backgroundCtx, sid, "screenshot", map[string]any{"tab": second.ID, "save": true})
	cancelBackground()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Physically background B captured in %s; A=%s, B=%s", time.Since(backgroundStarted), firstVisibility, secondVisibility)
	if view.(map[string]any)["tab"] != second.ID || view.(map[string]any)["artifact"] == nil {
		t.Fatal(view)
	}
	pixels, err := base64.StdEncoding.DecodeString(view.(map[string]any)["data"].(string))
	if err != nil {
		t.Fatal(err)
	}
	bitmap, err := png.Decode(bytes.NewReader(pixels))
	if err != nil {
		t.Fatal(err)
	}
	red, green, blue, _ := bitmap.At(0, 0).RGBA()
	if red != 0 || green != 0 || blue != 65535 {
		t.Fatalf("background screenshot captured wrong page: RGB=%d,%d,%d", red, green, blue)
	}
	if visibility(first.ID) != firstVisibility || visibility(second.ID) != secondVisibility {
		t.Fatal("background observation changed Chrome visibility/focus")
	}
	after, _ := e.Control(sid, "")
	if before != after {
		t.Fatalf("view changed selection/control: %v -> %v", before, after)
	}
	observed, err := e.Observe(ctx, sid, "tabs", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tab := range observed.([]Target) {
		if tab.Active != (tab.ID == first.ID) {
			t.Fatal(tab)
		}
	}
	if _, err = e.Run(ctx, sid, "agent", "fill", map[string]any{"tab": second.ID, "target": ref, "text": "wrong"}); err == nil || !strings.Contains(err.Error(), "target_tab_mismatch") {
		t.Fatalf("cross-tab snapshot ref: %v", err)
	}
	run("agent", "fill", map[string]any{"target": ref, "text": "first value"})
	run("agent", "fill", map[string]any{"tab": second.ID, "target": "css:#name", "text": "second value"})
	for _, test := range []struct{ tab, value string }{{first.ID, "first value"}, {second.ID, "second value"}} {
		value, err := e.Observe(ctx, sid, "element-info", map[string]any{"tab": test.tab, "target": "css:#name"})
		if err != nil {
			t.Fatal(err)
		}
		if value.(map[string]any)["value"] != test.value {
			t.Fatal(value)
		}
	}
	run("agent", "navigate", map[string]any{"tab": second.ID, "url": "about:blank"})
	run("agent", "fill", map[string]any{"tab": first.ID, "target": ref, "text": "first still live"})
	if _, err = e.Run(ctx, sid, "agent", "fill", map[string]any{"tab": second.ID, "target": ref, "text": "wrong again"}); err == nil {
		t.Fatal("reference crossed tab after navigation")
	}

	// A long awaited CDP evaluation must not hold attachMu or the action queue
	// needed by screenshots of another owned page. Observation preserves the
	// original cancellable agent lease even while that evaluation is pending.
	done := make(chan error, 1)
	go func() {
		_, err := e.Run(ctx, sid, "agent", "evaluate-js", map[string]any{"tab": first.ID, "expression": "new Promise(resolve => setTimeout(() => resolve('done'), 30000))"})
		done <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		current, _ := e.Control(sid, "")
		if current.Active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("agent evaluation did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	before, _ = e.Control(sid, "")
	viewCtx, stopView := context.WithTimeout(ctx, 3*time.Second)
	_, err = e.Observe(viewCtx, sid, "screenshot", map[string]any{"tab": second.ID})
	stopView()
	if err != nil {
		t.Fatalf("view queued behind pending agent evaluation: %v", err)
	}
	after, _ = e.Control(sid, "")
	if before != after || !after.Active {
		t.Fatalf("observation changed pending lease: %v -> %v", before, after)
	}
	e.Control(sid, "human")
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("takeover cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("view replaced the original cancellation lease")
	}
}
