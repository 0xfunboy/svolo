package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"svolo.local/core/internal/agent"
)

type resizeBrowser struct{ inertBrowser }

func (resizeBrowser) Call(context.Context, string, string, string, map[string]any, any) error {
	return nil
}

func TestViewportDuringCredentialFollowupDoesNotCancelRun(t *testing.T) {
	s, h, _ := setup(t)
	s.Engine.Transport = resizeBrowser{}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch calls.Add(1) {
		case 1:
			fmt.Fprint(w, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"The fixture password is missing. Supply one to continue."}]}]}`)
		case 2:
			encoded, _ := json.Marshal(input)
			if !strings.Contains(string(encoded), "password is missing") || !strings.Contains(string(encoded), "Use the disposable fixture credential") {
				t.Error("continuation lost prior question or user answer")
			}
			close(entered)
			select {
			case <-r.Context().Done():
				return
			case <-release:
			}
			fmt.Fprint(w, `{"status":"completed","output":[{"type":"function_call","call_id":"fill-fixture","name":"fill","arguments":"{\"tab\":\"fixture\",\"target\":\"css:input[name=password]\",\"text\":\"not-a-real-credential\"}"}]}`)
		default:
			fmt.Fprint(w, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Fixture field completed and verified."}]}]}`)
		}
	}))
	t.Cleanup(model.Close)
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	c := s.Config.Get()
	c.Providers = []agent.Provider{{ID: "resize-fixture", Kind: "responses", BaseURL: model.URL, Model: "fixture", MaxOutputTokens: 1024}}
	if err := s.Config.Set(c); err != nil {
		t.Fatal(err)
	}
	var filled atomic.Bool
	s.Agents.Execute = func(ctx context.Context, sid, name string, args map[string]any) (any, error) {
		if name != "fill" || sid != "one" || args["tab"] != "fixture" || args["text"] != "not-a-real-credential" {
			return nil, fmt.Errorf("unexpected fixture action")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		filled.Store(true)
		return map[string]any{"ok": true}, nil
	}
	start := func(prompt string, continuation bool) string {
		code, result, _ := request(t, s, h, "POST", "/v1/runs", s.Token, agent.RunRequest{Session: "one", Provider: "resize-fixture", Prompt: prompt, Continue: continuation, Autonomy: "ask", AllowedTools: []string{"fill"}, MaxSteps: 3}, nil)
		if code != 200 {
			t.Fatal(code, result)
		}
		return result["id"].(string)
	}
	wait := func(id string) {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			for _, r := range s.Agents.List() {
				if r.ID == id && r.Status != "running" && r.Status != "waiting_approval" {
					if r.Status != "completed" {
						t.Fatal("fixture failed", r.Status, r.Error)
					}
					if !s.Agents.Busy("one") {
						return
					}
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		for _, r := range s.Agents.List() {
			t.Logf("fixture id=%s status=%s steps=%d error=%s busy=%v providerCalls=%d", r.ID, r.Status, r.Steps, r.Error, s.Agents.Busy("one"), calls.Load())
		}
		t.Fatal("fixture run did not finish")
	}
	wait(start("Fill the local fixture form.", false))
	id := start("Use the disposable fixture credential and continue.", true)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("provider did not enter follow-up")
	}
	before, _ := s.Engine.Control("one", "")
	payload := map[string]any{"session": "one", "tab": "fixture", "width": 1920, "height": 1080, "dpr": 1, "mobile": false, "touch": false}
	code, result, _ := request(t, s, h, "POST", "/v1/browser/viewport", s.Token, payload, nil)
	if code != 200 {
		t.Fatal(code, result)
	}
	after, _ := s.Engine.Control("one", "")
	if before != after || after.Owner != "agent" || !s.Agents.Busy("one") {
		t.Fatal("resize cancelled follow-up", before, after)
	}
	for _, field := range []string{"url", "userAgent", "name", "reset", "arguments"} {
		payload[field] = "forbidden"
		code, _, _ = request(t, s, h, "POST", "/v1/browser/viewport", s.Token, payload, nil)
		delete(payload, field)
		if code != 400 {
			t.Fatal("unknown field accepted", field, code)
		}
	}
	payload["tab"] = "foreign"
	code, _, _ = request(t, s, h, "POST", "/v1/browser/viewport", s.Token, payload, nil)
	if code != 400 {
		t.Fatal("foreign tab accepted", code)
	}
	payload["session"] = "unknown"
	code, _, _ = request(t, s, h, "POST", "/v1/browser/viewport", s.Token, payload, nil)
	if code != 400 {
		t.Fatal("unknown session accepted", code)
	}
	once.Do(func() { close(release) })
	wait(id)
	if !filled.Load() {
		t.Fatal("follow-up never filled the fixture")
	}
}
