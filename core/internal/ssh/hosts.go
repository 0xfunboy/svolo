// Package ssh delegates SSH security and configuration resolution to system OpenSSH.
// No password/key parsing, automatic host-key acceptance or agent forwarding is added.
package ssh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"svolo.local/core/internal/processenv"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var aliasRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{0,199}$`)

type Host struct {
	ID            string `json:"id"`
	Alias         string `json:"alias"`
	Name          string `json:"name"`
	RemotePort    int    `json:"remotePort"`
	TokenEnv      string `json:"tokenEnv,omitempty"`
	TokenRef      string `json:"tokenRef,omitempty"`
	AutoReconnect bool   `json:"autoReconnect,omitempty"`
}
type Status struct {
	State     string `json:"state"`
	Protocol  int    `json:"protocol,omitempty"`
	Attempts  int    `json:"attempts,omitempty"`
	ID        string `json:"id"`
	Connected bool   `json:"connected"`
	LocalPort int    `json:"localPort,omitempty"`
	Error     string `json:"error,omitempty"`
}
type tunnel struct {
	host     Host
	cmd      *exec.Cmd
	done     chan struct{}
	port     int
	tail     *limited
	ready    bool
	protocol int
}
type reconnect struct {
	host     Host
	attempts int
	next     time.Time
	error    string
}
type attempt struct {
	host   Host
	cancel context.CancelFunc
	done   chan struct{}
	status Status
	err    error
}
type Manager struct {
	ctx           context.Context
	cancel        context.CancelFunc
	desired       map[string]*reconnect
	attempts      map[string]*attempt
	closed        bool
	mu            sync.Mutex
	tunnels       map[string]*tunnel
	Binary        string
	ResolveSecret func(string) (string, error)
}
type limited struct {
	mu sync.Mutex
	b  []byte
}

func (w *limited) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.b = append(w.b, p...)
	if len(w.b) > 4000 {
		w.b = w.b[len(w.b)-4000:]
	}
	return len(p), nil
}
func (w *limited) String() string { w.mu.Lock(); defer w.mu.Unlock(); return string(w.b) }
func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{ctx: ctx, cancel: cancel, tunnels: map[string]*tunnel{}, desired: map[string]*reconnect{}, attempts: map[string]*attempt{}, Binary: "ssh"}
	go m.reconnectLoop()
	return m
}
func Validate(h Host) error {
	if !aliasRE.MatchString(h.ID) || !aliasRE.MatchString(h.Alias) {
		return errors.New("invalid host id or SSH alias; use ~/.ssh/config for complex options")
	}
	if h.RemotePort < 1 || h.RemotePort > 65535 {
		return errors.New("remote port must be 1..65535")
	}
	if (h.TokenRef == "" && !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(h.TokenEnv)) || (h.TokenRef != "" && (!aliasRE.MatchString(h.TokenRef) || h.TokenEnv != "")) {
		return errors.New("remote token environment variable required")
	}
	return nil
}
func TunnelArgs(h Host, local int) []string {
	return []string{"-N", "-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ExitOnForwardFailure=yes", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3", "-o", "ForwardAgent=no", "-L", fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", local, h.RemotePort), "--", h.Alias}
}
func (m *Manager) Resolve(ctx context.Context, alias string) (map[string]string, error) {
	if !aliasRE.MatchString(alias) {
		return nil, errors.New("invalid SSH alias")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.Binary, "-G", "--", alias)
	cmd.Stdin = nil
	cmd.Env = processenv.SSH()
	b, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		i := strings.IndexByte(line, ' ')
		if i > 0 {
			key := line[:i]
			switch key {
			case "hostname", "user", "port", "proxyjump", "identityfile", "identitiesonly", "stricthostkeychecking":
				out[key] = line[i+1:]
			}
		}
	}
	return out, nil
}

// Connect coalesces concurrent callers. Success means an authenticated daemon
// with protocol version 1 answered, not merely that a TCP port became open.
func (m *Manager) Connect(ctx context.Context, h Host) (Status, error) {
	return m.connectRequested(ctx, h, nil)
}

// expected binds retries to the exact still-enabled reconnect generation. A
// queued background attempt cannot undo a user's explicit Disconnect.
func (m *Manager) connectRequested(ctx context.Context, h Host, expected *reconnect) (Status, error) {
	if err := Validate(h); err != nil {
		return Status{}, err
	}
	if _, err := m.token(h); err != nil {
		return Status{}, err
	}
	m.mu.Lock()
	if expected != nil && m.desired[h.ID] != expected {
		m.mu.Unlock()
		return Status{}, context.Canceled
	}
	if m.closed {
		m.mu.Unlock()
		return Status{}, errors.New("SSH manager is closed")
	}
	if a := m.attempts[h.ID]; a != nil {
		if a.host != h {
			m.mu.Unlock()
			return Status{}, errors.New("connection settings changed during pending SSH handshake")
		}
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return Status{}, ctx.Err()
		case <-a.done:
			return a.status, a.err
		}
	}
	if t := m.tunnels[h.ID]; t != nil {
		select {
		case <-t.done:
			delete(m.tunnels, h.ID)
		default:
			if t.host != h {
				m.mu.Unlock()
				return Status{}, errors.New("disconnect host before changing connection settings")
			}
			if t.ready {
				st := Status{ID: h.ID, Connected: true, State: "connected", LocalPort: t.port, Protocol: t.protocol}
				m.mu.Unlock()
				return st, nil
			}
		}
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	a := &attempt{host: h, done: make(chan struct{}), cancel: cancel}
	m.attempts[h.ID] = a
	m.mu.Unlock()
	st, err := m.connect(workCtx, h)
	m.mu.Lock()
	if workCtx.Err() != nil && err == nil {
		st = Status{ID: h.ID, State: "disconnected"}
		err = workCtx.Err()
	}
	a.status, a.err = st, err
	delete(m.attempts, h.ID)
	if err == nil && h.AutoReconnect && !m.closed {
		m.desired[h.ID] = &reconnect{host: h}
	}
	close(a.done)
	m.mu.Unlock()
	return st, err
}
func (m *Manager) connect(ctx context.Context, h Host) (Status, error) {
	m.mu.Lock()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		m.mu.Unlock()
		return Status{}, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	cmd := exec.Command(m.Binary, TunnelArgs(h, port)...)
	cmd.Env = processenv.SSH()
	tail := &limited{}
	cmd.Stdout = io.Discard
	cmd.Stderr = tail
	if err = cmd.Start(); err != nil {
		m.mu.Unlock()
		return Status{}, err
	}
	t := &tunnel{host: h, cmd: cmd, done: make(chan struct{}), port: port, tail: tail}
	m.tunnels[h.ID] = t
	m.mu.Unlock()
	go func() { _ = cmd.Wait(); close(t.done) }()
	success := false
	defer func() {
		if !success {
			m.stopTunnel(h.ID, t)
		}
	}()
	wait, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return Status{}, errors.New("SSH manager is closed")
		case <-t.done:
			return Status{ID: h.ID, State: "error", Error: tail.String()}, fmt.Errorf("SSH failed: %s", tail.String())
		case <-wait.Done():
			return Status{}, fmt.Errorf("SSH connection timed out: %w", wait.Err())
		case <-tick.C:
			check, done := context.WithTimeout(wait, 700*time.Millisecond)
			response, e := m.Do(check, h.ID, "GET", "/v1/health", nil)
			var health struct {
				OK       bool `json:"ok"`
				Protocol int  `json:"protocol"`
			}
			if e == nil {
				if response.StatusCode == http.StatusUnauthorized {
					response.Body.Close()
					done()
					return Status{}, errors.New("remote daemon rejected its credential")
				}
				if response.StatusCode == 200 {
					e = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&health)
				}
				response.Body.Close()
			}
			done()
			if e == nil && health.OK {
				if health.Protocol != 1 {
					return Status{}, fmt.Errorf("incompatible remote protocol %d; expected 1; no operation was sent", health.Protocol)
				}
				m.mu.Lock()
				t.ready = true
				t.protocol = health.Protocol
				m.mu.Unlock()
				success = true
				return Status{ID: h.ID, State: "connected", Connected: true, LocalPort: port, Protocol: health.Protocol}, nil
			}
		}
	}
}
func (m *Manager) stopTunnel(id string, t *tunnel) {
	m.mu.Lock()
	if m.tunnels[id] == t {
		delete(m.tunnels, id)
	}
	m.mu.Unlock()
	_ = t.cmd.Process.Kill()
	<-t.done
}
func (m *Manager) reconnectLoop() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case now := <-tick.C:
			m.mu.Lock()
			todo := []*reconnect{}
			for id, w := range m.desired {
				if m.attempts[id] != nil || now.Before(w.next) {
					continue
				}
				t := m.tunnels[id]
				up := false
				if t != nil {
					select {
					case <-t.done:
					default:
						up = t.ready
					}
				}
				if up {
					continue
				}
				w.attempts++
				delay := time.Second * time.Duration(1<<min(w.attempts, 6))
				w.next = now.Add(delay)
				todo = append(todo, w)
			}
			m.mu.Unlock()
			for _, wanted := range todo {
				go func(wanted *reconnect) {
					h := wanted.host
					_, err := m.connectRequested(m.ctx, h, wanted)
					if err != nil {
						m.mu.Lock()
						if w := m.desired[h.ID]; w == wanted {
							w.error = err.Error()
						}
						m.mu.Unlock()
					}
				}(wanted)
			}
		}
	}
}
func (m *Manager) Disconnect(id string) error {
	m.mu.Lock()
	delete(m.desired, id)
	if a := m.attempts[id]; a != nil {
		a.cancel()
	}
	t := m.tunnels[id]
	delete(m.tunnels, id)
	m.mu.Unlock()
	if t != nil {
		_ = t.cmd.Process.Kill()
		<-t.done
	}
	return nil
}
func (m *Manager) Status() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Status{}
	seen := map[string]bool{}
	for id, t := range m.tunnels {
		st := Status{ID: id, State: "connecting", LocalPort: t.port, Protocol: t.protocol}
		select {
		case <-t.done:
			st.State = "disconnected"
			st.Error = t.tail.String()
		default:
			st.Connected = t.ready
			if t.ready {
				st.State = "connected"
			}
		}
		if w := m.desired[id]; w != nil && !st.Connected {
			st.State = "reconnecting"
			st.Attempts = w.attempts
		}
		out = append(out, st)
		seen[id] = true
	}
	for id, w := range m.desired {
		if !seen[id] {
			out = append(out, Status{ID: id, State: "reconnecting", Attempts: w.attempts, Error: w.error})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (m *Manager) Do(ctx context.Context, id, method, path string, body io.Reader) (*http.Response, error) {
	if !strings.HasPrefix(path, "/v1/") || strings.Contains(path, "..") || strings.ContainsAny(path, "\r\n#") {
		return nil, errors.New("only explicit /v1/ paths can be forwarded")
	}
	m.mu.Lock()
	t := m.tunnels[id]
	m.mu.Unlock()
	if t == nil {
		return nil, errors.New("host is disconnected")
	}
	select {
	case <-t.done:
		return nil, errors.New("SSH tunnel exited")
	default:
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://127.0.0.1:"+strconv.Itoa(t.port)+path, body)
	if err != nil {
		return nil, err
	}
	token, err := m.token(t.host)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Svolo-Tunnel", "1")
	client := &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("remote daemon redirect refused") }}
	return client.Do(req)
}
func (m *Manager) Close() {
	m.cancel()
	m.mu.Lock()
	m.closed = true
	m.desired = map[string]*reconnect{}
	ids := []string{}
	for id := range m.tunnels {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Disconnect(id)
	}
}

func (m *Manager) token(h Host) (string, error) {
	if h.TokenRef != "" {
		if m.ResolveSecret == nil {
			return "", errors.New("SSH credential vault resolver is unavailable")
		}
		return m.ResolveSecret(h.TokenRef)
	}
	token := os.Getenv(h.TokenEnv)
	if token == "" {
		return "", fmt.Errorf("set %s on the desktop host to the remote daemon token", h.TokenEnv)
	}
	return token, nil
}
