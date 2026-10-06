package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"svolo.local/core/internal/store"
)

func TestProviderRetriesExplicit503WithIdenticalContext(t *testing.T) {
	for _, kind := range []string{"responses", "chat-completions"} {
		t.Run(kind, func(t *testing.T) {
			var count atomic.Int32
			var first string
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if count.Add(1) == 1 {
					first = string(body)
				} else if string(body) != first {
					t.Error("retry changed model, history or tool catalog")
				}
				if count.Load() < 3 {
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(503)
					fmt.Fprint(w, `{"error":"do not echo fixture-customer-secret"}`)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if kind == "responses" {
					fmt.Fprint(w, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Recovered"}]}]}`)
				} else {
					fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"Recovered"},"finish_reason":"stop"}]}`)
				}
			}))
			defer s.Close()
			var retries []ProviderRetry
			p := Provider{ID: "fixture", Kind: kind, BaseURL: s.URL, Model: "fixture", MaxOutputTokens: 128, onRetry: func(retry ProviderRetry) { retries = append(retries, retry) }}
			result, err := p.Complete(context.Background(), "fixture", []Turn{{Role: "user", Text: "fixture-customer-secret"}}, nil, nil)
			if err != nil || result.Text != "Recovered" || count.Load() != 3 {
				t.Fatal(result, err, count.Load())
			}
			if len(retries) != 2 || retries[0].Attempt != 2 || retries[1].Attempt != 3 || retries[0].Status != 503 || retries[1].MaxAttempts != 3 {
				t.Fatal(retries)
			}
		})
	}
}

func TestProviderRetryBoundsAndPrivateErrors(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 429, 500, 501, 502, 503, 504} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var count atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":"fixture-customer-secret fixture-key"}`)
			}))
			defer s.Close()
			p := Provider{ID: "fixture", Kind: "chat-completions", BaseURL: s.URL, Model: "fixture", MaxOutputTokens: 128}
			_, err := p.Complete(context.Background(), "", nil, nil, nil)
			var failure *ProviderHTTPError
			if !errors.As(err, &failure) || failure.Status != status || strings.Contains(err.Error(), "fixture-customer-secret") || strings.Contains(err.Error(), "fixture-key") {
				t.Fatal("unsafe or missing typed error", err)
			}
			expected := int32(1)
			if transientProviderStatus(status) {
				expected = 3
			}
			if count.Load() != expected || failure.Attempts != int(expected) {
				t.Fatal("incorrect retry bound", count.Load(), failure)
			}
		})
	}
}

func TestProviderRetryAfterAndCancellation(t *testing.T) {
	for _, header := range []string{"60", time.Now().Add(5 * time.Minute).UTC().Format(http.TimeFormat)} {
		t.Run(header, func(t *testing.T) {
			var count atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				w.Header().Set("Retry-After", header)
				w.WriteHeader(503)
			}))
			defer s.Close()
			p := Provider{ID: "fixture", Kind: "responses", BaseURL: s.URL, Model: "fixture", MaxOutputTokens: 128}
			_, err := p.Complete(context.Background(), "", nil, nil, nil)
			if err == nil || count.Load() != 1 {
				t.Fatal("long Retry-After ignored", err, count.Load())
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var count atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.Header().Set("Retry-After", "20")
		w.WriteHeader(503)
	}))
	defer s.Close()
	p := Provider{ID: "fixture", Kind: "responses", BaseURL: s.URL, Model: "fixture", MaxOutputTokens: 128, onRetry: func(ProviderRetry) { cancel() }}
	started := time.Now()
	_, err := p.Complete(ctx, "", nil, nil, nil)
	if !errors.Is(err, context.Canceled) || count.Load() != 1 || time.Since(started) > time.Second {
		t.Fatal("Stop did not cancel retry wait", err, count.Load())
	}
	if delay, ok := providerRetryDelay("", 2); !ok || delay != time.Second {
		t.Fatal("missing first backoff", delay, ok)
	}
	if delay, ok := providerRetryDelay("", 3); !ok || delay != 2*time.Second {
		t.Fatal("missing second backoff", delay, ok)
	}
}

func TestProviderDoesNotReplayPartialStreams(t *testing.T) {
	var count atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Partial answer\"}}]}\n\n")
	}))
	defer s.Close()
	p := Provider{ID: "fixture", Kind: "chat-completions", BaseURL: s.URL, Model: "fixture", MaxOutputTokens: 128, Stream: true}
	var text string
	_, err := p.Complete(context.Background(), "", nil, nil, func(delta string) { text += delta })
	if err == nil || text != "Partial answer" || count.Load() != 1 {
		t.Fatal("partial output replayed", text, err, count.Load())
	}
}

func TestProviderRecovers503BeforeSSEStarts(t *testing.T) {
	var count atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if count.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Recovered stream\"}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer s.Close()
	p := Provider{ID: "fixture", Kind: "chat-completions", BaseURL: s.URL, Model: "fixture", MaxOutputTokens: 128, Stream: true}
	var text string
	reply, err := p.Complete(context.Background(), "", nil, nil, func(delta string) { text += delta })
	if err != nil || text != "Recovered stream" || reply.Text != text || count.Load() != 2 {
		t.Fatal("SSE retry failed", reply, text, err, count.Load())
	}
}

func TestProviderDoesNotRetryTransportFailures(t *testing.T) {
	var count atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		connection.Close()
	}))
	defer s.Close()
	p := Provider{ID: "fixture", Kind: "responses", BaseURL: s.URL, Model: "fixture", MaxOutputTokens: 128}
	_, err := p.Complete(context.Background(), "", nil, nil, nil)
	if err == nil || count.Load() != 1 {
		t.Fatal("ambiguous transport failure replayed", err, count.Load())
	}
}

func TestRunnerRecovers503WithoutReplayingCompletedTool(t *testing.T) {
	var requests, executed atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch requests.Add(1) {
		case 1:
			fmt.Fprint(w, `{"status":"completed","output":[{"type":"function_call","call_id":"fill-once","name":"fill","arguments":"{\"tab\":\"fixture-tab\",\"target\":\"css:input\",\"text\":\"fixture-only\"}"}]}`)
		case 2:
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(503)
		default:
			if !strings.Contains(string(body), "function_call_output") || !strings.Contains(string(body), "fixture-verified") {
				t.Error("retry lost completed tool result")
			}
			fmt.Fprint(w, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Verified after recovery"}]}]}`)
		}
	}))
	defer s.Close()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := NewManager(st)
	defer m.Close()
	m.Providers = func() []Provider {
		return []Provider{{ID: "fixture", Kind: "responses", BaseURL: s.URL, Model: "fixture", MaxOutputTokens: 128}}
	}
	m.Tools = func() []Tool { return []Tool{{Name: "fill", InputSchema: map[string]any{"type": "object"}}} }
	m.Execute = func(context.Context, string, string, map[string]any) (any, error) {
		executed.Add(1)
		return map[string]any{"verified": "fixture-verified"}, nil
	}
	run, err := m.Start(RunRequest{Session: "retry-fixture", Provider: "fixture", Prompt: "Fill the local test field and verify.", Autonomy: "ask", AllowedTools: []string{"fill"}, MaxSteps: 2})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, r := range m.List() {
			if r.ID != run.ID || r.Status == "running" {
				continue
			}
			if r.Status != "completed" || r.Text != "Verified after recovery" || r.Steps != 2 || executed.Load() != 1 || requests.Load() != 3 {
				t.Fatal("tool replayed or recovery failed", r.Status, r.Error, r.Steps, executed.Load(), requests.Load())
			}
			retries := 0
			for _, event := range st.Events(0, run.Session, 100) {
				if event.Type == "provider.retry" {
					retries++
					data, _ := json.Marshal(event.Data)
					if strings.Contains(string(data), "fixture-only") || strings.Contains(string(data), "fixture-verified") {
						t.Fatal("retry metadata exposed tool data")
					}
				}
			}
			if retries != 1 {
				t.Fatal("retry progress missing", retries)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("recovery did not complete")
}

func TestRunnerPersistsExhaustedProviderError(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(503)
		fmt.Fprint(w, "fixture-customer-secret")
	}))
	defer s.Close()
	st, _ := store.Open(t.TempDir())
	defer st.Close()
	m := NewManager(st)
	defer m.Close()
	m.Providers = func() []Provider {
		return []Provider{{ID: "fixture", Kind: "responses", BaseURL: s.URL, Model: "fixture", MaxOutputTokens: 128}}
	}
	m.Tools = func() []Tool { return nil }
	m.Execute = func(context.Context, string, string, map[string]any) (any, error) {
		t.Error("failed provider executed a tool")
		return nil, nil
	}
	run, err := m.Start(RunRequest{Session: "exhausted", Provider: "fixture", Prompt: "Disposable request", Autonomy: "ask", MaxSteps: 1})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, r := range m.List() {
			if r.ID != run.ID || r.Status == "running" {
				continue
			}
			if r.Status != "failed" || r.ProviderError == nil || r.ProviderError.Status != 503 || r.ProviderError.Attempts != 3 || !r.ProviderError.Retryable || strings.Contains(r.Error, "fixture-customer-secret") {
				t.Fatal("unsafe terminal error", r.Status, r.Error, r.ProviderError)
			}
			if m.Busy(run.Session) {
				continue
			}
			var saved Run
			if err := st.Read("run-"+run.ID, &saved); err != nil || saved.ProviderError == nil || saved.ProviderError.Status != 503 {
				t.Fatal("provider error not persisted", err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("exhausted run did not terminate")
}
