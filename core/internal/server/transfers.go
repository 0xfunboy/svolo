package server

import (
	"errors"
	"net/http"
	"strconv"
	"svolo.local/core/internal/transfer"
)

// The source/destination workspace is taken from the registered session, never
// from a browser page or a client-supplied absolute root. Chunks survive reconnects.
func (s *Server) transferRequest(r *http.Request) (any, error) {
	sid, id := r.URL.Query().Get("session"), r.URL.Query().Get("id")
	switch r.URL.Path {
	case "/v1/transfers":
		if r.Method == "GET" {
			if _, ok := s.Config.Session(sid); !ok {
				return nil, errors.New("unknown session")
			}
			return s.Transfers.List(sid), nil
		}
		if err := method(r, "POST"); err != nil {
			return nil, err
		}
		var p transfer.Spec
		if err := decode(r, &p); err != nil {
			return nil, err
		}
		w, err := s.Config.Workspace(p.Session)
		if err != nil {
			return nil, err
		}
		path, err := w.Path(p.Path, true)
		if err != nil {
			return nil, err
		}
		if s.privatePath(path) {
			return nil, errors.New("private application state cannot be transferred")
		}
		return s.Transfers.Start(w.Root, p)
	case "/v1/transfers/status":
		if err := method(r, "GET"); err != nil {
			return nil, err
		}
		return s.Transfers.Get(sid, id)
	case "/v1/transfers/chunk":
		if r.Method == "GET" {
			offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
			if err != nil {
				return nil, err
			}
			b, err := s.Transfers.Read(sid, id, offset)
			return map[string]any{"data": b, "offset": offset, "nextOffset": offset + int64(len(b)), "eof": len(b) == 0}, err
		}
		if err := method(r, "POST"); err != nil {
			return nil, err
		}
		var p struct {
			Session string `json:"session"`
			ID      string `json:"id"`
			Offset  int64  `json:"offset"`
			Data    []byte `json:"data"`
		}
		if err := decode(r, &p); err != nil {
			return nil, err
		}
		return s.Transfers.Put(p.Session, p.ID, p.Offset, p.Data)
	case "/v1/transfers/commit", "/v1/transfers/abort":
		if err := method(r, "POST"); err != nil {
			return nil, err
		}
		var p struct {
			Session string `json:"session"`
			ID      string `json:"id"`
		}
		if err := decode(r, &p); err != nil {
			return nil, err
		}
		if r.URL.Path == "/v1/transfers/abort" {
			err := s.Transfers.Abort(p.Session, p.ID)
			return map[string]bool{"aborted": err == nil}, err
		}
		w, err := s.Config.Workspace(p.Session)
		if err != nil {
			return nil, err
		}
		state, err := s.Transfers.Get(p.Session, p.ID)
		if err != nil {
			return nil, err
		}
		path, err := w.Path(state.Path, true)
		if err != nil {
			return nil, err
		}
		if s.privatePath(path) {
			return nil, errors.New("private destination refused")
		}
		return s.Transfers.Commit(p.Session, p.ID, w.Root)
	case "/v1/transfers/download":
		if err := method(r, "POST"); err != nil {
			return nil, err
		}
		var p struct {
			Session string `json:"session"`
			Path    string `json:"path"`
		}
		if err := decode(r, &p); err != nil {
			return nil, err
		}
		w, err := s.Config.Workspace(p.Session)
		if err != nil {
			return nil, err
		}
		path, err := w.Path(p.Path, false)
		if err != nil {
			return nil, err
		}
		if s.privatePath(path) {
			return nil, errors.New("private source refused")
		}
		return s.Transfers.Download(w.Root, p.Session, p.Path)
	}
	return nil, errors.New("unknown transfer operation")
}
