package computer

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestNativeSupervisorHandshakeRestartAndCancellation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("this execution qualifies the Linux helper only")
	}
	if _, e := exec.LookPath("python3"); e != nil {
		t.Skip("native helper requires Python 3")
	}
	h := New(t.TempDir())
	defer h.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	read := func() int {
		t.Helper()
		raw, e := h.Call(ctx, "hello", map[string]any{})
		if e != nil {
			t.Fatal(e)
		}
		var value struct {
			Protocol int
			OS       string
			PID      int
		}
		if e = json.Unmarshal(raw, &value); e != nil || value.Protocol != 1 || value.OS != "linux" || value.PID <= 0 {
			t.Fatal(string(raw), e)
		}
		return value.PID
	}
	first := read()
	if read() != first {
		t.Fatal("helper restarted between read-only requests")
	}
	if _, e := h.Call(ctx, "unknown-test-method", nil); e == nil {
		t.Fatal("unknown method accepted")
	} else {
		var native *Error
		if !errors.As(e, &native) || native.Code != -32601 {
			t.Fatal(e)
		}
	}
	h.ConfigureForeground(true)
	second := read()
	if first == second {
		t.Fatal("permissions change failed to replace old helper")
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, e := h.Call(canceled, "hello", nil); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	h.Close()
	if read() == second {
		t.Fatal("closed helper reused")
	}
}
