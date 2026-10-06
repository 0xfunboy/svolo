package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPurgeSessionPreservesOthersAndSequenceAcrossRestart(t *testing.T) {
	root := t.TempDir()
	s, err := OpenWithOptions(root, Options{MaxJournalBytes: 250, MaxMemoryEvents: 2})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		sid := "keep"
		if i%2 == 0 {
			sid = "delete"
		}
		if _, err = s.Append(sid, "message", map[string]any{"text": sid + "-private-content"}); err != nil {
			t.Fatal(err)
		}
	}
	before := s.Range().Last
	if err = s.PurgeSession("delete"); err != nil {
		t.Fatal(err)
	}
	if got := s.Events(0, "delete", 100); len(got) != 0 {
		t.Fatal(got)
	}
	if got := s.Events(0, "keep", 100); len(got) != 6 {
		t.Fatal(len(got))
	}
	paths, _ := s.journalFiles()
	paths = append(paths, filepath.Join(root, "events.jsonl"))
	for _, p := range paths {
		data, _ := os.ReadFile(p)
		if strings.Contains(string(data), "delete-private-content") {
			t.Fatal("deleted content retained")
		}
	}
	s.Close()
	s, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Range().Last <= before {
		t.Fatal("cursor regressed")
	}
	ev, err := s.Append("keep", "next", nil)
	if err != nil || ev.Seq <= before {
		t.Fatal(ev, err)
	}
	if len(s.Events(0, "delete", 100)) != 0 {
		t.Fatal("deleted history returned on restart")
	}
}
