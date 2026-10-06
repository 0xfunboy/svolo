package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"svolo.local/core/internal/agent"
	"svolo.local/core/internal/browser"
)

func realServer(t *testing.T) (*Server, *httptest.Server, context.Context) {
	t.Helper()
	if os.Getenv("SVOLO_E2E") != "1" {
		t.Skip("set SVOLO_E2E=1 for actual Chromium acceptance; fixtures do not qualify external navigation")
	}
	s, e := New(Options{Data: t.TempDir(), Browser: "managed", Headless: true, NoSandboxForTest: os.Getenv("SVOLO_TEST_NO_SANDBOX") == "1"})
	if e != nil {
		t.Fatal(e)
	}
	h := httptest.NewServer(s)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(func() { cancel(); h.Close(); s.Close() })
	c := s.Config.Get()
	c.Sessions = []Session{{ID: "one", Workspace: t.TempDir()}}
	if e = s.Config.Set(c); e != nil {
		t.Fatal(e)
	}
	return s, h, ctx
}
func document(t *testing.T, s *Server, ctx context.Context, html string) string {
	t.Helper()
	trans := s.Engine.Transport
	target, e := trans.Create(ctx, "one", "about:blank")
	if e != nil {
		t.Fatal(e)
	}
	var tree struct {
		FrameTree struct {
			Frame struct {
				ID string `json:"id"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	if e = trans.Call(ctx, "one", target.ID, "Page.getFrameTree", map[string]any{}, &tree); e != nil {
		t.Fatal(e)
	}
	if e = trans.Call(ctx, "one", target.ID, "Page.setDocumentContent", map[string]any{"frameId": tree.FrameTree.Frame.ID, "html": html}, nil); e != nil {
		t.Fatal(e)
	}
	if e = trans.Activate(ctx, "one", target.ID); e != nil {
		t.Fatal(e)
	}
	_, e = s.Engine.Run(ctx, "one", "human", "switch-tab", map[string]any{"query": target.ID})
	if e != nil {
		t.Fatal(e)
	}
	return target.ID
}
func TestServerBrowserAgentAcceptance(t *testing.T) {
	s, h, ctx := realServer(t)
	tabID := document(t, s, ctx, `<!doctype html><title>Integration fixture</title><main><h1>Real browser / simulated model</h1><input id="name" aria-label="Name"><input type="password" id="password" value="fixture-secret"><button id="go" onclick="document.querySelector('#result').textContent='Done: '+document.querySelector('#name').value">Submit</button><p id="result">Pending</p></main>`)
	t.Run("RedactPasswordHTML", func(t *testing.T) {
		v, e := s.Engine.Run(ctx, "one", "human", "element-info", map[string]any{"target": "css:#password"})
		raw, _ := json.Marshal(v)
		if e != nil || strings.Contains(string(raw), "fixture-secret") {
			t.Fatal(string(raw), e)
		}
	})
	var calls atomic.Int32
	toolCall := func(id, name string, arguments map[string]any) map[string]any {
		arguments["tab"] = tabID
		encoded, _ := json.Marshal(arguments)
		return map[string]any{"type": "function_call", "call_id": id, "name": name, "arguments": string(encoded)}
	}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		w.Header().Set("Content-Type", "application/json")
		switch calls.Add(1) {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "completed", "output": []any{toolCall("fill", "fill", map[string]any{"target": "css:#name", "text": "Luca"}), toolCall("click", "click", map[string]any{"target": "css:#go"})}})
		case 2:
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "completed", "output": []any{toolCall("check", "assert-text", map[string]any{"target": "css:#result", "text": "Done: Luca", "match": "exact"}), toolCall("image", "screenshot", map[string]any{})}})
		default:
			raw, _ := json.Marshal(p)
			if !strings.Contains(string(raw), "input_image") {
				t.Error("missing vision screenshot observation")
			}
			fmt.Fprint(w, `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Fixture form completed and assertion verified."}]}]}`)
		}
	}))
	defer model.Close()
	c := s.Config.Get()
	c.Providers = []agent.Provider{{ID: "fixture", Kind: "responses", BaseURL: model.URL, Model: "mock-vision", Vision: true, MaxOutputTokens: 1024}}
	if e := s.Config.Set(c); e != nil {
		t.Fatal(e)
	}
	t.Run("HTTPAgentBrowserAndVisionLoop", func(t *testing.T) {
		code, v, _ := request(t, s, h, "POST", "/v1/runs", s.Token, agent.RunRequest{Session: "one", Provider: "fixture", Prompt: "Fill and verify", Autonomy: "browser", MaxSteps: 4}, nil)
		if code != 200 {
			t.Fatal(code, v)
		}
		deadline := time.Now().Add(20 * time.Second)
		for s.Agents.Busy("one") && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		runs := s.Agents.List()
		if len(runs) != 1 || runs[0].Status != "completed" || calls.Load() != 3 {
			t.Fatal(runs, calls.Load())
		}
	})
	_, _ = s.Engine.Control("one", "human")
	t.Run("TakeoverInvalidatesSnapshotRefs", func(t *testing.T) {
		v, e := s.Engine.Run(ctx, "one", "human", "snapshot-interactive", nil)
		if e != nil {
			t.Fatal(e)
		}
		ref := v.(map[string]any)["elements"].([]any)[0].(map[string]any)["ref"].(string)
		_, _ = s.Engine.Control("one", "agent")
		_, e = s.Engine.Run(ctx, "one", "agent", "click", map[string]any{"target": ref})
		if e == nil {
			t.Fatal("pre-takeover ref accepted")
		}
		_, _ = s.Engine.Control("one", "human")
	})
	t.Run("SampledMP4Artifact", func(t *testing.T) {
		_, e := s.doTool(ctx, "one", "human", "record-browser", map[string]any{"action": "start", "mode": "continuous", "intervalMs": 200}, 0)
		if e != nil {
			t.Fatal(e)
		}
		time.Sleep(850 * time.Millisecond)
		v, e := s.doTool(ctx, "one", "human", "record-browser", map[string]any{"action": "stop"}, 0)
		if e != nil {
			t.Fatal(v, e)
		}
		raw, _ := json.Marshal(v)
		if !strings.Contains(string(raw), "video/mp4") {
			t.Fatal(string(raw))
		}
		list, e := browser.ListArtifacts(s.Store.Root, "one")
		if e != nil || len(list) == 0 {
			t.Fatal(e, list)
		}
		for _, a := range list {
			if _, e = browser.VerifyArtifact(s.Store.Root, "one", a.ID); e != nil {
				t.Fatal(e)
			}
		}
	})
}
func TestSharedConsoleRendering(t *testing.T) {
	s, _, ctx := realServer(t)
	document(t, s, ctx, `<!doctype html><title>svolo Agent Core — synthetic UI smoke</title><div id="app"></div>`)
	js, e := webFiles.ReadFile("web/client.js")
	if e != nil {
		t.Fatal(e)
	}
	css, e := webFiles.ReadFile("web/style.css")
	if e != nil {
		t.Fatal(e)
	}
	cssJSON, _ := json.Marshal(string(css))
	script := strings.Replace(string(js), "export function mountCoreConsole", "function mountCoreConsole", 1)
	script += `\n;const style=document.createElement('style');style.textContent=` + string(cssJSON) + `;document.head.append(style);document.body.style.margin='0';document.body.style.background='#0c1119';
 const config={version:1,providers:[{id:'mock-provider',model:'UI fixture only'}],sessions:[{id:'one',name:'Local integration workspace',workspace:'/fixture/workspace'}],hosts:[],mcpServers:[]};
 window.fixtureTransport=async(path,method,body)=>{
  if(path==='/v1/health')return {version:'0.3.1-dev',productionQualified:false,browser:'managed'};
  if(path==='/v1/config')return config;
  if(path==='/v1/tools')return [{name:'snapshot-interactive',description:'Inspect the actual page',inputSchema:{type:'object',properties:{}}}];
  if(path.startsWith('/v1/control'))return {owner:'human',epoch:1};
  if(path==='/v1/runs')return [{id:'render-fixture',session:'one',status:'completed',provider:'mock-provider',text:'Synthetic UI smoke test. The shared console script is rendered by real Chromium. This is not an Electron build or a live remote-host test.',steps:3,started:'2026-10-05T00:00:00Z'}];
  if(path==='/v1/sessions'&&method==='POST'){config.sessions.push(body);return body;}
  if(path.startsWith('/v1/events')||path==='/v1/approvals'||path==='/v1/hosts/status'||path.startsWith('/v1/artifacts')||path==='/v1/tokens')return [];
  throw new Error('Unmocked UI endpoint '+path);
 };
 window.cleanup=mountCoreConsole(document.querySelector('#app'),window.fixtureTransport,{initialSession:'one'});true;`
	script = strings.ReplaceAll(script, `\n;`, "\n;")
	if _, e = s.Engine.Run(ctx, "one", "human", "evaluate-js", map[string]any{"expression": script}); e != nil {
		t.Fatal(e)
	}
	time.Sleep(250 * time.Millisecond)
	v, e := s.Engine.Run(ctx, "one", "human", "evaluate-js", map[string]any{"expression": "document.querySelectorAll('.ac-nav button').length"})
	if e != nil || v != float64(10) {
		t.Fatal(v, e)
	}
	_, e = s.Engine.Run(ctx, "one", "human", "evaluate-js", map[string]any{"expression": "document.querySelector('.ac-new').click(); !document.querySelector('.ac-session-editor').hidden"})
	if e != nil {
		t.Fatal(e)
	}
	_, _ = s.Engine.Run(ctx, "one", "human", "evaluate-js", map[string]any{"expression": "document.querySelector('.ac-session-cancel').click();true"})
	v, e = s.Engine.Run(ctx, "one", "human", "screenshot", nil)
	if e != nil {
		t.Fatal(e)
	}
	if out := os.Getenv("SVOLO_EVIDENCE_DIR"); out != "" {
		bytes, e := base64.StdEncoding.DecodeString(v.(map[string]any)["data"].(string))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.MkdirAll(out, 0755); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(out, "console-smoke.png"), bytes, 0644); e != nil {
			t.Fatal(e)
		}
	}
}
