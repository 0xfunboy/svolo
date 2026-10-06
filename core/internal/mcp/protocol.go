// Package mcp implements the tools subset of MCP with stdio and HTTP clients.
// Sampling, elicitation, OAuth discovery and arbitrary server-initiated execution
// are not advertised or silently granted.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"svolo.local/core/internal/identity"
	"sync"
)

const Version = "2025-11-25"

type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

// RawToolResult preserves an upstream MCP tools/call result, including image blocks.
type RawToolResult map[string]any

type Backend interface {
	ListTools(context.Context) ([]Tool, error)
	CallTool(context.Context, string, map[string]any) (any, error)
}

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func Handle(ctx context.Context, b Backend, m Message) Message {
	reply := Message{JSONRPC: "2.0", ID: m.ID}
	fail := func(code int, s string) Message { reply.Error = &RPCError{Code: code, Message: s}; return reply }
	if m.JSONRPC != "2.0" {
		return fail(-32600, "invalid JSON-RPC version")
	}
	switch m.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(m.Params, &p) != nil {
			return fail(-32602, "invalid initialize params")
		}
		version := p.ProtocolVersion
		if version != "2025-03-26" && version != "2025-06-18" && version != Version {
			version = Version
		}
		reply.Result = raw(map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]any{"name": identity.Slug, "version": identity.Version}, "instructions": "Only the explicitly selected session is available. Mutating actions require its control lease and may require approval in the UI."})
	case "ping":
		reply.Result = raw(map[string]any{})
	case "tools/list":
		tools, err := b.ListTools(ctx)
		if err != nil {
			return fail(-32603, err.Error())
		}
		reply.Result = raw(map[string]any{"tools": tools})
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if json.Unmarshal(m.Params, &p) != nil || p.Name == "" {
			return fail(-32602, "name and object arguments required")
		}
		out, err := b.CallTool(ctx, p.Name, p.Arguments)
		if err != nil {
			reply.Result = raw(map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": err.Error()}}})
			break
		}
		if rawResult, ok := out.(RawToolResult); ok {
			reply.Result = raw(rawResult)
			break
		}
		content := []any{}
		if result, ok := out.(map[string]any); ok {
			data, _ := result["data"].(string)
			mime, _ := result["mimeType"].(string)
			if data != "" && mime != "" {
				content = append(content, map[string]any{"type": "image", "data": data, "mimeType": mime})
				copy := map[string]any{}
				for k, v := range result {
					if k != "data" {
						copy[k] = v
					}
				}
				out = copy
			}
		}
		text, _ := json.Marshal(out)
		content = append(content, map[string]any{"type": "text", "text": string(text)})
		reply.Result = raw(map[string]any{"isError": false, "content": content})
	default:
		return fail(-32601, "method not supported")
	}
	return reply
}
func Serve(ctx context.Context, in io.Reader, out io.Writer, b Backend) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	var writeMu sync.Mutex
	write := func(m Message) { writeMu.Lock(); defer writeMu.Unlock(); _ = json.NewEncoder(out).Encode(m) }
	var wg sync.WaitGroup
	slots := make(chan struct{}, 16)
	var mu sync.Mutex
	active := map[string]context.CancelFunc{}
	initialized := false
	defer func() {
		mu.Lock()
		for _, cancel := range active {
			cancel()
		}
		mu.Unlock()
		wg.Wait()
	}()
	for scanner.Scan() {
		var m Message
		if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
			write(Message{JSONRPC: "2.0", ID: raw(nil), Error: &RPCError{Code: -32700, Message: "parse error"}})
			continue
		}
		if len(m.ID) == 0 {
			if m.Method == "notifications/cancelled" {
				var p struct {
					RequestID json.RawMessage `json:"requestId"`
				}
				if json.Unmarshal(m.Params, &p) == nil {
					mu.Lock()
					cancel := active[string(p.RequestID)]
					mu.Unlock()
					if cancel != nil {
						cancel()
					}
				}
			}
			continue
		}
		if m.Method == "initialize" {
			if initialized {
				write(Message{JSONRPC: "2.0", ID: m.ID, Error: &RPCError{Code: -32600, Message: "already initialized"}})
			} else {
				reply := Handle(ctx, b, m)
				write(reply)
				initialized = reply.Error == nil
			}
			continue
		}
		if !initialized {
			write(Message{JSONRPC: "2.0", ID: m.ID, Error: &RPCError{Code: -32600, Message: "initialize first"}})
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			write(Message{JSONRPC: "2.0", ID: m.ID, Error: &RPCError{Code: -32000, Message: "concurrent request limit"}})
			continue
		}
		reqCtx, cancel := context.WithCancel(ctx)
		key := string(m.ID)
		mu.Lock()
		if _, exists := active[key]; exists {
			mu.Unlock()
			cancel()
			<-slots
			write(Message{JSONRPC: "2.0", ID: m.ID, Error: &RPCError{Code: -32600, Message: "duplicate active request id"}})
			continue
		}
		active[key] = cancel
		mu.Unlock()
		wg.Add(1)
		go func(m Message) {
			defer wg.Done()
			defer func() { <-slots; cancel(); mu.Lock(); delete(active, key); mu.Unlock() }()
			write(Handle(reqCtx, b, m))
		}(m)
	}
	return scanner.Err()
}
func parseResponse(m Message, out any) error {
	if m.Error != nil {
		return fmt.Errorf("MCP error %d: %s", m.Error.Code, m.Error.Message)
	}
	if len(m.Result) == 0 {
		return errors.New("MCP response missing result")
	}
	if out != nil {
		return json.Unmarshal(m.Result, out)
	}
	return nil
}
