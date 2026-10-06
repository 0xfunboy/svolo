package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"svolo.local/core/internal/agent"
)

func TestHumanTakeoverWorksWhileCancelledExecutorFinishes(t *testing.T) {
	s, h, work := setup(t)
	if err := os.WriteFile(filepath.Join(work, "source.txt"), []byte("visible-to-human"), 0600); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	s.Agents.Execute = func(ctx context.Context, sid, name string, args map[string]any) (any, error) {
		close(entered)
		<-release // Deliberately model an already-dispatched slow operation.
		return nil, ctx.Err()
	}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"completed","output":[{"type":"function_call","call_id":"slow-read","name":"workspace-read","arguments":"{\"path\":\"source.txt\"}"}]}`)
	}))
	defer model.Close()
	c := s.Config.Get()
	c.Providers = []agent.Provider{{ID: "handoff-fixture", Kind: "responses", BaseURL: model.URL, Model: "fixture", MaxOutputTokens: 1024}}
	if err := s.Config.Set(c); err != nil {
		t.Fatal(err)
	}
	code, result, _ := request(t, s, h, "POST", "/v1/runs", s.Token, agent.RunRequest{Session: "one", Provider: "handoff-fixture", Prompt: "Read the fixture", Autonomy: "ask", MaxSteps: 2}, nil)
	if code != 200 {
		t.Fatal(code, result)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("agent never entered the slow tool")
	}
	before, _ := s.Engine.Control("one", "")
	if before.Owner != "agent" {
		t.Fatal(before)
	}
	code, result, _ = request(t, s, h, "POST", "/v1/tools/call", s.Token, map[string]any{"session": "one", "name": "workspace-read", "arguments": map[string]any{"path": "source.txt"}}, nil)
	if code != 200 {
		t.Fatal("observation was blocked by an active agent", code, result)
	}
	after, _ := s.Engine.Control("one", "")
	if after != before {
		t.Fatal("read-only tool changed control", before, after)
	}
	code, result, _ = request(t, s, h, "POST", "/v1/tools/call", s.Token, map[string]any{"session": "one", "name": "workspace-write", "arguments": map[string]any{"path": "human.txt", "content": "before takeover"}}, nil)
	if code != 400 {
		t.Fatal("mutation proceeded before takeover", code, result)
	}
	code, result, _ = request(t, s, h, "POST", "/v1/control", s.Token, map[string]any{"session": "one", "owner": "human"}, nil)
	if code != 200 || result["owner"] != "human" {
		t.Fatal("takeover did not immediately acknowledge the human", code, result)
	}
	if !s.Agents.Busy("one") {
		t.Fatal("fixture was meant to retain a pending cancelled executor")
	}
	code, result, _ = request(t, s, h, "POST", "/v1/tools/call", s.Token, map[string]any{"session": "one", "name": "workspace-write", "arguments": map[string]any{"path": "human.txt", "content": "human has control"}}, nil)
	if code != 200 {
		raw, _ := json.Marshal(result)
		t.Fatalf("human action still blocked after takeover: %d %s", code, raw)
	}
	data, err := os.ReadFile(filepath.Join(work, "human.txt"))
	if err != nil || string(data) != "human has control" {
		t.Fatal(string(data), err)
	}
}
