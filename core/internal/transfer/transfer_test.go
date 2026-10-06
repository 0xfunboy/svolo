package transfer

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"svolo.local/core/internal/store"
	"testing"
)

func digest(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }
func setup(t *testing.T) (*store.Store, *Manager, string) {
	t.Helper()
	s, e := store.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	m, e := New(s)
	if e != nil {
		t.Fatal(e)
	}
	return s, m, t.TempDir()
}
func TestResumeIdempotencyAndCommit(t *testing.T) {
	st, m, root := setup(t)
	data := []byte("binary\x00payload\xff")
	s, e := m.Start(root, Spec{Session: "s", Path: "nested/file.bin", Size: int64(len(data)), SHA256: digest(data)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Put("s", s.ID, 0, data[:4]); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Put("s", s.ID, 0, data[:4]); e != nil {
		t.Fatal("idempotent retry", e)
	}
	if _, e = m.Put("s", s.ID, 0, []byte("BAD!")); e == nil {
		t.Fatal("conflicting retry accepted")
	}
	// Emulate a crash after a data write but before the durable offset update.
	f, _ := os.OpenFile(m.part(s.ID), os.O_APPEND|os.O_WRONLY, 0600)
	f.Write([]byte("uncommitted"))
	f.Close()
	m, e = New(st)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Put("s", s.ID, 4, data[4:]); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Commit("s", s.ID, root); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(root, "nested/file.bin"))
	if string(b) != string(data) {
		t.Fatal("corruption")
	}
	if _, e = m.Commit("s", s.ID, root); e != nil {
		t.Fatal("duplicate commit", e)
	}
}
func TestChecksumConflictTraversalAndSession(t *testing.T) {
	_, m, root := setup(t)
	data := []byte("hello")
	if _, e := m.Start(root, Spec{Session: "s", Path: "../escape", Size: 5, SHA256: digest(data)}); e == nil {
		t.Fatal("traversal")
	}
	s, e := m.Start(root, Spec{Session: "s", Path: "file", Size: 5, SHA256: digest(data)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Put("other", s.ID, 0, data); e == nil {
		t.Fatal("cross session")
	}
	m.Put("s", s.ID, 0, []byte("wrong"))
	if _, e = m.Commit("s", s.ID, root); e == nil {
		t.Fatal("bad hash accepted")
	}
	if _, e = os.Stat(filepath.Join(root, "file")); !os.IsNotExist(e) {
		t.Fatal("wrote corrupt destination")
	}
	m.Abort("s", s.ID)
	os.WriteFile(filepath.Join(root, "existing"), data, 0600)
	s, e = m.Start(root, Spec{Session: "s", Path: "existing", Size: 5, SHA256: digest(data), ExpectedOldSHA256: digest(data)})
	if e != nil {
		t.Fatal(e)
	}
	m.Put("s", s.ID, 0, data)
	os.WriteFile(filepath.Join(root, "existing"), []byte("changed"), 0600)
	if _, e = m.Commit("s", s.ID, root); e == nil {
		t.Fatal("overwrote concurrently modified file")
	}
}
func TestDownloadSnapshotAndAbort(t *testing.T) {
	_, m, root := setup(t)
	data := []byte("immutable")
	os.WriteFile(filepath.Join(root, "f"), data, 0600)
	s, e := m.Download(root, "s", "f")
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(root, "f"), []byte("changed"), 0600)
	b, e := m.Read("s", s.ID, 0)
	if e != nil || string(b) != string(data) {
		t.Fatal(b, e)
	}
	if e = m.Abort("s", s.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Read("s", s.ID, 0); e == nil {
		t.Fatal("aborted download still served")
	}
}
func TestNoReplaceRace(t *testing.T) {
	_, m, root := setup(t)
	data := []byte("new")
	s, e := m.Start(root, Spec{Session: "s", Path: "f", Size: 3, SHA256: digest(data)})
	if e != nil {
		t.Fatal(e)
	}
	m.Put("s", s.ID, 0, data)
	os.WriteFile(filepath.Join(root, "f"), []byte("human edit"), 0600)
	if _, e = m.Commit("s", s.ID, root); e == nil {
		t.Fatal("replaced a file created after start")
	}
	b, _ := os.ReadFile(filepath.Join(root, "f"))
	if string(b) != "human edit" {
		t.Fatal("human data lost")
	}
}
