package server

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVaultControlPlaneDoesNotLeakSecrets(t *testing.T) {
	s, h, _ := setup(t)
	key := strings.Repeat("37", 32)
	secret := "fixture-key-not-for-logs"
	check := func(method, path string, body any, want int) map[string]any {
		t.Helper()
		code, v, _ := request(t, s, h, method, path, s.Token, body, nil)
		if code != want {
			t.Fatal(path, code, v)
		}
		return v
	}
	check("PUT", "/v1/credentials", map[string]any{"name": "provider-one", "value": secret}, 400)
	check("POST", "/v1/vault/unlock", map[string]string{"key": key}, 200)
	check("PUT", "/v1/credentials", map[string]string{"name": "provider-one", "value": secret}, 200)
	got, e := s.Vault.Get("provider-one")
	if e != nil || got != secret {
		t.Fatal(e)
	}
	for _, path := range []string{"/v1/credentials", "/v1/config", "/v1/events", "/v1/vault/status"} {
		req, _ := http.NewRequest("GET", h.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+s.Token)
		r, e := h.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if strings.Contains(string(b), secret) || strings.Contains(string(b), key) {
			t.Fatal("credential exposed:", path)
		}
	}
	e = filepath.Walk(s.Store.Root, func(path string, i os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if !i.IsDir() {
			b, _ := os.ReadFile(path)
			if strings.Contains(string(b), secret) || strings.Contains(string(b), key) {
				return fmt.Errorf("plaintext credential persisted: %s", filepath.Base(path))
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	check("POST", "/v1/vault/lock", map[string]any{}, 200)
	if _, e = s.Vault.Get("provider-one"); e == nil {
		t.Fatal("locked secret read")
	}
	check("POST", "/v1/vault/unlock", map[string]string{"key": strings.Repeat("38", 32)}, 400)
	check("POST", "/v1/vault/unlock", map[string]string{"key": key}, 200)
	check("DELETE", "/v1/credentials?name=provider-one", nil, 200)
	if _, e = s.Vault.Get("provider-one"); e == nil {
		t.Fatal("deleted secret read")
	}
}
func TestProjectHTTPAndAgentWorkspaceOwnership(t *testing.T) {
	s, h, work := setup(t)
	add := map[string]any{"op": map[string]any{"type": "add", "id": "abc999", "title": "Persistent HTTP card", "cwd": work}, "operationId": "http-add-one", "expectedRevision": 0}
	code, v, _ := request(t, s, h, "POST", "/v1/projects/board", s.Token, add, nil)
	if code != 200 || v["revision"] != float64(1) {
		t.Fatal(code, v)
	}
	code, v, _ = request(t, s, h, "POST", "/v1/projects/board", s.Token, add, nil)
	if code != 200 || v["revision"] != float64(1) {
		t.Fatal("duplicated operation", code, v)
	}
	for _, sid := range []string{"one", "two"} {
		code, v, _ = request(t, s, h, "POST", "/v1/tools/call", s.Token, toolRequest{Session: sid, Name: "project-board", Arguments: map[string]any{"action": "get"}}, nil)
		b, _ := json.Marshal(v)
		if code != 200 || strings.Contains(string(b), "abc999") != (sid == "one") {
			t.Fatal("workspace leak", sid, code, v)
		}
	}
	code, v, _ = request(t, s, h, "POST", "/v1/tools/call", s.Token, toolRequest{Session: "two", Name: "project-board", Arguments: map[string]any{"action": "apply", "operation": map[string]any{"type": "remove", "id": "abc999"}}}, nil)
	if code != 400 {
		t.Fatal("foreign card modified", code, v)
	}
	code, v, _ = request(t, s, h, "POST", "/v1/tools/call", s.Token, toolRequest{Session: "one", Name: "browser-operation", Arguments: map[string]any{"operation": "project-board", "arguments": map[string]any{"action": "get"}}}, nil)
	if code != 400 {
		t.Fatal("project action smuggled through browser wrapper", code, v)
	}
}
func TestTransferHTTPResumeChecksumAndIsolation(t *testing.T) {
	s, h, work := setup(t)
	content := []byte("transfer with a verified immutable checksum\n")
	hash := sha256.Sum256(content)
	digest := hex.EncodeToString(hash[:])
	code, v, _ := request(t, s, h, "POST", "/v1/transfers", s.Token, map[string]any{"session": "one", "path": "verified.txt", "size": len(content), "sha256": digest}, nil)
	if code != 200 {
		t.Fatal(code, v)
	}
	id := v["id"].(string)
	put := func(sid string, offset int, data []byte, want int) {
		t.Helper()
		code, v, _ := request(t, s, h, "POST", "/v1/transfers/chunk", s.Token, map[string]any{"session": sid, "id": id, "offset": offset, "data": data}, nil)
		if code != want {
			t.Fatal(code, v)
		}
	}
	put("two", 0, content, 400)
	put("one", 0, content[:9], 200)
	put("one", 0, content[:9], 200)
	put("one", 9, content[9:], 200)
	code, v, _ = request(t, s, h, "POST", "/v1/transfers/commit", s.Token, map[string]string{"session": "one", "id": id}, nil)
	if code != 200 {
		t.Fatal(code, v)
	}
	got, e := os.ReadFile(filepath.Join(work, "verified.txt"))
	if e != nil || string(got) != string(content) {
		t.Fatal(e)
	}
	code, v, _ = request(t, s, h, "POST", "/v1/transfers/download", s.Token, map[string]string{"session": "one", "path": "verified.txt"}, nil)
	if code != 200 {
		t.Fatal(code, v)
	}
	download := v["id"].(string)
	os.WriteFile(filepath.Join(work, "verified.txt"), []byte("changed after snapshot"), 0600)
	code, v, _ = request(t, s, h, "GET", "/v1/transfers/chunk?session=one&id="+download+"&offset=0", s.Token, nil, nil)
	if code != 200 {
		t.Fatal(code, v)
	}
	b, e := base64.StdEncoding.DecodeString(v["data"].(string))
	if e != nil || string(b) != string(content) {
		t.Fatal("download changed with source", e)
	}
	code, v, _ = request(t, s, h, "POST", "/v1/transfers/download", s.Token, map[string]string{"session": "one", "path": "../private/auth.token"}, nil)
	if code != 400 {
		t.Fatal("private transfer accepted", code, v)
	}
}
func TestNativeAndNewRoutesRequireAdministrativeControl(t *testing.T) {
	s, h, _ := setup(t)
	code, v, _ := request(t, s, h, "GET", "/v1/computer/settings", s.Token, nil, nil)
	if code != 200 || v["enabled"] != false {
		t.Fatal("desktop enabled without opt in", code, v)
	}
	code, v, _ = request(t, s, h, "POST", "/v1/computer/native", s.Token, map[string]any{"method": "hello", "params": map[string]any{}}, nil)
	if code != 400 {
		t.Fatal("raw native endpoint outside Electron", code, v)
	}
	_, v, _ = request(t, s, h, "POST", "/v1/tokens", s.Token, map[string]string{"session": "one", "label": "v02"}, nil)
	token := v["token"].(string)
	for _, path := range []string{"/v1/computer/settings", "/v1/computer/native", "/v1/vault/status", "/v1/credentials", "/v1/transfers?session=one", "/v1/projects/board", "/v1/hosts/bootstrap", "/v1/events/stream"} {
		code, v, _ = request(t, s, h, "GET", path, token, nil, nil)
		if code != 403 {
			t.Fatal(path, code, v)
		}
	}
}
func TestEventStreamResumeFilterAndReservedControlCapacity(t *testing.T) {
	s, h, _ := setup(t)
	first, e := s.Store.Append("one", "before.resume", nil)
	if e != nil {
		t.Fatal(e)
	}
	s.Store.Append("two", "foreign.event", nil)
	third, e := s.Store.Append("one", "after.resume", map[string]bool{"durable": true})
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	open := func(path string) *http.Response {
		t.Helper()
		r, _ := http.NewRequestWithContext(ctx, "GET", h.URL+path, nil)
		r.Header.Set("Authorization", "Bearer "+s.Token)
		r.Header.Set("Last-Event-ID", fmt.Sprint(first.Seq))
		res, e := h.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		return res
	}
	response := open("/v1/events/stream?session=" + url.QueryEscape("one"))
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	reader := bufio.NewReader(response.Body)
	var seen strings.Builder
	for !strings.Contains(seen.String(), "event: cursor\n") {
		line, e := reader.ReadString('\n')
		if e != nil {
			t.Fatal(e)
		}
		seen.WriteString(line)
	}
	if strings.Contains(seen.String(), "before.resume") || strings.Contains(seen.String(), "foreign.event") || !strings.Contains(seen.String(), "after.resume") || !strings.Contains(seen.String(), fmt.Sprintf("id: %d", third.Seq)) {
		t.Fatal(seen.String())
	}
	streams := []*http.Response{response}
	defer func() {
		for _, r := range streams {
			r.Body.Close()
		}
	}()
	for i := 1; i < 8; i++ {
		r := open("/v1/events/stream")
		if r.StatusCode != 200 {
			t.Fatal(r.StatusCode)
		}
		streams = append(streams, r)
	}
	denied := open("/v1/events/stream")
	defer denied.Body.Close()
	if denied.StatusCode != 429 {
		t.Fatal("unbounded event streams", denied.StatusCode)
	}
	code, v, _ := request(t, s, h, "GET", "/v1/health", s.Token, nil, nil)
	if code != 200 {
		t.Fatal("streams starved ordinary controls", code, v)
	}
}
