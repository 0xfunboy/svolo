package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

type Event struct {
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params"`
	SessionID string          `json:"sessionId,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type response struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}
type Client struct {
	s       *socket
	seq     atomic.Uint64
	mu      sync.Mutex
	pending map[uint64]chan response
	subs    map[uint64]chan Event
	nextSub uint64
	done    chan struct{}
	once    sync.Once
	err     error
	dropped atomic.Uint64
}

func Connect(ctx context.Context, endpoint string) (*Client, error) {
	s, e := dial(ctx, endpoint)
	if e != nil {
		return nil, e
	}
	c := &Client{s: s, pending: map[uint64]chan response{}, subs: map[uint64]chan Event{}, done: make(chan struct{})}
	go c.readLoop()
	return c, nil
}
func (c *Client) Call(ctx context.Context, session, method string, params any, out any) error {
	id := c.seq.Add(1)
	ch := make(chan response, 1)
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return errors.New("CDP disconnected")
	default:
	}
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	msg := map[string]any{"id": id, "method": method, "params": params}
	if session != "" {
		msg["sessionId"] = session
	}
	b, e := json.Marshal(msg)
	if e != nil {
		return e
	}
	if e = c.s.write(1, b); e != nil {
		c.fail(e)
		return e
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return errors.New("CDP connection closed; outcome of outstanding action is unknown")
	case r := <-ch:
		if r.Error != nil {
			return fmt.Errorf("CDP %s (%d): %s", method, r.Error.Code, r.Error.Message)
		}
		if out != nil {
			return json.Unmarshal(r.Result, out)
		}
		return nil
	}
}
func (c *Client) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer < 1 {
		buffer = 1
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextSub++
	id := c.nextSub
	ch := make(chan Event, buffer)
	select {
	case <-c.done:
		close(ch)
	default:
		c.subs[id] = ch
	}
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			c.mu.Lock()
			if _, ok := c.subs[id]; ok {
				delete(c.subs, id)
				close(ch)
			}
			c.mu.Unlock()
		})
	}
}
func (c *Client) DroppedEvents() uint64 { return c.dropped.Load() }
func (c *Client) Close() error          { c.fail(errors.New("closed")); return nil }
func (c *Client) Done() <-chan struct{} { return c.done }
func (c *Client) fail(e error) {
	c.once.Do(func() {
		c.mu.Lock()
		c.err = e
		close(c.done)
		for id, ch := range c.subs {
			close(ch)
			delete(c.subs, id)
		}
		c.mu.Unlock()
		c.s.conn.Close()
	})
}
func (c *Client) readLoop() {
	for {
		b, e := c.s.read()
		if e != nil {
			c.fail(e)
			return
		}
		var head struct {
			ID uint64 `json:"id"`
		}
		if e = json.Unmarshal(b, &head); e != nil {
			c.fail(e)
			return
		}
		if head.ID != 0 {
			var r response
			if json.Unmarshal(b, &r) != nil {
				continue
			}
			c.mu.Lock()
			ch := c.pending[r.ID]
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- r:
				default:
				}
			}
		} else {
			var event Event
			if json.Unmarshal(b, &event) != nil {
				continue
			}
			c.mu.Lock()
			for _, ch := range c.subs {
				select {
				case ch <- event:
				default:
					c.dropped.Add(1)
				}
			}
			c.mu.Unlock()
		}
	}
}
