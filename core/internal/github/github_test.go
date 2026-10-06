package github

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"svolo.local/core/internal/store"
	"testing"
)

func manager(t *testing.T) *Manager {
	t.Helper()
	st, e := store.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.Close() })
	return New(st)
}
func TestRemoteAndItemParsing(t *testing.T) {
	for _, raw := range []string{"https://github.com/acme/project.git", "git@github.com:acme/project.git", "ssh://git@github.com:22/acme/project.git"} {
		t.Run(raw, func(t *testing.T) {
			r := ParseRemote(raw)
			if r == nil || r.Host != "github.com" || r.Repo != "acme/project" {
				t.Fatal(r)
			}
		})
	}
	for _, raw := range []string{"/tmp/repo", "https://github.com/a/b/deeper", "https://github.com/../secret"} {
		t.Run(raw, func(t *testing.T) {
			if ParseRemote(raw) != nil {
				t.Fatal(raw)
			}
		})
	}
	r := PickRemote([]Remote{{"origin", "git@github.com:a/fork.git", ""}, {"upstream", "https://github.com/owner/base", ""}})
	if r.Repo != "owner/base" {
		t.Fatal(r)
	}
	r = PickRemote([]Remote{{"origin", "git@github.com:a/fork.git", "base"}, {"upstream", "https://github.com/owner/base", ""}})
	if r.Repo != "a/fork" {
		t.Fatal(r)
	}
	for _, raw := range []string{"#12", "12", "https://github.com/a/fork/pull/12/files#x"} {
		n, e := ParseInput(raw, r)
		if e != nil || n != 12 {
			t.Fatal(raw, n, e)
		}
	}
	for _, raw := range []string{"0", "https://github.com/b/other/issues/12", "--exec"} {
		if _, e := ParseInput(raw, r); e == nil {
			t.Fatal(raw)
		}
	}
}
func fixture(t *testing.T, m *Manager, access map[string]bool, orgs map[string]string) *[]string {
	t.Helper()
	calls := []string{}
	m.Exec = func(ctx context.Context, file string, args []string, env map[string]string) (string, error) {
		s := strings.Join(args, " ")
		calls = append(calls, file+" "+s)
		if file == "git" {
			return "remote.origin.url git@github.com:acme/project.git\n", nil
		}
		switch {
		case s == "auth status --json hosts":
			return `{"hosts":{"github.com":[{"login":"personal","active":true},{"login":"acme"},{"login":"work"}]}}`, nil
		case strings.HasPrefix(s, "auth token "):
			return args[len(args)-1] + "-test-token", nil
		case strings.Contains(s, "user/orgs"):
			return orgs[env["GH_TOKEN"]], nil
		case strings.Contains(s, "--jq .full_name"):
			if access[env["GH_TOKEN"]] {
				return "acme/project", nil
			}
			return "", &ExecError{Err: errors.New("Not Found (HTTP 404)"), File: "gh"}
		case strings.HasPrefix(s, "issue list"):
			return `[{"number":1,"title":"First","state":"OPEN","author":{"login":"author"},"labels":[],"body":"Body","url":"https://github.com/acme/project/issues/1"}]`, nil
		case strings.Contains(s, "issues/12"):
			return `{"title":"Twelve","url":"https://github.com/acme/project/pull/12","pr":true}`, nil
		}
		return "", errors.New("unexpected fixture call: " + s)
	}
	return &calls
}
func TestOwnerBeforeActiveAndSavedPersistence(t *testing.T) {
	m := manager(t)
	calls := fixture(t, m, map[string]bool{"acme-test-token": true, "personal-test-token": true}, nil)
	p := m.Project(context.Background(), "/project", false)
	if p.Problem != nil || p.Account.Login != "acme" || p.Account.Reason != "owner" {
		t.Fatal(p)
	}
	p = m.Project(context.Background(), "/project", false)
	if p.Account.Reason != "saved" {
		t.Fatal(p)
	}
	for _, c := range *calls {
		if strings.Contains(c, "auth switch") {
			t.Fatal("switched global account")
		}
	}
	var saved Settings
	if e := m.st.Read("github-settings", &saved); e != nil || saved.Saved["github.com/acme/project"] != "acme" {
		t.Fatal(saved, e)
	}
	b, _ := json.Marshal(saved)
	if strings.Contains(string(b), "token") {
		t.Fatal("persisted token")
	}
}
func TestOrganizationBeforeOtherAccessibleAccount(t *testing.T) {
	m := manager(t)
	fixture(t, m, map[string]bool{"work-test-token": true, "personal-test-token": true}, map[string]string{"work-test-token": "Acme\n"})
	p := m.Project(context.Background(), "/project", false)
	if p.Problem != nil || p.Account.Login != "work" || p.Account.Reason != "member" {
		t.Fatal(p)
	}
}
func TestExplicitAccountAndLookup(t *testing.T) {
	m := manager(t)
	fixture(t, m, map[string]bool{"personal-test-token": true}, nil)
	login := "personal"
	p := m.Choose(context.Background(), "/project", &login)
	if p.Problem != nil || p.Account.Reason != "chosen" {
		t.Fatal(p)
	}
	out := m.List(context.Background(), "/project", "issue", "open")
	if out["problem"] != nil || len(out["items"].([]map[string]any)) != 1 {
		t.Fatal(out)
	}
	lookup := m.Lookup(context.Background(), "/project", "12")
	if lookup["problem"] != nil || lookup["ref"].(map[string]any)["kind"] != "pr" {
		t.Fatal(lookup)
	}
}
func TestCredentialRedactionAnd401Refresh(t *testing.T) {
	m := manager(t)
	retrieved := 0
	attempt := 0
	m.Exec = func(_ context.Context, _ string, args []string, env map[string]string) (string, error) {
		if args[0] == "auth" {
			retrieved++
			return "private-token-123456", nil
		}
		attempt++
		if attempt == 1 {
			return "", &ExecError{Err: errors.New("Bad credentials private-token-123456 (HTTP 401)"), File: "gh"}
		}
		return "", errors.New("failed with private-token-123456 and ghp_abcdefghijklmnopqrstuvwxyz")
	}
	_, e := m.as(context.Background(), "github.com", "test", []string{"api"})
	p := m.problem(e)
	if retrieved != 2 || attempt != 2 || strings.Contains(p.Message, "private-token") || strings.Contains(p.Message, "ghp_") {
		t.Fatal(retrieved, attempt, p)
	}
	if TokenVariable("corp.ghe.com") != "GH_TOKEN" || TokenVariable("enterprise.local") != "GH_ENTERPRISE_TOKEN" {
		t.Fatal("wrong token destination")
	}
}
func TestListStateAndBodyPreserved(t *testing.T) {
	item := toItem("pr", map[string]any{"state": "MERGED", "body": strings.Repeat("x", 20005), "isDraft": true, "reviewDecision": "CHANGES_REQUESTED"})
	if item["state"] != "merged" || item["author"] != "ghost" || item["review"] != "changes_requested" || len([]rune(item["body"].(string))) != 20001 {
		t.Fatal(item)
	}
}
func TestNoAccess(t *testing.T) {
	m := manager(t)
	fixture(t, m, nil, nil)
	p := m.Project(context.Background(), "/project", false)
	if p.Problem == nil || p.Problem.Kind != "no-access" {
		t.Fatal(p)
	}
}
