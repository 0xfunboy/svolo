package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	after, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		after, _ = strconv.ParseUint(v, 10, 64)
	}
	session := r.URL.Query().Get("session")
	if session != "" {
		if _, ok := s.Config.Session(session); !ok {
			w.WriteHeader(400)
			return
		}
	}
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Time{})
	defer controller.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	send := func(kind string, id uint64, v any) error {
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintf(w, "event: %s\nid: %d\ndata: %s\n\n", kind, id, b); err != nil {
			return err
		}
		return controller.Flush()
	}
	for {
		rg := s.Store.Range()
		if rg.First > 0 && after < rg.First-1 {
			if send("gap", rg.First-1, rg) != nil {
				return
			}
			after = rg.First - 1
		}
		events := s.Store.Events(after, "", 250)
		for _, event := range events {
			if session == "" || session == event.Session {
				if send("journal", event.Seq, event) != nil {
					return
				}
			}
			after = event.Seq
		}
		if len(events) > 0 {
			if send("cursor", after, map[string]uint64{"after": after}) != nil {
				return
			}
			continue
		}
		if send("cursor", after, map[string]uint64{"after": after}) != nil {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		err := s.Store.Wait(ctx, after)
		cancel()
		if r.Context().Err() != nil || s.ctx.Err() != nil {
			return
		}
		if err != nil && err != context.DeadlineExceeded {
			return
		}
	}
}
