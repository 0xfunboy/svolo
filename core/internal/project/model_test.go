package project

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"svolo.local/core/internal/store"
	"testing"
)

func normalized(v any) any { b, _ := json.Marshal(v); var n any; _ = json.Unmarshal(b, &n); return n }
func TestProjectReducerContracts(t *testing.T) {
	b, e := os.ReadFile("testdata/contracts.json")
	if e != nil {
		t.Fatal(e)
	}
	var cases []struct {
		Domain             string
		Initial, Op, Value Object
		Now                int64
		Error              bool
	}
	if e = json.Unmarshal(b, &cases); e != nil {
		t.Fatal(e)
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("%s_%03d_%v", c.Domain, i, c.Op["type"]), func(t *testing.T) {
			got, e := Apply(c.Domain, c.Initial, c.Op, c.Now)
			if c.Error {
				if e == nil {
					t.Fatal("invalid input accepted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(normalized(got), normalized(c.Value)) {
				a, _ := json.Marshal(got)
				b, _ := json.Marshal(c.Value)
				t.Fatalf("got %s\nwant %s", a, b)
			}
		})
	}
}
func TestDomainCASPersistenceAndIdempotency(t *testing.T) {
	s, e := store.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r, e := New(s)
	if e != nil {
		t.Fatal(e)
	}
	m := Mutation{Op: Object{"type": "add", "id": "abc123", "title": "Card", "cwd": "/work"}, OperationID: "first"}
	v, e := r.Mutate("board", m)
	if e != nil || v.Revision != 1 {
		t.Fatal(v, e)
	}
	v, e = r.Mutate("board", m)
	if e != nil || v.Revision != 1 {
		t.Fatal("same operation duplicated", e)
	}
	m.OperationID = "second"
	if _, e = r.Mutate("board", m); e == nil {
		t.Fatal("stale write accepted")
	}
	m.OperationID = "first"
	m.Op["title"] = "different"
	if _, e = r.Mutate("board", m); e == nil {
		t.Fatal("changed request id accepted")
	}
	reload, e := New(s)
	if e != nil {
		t.Fatal(e)
	}
	got, e := reload.Get("board")
	if e != nil || got.Revision != 1 || len(list(got.Value["cards"])) != 1 {
		t.Fatal(got, e)
	}
	if got.Receipts != nil {
		t.Fatal("private dedupe metadata exposed")
	}
}
func TestWindowsPathsAndMigrationSafety(t *testing.T) {
	for _, p := range []string{`C:\work\project`, `D:/work`, `\\server\share\project`} {
		if _, e := path(p); e != nil {
			t.Fatal(p, e)
		}
	}
	for _, p := range []string{"relative", "C:relative", ""} {
		if _, e := path(p); e == nil {
			t.Fatal(p)
		}
	}
	if ProjectOf("/home/u/.svolo/worktrees/abc123/project") != "/project" {
		t.Fatal("worktree mapping")
	}
	bad := Empty("board")
	bad["cards"] = []any{Object{"id": "bad"}}
	if _, e := Validate("board", bad); e == nil {
		t.Fatal("corrupt migration silently accepted")
	}
}

func TestExplicitInvalidCardIDsAreNotReplaced(t *testing.T) {
	for _, id := range []any{"", 123, true, "too-long"} {
		if _, e := Apply("board", Empty("board"), Object{"type": "add", "id": id, "title": "Card", "cwd": "/work"}, 1); e == nil {
			t.Fatalf("explicit invalid ID accepted: %#v", id)
		}
	}
}
