package gitops

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func repo(t *testing.T) string {
	t.Helper()
	p := t.TempDir()
	ctx := context.Background()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "test@example.invalid"}, {"config", "user.name", "Svolo Test"}} {
		if _, e := Run(ctx, p, a...); e != nil {
			t.Fatal(e)
		}
	}
	os.WriteFile(filepath.Join(p, "start.txt"), []byte("start"), 0600)
	Run(ctx, p, "add", ".")
	if _, e := Run(ctx, p, "commit", "-qm", "initial"); e != nil {
		t.Fatal(e)
	}
	return p
}
func TestWorktreeRealGit(t *testing.T) {
	ctx := context.Background()
	p := repo(t)
	os.Mkdir(filepath.Join(p, "sub"), 0700)
	os.WriteFile(filepath.Join(p, "sub", "nested"), []byte("nested"), 0600)
	Run(ctx, p, "add", ".")
	Run(ctx, p, "commit", "-qm", "sub")
	m := &Manager{Home: t.TempDir()}
	var wg sync.WaitGroup
	results := make(chan *Worktree, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w, e := m.Prepare(ctx, filepath.Join(p, "sub"), "abc123", "Fix integration")
			if e != nil {
				t.Error(e)
			} else {
				results <- w
			}
		}()
	}
	wg.Wait()
	close(results)
	created := 0
	for w := range results {
		if w.Created {
			created++
		}
		if w.Branch != "svolo/abc123-fix-integration" {
			t.Fatal(w)
		}
		if _, e := os.Stat(filepath.Join(w.CWD, "nested")); e != nil {
			t.Fatal(e)
		}
	}
	if created != 1 {
		t.Fatal("created", created)
	}
}
func TestCommitGuards(t *testing.T) {
	ctx := context.Background()
	p := repo(t)
	h, e := ReadHead(ctx, p)
	if e != nil || h.SHA == nil {
		t.Fatal(e)
	}
	c, e := CommitNode(ctx, p, "n1", "No changes", h)
	if e != nil || c.Kind != "clean" {
		t.Fatal(c, e)
	}
	os.WriteFile(filepath.Join(p, "new"), []byte("data"), 0600)
	c, e = CommitNode(ctx, p, "n1", "Changes\nnormalized", h)
	if e != nil || c.Kind != "committed" {
		t.Fatal(c, e)
	}
	c, e = CommitNode(ctx, p, "n1", "Again", h)
	if e != nil || c.Kind != "worker" {
		t.Fatal(c, e)
	}
}
func TestInvalidWorktree(t *testing.T) {
	m := Manager{Home: t.TempDir()}
	for _, id := range []string{"../escape", "-option", "", "a/b"} {
		if _, e := m.Prepare(context.Background(), t.TempDir(), id, "title"); e == nil {
			t.Fatal(id)
		}
	}
	if _, e := BranchName("safe", "'$(rm -rf x)"); e != nil {
		t.Fatal(e)
	}
}
func TestNonRepo(t *testing.T) {
	m := Manager{Home: t.TempDir()}
	w, e := m.Prepare(context.Background(), t.TempDir(), "abc123", "title")
	if e != nil || w != nil {
		t.Fatal(w, e)
	}
}
