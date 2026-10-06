// Package atp keeps ATP orchestration in the host daemon. The bundled
// librarian remains the single writer of the ATP format, including replanning,
// decomposition, optimistic graph versions and cross-process file locks.
package atp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"svolo.local/core/internal/proc"
	"svolo.local/core/internal/processenv"
	"svolo.local/core/internal/workspace"
	"time"
)

const MaxPlan = 8 << 20
const MaxNodes = 4096

type Node struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Instruction  string   `json:"instruction"`
	Context      string   `json:"context,omitempty"`
	Dependencies []string `json:"dependencies"`
	Status       string   `json:"status"`
	Scope        bool     `json:"scope"`
	Children     []string `json:"children"`
	Closed       string   `json:"closed,omitempty"`
	Worker       string   `json:"worker,omitempty"`
	StartedAt    string   `json:"startedAt,omitempty"`
	CompletedAt  string   `json:"completedAt,omitempty"`
	Report       string   `json:"report,omitempty"`
	Artifacts    []string `json:"artifacts"`
	Effort       string   `json:"effort,omitempty"`
}
type Plan struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Nodes  []Node `json:"nodes"`
}
type PlanFile struct {
	Path       string `json:"path"`
	ModifiedAt int64  `json:"modifiedAt"`
	Plan       *Plan  `json:"plan,omitempty"`
	Error      string `json:"error,omitempty"`
}
type ProjectPlans struct {
	CWD   string     `json:"cwd"`
	Plans []PlanFile `json:"plans"`
}
type Librarian struct {
	Path   string
	Python string
}

func Resolve(root, path string) (string, error) {
	w, e := workspace.Open(root)
	if e != nil {
		return "", e
	}
	if !filepath.IsAbs(path) || !strings.HasSuffix(path, ".atp.json") {
		return "", errors.New("absolute .atp.json path required")
	}
	rel, e := filepath.Rel(w.Root, path)
	if e != nil {
		return "", e
	}
	return w.Path(filepath.ToSlash(rel), false)
}
func Read(root, path string) (PlanFile, error) {
	path, e := Resolve(root, path)
	if e != nil {
		return PlanFile{}, e
	}
	info, e := os.Stat(path)
	if e != nil {
		return PlanFile{}, e
	}
	if info.Size() > MaxPlan {
		return PlanFile{}, errors.New("ATP plan size budget exceeded")
	}
	data, e := os.ReadFile(path)
	if e != nil {
		return PlanFile{}, e
	}
	p, e := Parse(path, data)
	if e != nil {
		return PlanFile{Path: path, ModifiedAt: info.ModTime().UnixMilli(), Error: e.Error()}, nil
	}
	return PlanFile{Path: path, ModifiedAt: info.ModTime().UnixMilli(), Plan: &p}, nil
}
func Parse(path string, data []byte) (Plan, error) {
	var graph struct {
		Meta  map[string]any  `json:"meta"`
		Nodes json.RawMessage `json:"nodes"`
	}
	if len(data) > MaxPlan || json.Unmarshal(data, &graph) != nil {
		return Plan{}, errors.New("invalid ATP JSON")
	}
	p := Plan{Path: path, Name: strings.TrimSuffix(filepath.Base(path), ".atp.json"), Status: "DRAFT", Nodes: []Node{}}
	if n, ok := graph.Meta["project_name"].(string); ok && n != "" {
		p.Name = n
	}
	if status, ok := graph.Meta["project_status"].(string); ok && contains([]string{"DRAFT", "ACTIVE", "PAUSED", "ARCHIVED"}, status) {
		p.Status = status
	}
	d := json.NewDecoder(bytes.NewReader(graph.Nodes))
	tok, e := d.Token()
	if e != nil || tok != json.Delim('{') {
		return p, errors.New("not an ATP plan: no nodes")
	}
	for d.More() {
		key, e := d.Token()
		if e != nil {
			return p, e
		}
		id, ok := key.(string)
		if !ok {
			return p, errors.New("invalid node identity")
		}
		var raw map[string]any
		if e = d.Decode(&raw); e != nil {
			return p, e
		}
		if raw == nil {
			continue
		}
		if len(p.Nodes) >= MaxNodes {
			return p, errors.New("ATP node budget exceeded")
		}
		get := func(k string) string { v, _ := raw[k].(string); return v }
		arr := func(k string) []string {
			out := []string{}
			v, _ := raw[k].([]any)
			for _, x := range v {
				if s, ok := x.(string); ok {
					out = append(out, s)
				}
			}
			return out
		}
		title := get("title")
		if title == "" {
			title = id
		}
		status := get("status")
		if !contains([]string{"LOCKED", "READY", "CLAIMED", "COMPLETED", "FAILED"}, status) {
			status = "LOCKED"
		}
		closed := get("future_state")
		if closed != "CLOSED" && closed != "SUPERSEDED" {
			closed = ""
		}
		p.Nodes = append(p.Nodes, Node{ID: id, Title: title, Instruction: get("instruction"), Context: get("context"), Dependencies: arr("dependencies"), Status: status, Scope: get("type") == "SCOPE", Children: arr("scope_children"), Closed: closed, Worker: get("worker_id"), StartedAt: get("started_at"), CompletedAt: get("completed_at"), Report: get("report"), Artifacts: arr("artifacts"), Effort: get("reasoning_effort")})
	}
	ids := map[string]bool{}
	for _, n := range p.Nodes {
		ids[n.ID] = true
	}
	for i := range p.Nodes {
		n := &p.Nodes[i]
		deps := []string{}
		for _, id := range n.Dependencies {
			if ids[id] && id != n.ID {
				deps = append(deps, id)
			}
		}
		n.Dependencies = deps
		children := []string{}
		for _, id := range n.Children {
			if ids[id] {
				children = append(children, id)
			}
		}
		n.Children = children
	}
	return p, nil
}
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func Scan(root string) (ProjectPlans, error) {
	w, e := workspace.Open(root)
	if e != nil {
		return ProjectPlans{}, e
	}
	out := ProjectPlans{CWD: root, Plans: []PlanFile{}}
	paths := []string{}
	skip := []string{"node_modules", ".git", "dist", "build", "out", "target", ".venv", "venv", "Library"}
	visited := 0
	e = filepath.WalkDir(w.Root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		visited++
		if visited > 50000 {
			return errors.New("ATP scan entry budget exceeded")
		}
		rel, _ := filepath.Rel(w.Root, path)
		depth := len(strings.Split(filepath.ToSlash(rel), "/"))
		if d.IsDir() {
			if path != w.Root && (contains(skip, d.Name()) || depth >= 5) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".atp.json") {
			paths = append(paths, path)
			if len(paths) > 1024 {
				return errors.New("ATP plan count budget exceeded")
			}
		}
		return nil
	})
	if e != nil {
		return out, e
	}
	sort.Strings(paths)
	for _, path := range paths {
		f, e := Read(root, path)
		if e != nil {
			f = PlanFile{Path: path, Error: e.Error()}
		}
		out.Plans = append(out.Plans, f)
	}
	return out, nil
}

type output struct {
	b        bytes.Buffer
	exceeded bool
}

func (o *output) Write(p []byte) (int, error) {
	n := len(p)
	if o.b.Len()+n > MaxPlan {
		o.exceeded = true
		p = p[:max(0, MaxPlan-o.b.Len())]
	}
	_, _ = o.b.Write(p)
	return n, nil
}

var commands = map[string]bool{"atp-activate-project": true, "atp-claim-task": true, "atp-release-claim": true, "atp-complete-task": true, "atp-decompose-task": true, "atp-apply-future-patch": true, "atp-read-graph": true, "atp-status-summary": true}

func (l Librarian) Call(ctx context.Context, root, plan, command string, args []string) (string, error) {
	plan, e := Resolve(root, plan)
	if e != nil {
		return "", e
	}
	if !commands[command] || len(args) > 256 {
		return "", errors.New("unsupported ATP operation")
	}
	if !filepath.IsAbs(l.Path) {
		return "", errors.New("bundled ATP librarian absolute path not configured")
	}
	if _, e = os.Stat(l.Path); e != nil {
		return "", e
	}
	for i, a := range args {
		if strings.ContainsRune(a, 0) || len(a) > 1<<20 {
			return "", errors.New("invalid ATP argument")
		}
		if a == "--plan-path" {
			return "", errors.New("plan override refused")
		}
		if strings.HasSuffix(a, "-file") && strings.HasPrefix(a, "--") {
			if i+1 >= len(args) {
				return "", errors.New("missing ATP file")
			}
			w, e := workspace.Open(root)
			if e != nil {
				return "", e
			}
			value := args[i+1]
			if filepath.IsAbs(value) {
				value, e = filepath.Rel(root, value)
				if e != nil {
					return "", e
				}
			}
			if _, e = w.Path(filepath.ToSlash(value), false); e != nil {
				return "", e
			}
		}
	}
	python := l.Python
	if python == "" {
		python = "python3"
	}
	argv := append([]string{l.Path, command, "--plan-path", plan}, args...)
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, argv...)
	cmd.Dir = root
	env := []string{}
	for _, x := range processenv.Safe() {
		if !strings.HasPrefix(x, "ATP_") {
			env = append(env, x)
		}
	}
	cmd.Env = env
	proc.Configure(cmd)
	cmd.WaitDelay = 2 * time.Second
	out, errout := &output{}, &output{}
	cmd.Stdout = out
	cmd.Stderr = errout
	e = cmd.Run()
	if e != nil {
		return "", fmt.Errorf("ATP librarian: %w: %s", e, strings.TrimSpace(errout.b.String()))
	}
	if out.exceeded || errout.exceeded {
		return "", errors.New("ATP output budget exceeded")
	}
	return strings.TrimSpace(out.b.String()), nil
}

type Claim struct {
	Kind    string `json:"kind"`
	Node    string `json:"node,omitempty"`
	Title   string `json:"title,omitempty"`
	Packet  string `json:"packet,omitempty"`
	Message string `json:"message,omitempty"`
}

func ParseClaim(raw string) (Claim, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "TASK ASSIGNED: ") {
		first := strings.SplitN(raw, "\n", 2)[0]
		a := strings.SplitN(strings.TrimPrefix(first, "TASK ASSIGNED: "), " - ", 2)
		if len(a) == 2 && a[0] != "" {
			return Claim{Kind: "assigned", Node: a[0], Title: strings.TrimSpace(a[1]), Packet: raw}, nil
		}
	}
	if strings.HasPrefix(raw, "NO_TASKS_AVAILABLE") {
		return Claim{Kind: "none", Message: raw}, nil
	}
	if strings.HasPrefix(raw, "Project is not ACTIVE") {
		return Claim{Kind: "inactive", Message: raw}, nil
	}
	return Claim{}, errors.New("unexpected ATP claim response")
}

var _ io.Writer = (*output)(nil)
