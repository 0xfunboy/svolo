// Package github implements the multi-account GitHub workflow on the
// host. Tokens are read through gh, scoped to its matching host, held only in
// memory, and scrubbed from all returned errors. No auth switch is performed.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"svolo.local/core/internal/proc"
	"svolo.local/core/internal/processenv"
	"svolo.local/core/internal/store"
	"sync"
	"time"
)

type Repo struct {
	Host string `json:"host"`
	Repo string `json:"repo"`
}
type Remote struct {
	Name     string
	URL      string
	Resolved string
}

var remoteURL = regexp.MustCompile(`(?i)^(?:https?|git|ssh)://(?:[^@/]+@)?([A-Za-z0-9][A-Za-z0-9.-]*)(?::[0-9]+)?/([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+?)(?:\.git)?/?$`)
var remoteSCP = regexp.MustCompile(`^(?:[^@/]+@)?([A-Za-z0-9][A-Za-z0-9.-]*):([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+?)(?:\.git)?/?$`)
var repoRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var loginRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
var tokenRE = regexp.MustCompile(`\b(?:gh[opsur]_[A-Za-z0-9]{16,}|github_pat_[A-Za-z0-9_]{16,})\b`)
var remoteLine = regexp.MustCompile(`^remote\.(.+)\.(url|gh-resolved) (.*)$`)

func ParseRemote(raw string) *Repo {
	a := remoteURL.FindStringSubmatch(raw)
	if a == nil {
		a = remoteSCP.FindStringSubmatch(raw)
	}
	if len(a) != 3 {
		return nil
	}
	for _, part := range strings.Split(a[2], "/") {
		if part == "." || part == ".." || part == "" {
			return nil
		}
	}
	return &Repo{Host: strings.ToLower(a[1]), Repo: a[2]}
}
func PickRemote(remotes []Remote) *Repo {
	type pair struct {
		r Remote
		p *Repo
	}
	parsed := []pair{}
	for _, r := range remotes {
		if p := ParseRemote(r.URL); p != nil {
			parsed = append(parsed, pair{r, p})
		}
	}
	for _, p := range parsed {
		if p.r.Resolved == "base" {
			return p.p
		}
	}
	for _, p := range parsed {
		if repoRE.MatchString(p.r.Resolved) {
			return &Repo{p.p.Host, p.r.Resolved}
		}
	}
	rank := func(n string) int {
		switch n {
		case "upstream":
			return 0
		case "github":
			return 1
		case "origin":
			return 2
		}
		return 3
	}
	sort.SliceStable(parsed, func(i, j int) bool { return rank(parsed[i].r.Name) < rank(parsed[j].r.Name) })
	if len(parsed) == 0 {
		return nil
	}
	return parsed[0].p
}
func TokenVariable(host string) string {
	if host == "github.com" || strings.HasSuffix(host, ".ghe.com") {
		return "GH_TOKEN"
	}
	return "GH_ENTERPRISE_TOKEN"
}

type ExecError struct {
	Err     error
	File    string
	Stdout  string
	Missing bool
}

func (e *ExecError) Error() string { return e.Err.Error() }

type Exec func(context.Context, string, []string, map[string]string) (string, error)
type bounded struct {
	b        bytes.Buffer
	overflow bool
	limit    int
}

func (b *bounded) Write(p []byte) (int, error) {
	n := len(p)
	if b.b.Len()+n > b.limit {
		b.overflow = true
		p = p[:max(0, b.limit-b.b.Len())]
	}
	b.b.Write(p)
	return n, nil
}
func run(ctx context.Context, file string, args []string, env map[string]string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, file, args...)
	cmd.Env = append(processenv.Safe(), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "NO_COLOR=1", "LC_ALL=C")
	if dir := os.Getenv("GH_CONFIG_DIR"); dir != "" {
		cmd.Env = append(cmd.Env, "GH_CONFIG_DIR="+dir)
	}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	proc.Configure(cmd)
	cmd.WaitDelay = 2 * time.Second
	out, stderr := &bounded{limit: 32 << 20}, &bounded{limit: 32 << 10}
	cmd.Stdout = out
	cmd.Stderr = stderr
	e := cmd.Run()
	if e != nil {
		msg := strings.TrimSpace(stderr.b.String())
		if msg == "" {
			msg = e.Error()
		}
		return "", &ExecError{errors.New(msg), file, out.b.String(), errors.Is(e, exec.ErrNotFound) || os.IsNotExist(e)}
	}
	if out.overflow {
		return "", errors.New("GitHub output budget exceeded")
	}
	return out.b.String(), nil
}

type Problem struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func (p *Problem) Error() string { return p.Message }

type Settings struct {
	Version int               `json:"version"`
	Chosen  map[string]string `json:"chosen"`
	Saved   map[string]string `json:"saved"`
}
type Account struct {
	Login  string `json:"login"`
	Reason string `json:"reason"`
}
type Project struct {
	Repo     *Repo    `json:"repo,omitempty"`
	Account  *Account `json:"account,omitempty"`
	Accounts []string `json:"accounts"`
	Chosen   string   `json:"chosen,omitempty"`
	Problem  *Problem `json:"problem,omitempty"`
}
type Manager struct {
	mu         sync.Mutex
	st         *store.Store
	Exec       Exec
	settings   Settings
	accounts   map[string][]string
	accountsAt time.Time
	tokens     map[string]string
	secrets    map[string]bool
	orgs       map[string]map[string]bool
	loaded     bool
}

func New(st *store.Store) *Manager {
	m := &Manager{st: st, Exec: run, settings: Settings{Version: 1, Chosen: map[string]string{}, Saved: map[string]string{}}, tokens: map[string]string{}, secrets: map[string]bool{}, orgs: map[string]map[string]bool{}}
	if e := st.Read("github-settings", &m.settings); e == nil {
		m.loaded = true
	}
	if m.settings.Chosen == nil {
		m.settings.Chosen = map[string]string{}
	}
	if m.settings.Saved == nil {
		m.settings.Saved = map[string]string{}
	}
	return m
}
func (m *Manager) persist() error { return m.st.Write("github-settings", m.settings) }
func (m *Manager) Import(s Settings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loaded {
		return nil
	}
	if s.Version != 1 || len(s.Chosen) > 1000 || len(s.Saved) > 1000 {
		return errors.New("invalid GitHub settings import")
	}
	for p, l := range s.Chosen {
		if !filepath.IsAbs(p) || !loginRE.MatchString(l) {
			return errors.New("invalid chosen account")
		}
	}
	for _, l := range s.Saved {
		if !loginRE.MatchString(l) {
			return errors.New("invalid saved account")
		}
	}
	m.settings = s
	if m.settings.Chosen == nil {
		m.settings.Chosen = map[string]string{}
	}
	if m.settings.Saved == nil {
		m.settings.Saved = map[string]string{}
	}
	if e := m.persist(); e != nil {
		return e
	}
	m.loaded = true
	return nil
}
func (m *Manager) problem(e error) *Problem {
	if e == nil {
		return nil
	}
	var p *Problem
	if errors.As(e, &p) {
		return p
	}
	kind := "failed"
	var ex *ExecError
	if errors.As(e, &ex) && ex.Missing && ex.File == "gh" {
		kind = "no-gh"
	}
	text := e.Error()
	for s := range m.secrets {
		text = strings.ReplaceAll(text, s, "***")
	}
	text = tokenRE.ReplaceAllString(text, "***")
	return &Problem{kind, text}
}
func (m *Manager) forget() {
	m.accounts = nil
	m.tokens = map[string]string{}
	m.orgs = map[string]map[string]bool{}
}
func (m *Manager) repo(ctx context.Context, cwd string) (*Repo, error) {
	text, e := m.Exec(ctx, "git", []string{"-C", cwd, "config", "--get-regexp", "^remote\\..*\\.(url|gh-resolved)$"}, nil)
	if e != nil {
		var ex *ExecError
		if errors.As(e, &ex) && ex.Missing {
			return nil, e
		}
		return nil, &Problem{"no-repo", "No GitHub remote is configured for this workspace."}
	}
	rs := []Remote{}
	indices := map[string]int{}
	for _, line := range strings.Split(text, "\n") {
		a := remoteLine.FindStringSubmatch(line)
		if a == nil {
			continue
		}
		i, ok := indices[a[1]]
		if !ok {
			i = len(rs)
			indices[a[1]] = i
			rs = append(rs, Remote{Name: a[1]})
		}
		if a[2] == "url" && rs[i].URL == "" {
			rs[i].URL = a[3]
		}
		if a[2] == "gh-resolved" {
			rs[i].Resolved = a[3]
		}
	}
	repo := PickRemote(rs)
	if repo == nil {
		return nil, &Problem{"no-repo", "No supported GitHub remote is configured."}
	}
	return repo, nil
}
func (m *Manager) logins(ctx context.Context, host string) ([]string, error) {
	if m.accounts == nil || time.Since(m.accountsAt) > 10*time.Minute {
		text, e := m.Exec(ctx, "gh", []string{"auth", "status", "--json", "hosts"}, nil)
		if e != nil {
			var ex *ExecError
			if errors.As(e, &ex) && strings.HasPrefix(strings.TrimSpace(ex.Stdout), "{") {
				text = ex.Stdout
			} else {
				return nil, e
			}
		}
		var parsed struct {
			Hosts map[string][]struct {
				Login  string `json:"login"`
				Active bool   `json:"active"`
			} `json:"hosts"`
		}
		if e = json.Unmarshal([]byte(text), &parsed); e != nil {
			return nil, e
		}
		m.accounts = map[string][]string{}
		for h, as := range parsed.Hosts {
			sort.SliceStable(as, func(i, j int) bool { return as[i].Active && !as[j].Active })
			seen := map[string]bool{}
			for _, a := range as {
				if loginRE.MatchString(a.Login) && !seen[a.Login] {
					m.accounts[strings.ToLower(h)] = append(m.accounts[strings.ToLower(h)], a.Login)
					seen[a.Login] = true
				}
			}
		}
		m.accountsAt = time.Now()
	}
	logins := m.accounts[host]
	if len(logins) == 0 {
		return nil, &Problem{"no-login", "No gh account for " + host + ". Authenticate with gh auth login on this host."}
	}
	return logins, nil
}
func (m *Manager) token(ctx context.Context, host, login string) (string, error) {
	key := host + " " + login
	if value := m.tokens[key]; value != "" {
		return value, nil
	}
	text, e := m.Exec(ctx, "gh", []string{"auth", "token", "--hostname", host, "--user", login}, nil)
	if e != nil {
		return "", e
	}
	token := strings.TrimSpace(text)
	if token == "" || strings.ContainsAny(token, "\r\n\x00") || len(token) > 8192 {
		return "", &Problem{"no-login", "gh returned no usable token for the selected account"}
	}
	m.tokens[key] = token
	m.secrets[token] = true
	return token, nil
}
func status(e error, n string) bool { return e != nil && strings.Contains(e.Error(), "(HTTP "+n+")") }
func (m *Manager) as(ctx context.Context, host, login string, args []string) (string, error) {
	for i := 0; i < 2; i++ {
		token, e := m.token(ctx, host, login)
		if e != nil {
			return "", e
		}
		text, e := m.Exec(ctx, "gh", args, map[string]string{TokenVariable(host): token})
		if e == nil {
			return text, nil
		}
		if i == 0 && (status(e, "401") || strings.Contains(e.Error(), "Bad credentials")) {
			delete(m.tokens, host+" "+login)
			continue
		}
		return "", e
	}
	return "", errors.New("token refresh exhausted")
}
func has(a []string, x string) bool {
	for _, v := range a {
		if v == x {
			return true
		}
	}
	return false
}
func (m *Manager) account(ctx context.Context, cwd string, repo *Repo) (*Account, error) {
	logins, e := m.logins(ctx, repo.Host)
	if e != nil {
		return nil, e
	}
	key := strings.ToLower(repo.Host + "/" + repo.Repo)
	if chosen := m.settings.Chosen[cwd]; chosen != "" {
		if has(logins, chosen) {
			return &Account{chosen, "chosen"}, nil
		}
		delete(m.settings.Chosen, cwd)
		if e = m.persist(); e != nil {
			return nil, e
		}
	}
	if saved := m.settings.Saved[key]; saved != "" {
		if has(logins, saved) {
			return &Account{saved, "saved"}, nil
		}
		delete(m.settings.Saved, key)
		if e = m.persist(); e != nil {
			return nil, e
		}
	}
	owner := strings.ToLower(strings.Split(repo.Repo, "/")[0])
	tried := map[string]bool{}
	attempt := func(candidates []string, reason string) (*Account, error) {
		for _, login := range candidates {
			if tried[login] {
				continue
			}
			tried[login] = true
			_, e := m.as(ctx, repo.Host, login, []string{"api", "--hostname", repo.Host, "repos/" + repo.Repo, "--jq", ".full_name"})
			if e != nil {
				if status(e, "401") || status(e, "403") || status(e, "404") {
					continue
				}
				return nil, e
			}
			m.settings.Saved[key] = login
			if e = m.persist(); e != nil {
				return nil, e
			}
			return &Account{login, reason}, nil
		}
		return nil, nil
	}
	owners := []string{}
	others := []string{}
	for _, l := range logins {
		if strings.ToLower(l) == owner {
			owners = append(owners, l)
		} else {
			others = append(others, l)
		}
	}
	if a, e := attempt(owners, "owner"); a != nil || e != nil {
		return a, e
	}
	members := []string{}
	for _, login := range others {
		key := repo.Host + " " + login
		orgs := m.orgs[key]
		if orgs == nil {
			orgs = map[string]bool{}
			out, e := m.as(ctx, repo.Host, login, []string{"api", "--hostname", repo.Host, "user/orgs", "--paginate", "--jq", ".[].login"})
			if e == nil {
				for _, o := range strings.Split(out, "\n") {
					orgs[strings.ToLower(strings.TrimSpace(o))] = true
				}
			}
			m.orgs[key] = orgs
		}
		if orgs[owner] {
			members = append(members, login)
		}
	}
	if a, e := attempt(members, "member"); a != nil || e != nil {
		return a, e
	}
	if a, e := attempt(others, "access"); a != nil || e != nil {
		return a, e
	}
	return nil, &Problem{"no-access", "None of the logged-in accounts can read " + repo.Repo}
}
func (m *Manager) Project(ctx context.Context, cwd string, refresh bool) Project {
	m.mu.Lock()
	defer m.mu.Unlock()
	if refresh {
		m.forget()
	}
	return m.project(ctx, cwd)
}
func (m *Manager) project(ctx context.Context, cwd string) Project {
	out := Project{Accounts: []string{}, Chosen: m.settings.Chosen[cwd]}
	repo, e := m.repo(ctx, cwd)
	out.Repo = repo
	if e != nil {
		out.Problem = m.problem(e)
		return out
	}
	logins, e := m.logins(ctx, repo.Host)
	out.Accounts = append(out.Accounts, logins...)
	if e == nil {
		out.Account, e = m.account(ctx, cwd, repo)
	}
	out.Chosen = m.settings.Chosen[cwd]
	out.Problem = m.problem(e)
	return out
}
func (m *Manager) Choose(ctx context.Context, cwd string, login *string) Project {
	m.mu.Lock()
	defer m.mu.Unlock()
	if login != nil {
		if !loginRE.MatchString(*login) {
			return Project{Accounts: []string{}, Problem: &Problem{"failed", "Invalid GitHub login"}}
		}
		repo, e := m.repo(ctx, cwd)
		if e != nil {
			return Project{Accounts: []string{}, Problem: m.problem(e)}
		}
		logins, e := m.logins(ctx, repo.Host)
		if e != nil || !has(logins, *login) {
			return Project{Accounts: []string{}, Problem: &Problem{"no-login", "That account is not logged in on this host"}}
		}
		m.settings.Chosen[cwd] = *login
	} else {
		delete(m.settings.Chosen, cwd)
	}
	if e := m.persist(); e != nil {
		return Project{Accounts: []string{}, Problem: m.problem(e)}
	}
	return m.project(ctx, cwd)
}
func (m *Manager) call(ctx context.Context, cwd string, repo *Repo, args []string) (string, error) {
	account, e := m.account(ctx, cwd, repo)
	if e != nil {
		return "", e
	}
	out, e := m.as(ctx, repo.Host, account.Login, args)
	if e == nil || !strings.Contains(e.Error(), "Could not resolve to a Repository") {
		return out, e
	}
	if account.Reason == "chosen" {
		return "", &Problem{"no-access", "The explicitly chosen account cannot read this repository"}
	}
	delete(m.settings.Saved, strings.ToLower(repo.Host+"/"+repo.Repo))
	if e = m.persist(); e != nil {
		return "", e
	}
	account, e = m.account(ctx, cwd, repo)
	if e != nil {
		return "", e
	}
	return m.as(ctx, repo.Host, account.Login, args)
}
func (m *Manager) List(ctx context.Context, cwd, kind, filter string) map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	fail := func(e error) map[string]any { return map[string]any{"problem": m.problem(e)} }
	if (kind != "issue" && kind != "pr") || (filter != "open" && filter != "closed") {
		return fail(errors.New("invalid GitHub list kind or state"))
	}
	repo, e := m.repo(ctx, cwd)
	if e != nil {
		return fail(e)
	}
	fields := "number,title,state,author,labels,createdAt,updatedAt,url,body"
	if kind == "pr" {
		fields += ",isDraft,headRefName,baseRefName,reviewDecision"
	}
	text, e := m.call(ctx, cwd, repo, []string{kind, "list", "--repo", repo.Host + "/" + repo.Repo, "--state", filter, "--limit", "101", "--json", fields})
	if e != nil {
		return fail(e)
	}
	var raw []map[string]any
	if e = json.Unmarshal([]byte(text), &raw); e != nil {
		return fail(e)
	}
	items := []map[string]any{}
	for _, r := range raw[:min(100, len(raw))] {
		items = append(items, toItem(kind, r))
	}
	return map[string]any{"items": items, "more": len(raw) > 100}
}
func toItem(kind string, raw map[string]any) map[string]any {
	body, _ := raw["body"].(string)
	runes := []rune(body)
	if len(runes) > 20000 {
		body = string(runes[:20000]) + "…"
	}
	author := "ghost"
	if a, ok := raw["author"].(map[string]any); ok {
		if l, ok := a["login"].(string); ok && l != "" {
			author = l
		}
	}
	state := "open"
	if raw["state"] == "MERGED" {
		state = "merged"
	} else if raw["state"] == "CLOSED" {
		state = "closed"
	}
	labels := raw["labels"]
	if labels == nil {
		labels = []any{}
	}
	out := map[string]any{"kind": kind, "number": raw["number"], "title": raw["title"], "state": state, "author": author, "labels": labels, "createdAt": raw["createdAt"], "updatedAt": raw["updatedAt"], "url": raw["url"], "body": body}
	if kind == "pr" {
		out["draft"] = raw["isDraft"] == true
		out["head"] = raw["headRefName"]
		out["base"] = raw["baseRefName"]
		review, _ := raw["reviewDecision"].(string)
		review = strings.ToLower(review)
		if has([]string{"approved", "changes_requested", "review_required"}, review) {
			out["review"] = review
		}
	}
	return out
}

var numberRE = regexp.MustCompile(`^#?([0-9]{1,9})$`)
var itemURL = regexp.MustCompile(`(?i)^https?://([^/]+)/([^/]+/[^/]+)/(issues|pull)/([0-9]{1,9})(?:[/?#].*)?$`)

func ParseInput(input string, repo *Repo) (int, error) {
	input = strings.TrimSpace(input)
	if a := numberRE.FindStringSubmatch(input); a != nil {
		n, _ := strconv.Atoi(a[1])
		if n > 0 {
			return n, nil
		}
	}
	if a := itemURL.FindStringSubmatch(input); a != nil {
		if strings.ToLower(a[1]) != repo.Host || !strings.EqualFold(a[2], repo.Repo) {
			return 0, errors.New("link belongs to another repository")
		}
		n, _ := strconv.Atoi(a[4])
		if n > 0 {
			return n, nil
		}
	}
	return 0, errors.New("enter an issue/PR number or a link into this repository")
}
func (m *Manager) Lookup(ctx context.Context, cwd, input string) map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	fail := func(e error) map[string]any { return map[string]any{"problem": m.problem(e)} }
	repo, e := m.repo(ctx, cwd)
	if e != nil {
		return fail(e)
	}
	n, e := ParseInput(input, repo)
	if e != nil {
		return fail(e)
	}
	text, e := m.call(ctx, cwd, repo, []string{"api", "--hostname", repo.Host, fmt.Sprintf("repos/%s/issues/%d", repo.Repo, n), "--jq", "{title, url: .html_url, pr: (.pull_request != null)}"})
	if e != nil {
		return fail(e)
	}
	var raw struct {
		Title string `json:"title"`
		URL   string `json:"url"`
		PR    bool   `json:"pr"`
	}
	if e = json.Unmarshal([]byte(text), &raw); e != nil {
		return fail(e)
	}
	kind := "issue"
	if raw.PR {
		kind = "pr"
	}
	expected := fmt.Sprintf("https://%s/%s/%s/%d", repo.Host, repo.Repo, map[bool]string{true: "pull", false: "issues"}[raw.PR], n)
	if raw.URL != expected {
		return fail(errors.New("GitHub returned a link outside the expected repository identity"))
	}
	return map[string]any{"ref": map[string]any{"kind": kind, "host": repo.Host, "repo": repo.Repo, "number": n, "url": raw.URL, "title": raw.Title}}
}
