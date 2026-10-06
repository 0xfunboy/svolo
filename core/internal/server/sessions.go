package server

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// deleteSession runs under sessionLifecycle's exclusive lock. Cancellation and
// durable cleanup finish before removing the configuration, allowing retry on
// an I/O failure without declaring a partly removed chat successfully deleted.
func (s *Server) deleteSession(sid string) error {
	if _, exists := s.Config.Session(sid); !exists {
		return errors.New("session not found")
	}
	if s.Agents.Busy(sid) {
		return errors.New("stop the agent before deleting this session")
	}
	s.mu.Lock()
	recording := s.recordings[sid]
	delete(s.recordings, sid)
	s.mu.Unlock()
	if recording != nil {
		recording.cancel()
		<-recording.done
	}
	if err := s.Engine.ForgetSession(sid); err != nil {
		return err
	}
	if err := s.Agents.DeleteSession(sid); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(s.Store.Root, "recordings", sid)); err != nil {
		return err
	}
	entries, err := os.ReadDir(s.Store.Root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "recording-") && strings.HasSuffix(entry.Name(), ".json") {
			var metadata struct {
				Session string `json:"session"`
			}
			name := strings.TrimSuffix(entry.Name(), ".json")
			if err := s.Store.Read(name, &metadata); err != nil {
				return err
			}
			if metadata.Session == sid {
				if err := s.Store.Remove(name); err != nil {
					return err
				}
			}
		}
	}
	s.mu.Lock()
	saved := []scope{}
	for id, credential := range s.scopes {
		if credential.Session == sid {
			delete(s.scopes, id)
		} else {
			saved = append(saved, credential)
		}
	}
	for id, connection := range s.mcpSessions {
		if connection.Session == sid {
			delete(s.mcpSessions, id)
		}
	}
	err = s.Store.Write("token-scopes", saved)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.Config.Remove(sid)
}
