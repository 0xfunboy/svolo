package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"svolo.local/core/internal/computer"
	"sync"
)

type computerSettings struct {
	Enabled         bool `json:"enabled"`
	AllowForeground bool `json:"allowForeground"`
}
type computerState struct {
	mu       sync.Mutex
	Settings computerSettings
	Grants   map[string]string
	Helper   *computer.Helper
}

func (s *Server) initComputer() error {
	c := &computerState{Grants: map[string]string{}, Helper: computer.New(s.Store.Root)}
	if err := s.Store.Read("computer-settings", &c.Settings); err != nil && !os.IsNotExist(err) {
		return err
	}
	c.Helper.ConfigureForeground(c.Settings.AllowForeground)
	c.Helper.Notify = func(method string, params json.RawMessage) {
		// Never block the helper's stdout reader on an action holding the app mutex.
		data := append(json.RawMessage(nil), params...)
		go func() {
			var p struct {
				Session string `json:"session"`
				App     string `json:"app"`
			}
			_ = json.Unmarshal(data, &p)
			_, _ = s.Store.Append(p.Session, "computer."+method, data)
			if method == "cancelled" {
				c.mu.Lock()
				if c.Grants[p.App] == p.Session {
					delete(c.Grants, p.App)
				}
				c.mu.Unlock()
				_ = s.Agents.Stop(p.Session)
			}
		}()
	}
	s.Computer = c
	return nil
}
func (s *Server) computerRequest(r *http.Request) (any, error) {
	c := s.Computer
	if r.URL.Path == "/v1/computer/settings" {
		if r.Method == "GET" {
			c.mu.Lock()
			defer c.mu.Unlock()
			return c.Settings, nil
		}
		if err := method(r, "PUT"); err != nil {
			return nil, err
		}
		var p computerSettings
		if err := decode(r, &p); err != nil {
			return nil, err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if err := s.Store.Write("computer-settings", p); err != nil {
			return nil, err
		}
		c.Settings = p
		c.Helper.ConfigureForeground(p.AllowForeground)
		if !p.Enabled {
			c.Grants = map[string]string{}
			c.Helper.Close()
		}
		return p, nil
	}
	if r.URL.Path == "/v1/computer/capabilities" {
		if err := method(r, "GET"); err != nil {
			return nil, err
		}
		return c.Helper.Call(r.Context(), "hello", map[string]any{})
	}
	// Only the private Electron main-process adapter can use the native extension
	// protocol. It applies the configured per-app ComputerAgent approvals first.
	// This path is excluded from renderer route allowlists and SSH proxying.
	if s.Bridge == nil {
		return nil, errors.New("native extension provider is available only to the Electron broker")
	}
	if err := method(r, "POST"); err != nil {
		return nil, err
	}
	var p struct {
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := decode(r, &p); err != nil {
		return nil, err
	}
	raw, err := c.Helper.Call(r.Context(), p.Method, p.Params)
	if err != nil {
		var native *computer.Error
		if errors.As(err, &native) {
			return map[string]any{"error": native}, nil
		}
		return nil, err
	}
	return map[string]any{"result": raw}, nil
}

// Direct Go agent/MCP entry point: independent of pi, with an additional app
// grant that cannot be bypassed by a run-level allowedTools list.
func (s *Server) computerTool(ctx context.Context, sid, actor string, a map[string]any) (any, error) {
	c := s.Computer
	action := arg(a, "action")
	p := object(a, "arguments")
	if p == nil {
		p = map[string]any{}
	}
	allowed := map[string]bool{"list_apps": true, "get_app_state": true, "screenshot": true, "click": true, "drag": true, "scroll": true, "type_text": true, "press_key": true, "set_value": true, "select_text": true, "perform_secondary_action": true, "paste": true, "end": true}
	if !allowed[action] {
		return nil, errors.New("unknown Computer Use action")
	}
	c.mu.Lock()
	enabled := c.Settings.Enabled
	c.mu.Unlock()
	if !enabled {
		return nil, errors.New("Computer Use is disabled; explicitly enable it in settings")
	}
	if action == "list_apps" {
		return c.Helper.Call(ctx, action, map[string]any{})
	}
	if action == "end" {
		c.mu.Lock()
		apps := []string{}
		for app, owner := range c.Grants {
			if owner == sid {
				delete(c.Grants, app)
				apps = append(apps, app)
			}
		}
		c.mu.Unlock()
		for _, app := range apps {
			_, _ = c.Helper.Call(ctx, "overlay_hide", map[string]any{"app": app})
		}
		return map[string]bool{"released": true}, nil
	}
	app := arg(a, "app")
	if strings.TrimSpace(app) == "" {
		return nil, errors.New("explicit application identity required")
	}
	result, err := c.Helper.Call(ctx, "resolve_app", map[string]any{"app": app, "launch": false})
	if err != nil {
		return nil, err
	}
	var target struct {
		ID   string `json:"bundleId"`
		Name string `json:"displayName"`
		PID  int    `json:"pid"`
	}
	if err = json.Unmarshal(result, &target); err != nil || target.ID == "" {
		return nil, errors.New("native application resolution failed")
	}
	c.mu.Lock()
	owner := c.Grants[target.ID]
	c.mu.Unlock()
	if owner != "" && owner != sid {
		return nil, errors.New("application is owned by another session")
	}
	if owner == "" && actor == "agent" {
		if err = s.Agents.Ask(ctx, "computer", sid, "computer-app-access", map[string]any{"app": target.Name, "identity": target.ID, "pid": target.PID}); err != nil {
			return nil, err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.Settings.Enabled {
		return nil, errors.New("Computer Use was disabled")
	}
	if current := c.Grants[target.ID]; current != "" && current != sid {
		return nil, errors.New("application ownership changed")
	}
	if owner == "" {
		ctrl, e := s.Engine.Control(sid, "")
		if actor == "agent" && (e != nil || ctrl.Owner != "agent") {
			return nil, errors.New("control was taken by the human")
		}
		_, err = c.Helper.Call(ctx, "overlay_show", map[string]any{"app": map[string]any{"bundleId": target.ID, "pid": target.PID}, "session": sid, "session_label": sid})
		if err != nil {
			return nil, err
		}
		c.Grants[target.ID] = sid
	}
	p["app"] = map[string]any{"bundleId": target.ID, "pid": target.PID}
	return c.Helper.Call(ctx, action, p)
}
