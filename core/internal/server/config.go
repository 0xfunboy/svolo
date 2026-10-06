package server

import (
	"errors"
	"os"
	"path/filepath"
	"svolo.local/core/internal/agent"
	"svolo.local/core/internal/mcp"
	"svolo.local/core/internal/ssh"
	"svolo.local/core/internal/store"
	"svolo.local/core/internal/workspace"
	"sync"
)

type Session struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Workspace string `json:"workspace,omitempty"`
	AllowExec bool   `json:"allowExec"`
}
type Config struct {
	Version   int              `json:"version"`
	Providers []agent.Provider `json:"providers"`
	MCP       []mcp.Config     `json:"mcpServers"`
	Hosts     []ssh.Host       `json:"hosts"`
	Sessions  []Session        `json:"sessions"`
}
type configStore struct {
	mu    sync.RWMutex
	store *store.Store
	value Config
}

func loadConfig(st *store.Store) (*configStore, error) {
	c := &configStore{store: st, value: Config{Version: 1, Providers: []agent.Provider{}, MCP: []mcp.Config{}, Hosts: []ssh.Host{}, Sessions: []Session{}}}
	if err := st.Read("config", &c.value); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := validateConfig(c.value); err != nil {
		return nil, err
	}
	return c, nil
}
func validateConfig(c Config) error {
	if c.Version != 1 {
		return errors.New("unsupported config version")
	}
	if len(c.Providers) > 32 || len(c.MCP) > 32 || len(c.Hosts) > 64 || len(c.Sessions) > 1000 {
		return errors.New("configuration count limit exceeded")
	}
	seen := map[string]bool{}
	for _, p := range c.Providers {
		if !store.ValidID(p.ID) || seen["p:"+p.ID] {
			return errors.New("invalid or duplicate provider id")
		}
		seen["p:"+p.ID] = true
		if err := p.Validate(); err != nil {
			return err
		}
	}
	for _, h := range c.Hosts {
		if seen["h:"+h.ID] {
			return errors.New("duplicate host")
		}
		seen["h:"+h.ID] = true
		if err := ssh.Validate(h); err != nil {
			return err
		}
	}
	for _, s := range c.Sessions {
		if !store.ValidID(s.ID) || seen["s:"+s.ID] {
			return errors.New("invalid or duplicate session")
		}
		seen["s:"+s.ID] = true
		if s.Workspace != "" {
			if !filepath.IsAbs(s.Workspace) {
				return errors.New("workspace path must be absolute on this host")
			}
		}
	}
	for _, m := range c.MCP {
		if !store.ValidID(m.ID) || seen["m:"+m.ID] {
			return errors.New("invalid or duplicate MCP server id")
		}
		seen["m:"+m.ID] = true
		if (m.Command == "") == (m.URL == "") {
			return errors.New("MCP server requires exactly one command or URL")
		}
		if len(m.Args) > 100 {
			return errors.New("MCP argument limit")
		}
	}
	return nil
}
func (c *configStore) Get() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v := c.value
	v.Providers = append([]agent.Provider{}, v.Providers...)
	v.Hosts = append([]ssh.Host{}, v.Hosts...)
	v.MCP = append([]mcp.Config{}, v.MCP...)
	v.Sessions = append([]Session{}, v.Sessions...)
	return v
}
func (c *configStore) Set(v Config) error {
	if err := validateConfig(v); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.store.Write("config", v); err != nil {
		return err
	}
	c.value = v
	return nil
}
func (c *configStore) Session(id string) (Session, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, s := range c.value.Sessions {
		if s.ID == id {
			return s, true
		}
	}
	return Session{}, false
}
func (c *configStore) Workspace(id string) (workspace.Workspace, error) {
	s, ok := c.Session(id)
	if !ok || s.Workspace == "" {
		return workspace.Workspace{}, errors.New("session has no explicitly registered workspace")
	}
	return workspace.Open(s.Workspace)
}

// Upsert serializes read-modify-write so simultaneous sessions cannot overwrite
// each other. Full configuration PUT deliberately remains last-writer-wins.
func (c *configStore) Upsert(session Session) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := c.value
	v.Sessions = append([]Session{}, v.Sessions...)
	found := false
	for i, x := range v.Sessions {
		if x.ID == session.ID {
			v.Sessions[i] = session
			found = true
			break
		}
	}
	if !found {
		v.Sessions = append(v.Sessions, session)
	}
	if err := validateConfig(v); err != nil {
		return err
	}
	if err := c.store.Write("config", v); err != nil {
		return err
	}
	c.value = v
	return nil
}

func (c *configStore) Host(id string) (ssh.Host, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, h := range c.value.Hosts {
		if h.ID == id {
			return h, true
		}
	}
	return ssh.Host{}, false
}
func (c *configStore) UpsertHost(h ssh.Host) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := c.value
	v.Hosts = append([]ssh.Host(nil), v.Hosts...)
	found := false
	for i := range v.Hosts {
		if v.Hosts[i].ID == h.ID {
			v.Hosts[i] = h
			found = true
			break
		}
	}
	if !found {
		v.Hosts = append(v.Hosts, h)
	}
	if err := validateConfig(v); err != nil {
		return err
	}
	if err := c.store.Write("config", v); err != nil {
		return err
	}
	c.value = v
	return nil
}

// Remove serializes the session edit without changing providers or workspaces.
func (c *configStore) Remove(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := c.value
	v.Sessions = []Session{}
	found := false
	for _, session := range c.value.Sessions {
		if session.ID == id {
			found = true
		} else {
			v.Sessions = append(v.Sessions, session)
		}
	}
	if !found {
		return errors.New("session not found")
	}
	if err := c.store.Write("config", v); err != nil {
		return err
	}
	c.value = v
	return nil
}
