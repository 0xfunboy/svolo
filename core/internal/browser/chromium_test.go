package browser

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestChromiumAcceptance(t *testing.T) {
	if os.Getenv("SVOLO_E2E") != "1" {
		t.Skip("set SVOLO_E2E=1 to run real Chromium acceptance")
	}
	const fixture = `<!doctype html><title>Fixture</title><main><h1>Agent browser fixture</h1><label>Name <input id="name" aria-label="Name"></label><button id="go" onclick="document.querySelector('#result').textContent='Done: '+document.querySelector('#name').value">Submit</button><p id="result">Pending</p><input id="check" type="checkbox"><select id="select"><option value="a">Alpha</option><option value="b">Beta</option></select><a id="next" href="/next">Next</a><input type="file" id="file"><div id="shadow"></div><img id="pic" width="1" height="1" src="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a+WQAAAAASUVORK5CYII="><script>document.querySelector('#shadow').attachShadow({mode:'open'}).innerHTML='<button id="inside">Shadow button</button>'</script></main>`
	m := NewManaged(ManagedOptions{Root: t.TempDir(), Headless: true, NoSandboxForTest: os.Getenv("SVOLO_TEST_NO_SANDBOX") == "1"})
	defer m.Close()
	e := NewEngine(m, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	run := func(t *testing.T, name string, a map[string]any) any {
		t.Helper()
		v, er := e.Run(ctx, "acceptance", "human", name, a)
		if er != nil {
			t.Fatalf("%s: %v", name, er)
		}
		return v
	}
	t.Run("Navigate", func(t *testing.T) {
		run(t, "navigate", map[string]any{"url": "about:blank"})
		tabs, err := m.List(ctx, "acceptance")
		if err != nil || len(tabs) == 0 {
			t.Fatal(err)
		}
		var tree struct {
			FrameTree struct {
				Frame struct {
					ID string `json:"id"`
				} `json:"frame"`
			} `json:"frameTree"`
		}
		if err = m.Call(ctx, "acceptance", tabs[0].ID, "Page.getFrameTree", map[string]any{}, &tree); err != nil {
			t.Fatal(err)
		}
		if err = m.Call(ctx, "acceptance", tabs[0].ID, "Page.setDocumentContent", map[string]any{"frameId": tree.FrameTree.Frame.ID, "html": fixture}, nil); err != nil {
			t.Fatal(err)
		}
		t.Log("Synthetic local document: this does not qualify external navigation, which environment policy blocks.")
		run(t, "wait-for", map[string]any{"condition": "visible", "value": "css:#go", "seconds": 10})
	})
	var ref string
	t.Run("SemanticSnapshot", func(t *testing.T) {
		v := run(t, "snapshot-interactive", nil).(map[string]any)
		for _, raw := range v["elements"].([]any) {
			n := raw.(map[string]any)
			if n["label"] == "Name" {
				ref = n["ref"].(string)
			}
		}
		if ref == "" {
			t.Fatal("missing Name ref")
		}
	})
	t.Run("FillAndClick", func(t *testing.T) {
		run(t, "fill", map[string]any{"target": ref, "text": "Luca"})
		run(t, "click", map[string]any{"target": "css:#go"})
		run(t, "assert-text", map[string]any{"target": "css:#result", "text": "Done: Luca", "match": "exact"})
	})
	t.Run("CheckboxAndSelect", func(t *testing.T) {
		run(t, "check", map[string]any{"target": "css:#check"})
		run(t, "select", map[string]any{"target": "css:#select", "value": "b"})
		v := run(t, "element-info", map[string]any{"target": "css:#select"}).(map[string]any)
		if v["value"] != "b" {
			t.Fatal(v)
		}
	})
	t.Run("ShadowDOM", func(t *testing.T) { run(t, "assert-visible", map[string]any{"target": "css:#inside"}) })
	t.Run("ImageReady", func(t *testing.T) { run(t, "assert-image-ready", map[string]any{"target": "css:#pic"}) })
	t.Run("ScreenshotArtifact", func(t *testing.T) {
		v := run(t, "screenshot", map[string]any{"save": true}).(map[string]any)
		a := v["artifact"].(Artifact)
		if a.Width < 1 || a.Height < 1 {
			t.Fatal(a)
		}
		if _, err := VerifyArtifact(e.Data, "acceptance", a.ID); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Accessibility", func(t *testing.T) {
		v := run(t, "accessibility-tree", nil)
		if len(v.([]map[string]any)) < 3 {
			t.Fatal(v)
		}
	})
	t.Run("Upload", func(t *testing.T) {
		f, er := os.CreateTemp(t.TempDir(), "upload-*.txt")
		if er != nil {
			t.Fatal(er)
		}
		f.WriteString("uploaded")
		f.Close()
		run(t, "upload", map[string]any{"target": "css:#file", "file": f.Name()})
		v := run(t, "evaluate-js", map[string]any{"expression": "document.querySelector('#file').files.length"})
		if v != float64(1) {
			t.Fatal(v)
		}
	})
	t.Run("StaleReference", func(t *testing.T) {
		run(t, "snapshot-interactive", nil)
		if _, er := e.Run(ctx, "acceptance", "human", "click", map[string]any{"target": ref}); er == nil || !strings.Contains(er.Error(), "target_stale") {
			t.Fatal(er)
		}
	})
	t.Run("ConsoleEventTransport", func(t *testing.T) {
		run(t, "inspect-network", map[string]any{"action": "start"})
		run(t, "evaluate-js", map[string]any{"expression": "console.log('event-test');true"})
		time.Sleep(100 * time.Millisecond)
		v := run(t, "inspect-network", map[string]any{"action": "show"}).(map[string]any)
		if len(v["entries"].([]map[string]any)) == 0 {
			t.Fatal("no network events")
		}
		run(t, "inspect-network", map[string]any{"action": "stop"})
	})
	t.Run("TakeControl", func(t *testing.T) {
		e.Control("acceptance", "agent")
		if _, er := e.Run(ctx, "acceptance", "agent", "read-page", nil); er != nil {
			t.Fatal(er)
		}
		e.Control("acceptance", "human")
		if _, er := e.Run(ctx, "acceptance", "agent", "click", map[string]any{"target": "css:#go"}); er == nil {
			t.Fatal("agent bypassed human control")
		}
	})
	t.Run("IsolatedSession", func(t *testing.T) {
		v, er := e.Run(ctx, "second", "human", "state", nil)
		if er != nil {
			t.Fatal(er)
		}
		if v.(map[string]any)["url"] != "about:blank" {
			t.Fatal(v)
		}
	})
	t.Run("Tabs", func(t *testing.T) {
		run(t, "open-tab", map[string]any{"url": "about:blank"})
		run(t, "evaluate-js", map[string]any{"expression": "document.title='Next page'"})
		run(t, "wait-for", map[string]any{"condition": "title", "value": "Next page"})
		run(t, "assert-title", map[string]any{"expected": "Next page"})
		v := run(t, "tabs", nil).([]Target)
		if len(v) < 2 {
			t.Fatal(v)
		}
		run(t, "close-tab", nil)
		run(t, "assert-title", map[string]any{"expected": "Fixture"})
	})
}
