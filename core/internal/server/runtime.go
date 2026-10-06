package server

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"svolo.local/core/internal/piruntime"
	"time"
)

func (s *Server) runtimeRequest(r *http.Request) (any, error) {
	if r.URL.Path == "/v1/runtime" {
		if r.Method == "GET" {
			return s.Runtime.List(), nil
		}
		if err := method(r, "POST"); err != nil {
			return nil, err
		}
		var o piruntime.Options
		if err := decode(r, &o); err != nil {
			return nil, err
		}
		ses, ok := s.Config.Session(o.Session)
		if !ok || ses.Workspace == "" {
			return nil, errors.New("runtime requires a registered workspace")
		}
		a, e := filepath.EvalSymlinks(ses.Workspace)
		if e != nil {
			return nil, e
		}
		b, e := filepath.EvalSymlinks(o.CWD)
		if e != nil || a != b {
			return nil, errors.New("runtime cwd is not the registered workspace")
		}
		return s.Runtime.Start(o)
	}
	if r.URL.Path == "/v1/runtime/events" {
		if err := method(r, "GET"); err != nil {
			return nil, err
		}
		after, e := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
		if e != nil && r.URL.Query().Get("after") != "" {
			return nil, e
		}
		return s.Runtime.Events(r.URL.Query().Get("id"), after)
	}
	if err := method(r, "POST"); err != nil {
		return nil, err
	}
	var p struct {
		ID      string         `json:"id"`
		Command map[string]any `json:"command"`
	}
	if e := decode(r, &p); e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	switch r.URL.Path {
	case "/v1/runtime/send":
		return s.Runtime.Send(ctx, p.ID, p.Command)
	case "/v1/runtime/ui":
		return map[string]bool{"sent": true}, s.Runtime.ReplyUI(p.ID, p.Command)
	case "/v1/runtime/stop":
		return map[string]bool{"stopped": true}, s.Runtime.Stop(ctx, p.ID)
	}
	return nil, errors.New("unknown runtime route")
}
