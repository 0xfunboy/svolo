package store

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExclusiveOwnershipSurvivesStaleLockFile(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := Open(root); err == nil {
		second.Close()
		t.Fatal("second owner admitted")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := Open(root)
	if err != nil {
		t.Fatal("stale lock prevented restart:", err)
	}
	next.Close()
}
func TestCrossProcessLock(t *testing.T) {
	if root := os.Getenv("SVOLO_LOCK_TEST_CHILD"); root != "" {
		s, err := Open(root)
		if err == nil {
			s.Close()
			os.Exit(21)
		}
		os.Exit(0)
	}
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrossProcessLock$")
	cmd.Env = append(os.Environ(), "SVOLO_LOCK_TEST_CHILD="+root)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child lock: %s %v", b, err)
	}
}
func TestBoundedJournalAndDiskReplay(t *testing.T) {
	root := t.TempDir()
	opts := Options{MaxJournalBytes: 500, RetainedSegments: 3, MaxMemoryEvents: 3}
	s, err := OpenWithOptions(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 80; i++ {
		if _, err = s.Append("a", "event", fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	r := s.Range()
	if r.MemoryEvents > 6 || r.First <= 1 || r.Last != 80 {
		t.Fatal(r)
	}
	events := s.Events(r.First-1, "", 1000)
	if len(events) != int(r.Last-r.First+1) {
		t.Fatalf("replay incomplete: %d %+v", len(events), r)
	}
	for i, ev := range events {
		if ev.Seq != r.First+uint64(i) {
			t.Fatal("gap", ev)
		}
	}
	s.Close()
	s, err = OpenWithOptions(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Range().Last != 80 {
		t.Fatal(s.Range())
	}
	ev, err := s.Append("a", "restart", nil)
	if err != nil || ev.Seq != 81 {
		t.Fatal(ev, err)
	}
	files, _ := s.journalFiles()
	if len(files) > 3 {
		t.Fatal("unbounded segments", files)
	}
}
func TestStateSymlinkRefused(t *testing.T) {
	root := t.TempDir()
	other := filepath.Join(t.TempDir(), "other")
	if err := os.WriteFile(other, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(root, "events.jsonl")); err != nil {
		t.Skip("symlink unavailable")
	}
	if s, err := Open(root); err == nil {
		s.Close()
		t.Fatal("followed state symlink")
	}
}
func TestDocumentQuota(t *testing.T) {
	s, err := OpenWithOptions(t.TempDir(), Options{MaxDocumentBytes: 10})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Write("huge", "01234567890123456789"); err == nil {
		t.Fatal("quota ignored")
	}
}
