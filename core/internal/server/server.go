// Package server exposes an authenticated, loopback-only control plane. Browser
// pages never receive the admin token. Scoped MCP credentials cannot approve
// their own operations, edit configuration, start processes or proxy SSH hosts.
package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"svolo.local/core/internal/agent"
	"svolo.local/core/internal/atp"
	"svolo.local/core/internal/browser"
	gh "svolo.local/core/internal/github"
	"svolo.local/core/internal/gitops"
	"svolo.local/core/internal/identity"
	"svolo.local/core/internal/mcp"
	"svolo.local/core/internal/piruntime"
	"svolo.local/core/internal/project"
	"svolo.local/core/internal/ssh"
	"svolo.local/core/internal/store"
	"svolo.local/core/internal/transfer"
	"svolo.local/core/internal/vault"
	"svolo.local/core/internal/workflow"
)

const Version = identity.Version

//go:embed web/*
var webFiles embed.FS

type Options struct {
	Data, Browser, Chromium    string
	ChromiumProxy              string
	Headless, NoSandboxForTest bool
	Transport                  browser.Transport
	Shutdown                   func()
	ArtifactsDir               string
}
type scope struct {
	ID      string    `json:"id"`
	Hash    string    `json:"hash"`
	Session string    `json:"session"`
	Label   string    `json:"label"`
	Created time.Time `json:"created"`
}
type credential struct {
	Admin         bool
	Session, Hash string
}
type mcpSession struct {
	Hash, Session string
	Used          time.Time
}
type remoteTool struct {
	client mcp.Client
	name   string
	tool   agent.Tool
}
type Server struct {
	Runtime          *piruntime.Manager
	ATP              *atp.Scheduler
	Git              *gitops.Manager
	GitHub           *gh.Manager
	Projects         *project.Repository
	Store            *store.Store
	Engine           *browser.Engine
	Bridge           *browser.Bridge
	Agents           *agent.Manager
	SSH              *ssh.Manager
	Config           *configStore
	Token            string
	Vault            *vault.Vault
	Transfers        *transfer.Manager
	Computer         *computerState
	Mode             string
	mu               sync.Mutex
	sessionLifecycle sync.RWMutex
	scopes           map[string]scope
	mcpSessions      map[string]mcpSession
	ext              map[string]remoteTool
	clients          []mcp.Client
	recordings       map[string]*recording
	routines         *workflow.Runner
	ctx              context.Context
	cancel           context.CancelFunc
	web              http.Handler
	slots            chan struct{}
	streams          chan struct{}
	once             sync.Once
	mcpRefresh       sync.Mutex
	shutdown         func()
	artifactsDir     string
}

func New(opts Options) (*Server, error) {
	if opts.Data == "" {
		return nil, errors.New("explicit data directory required")
	}
	st, err := store.Open(opts.Data)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Server, error) { st.Close(); return nil, err }
	cfg, err := loadConfig(st)
	if err != nil {
		return fail(err)
	}
	projects, err := project.New(st)
	if err != nil {
		return fail(err)
	}
	tokenPath := filepath.Join(st.Root, "auth.token")
	tokenBytes, err := os.ReadFile(tokenPath)
	if os.IsNotExist(err) {
		tokenBytes = []byte(secret())
		err = store.Atomic(tokenPath, tokenBytes, 0600)
	}
	if err != nil {
		return fail(err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if len(token) < 32 {
		return fail(errors.New("invalid auth.token; refuses weak credentials"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{Projects: projects, Store: st, Config: cfg, Token: token, Mode: opts.Browser, ctx: ctx, cancel: cancel, scopes: map[string]scope{}, mcpSessions: map[string]mcpSession{}, ext: map[string]remoteTool{}, recordings: map[string]*recording{}, slots: make(chan struct{}, 64), streams: make(chan struct{}, 8)}
	s.shutdown = opts.Shutdown
	s.artifactsDir = opts.ArtifactsDir
	s.Transfers, err = transfer.New(st)
	if err != nil {
		cancel()
		return fail(err)
	}
	s.Vault = vault.Open(st.Root)
	if key := os.Getenv("SVOLO_VAULT_KEY"); key != "" {
		if err := s.Vault.Unlock(key); err != nil {
			cancel()
			return fail(err)
		}
	}
	var trans browser.Transport = opts.Transport
	if trans == nil {
		switch opts.Browser {
		case "electron":
			s.Bridge = browser.NewBridge()
			trans = s.Bridge
		case "managed", "":
			s.Mode = "managed"
			trans = browser.NewManaged(browser.ManagedOptions{Executable: opts.Chromium, Proxy: opts.ChromiumProxy, Root: st.Root, Headless: opts.Headless, NoSandboxForTest: opts.NoSandboxForTest})
		default:
			cancel()
			return fail(errors.New("browser must be managed or electron"))
		}
	}
	s.Engine = browser.NewEngine(trans, st.Root)
	s.Engine.Events = func(sid, kind string, data any) { _, _ = st.Append(sid, kind, data) }
	s.Agents = agent.NewManager(st)
	s.Runtime = piruntime.New(st, os.Getenv("SVOLO_PI_BIN"))
	s.ATP = atp.New(st, s.Runtime, atp.Librarian{Path: os.Getenv("SVOLO_ATP_LIBRARIAN"), Python: os.Getenv("SVOLO_PYTHON_BIN")})
	home, _ := os.UserHomeDir()
	s.Git = &gitops.Manager{Home: home}
	s.GitHub = gh.New(st)
	if err = s.initComputer(); err != nil {
		s.Close()
		return nil, err
	}
	s.Agents.Tools = s.Tools
	s.Agents.Providers = func() []agent.Provider {
		ps := s.Config.Get().Providers
		for i := range ps {
			ps[i].ResolveSecret = s.Vault.Get
		}
		return ps
	}
	s.Agents.TakeControl = func(sid, owner string) {
		current, err := s.Engine.Control(sid, "")
		if err == nil && current.Owner != owner {
			_, _ = s.Engine.Control(sid, owner)
		}
	}
	s.Agents.BrowserContext = func(ctx context.Context, sid string) (any, error) {
		return s.Engine.Observe(ctx, sid, "tabs", nil)
	}
	s.Agents.Execute = func(ctx context.Context, sid, name string, args map[string]any) (any, error) {
		return s.doTool(ctx, sid, "agent", name, args, 0)
	}
	s.SSH = ssh.NewManager()
	s.SSH.ResolveSecret = s.Vault.Get
	s.routines = workflow.New(st, filepath.Join(st.Root, "routines"))
	_ = os.MkdirAll(s.routines.Root, 0700)
	s.routines.Execute = func(ctx context.Context, sid, name string, args map[string]any) (any, error) {
		actor, _ := ctx.Value(actorKey{}).(string)
		if actor == "" {
			actor = "agent"
		}
		if actor == "agent" && !s.readOnly(name) {
			if err := s.Agents.Ask(ctx, "routine", sid, name, args); err != nil {
				return nil, err
			}
		}
		return s.doTool(ctx, sid, actor, name, args, 1)
	}
	s.routines.Notify = func(sid, msg string) { _, _ = s.Store.Append(sid, "routine.human", map[string]any{"message": msg}) }
	s.routines.CloseBrowser = trans.CloseSession
	var saved []scope
	if err = st.Read("token-scopes", &saved); err != nil && !os.IsNotExist(err) {
		s.Close()
		return nil, err
	}
	for _, x := range saved {
		s.scopes[x.Hash] = x
	}
	static, _ := fs.Sub(webFiles, "web")
	s.web = http.FileServer(http.FS(static))
	return s, nil
}

type actorKey struct{}

func secret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func digest(token string) string { h := sha256.Sum256([]byte(token)); return hex.EncodeToString(h[:]) }
func (s *Server) Close() {
	s.once.Do(func() {
		s.cancel()
		if s.ATP != nil {
			s.ATP.Close()
		}
		if s.Runtime != nil {
			s.Runtime.Close()
		}
		if s.Computer != nil {
			s.Computer.Helper.Close()
		}
		if s.Agents != nil {
			s.Agents.Close()
		}
		s.mu.Lock()
		recs := []*recording{}
		for _, r := range s.recordings {
			recs = append(recs, r)
		}
		clients := append([]mcp.Client(nil), s.clients...)
		s.mu.Unlock()
		for _, r := range recs {
			r.cancel()
			<-r.done
		}
		for _, c := range clients {
			_ = c.Close()
		}
		if s.SSH != nil {
			s.SSH.Close()
		}
		if s.Engine != nil {
			_ = s.Engine.Transport.Close()
		}
		if s.Vault != nil {
			s.Vault.Lock()
		}
		_ = s.Store.Close()
	})
}
func (s *Server) authenticate(r *http.Request) (credential, bool) {
	a := r.Header.Get("Authorization")
	if !strings.HasPrefix(a, "Bearer ") {
		return credential{}, false
	}
	token := strings.TrimPrefix(a, "Bearer ")
	if len(token) > 256 {
		return credential{}, false
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.Token)) == 1 {
		return credential{Admin: true, Hash: digest(token)}, true
	}
	hash := digest(token)
	s.mu.Lock()
	sc, ok := s.scopes[hash]
	s.mu.Unlock()
	return credential{Session: sc.Session, Hash: hash}, ok
}
func loopbackHost(raw string) bool {
	host, _, err := net.SplitHostPort(raw)
	if err != nil {
		host = raw
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func failHTTP(w http.ResponseWriter, status int, err error) {
	jsonOut(w, status, map[string]any{"error": err.Error()})
}
func decode(r *http.Request, out any) error {
	d := json.NewDecoder(io.LimitReader(r.Body, (16<<20)+1))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("exactly one JSON object required")
	}
	return nil
}
func method(r *http.Request, v string) error {
	if r.Method != v {
		return errors.New("method must be " + v)
	}
	return nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	if !loopbackHost(r.Host) {
		failHTTP(w, 403, errors.New("loopback Host required"))
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, e := url.Parse(origin)
		if e != nil || u.Host != r.Host || u.Scheme != "http" || u.Path != "" {
			failHTTP(w, 403, errors.New("cross-origin requests refused"))
			return
		}
	}
	if !strings.HasPrefix(r.URL.Path, "/v1/") {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(405)
			return
		}
		s.web.ServeHTTP(w, r)
		return
	}
	cred, ok := s.authenticate(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		failHTTP(w, 401, errors.New("authentication required"))
		return
	}
	if !cred.Admin && r.URL.Path != "/v1/mcp" {
		failHTTP(w, 403, errors.New("scoped tool credential cannot access the control plane"))
		return
	}
	if r.URL.Path == "/v1/events/stream" {
		select {
		case s.streams <- struct{}{}:
			defer func() { <-s.streams }()
		default:
			failHTTP(w, 429, errors.New("event stream budget exceeded"))
			return
		}
		s.stream(w, r)
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		failHTTP(w, 429, errors.New("concurrent request budget exceeded"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<20)

	// Serialize deletion with all finite session operations; streams have already
	// been dispatched above and are read-only.
	if r.URL.Path == "/v1/sessions" && r.Method == "DELETE" {
		s.sessionLifecycle.Lock()
		defer s.sessionLifecycle.Unlock()
	} else {
		s.sessionLifecycle.RLock()
		defer s.sessionLifecycle.RUnlock()
	}
	if r.URL.Path == "/v1/mcp" {
		s.handleMCP(w, r, cred)
		return
	}
	var value any
	var err error
	switch r.URL.Path {
	case "/v1/projects/github":
		value, err = s.githubRequest(r)
	case "/v1/atp/runs", "/v1/atp/start", "/v1/atp/stop", "/v1/atp/held", "/v1/projects/atp", "/v1/git":
		value, err = s.projectRuntime(r)
	case "/v1/runtime", "/v1/runtime/send", "/v1/runtime/ui", "/v1/runtime/stop", "/v1/runtime/events":
		value, err = s.runtimeRequest(r)
	case "/v1/projects/board", "/v1/projects/laments":
		value, err = s.projectRequest(r)
	case "/v1/computer/settings", "/v1/computer/capabilities", "/v1/computer/native":
		value, err = s.computerRequest(r)
	case "/v1/transfers", "/v1/transfers/status", "/v1/transfers/chunk", "/v1/transfers/commit", "/v1/transfers/abort", "/v1/transfers/download":
		value, err = s.transferRequest(r)
	case "/v1/daemon/stop":
		err = method(r, "POST")
		if err == nil {
			if s.shutdown == nil {
				err = errors.New("daemon shutdown callback unavailable")
			} else {
				value = map[string]bool{"stopping": true}
				go func() { time.Sleep(100 * time.Millisecond); s.shutdown() }()
			}
		}
	case "/v1/vault/status":
		err = method(r, "GET")
		value = map[string]any{"unlocked": s.Vault.Unlocked(), "externalKeyRequired": true, "contains": "credentials only; profiles and conversations are not encrypted"}
	case "/v1/vault/unlock":
		var p struct {
			Key string `json:"key"`
		}
		if err = method(r, "POST"); err == nil {
			err = decode(r, &p)
		}
		if err == nil {
			err = s.Vault.Unlock(p.Key)
			value = map[string]bool{"unlocked": err == nil}
		}
	case "/v1/vault/lock":
		if err = method(r, "POST"); err == nil {
			s.Vault.Lock()
			value = map[string]bool{"locked": true}
		}
	case "/v1/credentials":
		if r.Method == "GET" {
			value, err = s.Vault.List()
		} else if r.Method == "PUT" {
			var p struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			}
			if err = decode(r, &p); err == nil {
				err = s.Vault.Put(p.Name, p.Value)
				value = map[string]bool{"stored": err == nil}
			}
		} else if r.Method == "DELETE" {
			err = s.Vault.Delete(r.URL.Query().Get("name"))
			value = map[string]bool{"deleted": err == nil}
		} else {
			err = errors.New("GET, PUT or DELETE required")
		}
	case "/v1/events/cursor":
		err = method(r, "GET")
		value = s.Store.Range()
	case "/v1/health":
		err = method(r, "GET")
		value = map[string]any{"ok": true, "protocol": 1, "version": Version, "browser": s.Mode, "productionQualified": false, "features": map[string]bool{"tools": true, "agents": true, "mcpClient": true, "mcpServer": true, "ssh": true, "cef": false}}
	case "/v1/config":
		if r.Method == "GET" {
			value = s.Config.Get()
		} else if r.Method == "PUT" {
			var c Config
			if err = decode(r, &c); err == nil {
				err = s.Config.Set(c)
				value = c
			}
		} else {
			err = errors.New("GET or PUT required")
		}
	case "/v1/sessions":
		if r.Method == "GET" {
			value = s.Config.Get().Sessions
		} else if r.Method == "POST" {
			var session Session
			if err = decode(r, &session); err == nil {
				err = s.Config.Upsert(session)
				value = session
			}
		} else if r.Method == "DELETE" {
			err = s.deleteSession(r.URL.Query().Get("session"))
			value = map[string]bool{"deleted": err == nil}
		} else {
			err = errors.New("GET, POST or DELETE required")
		}

	case "/v1/control":
		if r.Method == "GET" {
			value, err = s.Engine.Control(r.URL.Query().Get("session"), "")
		} else if r.Method == "POST" {
			var p struct {
				Session string `json:"session"`
				Owner   string `json:"owner"`
			}
			if err = decode(r, &p); err == nil {
				if p.Owner == "human" {
					_ = s.Agents.Stop(p.Session)
				}
				value, err = s.Engine.Control(p.Session, p.Owner)
			}
		} else {
			err = errors.New("GET or POST required")
		}
	case "/v1/tools":
		err = method(r, "GET")
		value = s.Tools()
	case "/v1/tools/call":
		var p toolRequest
		if err = method(r, "POST"); err == nil {
			err = decode(r, &p)
		}
		if err == nil {
			c, e := s.Engine.Control(p.Session, "")
			err = e
			readOnly := s.readOnly(browser.Canonical(p.Name))
			if err == nil && !readOnly && c.Owner != "human" {
				if s.Agents.Busy(p.Session) {
					err = errors.New("take human control before sending manual actions")
				} else {
					_, err = s.Engine.Control(p.Session, "human")
				}
			}
			if err == nil {
				value, err = s.doTool(r.Context(), p.Session, "human", p.Name, p.Arguments, 0)
			}
		}
	case "/v1/browser/tabs":
		err = method(r, "GET")
		sid := r.URL.Query().Get("session")
		if _, ok := s.Config.Session(sid); !ok {
			err = errors.New("unknown session")
		}
		if err == nil {
			value, err = s.Engine.Observe(r.Context(), sid, "tabs", nil)
		}
	case "/v1/view":
		err = method(r, "GET")
		sid := r.URL.Query().Get("session")
		if _, ok := s.Config.Session(sid); !ok {
			err = errors.New("unknown session")
		}
		if err == nil {
			a := map[string]any{}
			if tab := r.URL.Query().Get("tab"); tab != "" {
				a["tab"] = tab
			}
			value, err = s.Engine.Observe(r.Context(), sid, "screenshot", a)
		}
	case "/v1/input":
		var p struct {
			Session   string         `json:"session"`
			Arguments map[string]any `json:"arguments"`
		}
		if err = method(r, "POST"); err == nil {
			err = decode(r, &p)
		}
		if err == nil {
			_ = s.Agents.Stop(p.Session)
			_, _ = s.Engine.Control(p.Session, "human")
			value, err = s.Engine.Run(r.Context(), p.Session, "human", "input", p.Arguments)
		}
	case "/v1/runs":
		if r.Method == "GET" {
			value = s.Agents.List()
		} else if r.Method == "POST" {
			var p agent.RunRequest
			if err = decode(r, &p); err == nil {
				if _, exists := s.Config.Session(p.Session); !exists {
					err = errors.New("register the workspace session first")
				} else {
					value, err = s.Agents.Start(p)
				}
			}
		} else {
			err = errors.New("GET or POST required")
		}
	case "/v1/runs/stop":
		var p struct {
			ID string `json:"id"`
		}
		if err = method(r, "POST"); err == nil {
			err = decode(r, &p)
		}
		if err == nil {
			err = s.Agents.Stop(p.ID)
			value = map[string]bool{"stopped": err == nil}
		}
	case "/v1/approvals":
		if r.Method == "GET" {
			value = s.Agents.Approvals()
		} else if r.Method == "POST" {
			var p struct {
				ID       string `json:"id"`
				Approved bool   `json:"approved"`
			}
			if err = decode(r, &p); err == nil {
				err = s.Agents.Approve(p.ID, p.Approved)
				value = map[string]bool{"recorded": err == nil}
			}
		} else {
			err = errors.New("GET or POST required")
		}
	case "/v1/events":
		err = method(r, "GET")
		after, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
		cursor := s.Store.Range()
		w.Header().Set("X-Svolo-Events-First", strconv.FormatUint(cursor.First, 10))
		if after > 0 && cursor.First > 0 && after < cursor.First-1 {
			w.Header().Set("X-Svolo-Events-Gap", "true")
		}
		value = s.Store.Events(after, r.URL.Query().Get("session"), 1000)
	case "/v1/artifacts":
		err = method(r, "GET")
		if err == nil {
			value, err = browser.ListArtifacts(s.Store.Root, r.URL.Query().Get("session"))
		}
	case "/v1/artifact":
		if err = method(r, "GET"); err == nil {
			var a browser.Artifact
			a, err = browser.VerifyArtifact(s.Store.Root, r.URL.Query().Get("session"), r.URL.Query().Get("id"))
			if err == nil {
				f, e := os.Open(a.Path)
				if e != nil {
					err = e
				} else {
					defer f.Close()
					w.Header().Set("Content-Type", "application/octet-stream")
					w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", a.Name))
					http.ServeContent(w, r, a.Name, a.Created, f)
					return
				}
			}
		}
	case "/v1/tokens":
		value, err = s.tokens(r)
	case "/v1/mcp/refresh":
		err = method(r, "POST")
		if err == nil {
			value, err = s.refreshMCP(r.Context())
		}
	case "/v1/hosts/bootstrap":
		var p struct {
			ID      string               `json:"id"`
			Options ssh.BootstrapOptions `json:"options"`
		}
		if err = method(r, "POST"); err == nil {
			err = decode(r, &p)
		}
		if err == nil {
			value, err = s.bootstrapHost(r.Context(), p.ID, p.Options)
		}
	case "/v1/hosts/detect":
		var p struct {
			ID string `json:"id"`
		}
		if err = method(r, "POST"); err == nil {
			err = decode(r, &p)
		}
		if err == nil {
			h, ok := s.Config.Host(p.ID)
			if !ok {
				err = errors.New("unknown host")
			} else {
				value, err = s.SSH.Detect(r.Context(), h)
			}
		}
	case "/v1/hosts/status":
		err = method(r, "GET")
		value = s.SSH.Status()
	case "/v1/hosts/resolve":
		var p struct {
			Alias string `json:"alias"`
		}
		if err = method(r, "POST"); err == nil {
			err = decode(r, &p)
		}
		if err == nil {
			value, err = s.SSH.Resolve(r.Context(), p.Alias)
		}
	case "/v1/hosts/connect", "/v1/hosts/disconnect":
		var p struct {
			ID string `json:"id"`
		}
		if err = method(r, "POST"); err == nil {
			err = decode(r, &p)
		}
		if err == nil {
			if strings.HasSuffix(r.URL.Path, "disconnect") {
				err = s.SSH.Disconnect(p.ID)
				value = map[string]bool{"disconnected": err == nil}
			} else {
				found := false
				for _, h := range s.Config.Get().Hosts {
					if h.ID == p.ID {
						found = true
						value, err = s.SSH.Connect(r.Context(), h)
						break
					}
				}
				if !found {
					err = errors.New("unknown host")
				}
			}
		}
	case "/v1/remote-artifact":
		err = method(r, "GET")
		if err == nil {
			q := r.URL.Query()
			sid, id := q.Get("session"), q.Get("id")
			if !store.ValidID(sid) || !store.ValidID(id) {
				err = errors.New("invalid artifact identity")
			} else {
				var rr *http.Response
				rr, err = s.SSH.Do(r.Context(), q.Get("host"), "GET", "/v1/artifact?session="+url.QueryEscape(sid)+"&id="+url.QueryEscape(id), nil)
				if err == nil {
					defer rr.Body.Close()
					w.Header().Set("Content-Type", "application/octet-stream")
					w.Header().Set("Content-Disposition", "attachment")
					w.WriteHeader(rr.StatusCode)
					_, _ = io.Copy(w, io.LimitReader(rr.Body, 64<<20))
					return
				}
			}
		}
	case "/v1/remote":
		var p struct {
			Host   string          `json:"host"`
			Method string          `json:"method"`
			Path   string          `json:"path"`
			Body   json.RawMessage `json:"body"`
		}
		if err = method(r, "POST"); err == nil {
			err = decode(r, &p)
		}
		if err == nil {
			if strings.HasPrefix(p.Path, "/v1/remote") || strings.HasPrefix(p.Path, "/v1/bridge/") || strings.HasPrefix(p.Path, "/v1/computer/native") || strings.HasPrefix(p.Path, "/v1/events/stream") {
				err = errors.New("recursive proxy and remote UI bridges refused")
			} else {
				var rr *http.Response
				rr, err = s.SSH.Do(r.Context(), p.Host, p.Method, p.Path, strings.NewReader(string(p.Body)))
				if err == nil {
					defer rr.Body.Close()
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(rr.StatusCode)
					_, _ = io.Copy(w, io.LimitReader(rr.Body, 32<<20))
					return
				}
			}
		}
	case "/v1/bridge/poll":
		err = method(r, "GET")
		if err == nil {
			if s.Bridge == nil {
				err = errors.New("not an Electron bridge daemon")
			} else {
				ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
				defer cancel()
				value, err = s.Bridge.Poll(ctx)
				if errors.Is(err, context.DeadlineExceeded) {
					w.WriteHeader(204)
					return
				}
			}
		}
	case "/v1/bridge/authorize":
		var p struct {
			ID string `json:"id"`
		}
		if err = method(r, "POST"); err == nil {
			err = decode(r, &p)
		}
		if err == nil {
			if s.Bridge == nil {
				err = errors.New("no bridge")
			} else {
				value, err = s.Bridge.Authorize(p.ID, s.Engine)
			}
		}
	case "/v1/bridge/result":
		var p browser.BridgeReply
		if err = method(r, "POST"); err == nil {
			err = decode(r, &p)
		}
		if err == nil {
			if s.Bridge == nil {
				err = errors.New("no bridge")
			} else {
				s.Bridge.Reply(p)
				value = map[string]bool{"received": true}
			}
		}
	case "/v1/bridge/event":
		var p struct {
			Session string          `json:"session"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err = method(r, "POST"); err == nil {
			err = decode(r, &p)
		}
		if err == nil {
			s.Engine.ObserveEvent(p.Session, p.Method, p.Params)
			value = map[string]bool{"received": true}
		}
	case "/v1/extensions/browser":
		var p struct {
			Session   string         `json:"session"`
			Arguments map[string]any `json:"arguments"`
		}
		if err = method(r, "POST"); err == nil {
			err = decode(r, &p)
		}
		if err == nil {
			if s.Bridge == nil {
				err = errors.New("desktop browser adapter requires Electron")
			} else if s.Agents.Busy(p.Session) {
				err = errors.New("the Go agent already owns this session")
			} else {
				act, _ := p.Arguments["action"].(string)
				if act == "evaluate" {
					err = s.Agents.Ask(r.Context(), "pi", p.Session, "extension-evaluate", p.Arguments)
				}
				if err == nil {
					value, err = s.Engine.Run(r.Context(), p.Session, "agent", "extension-action", p.Arguments)
				}
			}
		}
	default:
		failHTTP(w, 404, errors.New("endpoint not found"))
		return
	}
	if err != nil {
		failHTTP(w, 400, err)
		return
	}
	if value == nil {
		value = map[string]bool{"ok": true}
	}
	jsonOut(w, 200, value)
}
func (s *Server) tokens(r *http.Request) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	persist := func() error {
		list := []scope{}
		for _, x := range s.scopes {
			list = append(list, x)
		}
		return s.Store.Write("token-scopes", list)
	}
	switch r.Method {
	case "GET":
		list := []scope{}
		for _, x := range s.scopes {
			x.Hash = ""
			list = append(list, x)
		}
		return list, nil
	case "DELETE":
		id := r.URL.Query().Get("id")
		for h, x := range s.scopes {
			if x.ID == id {
				delete(s.scopes, h)
				return map[string]bool{"revoked": true}, persist()
			}
		}
		return nil, errors.New("token id not found")
	case "POST":
		var p struct {
			Session string `json:"session"`
			Label   string `json:"label"`
		}
		if err := decode(r, &p); err != nil {
			return nil, err
		}
		if _, ok := s.Config.Session(p.Session); !ok {
			return nil, errors.New("unknown session")
		}
		if len(s.scopes) >= 1000 {
			return nil, errors.New("credential limit")
		}
		token := secret()
		x := scope{ID: store.ID(), Hash: digest(token), Session: p.Session, Label: p.Label, Created: time.Now().UTC()}
		s.scopes[x.Hash] = x
		if err := persist(); err != nil {
			delete(s.scopes, x.Hash)
			return nil, err
		}
		return map[string]any{"id": x.ID, "session": x.Session, "token": token, "scope": "mcp-tools-only"}, nil
	}
	return nil, errors.New("GET, POST or DELETE required")
}
