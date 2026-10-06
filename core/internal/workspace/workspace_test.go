package workspace

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPathsReadWrite(t *testing.T) {
	w, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = w.Write("nested/a.txt", "hello"); e != nil {
		t.Fatal(e)
	}
	v, e := w.Read("nested/a.txt", 100)
	if e != nil || v != "hello" {
		t.Fatal(v, e)
	}
	for _, p := range []string{"../escape", "/etc/passwd", "a:stream", "a\\..\\b"} {
		if _, e = w.Path(p, true); e == nil {
			t.Fatal("accepted", p)
		}
	}
	if _, e = w.Read("nested/a.txt", 2); e == nil {
		t.Fatal("read limit")
	}
	if entries, e := w.List("nested"); e != nil || len(entries) != 1 {
		t.Fatal(entries, e)
	}
}
func TestRejectSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privilege platform-specific")
	}
	w, _ := Open(t.TempDir())
	os.Symlink(t.TempDir(), filepath.Join(w.Root, "link"))
	if e := w.Write("link/pwn.txt", "x"); e == nil {
		t.Fatal("followed symlink")
	}
}
func TestExplicitProgram(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixture")
	}
	w, _ := Open(t.TempDir())
	r, e := w.Execute(context.Background(), "/bin/sh", []string{"-c", "printf test; exit 7"}, 3)
	if e != nil || r.ExitCode != 7 || r.Output != "test" {
		t.Fatal(r, e)
	}
}
