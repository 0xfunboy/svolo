package server

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"svolo.local/core/internal/atp"
	"svolo.local/core/internal/gitops"
	"time"
)

func (s *Server) projectRuntime(r *http.Request) (any, error) {
	if r.URL.Path == "/v1/atp/runs" {
		if e := method(r, "GET"); e != nil {
			return nil, e
		}
		return s.ATP.List(), nil
	}
	if r.URL.Path == "/v1/atp/held" {
		if e := method(r, "GET"); e != nil {
			return nil, e
		}
		return s.ATP.Held(), nil
	}
	if r.URL.Path == "/v1/atp/stop" {
		var p struct {
			ID string `json:"id"`
		}
		if e := method(r, "POST"); e != nil {
			return nil, e
		}
		if e := decode(r, &p); e != nil {
			return nil, e
		}
		c, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		return map[string]bool{"stopped": true}, s.ATP.Stop(c, p.ID)
	}
	if r.URL.Path == "/v1/atp/start" {
		var o atp.RunOptions
		if e := method(r, "POST"); e != nil {
			return nil, e
		}
		if e := decode(r, &o); e != nil {
			return nil, e
		}
		sess, ok := s.Config.Session(o.Session)
		if !ok || !sess.AllowExec || sess.Workspace == "" {
			return nil, errors.New("ATP execution requires an explicitly authorized workspace session")
		}
		a, e := filepath.EvalSymlinks(sess.Workspace)
		if e != nil {
			return nil, e
		}
		b, e := filepath.EvalSymlinks(o.Workspace)
		if e != nil || a != b {
			return nil, errors.New("ATP workspace mismatch")
		}
		return s.ATP.Start(o)
	}
	var p struct {
		Session string       `json:"session"`
		Action  string       `json:"action"`
		Plan    string       `json:"plan"`
		Agent   string       `json:"agent"`
		Node    string       `json:"node"`
		Title   string       `json:"title"`
		Reason  string       `json:"reason"`
		Held    bool         `json:"held"`
		Before  *gitops.Head `json:"before"`
		Card    struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"card"`
	}
	if e := method(r, "POST"); e != nil {
		return nil, e
	}
	if e := decode(r, &p); e != nil {
		return nil, e
	}
	w, e := s.Config.Workspace(p.Session)
	if e != nil {
		return nil, e
	}
	if r.URL.Path == "/v1/git" {
		switch p.Action {
		case "head":
			return gitops.ReadHead(r.Context(), w.Root)
		case "status":
			return gitops.Run(r.Context(), w.Root, "status", "--porcelain")
		case "diff":
			return gitops.Run(r.Context(), w.Root, "diff", "--no-ext-diff", "--")
		case "worktree", "commit":
			ss, _ := s.Config.Session(p.Session)
			if !ss.AllowExec {
				return nil, errors.New("Git mutations require explicit workspace execution authorization")
			}
			if p.Action == "commit" {
				return gitops.CommitNode(r.Context(), w.Root, p.Node, p.Title, p.Before)
			}
			return s.Git.Prepare(r.Context(), w.Root, p.Card.ID, p.Card.Title)
		}
		return nil, errors.New("unsupported Git operation")
	}
	if p.Action == "scan" {
		return atp.Scan(w.Root)
	}
	plan, e := atp.Resolve(w.Root, p.Plan)
	if e != nil {
		return nil, e
	}
	switch p.Action {
	case "read":
		f, e := atp.Read(w.Root, plan)
		if e != nil {
			return nil, e
		}
		if f.Plan == nil {
			return nil, errors.New(f.Error)
		}
		return f.Plan, nil
	case "activate":
		return s.ATP.Librarian.Call(r.Context(), w.Root, plan, "atp-activate-project", []string{"--actor-id", "svolo", "--reason", "The user activated the plan in Svolo."})
	case "claim":
		return s.ATP.Claim(r.Context(), w.Root, plan, p.Agent)
	case "release":
		return s.ATP.Librarian.Call(r.Context(), w.Root, plan, "atp-release-claim", []string{"--node-id", p.Node, "--agent-id", p.Agent, "--reason", p.Reason})
	case "hold":
		if e = s.ATP.SetHeld(plan, p.Held); e != nil {
			return nil, e
		}
		return s.ATP.Held(), nil
	}
	return nil, errors.New("unsupported ATP operation")
}
