package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"svolo.local/core/internal/agent"
	"svolo.local/core/internal/browser"
	"svolo.local/core/internal/store"
)

type toolRequest struct {
	Session   string         `json:"session"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func (s *Server) Tools() []agent.Tool {
	out := []agent.Tool{}
	for _, t := range builtins() {
		if s.Bridge != nil || !strings.HasPrefix(t.Name, "pi-") {
			out = append(out, t)
		}
	}
	s.mu.Lock()
	for _, t := range s.ext {
		out = append(out, t.tool)
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
func (s *Server) readOnly(name string) bool {
	for _, t := range builtins() {
		if t.Name == name {
			return t.ReadOnly
		}
	}
	return false
}
func arg(a map[string]any, key string) string { v, _ := a[key].(string); return v }
func num(a map[string]any, key string, def float64) float64 {
	switch v := a[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		n, e := v.Float64()
		if e == nil {
			return n
		}
	}
	return def
}
func yes(a map[string]any, key string) bool { v, _ := a[key].(bool); return v }
func object(a map[string]any, key string) map[string]any {
	v, _ := a[key].(map[string]any)
	if v == nil {
		return map[string]any{}
	}
	return v
}
func validateArgs(name string, a map[string]any) error {
	for _, d := range definitions {
		if d.name != name {
			continue
		}
		for _, key := range d.required {
			if _, ok := a[key]; !ok {
				return fmt.Errorf("%s requires %s", name, key)
			}
		}
		for key, value := range a {
			kind, ok := d.props[key]
			if key == "tab" {
				kind = "string"
				ok = true
			}
			if !ok {
				return fmt.Errorf("unknown %s argument: %s", name, key)
			}
			valid := false
			switch kind {
			case "string":
				_, valid = value.(string)
			case "number":
				switch value.(type) {
				case int, int64, float64, json.Number:
					valid = true
				}
			case "boolean":
				_, valid = value.(bool)
			case "object":
				_, valid = value.(map[string]any)
			case "array":
				switch v := value.(type) {
				case []string:
					valid = true
				case []any:
					valid = true
					for _, i := range v {
						if _, ok := i.(string); !ok {
							valid = false
						}
					}
				}
			}
			if !valid {
				return fmt.Errorf("%s argument %s must be %s", name, key, kind)
			}
		}
		return nil
	}
	return errors.New("unknown built-in tool")
}
func (s *Server) doTool(ctx context.Context, sid, actor, name string, a map[string]any, depth int) (any, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if depth > 4 {
		return nil, errors.New("nested tool depth exceeded")
	}
	if !store.ValidID(sid) {
		return nil, errors.New("invalid session")
	}
	if _, ok := s.Config.Session(sid); !ok && !strings.HasPrefix(sid, "task-") {
		return nil, errors.New("register the session first")
	}
	if a == nil {
		a = map[string]any{}
	}
	if actor == "agent" {
		c, err := s.Engine.Control(sid, "")
		if err != nil {
			return nil, err
		}
		if c.Owner != "agent" {
			return nil, errors.New("control_not_owned: human has control")
		}
	}
	s.mu.Lock()
	external, ok := s.ext[name]
	s.mu.Unlock()
	if ok {
		var result any
		err := external.client.Call(ctx, "tools/call", map[string]any{"name": external.name, "arguments": a}, &result)
		return result, err
	}
	if err := validateArgs(name, a); err != nil {
		return nil, err
	}
	var result any
	var err error
	switch name {
	case "project-board", "project-laments":
		return s.projectTool(sid, strings.TrimPrefix(name, "project-"), a)
	case "computer-use":
		return s.computerTool(ctx, sid, actor, a)
	case "pi-kanban", "pi-atp", "pi-lament", "pi-computer":
		if s.Bridge == nil {
			return nil, errors.New("desktop extension domain requires the Electron adapter and live pi session")
		}
		return s.Bridge.Native(ctx, sid, "/"+strings.TrimPrefix(name, "pi-"), object(a, "arguments"))
	case "browser-schema":
		for _, t := range builtins() {
			if t.Name == arg(a, "name") {
				return t, nil
			}
		}
		return nil, errors.New("operation not found")
	case "browser-operation":
		nested := arg(a, "operation")
		if !primitive(nested) {
			return nil, errors.New("wrapper accepts only browser primitives, not policy, workflow or system operations")
		}
		if actor == "agent" && !s.readOnly(nested) {
			if err = s.Agents.Ask(ctx, "browser-operation", sid, nested, object(a, "arguments")); err != nil {
				return nil, err
			}
		}
		return s.doTool(ctx, sid, actor, nested, object(a, "arguments"), depth+1)
	case "workspace-list", "workspace-read", "workspace-write", "workspace-exec":
		w, e := s.Config.Workspace(sid)
		if e != nil {
			return nil, e
		}
		if name != "workspace-exec" {
			target, e := w.Path(arg(a, "path"), name == "workspace-write")
			if e != nil {
				return nil, e
			}
			if s.privatePath(target) {
				return nil, errors.New("daemon private state is not an agent workspace")
			}
		}
		switch name {
		case "workspace-list":
			return w.List(arg(a, "path"))
		case "workspace-read":
			text, e := w.Read(arg(a, "path"), 2<<20)
			return map[string]any{"content": text}, e
		case "workspace-write":
			e := w.Write(arg(a, "path"), arg(a, "content"))
			return map[string]any{"written": e == nil, "path": arg(a, "path")}, e
		case "workspace-exec":
			session, _ := s.Config.Session(sid)
			if !session.AllowExec {
				return nil, errors.New("process execution is disabled for this session")
			}
			args := []string{}
			switch values := a["args"].(type) {
			case []string:
				args = values
			case []any:
				for _, v := range values {
					args = append(args, v.(string))
				}
			}
			return w.Execute(ctx, arg(a, "program"), args, int(num(a, "seconds", 60)))
		}
	case "verify-artifact":
		return browser.VerifyArtifact(s.Store.Root, sid, arg(a, "id"))
	case "profile-import":
		m, ok := s.Engine.Transport.(*browser.Managed)
		if !ok {
			return nil, errors.New("profile import requires a managed Chromium session; Electron's retained shared partition is not overwritten")
		}
		err = m.ImportProfile(ctx, sid, arg(a, "source"), yes(a, "force"))
		return map[string]any{"imported": err == nil}, err
	case "downloads":
		if err = s.Engine.ConfigureDownloads(ctx, sid); err != nil {
			return nil, err
		}
		return s.downloads(sid)
	case "wait-download":
		return s.waitDownload(ctx, sid, a)
	case "record-browser":
		return s.record(ctx, sid, actor, a)
	case "hitl":
		err = s.Agents.Ask(ctx, "human-intervention", sid, "hitl", a)
		return map[string]any{"acknowledged": err == nil}, err
	case "call-routine":
		if depth > 0 {
			return nil, errors.New("nested routines are refused to preserve the single graph budget")
		}
		ctx = context.WithValue(ctx, actorKey{}, actor)
		if resume := arg(a, "resume"); resume != "" {
			return s.routines.Resume(ctx, resume, sid, object(a, "variables"))
		}
		return s.routines.Start(ctx, arg(a, "name"), sid, object(a, "variables"))
	case "browser-task":
		if depth > 0 {
			return nil, errors.New("nested task sessions are refused")
		}
		tool := arg(a, "tool")
		if !primitive(tool) {
			return nil, errors.New("task accepts a browser primitive")
		}
		task := "task-" + store.ID()
		_, _ = s.Engine.Control(task, actor)
		if !yes(a, "persist") {
			defer s.Engine.Transport.CloseSession(task)
		}
		if _, err = s.Engine.Run(ctx, task, actor, "open-browser", map[string]any{"url": arg(a, "url")}); err != nil {
			return nil, err
		}
		if actor == "agent" && !s.readOnly(tool) {
			if err = s.Agents.Ask(ctx, "browser-task", task, tool, object(a, "arguments")); err != nil {
				return nil, err
			}
		}
		result, err = s.doTool(ctx, task, actor, tool, object(a, "arguments"), depth+1)
		return map[string]any{"taskSession": task, "persisted": yes(a, "persist"), "result": result}, err
	case "upload":
		w, e := s.Config.Workspace(sid)
		if e != nil {
			return nil, e
		}
		path, e := w.Path(arg(a, "file"), false)
		if e != nil {
			return nil, e
		}
		if s.privatePath(path) {
			return nil, errors.New("daemon private state cannot be uploaded")
		}
		st, e := os.Stat(path)
		if e != nil || !st.Mode().IsRegular() {
			return nil, errors.New("upload requires a regular workspace file")
		}
		copy := map[string]any{}
		for k, v := range a {
			copy[k] = v
		}
		copy["file"] = path
		a = copy
	}
	result, err = s.Engine.Run(ctx, sid, actor, name, a)
	if err == nil && !s.readOnly(name) {
		s.stepRecording(sid)
	}
	return result, err
}
func primitive(name string) bool {
	switch name {
	case "open-browser", "close-browser", "open-tab", "state", "viewport", "console":
		return true
	}
	for _, d := range definitions {
		if d.name == name {
			return !strings.HasPrefix(d.name, "project-") && d.name != "computer-use" && !strings.HasPrefix(d.name, "pi-") && d.name != "downloads" && d.name != "wait-download" && d.name != "verify-artifact" && d.name != "record-browser" && d.name != "profile-import" && d.name != "browser-task" && d.name != "call-routine" && d.name != "hitl" && !strings.HasPrefix(d.name, "workspace-") && d.name != "browser-schema" && d.name != "browser-operation"
		}
	}
	return false
}

type download struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	ModifiedMS int64  `json:"modifiedMs"`
}

func (s *Server) downloads(sid string) ([]download, error) {
	dir := filepath.Join(s.Store.Root, "downloads", sid)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []download{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []download{}
	for _, e := range entries {
		if e.IsDir() || e.Type()&os.ModeSymlink != 0 {
			continue
		}
		lower := strings.ToLower(e.Name())
		if strings.HasSuffix(lower, ".crdownload") || strings.HasSuffix(lower, ".part") || strings.HasSuffix(lower, ".tmp") {
			continue
		}
		st, err := e.Info()
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		out = append(out, download{Name: e.Name(), Size: st.Size(), ModifiedMS: st.ModTime().UnixMilli()})
		if len(out) >= 2000 {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModifiedMS < out[j].ModifiedMS })
	return out, nil
}
func (s *Server) waitDownload(ctx context.Context, sid string, a map[string]any) (any, error) {
	if err := s.Engine.ConfigureDownloads(ctx, sid); err != nil {
		return nil, err
	}
	sec := num(a, "seconds", 30)
	if sec < 1 || sec > 120 {
		return nil, errors.New("seconds must be 1..120")
	}
	after := int64(num(a, "afterMs", 0))
	if after <= 0 {
		return nil, errors.New("afterMs must be a positive epoch timestamp")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(sec*float64(time.Second)))
	defer cancel()
	tick := time.NewTicker(350 * time.Millisecond)
	defer tick.Stop()
	seen := map[string]download{}
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("download not verified: %w", ctx.Err())
		case <-tick.C:
			entries, err := s.downloads(sid)
			if err != nil {
				return nil, err
			}
			for _, x := range entries {
				if x.ModifiedMS < after || x.Size == 0 || !strings.Contains(x.Name, arg(a, "nameContains")) {
					continue
				}
				old, ok := seen[x.Name]
				seen[x.Name] = x
				if !ok || old != x {
					continue
				}
				if x.Size > 64<<20 {
					return nil, errors.New("download exceeds artifact size budget")
				}
				p := filepath.Join(s.Store.Root, "downloads", sid, x.Name)
				data, err := os.ReadFile(p)
				if err != nil {
					return nil, err
				}
				if int64(len(data)) != x.Size {
					continue
				}
				return browser.SaveArtifact(s.Store.Root, sid, x.Name, data)
			}
		}
	}
}

func (s *Server) privatePath(path string) bool {
	rel, err := filepath.Rel(s.Store.Root, path)
	return err == nil && (rel == "." || (!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".."))
}
