package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"svolo.local/core/internal/agent"
	"svolo.local/core/internal/browser"
)

type inertBrowser struct{}

func (inertBrowser) List(context.Context, string) ([]browser.Target, error) {
	return []browser.Target{{ID: "fixture", Type: "page", URL: "about:blank", Title: "fixture"}}, nil
}
func (inertBrowser) Create(context.Context, string, string) (browser.Target, error) {
	return browser.Target{ID: "fixture", Type: "page"}, nil
}
func (inertBrowser) Call(context.Context, string, string, string, map[string]any, any) error {
	return fmt.Errorf("test does not simulate CDP")
}
func (inertBrowser) Activate(context.Context, string, string) error { return nil }
func (inertBrowser) CloseTab(context.Context, string, string) error { return nil }
func (inertBrowser) CloseSession(string) error                      { return nil }
func (inertBrowser) Close() error                                   { return nil }

func setup(t *testing.T) (*Server, *httptest.Server, string) {
	t.Helper()
	root := t.TempDir()
	s, e := New(Options{Data: filepath.Join(root, "private"), Browser: "managed", Transport: inertBrowser{}})
	if e != nil {
		t.Fatal(e)
	}
	work := filepath.Join(root, "workspace")
	if e = os.MkdirAll(work, 0700); e != nil {
		t.Fatal(e)
	}
	c := s.Config.Get()
	c.Sessions = []Session{{ID: "one", Workspace: work}, {ID: "two", Workspace: t.TempDir()}}
	if e = s.Config.Set(c); e != nil {
		t.Fatal(e)
	}
	h := httptest.NewServer(s)
	t.Cleanup(func() { h.Close(); s.Close() })
	return s, h, work
}
func request(t *testing.T, s *Server, h *httptest.Server, method, path, token string, body any, headers map[string]string) (int, map[string]any, http.Header) {
	t.Helper()
	var data []byte
	var e error
	if body != nil {
		data, e = json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
	}
	r, e := http.NewRequest(method, h.URL+path, bytes.NewReader(data))
	if e != nil {
		t.Fatal(e)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		if k == "Host" {
			r.Host = v
		} else {
			r.Header.Set(k, v)
		}
	}
	rr, e := h.Client().Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer rr.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&out)
	return rr.StatusCode, out, rr.Header
}
func TestControlPlaneSecurity(t *testing.T) {
	s, h, _ := setup(t)
	for _, tc := range []struct {
		name, path, token string
		headers           map[string]string
		want              int
	}{
		{"anonymous", "/v1/health", "", nil, 401}, {"bad-bearer", "/v1/health", "incorrect", nil, 401},
		{"admin", "/v1/health", s.Token, nil, 200},
		{"rebind-host", "/v1/health", s.Token, map[string]string{"Host": "attacker.invalid"}, 403},
		{"cross-origin", "/v1/health", s.Token, map[string]string{"Origin": "https://attacker.invalid"}, 403},
		{"null-origin", "/v1/health", s.Token, map[string]string{"Origin": "null"}, 403},
		{"same-origin", "/v1/health", s.Token, map[string]string{"Origin": h.URL}, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, v, headers := request(t, s, h, "GET", tc.path, tc.token, nil, tc.headers)
			if code != tc.want {
				t.Fatal(code, v)
			}
			if headers.Get("Cache-Control") != "no-store" {
				t.Fatal("sensitive HTTP caching allowed")
			}
			if code == 200 && v["productionQualified"] != false {
				t.Fatal("unsupported production claim")
			}
		})
	}
	code, v, _ := request(t, s, h, "POST", "/v1/sessions", s.Token, map[string]any{"id": "x", "admin": true}, nil)
	if code != 400 {
		t.Fatal(code, v)
	}
	code, v, _ = request(t, s, h, "POST", "/v1/remote", s.Token, map[string]any{"host": "x", "path": "/v1/bridge/poll", "method": "GET"}, nil)
	if code != 400 {
		t.Fatal(code, v)
	}
}
func TestScopedMCPDoesNotAuthorizeItself(t *testing.T) {
	s, h, work := setup(t)
	if e := os.WriteFile(filepath.Join(work, "hello.txt"), []byte("workspace-one"), 0600); e != nil {
		t.Fatal(e)
	}
	code, minted, _ := request(t, s, h, "POST", "/v1/tokens", s.Token, map[string]any{"session": "one", "label": "fixture"}, nil)
	if code != 200 {
		t.Fatal(code, minted)
	}
	token := minted["token"].(string)
	id := minted["id"].(string)
	for _, path := range []string{"/v1/config", "/v1/tokens", "/v1/approvals", "/v1/remote", "/v1/bridge/poll", "/v1/tools/call"} {
		code, v, _ := request(t, s, h, "GET", path, token, nil, nil)
		if code != 403 {
			t.Fatal(path, code, v)
		}
	}
	init := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "1"}}}
	code, v, headers := request(t, s, h, "POST", "/v1/mcp?session=two", token, init, nil)
	if code != 200 || v["error"] != nil {
		t.Fatal(code, v)
	}
	mid := headers.Get("Mcp-Session-Id")
	if mid == "" {
		t.Fatal("missing MCP connection identity")
	}
	_, _ = s.Engine.Control("one", "agent")
	call := map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "workspace-read", "arguments": map[string]any{"path": "hello.txt"}}}
	code, v, _ = request(t, s, h, "POST", "/v1/mcp?session=two", token, call, map[string]string{"Mcp-Session-Id": mid})
	raw, _ := json.Marshal(v)
	if code != 200 || !strings.Contains(string(raw), "workspace-one") {
		t.Fatal(code, string(raw))
	}
	// Even admin cannot steal a connection established under another credential.
	code, v, _ = request(t, s, h, "POST", "/v1/mcp?session=one", s.Token, call, map[string]string{"Mcp-Session-Id": mid})
	if code != 404 {
		t.Fatal(code, v)
	}
	code, v, _ = request(t, s, h, "DELETE", "/v1/tokens?id="+id, s.Token, nil, nil)
	if code != 200 {
		t.Fatal(code, v)
	}
	code, v, _ = request(t, s, h, "POST", "/v1/mcp", token, call, map[string]string{"Mcp-Session-Id": mid})
	if code != 401 {
		t.Fatal(code, v)
	}
}
func TestWorkspaceAndWrapperBoundaries(t *testing.T) {
	s, h, work := setup(t)
	call := func(name string, args map[string]any, want int) {
		t.Helper()
		code, v, _ := request(t, s, h, "POST", "/v1/tools/call", s.Token, toolRequest{Session: "one", Name: name, Arguments: args}, nil)
		if code != want {
			t.Fatal(name, code, v)
		}
	}
	call("workspace-write", map[string]any{"path": "hello.txt", "content": "written through control plane"}, 200)
	data, e := os.ReadFile(filepath.Join(work, "hello.txt"))
	if e != nil || string(data) != "written through control plane" {
		t.Fatal(string(data), e)
	}
	call("workspace-read", map[string]any{"path": "../private/auth.token"}, 400)
	call("workspace-exec", map[string]any{"program": "echo", "args": []any{"no"}}, 400)
	call("browser-operation", map[string]any{"operation": "workspace-exec", "arguments": map[string]any{"program": "echo"}}, 400)
	call("browser-operation", map[string]any{"operation": "pi-computer", "arguments": map[string]any{}}, 400)
	call("navigate", map[string]any{"url": 7}, 400)
	call("navigate", map[string]any{"url": "about:blank", "surprise": true}, 400)
	call("pi-computer", map[string]any{"arguments": map[string]any{}}, 400)
	// A broad workspace must still not expose daemon credentials.
	c := s.Config.Get()
	c.Sessions[0].Workspace = filepath.Dir(s.Store.Root)
	if e = s.Config.Set(c); e != nil {
		t.Fatal(e)
	}
	call("workspace-read", map[string]any{"path": "private/auth.token"}, 400)
}
func TestObservationDoesNotStealControl(t *testing.T) {
	s, h, _ := setup(t)
	before, e := s.Engine.Control("one", "agent")
	if e != nil {
		t.Fatal(e)
	}
	code, _, _ := request(t, s, h, "GET", "/v1/browser/tabs?session=one", s.Token, nil, nil)
	if code != 200 {
		t.Fatal(code)
	}
	after, _ := s.Engine.Control("one", "")
	if before != after {
		t.Fatal(before, after)
	}
}
func TestHTTPAgentProviderRoundTrip(t *testing.T) {
	s, h, work := setup(t)
	_ = os.WriteFile(filepath.Join(work, "source.txt"), []byte("expected-content"), 0600)
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			fmt.Fprint(w, `{"status":"completed","output":[{"type":"function_call","call_id":"tool-1","name":"workspace-read","arguments":"{\"path\":\"source.txt\"}"}]}`)
		} else {
			raw, _ := json.Marshal(p)
			if !strings.Contains(string(raw), "expected-content") {
				t.Error("missing tool output", string(raw))
			}
			fmt.Fprint(w, `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Verified expected-content"}]}]}`)
		}
	}))
	defer model.Close()
	c := s.Config.Get()
	c.Providers = []agent.Provider{{ID: "fixture", Kind: "responses", BaseURL: model.URL, Model: "test-only", MaxOutputTokens: 1024}}
	if e := s.Config.Set(c); e != nil {
		t.Fatal(e)
	}
	code, v, _ := request(t, s, h, "POST", "/v1/runs", s.Token, agent.RunRequest{Session: "one", Provider: "fixture", Prompt: "read source", Autonomy: "ask", MaxSteps: 3}, nil)
	if code != 200 {
		t.Fatal(code, v)
	}
	deadline := time.Now().Add(5 * time.Second)
	for s.Agents.Busy("one") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	runs := s.Agents.List()
	if len(runs) != 1 || runs[0].Status != "completed" || runs[0].Text != "Verified expected-content" || calls.Load() != 2 {
		t.Fatal(runs, calls.Load())
	}
}
func TestApprovalDenyAndSingleUse(t *testing.T) {
	s, _, _ := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Agents.Ask(ctx, "test", "one", "workspace-write", map[string]any{"path": "x"}) }()
	for len(s.Agents.Approvals()) == 0 && ctx.Err() == nil {
		time.Sleep(time.Millisecond)
	}
	approvals := s.Agents.Approvals()
	if len(approvals) != 1 {
		t.Fatal("approval missing")
	}
	id := approvals[0].ID
	if e := s.Agents.Approve(id, false); e != nil {
		t.Fatal(e)
	}
	if e := s.Agents.Approve(id, true); e == nil {
		t.Fatal("approval reused")
	}
	if e := <-done; e == nil {
		t.Fatal("denied operation allowed")
	}
}
func TestCatalogContracts(t *testing.T) {
	seen := map[string]bool{}
	for _, tool := range builtins() {
		if seen[tool.Name] {
			t.Fatal("duplicate", tool.Name)
		}
		seen[tool.Name] = true
		if tool.Description == "" || tool.InputSchema["type"] != "object" {
			t.Fatal(tool)
		}
		if tool.Name == "evaluate-js" && tool.ReadOnly {
			t.Fatal("JS classified read-only")
		}
	}
}

func TestConcurrentSessionRegistration(t *testing.T) {
	s, h, _ := setup(t)
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			code, v, _ := request(t, s, h, "POST", "/v1/sessions", s.Token, Session{ID: fmt.Sprintf("parallel-%d", i)}, nil)
			if code != 200 {
				t.Error(code, v)
			}
		}(i)
	}
	wg.Wait()
	if n := len(s.Config.Get().Sessions); n != 26 {
		t.Fatal("lost session registrations", n)
	}
}

func TestDeleteChatRemovesHistoryProfileAndKeepsWorkspace(t *testing.T) {
	s, h, work := setup(t)
	for _, sid := range []string{"one", "two"} {
		if err := s.Store.Write("run-"+sid, agent.Run{ID: sid, Session: sid, Status: "completed", Text: "private " + sid}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Store.Append(sid, "message.delta", map[string]any{"text": "private " + sid}); err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"profiles", "artifacts"} {
			dir := filepath.Join(s.Store.Root, kind, sid)
			os.MkdirAll(dir, 0700)
			os.WriteFile(filepath.Join(dir, "fixture"), []byte("private"), 0600)
		}
	}
	s.Agents.Close()
	s.Agents = agent.NewManager(s.Store)
	file := filepath.Join(work, "keep.txt")
	os.WriteFile(file, []byte("workspace"), 0600)
	status, _, _ := request(t, s, h, "DELETE", "/v1/sessions?session=one", s.Token, nil, nil)
	if status != 200 {
		t.Fatal(status)
	}
	if _, ok := s.Config.Session("one"); ok {
		t.Fatal("deleted session still configured")
	}
	if _, ok := s.Config.Session("two"); !ok {
		t.Fatal("other session removed")
	}
	if runs := s.Agents.List(); len(runs) != 1 || runs[0].Session != "two" {
		t.Fatal(runs)
	}
	if len(s.Store.Events(0, "one", 100)) != 0 {
		t.Fatal("deleted events retained")
	}
	for _, kind := range []string{"profiles", "artifacts"} {
		if _, err := os.Stat(filepath.Join(s.Store.Root, kind, "one")); !os.IsNotExist(err) {
			t.Fatal(kind, err)
		}
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("workspace deleted", err)
	}
	if _, err := os.Stat(filepath.Join(s.Store.Root, "run-one.json")); !os.IsNotExist(err) {
		t.Fatal("run persisted", err)
	}
	status, _, _ = request(t, s, h, "DELETE", "/v1/sessions?session=one", s.Token, nil, nil)
	if status != 400 {
		t.Fatal(status)
	}
}
