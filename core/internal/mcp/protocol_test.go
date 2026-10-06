package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type backend struct{}

func (backend) ListTools(context.Context) ([]Tool, error) {
	return []Tool{{Name: "echo", InputSchema: map[string]any{"type": "object"}}}, nil
}
func (backend) CallTool(_ context.Context, name string, p map[string]any) (any, error) {
	if name != "echo" {
		return nil, fmt.Errorf("unknown tool")
	}
	return p, nil
}
func TestLifecycleAndCall(t *testing.T) {
	in := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"x":1}}}
`
	var out bytes.Buffer
	if e := Serve(context.Background(), strings.NewReader(in), &out, backend{}); e != nil {
		t.Fatal(e)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatal(out.String())
	}
	for _, line := range lines {
		var m Message
		if json.Unmarshal([]byte(line), &m) != nil || m.Error != nil {
			t.Fatal(line)
		}
	}
}
func TestInitializeRequired(t *testing.T) {
	var out bytes.Buffer
	Serve(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`), &out, backend{})
	if !strings.Contains(out.String(), "initialize first") {
		t.Fatal(out.String())
	}
}
func TestToolErrorIsResult(t *testing.T) {
	r := Handle(context.Background(), backend{}, Message{JSONRPC: "2.0", ID: raw(1), Method: "tools/call", Params: raw(map[string]any{"name": "bad", "arguments": map[string]any{}})})
	if r.Error != nil || !strings.Contains(string(r.Result), `"isError":true`) {
		t.Fatal(r)
	}
}
func TestHTTPClientAndDiscovery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			t.Error("missing Accept")
		}
		var msg Message
		json.NewDecoder(r.Body).Decode(&msg)
		if len(msg.ID) == 0 {
			w.WriteHeader(202)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Handle(r.Context(), backend{}, msg))
	}))
	defer srv.Close()
	c, e := NewHTTP(context.Background(), Config{URL: srv.URL})
	if e != nil {
		t.Fatal(e)
	}
	tools, e := Discover(context.Background(), c)
	if e != nil || len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatal(tools, e)
	}
}
func TestVersionNegotiation(t *testing.T) {
	r := Handle(context.Background(), backend{}, Message{JSONRPC: "2.0", ID: raw(1), Method: "initialize", Params: raw(map[string]any{"protocolVersion": "unknown"})})
	if !strings.Contains(string(r.Result), Version) {
		t.Fatal(r)
	}
}
