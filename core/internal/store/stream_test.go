package store

import (
	"context"
	"testing"
	"time"
)

func TestWaitDurableBroadcastCancelAndClose(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	listeners := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() { listeners <- s.Wait(ctx, 0) }()
	}
	event, e := s.Append("one", "test.broadcast", map[string]bool{"ok": true})
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 8; i++ {
		if e = <-listeners; e != nil {
			t.Fatal(e)
		}
	}
	if len(s.Events(0, "one", 8)) != 1 {
		t.Fatal("notification before durable visibility")
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if e = s.Wait(canceled, event.Seq); e != context.Canceled {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- s.Wait(ctx, event.Seq) }()
	s.Close()
	if e = <-done; e == nil {
		t.Fatal("closed journal accepted waiter")
	}
}
