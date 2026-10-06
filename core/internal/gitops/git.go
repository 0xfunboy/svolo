// Package gitops owns Git subprocesses and worktree transactions for a host.
// Commands are fixed argv operations, never shell strings. It never deletes a
// working tree, resets a checkout, pushes, or rewrites the user's git config.
package gitops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"svolo.local/core/internal/proc"
	"svolo.local/core/internal/processenv"
	"sync"
	"time"
)

type limited struct {
	b        bytes.Buffer
	max      int
	overflow bool
}

func (b *limited) Write(p []byte) (int, error) {
	n := len(p)
	if b.b.Len()+n > b.max {
		b.overflow = true
		p = p[:max(0, b.max-b.b.Len())]
	}
	_, _ = b.b.Write(p)
	return n, nil
}
func Run(ctx context.Context, cwd string, args ...string) (string, error) {
	if !filepath.IsAbs(cwd) {
		return "", errors.New("Git workspace must be absolute")
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	argv := append([]string{"-c", "core.quotepath=false", "-C", cwd}, args...)
	cmd := exec.CommandContext(ctx, "git", argv...)
	cmd.Env = append(processenv.SSH(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	proc.Configure(cmd)
	cmd.WaitDelay = 2 * time.Second
	out, errout := &limited{max: 16 << 20}, &limited{max: 32 << 10}
	cmd.Stdout = out
	cmd.Stderr = errout
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git operation failed: %w: %s", err, strings.TrimSpace(errout.b.String()))
	}
	if out.overflow {
		return "", errors.New("Git output budget exceeded")
	}
	return strings.TrimSpace(out.b.String()), nil
}

type Head struct {
	SHA    *string `json:"sha"`
	Branch string  `json:"branch"`
}

func ReadHead(ctx context.Context, cwd string) (*Head, error) {
	branch, err := Run(ctx, cwd, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		inside, e := Run(ctx, cwd, "rev-parse", "--is-inside-work-tree")
		if e != nil || inside != "true" {
			return nil, nil
		}
		return &Head{}, nil
	}
	sha, e := Run(ctx, cwd, "rev-parse", "--verify", "-q", "HEAD")
	if e != nil {
		return nil, e
	}
	return &Head{SHA: &sha, Branch: branch}, nil
}

type Commit struct {
	Kind string `json:"kind"`
	SHA  string `json:"sha,omitempty"`
}

func CommitNode(ctx context.Context, cwd, node, title string, before *Head) (Commit, error) {
	now, e := ReadHead(ctx, cwd)
	if e != nil {
		return Commit{}, e
	}
	if now == nil || before == nil {
		return Commit{Kind: "no-repo"}, nil
	}
	equal := now.SHA == nil && before.SHA == nil || now.SHA != nil && before.SHA != nil && *now.SHA == *before.SHA
	if !equal {
		return Commit{Kind: "worker"}, nil
	}
	status, e := Run(ctx, cwd, "status", "--porcelain", "--", ".")
	if e != nil {
		return Commit{}, e
	}
	if status == "" {
		return Commit{Kind: "clean"}, nil
	}
	if !idRE.MatchString(node) {
		return Commit{}, errors.New("invalid node id")
	}
	if _, e = Run(ctx, cwd, "add", "-A", "--", ".", ":(exclude,glob)**/*.atp.json.lock"); e != nil {
		return Commit{}, e
	}
	title = strings.Join(strings.Fields(title), " ")
	r := []rune(title)
	if len(r) > 72 {
		title = string(r[:72])
	}
	if _, e = Run(ctx, cwd, "commit", "-q", "-m", "node("+node+"): "+title); e != nil {
		return Commit{}, e
	}
	sha, e := Run(ctx, cwd, "rev-parse", "HEAD")
	return Commit{Kind: "committed", SHA: sha}, e
}

var idRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,100}$`)
var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// BranchName uses a conservative ASCII slug. Identity, not spelling, determines reuse.
func BranchName(id, title string) (string, error) {
	if !idRE.MatchString(id) {
		return "", errors.New("invalid worktree identity")
	}
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(slug) > 40 {
		cut := strings.LastIndex(slug[:41], "-")
		if cut <= 0 {
			cut = 40
		}
		slug = slug[:cut]
	}
	if slug != "" {
		return "svolo/" + id + "-" + slug, nil
	}
	return "svolo/" + id, nil
}

type Worktree struct {
	CWD     string `json:"cwd"`
	Branch  string `json:"branch"`
	Created bool   `json:"created"`
	Dirty   bool   `json:"dirty"`
}
type Manager struct {
	mu   sync.Mutex
	Home string
}

func (m *Manager) Prepare(ctx context.Context, project, id, title string) (*Worktree, error) {
	if !filepath.IsAbs(project) || !filepath.IsAbs(m.Home) || !idRE.MatchString(id) {
		return nil, errors.New("absolute paths and valid worktree identity required")
	}
	project = filepath.Clean(project)
	real, e := filepath.EvalSymlinks(project)
	if e != nil {
		return nil, e
	}
	if real != project {
		return nil, errors.New("open the real repository path, not a symbolic alias")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	top, e := Run(ctx, project, "rev-parse", "--show-toplevel")
	if e != nil {
		if strings.Contains(e.Error(), "not a git repository") {
			return nil, nil
		}
		return nil, e
	}
	sub, e := filepath.Rel(top, project)
	if e != nil || sub == ".." || strings.HasPrefix(sub, ".."+string(filepath.Separator)) {
		return nil, errors.New("workspace outside repository")
	}
	// Namespace each worktree by project path; include the drive name on Windows.
	safeTop := strings.TrimLeft(strings.ReplaceAll(filepath.ToSlash(top), ":", ""), "/")
	dir := filepath.Join(m.Home, ".svolo", "worktrees", id, filepath.FromSlash(safeTop))
	cwd := filepath.Join(dir, sub)
	dirty, e := Run(ctx, project, "status", "--porcelain")
	if e != nil {
		return nil, e
	}
	if isWorktree(ctx, dir) {
		branch, e := Run(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
		return &Worktree{CWD: cwd, Branch: branch, Dirty: dirty != ""}, e
	}
	if _, e = os.Lstat(dir); e == nil {
		return nil, errors.New("worktree destination already exists and is not a worktree; refusing to overwrite")
	}
	if _, e = Run(ctx, project, "worktree", "prune"); e != nil {
		return nil, e
	}
	branches, e := Run(ctx, project, "for-each-ref", "--format=%(refname)", "refs/heads/svolo/"+id, "refs/heads/svolo/"+id+"-*")
	if e != nil {
		return nil, e
	}
	existing := ""
	if branches != "" {
		existing = strings.TrimPrefix(strings.Split(branches, "\n")[0], "refs/heads/")
	}
	branch, e := BranchName(id, title)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(filepath.Dir(dir), 0700); e != nil {
		return nil, e
	}
	if existing != "" {
		branch = existing
		_, e = Run(ctx, project, "worktree", "add", dir, branch)
	} else {
		_, e = Run(ctx, project, "worktree", "add", "-b", branch, dir, "HEAD")
	}
	if e != nil {
		return nil, e
	}
	return &Worktree{CWD: cwd, Branch: branch, Created: true, Dirty: dirty != ""}, nil
}
func isWorktree(ctx context.Context, path string) bool {
	st, e := os.Stat(path)
	if e != nil || !st.IsDir() {
		return false
	}
	top, e := Run(ctx, path, "rev-parse", "--show-toplevel")
	if e != nil {
		return false
	}
	real, e := filepath.EvalSymlinks(path)
	return e == nil && filepath.Clean(top) == real
}
