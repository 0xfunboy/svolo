package store

import (
	"context"
	"errors"
)

// Wait broadcasts a durable journal commit without keeping per-consumer queues.
// Consumers replay by sequence and detect retention gaps via Range().
func (s *Store) Wait(ctx context.Context, after uint64) error {
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return errors.New("store closed")
		}
		if s.seq > after {
			s.mu.Unlock()
			return nil
		}
		ch := s.wake
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
	}
}
