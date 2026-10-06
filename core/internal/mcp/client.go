package mcp

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
	"os/exec"
	"strings"
	"svolo.local/core/internal/identity"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	ID                string                       `json:"id"`
	Command           string                       `json:"command,omitempty"`
	Args              []string                     `json:"args,omitempty"`
	EnvKeys           []string                     `json:"envKeys,omitempty"`
	URL               string                       `json:"url,omitempty"`
	TokenEnv          string                       `json:"tokenEnv,omitempty"`
	TokenRef          string                       `json:"tokenRef,omitempty"`
	ResolveSecret     func(string) (string, error) `json:"-"`
	AllowInsecureHTTP bool                         `json:"allowInsecureHTTP,omitempty"`
	Enabled           bool                         `json:"enabled"`
}
type Client interface {
	Call(context.Context, string, any, any) error
	Notify(context.Context, string, any) error
	Close() error
}
type StdioClient struct {
	cmd     *exec.Cmd
	in      io.WriteCloser
	writeMu sync.Mutex
	mu      sync.Mutex
	seq     atomic.Uint64
	pending map[string]chan Message
	done    chan struct{}
	once    sync.Once
	err     error
	wait    chan struct{}
}

func NewStdio(ctx context.Context, cfg Config) (*StdioClient, error) {
	if cfg.Command == "" || cfg.URL != "" {
		return nil, errors.New("exactly one MCP command or URL required")
	}
	cmd := exec.Command(cfg.Command, cfg.Args...)
	env := []string{}
	keys := append([]string{"PATH", "HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "TEMP", "TMP", "SystemRoot", "COMSPEC"}, cfg.EnvKeys...)
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k] {
			continue
		}
		seen[k] = true
		if strings.ContainsAny(k, "=\x00") {
			return nil, errors.New("invalid environment variable name")
		}
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	cmd.Env = env
	cmd.Stderr = io.Discard
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	c := &StdioClient{cmd: cmd, in: in, pending: map[string]chan Message{}, done: make(chan struct{}), wait: make(chan struct{})}
	go c.read(stdout)
	go func() { err := cmd.Wait(); c.fail(err); close(c.wait) }()
	initCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var out map[string]any
	if err = c.Call(initCtx, "initialize", map[string]any{"protocolVersion": Version, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": identity.Slug, "version": identity.Version}}, &out); err != nil {
		c.Close()
		return nil, err
	}
	if err = c.Notify(initCtx, "notifications/initialized", map[string]any{}); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}
func (c *StdioClient) send(m Message) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return json.NewEncoder(c.in).Encode(m)
}
func (c *StdioClient) Call(ctx context.Context, method string, params any, out any) error {
	id := fmt.Sprint(c.seq.Add(1))
	key := id
	ch := make(chan Message, 1)
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return errors.New("MCP server exited")
	default:
	}
	c.pending[key] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, key); c.mu.Unlock() }()
	if err := c.send(Message{JSONRPC: "2.0", ID: json.RawMessage(id), Method: method, Params: raw(params)}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		_ = c.Notify(context.Background(), "notifications/cancelled", map[string]any{"requestId": json.RawMessage(id), "reason": "cancelled"})
		return ctx.Err()
	case <-c.done:
		return errors.New("MCP connection closed; action outcome may be unknown")
	case msg := <-ch:
		return parseResponse(msg, out)
	}
}
func (c *StdioClient) Notify(ctx context.Context, method string, p any) error {
	return c.send(Message{JSONRPC: "2.0", Method: method, Params: raw(p)})
}
func (c *StdioClient) read(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		var msg Message
		if json.Unmarshal(sc.Bytes(), &msg) != nil {
			c.fail(errors.New("non-JSON MCP stdout"))
			return
		}
		if len(msg.ID) > 0 && msg.Method != "" {
			_ = c.send(Message{JSONRPC: "2.0", ID: msg.ID, Error: &RPCError{Code: -32601, Message: "client does not advertise sampling, roots or elicitation"}})
			continue
		}
		c.mu.Lock()
		ch := c.pending[string(msg.ID)]
		c.mu.Unlock()
		if ch != nil {
			select {
			case ch <- msg:
			default:
			}
		}
	}
	c.fail(sc.Err())
}
func (c *StdioClient) fail(err error) {
	c.once.Do(func() { c.mu.Lock(); c.err = err; close(c.done); c.mu.Unlock() })
}
func (c *StdioClient) Close() error {
	c.in.Close()
	select {
	case <-c.wait:
	case <-time.After(time.Second):
		_ = c.cmd.Process.Kill()
		<-c.wait
	}
	return nil
}

type HTTPClient struct {
	URL, Token, Session string
	seq                 atomic.Uint64
	client              *http.Client
	mu                  sync.Mutex
	version             string
}

func NewHTTP(ctx context.Context, cfg Config) (*HTTPClient, error) {
	u, e := url.Parse(cfg.URL)
	if e != nil || u.Host == "" || u.User != nil || cfg.Command != "" || u.Fragment != "" {
		return nil, errors.New("invalid MCP URL")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && (u.Scheme != "http" || (!cfg.AllowInsecureHTTP && u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()))) {
		return nil, errors.New("MCP requires HTTPS except explicitly allowed local endpoints")
	}
	c := &HTTPClient{URL: cfg.URL, version: Version, client: &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("MCP redirect refused") }}}
	if cfg.TokenEnv != "" {
		c.Token = os.Getenv(cfg.TokenEnv)
		if c.Token == "" {
			return nil, fmt.Errorf("MCP token env %s not set", cfg.TokenEnv)
		}
	}
	if cfg.TokenRef != "" {
		if cfg.TokenEnv != "" || cfg.ResolveSecret == nil {
			return nil, errors.New("choose a vault token reference or environment variable, with a resolver")
		}
		c.Token, e = cfg.ResolveSecret(cfg.TokenRef)
		if e != nil {
			return nil, e
		}
	}
	var out struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if e = c.Call(ctx, "initialize", map[string]any{"protocolVersion": Version, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": identity.Slug, "version": identity.Version}}, &out); e != nil {
		return nil, e
	}
	c.version = out.ProtocolVersion
	if e = c.Notify(ctx, "notifications/initialized", map[string]any{}); e != nil {
		return nil, e
	}
	return c, nil
}
func (c *HTTPClient) request(ctx context.Context, m Message, out any) error {
	b, _ := json.Marshal(m)
	req, e := http.NewRequestWithContext(ctx, "POST", c.URL, bytes.NewReader(b))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", c.version)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	c.mu.Lock()
	session := c.Session
	c.mu.Unlock()
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	resp, e := c.client.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("MCP HTTP %d", resp.StatusCode)
	}
	if s := resp.Header.Get("Mcp-Session-Id"); s != "" {
		c.mu.Lock()
		c.Session = s
		c.mu.Unlock()
	}
	if len(m.ID) == 0 {
		return nil
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		sc := bufio.NewScanner(io.LimitReader(resp.Body, 16<<20))
		sc.Buffer(make([]byte, 64<<10), 8<<20)
		var lines []string
		parse := func() (bool, error) {
			if len(lines) == 0 {
				return false, nil
			}
			var reply Message
			e := json.Unmarshal([]byte(strings.Join(lines, "\n")), &reply)
			lines = nil
			if e != nil {
				return false, e
			}
			if string(reply.ID) == string(m.ID) {
				return true, parseResponse(reply, out)
			}
			return false, nil
		}
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				ok, e := parse()
				if ok || e != nil {
					return e
				}
			} else if strings.HasPrefix(line, "data:") {
				lines = append(lines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			}
		}
		if ok, e := parse(); ok || e != nil {
			return e
		}
		if sc.Err() != nil {
			return sc.Err()
		}
		return errors.New("MCP SSE ended before matching response")
	}
	var reply Message
	if e = json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&reply); e != nil {
		return e
	}
	if string(reply.ID) != string(m.ID) {
		return errors.New("MCP response id mismatch")
	}
	return parseResponse(reply, out)
}
func (c *HTTPClient) Call(ctx context.Context, method string, p any, out any) error {
	return c.request(ctx, Message{JSONRPC: "2.0", ID: raw(c.seq.Add(1)), Method: method, Params: raw(p)}, out)
}
func (c *HTTPClient) Notify(ctx context.Context, method string, p any) error {
	return c.request(ctx, Message{JSONRPC: "2.0", Method: method, Params: raw(p)}, nil)
}
func (c *HTTPClient) Close() error { return nil }
func Discover(ctx context.Context, c Client) ([]Tool, error) {
	all := []Tool{}
	cursor := ""
	for page := 0; page < 20; page++ {
		p := map[string]any{}
		if cursor != "" {
			p["cursor"] = cursor
		}
		var r struct {
			Tools []Tool `json:"tools"`
			Next  string `json:"nextCursor"`
		}
		if err := c.Call(ctx, "tools/list", p, &r); err != nil {
			return nil, err
		}
		all = append(all, r.Tools...)
		if len(all) > 1000 {
			return nil, errors.New("MCP tool catalog exceeds limit")
		}
		if r.Next == "" {
			return all, nil
		}
		if r.Next == cursor {
			return nil, errors.New("MCP pagination cursor did not advance")
		}
		cursor = r.Next
	}
	return nil, errors.New("MCP pagination limit exceeded")
}
