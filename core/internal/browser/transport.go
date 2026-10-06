package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"svolo.local/core/internal/cdp"
	"svolo.local/core/internal/processenv"
	"svolo.local/core/internal/store"
	"sync"
	"time"
)

type Target struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Type   string `json:"type"`
	Active bool   `json:"active"`
}
type Transport interface {
	List(context.Context, string) ([]Target, error)
	Create(context.Context, string, string) (Target, error)
	Call(context.Context, string, string, string, map[string]any, any) error
	Activate(context.Context, string, string) error
	CloseTab(context.Context, string, string) error
	CloseSession(string) error
	Close() error
}
type ManagedOptions struct {
	Executable, Root string
	Proxy            string
	Headless         bool
	NoSandboxForTest bool
}
type process struct {
	cmd       *exec.Cmd
	done      chan struct{}
	client    *cdp.Client
	mu        sync.Mutex
	attachMu  sync.Mutex
	attached  map[string]string
	owned     map[string]bool
	bootstrap string
	stderr    *tailWriter
}
type Managed struct {
	opts      ManagedOptions
	mu        sync.Mutex
	processes map[string]*process
	closed    bool
}
type tailWriter struct {
	mu sync.Mutex
	b  []byte
}

func (t *tailWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > 6000 {
		t.b = t.b[len(t.b)-6000:]
	}
	return len(p), nil
}
func (t *tailWriter) String() string { t.mu.Lock(); defer t.mu.Unlock(); return string(t.b) }
func NewManaged(opts ManagedOptions) *Managed {
	return &Managed{opts: opts, processes: map[string]*process{}}
}
func Executable() string {
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "chrome", "msedge"} {
		if p, e := exec.LookPath(name); e == nil {
			return p
		}
	}
	var candidates []string
	if runtime.GOOS == "windows" {
		for _, base := range []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)"), os.Getenv("LOCALAPPDATA")} {
			candidates = append(candidates, filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"), filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"))
		}
	}
	if runtime.GOOS == "darwin" {
		candidates = []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium"}
	}
	for _, p := range candidates {
		if st, e := os.Stat(p); e == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}
func (m *Managed) get(ctx context.Context, sid string) (*process, error) {
	if !store.ValidID(sid) {
		return nil, errors.New("invalid browser session")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("browser manager closed")
	}
	if p := m.processes[sid]; p != nil {
		select {
		case <-p.done:
			return nil, errors.New("browser exited; explicit close/reopen required")
		default:
			return p, nil
		}
	}
	exe := m.opts.Executable
	if exe == "" {
		exe = Executable()
	}
	if exe == "" {
		return nil, errors.New("Chromium/Chrome not found; configure browser.executable")
	}
	profile := filepath.Join(m.opts.Root, "profiles", sid)
	if e := os.MkdirAll(profile, 0700); e != nil {
		return nil, e
	}
	// A stale DevToolsActivePort must not attach us to an unrelated browser. Never delete Chromium's singleton lock.
	for _, lock := range []string{"SingletonLock", "SingletonSocket"} {
		if _, e := os.Lstat(filepath.Join(profile, lock)); e == nil {
			return nil, fmt.Errorf("profile in use or stale lock: %s; inspect it before restarting", lock)
		}
	}
	_ = os.Remove(filepath.Join(profile, "DevToolsActivePort"))
	// Start only the managed CDP process. Page creation is explicit, so Chromium
	// cannot place a second, unrequested startup tab in front of the agent page.
	// The legacy screenshot path waits for ForceRedraw, which can stall on a
	// static background page. Chromium's new-surface path requests a fresh
	// compositor surface while keeping the captured page hidden and unfocused.
	args := []string{"--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0", "--user-data-dir=" + profile, "--no-first-run", "--no-default-browser-check", "--disable-background-networking", "--disable-sync", "--disable-component-update", "--disable-dev-shm-usage", "--no-startup-window", "--enable-features=CDPScreenshotNewSurface"}
	if m.opts.Proxy != "" {
		args = append(args, "--proxy-server="+m.opts.Proxy, "--proxy-bypass-list=<-loopback>", "--force-webrtc-ip-handling-policy=disable_non_proxied_udp", "--disable-quic")
	}
	if m.opts.Headless {
		args = append(args, "--headless=new")
	}
	if m.opts.NoSandboxForTest {
		if os.Getenv("SVOLO_TEST_NO_SANDBOX") != "1" {
			return nil, errors.New("sandbox bypass requires explicit SVOLO_TEST_NO_SANDBOX=1; tests only")
		}
		args = append(args, "--no-sandbox")
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = processenv.Safe()
	tail := &tailWriter{}
	cmd.Stderr = tail
	cmd.Stdout = tail
	if e := cmd.Start(); e != nil {
		return nil, e
	}
	p := &process{cmd: cmd, done: make(chan struct{}), attached: map[string]string{}, owned: map[string]bool{}, stderr: tail}
	go func() { _ = cmd.Wait(); close(p.done) }()
	success := false
	defer func() {
		if !success {
			_ = cmd.Process.Kill()
			<-p.done
		}
	}()
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		b, e := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
		if e == nil {
			lines := strings.Split(strings.TrimSpace(string(b)), "\n")
			if len(lines) >= 2 {
				port, e := strconv.Atoi(lines[0])
				if e == nil && port > 0 && port <= 65535 && strings.HasPrefix(lines[1], "/devtools/browser/") {
					p.client, e = cdp.Connect(waitCtx, fmt.Sprintf("ws://127.0.0.1:%d%s", port, lines[1]))
					if e == nil {
						break
					}
				}
			}
		}
		select {
		case <-waitCtx.Done():
			return nil, fmt.Errorf("browser startup: %w; %s", waitCtx.Err(), tail.String())
		case <-p.done:
			return nil, fmt.Errorf("browser exited at startup: %s", tail.String())
		case <-ticker.C:
		}
	}
	var list struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
		} `json:"targetInfos"`
	}
	if e := p.client.Call(ctx, "", "Target.getTargets", map[string]any{}, &list); e != nil {
		return nil, e
	}
	for _, t := range list.TargetInfos {
		if t.Type == "page" {
			p.owned[t.TargetID] = true
		}
	}
	if len(p.owned) == 0 {
		// Exactly one manager-created bootstrap page keeps initial observations
		// usable. Its known ID may be reused for the first explicit page creation.
		var bootstrap struct {
			TargetID string `json:"targetId"`
		}
		if err := p.client.Call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"}, &bootstrap); err != nil {
			return nil, err
		}
		p.bootstrap = bootstrap.TargetID
		p.owned[bootstrap.TargetID] = true
	}
	events, stop := p.client.Subscribe(512)
	go func() {
		defer stop()
		for event := range events {
			if event.Method == "Target.targetDestroyed" {
				var x struct {
					TargetID string `json:"targetId"`
				}
				if json.Unmarshal(event.Params, &x) == nil {
					p.mu.Lock()
					delete(p.owned, x.TargetID)
					delete(p.attached, x.TargetID)
					p.mu.Unlock()
				}
			}
			if event.Method == "Target.detachedFromTarget" {
				var x struct {
					SessionID string `json:"sessionId"`
				}
				if json.Unmarshal(event.Params, &x) == nil {
					p.mu.Lock()
					for id, session := range p.attached {
						if session == x.SessionID {
							delete(p.attached, id)
						}
					}
					p.mu.Unlock()
				}
			}
			if event.Method == "Target.targetCreated" {
				var x struct {
					TargetInfo struct{ TargetID, Type, OpenerID string } `json:"targetInfo"`
				}
				if json.Unmarshal(event.Params, &x) == nil && x.TargetInfo.Type == "page" {
					p.mu.Lock()
					if p.owned[x.TargetInfo.OpenerID] {
						p.owned[x.TargetInfo.TargetID] = true
					}
					p.mu.Unlock()
				}
			}
		}
	}()
	_ = p.client.Call(ctx, "", "Target.setDiscoverTargets", map[string]any{"discover": true}, nil)
	success = true
	m.processes[sid] = p
	return p, nil
}
func (m *Managed) List(ctx context.Context, sid string) ([]Target, error) {
	p, e := m.get(ctx, sid)
	if e != nil {
		return nil, e
	}
	var r struct {
		TargetInfos []struct {
			TargetID         string `json:"targetId"`
			URL, Title, Type string
		} `json:"targetInfos"`
	}
	if e = p.client.Call(ctx, "", "Target.getTargets", map[string]any{}, &r); e != nil {
		return nil, e
	}
	out := []Target{}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, t := range r.TargetInfos {
		if t.Type == "page" && p.owned[t.TargetID] {
			out = append(out, Target{ID: t.TargetID, URL: t.URL, Title: t.Title, Type: t.Type})
		}
	}
	return out, nil
}
func (m *Managed) Create(ctx context.Context, sid, url string) (Target, error) {
	p, e := m.get(ctx, sid)
	if e != nil {
		return Target{}, e
	}
	p.mu.Lock()
	bootstrap := p.bootstrap
	p.bootstrap = ""
	owned := p.owned[bootstrap]
	p.mu.Unlock()
	if bootstrap != "" && owned {
		var info struct {
			TargetInfo struct{ URL, Title string } `json:"targetInfo"`
		}
		if err := p.client.Call(ctx, "", "Target.getTargetInfo", map[string]any{"targetId": bootstrap}, &info); err == nil && info.TargetInfo.URL == "about:blank" && (info.TargetInfo.Title == "" || info.TargetInfo.Title == "about:blank") {
			if err = m.Call(ctx, sid, bootstrap, "Page.navigate", map[string]any{"url": url}, nil); err != nil {
				return Target{}, err
			}
			return Target{ID: bootstrap, URL: url, Type: "page"}, nil
		}
	}
	var r struct {
		TargetID string `json:"targetId"`
	}
	e = p.client.Call(ctx, "", "Target.createTarget", map[string]any{"url": url}, &r)
	if e != nil {
		return Target{}, e
	}
	p.mu.Lock()
	p.owned[r.TargetID] = true
	p.mu.Unlock()
	return Target{ID: r.TargetID, URL: url, Type: "page"}, nil
}
func (m *Managed) Call(ctx context.Context, sid, tid, method string, params map[string]any, out any) error {
	p, e := m.get(ctx, sid)
	if e != nil {
		return e
	}
	p.attachMu.Lock()
	p.mu.Lock()
	if !p.owned[tid] {
		p.mu.Unlock()
		p.attachMu.Unlock()
		return errors.New("target is not owned by this browser session")
	}
	session := p.attached[tid]
	if p.bootstrap == tid && (method == "Page.navigate" || method == "Page.reload" || method == "Page.navigateToHistoryEntry" || strings.HasPrefix(method, "Input.") || strings.HasPrefix(method, "DOM.")) {
		p.bootstrap = ""
	}
	p.mu.Unlock()
	if session == "" {
		var r struct {
			SessionID string `json:"sessionId"`
		}
		if e = p.client.Call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": tid, "flatten": true}, &r); e != nil {
			p.attachMu.Unlock()
			return e
		}
		session = r.SessionID
		p.mu.Lock()
		p.attached[tid] = session
		p.mu.Unlock()
	}
	p.attachMu.Unlock()
	if strings.HasPrefix(method, "Browser.") {
		return p.client.Call(ctx, "", method, params, out)
	}
	return p.client.Call(ctx, session, method, params, out)
}
func (m *Managed) Activate(ctx context.Context, sid, tid string) error {
	return m.browserTarget(ctx, sid, tid, "Target.activateTarget")
}
func (m *Managed) CloseTab(ctx context.Context, sid, tid string) error {
	p, err := m.get(ctx, sid)
	if err != nil {
		return err
	}
	if err = m.browserTarget(ctx, sid, tid, "Target.closeTarget"); err != nil {
		return err
	}
	// The close acknowledgement can precede targetDestroyed/getTargets removal.
	// Remove ownership immediately so the next action cannot select the dead tab.
	p.mu.Lock()
	delete(p.owned, tid)
	delete(p.attached, tid)
	p.mu.Unlock()
	return nil
}
func (m *Managed) browserTarget(ctx context.Context, sid, tid, method string) error {
	p, e := m.get(ctx, sid)
	if e != nil {
		return e
	}
	p.mu.Lock()
	ok := p.owned[tid]
	p.mu.Unlock()
	if !ok {
		return errors.New("target not owned")
	}
	return p.client.Call(ctx, "", method, map[string]any{"targetId": tid}, nil)
}
func (m *Managed) CloseSession(sid string) error {
	m.mu.Lock()
	p := m.processes[sid]
	delete(m.processes, sid)
	m.mu.Unlock()
	if p == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = p.client.Call(ctx, "", "Browser.close", map[string]any{}, nil)
	p.client.Close()
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
	return nil
}
func (m *Managed) Close() error {
	m.mu.Lock()
	m.closed = true
	ids := []string{}
	for id := range m.processes {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.CloseSession(id)
	}
	return nil
}
func (m *Managed) Client(ctx context.Context, sid string) (*cdp.Client, error) {
	p, e := m.get(ctx, sid)
	if e != nil {
		return nil, e
	}
	return p.client, nil
}
