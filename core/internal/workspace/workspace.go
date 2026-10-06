// Package workspace provides conservative path validation, not an OS security sandbox.
// Shell processes need an explicit trust grant; they are disabled by default in the API.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"svolo.local/core/internal/processenv"
	"svolo.local/core/internal/store"
	"sync"
	"time"
)

type Workspace struct{ Root string }

func Open(root string) (Workspace, error) {
	abs, e := filepath.Abs(root)
	if e != nil {
		return Workspace{}, e
	}
	abs, e = filepath.EvalSymlinks(abs)
	if e != nil {
		return Workspace{}, e
	}
	st, e := os.Stat(abs)
	if e != nil || !st.IsDir() {
		return Workspace{}, errors.New("workspace must be an existing directory")
	}
	return Workspace{Root: abs}, nil
}
func (w Workspace) Path(rel string, allowMissing bool) (string, error) {
	if rel == "" {
		rel = "."
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") || strings.ContainsAny(rel, "\x00:") || strings.Contains(rel, "\\") {
		return "", errors.New("use a relative workspace path without backslash, NUL or colon")
	}
	path := filepath.Join(w.Root, filepath.FromSlash(rel))
	r, e := filepath.Rel(w.Root, path)
	if e != nil || r == ".." || strings.HasPrefix(r, ".."+string(os.PathSeparator)) {
		return "", errors.New("path escapes workspace")
	}
	current := w.Root
	for _, part := range strings.Split(r, string(os.PathSeparator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		st, e := os.Lstat(current)
		if os.IsNotExist(e) && allowMissing {
			continue
		}
		if e != nil {
			return "", e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("symlinks inside workspace are not followed")
		}
	}
	return path, nil
}
func (w Workspace) Read(rel string, limit int64) (string, error) {
	if limit < 1 || limit > 2<<20 {
		limit = 2 << 20
	}
	p, e := w.Path(rel, false)
	if e != nil {
		return "", e
	}
	f, e := os.Open(p)
	if e != nil {
		return "", e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	if st.Size() > limit {
		return "", errors.New("file exceeds read budget")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return "", errors.New("file grew beyond read budget")
	}
	return string(b), e
}
func (w Workspace) Write(rel, content string) error {
	if len(content) > 2<<20 {
		return errors.New("write exceeds 2 MiB budget")
	}
	p, e := w.Path(rel, true)
	if e != nil {
		return e
	}
	if p == w.Root {
		return errors.New("cannot replace workspace root")
	}
	return store.Atomic(p, []byte(content), 0600)
}

type Entry struct {
	Name     string    `json:"name"`
	Dir      bool      `json:"directory"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

func (w Workspace) List(rel string) ([]Entry, error) {
	p, e := w.Path(rel, false)
	if e != nil {
		return nil, e
	}
	list, e := os.ReadDir(p)
	if e != nil {
		return nil, e
	}
	out := []Entry{}
	for _, x := range list {
		if len(out) >= 2000 {
			break
		}
		st, e := x.Info()
		if e != nil {
			continue
		}
		out = append(out, Entry{Name: x.Name(), Dir: x.IsDir(), Size: st.Size(), Modified: st.ModTime()})
	}
	return out, nil
}

type Result struct {
	Output     string `json:"output"`
	ExitCode   int    `json:"exitCode"`
	Truncated  bool   `json:"truncated"`
	DurationMS int64  `json:"durationMs"`
}
type boundedWriter struct {
	mu        sync.Mutex
	b         []byte
	truncated bool
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	remaining := (1 << 20) - len(w.b)
	if remaining < len(p) {
		p = p[:remaining]
		w.truncated = true
	}
	w.b = append(w.b, p...)
	return n, nil
}
func (w Workspace) Execute(ctx context.Context, program string, args []string, seconds int) (Result, error) {
	if program == "" || strings.ContainsRune(program, 0) {
		return Result{}, errors.New("program required")
	}
	if len(args) > 256 {
		return Result{}, errors.New("too many process arguments")
	}
	if seconds == 0 {
		seconds = 60
	}
	if seconds < 1 || seconds > 600 {
		return Result{}, errors.New("timeout must be 1..600 seconds")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = w.Root
	cmd.Env = processenv.Safe()
	out := &boundedWriter{}
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Stdin = nil
	configureProcess(cmd)
	cmd.WaitDelay = 2 * time.Second
	started := time.Now()
	err := cmd.Run()
	r := Result{Output: string(out.b), Truncated: out.truncated, DurationMS: time.Since(started).Milliseconds()}
	if cmd.ProcessState != nil {
		r.ExitCode = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		return r, ctx.Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return r, nil
	}
	if err != nil {
		return r, fmt.Errorf("process start: %w", err)
	}
	return r, nil
}
