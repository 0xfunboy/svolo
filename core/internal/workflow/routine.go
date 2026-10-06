// Package workflow executes bounded, persisted JSON graphs. An interrupted action
// is never replayed automatically; explicit resume is only allowed from a human node.
package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"svolo.local/core/internal/store"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Guard struct {
	Path  string `json:"path"`
	Op    string `json:"op"`
	Value any    `json:"value,omitempty"`
}
type Node struct {
	Tool      string          `json:"tool,omitempty"`
	Args      json.RawMessage `json:"args,omitempty"`
	Next      string          `json:"next,omitempty"`
	OnError   json.RawMessage `json:"on_error,omitempty"`
	Save      string          `json:"save,omitempty"`
	Guard     *Guard          `json:"guard,omitempty"`
	Then      string          `json:"then,omitempty"`
	Else      string          `json:"else,omitempty"`
	Terminal  string          `json:"terminal,omitempty"`
	Message   string          `json:"message,omitempty"`
	HITL      string          `json:"hitl,omitempty"`
	Resume    string          `json:"resume,omitempty"`
	MaxVisits int             `json:"max_visits,omitempty"`
}
type Graph struct {
	Entry         string          `json:"entry"`
	MaxSteps      int             `json:"max_steps"`
	MaxDurationMS int             `json:"max_duration_ms,omitempty"`
	KeepBrowser   bool            `json:"keep_browser,omitempty"`
	Nodes         map[string]Node `json:"nodes"`
}
type State struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Session      string         `json:"session"`
	Status       string         `json:"status"`
	Node         string         `json:"node"`
	Steps        int            `json:"steps"`
	Variables    map[string]any `json:"variables"`
	Visits       map[string]int `json:"visits"`
	Started      time.Time      `json:"started"`
	Error        string         `json:"error,omitempty"`
	OwnedBrowser bool           `json:"ownedBrowser"`
	Graph        Graph          `json:"graph"`
}
type Executor func(context.Context, string, string, map[string]any) (any, error)
type Runner struct {
	Store        *store.Store
	Root         string
	Execute      Executor
	Notify       func(string, string)
	CloseBrowser func(string) error
	mu           sync.Mutex
	active       map[string]bool
}

func New(st *store.Store, root string) *Runner {
	return &Runner{Store: st, Root: root, active: map[string]bool{}}
}
func (g *Graph) Validate() error {
	if len(g.Nodes) == 0 || len(g.Nodes) > 1000 {
		return errors.New("graph must have 1..1000 nodes")
	}
	if _, ok := g.Nodes[g.Entry]; !ok {
		return errors.New("entry node not found")
	}
	if g.MaxSteps == 0 {
		g.MaxSteps = 100
	}
	if g.MaxSteps < 1 || g.MaxSteps > 1000 {
		return errors.New("max_steps must be 1..1000")
	}
	if g.MaxDurationMS == 0 {
		g.MaxDurationMS = 600000
	}
	if g.MaxDurationMS < 1 || g.MaxDurationMS > 1200000 {
		return errors.New("max_duration_ms out of range")
	}
	for name, n := range g.Nodes {
		kinds := 0
		for _, v := range []bool{n.Tool != "", n.Guard != nil, n.Terminal != "", n.HITL != ""} {
			if v {
				kinds++
			}
		}
		if kinds != 1 {
			return fmt.Errorf("node %s must have one execution kind", name)
		}
		if n.Terminal != "" && n.Terminal != "success" && n.Terminal != "failure" {
			return fmt.Errorf("node %s has invalid terminal", name)
		}
		if n.MaxVisits < 0 || n.MaxVisits > 1000 {
			return errors.New("max_visits out of range")
		}
		for _, next := range []string{n.Next, n.Then, n.Else, n.Resume} {
			if next != "" {
				if _, ok := g.Nodes[next]; !ok {
					return fmt.Errorf("node %s links to absent node %s", name, next)
				}
			}
		}
		if len(n.OnError) > 0 {
			var dest string
			if json.Unmarshal(n.OnError, &dest) == nil {
				if _, ok := g.Nodes[dest]; !ok {
					return errors.New("on_error target missing")
				}
			} else {
				var routes map[string]string
				if json.Unmarshal(n.OnError, &routes) != nil {
					return errors.New("invalid on_error")
				}
				for _, dest := range routes {
					if _, ok := g.Nodes[dest]; !ok {
						return errors.New("on_error target missing")
					}
				}
			}
		}
	}
	return nil
}
func (r *Runner) Start(ctx context.Context, name, sid string, vars map[string]any) (State, error) {
	if !store.ValidID(name) || !store.ValidID(sid) {
		return State{}, errors.New("invalid routine/session name")
	}
	b, err := os.ReadFile(filepath.Join(r.Root, name+".json"))
	if err != nil {
		return State{}, err
	}
	if len(b) > 1<<20 {
		return State{}, errors.New("routine exceeds 1 MiB")
	}
	var g Graph
	if err = json.Unmarshal(b, &g); err != nil {
		return State{}, err
	}
	if err = g.Validate(); err != nil {
		return State{}, err
	}
	if vars == nil {
		vars = map[string]any{}
	}
	s := State{ID: store.ID(), Name: name, Session: sid, Status: "running", Node: g.Entry, Variables: vars, Visits: map[string]int{}, Started: time.Now().UTC(), Graph: g}
	return r.run(ctx, s)
}
func (r *Runner) Resume(ctx context.Context, id, sid string, vars map[string]any) (State, error) {
	if !store.ValidID(id) {
		return State{}, errors.New("invalid routine run id")
	}
	var s State
	if err := r.Store.Read("routine-"+id, &s); err != nil {
		return State{}, err
	}
	if s.Session != sid {
		return s, errors.New("routine belongs to another session")
	}
	if s.Status != "waiting_human" {
		return s, errors.New("only a human-suspended routine may resume; uncertain actions are not replayed")
	}
	for k, v := range vars {
		s.Variables[k] = v
	}
	s.Status = "running"
	return r.run(ctx, s)
}
func (r *Runner) run(ctx context.Context, s State) (State, error) {
	r.mu.Lock()
	if r.active[s.ID] {
		r.mu.Unlock()
		return s, errors.New("routine already active")
	}
	r.active[s.ID] = true
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.active, s.ID); r.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.Graph.MaxDurationMS)*time.Millisecond)
	defer cancel()
	save := func() error { return r.Store.Write("routine-"+s.ID, s) }
	fail := func(err error) (State, error) {
		s.Status = "failed"
		s.Error = err.Error()
		_ = save()
		if s.OwnedBrowser && !s.Graph.KeepBrowser && r.CloseBrowser != nil {
			_ = r.CloseBrowser(s.Session)
		}
		return s, err
	}
	for s.Steps < s.Graph.MaxSteps {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		n, ok := s.Graph.Nodes[s.Node]
		if !ok {
			return fail(errors.New("graph reached missing node"))
		}
		s.Steps++
		s.Visits[s.Node]++
		if n.MaxVisits > 0 && s.Visits[s.Node] > n.MaxVisits {
			return fail(errors.New("node visit budget exhausted"))
		}
		s.Status = "running"
		if err := save(); err != nil {
			return fail(err)
		}
		if n.Terminal != "" {
			s.Status = n.Terminal
			s.Error = ""
			if n.Terminal == "failure" {
				s.Error = Template(n.Message, s.Variables)
			}
			if err := save(); err != nil {
				return fail(err)
			}
			if s.OwnedBrowser && !s.Graph.KeepBrowser && r.CloseBrowser != nil {
				if err := r.CloseBrowser(s.Session); err != nil {
					return fail(err)
				}
			}
			if n.Terminal == "failure" {
				return s, errors.New(s.Error)
			}
			return s, nil
		}
		if n.Guard != nil {
			pass, err := testGuard(*n.Guard, s.Variables)
			if err != nil {
				return fail(err)
			}
			if pass {
				s.Node = n.Then
			} else {
				s.Node = n.Else
			}
			continue
		}
		if n.HITL != "" {
			message := Template(n.HITL, s.Variables)
			s.Node = n.Resume
			s.Status = "waiting_human"
			if err := save(); err != nil {
				return fail(err)
			}
			if r.Notify != nil {
				r.Notify(s.Session, message)
			}
			_, err := r.Store.Append(s.Session, "routine.waiting_human", map[string]any{"id": s.ID, "message": message})
			return s, err
		}
		args, err := Arguments(n.Tool, n.Args, s.Variables)
		if err != nil {
			return fail(err)
		}
		// Persist the uncertain boundary before executing any external side effect.
		s.Status = "executing"
		if err := save(); err != nil {
			return fail(err)
		}
		result, toolErr := r.Execute(ctx, s.Session, n.Tool, args)
		s.Status = "running"
		if toolErr != nil {
			s.Variables["_error"] = map[string]any{"message": toolErr.Error(), "kind": errorKind(toolErr.Error())}
			dest := ""
			if len(n.OnError) > 0 {
				if json.Unmarshal(n.OnError, &dest) != nil {
					var routes map[string]string
					_ = json.Unmarshal(n.OnError, &routes)
					dest = routes[errorKind(toolErr.Error())]
					if dest == "" {
						dest = routes["*"]
					}
				}
			}
			if dest == "" {
				return fail(toolErr)
			}
			s.Node = dest
		} else {
			if n.Tool == "open-browser" {
				s.OwnedBrowser = true
			}
			if n.Save != "" {
				s.Variables[n.Save] = result
			}
			delete(s.Variables, "_error")
			s.Node = n.Next
		}
		if err := save(); err != nil {
			return fail(err)
		}
	}
	return fail(errors.New("routine step budget exhausted"))
}

var templateRE = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.-]+)\s*\}\}`)

func lookup(vars map[string]any, path string) (any, bool) {
	var v any = vars
	for _, part := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return v, true
}
func Template(s string, vars map[string]any) string {
	return templateRE.ReplaceAllStringFunc(s, func(key string) string {
		matches := templateRE.FindStringSubmatch(key)
		v, ok := lookup(vars, matches[1])
		if !ok {
			return key
		}
		if text, ok := v.(string); ok {
			return text
		}
		b, _ := json.Marshal(v)
		return string(b)
	})
}
func expand(v any, vars map[string]any) any {
	switch x := v.(type) {
	case string:
		matches := templateRE.FindStringSubmatch(x)
		if len(matches) > 0 && matches[0] == x {
			if v, ok := lookup(vars, matches[1]); ok {
				return v
			}
		}
		return Template(x, vars)
	case map[string]any:
		out := map[string]any{}
		for k, v := range x {
			out[k] = expand(v, vars)
		}
		return out
	case []any:
		out := []any{}
		for _, v := range x {
			out = append(out, expand(v, vars))
		}
		return out
	}
	return v
}
func Arguments(tool string, b json.RawMessage, vars map[string]any) (map[string]any, error) {
	if len(b) == 0 {
		return map[string]any{}, nil
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	v = expand(v, vars)
	if m, ok := v.(map[string]any); ok {
		return m, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, errors.New("routine args must be object or array")
	}
	names := map[string][]string{"open-browser": {"url"}, "navigate": {"url"}, "click": {"target"}, "fill": {"text", "target"}, "type-text": {"text", "target"}, "press-key": {"key"}, "assert-url": {"expected", "match"}, "assert-title": {"expected", "match"}, "assert-visible": {"target"}, "assert-text": {"target", "text", "match"}, "assert-image-ready": {"target"}, "screenshot": {"target"}, "wait-for": {"condition", "value", "seconds"}, "wait": {"condition", "value", "seconds"}, "evaluate-js": {"expression"}, "verify-artifact": {"id"}, "wait-download": {"afterMs", "seconds", "nameContains"}, "scroll": {"destination"}, "select": {"target", "value"}, "check": {"target"}}[tool]
	if names == nil {
		return nil, fmt.Errorf("positional arguments unsupported for %s; use named object arguments", tool)
	}
	if len(list) > len(names) {
		return nil, fmt.Errorf("too many arguments for %s; semantic flags are not silently ignored", tool)
	}
	out := map[string]any{}
	for i, x := range list {
		if names[i] == "seconds" || names[i] == "afterMs" {
			if s, ok := x.(string); ok {
				var n json.Number
				if json.Unmarshal([]byte(s), &n) == nil {
					f, _ := n.Float64()
					x = f
				}
			}
		}
		out[names[i]] = x
	}
	if tool == "screenshot" {
		out["save"] = true
	}
	return out, nil
}
func testGuard(g Guard, vars map[string]any) (bool, error) {
	v, ok := lookup(vars, g.Path)
	switch g.Op {
	case "exists":
		return ok, nil
	case "true":
		return v == true, nil
	case "false":
		return v == false, nil
	case "equals":
		return reflect.DeepEqual(v, g.Value), nil
	case "not_equals":
		return !reflect.DeepEqual(v, g.Value), nil
	case "error_kind":
		return v == g.Value, nil
	}
	return false, errors.New("unknown guard operator")
}
func errorKind(message string) string {
	for _, kind := range []string{"target_stale", "target_not_found", "target_not_visible", "condition_failed", "condition_timeout", "control_not_owned"} {
		if strings.Contains(message, kind) {
			return kind
		}
	}
	return "tool_error"
}
