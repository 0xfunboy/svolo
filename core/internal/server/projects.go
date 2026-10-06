package server

import (
	"errors"
	"net/http"
	"strings"
	"svolo.local/core/internal/project"
	"svolo.local/core/internal/store"
)

func (s *Server) projectRequest(r *http.Request) (any, error) {
	domain := strings.TrimPrefix(r.URL.Path, "/v1/projects/")
	if r.Method == "GET" {
		return s.Projects.Get(domain)
	}
	if e := method(r, "POST"); e != nil {
		return nil, e
	}
	var m project.Mutation
	if e := decode(r, &m); e != nil {
		return nil, e
	}
	return s.Projects.Mutate(domain, m)
}

// Tools see only the registered session's project; global GUI administration
// uses separate authenticated routes. Import and cross-project moves are refused.
func (s *Server) projectTool(sid, domain string, a map[string]any) (any, error) {
	session, ok := s.Config.Session(sid)
	if !ok || session.Workspace == "" {
		return nil, errors.New("registered project workspace required")
	}
	cwd := project.ProjectOf(session.Workspace)
	snap, e := s.Projects.Get(domain)
	if e != nil {
		return nil, e
	}
	key := "cards"
	if domain == "laments" {
		key = "laments"
	}
	filtered := []any{}
	for _, r := range snap.Value[key].([]any) {
		if r.(map[string]any)["cwd"] == cwd {
			filtered = append(filtered, r)
		}
	}
	action := arg(a, "action")
	if action == "get" {
		return map[string]any{"version": 1, key: filtered, "revision": snap.Revision}, nil
	}
	if action != "apply" {
		return nil, errors.New("project action must be get or apply")
	}
	op := object(a, "operation")
	kind := arg(op, "type")
	if kind == "import" {
		return nil, errors.New("agents cannot import global project state")
	}
	// Make a copy before adding ownership context.
	clean := map[string]any{}
	for k, v := range op {
		clean[k] = v
	}
	op = clean
	if kind == "add" || kind == "file" {
		op["cwd"] = cwd
	} else {
		found := false
		for _, r := range filtered {
			if r.(map[string]any)["id"] == op["id"] {
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("item is not owned by this workspace")
		}
	}
	if _, e = s.Projects.Mutate(domain, project.Mutation{Op: op, ExpectedRevision: snap.Revision, OperationID: store.ID()}); e != nil {
		return nil, e
	}
	return s.projectTool(sid, domain, map[string]any{"action": "get"})
}
