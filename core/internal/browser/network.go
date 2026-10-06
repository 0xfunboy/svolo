package browser

import (
	"context"
	"encoding/json"
	"net/url"
	"sync"
	"time"
)

// ObserveEvent records metadata only. Authorization headers, cookies, request/response bodies
// and query strings are intentionally excluded from the default persistent trace.
func (e *Engine) ObserveEvent(sid, method string, params json.RawMessage) {
	if method != "Network.requestWillBeSent" && method != "Network.responseReceived" && method != "Network.loadingFailed" && method != "Runtime.consoleAPICalled" {
		return
	}
	var p map[string]any
	if json.Unmarshal(params, &p) != nil {
		return
	}
	item := map[string]any{"at": time.Now().UTC(), "method": method, "requestId": p["requestId"]}
	for _, key := range []string{"request", "response"} {
		if m, ok := p[key].(map[string]any); ok {
			for _, k := range []string{"url", "method", "status", "mimeType"} {
				if k == "url" {
					if s, ok := m[k].(string); ok {
						u, er := url.Parse(s)
						if er == nil {
							u.RawQuery = ""
							u.Fragment = ""
							u.User = nil
							item[k] = u.String()
						}
					}
				} else {
					item[k] = m[k]
				}
			}
		}
	}
	if method == "Network.loadingFailed" {
		item["errorText"] = p["errorText"]
	}
	if method == "Runtime.consoleAPICalled" {
		item["level"] = p["type"]
		var args []any
		if a, ok := p["args"].([]any); ok {
			for _, arg := range a {
				if m, ok := arg.(map[string]any); ok {
					v := m["value"]
					if s, ok := v.(string); ok && len(s) > 1000 {
						v = s[:1000]
					}
					args = append(args, v)
				}
			}
		}
		item["arguments"] = args
	}
	e.networkMu.Lock()
	defer e.networkMu.Unlock()
	if _, active := e.networkStops[sid]; !active {
		return
	}
	e.network[sid] = append(e.network[sid], item)
	if len(e.network[sid]) > 1000 {
		e.network[sid] = e.network[sid][len(e.network[sid])-1000:]
	}
}
func (e *Engine) inspectNetwork(ctx context.Context, sid, tid string, a map[string]any) (any, error) {
	action := str(a, "action")
	if action == "" {
		action = "show"
	}
	if action == "start" {
		e.networkMu.Lock()
		_, active := e.networkStops[sid]
		e.networkMu.Unlock()
		if !active {
			var stop func() = func() {}
			if m, ok := e.Transport.(*Managed); ok {
				client, err := m.Client(ctx, sid)
				if err != nil {
					return nil, err
				}
				events, unsub := client.Subscribe(1024)
				var once sync.Once
				stop = func() { once.Do(unsub) }
				go func() {
					for ev := range events {
						e.ObserveEvent(sid, ev.Method, ev.Params)
					}
				}()
			}
			e.networkMu.Lock()
			e.networkStops[sid] = stop
			e.network[sid] = []map[string]any{}
			e.networkMu.Unlock()
		}
		if _, err := e.call(ctx, sid, tid, "Network.enable", map[string]any{}); err != nil {
			return nil, err
		}
		if _, err := e.call(ctx, sid, tid, "Runtime.enable", map[string]any{}); err != nil {
			return nil, err
		}
	} else if action == "stop" {
		e.networkMu.Lock()
		stop := e.networkStops[sid]
		delete(e.networkStops, sid)
		e.networkMu.Unlock()
		if stop != nil {
			stop()
		}
		_, _ = e.call(ctx, sid, tid, "Network.disable", map[string]any{})
	}
	e.networkMu.Lock()
	defer e.networkMu.Unlock()
	out := make([]map[string]any, len(e.network[sid]))
	copy(out, e.network[sid])
	return map[string]any{"active": e.networkStops[sid] != nil, "entries": out, "maxEntries": 1000, "redacted": true}, nil
}
