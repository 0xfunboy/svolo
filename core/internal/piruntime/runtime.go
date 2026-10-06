// Package piruntime supervises pi RPC in the host daemon, not in a window.
// Compatibility extensions still execute inside pi. No command is replayed after
// transport loss, process exit, daemon restart, or an ambiguous timeout.
package piruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"svolo.local/core/internal/proc"
	"svolo.local/core/internal/processenv"
	"svolo.local/core/internal/store"
	"sync"
	"time"
)

const MaxLine = 8 << 20
const MaxProcesses = 32

type Options struct {
	Session     string            `json:"session"`
	CWD         string            `json:"cwd"`
	SessionPath string            `json:"sessionPath,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
}
type State struct {
	ID       string    `json:"id"`
	Session  string    `json:"session"`
	CWD      string    `json:"cwd"`
	PID      int       `json:"pid"`
	Status   string    `json:"status"`
	Started  time.Time `json:"started"`
	ExitCode *int      `json:"exitCode,omitempty"`
	Error    string    `json:"error,omitempty"`
}
type Event struct {
	Sequence uint64          `json:"sequence"`
	Kind     string          `json:"kind"`
	Record   json.RawMessage `json:"record"`
}
type Events struct {
	Events []Event `json:"events"`
	Cursor uint64  `json:"cursor"`
	Gap    bool    `json:"gap"`
	State  State   `json:"state"`
}
type process struct {
	mu         sync.Mutex
	write      sync.Mutex
	state      State
	cmd        *exec.Cmd
	input      io.WriteCloser
	cancel     context.CancelFunc
	done       chan struct{}
	seq        uint64
	pending    map[string]chan json.RawMessage
	events     []Event
	eventSeq   uint64
	eventBytes int
	stderr     string
	secrets    []string
}
type Manager struct {
	mu         sync.Mutex
	processes  map[string]*process
	st         *store.Store
	Executable string
	Prefix     []string
	closed     bool
}

func New(st *store.Store, executable string) *Manager {
	if executable == "" {
		executable = "pi"
	}
	return &Manager{processes: map[string]*process{}, st: st, Executable: executable}
}
func (m *Manager) Start(o Options) (State, error) {
	if !store.ValidID(o.Session) || !filepath.IsAbs(o.CWD) {
		return State{}, errors.New("registered session and absolute working directory required")
	}
	cwd, err := filepath.EvalSymlinks(o.CWD)
	if err != nil {
		return State{}, err
	}
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return State{}, errors.New("working directory is not a directory")
	}
	if len(o.Args) > 256 || len(o.Env) > 64 {
		return State{}, errors.New("runtime argument budget exceeded")
	}
	for _, a := range o.Args {
		if strings.ContainsRune(a, 0) || len(a) > 65536 {
			return State{}, errors.New("invalid runtime argument")
		}
	}
	if o.SessionPath != "" && (!filepath.IsAbs(o.SessionPath) || !strings.HasSuffix(o.SessionPath, ".jsonl")) {
		return State{}, errors.New("invalid pi session path")
	}
	env := processenv.Safe()
	secrets := []string{}
	for k, v := range o.Env {
		if strings.ContainsAny(k, "=\x00\r\n") || strings.ContainsRune(v, 0) || len(k) > 128 || len(v) > 32768 {
			return State{}, errors.New("invalid runtime environment")
		}
		if strings.Contains(strings.ToUpper(k), "VAULT_KEY") || strings.EqualFold(k, "SVOLO_CORE_TOKEN") {
			return State{}, errors.New("host credentials cannot enter the pi runtime")
		}
		env = append(env, k+"="+v)
		if len(v) > 8 {
			secrets = append(secrets, v)
		}
	}
	args := append(append([]string{}, m.Prefix...), "--mode", "rpc")
	args = append(args, o.Args...)
	if o.SessionPath != "" {
		args = append(args, "--session", o.SessionPath)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, m.Executable, args...)
	cmd.Dir = cwd
	cmd.Env = env
	proc.Configure(cmd)
	cmd.WaitDelay = 2 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return State{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return State{}, err
	}
	input, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return State{}, err
	}
	p := &process{state: State{ID: store.ID(), Session: o.Session, CWD: cwd, Status: "running", Started: time.Now().UTC()}, cmd: cmd, input: input, cancel: cancel, done: make(chan struct{}), pending: map[string]chan json.RawMessage{}, events: []Event{}, secrets: secrets}
	m.mu.Lock()
	active := 0
	for _, x := range m.processes {
		x.mu.Lock()
		if x.state.Status == "running" {
			active++
		}
		x.mu.Unlock()
	}
	if m.closed || active >= MaxProcesses {
		m.mu.Unlock()
		cancel()
		return State{}, errors.New("runtime closed or process budget exceeded")
	}
	if err = cmd.Start(); err != nil {
		m.mu.Unlock()
		cancel()
		return State{}, fmt.Errorf("cannot start pi runtime: %w", err)
	}
	p.state.PID = cmd.Process.Pid
	m.processes[p.state.ID] = p
	m.mu.Unlock()
	initial := p.snapshot()
	m.persist(p)
	var readers sync.WaitGroup
	readers.Add(2)
	go func() { defer readers.Done(); p.read(stdout, false) }()
	go func() { defer readers.Done(); p.read(stderr, true) }()
	go func() {
		readers.Wait()
		err := cmd.Wait()
		p.mu.Lock()
		code := cmd.ProcessState.ExitCode()
		p.state.ExitCode = &code
		p.state.Status = "exited"
		if err != nil {
			p.state.Error = p.scrub(err.Error())
		}
		for id, ch := range p.pending {
			delete(p.pending, id)
			close(ch)
		}
		tail := p.stderr
		p.mu.Unlock()
		p.emit("exit", map[string]any{"code": code, "signal": nil, "stderrTail": tail})
		m.persist(p)
		cancel()
		close(p.done)
	}()
	return initial, nil
}
func (p *process) scrub(s string) string {
	for _, v := range p.secrets {
		s = strings.ReplaceAll(s, v, "[redacted]")
	}
	return s
}
func (p *process) snapshot() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.state
	if s.ExitCode != nil {
		v := *s.ExitCode
		s.ExitCode = &v
	}
	return s
}
func (m *Manager) persist(p *process) {
	if m.st != nil {
		_ = m.st.Write("runtime-"+p.state.ID, p.snapshot())
	}
}
func (p *process) emit(kind string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.eventSeq++
	p.events = append(p.events, Event{p.eventSeq, kind, b})
	p.eventBytes += len(b)
	for len(p.events) > 1024 || p.eventBytes > 16<<20 {
		p.eventBytes -= len(p.events[0].Record)
		p.events = p.events[1:]
	}
}
func (p *process) read(r io.Reader, stderr bool) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 65536), MaxLine)
	for s.Scan() {
		line := append([]byte(nil), s.Bytes()...)
		if stderr {
			p.mu.Lock()
			p.stderr = p.scrub(p.stderr + string(line) + "\n")
			if len(p.stderr) > 4000 {
				p.stderr = p.stderr[len(p.stderr)-4000:]
			}
			p.mu.Unlock()
			continue
		}
		var record map[string]json.RawMessage
		if json.Unmarshal(line, &record) != nil {
			p.emit("protocol-warning", map[string]string{"error": "runtime emitted non-JSON output (content withheld)"})
			continue
		}
		var typ, id string
		_ = json.Unmarshal(record["type"], &typ)
		_ = json.Unmarshal(record["id"], &id)
		if typ == "response" && id != "" {
			p.mu.Lock()
			ch := p.pending[id]
			if ch != nil {
				delete(p.pending, id)
				ch <- line
			}
			p.mu.Unlock()
			continue
		}
		p.emit("record", json.RawMessage(line))
	}
	if err := s.Err(); err != nil {
		p.emit("protocol-error", map[string]string{"error": "runtime stream exceeded limit or failed"})
		p.cancel()
	}
}
func (m *Manager) get(id string) (*process, error) {
	m.mu.Lock()
	p := m.processes[id]
	m.mu.Unlock()
	if p == nil {
		return nil, errors.New("unknown runtime; it may have ended before daemon restart")
	}
	return p, nil
}
func (m *Manager) Send(ctx context.Context, id string, command map[string]any) (json.RawMessage, error) {
	p, err := m.get(id)
	if err != nil {
		return nil, err
	}
	typ, _ := command["type"].(string)
	if typ == "" || typ == "extension_ui_response" {
		return nil, errors.New("RPC command type required; UI replies use separate route")
	}
	p.mu.Lock()
	if p.state.Status != "running" {
		p.mu.Unlock()
		return nil, errors.New("pi is not running")
	}
	if len(p.pending) >= 128 {
		p.mu.Unlock()
		return nil, errors.New("pending runtime command budget exceeded")
	}
	p.seq++
	rid := fmt.Sprintf("g%d", p.seq)
	ch := make(chan json.RawMessage, 1)
	p.pending[rid] = ch
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.pending, rid); p.mu.Unlock() }()
	copy := map[string]any{}
	for k, v := range command {
		copy[k] = v
	}
	copy["id"] = rid
	if err = p.writeRecord(copy); err != nil {
		return nil, err
	}
	select {
	case out, ok := <-ch:
		if !ok {
			return nil, errors.New("runtime exited; result unknown, command not replayed")
		}
		return out, nil
	case <-ctx.Done():
		return nil, errors.New("runtime reply timed out; action may have executed, no automatic replay")
	}
}
func (p *process) writeRecord(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > MaxLine-1 {
		return errors.New("RPC record exceeds limit")
	}
	p.write.Lock()
	defer p.write.Unlock()
	_, err = p.input.Write(append(b, '\n'))
	return err
}
func (m *Manager) ReplyUI(id string, reply map[string]any) error {
	p, err := m.get(id)
	if err != nil {
		return err
	}
	if reply["type"] != "extension_ui_response" {
		return errors.New("extension_ui_response required")
	}
	return p.writeRecord(reply)
}
func (m *Manager) Events(id string, after uint64) (Events, error) {
	p, err := m.get(id)
	if err != nil {
		return Events{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := Events{Events: []Event{}, Cursor: p.eventSeq, State: p.state}
	if len(p.events) > 0 && after+1 < p.events[0].Sequence {
		out.Gap = true
	}
	for _, e := range p.events {
		if e.Sequence > after {
			copy := e
			copy.Record = append(json.RawMessage(nil), e.Record...)
			out.Events = append(out.Events, copy)
		}
	}
	return out, nil
}
func (m *Manager) List() []State {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []State{}
	for _, p := range m.processes {
		out = append(out, p.snapshot())
	}
	return out
}
func (m *Manager) Stop(ctx context.Context, id string) error {
	p, err := m.get(id)
	if err != nil {
		return err
	}
	p.write.Lock()
	_ = p.input.Close()
	p.write.Unlock()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		p.cancel()
		return ctx.Err()
	case <-timer.C:
	}
	_ = proc.Terminate(p.cmd)
	timer.Reset(2 * time.Second)
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		p.cancel()
		return ctx.Err()
	case <-timer.C:
		p.cancel()
	}
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	ps := []*process{}
	for _, p := range m.processes {
		ps = append(ps, p)
	}
	m.mu.Unlock()
	for _, p := range ps {
		p.cancel()
	}
	for _, p := range ps {
		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
		}
	}
}
