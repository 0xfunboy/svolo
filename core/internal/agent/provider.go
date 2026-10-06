// Package agent implements the provider-neutral agent loop. API keys are read from
// named environment variables on the execution host and are never returned to UI.
package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	ReadOnly    bool           `json:"readOnly"`
}
type Provider struct {
	ID                string                       `json:"id"`
	Kind              string                       `json:"kind"`
	BaseURL           string                       `json:"baseURL"`
	APIKeyEnv         string                       `json:"apiKeyEnv,omitempty"`
	APIKeyRef         string                       `json:"apiKeyRef,omitempty"`
	ResolveSecret     func(string) (string, error) `json:"-"`
	Model             string                       `json:"model"`
	Vision            bool                         `json:"vision"`
	Stream            bool                         `json:"stream"`
	AllowInsecureHTTP bool                         `json:"allowInsecureHTTP,omitempty"`
	MaxOutputTokens   int                          `json:"maxOutputTokens"`
	ReasoningEffort   string                       `json:"reasoningEffort,omitempty"`
}
type Image struct {
	MIME string `json:"mimeType"`
	Data string `json:"data"`
}
type Call struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type Turn struct {
	Role   string  `json:"role"`
	Text   string  `json:"text,omitempty"`
	Images []Image `json:"images,omitempty"`
	Calls  []Call  `json:"calls,omitempty"`
	CallID string  `json:"callId,omitempty"`
	Raw    []any   `json:"raw,omitempty"`
}
type Reply struct {
	Text  string
	Calls []Call
	Raw   []any
	Usage map[string]any
}

func (p Provider) Validate() error {
	if p.ID == "" || p.Model == "" {
		return errors.New("provider id and model required")
	}
	if p.Kind != "responses" && p.Kind != "chat-completions" {
		return errors.New("provider kind must be responses or chat-completions")
	}
	u, e := url.Parse(p.BaseURL)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid provider base URL")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || (!p.AllowInsecureHTTP && u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
			return errors.New("HTTPS required; non-loopback HTTP needs explicit allowInsecureHTTP")
		}
	}
	if p.MaxOutputTokens < 128 || p.MaxOutputTokens > 65536 {
		return errors.New("maxOutputTokens must be 128..65536")
	}
	if p.APIKeyEnv != "" && p.APIKeyRef != "" {
		return errors.New("choose an API key environment variable OR vault reference")
	}
	if p.APIKeyRef != "" && (len(p.APIKeyRef) > 120 || strings.ContainsAny(p.APIKeyRef, "/\\\x00")) {
		return errors.New("invalid API credential reference")
	}
	if p.APIKeyEnv != "" {
		for _, c := range p.APIKeyEnv {
			if !(c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
				return errors.New("invalid API key environment variable")
			}
		}
	}
	return nil
}
func (p Provider) Complete(ctx context.Context, system string, turns []Turn, tools []Tool, delta func(string)) (Reply, error) {
	if err := p.Validate(); err != nil {
		return Reply{}, err
	}
	body := map[string]any{"model": p.Model, "stream": p.Stream}
	var definitions []any
	for _, t := range tools {
		if p.Kind == "responses" {
			definitions = append(definitions, map[string]any{"type": "function", "name": t.Name, "description": t.Description, "parameters": t.InputSchema, "strict": false})
		} else {
			definitions = append(definitions, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.InputSchema}})
		}
	}
	if len(definitions) > 0 {
		body["tools"] = definitions
	}
	path := "/responses"
	if p.Kind == "responses" {
		body["instructions"] = system
		body["max_output_tokens"] = p.MaxOutputTokens
		body["store"] = false
		if p.ReasoningEffort != "" {
			body["reasoning"] = map[string]any{"effort": p.ReasoningEffort}
		}
		input := []any{}
		for _, t := range turns {
			if t.Role == "tool" {
				input = append(input, map[string]any{"type": "function_call_output", "call_id": t.CallID, "output": t.Text})
				continue
			}
			if len(t.Raw) > 0 && t.Role == "assistant" {
				input = append(input, t.Raw...)
				continue
			}
			content := []any{}
			if t.Text != "" {
				content = append(content, map[string]any{"type": "input_text", "text": t.Text})
			}
			for _, im := range t.Images {
				if !p.Vision {
					return Reply{}, errors.New("provider vision capability not enabled")
				}
				content = append(content, map[string]any{"type": "input_image", "image_url": "data:" + im.MIME + ";base64," + im.Data})
			}
			input = append(input, map[string]any{"role": t.Role, "content": content})
		}
		body["input"] = input
	} else {
		path = "/chat/completions"
		body["max_tokens"] = p.MaxOutputTokens
		messages := []any{map[string]any{"role": "system", "content": system}}
		for _, t := range turns {
			m := map[string]any{"role": t.Role, "content": t.Text}
			if t.Role == "tool" {
				m["tool_call_id"] = t.CallID
			}
			if len(t.Images) > 0 {
				if !p.Vision {
					return Reply{}, errors.New("provider vision capability not enabled")
				}
				c := []any{map[string]any{"type": "text", "text": t.Text}}
				for _, im := range t.Images {
					c = append(c, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + im.MIME + ";base64," + im.Data}})
				}
				m["content"] = c
			}
			if len(t.Calls) > 0 {
				calls := []any{}
				for _, c := range t.Calls {
					calls = append(calls, map[string]any{"id": c.ID, "type": "function", "function": map[string]any{"name": c.Name, "arguments": c.Arguments}})
				}
				m["tool_calls"] = calls
			}
			messages = append(messages, m)
		}
		body["messages"] = messages
	}
	b, err := json.Marshal(body)
	if err != nil {
		return Reply{}, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(p.BaseURL, "/")+path, bytes.NewReader(b))
	if err != nil {
		return Reply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	key := ""
	if p.APIKeyEnv != "" {
		key = os.Getenv(p.APIKeyEnv)
		if key == "" {
			return Reply{}, fmt.Errorf("API key environment variable %s is not set on this host", p.APIKeyEnv)
		}
	}
	if p.APIKeyRef != "" {
		if p.ResolveSecret == nil {
			return Reply{}, errors.New("vault resolver unavailable")
		}
		key, err = p.ResolveSecret(p.APIKeyRef)
		if err != nil {
			return Reply{}, err
		}
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{Timeout: 180 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("provider redirects refused to protect credentials")
	}}
	resp, err := client.Do(req)
	if err != nil {
		return Reply{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		text := string(msg)
		if key != "" {
			text = strings.ReplaceAll(text, key, "[redacted]")
		}
		return Reply{}, fmt.Errorf("provider HTTP %d: %s", resp.StatusCode, text)
	}
	if p.Stream {
		return p.readStream(resp.Body, delta)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return Reply{}, err
	}
	if p.Kind == "responses" {
		return parseResponses(raw)
	}
	return parseChat(raw)
}
func parseResponses(raw []byte) (Reply, error) {
	var r struct {
		Status string         `json:"status"`
		Output []any          `json:"output"`
		Usage  map[string]any `json:"usage"`
		Error  any            `json:"error"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return Reply{}, err
	}
	if r.Error != nil {
		return Reply{}, fmt.Errorf("provider error: %v", r.Error)
	}
	if r.Status != "" && r.Status != "completed" {
		return Reply{}, fmt.Errorf("provider response did not complete: %s", r.Status)
	}
	out := Reply{Raw: r.Output, Usage: r.Usage}
	for _, v := range r.Output {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if m["type"] == "function_call" {
			c := Call{ID: asString(m["call_id"]), Name: asString(m["name"]), Arguments: asString(m["arguments"])}
			if c.ID == "" || c.Name == "" {
				return Reply{}, errors.New("malformed function call")
			}
			out.Calls = append(out.Calls, c)
		}
		if content, ok := m["content"].([]any); ok {
			for _, part := range content {
				if p, ok := part.(map[string]any); ok {
					if p["type"] == "output_text" {
						out.Text += asString(p["text"])
					} else if p["type"] == "refusal" {
						out.Text += asString(p["refusal"])
					}
				}
			}
		}
	}
	return out, nil
}
func parseChat(raw []byte) (Reply, error) {
	var r struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			Finish string `json:"finish_reason"`
		} `json:"choices"`
		Usage map[string]any `json:"usage"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return Reply{}, err
	}
	if len(r.Choices) != 1 {
		return Reply{}, errors.New("provider must return one choice")
	}
	c := r.Choices[0]
	if c.Finish != "stop" && c.Finish != "tool_calls" && c.Finish != "" {
		return Reply{}, fmt.Errorf("incomplete completion: %s", c.Finish)
	}
	out := Reply{Text: c.Message.Content, Usage: r.Usage}
	for _, t := range c.Message.ToolCalls {
		out.Calls = append(out.Calls, Call{ID: t.ID, Name: t.Function.Name, Arguments: t.Function.Arguments})
	}
	return out, nil
}
func asString(v any) string { s, _ := v.(string); return s }
func (p Provider) readStream(r io.Reader, delta func(string)) (Reply, error) {
	scanner := bufio.NewScanner(io.LimitReader(r, 32<<20))
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	var data []string
	var out Reply
	calls := map[int]*Call{}
	done := false
	complete := false
	consume := func(payload string) error {
		if payload == "[DONE]" {
			done = true
			return nil
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			return err
		}
		if p.Kind == "responses" {
			switch asString(ev["type"]) {
			case "response.output_text.delta":
				d := asString(ev["delta"])
				if delta != nil {
					delta(d)
				}
			case "response.completed":
				b, _ := json.Marshal(ev["response"])
				var err error
				out, err = parseResponses(b)
				if err != nil {
					return err
				}
				complete = true
			case "response.failed", "response.incomplete", "error":
				return fmt.Errorf("provider stream failed: %s", asString(ev["type"]))
			}
			return nil
		}
		if errValue := ev["error"]; errValue != nil {
			return fmt.Errorf("provider stream error: %v", errValue)
		}
		if usage, ok := ev["usage"].(map[string]any); ok {
			out.Usage = usage
		}
		choices, _ := ev["choices"].([]any)
		for _, v := range choices {
			c, _ := v.(map[string]any)
			if finish := asString(c["finish_reason"]); finish != "" {
				if finish != "stop" && finish != "tool_calls" {
					return fmt.Errorf("incomplete completion: %s", finish)
				}
				complete = true
			}
			d, _ := c["delta"].(map[string]any)
			text := asString(d["content"])
			out.Text += text
			if text != "" && delta != nil {
				delta(text)
			}
			if tc, ok := d["tool_calls"].([]any); ok {
				for _, v := range tc {
					m, _ := v.(map[string]any)
					n, _ := m["index"].(float64)
					i := int(n)
					if i < 0 || i > 100 {
						return errors.New("tool call index limit")
					}
					call := calls[i]
					if call == nil {
						call = &Call{}
						calls[i] = call
					}
					if id := asString(m["id"]); id != "" {
						call.ID = id
					}
					f, _ := m["function"].(map[string]any)
					call.Name += asString(f["name"])
					call.Arguments += asString(f["arguments"])
				}
			}
		}
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(data) > 0 {
				if err := consume(strings.Join(data, "\n")); err != nil {
					return Reply{}, err
				}
				data = nil
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return Reply{}, err
	}
	if len(data) > 0 {
		if err := consume(strings.Join(data, "\n")); err != nil {
			return Reply{}, err
		}
	}
	if !complete {
		return Reply{}, errors.New("stream ended without a completed response")
	}
	if p.Kind == "chat-completions" {
		if !done {
			return Reply{}, errors.New("chat stream ended without DONE")
		}
		for i := 0; i < len(calls); i++ {
			c := calls[i]
			if c == nil || c.ID == "" || c.Name == "" {
				return Reply{}, errors.New("malformed streamed function call")
			}
			out.Calls = append(out.Calls, *c)
		}
	}
	return out, nil
}
