package browser

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestChromiumViewportAndPasswordFollowup(t *testing.T) {
	if os.Getenv("SVOLO_E2E") != "1" {
		t.Skip("set SVOLO_E2E=1 for real Chromium viewport/fixture-form acceptance")
	}
	m := NewManaged(ManagedOptions{Root: t.TempDir(), Headless: true})
	defer m.Close()
	e := NewEngine(m, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	const sid = "viewport-form-fixture"
	form, err := m.Create(ctx, sid, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Call(ctx, sid, form.ID, "Runtime.evaluate", map[string]any{"expression": `document.body.innerHTML='<label>Name<input name="name"></label><label>Password<input name="password" type="password"></label>'; true`, "returnByValue": true}, nil); err != nil {
		t.Fatal(err)
	}
	other, err := m.Create(ctx, sid, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	e.Control(sid, "agent")
	if _, err = e.Run(ctx, sid, "agent", "switch-tab", map[string]any{"tab": form.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Run(ctx, sid, "agent", "fill", map[string]any{"tab": form.ID, "target": "css:input[name=name]", "text": "Fixture User"}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := e.Run(ctx, sid, "agent", "snapshot-interactive", map[string]any{"tab": form.ID})
	if err != nil {
		t.Fatal(err)
	}
	doc := snapshot.(map[string]any)["document"].(string)
	before, _ := e.Control(sid, "")
	if _, err = e.ConfigureViewport(ctx, sid, ViewportSettings{Tab: form.ID, Width: 1920, Height: 1080, DPR: 1}); err != nil {
		t.Fatal(err)
	}
	after, _ := e.Control(sid, "")
	if before != after {
		t.Fatal("resize changed real agent lease", before, after)
	}
	if _, err = e.Run(ctx, sid, "agent", "fill", map[string]any{"tab": form.ID, "target": "@" + doc + ":1", "text": "wrong"}); err == nil || !strings.Contains(err.Error(), "target_stale") {
		t.Fatal("pre-resize reference remained usable", err)
	}
	if _, err = e.Run(ctx, sid, "agent", "snapshot-interactive", map[string]any{"tab": form.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Run(ctx, sid, "agent", "fill", map[string]any{"tab": form.ID, "target": "css:input[name=password]", "text": "not-a-real-credential"}); err != nil {
		t.Fatal("credential follow-up failed", err)
	}
	value, err := e.evaluate(ctx, sid, form.ID, `({width:innerWidth,height:innerHeight,namePreserved:document.querySelector('[name=name]').value==='Fixture User',passwordFilled:document.querySelector('[name=password]').value==='not-a-real-credential',focused:document.hasFocus()})`, true)
	if err != nil {
		t.Fatal(err)
	}
	data := value.(map[string]any)
	if data["width"] != float64(1920) || data["height"] != float64(1080) || data["namePreserved"] != true || data["passwordFilled"] != true || data["focused"] != true {
		t.Fatal("resizing lost form state or geometry", data)
	}
	if _, err = e.ConfigureViewport(ctx, sid, ViewportSettings{Tab: other.ID, Width: 1080, Height: 1920, DPR: 1, Mobile: true, Touch: true}); err != nil {
		t.Fatal(err)
	}
	after, _ = e.Control(sid, "")
	if after != before {
		t.Fatal("background resize retargeted real agent", before, after)
	}
	value, err = e.evaluate(ctx, sid, other.ID, `document.visibilityState+':'+document.hasFocus()`, true)
	if err != nil || value != "hidden:false" {
		t.Fatal("background resize changed tab focus", value, err)
	}
	shot, err := e.Observe(ctx, sid, "screenshot", map[string]any{"tab": form.ID})
	if err != nil {
		t.Fatal(err)
	}
	viewport := shot.(map[string]any)["viewport"].(map[string]any)
	if number(viewport, "width", 0) != 1920 || number(viewport, "height", 0) != 1080 {
		t.Fatal("new geometry missing from capture", viewport)
	}
}
