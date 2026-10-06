// Package project owns workspace board and activity records. Its JSON shapes
// are shared with the desktop. Applicable notices are in legal/desktop-components.txt.
package project

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"unicode/utf16"
)

type Object = map[string]any

var idRE = regexp.MustCompile(`^[a-z0-9]{6}$`)
var driveRE = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
var branchRE = regexp.MustCompile(`^[\w./+-]+$`)
var hostRE = regexp.MustCompile(`(?i)^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
var repoRE = regexp.MustCompile(`^[\w.-]{1,100}/[\w.-]{1,100}$`)

func str(v any) string { s, _ := v.(string); return s }
func obj(v any) Object { x, _ := v.(map[string]any); return x }
func list(v any) []any {
	a, _ := v.([]any)
	if a == nil {
		return []any{}
	}
	return a
}
func text(v any, name string, max int) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s must be text", name)
	}
	if len(utf16.Encode([]rune(s))) > max {
		return "", fmt.Errorf("%s is too long", name)
	}
	return s, nil
}
func title(v any, max int) (string, error) {
	s, e := text(v, "title", max)
	if e != nil {
		return "", e
	}
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "", errors.New("a title is required")
	}
	return s, nil
}
func path(v any) (string, error) {
	s, ok := v.(string)
	if !ok || len(s) > 4096 || strings.ContainsRune(s, 0) || !(strings.HasPrefix(s, "/") || driveRE.MatchString(s) || strings.HasPrefix(s, `\\`)) {
		return "", errors.New("absolute project/chat path required")
	}
	return s, nil
}
func ProjectOf(p string) string {
	normalized := strings.ReplaceAll(p, `\`, "/")
	marker := "/.svolo/worktrees/"
	at := strings.Index(normalized, marker)
	if at < 0 {
		return p
	}
	rest := normalized[at+len(marker):]
	slash := strings.Index(rest, "/")
	if slash <= 0 || !idRE.MatchString(rest[:slash]) {
		return p
	}
	project := rest[slash:]
	if strings.HasPrefix(project, "/drive-") && len(project) > 9 && project[8] == '/' {
		return strings.ToUpper(project[7:8]) + ":" + strings.ReplaceAll(project[8:], "/", `\`)
	}
	return project
}
func column(v any) bool    { return v == "todo" || v == "in_progress" || v == "in_review" || v == "done" }
func severity(v any) bool  { return v == "annoying" || v == "costly" || v == "blocking" }
func number(v any) float64 { n, _ := v.(float64); return n }
func fresh(items []any) string {
	for {
		n, _ := rand.Int(rand.Reader, big.NewInt(2176782336))
		s := n.Text(36)
		s = strings.Repeat("0", 6-len(s)) + s
		found := false
		for _, r := range items {
			if obj(r)["id"] == s {
				found = true
				break
			}
		}
		if !found {
			return s
		}
	}
}
func copyValue(v Object) Object {
	b, _ := json.Marshal(v)
	var out Object
	_ = json.Unmarshal(b, &out)
	return out
}
func Empty(domain string) Object {
	key := "cards"
	if domain == "laments" {
		key = "laments"
	}
	return Object{"version": 1, key: []any{}}
}
func validDomain(s string) bool { return s == "board" || s == "laments" }
func tags(v any) ([]any, error) {
	a, ok := v.([]any)
	if !ok {
		return nil, errors.New("tags must be a list")
	}
	out := []any{}
	seen := map[string]bool{}
	for _, v := range a {
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("a tag must be text")
		}
		s = strings.ToLower(strings.Join(strings.Fields(strings.TrimLeft(strings.TrimSpace(s), "#")), "-"))
		if len(utf16.Encode([]rune(s))) > 24 {
			return nil, errors.New("tag is too long")
		}
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if len(out) > 6 {
		return nil, errors.New("too many tags")
	}
	return out, nil
}
func ghKey(v Object) string {
	return strings.ToLower(fmt.Sprintf("%s/%s#%.0f", str(v["host"]), str(v["repo"]), number(v["number"])))
}
func github(v any) (Object, error) {
	r := obj(v)
	if r["kind"] != "issue" && r["kind"] != "pr" {
		return nil, errors.New("GitHub link must be issue or pr")
	}
	h, repo, n, u := str(r["host"]), str(r["repo"]), number(r["number"]), str(r["url"])
	if !hostRE.MatchString(h) || !repoRE.MatchString(repo) || n < 1 || n > 9007199254740991 || n != float64(int64(n)) {
		return nil, errors.New("invalid GitHub reference")
	}
	h = strings.ToLower(h)
	if !strings.HasPrefix(strings.ToLower(u), "https://"+h+"/") || len(u) > 2048 {
		return nil, errors.New("GitHub URL must belong to its host")
	}
	name := r["title"]
	if name == nil {
		name = ""
	}
	s, e := text(name, "GitHub title", 300)
	if e != nil {
		return nil, e
	}
	return Object{"kind": r["kind"], "host": h, "repo": repo, "number": n, "url": u, "title": strings.Join(strings.Fields(s), " ")}, nil
}
func githubList(v any) ([]any, error) {
	a, ok := v.([]any)
	if !ok {
		return nil, errors.New("github must be a list")
	}
	out := []any{}
	indices := map[string]int{}
	for _, r := range a {
		g, e := github(r)
		if e != nil {
			return nil, e
		}
		key := ghKey(g)
		if i, ok := indices[key]; ok {
			out[i] = g
		} else {
			indices[key] = len(out)
			out = append(out, g)
		}
	}
	if len(out) > 20 {
		return nil, errors.New("too many GitHub links")
	}
	return out, nil
}
func find(rows []any, id any) (Object, error) {
	for _, r := range rows {
		if obj(r)["id"] == id {
			return obj(r), nil
		}
	}
	return nil, fmt.Errorf("no item %v", id)
}
func remove(rows []any, id any) []any {
	out := []any{}
	for _, r := range rows {
		if obj(r)["id"] != id {
			out = append(out, r)
		}
	}
	return out
}
func place(rows []any, c Object, op Object) []any {
	index := -1
	before, present := op["before"]
	for i, r := range rows {
		if obj(r)["column"] == c["column"] && ((!present) || (str(before) != "" && obj(r)["id"] == before)) {
			index = i
			break
		}
	}
	if index < 0 {
		for i, r := range rows {
			if obj(r)["column"] == c["column"] {
				index = i + 1
			}
		}
		if index < 0 {
			index = len(rows)
		}
	}
	out := make([]any, 0, len(rows)+1)
	out = append(out, rows[:index]...)
	out = append(out, c)
	out = append(out, rows[index:]...)
	return out
}
func chat(v any) (Object, error) {
	r := obj(v)
	p, e := path(r["path"])
	if e != nil {
		return nil, e
	}
	cwd, e := path(r["cwd"])
	if e != nil {
		return nil, e
	}
	return Object{"path": p, "cwd": cwd}, nil
}

// Validate never silently discards records during migration. Unknown extra
// fields are retained, and old cards missing tags/github receive empty arrays.
func Validate(domain string, value Object) (Object, error) {
	if !validDomain(domain) {
		return nil, errors.New("unknown project domain")
	}
	v := copyValue(value)
	key := "cards"
	if domain == "laments" {
		key = "laments"
	}
	rows, ok := v[key].([]any)
	if !ok || len(rows) > 10000 {
		return nil, errors.New("invalid or oversized project file")
	}
	seen := map[string]bool{}
	for _, r := range rows {
		c := obj(r)
		id := str(c["id"])
		if id == "" || seen[id] || c["title"] == nil || c["cwd"] == nil || c["createdAt"] == nil || c["updatedAt"] == nil {
			return nil, errors.New("malformed or duplicate project item")
		}
		seen[id] = true
		if _, e := path(c["cwd"]); e != nil {
			return nil, e
		}
		if _, ok := c["reports"].([]any); !ok {
			return nil, errors.New("invalid reports")
		}
		if domain == "board" {
			if !column(c["column"]) {
				return nil, errors.New("invalid column")
			}
			if _, ok := c["chats"].([]any); !ok {
				return nil, errors.New("invalid chats")
			}
			if c["notes"] == nil {
				return nil, errors.New("missing notes")
			}
			if c["tags"] == nil {
				c["tags"] = []any{}
			}
			if c["github"] == nil {
				c["github"] = []any{}
			}
			for _, r := range list(c["github"]) {
				if _, e := github(r); e != nil {
					return nil, e
				}
			}
		} else {
			if len(list(c["reports"])) == 0 {
				return nil, errors.New("empty lament")
			}
			for _, r := range list(c["reports"]) {
				if !severity(obj(r)["severity"]) {
					return nil, errors.New("invalid severity")
				}
			}
		}
	}
	v["version"] = 1
	return v, nil
}
func Apply(domain string, initial Object, op Object, now int64) (Object, error) {
	if !validDomain(domain) {
		return nil, errors.New("unknown domain")
	}
	v := copyValue(initial)
	if domain == "board" {
		return board(v, op, now)
	}
	return lament(v, op, now)
}
func board(v, op Object, now int64) (Object, error) {
	rows := list(v["cards"])
	kind := str(op["type"])
	var c Object
	var e error
	if kind != "add" {
		c, e = find(rows, op["id"])
		if e != nil {
			return nil, e
		}
	}
	switch kind {
	case "add":
		if len(rows) >= 10000 {
			return nil, errors.New("card quota reached")
		}
		id := str(op["id"])
		if op["id"] == nil {
			id = fresh(rows)
		}
		if !idRE.MatchString(id) {
			return nil, errors.New("invalid card id")
		}
		if _, e = find(rows, id); e == nil {
			return nil, errors.New("duplicate card id")
		}
		name, e := title(op["title"], 300)
		if e != nil {
			return nil, e
		}
		cwd, e := path(op["cwd"])
		if e != nil {
			return nil, e
		}
		col := op["column"]
		if col == nil {
			col = "todo"
		}
		if !column(col) {
			return nil, errors.New("invalid column")
		}
		notes := ""
		if x, ok := op["notes"]; ok {
			notes, e = text(x, "notes", 20000)
			if e != nil {
				return nil, e
			}
		}
		tt := []any{}
		if x, ok := op["tags"]; ok {
			tt, e = tags(x)
			if e != nil {
				return nil, e
			}
		}
		gg := []any{}
		if x, ok := op["github"]; ok {
			gg, e = githubList(x)
			if e != nil {
				return nil, e
			}
		}
		c = Object{"id": id, "title": name, "cwd": cwd, "column": col, "notes": notes, "tags": tt, "github": gg, "chats": []any{}, "reports": []any{}, "createdAt": now, "updatedAt": now}
		v["cards"] = place(rows, c, op)
	case "edit":
		if x, ok := op["title"]; ok {
			c["title"], e = title(x, 300)
			if e != nil {
				return nil, e
			}
		}
		if x, ok := op["notes"]; ok {
			c["notes"], e = text(x, "notes", 20000)
			if e != nil {
				return nil, e
			}
		}
		if x, ok := op["tags"]; ok {
			c["tags"], e = tags(x)
			if e != nil {
				return nil, e
			}
		}
		c["updatedAt"] = now
	case "move":
		if !column(op["column"]) {
			return nil, errors.New("invalid column")
		}
		if op["before"] == c["id"] {
			return v, nil
		}
		c["column"] = op["column"]
		c["updatedAt"] = now
		v["cards"] = place(remove(rows, c["id"]), c, op)
	case "remove":
		v["cards"] = remove(rows, c["id"])
	case "attach":
		ref, e := chat(op["chat"])
		if e != nil {
			return nil, e
		}
		if ProjectOf(str(ref["cwd"])) != c["cwd"] {
			return nil, errors.New("chat belongs to another project")
		}
		ref["at"] = now
		if label := obj(op["chat"])["label"]; str(label) != "" {
			x, e := text(label, "label", 200)
			if e != nil {
				return nil, e
			}
			ref["label"] = strings.Join(strings.Fields(x), " ")
		}
		for _, r := range list(c["chats"]) {
			if obj(r)["path"] == ref["path"] {
				return v, nil
			}
		}
		for _, r := range rows {
			other := obj(r)
			keep := []any{}
			for _, r := range list(other["chats"]) {
				if obj(r)["path"] != ref["path"] {
					keep = append(keep, r)
				}
			}
			other["chats"] = keep
		}
		c["chats"] = append(list(c["chats"]), ref)
		c["updatedAt"] = now
	case "detach":
		keep := []any{}
		for _, r := range list(c["chats"]) {
			if obj(r)["path"] != op["path"] {
				keep = append(keep, r)
			}
		}
		if len(keep) != len(list(c["chats"])) {
			c["chats"] = keep
			c["updatedAt"] = now
		}
	case "report":
		x, e := text(op["text"], "report", 4000)
		if e != nil {
			return nil, e
		}
		r := Object{"at": now, "text": strings.TrimSpace(x)}
		if col, ok := op["column"]; ok {
			if !column(col) {
				return nil, errors.New("invalid column")
			}
			if col != c["column"] {
				r["column"] = col
			}
		}
		if p := str(op["chat"]); p != "" {
			r["chat"], e = path(p)
			if e != nil {
				return nil, e
			}
		}
		if r["text"] == "" && r["column"] == nil {
			return v, nil
		}
		reports := append(list(c["reports"]), r)
		if len(reports) > 50 {
			reports = reports[len(reports)-50:]
		}
		c["reports"] = reports
		c["updatedAt"] = now
		if r["column"] != nil {
			c["column"] = r["column"]
			v["cards"] = place(remove(rows, c["id"]), c, Object{})
		}
	case "link":
		g, e := github(op["github"])
		if e != nil {
			return nil, e
		}
		refs := list(c["github"])
		i := -1
		for j, r := range refs {
			if ghKey(obj(r)) == ghKey(g) {
				i = j
				break
			}
		}
		if i < 0 {
			if len(refs) >= 20 {
				return nil, errors.New("too many GitHub links")
			}
			refs = append(refs, g)
		} else {
			a, _ := json.Marshal(refs[i])
			b, _ := json.Marshal(g)
			if string(a) == string(b) {
				return v, nil
			}
			refs[i] = g
		}
		c["github"] = refs
		c["updatedAt"] = now
	case "unlink":
		key := ghKey(obj(op["github"]))
		keep := []any{}
		for _, r := range list(c["github"]) {
			if ghKey(obj(r)) != key {
				keep = append(keep, r)
			}
		}
		if len(keep) != len(list(c["github"])) {
			c["github"] = keep
			c["updatedAt"] = now
		}
	default:
		return nil, errors.New("unknown board operation")
	}
	return v, nil
}
func lamentReport(op Object, now int64) (Object, error) {
	if !severity(op["severity"]) {
		return nil, errors.New("invalid severity")
	}
	s, e := text(op["text"], "body", 8000)
	if e != nil {
		return nil, e
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("a lament needs a body")
	}
	r := Object{"at": now, "text": s, "severity": op["severity"]}
	if op["chat"] != nil {
		r["chat"], e = chat(op["chat"])
		if e != nil {
			return nil, e
		}
	}
	return r, nil
}
func lament(v, op Object, now int64) (Object, error) {
	rows := list(v["laments"])
	kind := str(op["type"])
	var c Object
	var e error
	if kind != "file" {
		c, e = find(rows, op["id"])
		if e != nil {
			return nil, e
		}
	}
	switch kind {
	case "file":
		if len(rows) >= 10000 {
			return nil, errors.New("lament quota reached")
		}
		id := str(op["id"])
		if op["id"] == nil {
			id = fresh(rows)
		}
		if !idRE.MatchString(id) {
			return nil, errors.New("invalid lament id")
		}
		if _, e = find(rows, id); e == nil {
			return nil, errors.New("duplicate lament id")
		}
		name, e := title(op["title"], 200)
		if e != nil {
			return nil, e
		}
		cwd, e := path(op["cwd"])
		if e != nil {
			return nil, e
		}
		r, e := lamentReport(op, now)
		if e != nil {
			return nil, e
		}
		c = Object{"id": id, "title": name, "cwd": cwd, "reports": []any{r}, "createdAt": now, "updatedAt": now}
		v["laments"] = append(rows, c)
	case "repeat":
		r, e := lamentReport(op, now)
		if e != nil {
			return nil, e
		}
		reports := append(list(c["reports"]), r)
		if len(reports) > 30 {
			reports = append([]any{reports[0]}, reports[len(reports)-29:]...)
		}
		c["reports"] = reports
		delete(c, "resolvedAt")
		c["updatedAt"] = now
	case "fix":
		ref, e := chat(op["chat"])
		if e != nil {
			return nil, e
		}
		fix := Object{"at": now, "chat": ref}
		if b, ok := op["branch"]; ok {
			if len(str(b)) > 200 || !branchRE.MatchString(str(b)) || strings.Contains(str(b), "..") {
				return nil, errors.New("invalid branch")
			}
			fix["branch"] = b
		}
		fixes := []any{}
		for _, f := range list(c["fixes"]) {
			if obj(obj(f)["chat"])["path"] != ref["path"] {
				fixes = append(fixes, f)
			}
		}
		fixes = append(fixes, fix)
		if len(fixes) > 10 {
			fixes = fixes[len(fixes)-10:]
		}
		c["fixes"] = fixes
	case "resolve":
		resolved, ok := op["resolved"].(bool)
		if !ok {
			return nil, errors.New("resolved must be boolean")
		}
		was := number(c["resolvedAt"]) != 0
		if was == resolved {
			return v, nil
		}
		if resolved {
			c["resolvedAt"] = now
		} else {
			delete(c, "resolvedAt")
		}
		c["updatedAt"] = now
	case "remove":
		v["laments"] = remove(rows, c["id"])
	default:
		return nil, errors.New("unknown lament operation")
	}
	return v, nil
}
