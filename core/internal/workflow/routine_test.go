package workflow

import (
	"context"
	"os"
	"path/filepath"
	"svolo.local/core/internal/store"
	"testing"
)

func TestGraphPauseResumeAndGuard(t *testing.T) {
	st, _ := store.Open(t.TempDir())
	defer st.Close()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.json"), []byte(`{"entry":"read","max_steps":10,"nodes":{"read":{"tool":"fake","args":{"value":"{{ input }}"},"save":"result","next":"guard"},"guard":{"guard":{"path":"result.ok","op":"true"},"then":"human","else":"fail"},"human":{"hitl":"Check it","resume":"done"},"done":{"terminal":"success"},"fail":{"terminal":"failure"}}}`), 0600)
	r := New(st, dir)
	calls := 0
	r.Execute = func(ctx context.Context, s, tool string, p map[string]any) (any, error) {
		calls++
		if p["value"] != float64(3) {
			tst := p
			t.Error(tst)
		}
		return map[string]any{"ok": true}, nil
	}
	s, e := r.Start(context.Background(), "test", "session", map[string]any{"input": float64(3)})
	if e != nil || s.Status != "waiting_human" {
		t.Fatal(s, e)
	}
	s, e = r.Resume(context.Background(), s.ID, "session", nil)
	if e != nil || s.Status != "success" || calls != 1 {
		t.Fatal(s, e, calls)
	}
	if _, e = r.Resume(context.Background(), s.ID, "session", nil); e == nil {
		t.Fatal("replayed complete graph")
	}
}
func TestBudgetAndUnknownFlags(t *testing.T) {
	g := Graph{Entry: "a", Nodes: map[string]Node{"a": {Tool: "read", Next: "missing"}}}
	if g.Validate() == nil {
		t.Fatal("bad transition")
	}
	if _, e := Arguments("verify-artifact", []byte(`["id","--semantic","url"]`), nil); e == nil {
		t.Fatal("silently ignored semantic flags")
	}
}
func TestTypedTemplate(t *testing.T) {
	a, e := Arguments("foo", []byte(`{"number":"{{ v }}","label":"a {{ n.x }}"}`), map[string]any{"v": float64(5), "n": map[string]any{"x": "b"}})
	if e != nil || a["number"] != float64(5) || a["label"] != "a b" {
		t.Fatal(a, e)
	}
}
