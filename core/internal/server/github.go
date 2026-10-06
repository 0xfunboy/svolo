package server

import (
	"errors"
	"net/http"
	gh "svolo.local/core/internal/github"
)

func (s *Server) githubRequest(r *http.Request) (any, error) {
	if e := method(r, "POST"); e != nil {
		return nil, e
	}
	var p struct {
		Session  string      `json:"session"`
		Action   string      `json:"action"`
		Refresh  bool        `json:"refresh"`
		Login    *string     `json:"login"`
		Kind     string      `json:"kind"`
		Filter   string      `json:"filter"`
		Input    string      `json:"input"`
		Settings gh.Settings `json:"settings"`
	}
	if e := decode(r, &p); e != nil {
		return nil, e
	}
	if p.Action == "import" {
		return map[string]bool{"imported": true}, s.GitHub.Import(p.Settings)
	}
	w, e := s.Config.Workspace(p.Session)
	if e != nil {
		return nil, e
	}
	switch p.Action {
	case "project":
		return s.GitHub.Project(r.Context(), w.Root, p.Refresh), nil
	case "choose":
		return s.GitHub.Choose(r.Context(), w.Root, p.Login), nil
	case "list":
		return s.GitHub.List(r.Context(), w.Root, p.Kind, p.Filter), nil
	case "lookup":
		return s.GitHub.Lookup(r.Context(), w.Root, p.Input), nil
	}
	return nil, errors.New("unsupported GitHub operation")
}
