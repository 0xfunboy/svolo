package store

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestPersistenceAndTornTail(t *testing.T) {
	d := t.TempDir()
	s, e := Open(d)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Write("config", map[string]int{"a": 1}); e != nil {
		t.Fatal(e)
	}
	ev, e := s.Append("s", "ok", nil)
	if e != nil || ev.Seq != 1 {
		t.Fatal(ev, e)
	}
	s.Close()
	f, _ := os.OpenFile(filepath.Join(d, "events.jsonl"), os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString(`{"seq":2`)
	f.Close()
	s, e = Open(d)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var v map[string]int
	if e = s.Read("config", &v); e != nil || v["a"] != 1 {
		t.Fatal(v, e)
	}
	ev, e = s.Append("s", "ok", nil)
	if e != nil || ev.Seq != 2 {
		t.Fatal(ev, e)
	}
}
func TestConcurrentJournal(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.Append("a", "x", nil); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	ev := s.Events(0, "a", 100)
	if len(ev) != 30 {
		t.Fatal(len(ev))
	}
	for i, e := range ev {
		if int(e.Seq) != i+1 {
			t.Fatal(e.Seq)
		}
	}
}
func TestTraversalAndCorruption(t *testing.T) {
	s, _ := Open(t.TempDir())
	defer s.Close()
	if e := s.Write("../escape", 1); e == nil {
		t.Fatal("allowed traversal")
	}
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "events.jsonl"), []byte("bad\n"), 0600)
	if s, e := Open(d); e == nil {
		s.Close()
		t.Fatal("ignored corrupt journal")
	}
}
