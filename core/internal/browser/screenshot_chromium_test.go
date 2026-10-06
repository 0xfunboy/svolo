package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/png"
	"os"
	"testing"
	"time"
)

func TestChromiumAgedBackgroundObservation(t *testing.T) {
	if os.Getenv("SVOLO_E2E") != "1" {
		t.Skip("set SVOLO_E2E=1 to run real Chromium background screenshot acceptance")
	}
	m := NewManaged(ManagedOptions{Root: t.TempDir(), Headless: true})
	defer m.Close()
	e := NewEngine(m, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const sid = "background-observation"
	first, err := m.Create(ctx, sid, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	setDocument := func(tab, color string) {
		t.Helper()
		if err := m.Call(ctx, sid, tab, "Runtime.evaluate", map[string]any{"expression": `document.body.innerHTML='<style>html,body{background:` + color + `;margin:0}</style><h1>Static form</h1><input name="fullname">'; true`, "returnByValue": true}, nil); err != nil {
			t.Fatal(err)
		}
	}
	setDocument(first.ID, "red")
	second, err := m.Create(ctx, sid, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	setDocument(second.ID, "blue")
	if _, err = e.Control(sid, "agent"); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Run(ctx, sid, "agent", "switch-tab", map[string]any{"tab": first.ID}); err != nil {
		t.Fatal(err)
	}
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
	if visibility(first.ID) != "visible:true" || visibility(second.ID) != "hidden:false" {
		t.Fatal("fixture did not place A in front of hidden B")
	}
	before, _ := e.Control(sid, "")
	// A freshly rendered hidden page is insufficient to reproduce Chromium's
	// legacy ForceRedraw stall. Let this static page become idle first.
	select {
	case <-time.After(6 * time.Second):
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for i := 0; i < 6; i++ {
		viewCtx, stop := context.WithTimeout(ctx, 2*time.Second)
		started := time.Now()
		view, err := e.Observe(viewCtx, sid, "screenshot", map[string]any{"tab": second.ID})
		stop()
		if err != nil {
			t.Fatalf("aged hidden capture %d stalled or failed: %v", i, err)
		}
		shot := view.(map[string]any)
		if shot["tab"] != second.ID {
			t.Fatal("background observation returned another tab")
		}
		pixels, err := base64.StdEncoding.DecodeString(shot["data"].(string))
		if err != nil {
			t.Fatal(err)
		}
		bitmap, err := png.Decode(bytes.NewReader(pixels))
		if err != nil {
			t.Fatal(err)
		}
		red, green, blue, _ := bitmap.At(0, 0).RGBA()
		if red != 0 || green != 0 || blue != 65535 {
			t.Fatalf("capture %d returned wrong page pixels: RGB=%d,%d,%d", i, red, green, blue)
		}
		if visibility(first.ID) != "visible:true" || visibility(second.ID) != "hidden:false" {
			t.Fatal("background observation changed physical tab focus or visibility")
		}
		after, _ := e.Control(sid, "")
		if after != before {
			t.Fatalf("background observation changed control: %v -> %v", before, after)
		}
		t.Logf("Aged hidden capture %d completed in %s with correct B pixels and unchanged focus/control", i, time.Since(started))
		select {
		case <-time.After(300 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
