package agent

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskUploadRequiresSelectedFileAndHumanApprovalDespitePreapproval(t *testing.T) {
	var steps atomic.Int64
	var executions atomic.Int64
	m := newHarnessManager(t, func(w http.ResponseWriter, r *http.Request) {
		if steps.Add(1) == 1 {
			harnessReply(w, "", Call{ID: "upload", Name: "upload", Arguments: `{"tab":"portal","target":"css:input[type=file]","file":"selected.pdf"}`})
		} else {
			harnessReply(w, "Reviewed upload complete")
		}
	})
	m.Tools = func() []Tool { return []Tool{harnessTool("upload", false)} }
	m.BrowserContext = func(context.Context, string) (any, error) {
		return []map[string]any{{"id": "portal", "url": "https://portal.example/form", "active": true}}, nil
	}
	m.Execute = func(context.Context, string, string, map[string]any) (any, error) {
		executions.Add(1)
		return map[string]any{"ok": true}, nil
	}
	run, e := m.Start(RunRequest{Session: "forms", Provider: "fixture", Prompt: "Upload the selected contract", Autonomy: "browser", AllowedTools: []string{"upload"}, UploadFiles: []string{"selected.pdf"}, UploadOrigins: []string{"https://portal.example"}, TaskOrigins: []string{"https://portal.example"}, MaxSteps: 2})
	if e != nil {
		t.Fatal(e)
	}
	var approval *Approval
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		list := m.Approvals()
		if len(list) > 0 {
			approval = &list[0]
			break
		}
		time.Sleep(time.Millisecond)
	}
	if approval == nil {
		t.Fatal("upload did not require approval")
	}
	if executions.Load() != 0 {
		t.Fatal("file uploaded before approval")
	}
	if approval.Arguments["destinationOrigin"] != "https://portal.example" {
		t.Fatal("approval missing verified destination")
	}
	if e = m.Approve(approval.ID, true); e != nil {
		t.Fatal(e)
	}
	finished := waitHarnessRun(t, m, run.ID)
	if finished.Status != "completed" || executions.Load() != 1 {
		t.Fatal(finished.Status, executions.Load())
	}
}
func TestTaskUploadRejectsForeignFileWrapperAndChangedDestination(t *testing.T) {
	m := &Manager{BrowserContext: func(context.Context, string) (any, error) {
		return []map[string]any{{"id": "portal", "url": "https://foreign.example/"}}, nil
	}}
	req := RunRequest{UploadFiles: []string{"selected.pdf"}, UploadOrigins: []string{"https://portal.example"}}
	for _, args := range []map[string]any{{"tab": "portal", "file": "private.txt"}, {"tab": "portal", "file": "selected.pdf"}} {
		if _, e := m.validateUpload(context.Background(), "forms", req, "upload", args); e == nil {
			t.Fatal("unsafe upload accepted")
		}
	}
	if _, e := m.validateUpload(context.Background(), "forms", req, "browser-operation", map[string]any{"operation": "upload"}); e == nil {
		t.Fatal("wrapper bypassed scoped upload")
	}
	var step atomic.Int64
	var origin atomic.Value
	origin.Store("https://portal.example/form")
	var executions atomic.Int64
	manager := newHarnessManager(t, func(w http.ResponseWriter, r *http.Request) {
		if step.Add(1) == 1 {
			harnessReply(w, "", Call{ID: "upload", Name: "upload", Arguments: `{"tab":"portal","target":"css:input","file":"selected.pdf"}`})
		} else {
			harnessReply(w, "Upload blocked after destination changed")
		}
	})
	manager.Tools = func() []Tool { return []Tool{harnessTool("upload", false)} }
	manager.BrowserContext = func(context.Context, string) (any, error) {
		return []map[string]any{{"id": "portal", "url": origin.Load().(string)}}, nil
	}
	manager.Execute = func(context.Context, string, string, map[string]any) (any, error) { executions.Add(1); return nil, nil }
	run, e := manager.Start(RunRequest{Session: "forms", Provider: "fixture", Prompt: "Upload only after approval", Autonomy: "ask", UploadFiles: []string{"selected.pdf"}, MaxSteps: 2})
	if e != nil {
		t.Fatal(e)
	}
	until := time.Now().Add(3 * time.Second)
	var answered bool
	for time.Now().Before(until) {
		pending := manager.Approvals()
		if len(pending) > 0 {
			origin.Store("https://foreign.example/")
			if e = manager.Approve(pending[0].ID, true); e != nil {
				t.Fatal(e)
			}
			answered = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !answered {
		t.Fatal("approval missing")
	}
	finished := waitHarnessRun(t, manager, run.ID)
	if executions.Load() != 0 {
		t.Fatal("changed destination received file")
	}
	if err:=manager.Store.Read("run-"+finished.ID,&finished);err!=nil{t.Fatal(err)}
	found := false
	for _, turn := range finished.History {
		if strings.Contains(turn.Text, "destination changed") || strings.Contains(turn.Text,"outside the task profile") {
			found = true
		}
	}
	if !found {
		t.Fatal("changed destination was not reported")
	}
}
