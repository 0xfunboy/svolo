package browser

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"svolo.local/core/internal/store"
	"sync"
	"time"
)

//go:embed page.js
var pageJS string

type Control struct {
	Owner  string `json:"owner"`
	Epoch  uint64 `json:"epoch"`
	Active bool   `json:"active"`
	Tab    string `json:"tab,omitempty"`
}
type reference struct {
	Epoch uint64
	Tab   string
}
type session struct {
	op             sync.Mutex
	mu             sync.Mutex
	control        Control
	target         string
	cancel         context.CancelFunc
	refs           map[string]reference
	extensionEpoch uint64
}
type Engine struct {
	Transport    Transport
	mu           sync.Mutex
	sessions     map[string]*session
	Data         string
	Events       func(string, string, any)
	networkMu    sync.Mutex
	network      map[string][]map[string]any
	networkStops map[string]func()
}

func NewEngine(t Transport, data string) *Engine {
	return &Engine{Transport: t, Data: data, sessions: map[string]*session{}, network: map[string][]map[string]any{}, networkStops: map[string]func(){}}
}
func (e *Engine) get(sid string) (*session, error) {
	if !store.ValidID(sid) {
		return nil, errors.New("invalid session id")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.sessions[sid]
	if s == nil {
		s = &session{control: Control{Owner: "human", Epoch: 1}, refs: map[string]reference{}}
		e.sessions[sid] = s
	}
	return s, nil
}
func (e *Engine) Control(sid, owner string) (Control, error) {
	s, err := e.get(sid)
	if err != nil {
		return Control{}, err
	}
	s.mu.Lock()
	if owner != "" {
		if owner != "human" && owner != "agent" {
			s.mu.Unlock()
			return Control{}, errors.New("owner must be human or agent")
		}
		s.control.Owner = owner
		s.control.Epoch++
		if s.cancel != nil {
			s.cancel()
		}
	}
	c := s.control
	c.Tab = s.target
	s.mu.Unlock()
	if owner != "" && e.Events != nil {
		e.Events(sid, "browser.control", c)
	}
	return c, nil
}
func (e *Engine) SessionIDs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []string{}
	for id := range e.sessions {
		out = append(out, id)
	}
	return out
}
func Canonical(name string) string {
	aliases := map[string]string{"browser_snapshot": "snapshot-interactive", "browser_open": "navigate", "browser_click": "click", "browser_type": "fill", "browser_press": "press-key", "browser_screenshot": "screenshot", "browser_evaluate": "evaluate-js", "browser_console": "console", "browser_state": "state", "browser_viewport": "viewport", "snapshot": "snapshot-interactive", "read": "read-page", "new-tab": "open-tab"}
	if x := aliases[name]; x != "" {
		return x
	}
	return name
}
func IsRead(name string) bool {
	switch Canonical(name) {
	case "state", "tabs", "snapshot-interactive", "find-interactive", "read-page", "inspect-inputs", "inspect-elements", "element-info", "query-selector", "get-element", "accessibility-tree", "inspect-links", "inspect-images", "assert-url", "assert-title", "assert-visible", "assert-text", "assert-image-ready", "screenshot", "wait", "wait-for", "downloads", "verify-artifact", "wait-download", "console":
		return true
	}
	return false
}
func (e *Engine) Run(ctx context.Context, sid, actor, name string, args map[string]any) (any, error) {
	name = Canonical(name)
	if actor == "human" && IsRead(name) {
		return e.Observe(ctx, sid, name, args)
	}
	if args == nil {
		args = map[string]any{}
	}
	s, err := e.get(sid)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	epoch := s.control.Epoch
	s.mu.Unlock()
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	if actor != "human" && (s.control.Owner != "agent" || s.control.Epoch != epoch) {
		s.mu.Unlock()
		return nil, errors.New("control_not_owned: user takeover or stale queued action")
	}
	if actor != "human" && actor != "agent" {
		s.mu.Unlock()
		return nil, errors.New("invalid actor")
	}
	for _, key := range []string{"target", "source", "destination", "value"} {
		ref, _ := args[key].(string)
		if strings.HasPrefix(ref, "@") {
			doc := strings.SplitN(strings.TrimPrefix(ref, "@"), ":", 2)[0]
			if s.refs[doc].Epoch != s.control.Epoch {
				s.mu.Unlock()
				return nil, errors.New("target_stale: control changed; take a new snapshot")
			}
		}
	}
	if name == "extension-action" && (args["action"] == "click" || args["action"] == "type") && s.extensionEpoch != s.control.Epoch {
		s.mu.Unlock()
		return nil, errors.New("target_stale: take browser_snapshot after taking or returning control")
	}
	ctx, cancel := context.WithTimeout(ctx, 70*time.Second)
	s.cancel = cancel
	s.control.Active = true
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); s.cancel = nil; s.control.Active = false; s.mu.Unlock() }()
	ctx = context.WithValue(ctx, controlContextKey{}, controlLease{Actor: actor, Epoch: epoch})
	result, err := e.dispatch(ctx, sid, s, name, args)
	if err == nil && name == "extension-action" && (args["action"] == "snapshot" || args["action"] == "open" || args["action"] == "back" || args["action"] == "click" || args["action"] == "type") {
		s.mu.Lock()
		s.extensionEpoch = epoch
		s.mu.Unlock()
	}
	if e.Events != nil {
		d := map[string]any{"tool": name, "ok": err == nil}
		if err != nil {
			d["error"] = err.Error()
		}
		e.Events(sid, "browser.action", d)
	}
	return result, err
}

type observationContextKey struct{}
type observationTarget struct {
	Tab string
}

// Observe reads an owned page without joining the action queue, changing the
// selected target, or modifying/cancelling the current control lease. CDP calls
// may run concurrently with an agent wait or navigation.
func (e *Engine) Observe(ctx context.Context, sid, name string, args map[string]any) (any, error) {
	name = Canonical(name)
	if !IsRead(name) {
		return nil, errors.New("observation_requires_read_operation")
	}
	if args == nil {
		args = map[string]any{}
	}
	s, err := e.get(sid)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	selected := s.target
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 70*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, observationContextKey{}, observationTarget{Tab: selected})
	return e.dispatch(ctx, sid, s, name, args)
}

func (e *Engine) tabs(ctx context.Context, sid string, s *session) ([]Target, error) {
	tabs, err := e.Transport.List(ctx, sid)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	selected := s.target
	s.mu.Unlock()
	if observed, ok := ctx.Value(observationContextKey{}).(observationTarget); ok {
		selected = observed.Tab
	}
	for i := range tabs {
		tabs[i].Active = tabs[i].ID == selected
	}
	return tabs, nil
}

func (s *session) selectTarget(id string) {
	s.mu.Lock()
	s.target = id
	s.mu.Unlock()
}

func (e *Engine) target(ctx context.Context, sid string, s *session, a map[string]any) (string, error) {
	tabs, err := e.Transport.List(ctx, sid)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	selected := s.target
	refTab := ""
	for _, key := range []string{"target", "source", "destination", "value"} {
		ref := str(a, key)
		if !strings.HasPrefix(ref, "@") {
			continue
		}
		doc := strings.SplitN(strings.TrimPrefix(ref, "@"), ":", 2)[0]
		bound, ok := s.refs[doc]
		if !ok || bound.Epoch != s.control.Epoch {
			s.mu.Unlock()
			return "", errors.New("target_stale: control changed; take a new snapshot")
		}
		if refTab != "" && refTab != bound.Tab {
			s.mu.Unlock()
			return "", errors.New("target_tab_mismatch: references belong to different tabs")
		}
		refTab = bound.Tab
	}
	s.mu.Unlock()
	observed, observation := ctx.Value(observationContextKey{}).(observationTarget)
	if observation {
		selected = observed.Tab
	}
	id := str(a, "tab")
	if id != "" && refTab != "" && id != refTab {
		return "", errors.New("target_tab_mismatch: reference belongs to another tab")
	}
	if id == "" && refTab != "" {
		id = refTab
	}
	if id != "" {
		for _, t := range tabs {
			if t.ID == id {
				if !observation {
					s.selectTarget(id)
				}
				return id, nil
			}
		}
		return "", errors.New("target_not_owned_or_closed")
	}
	for _, t := range tabs {
		if t.ID == selected {
			return selected, nil
		}
	}
	if len(tabs) > 1 {
		return "", errors.New("tab_ambiguous: specify an owned tab ID")
	}
	if len(tabs) == 1 {
		if !observation {
			s.selectTarget(tabs[0].ID)
		}
		return tabs[0].ID, nil
	}
	if observation {
		return "", errors.New("target_not_owned_or_closed")
	}
	t, err := e.Transport.Create(ctx, sid, "about:blank")
	if err == nil {
		s.selectTarget(t.ID)
	}
	return t.ID, err
}
func ValidURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if raw == "about:blank" {
		return nil
	}
	if (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
		return errors.New("only http(s) and about:blank navigation allowed; no embedded credentials")
	}
	return nil
}
func str(a map[string]any, k string) string { v, _ := a[k].(string); return v }
func number(a map[string]any, k string, def float64) float64 {
	switch v := a[k].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case json.Number:
		n, _ := v.Float64()
		return n
	}
	return def
}
func (e *Engine) call(ctx context.Context, sid, tid, method string, a map[string]any) (any, error) {
	var out any
	err := e.Transport.Call(ctx, sid, tid, method, a, &out)
	return out, err
}
func (e *Engine) evaluate(ctx context.Context, sid, tid, expression string, byValue bool) (any, error) {
	var out struct {
		Result struct {
			Value          any    `json:"value"`
			ObjectID       string `json:"objectId"`
			Unserializable string `json:"unserializableValue"`
			Description    string `json:"description"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	err := e.Transport.Call(ctx, sid, tid, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": byValue, "awaitPromise": true, "userGesture": true}, &out)
	if err != nil {
		return nil, err
	}
	if len(out.Exception) > 0 && string(out.Exception) != "null" {
		return nil, fmt.Errorf("page_script_error: %s", out.Exception)
	}
	if !byValue {
		return out.Result.ObjectID, nil
	}
	if out.Result.Unserializable != "" {
		return out.Result.Unserializable, nil
	}
	return out.Result.Value, nil
}
func (e *Engine) page(ctx context.Context, sid, tid, op string, a map[string]any) (any, error) {
	b, _ := json.Marshal(a)
	o, _ := json.Marshal(op)
	value, err := e.evaluate(ctx, sid, tid, pageJS+"("+string(o)+","+string(b)+")", true)
	if out, ok := value.(map[string]any); ok && (op == "state" || op == "snapshot-interactive" || op == "find-interactive" || op == "read-page") {
		out["tab"] = tid
	}
	if err == nil && (op == "snapshot-interactive" || op == "find-interactive") {
		if out, ok := value.(map[string]any); ok {
			if doc, ok := out["document"].(string); ok {
				session, _ := e.get(sid)
				session.mu.Lock()
				if len(session.refs) > 1000 {
					session.refs = map[string]reference{}
				}
				epoch := session.control.Epoch
				if lease, ok := ctx.Value(controlContextKey{}).(controlLease); ok {
					epoch = lease.Epoch
				}
				session.refs[doc] = reference{Epoch: epoch, Tab: tid}
				session.mu.Unlock()
			}
		}
	}
	return value, err
}
func (e *Engine) dispatch(ctx context.Context, sid string, s *session, name string, a map[string]any) (any, error) {
	switch name {
	case "extension-action":
		br, ok := e.Transport.(*Bridge)
		if !ok {
			return nil, errors.New("extension adapter requires Electron transport")
		}
		var out any
		if err := br.Legacy(ctx, sid, a, &out); err != nil {
			return nil, err
		}
		return out, nil

	case "tabs":
		return e.tabs(ctx, sid, s)
	case "close-browser":
		s.selectTarget("")
		return map[string]any{"closed": true}, e.Transport.CloseSession(sid)
	case "open-browser", "open-tab":
		u := str(a, "url")
		if u == "" {
			u = "about:blank"
		}
		if err := ValidURL(u); err != nil {
			return nil, err
		}
		t, err := e.Transport.Create(ctx, sid, u)
		if err == nil {
			s.selectTarget(t.ID)
		}
		return t, err
	case "switch-tab":
		tabs, err := e.Transport.List(ctx, sid)
		if err != nil {
			return nil, err
		}
		query := str(a, "query")
		if query == "" {
			query = str(a, "tab")
		}
		var matched []Target
		for _, t := range tabs {
			if t.ID == query || t.URL == query || t.Title == query {
				matched = append(matched, t)
			}
		}
		if len(matched) != 1 {
			return nil, errors.New("tab match must be unique")
		}
		if err = e.Transport.Activate(ctx, sid, matched[0].ID); err != nil {
			return nil, err
		}
		s.selectTarget(matched[0].ID)
		return matched[0], nil
	}
	tid, err := e.target(ctx, sid, s, a)
	if err != nil {
		return nil, err
	}
	switch name {
	case "navigate":
		u := str(a, "url")
		if err := ValidURL(u); err != nil {
			return nil, err
		}
		var r struct {
			ErrorText string `json:"errorText"`
		}
		if err = e.Transport.Call(ctx, sid, tid, "Page.navigate", map[string]any{"url": u}, &r); err != nil {
			return nil, err
		}
		if r.ErrorText != "" {
			return nil, errors.New(r.ErrorText)
		}
		return map[string]any{"tab": tid, "requestedURL": u, "navigationDispatched": true, "note": "Use assertions to verify the destination and application state."}, nil
	case "close-tab":
		if err = e.Transport.CloseTab(ctx, sid, tid); err != nil {
			return nil, err
		}
		s.mu.Lock()
		if s.target == tid {
			s.target = ""
		}
		s.mu.Unlock()
		return map[string]any{"closed": tid}, nil
	case "open-in-new-tab":
		v, err := e.page(ctx, sid, tid, "href", a)
		if err != nil {
			return nil, err
		}
		u, _ := v.(string)
		if err = ValidURL(u); err != nil {
			return nil, err
		}
		t, err := e.Transport.Create(ctx, sid, u)
		if err == nil {
			s.selectTarget(t.ID)
		}
		return t, err
	case "tab-history":
		var h struct {
			Current int `json:"currentIndex"`
			Entries []struct {
				ID int `json:"id"`
			} `json:"entries"`
		}
		if err = e.Transport.Call(ctx, sid, tid, "Page.getNavigationHistory", map[string]any{}, &h); err != nil {
			return nil, err
		}
		i := h.Current - 1
		if str(a, "direction") == "forward" {
			i = h.Current + 1
		}
		if i < 0 || i >= len(h.Entries) {
			return nil, errors.New("no history entry")
		}
		return e.call(ctx, sid, tid, "Page.navigateToHistoryEntry", map[string]any{"entryId": h.Entries[i].ID})
	case "reload":
		return e.call(ctx, sid, tid, "Page.reload", map[string]any{})
	case "click":
		return e.click(ctx, sid, tid, a)
	case "fill", "type-text":
		if str(a, "target") != "" {
			aa := clone(a)
			aa["clear"] = name == "fill"
			if _, err = e.page(ctx, sid, tid, "focus", aa); err != nil {
				return nil, err
			}
		} else if name == "fill" {
			return nil, errors.New("fill requires target")
		}
		return e.call(ctx, sid, tid, "Input.insertText", map[string]any{"text": str(a, "text")})
	case "press-key":
		return map[string]any{"dispatched": true}, e.key(ctx, sid, tid, str(a, "key"))
	case "check":
		v, err := e.page(ctx, sid, tid, "check-state", a)
		if err != nil {
			return nil, err
		}
		if m, ok := v.(map[string]any); ok && m["checked"] == true {
			return v, nil
		}
		if _, err = e.click(ctx, sid, tid, a); err != nil {
			return nil, err
		}
		v, err = e.page(ctx, sid, tid, "check-state", a)
		if err == nil && v.(map[string]any)["checked"] != true {
			return nil, errors.New("condition_failed: checkbox not checked")
		}
		return v, err
	case "dialog":
		action := str(a, "action")
		if action != "accept" && action != "dismiss" {
			return nil, errors.New("dialog action must be accept or dismiss")
		}
		return e.call(ctx, sid, tid, "Page.handleJavaScriptDialog", map[string]any{"accept": action == "accept", "promptText": str(a, "text")})
	case "drag":
		return e.drag(ctx, sid, tid, a)
	case "wait", "wait-for":
		seconds := number(a, "seconds", 10)
		if seconds < 0 || seconds > 60 {
			return nil, errors.New("wait must be between 0 and 60 seconds")
		}
		wctx, cancel := context.WithTimeout(ctx, time.Duration(seconds*float64(time.Second)))
		defer cancel()
		for {
			v, er := e.page(wctx, sid, tid, "condition", a)
			if er == nil && v == true {
				return map[string]any{"verified": true, "condition": a}, nil
			}
			select {
			case <-wctx.Done():
				return nil, fmt.Errorf("condition_timeout: %s", str(a, "condition"))
			case <-time.After(100 * time.Millisecond):
			}
		}
	case "evaluate-js":
		return e.evaluate(ctx, sid, tid, str(a, "expression"), true)
	case "inject-js":
		script := str(a, "script")
		if script == "" {
			return nil, errors.New("script required")
		}
		if a["persistent"] == true {
			if _, err = e.call(ctx, sid, tid, "Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": script}); err != nil {
				return nil, err
			}
		}
		return e.evaluate(ctx, sid, tid, script, true)
	case "accessibility-tree":
		var r struct {
			Nodes []map[string]any `json:"nodes"`
		}
		if err = e.Transport.Call(ctx, sid, tid, "Accessibility.getFullAXTree", map[string]any{}, &r); err != nil {
			return nil, err
		}
		out := []map[string]any{}
		max := int(number(a, "max", 200))
		if max < 1 || max > 2000 {
			return nil, errors.New("max must be 1..2000")
		}
		for _, n := range r.Nodes {
			if n["ignored"] != true {
				out = append(out, n)
				if len(out) >= max {
					break
				}
			}
		}
		return out, nil
	case "screenshot":
		return e.screenshot(ctx, sid, tid, a)
	case "viewport":
		if a["reset"] == true {
			if _, err = e.call(ctx, sid, tid, "Emulation.clearDeviceMetricsOverride", map[string]any{}); err != nil {
				return nil, err
			}
			if _, err = e.call(ctx, sid, tid, "Emulation.setTouchEmulationEnabled", map[string]any{"enabled": false}); err != nil {
				return nil, err
			}
			return e.call(ctx, sid, tid, "Emulation.setUserAgentOverride", map[string]any{"userAgent": ""})
		}
		w, h, dpr := number(a, "width", 1280), number(a, "height", 800), number(a, "dpr", 1)
		if w < 100 || h < 100 || w > 8192 || h > 8192 || dpr < .1 || dpr > 5 {
			return nil, errors.New("invalid viewport")
		}
		if _, err = e.call(ctx, sid, tid, "Emulation.setDeviceMetricsOverride", map[string]any{"width": int(w), "height": int(h), "deviceScaleFactor": dpr, "mobile": a["mobile"] == true}); err != nil {
			return nil, err
		}
		if _, err = e.call(ctx, sid, tid, "Emulation.setTouchEmulationEnabled", map[string]any{"enabled": a["touch"] == true}); err != nil {
			return nil, err
		}
		if ua := str(a, "userAgent"); ua != "" {
			if _, err = e.call(ctx, sid, tid, "Emulation.setUserAgentOverride", map[string]any{"userAgent": ua}); err != nil {
				return nil, err
			}
		}
		return a, nil
	case "upload":
		path := str(a, "file")
		if !filepath.IsAbs(path) {
			return nil, errors.New("upload path must be absolute on the browser host")
		}
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			return nil, errors.New("upload file not found")
		}
		b, _ := json.Marshal(a)
		obj, err := e.evaluate(ctx, sid, tid, pageJS+"(\"upload-object\","+string(b)+")", false)
		if err != nil {
			return nil, err
		}
		id, _ := obj.(string)
		if id == "" {
			return nil, errors.New("file input not found")
		}
		defer e.Transport.Call(context.Background(), sid, tid, "Runtime.releaseObject", map[string]any{"objectId": id}, nil)
		return e.call(ctx, sid, tid, "DOM.setFileInputFiles", map[string]any{"objectId": id, "files": []string{path}})
	case "inspect-network":
		return e.inspectNetwork(ctx, sid, tid, a)
	case "console":
		return e.inspectNetwork(ctx, sid, tid, map[string]any{"action": "show"})
	case "state", "snapshot-interactive", "find-interactive", "read-page", "inspect-inputs", "inspect-elements", "element-info", "query-selector", "get-element", "inspect-links", "inspect-images", "select", "scroll", "assert-url", "assert-title", "assert-visible", "assert-text", "assert-image-ready", "highlight", "clear-highlight":
		return e.page(ctx, sid, tid, name, a)
	case "input":
		return e.input(ctx, sid, tid, a)
	default:
		return nil, fmt.Errorf("unsupported_browser_tool: %s", name)
	}
}
func clone(a map[string]any) map[string]any {
	b := map[string]any{}
	for k, v := range a {
		b[k] = v
	}
	return b
}
func (e *Engine) click(ctx context.Context, sid, tid string, a map[string]any) (any, error) {
	v, err := e.page(ctx, sid, tid, "locate", a)
	if err != nil {
		return nil, err
	}
	p := v.(map[string]any)
	for _, kind := range []string{"mouseMoved", "mousePressed", "mouseReleased"} {
		if _, err = e.call(ctx, sid, tid, "Input.dispatchMouseEvent", map[string]any{"type": kind, "x": p["x"], "y": p["y"], "button": "left", "clickCount": 1}); err != nil {
			return nil, err
		}
	}
	return map[string]any{"dispatched": true, "target": a["target"], "note": "A click is not evidence of task completion."}, nil
}
func (e *Engine) key(ctx context.Context, sid, tid, key string) error {
	parts := strings.Split(key, "+")
	mod := 0
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(p) {
		case "ctrl", "control":
			mod |= 2
		case "alt":
			mod |= 1
		case "shift":
			mod |= 8
		case "meta", "cmd", "command":
			mod |= 4
		default:
			return errors.New("unknown key modifier")
		}
	}
	k := parts[len(parts)-1]
	codes := map[string]int{"Enter": 13, "Tab": 9, "Escape": 27, "Backspace": 8, "Delete": 46, "ArrowLeft": 37, "ArrowUp": 38, "ArrowRight": 39, "ArrowDown": 40, "Home": 36, "End": 35, "PageUp": 33, "PageDown": 34, "Space": 32}
	code := codes[k]
	if code == 0 {
		if len([]rune(k)) != 1 {
			return errors.New("unsupported key")
		}
		code = int([]rune(strings.ToUpper(k))[0])
	}
	for _, kind := range []string{"keyDown", "keyUp"} {
		p := map[string]any{"type": kind, "key": k, "windowsVirtualKeyCode": code, "modifiers": mod}
		if kind == "keyDown" && mod == 0 && len([]rune(k)) == 1 {
			p["text"] = k
		}
		if kind == "keyDown" && k == "Enter" {
			p["text"] = "\r"
		}
		if _, err := e.call(ctx, sid, tid, "Input.dispatchKeyEvent", p); err != nil {
			return err
		}
	}
	return nil
}
func (e *Engine) drag(ctx context.Context, sid, tid string, a map[string]any) (any, error) {
	start, err := e.page(ctx, sid, tid, "locate", map[string]any{"target": str(a, "source")})
	if err != nil {
		return nil, err
	}
	end, err := e.page(ctx, sid, tid, "locate", map[string]any{"target": str(a, "destination")})
	if err != nil {
		return nil, err
	}
	p, q := start.(map[string]any), end.(map[string]any)
	x, y := p["x"].(float64), p["y"].(float64)
	x2, y2 := q["x"].(float64), q["y"].(float64)
	if _, err = e.call(ctx, sid, tid, "Input.dispatchMouseEvent", map[string]any{"type": "mousePressed", "x": x, "y": y, "button": "left", "clickCount": 1}); err != nil {
		return nil, err
	}
	for i := 1; i <= 12; i++ {
		f := float64(i) / 12
		if _, err = e.call(ctx, sid, tid, "Input.dispatchMouseEvent", map[string]any{"type": "mouseMoved", "x": x + (x2-x)*f, "y": y + (y2-y)*f, "button": "left", "buttons": 1}); err != nil {
			return nil, err
		}
	}
	return e.call(ctx, sid, tid, "Input.dispatchMouseEvent", map[string]any{"type": "mouseReleased", "x": x2, "y": y2, "button": "left", "clickCount": 1})
}
func (e *Engine) input(ctx context.Context, sid, tid string, a map[string]any) (any, error) {
	switch str(a, "kind") {
	case "text":
		return e.call(ctx, sid, tid, "Input.insertText", map[string]any{"text": str(a, "text")})
	case "key":
		return map[string]any{"sent": true}, e.key(ctx, sid, tid, str(a, "key"))
	case "click":
		x, y := number(a, "x", -1), number(a, "y", -1)
		if x < 0 || y < 0 || x > 16384 || y > 16384 {
			return nil, errors.New("invalid input coordinates")
		}
		for _, kind := range []string{"mousePressed", "mouseReleased"} {
			if _, err := e.call(ctx, sid, tid, "Input.dispatchMouseEvent", map[string]any{"type": kind, "x": x, "y": y, "button": "left", "clickCount": 1}); err != nil {
				return nil, err
			}
		}
		return map[string]any{"sent": true}, nil
	case "wheel":
		return e.call(ctx, sid, tid, "Input.dispatchMouseEvent", map[string]any{"type": "mouseWheel", "x": number(a, "x", 0), "y": number(a, "y", 0), "deltaX": number(a, "deltaX", 0), "deltaY": number(a, "deltaY", 0)})
	}
	return nil, errors.New("unsupported input kind")
}
func (e *Engine) screenshot(ctx context.Context, sid, tid string, a map[string]any) (any, error) {
	params := map[string]any{"format": "png", "fromSurface": true, "captureBeyondViewport": false}
	if str(a, "target") != "" {
		v, err := e.page(ctx, sid, tid, "locate", a)
		if err != nil {
			return nil, err
		}
		r := v.(map[string]any)["box"].(map[string]any)
		var metrics struct {
			Layout struct{ PageX, PageY float64 } `json:"cssLayoutViewport"`
		}
		if err = e.Transport.Call(ctx, sid, tid, "Page.getLayoutMetrics", map[string]any{}, &metrics); err != nil {
			return nil, err
		}
		r["x"] = number(r, "x", 0) + metrics.Layout.PageX
		r["y"] = number(r, "y", 0) + metrics.Layout.PageY
		r["scale"] = 1
		params["captureBeyondViewport"] = true
		params["clip"] = r
	}
	if a["fullPage"] == true {
		var m struct {
			Content struct{ X, Y, Width, Height float64 } `json:"cssContentSize"`
		}
		if err := e.Transport.Call(ctx, sid, tid, "Page.getLayoutMetrics", map[string]any{}, &m); err != nil {
			return nil, err
		}
		if m.Content.Width*m.Content.Height > 64e6 || m.Content.Width > 16384 || m.Content.Height > 16384 {
			return nil, errors.New("full-page screenshot exceeds pixel budget")
		}
		params["captureBeyondViewport"] = true
		params["clip"] = map[string]any{"x": 0, "y": 0, "width": m.Content.Width, "height": m.Content.Height, "scale": 1}
	}
	var shot struct {
		Data string `json:"data"`
	}
	if err := e.Transport.Call(ctx, sid, tid, "Page.captureScreenshot", params, &shot); err != nil {
		return nil, err
	}
	b, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"mimeType": "image/png", "data": shot.Data, "tab": tid}
	var metrics struct {
		Layout struct{ ClientWidth, ClientHeight float64 } `json:"cssLayoutViewport"`
	}
	if err := e.Transport.Call(ctx, sid, tid, "Page.getLayoutMetrics", map[string]any{}, &metrics); err == nil {
		result["viewport"] = map[string]any{"width": metrics.Layout.ClientWidth, "height": metrics.Layout.ClientHeight}
	}
	if a["save"] == true {
		artifact, err := SaveArtifact(e.Data, sid, "screenshot.png", b)
		if err != nil {
			return nil, err
		}
		result["artifact"] = artifact
	}
	return result, nil
}

// ForgetSession closes the browser and removes its private profile and artifacts.
// The caller must prevent concurrent session operations and running agents.
func (e *Engine) ForgetSession(sid string) error {
	if !store.ValidID(sid) {
		return errors.New("invalid session id")
	}
	if err := e.Transport.CloseSession(sid); err != nil {
		return err
	}
	e.mu.Lock()
	delete(e.sessions, sid)
	e.mu.Unlock()
	e.networkMu.Lock()
	stop := e.networkStops[sid]
	delete(e.networkStops, sid)
	delete(e.network, sid)
	e.networkMu.Unlock()
	if stop != nil {
		stop()
	}
	if err := os.RemoveAll(filepath.Join(e.Data, "profiles", sid)); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(e.Data, "artifacts", sid))
}
