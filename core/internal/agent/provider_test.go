package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"svolo.local/core/internal/store"
	"testing"
	"time"
)

func TestResponsesToolProtocol(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Error(r.URL)
		}
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		if b["store"] != false {
			t.Error("server storage was not disabled")
		}
		if _, ok := b["tools"].([]any); !ok {
			t.Error("missing tools")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"completed","output":[{"type":"function_call","call_id":"c1","name":"state","arguments":"{}"}],"usage":{"input_tokens":12}}`)
	}))
	defer s.Close()
	p := Provider{ID: "test", Kind: "responses", BaseURL: s.URL + "/v1", Model: "fixture", MaxOutputTokens: 1024}
	out, e := p.Complete(context.Background(), "test", []Turn{{Role: "user", Text: "hello"}}, []Tool{{Name: "state", InputSchema: map[string]any{"type": "object"}}}, nil)
	if e != nil || len(out.Calls) != 1 || out.Calls[0].Name != "state" {
		t.Fatal(out, e)
	}
}
func TestResponsesSSE(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"Hello\"}]}]}}\n\n")
	}))
	defer s.Close()
	p := Provider{ID: "p", Kind: "responses", BaseURL: s.URL, Model: "test", Stream: true, MaxOutputTokens: 1024}
	var delta string
	out, e := p.Complete(context.Background(), "", nil, nil, func(s string) { delta += s })
	if e != nil || out.Text != "Hello" || delta != "Hello" {
		t.Fatal(out, delta, e)
	}
}
func TestChatSSECalls(t *testing.T) {
	stream := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"abc","function":{"name":"click","arguments":"{\"target\":"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"css:button\"}"}}]},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	p := Provider{Kind: "chat-completions"}
	r, e := p.readStream(strings.NewReader(stream), nil)
	if e != nil || len(r.Calls) != 1 || r.Calls[0].Arguments != `{"target":"css:button"}` {
		t.Fatal(r, e)
	}
}
func TestProviderValidationAndTruncation(t *testing.T) {
	p := Provider{ID: "p", Kind: "responses", BaseURL: "http://example.com", Model: "m", MaxOutputTokens: 1024}
	if p.Validate() == nil {
		t.Fatal("insecure remote allowed")
	}
	p.BaseURL = "https://example.com/v1"
	if e := p.Validate(); e != nil {
		t.Fatal(e)
	}
	if _, e := parseResponses([]byte(`{"status":"incomplete","output":[]}`)); e == nil {
		t.Fatal("incomplete marked complete")
	}
	if _, e := p.readStream(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hi\"}\n\n"), nil); e == nil {
		t.Fatal("truncated stream accepted")
	}
}
func TestAPIKeyRedacted(t *testing.T) {
	t.Setenv("SVOLO_TEST_KEY", "secret-test-key")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-test-key" {
			t.Error("missing auth")
		}
		w.WriteHeader(401)
		fmt.Fprint(w, "invalid key secret-test-key")
	}))
	defer s.Close()
	p := Provider{ID: "p", Kind: "responses", BaseURL: s.URL, Model: "m", MaxOutputTokens: 1024, APIKeyEnv: "SVOLO_TEST_KEY"}
	_, e := p.Complete(context.Background(), "", nil, nil, nil)
	if e == nil || strings.Contains(e.Error(), "secret-test-key") {
		t.Fatal(e)
	}
}
func TestRunnerExecutesToolsThenFinal(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Input []map[string]any `json:"input"`
		}
		json.NewDecoder(r.Body).Decode(&b)
		hasResult := false
		for _, v := range b.Input {
			if v["type"] == "function_call_output" {
				hasResult = true
			}
		}
		if hasResult {
			fmt.Fprint(w, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Verified result"}]}]}`)
		} else {
			fmt.Fprint(w, `{"status":"completed","output":[{"type":"function_call","call_id":"c","name":"state","arguments":"{\"tab\":\"fixture-tab\"}"}]}`)
		}
	}))
	defer s.Close()
	st, _ := store.Open(t.TempDir())
	defer st.Close()
	m := NewManager(st)
	defer m.Close()
	m.Providers = func() []Provider {
		return []Provider{{ID: "p", Kind: "responses", BaseURL: s.URL, Model: "m", MaxOutputTokens: 1024}}
	}
	m.Tools = func() []Tool {
		return []Tool{{Name: "state", ReadOnly: true, InputSchema: map[string]any{"type": "object"}}}
	}
	m.Execute = func(context.Context, string, string, map[string]any) (any, error) {
		return map[string]any{"verified": true}, nil
	}
	run, e := m.Start(RunRequest{Session: "s", Provider: "p", Prompt: "test", Autonomy: "ask", MaxSteps: 3})
	if e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, r := range m.List() {
			if r.ID == run.ID && r.Status == "completed" {
				if r.Text != "Verified result" {
					t.Fatal(r)
				}
				return
			}
			if r.Status == "failed" {
				t.Fatal(r.Error)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run did not finish")
}
func TestApprovalDenialAndRecovery(t *testing.T) {
	st, _ := store.Open(t.TempDir())
	defer st.Close()
	m := NewManager(st)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- m.Ask(ctx, "r", "s", "exec", map[string]any{"program": "sh"}) }()
	for len(m.Approvals()) == 0 {
		time.Sleep(time.Millisecond)
	}
	a := m.Approvals()[0]
	if e := m.Approve(a.ID, false); e != nil {
		t.Fatal(e)
	}
	if e := <-done; e == nil {
		t.Fatal("denial ignored")
	}
	if len(m.Approvals()) != 0 {
		t.Fatal("approval leaked")
	}
	_ = st.Write("run-crashed", Run{ID: "crashed", Session: "s", Status: "running"})
	m = NewManager(st)
	if m.List()[0].Status != "interrupted" {
		t.Fatal(m.List())
	}
}
