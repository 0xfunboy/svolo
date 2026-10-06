package atp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"svolo.local/core/internal/gitops"
	"svolo.local/core/internal/piruntime"
	"svolo.local/core/internal/store"
	"sync"
	"time"
)

type RunOptions struct {
	Session       string            `json:"session"`
	Workspace     string            `json:"workspace"`
	Plan          string            `json:"plan"`
	Runtime       piruntime.Options `json:"runtime"`
	CommitPerNode bool              `json:"commitPerNode"`
	AgentID       string            `json:"agentId,omitempty"`
	Provider      string            `json:"provider,omitempty"`
	Model         string            `json:"model,omitempty"`
	Thinking      string            `json:"thinking,omitempty"`
}
type Run struct {
	ID          string `json:"id"`
	Session     string `json:"session"`
	CWD         string `json:"cwd"`
	Plan        string `json:"plan"`
	Phase       string `json:"phase"`
	Node        string `json:"node,omitempty"`
	Title       string `json:"title,omitempty"`
	Runtime     string `json:"runtime,omitempty"`
	SessionPath string `json:"sessionPath,omitempty"`
	Since       int64  `json:"since"`
	Updated     int64  `json:"updated"`
	Error       string `json:"error,omitempty"`
	Message     string `json:"message,omitempty"`
	Completed   int    `json:"completed"`
}
type live struct {
	state  Run
	cancel context.CancelFunc
	done   chan struct{}
}
type Scheduler struct {
	mu            sync.Mutex
	st            *store.Store
	Runtime       *piruntime.Manager
	Librarian     Librarian
	runs          map[string]*live
	held          map[string]bool
	closed        bool
	WorkerTimeout time.Duration
}

func New(st *store.Store, runtime *piruntime.Manager, librarian Librarian) *Scheduler {
	m := &Scheduler{st: st, Runtime: runtime, Librarian: librarian, runs: map[string]*live{}, held: map[string]bool{}, WorkerTimeout: time.Hour}
	_ = st.Read("atp-held", &m.held)
	if m.held == nil {
		m.held = map[string]bool{}
	}
	var states []Run
	_ = st.Read("atp-runs", &states)
	for _, s := range states {
		if active(s.Phase) {
			s.Phase = "interrupted"
			s.Error = "Daemon restarted. Inspect the node and working tree; resume explicitly. No command was replayed."
		}
		done := make(chan struct{})
		close(done)
		m.runs[s.ID] = &live{state: s, done: done}
	}
	return m
}
func active(p string) bool {
	return p != "finished" && p != "failed" && p != "stopped" && p != "interrupted"
}
func (m *Scheduler) saveLocked() error {
	states := []Run{}
	for _, l := range m.runs {
		states = append(states, l.state)
	}
	return m.st.Write("atp-runs", states)
}
func (m *Scheduler) List() []Run {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Run{}
	for _, l := range m.runs {
		out = append(out, l.state)
	}
	return out
}
func (m *Scheduler) Held() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []string{}
	for p, h := range m.held {
		if h {
			out = append(out, p)
		}
	}
	return out
}
func (m *Scheduler) SetHeld(path string, value bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if value {
		m.held[path] = true
	} else {
		delete(m.held, path)
	}
	return m.st.Write("atp-held", m.held)
}
func (m *Scheduler) heldPlan(path string) bool { m.mu.Lock(); defer m.mu.Unlock(); return m.held[path] }
func (m *Scheduler) patch(id string, f func(*Run)) {
	m.mu.Lock()
	l := m.runs[id]
	if l != nil {
		f(&l.state)
		l.state.Updated = max(time.Now().UnixMilli(), l.state.Updated+1)
		if e := m.saveLocked(); e != nil {
			l.state.Error = "Cannot persist ATP state: " + e.Error()
			if l.cancel != nil {
				l.cancel()
			}
		}
		_, _ = m.st.Append(l.state.Session, "atp.state", l.state)
	}
	m.mu.Unlock()
}
func (m *Scheduler) Start(o RunOptions) (Run, error) {
	path, e := Resolve(o.Workspace, o.Plan)
	if e != nil {
		return Run{}, e
	}
	if !store.ValidID(o.Session) {
		return Run{}, errors.New("ATP session required")
	}
	if o.Runtime.CWD != "" && filepath.Clean(o.Runtime.CWD) != filepath.Clean(o.Workspace) {
		return Run{}, errors.New("ATP worker workspace mismatch")
	}
	if o.AgentID == "" {
		o.AgentID = "svolo-w1"
	}
	if !store.ValidID(o.AgentID) {
		return Run{}, errors.New("invalid ATP agent identity")
	}
	o.Plan = path
	o.Runtime.Session = o.Session
	o.Runtime.CWD = o.Workspace
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Run{}, errors.New("ATP scheduler is shutting down")
	}
	count := 0
	for _, l := range m.runs {
		if active(l.state.Phase) {
			count++
			if l.state.Plan == path {
				return Run{}, errors.New("plan already running")
			}
		}
	}
	if count >= 16 || len(m.runs) >= 1000 {
		return Run{}, errors.New("ATP run budget exceeded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	state := Run{ID: store.ID(), Session: o.Session, CWD: o.Workspace, Plan: path, Phase: "starting", Since: time.Now().UnixMilli(), Updated: time.Now().UnixMilli()}
	l := &live{state: state, cancel: cancel, done: make(chan struct{})}
	m.runs[state.ID] = l
	if e = m.saveLocked(); e != nil {
		delete(m.runs, state.ID)
		cancel()
		return Run{}, e
	}
	go func() {
		defer close(l.done)
		defer cancel()
		e := m.work(ctx, state.ID, o)
		m.patch(state.ID, func(s *Run) {
			if e != nil {
				s.Error = e.Error()
				if ctx.Err() != nil {
					s.Phase = "stopped"
				} else {
					s.Phase = "failed"
				}
			} else {
				s.Phase = "finished"
			}
		})
	}()
	return state, nil
}
func (m *Scheduler) Stop(ctx context.Context, id string) error {
	m.mu.Lock()
	l := m.runs[id]
	if l != nil && l.cancel != nil {
		l.cancel()
	}
	m.mu.Unlock()
	if l == nil {
		return errors.New("unknown ATP run")
	}
	select {
	case <-l.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (m *Scheduler) Close() {
	m.mu.Lock()
	m.closed = true
	ls := []*live{}
	for _, l := range m.runs {
		ls = append(ls, l)
		if l.cancel != nil {
			l.cancel()
		}
	}
	m.mu.Unlock()
	for _, l := range ls {
		<-l.done
	}
}
func (m *Scheduler) work(ctx context.Context, id string, o RunOptions) error {
	if _, e := m.Librarian.Call(ctx, o.Workspace, o.Plan, "atp-activate-project", []string{"--actor-id", "svolo", "--reason", "The user started this plan in Svolo."}); e != nil {
		return e
	}
	for steps := 0; steps < MaxNodes; steps++ {
		if e := ctx.Err(); e != nil {
			return e
		}
		for m.heldPlan(o.Plan) {
			m.patch(id, func(s *Run) { s.Phase = "held" })
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(300 * time.Millisecond):
			}
		}
		m.patch(id, func(s *Run) { s.Phase = "claiming"; s.Node = ""; s.Title = ""; s.Runtime = "" })
		before, e := Read(o.Workspace, o.Plan)
		if e != nil || before.Plan == nil {
			return errors.New("cannot read ATP plan before claim")
		}
		text, e := m.Librarian.Call(ctx, o.Workspace, o.Plan, "atp-claim-task", []string{"--agent-id", o.AgentID})
		if e != nil {
			return e
		}
		claim, e := ParseClaim(text)
		if e != nil {
			return e
		}
		if claim.Kind == "inactive" {
			return errors.New(claim.Message)
		}
		if claim.Kind == "none" {
			after, e := Read(o.Workspace, o.Plan)
			if e != nil || after.Plan == nil {
				return errors.New("cannot verify final plan")
			}
			left := 0
			for _, n := range after.Plan.Nodes {
				if !n.Scope && n.Closed == "" && n.Status != "COMPLETED" {
					left++
				}
			}
			if left > 0 {
				return fmt.Errorf("no ready task; %d nodes remain incomplete or held elsewhere", left)
			}
			m.patch(id, func(s *Run) { s.Message = "All runnable nodes completed; verified in the plan." })
			return nil
		}
		resumed := false
		for _, n := range before.Plan.Nodes {
			if n.ID == claim.Node && n.Status == "CLAIMED" && n.Worker == o.AgentID {
				resumed = true
			}
		}
		if e = m.runNode(ctx, id, o, claim, resumed); e != nil {
			return e
		}
	}
	return errors.New("ATP scheduler step budget exceeded")
}
func (m *Scheduler) runNode(ctx context.Context, id string, o RunOptions, claim Claim, resumed bool) (result error) {
	head, e := gitops.ReadHead(ctx, o.Workspace)
	if e != nil {
		return e
	}
	hash := sha256.Sum256([]byte(claim.Node))
	dir := filepath.Join(m.st.Root, "atp-sessions", id)
	if e = os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	options := o.Runtime
	options.SessionPath = filepath.Join(dir, hex.EncodeToString(hash[:12])+".jsonl")
	m.patch(id, func(s *Run) {
		s.Node = claim.Node
		s.Title = claim.Title
		s.Phase = "working"
		s.SessionPath = options.SessionPath
	})
	state, e := m.Runtime.Start(options)
	if e != nil {
		return e
	}
	m.patch(id, func(s *Run) { s.Runtime = state.ID })
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		_ = m.Runtime.Stop(stop, state.ID)
		if ctx.Err() != nil {
			release, c := context.WithTimeout(context.Background(), 5*time.Second)
			defer c()
			_, _ = m.Librarian.Call(release, o.Workspace, o.Plan, "atp-release-claim", []string{"--node-id", claim.Node, "--agent-id", o.AgentID, "--reason", "Svolo run stopped; partial changes preserved."})
		}
	}()
	send := func(command map[string]any) error {
		c, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		raw, e := m.Runtime.Send(c, state.ID, command)
		if e != nil {
			return e
		}
		var reply struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		if e = json.Unmarshal(raw, &reply); e != nil {
			return e
		}
		if !reply.Success {
			return errors.New("pi RPC refused: " + reply.Error)
		}
		return nil
	}
	if o.Model != "" {
		if e = send(map[string]any{"type": "set_model", "provider": o.Provider, "modelId": o.Model}); e != nil {
			return e
		}
	}
	if o.Thinking != "" {
		if e = send(map[string]any{"type": "set_thinking_level", "level": o.Thinking}); e != nil {
			return e
		}
	}
	name := "ATP " + claim.Node + ": " + claim.Title
	_ = send(map[string]any{"type": "set_session_name", "name": name})
	argv, _ := json.Marshal([]string{m.Librarian.Python, m.Librarian.Path, "<command>", "--plan-path", o.Plan})
	message := fmt.Sprintf("### Svolo host-owned ATP worker\nworkspace: %s\nplan: %s\nagent_id: %s\nlibrarian argv (replace <command>): %s\nResume of an earlier claim: %t\nOnly execute the assigned node. Do not claim another task. Preserve unrelated changes. Complete, fail, or decompose this node through the librarian, never by directly writing the plan. After completing or decomposing, stop. Run relevant tests and include their actual results in the report.\n\n%s", o.Workspace, o.Plan, o.AgentID, argv, resumed, claim.Packet)
	cursor := uint64(0)
	for turn := 0; turn < 2; turn++ {
		events, e := m.Runtime.Events(state.ID, cursor)
		if e != nil {
			return e
		}
		cursor = events.Cursor
		if turn > 0 {
			message = "The claimed node is still CLAIMED. Finish it through the librarian (DONE, FAILED, or decomposition), with an honest report, then stop. Do not claim another node."
			m.patch(id, func(s *Run) { s.Phase = "nudging" })
		}
		if e = send(map[string]any{"type": "prompt", "message": message}); e != nil {
			return e
		}
		if cursor, e = m.waitTurn(ctx, state.ID, cursor); e != nil {
			return e
		}
		file, e := Read(o.Workspace, o.Plan)
		if e != nil || file.Plan == nil {
			return errors.New("cannot verify worker result")
		}
		found := false
		var node Node
		for _, n := range file.Plan.Nodes {
			if n.ID == claim.Node {
				found = true
				node = n
				break
			}
		}
		if !found {
			return errors.New("worker removed the assigned node; result not accepted")
		}
		if node.Status == "FAILED" {
			return fmt.Errorf("node %s failed: %s", node.ID, node.Report)
		}
		if node.Status == "COMPLETED" || node.Scope || node.Closed != "" {
			if o.CommitPerNode {
				m.patch(id, func(s *Run) { s.Phase = "committing" })
				if _, e = gitops.CommitNode(ctx, o.Workspace, claim.Node, claim.Title, head); e != nil {
					return e
				}
			}
			m.patch(id, func(s *Run) { s.Completed++ })
			return nil
		}
		if node.Status != "CLAIMED" || node.Worker != o.AgentID {
			return errors.New("assigned node changed ownership; manual review required")
		}
	}
	return errors.New("worker left node claimed after a reminder; no completion assumed")
}
func (m *Scheduler) waitTurn(ctx context.Context, id string, cursor uint64) (uint64, error) {
	timeout := m.WorkerTimeout
	if timeout <= 0 {
		timeout = time.Hour
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		events, e := m.Runtime.Events(id, cursor)
		if e != nil {
			return cursor, e
		}
		if events.Gap {
			return cursor, errors.New("worker event gap; state needs manual review")
		}
		for _, ev := range events.Events {
			if ev.Kind != "record" {
				continue
			}
			var rec struct {
				Type      string `json:"type"`
				WillRetry bool   `json:"willRetry"`
			}
			_ = json.Unmarshal(ev.Record, &rec)
			if rec.Type == "agent_end" && !rec.WillRetry {
				return events.Cursor, nil
			}
		}
		cursor = events.Cursor
		if events.State.Status != "running" {
			return cursor, errors.New("worker exited before finishing its turn")
		}
		select {
		case <-ctx.Done():
			return cursor, ctx.Err()
		case <-ticker.C:
		}
	}
}
func (m *Scheduler) Claim(ctx context.Context, root, path, agent string) (Claim, error) {
	resolved, e := Resolve(root, path)
	if e != nil {
		return Claim{}, e
	}
	if m.heldPlan(resolved) {
		return Claim{Kind: "held", Message: "Plan held by its orchestrator."}, nil
	}
	text, e := m.Librarian.Call(ctx, root, resolved, "atp-claim-task", []string{"--agent-id", agent})
	if e != nil {
		return Claim{}, e
	}
	return ParseClaim(text)
}

var _ = strings.TrimSpace
