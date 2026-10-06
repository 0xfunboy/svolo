package browser

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeTransport struct {
	block   chan struct{}
	started chan struct{}
}

func (f *fakeTransport) List(context.Context, string) ([]Target, error) {
	return []Target{{ID: "tab", Type: "page"}}, nil
}
func (f *fakeTransport) Create(context.Context, string, string) (Target, error) {
	return Target{ID: "tab"}, nil
}
func (f *fakeTransport) Call(ctx context.Context, s, t, m string, p map[string]any, out any) error {
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	if f.block != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.block:
		}
	}
	if out != nil {
		b := []byte(`{"result":{"value":true}}`)
		return json.Unmarshal(b, out)
	}
	return nil
}
func (f *fakeTransport) Activate(context.Context, string, string) error { return nil }
func (f *fakeTransport) CloseTab(context.Context, string, string) error { return nil }
func (f *fakeTransport) CloseSession(string) error                      { return nil }
func (f *fakeTransport) Close() error                                   { return nil }
func TestControlDefaultsToHuman(t *testing.T) {
	e := NewEngine(&fakeTransport{}, t.TempDir())
	if _, err := e.Run(context.Background(), "test", "agent", "state", nil); err == nil {
		t.Fatal("agent ran without control")
	}
	if _, err := e.Control("test", "agent"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Run(context.Background(), "test", "agent", "state", nil); err != nil {
		t.Fatal(err)
	}
}
func TestTakeoverCancelsInFlight(t *testing.T) {
	f := &fakeTransport{block: make(chan struct{}), started: make(chan struct{}, 1)}
	e := NewEngine(f, t.TempDir())
	e.Control("s", "agent")
	done := make(chan error, 1)
	go func() { _, err := e.Run(context.Background(), "s", "agent", "state", nil); done <- err }()
	<-f.started
	e.Control("s", "human")
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("takeover did not cancel")
	}
}
func TestURLPolicy(t *testing.T) {
	for _, s := range []string{"https://example.com/", "http://127.0.0.1:123/", "about:blank"} {
		if err := ValidURL(s); err != nil {
			t.Fatal(s, err)
		}
	}
	for _, s := range []string{"javascript:alert(1)", "file:///etc/passwd", "data:text/html,foo", "http://user:pw@localhost/", "http:///oops", "chrome://settings"} {
		if ValidURL(s) == nil {
			t.Fatal("accepted", s)
		}
	}
}
func TestArtifactIntegrity(t *testing.T) {
	d := t.TempDir()
	a, err := SaveArtifact(d, "s", "test.txt", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyArtifact(d, "s", a.ID); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(a.Path, []byte("bad!!"), 0600)
	if _, err = VerifyArtifact(d, "s", a.ID); err == nil {
		t.Fatal("tampering undetected")
	}
	if _, err = VerifyArtifact(d, "../s", a.ID); err == nil {
		t.Fatal("traversal allowed")
	}
	if _, err = SaveArtifact(d, "s", filepath.Join("..", "safe.txt"), []byte("x")); err != nil {
		t.Fatal(err)
	}
}
func TestBridgeCancellationDoesNotReplay(t *testing.T) {
	b := NewBridge()
	defer b.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := b.Create(ctx, "s", "about:blank"); done <- err }()
	req, err := b.Poll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	<-done
	if b.Reply(BridgeReply{ID: req.ID, Result: json.RawMessage(`{}`)}) {
		t.Fatal("accepted late reply")
	}
}
