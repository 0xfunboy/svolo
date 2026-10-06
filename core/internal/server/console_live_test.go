package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"svolo.local/core/internal/browser"
	"testing"
	"time"
)

// The real shared GUI drives a real HTTP daemon through a test-only CDP binding.
// It is not an Electron build. No admin bearer is injected into page JavaScript.
func TestSharedConsoleAgainstRealCore(t *testing.T) {
	s, h, ctx := realServer(t)
	target := document(t, s, ctx, `<!doctype html><title>Svolo live core acceptance</title><div id="app"></div>`)
	managed := s.Engine.Transport.(*browser.Managed)
	client, e := managed.Client(ctx, "one")
	if e != nil {
		t.Fatal(e)
	}
	events, stop := client.Subscribe(128)
	defer stop()
	e = managed.Call(ctx, "one", target, "Runtime.addBinding", map[string]any{"name": "_svoloFixtureIPC"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	go func() {
		for event := range events {
			if event.Method != "Runtime.bindingCalled" {
				continue
			}
			var ev struct {
				Name               string
				Payload            string
				ExecutionContextID int
			}
			if json.Unmarshal(event.Params, &ev) != nil || ev.Name != "_svoloFixtureIPC" {
				continue
			}
			var q struct {
				ID     int             `json:"id"`
				Path   string          `json:"path"`
				Method string          `json:"method"`
				Body   json.RawMessage `json:"body"`
			}
			if json.Unmarshal([]byte(ev.Payload), &q) != nil {
				continue
			}
			func() {
				var value any
				var failure string
				if !strings.HasPrefix(q.Path, "/v1/") {
					failure = "test broker rejects external endpoints"
				} else {
					r, e := http.NewRequestWithContext(ctx, q.Method, h.URL+q.Path, bytes.NewReader(q.Body))
					if e != nil {
						failure = e.Error()
					} else {
						r.Header.Set("Authorization", "Bearer "+s.Token)
						r.Header.Set("Content-Type", "application/json")
						rr, e := h.Client().Do(r)
						if e != nil {
							failure = e.Error()
						} else {
							b, _ := io.ReadAll(rr.Body)
							rr.Body.Close()
							if e = json.Unmarshal(b, &value); e != nil {
								failure = e.Error()
							} else if rr.StatusCode != 200 {
								failure = fmt.Sprintf("HTTP %d: %s", rr.StatusCode, b)
							}
						}
					}
				}
				v, _ := json.Marshal(value)
				f, _ := json.Marshal(failure)
				expression := fmt.Sprintf("window._svoloResolve(%d,%s,%s)", q.ID, v, f)
				_ = client.Call(ctx, event.SessionID, "Runtime.evaluate", map[string]any{"expression": expression, "contextId": ev.ExecutionContextID}, nil)
			}()
		}
	}()
	evaluate := func(expression string) json.RawMessage {
		t.Helper()
		var result struct {
			Result struct {
				Value json.RawMessage `json:"value"`
			}
			ExceptionDetails json.RawMessage `json:"exceptionDetails"`
		}
		e := managed.Call(ctx, "one", target, "Runtime.evaluate", map[string]any{"expression": expression, "awaitPromise": true, "returnByValue": true}, &result)
		if e != nil || len(result.ExceptionDetails) > 0 {
			t.Fatal(expression, e, string(result.ExceptionDetails))
		}
		return result.Result.Value
	}
	wait := func(expression string) {
		t.Helper()
		deadline := time.Now().Add(6 * time.Second)
		for time.Now().Before(deadline) {
			var ok bool
			_ = json.Unmarshal(evaluate(expression), &ok)
			if ok {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatal("UI condition:", expression, string(evaluate("document.body.innerText")))
	}
	js, _ := webFiles.ReadFile("web/client.js")
	css, _ := webFiles.ReadFile("web/style.css")
	style, _ := json.Marshal(string(css))
	script := strings.Replace(string(js), "export function mountCoreConsole", "function mountCoreConsole", 1)
	script += `\n;const style=document.createElement('style');style.textContent=` + string(style) + `;document.head.append(style);window._svoloPending=new Map();window._svoloSeq=0;window._svoloResolve=(id,value,error)=>{const p=_svoloPending.get(id);if(p){_svoloPending.delete(id);error?p.reject(Error(error)):p.resolve(value);}};
 window._svoloTransport=(path,method='GET',body)=>new Promise((resolve,reject)=>{const id=++_svoloSeq;_svoloPending.set(id,{resolve,reject});_svoloFixtureIPC(JSON.stringify({id,path,method,body}));});
 window._cleanup=mountCoreConsole(document.querySelector('#app'),_svoloTransport,{initialSession:'one'});true;`
	evaluate(strings.ReplaceAll(script, `\n;`, "\n;"))
	wait(fmt.Sprintf(`document.querySelector('.ac-health').textContent.includes(%q)`, Version))
	evaluate(`document.querySelector('[data-page="security"]').click();true`)
	wait(`document.querySelector('.ac-vault-status').textContent.includes('"unlocked": false')`)
	evaluate(`document.querySelector('.ac-vault-key').value='` + strings.Repeat("36", 32) + `';document.querySelector('.ac-vault-unlock').click();true`)
	wait(`document.querySelector('.ac-vault-status').textContent.includes('"unlocked": true')`)
	evaluate(`document.querySelector('.ac-secret-ref').value='ui-fixture';document.querySelector('.ac-secret-value').value='fixture-secret-from-ui';document.querySelector('.ac-secret-save').click();true`)
	wait(`document.querySelector('.ac-secret-list').textContent.includes('ui-fixture')`)
	secret, e := s.Vault.Get("ui-fixture")
	if e != nil || secret != "fixture-secret-from-ui" {
		t.Fatal("GUI did not reach real vault", e)
	}
	evaluate(`document.querySelector('.ac-vault-lock').click();true`)
	wait(`document.querySelector('.ac-vault-status').textContent.includes('"unlocked": false')`)
	if s.Vault.Unlocked() {
		t.Fatal("vault remained unlocked")
	}
	evaluate(`document.querySelector('[data-page="hosts"]').click();true`)
	evaluate(`document.querySelector('.ac-host-id').value='fixture';document.querySelector('.ac-host-alias').value='fixture-alias';document.querySelector('.ac-host-port').value='7441';document.querySelector('.ac-host-add').click();true`)
	wait(`document.querySelector('.ac-host-list').textContent.includes('fixture-alias')`)
	if len(s.Config.Get().Hosts) != 1 || s.Config.Get().Hosts[0].Alias != "fixture-alias" {
		t.Fatal("GUI host configuration not persisted")
	}
	var screenshot struct {
		Data string `json:"data"`
	}
	if e = managed.Call(ctx, "one", target, "Page.captureScreenshot", map[string]any{"format": "png"}, &screenshot); e != nil {
		t.Fatal(e)
	}
	if out := os.Getenv("SVOLO_EVIDENCE_DIR"); out != "" {
		b, _ := base64.StdEncoding.DecodeString(screenshot.Data)
		os.MkdirAll(out, 0755)
		if e = os.WriteFile(filepath.Join(out, "console-live-core.png"), b, 0644); e != nil {
			t.Fatal(e)
		}
	}
	evaluate(`window._cleanup();true`)
}
