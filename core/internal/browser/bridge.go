package browser

import (
	"context"
	"encoding/json"
	"errors"
	"svolo.local/core/internal/store"
	"sync"
	"time"
)

// Bridge transports browser commands to the Electron main process. It does not expose CDP on a TCP port.
// Requests have bounded lifetimes and are NEVER replayed after a disconnect (mutations may have executed).
type BridgeRequest struct {
	ID       string         `json:"id"`
	Session  string         `json:"session"`
	Action   string         `json:"action"`
	Target   string         `json:"target,omitempty"`
	Method   string         `json:"method,omitempty"`
	Params   map[string]any `json:"params,omitempty"`
	Deadline time.Time      `json:"deadline"`
	Actor    string         `json:"actor,omitempty"`
	Epoch    uint64         `json:"epoch,omitempty"`
}
type BridgeReply struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error,omitempty"`
}
type Bridge struct {
	queue   chan BridgeRequest
	mu      sync.Mutex
	pending map[string]chan BridgeReply
	leases  map[string]bridgeLease
	closed  chan struct{}
	once    sync.Once
}

func NewBridge() *Bridge {
	return &Bridge{queue: make(chan BridgeRequest, 128), pending: map[string]chan BridgeReply{}, leases: map[string]bridgeLease{}, closed: make(chan struct{})}
}
func (b *Bridge) Poll(ctx context.Context) (BridgeRequest, error) {
	for {
		select {
		case <-ctx.Done():
			return BridgeRequest{}, ctx.Err()
		case <-b.closed:
			return BridgeRequest{}, errors.New("bridge closed")
		case r := <-b.queue:
			b.mu.Lock()
			_, valid := b.pending[r.ID]
			b.mu.Unlock()
			if valid && time.Now().Before(r.Deadline) {
				return r, nil
			}
		}
	}
}
func (b *Bridge) Reply(r BridgeReply) bool {
	b.mu.Lock()
	ch := b.pending[r.ID]
	b.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- r:
		return true
	default:
		return false
	}
}
func (b *Bridge) request(ctx context.Context, r BridgeRequest, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if lease, ok := ctx.Value(controlContextKey{}).(controlLease); ok {
		r.Actor = lease.Actor
		r.Epoch = lease.Epoch
	}
	r.ID = store.ID()
	r.Deadline, _ = ctx.Deadline()
	ch := make(chan BridgeReply, 1)
	b.mu.Lock()
	b.pending[r.ID] = ch
	b.leases[r.ID] = bridgeLease{r, ctx}
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.pending, r.ID); delete(b.leases, r.ID); b.mu.Unlock() }()
	select {
	case b.queue <- r:
	case <-ctx.Done():
		return ctx.Err()
	case <-b.closed:
		return errors.New("bridge closed")
	}
	select {
	case <-ctx.Done():
		return errors.New("desktop bridge timed out/cancelled; do not automatically repeat a mutating action")
	case <-b.closed:
		return errors.New("bridge closed")
	case reply := <-ch:
		if reply.Error != "" {
			return errors.New(reply.Error)
		}
		if out != nil {
			return json.Unmarshal(reply.Result, out)
		}
		return nil
	}
}
func (b *Bridge) List(ctx context.Context, sid string) (out []Target, e error) {
	e = b.request(ctx, BridgeRequest{Session: sid, Action: "list"}, &out)
	return
}
func (b *Bridge) Create(ctx context.Context, sid, url string) (out Target, e error) {
	e = b.request(ctx, BridgeRequest{Session: sid, Action: "create", Params: map[string]any{"url": url}}, &out)
	return
}
func (b *Bridge) Call(ctx context.Context, sid, tid, method string, p map[string]any, out any) error {
	return b.request(ctx, BridgeRequest{Session: sid, Action: "cdp", Target: tid, Method: method, Params: p}, out)
}
func (b *Bridge) Activate(ctx context.Context, sid, tid string) error {
	return b.request(ctx, BridgeRequest{Session: sid, Action: "activate", Target: tid}, nil)
}
func (b *Bridge) CloseTab(ctx context.Context, sid, tid string) error {
	return b.request(ctx, BridgeRequest{Session: sid, Action: "close", Target: tid}, nil)
}
func (b *Bridge) CloseSession(sid string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return b.request(ctx, BridgeRequest{Session: sid, Action: "close-session"}, nil)
}
func (b *Bridge) Close() error { b.once.Do(func() { close(b.closed) }); return nil }
func (b *Bridge) Legacy(ctx context.Context, sid string, p map[string]any, out any) error {
	return b.request(ctx, BridgeRequest{Session: sid, Action: "extension-action", Params: p}, out)
}

// Native forwards the Svolo extension domain through its trusted main-process adapter.
// It is not a general HTTP proxy and does not bypass the configured domain policy.
func (b *Bridge) Native(ctx context.Context, sid, route string, args map[string]any) (any, error) {
	var out any
	err := b.request(ctx, BridgeRequest{Session: sid, Action: "native", Params: map[string]any{"route": route, "arguments": args}}, &out)
	return out, err
}

// Authorize must be checked at the native boundary, not only when dequeuing.
// A cancelled request is never authorized even if already delivered to a host.
type controlContextKey struct{}
type controlLease struct {
	Actor string
	Epoch uint64
}
type bridgeLease struct {
	Request BridgeRequest
	Context context.Context
}

func (b *Bridge) Authorize(id string, engine *Engine) (BridgeRequest, error) {
	b.mu.Lock()
	lease, ok := b.leases[id]
	b.mu.Unlock()
	if !ok || lease.Context.Err() != nil || time.Now().After(lease.Request.Deadline) {
		return BridgeRequest{}, errors.New("native_lease_expired: do not replay")
	}
	r := lease.Request
	if r.Actor != "" {
		current, e := engine.Control(r.Session, "")
		if e != nil {
			return BridgeRequest{}, e
		}
		if current.Epoch != r.Epoch || (r.Actor == "agent" && current.Owner != "agent") {
			return BridgeRequest{}, errors.New("native_lease_revoked: human takeover")
		}
	}
	return r, nil
}
