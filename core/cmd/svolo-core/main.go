// svolo-core is deliberately a small standard-library-only host service. The
// desktop, stdio MCP clients and remote hosts all call the same tool backend.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"svolo.local/core/internal/browser"
	"svolo.local/core/internal/mcp"
	"svolo.local/core/internal/server"
	"svolo.local/core/internal/service"
	"svolo.local/core/internal/store"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "svolo-core:", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 {
		fmt.Println(`svolo-core 0.3.1-dev
Commands:
  serve    Start the authenticated loopback daemon and operational console.
  mcp      Expose the selected daemon session to an MCP stdio client.
  call     Invoke a tool explicitly as the authenticated user.
  doctor   Report local prerequisites, without installing or changing them.
  catalog  Print builtin tool schemas without opening user data or a browser.
  version  Print the source build version.
  service  Install, query or stop a persistent per-user daemon.

Run a command with -h for its options. Development release, not production-qualified.`)
		return nil
	}
	switch args[0] {
	case "service":
		return serviceCommand(args[1:])
	case "serve":
		return serve(args[1:])
	case "mcp":
		return stdio(args[1:])
	case "call":
		return call(args[1:])
	case "catalog":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"schemaVersion": 1, "product": "Svolo", "version": server.Version, "tools": server.BuiltinCatalog()})
	case "version":
		fmt.Println(server.Version)
		return nil
	case "doctor":
		return doctor()
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
func defaultData() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "svolo-agent-browser", "core")
}
func serve(args []string) error {
	f := flag.NewFlagSet("serve", flag.ContinueOnError)
	data := f.String("data", defaultData(), "Private persistent data directory (do not place inside agent workspace)")
	listen := f.String("listen", "127.0.0.1:7331", "Loopback bind address; use :0 for an ephemeral port")
	mode := f.String("browser", "managed", "managed (standalone Chromium) or electron (private IPC adapter)")
	chromium := f.String("chromium", "", "Optional absolute Chromium executable")
	chromiumProxy := f.String("chromium-proxy", "", "Optional HTTP proxy for managed browser network isolation")
	artifacts := f.String("artifacts-dir", "", "Root of cross-platform core binary artifacts for SSH bootstrap")
	headless := f.Bool("headless", false, "Launch managed Chromium without native windows")
	noSandbox := f.Bool("no-sandbox-test", false, "TEST ONLY: also requires SVOLO_TEST_NO_SANDBOX=1")
	ready := f.String("ready-file", "", "Optional private JSON file containing address and token file path, not token value")
	if err := f.Parse(args); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("daemon must bind an explicit loopback IP, never 0.0.0.0")
	}
	absolute, err := filepath.Abs(*data)
	if err != nil {
		return err
	}
	stopRequested := make(chan struct{})
	var stopOnce sync.Once
	s, err := server.New(server.Options{ArtifactsDir: *artifacts, Shutdown: func() { stopOnce.Do(func() { close(stopRequested) }) }, Data: absolute, Browser: *mode, Chromium: *chromium, ChromiumProxy: *chromiumProxy, Headless: *headless, NoSandboxForTest: *noSandbox})
	if err != nil {
		return err
	}
	defer s.Close()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	url := "http://" + listener.Addr().String()
	info := map[string]any{"type": "ready", "url": url, "tokenFile": filepath.Join(absolute, "auth.token"), "data": absolute, "browser": *mode, "version": server.Version, "pid": os.Getpid(), "productionQualified": false}
	if err = service.WriteInfo(absolute, service.Info{Type: "ready", URL: url, Data: absolute, TokenFile: filepath.Join(absolute, "auth.token"), Version: server.Version, PID: os.Getpid()}); err != nil {
		listener.Close()
		return err
	}
	defer os.Remove(filepath.Join(absolute, "service.json"))
	if *ready != "" {
		b, _ := json.MarshalIndent(info, "", "  ")
		if err = store.Atomic(*ready, b, 0600); err != nil {
			listener.Close()
			return err
		}
		defer os.Remove(*ready)
	}
	_ = json.NewEncoder(os.Stdout).Encode(info)
	httpServer := &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 25 * time.Second, WriteTimeout: 7 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		select {
		case <-stopRequested:
			cancel()
		case <-ctx.Done():
		}
	}()
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	select {
	case err = <-done:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
		// Cancel active agent operations and bridge waits before draining HTTP. No
		// in-flight action is automatically replayed by the next host instance.
		s.Close()
		stop, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err = httpServer.Shutdown(stop); err != nil {
			_ = httpServer.Close()
		}
		return err
	}
}

type remoteBackend struct{ client mcp.Client }

func (b remoteBackend) ListTools(ctx context.Context) ([]mcp.Tool, error) {
	return mcp.Discover(ctx, b.client)
}
func (b remoteBackend) CallTool(ctx context.Context, name string, a map[string]any) (any, error) {
	var value map[string]any
	err := b.client.Call(ctx, "tools/call", map[string]any{"name": name, "arguments": a}, &value)
	if err != nil {
		return nil, err
	}
	if value["isError"] == true {
		return nil, fmt.Errorf("remote tool rejected: %v", value["content"])
	}
	return mcp.RawToolResult(value), nil
}
func readToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(b))
	if len(v) < 32 || len(v) > 256 {
		return "", errors.New("invalid token file")
	}
	return v, nil
}
func stdio(args []string) error {
	f := flag.NewFlagSet("mcp", flag.ContinueOnError)
	address := f.String("url", "http://127.0.0.1:7331", "Daemon base URL")
	tokenFile := f.String("token-file", "", "File containing a scoped tools-only token minted in the control plane")
	session := f.String("session", "", "Session id (needed only for admin compatibility)")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *tokenFile == "" {
		return errors.New("--token-file is required; use a scoped token, not auth.token")
	}
	token, err := readToken(*tokenFile)
	if err != nil {
		return err
	}
	// Isolated helper process environment; never print this value to protocol stdout.
	const key = "SVOLO_MCP_CLIENT_TOKEN"
	_ = os.Setenv(key, token)
	defer os.Unsetenv(key)
	endpoint := strings.TrimRight(*address, "/") + "/v1/mcp"
	if *session != "" {
		if !store.ValidID(*session) {
			return errors.New("invalid session")
		}
		endpoint += "?session=" + *session
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	client, err := mcp.NewHTTP(ctx, mcp.Config{ID: "daemon", URL: endpoint, TokenEnv: key})
	if err != nil {
		return err
	}
	defer client.Close()
	return mcp.Serve(ctx, os.Stdin, os.Stdout, remoteBackend{client: client})
}
func call(args []string) error {
	f := flag.NewFlagSet("call", flag.ContinueOnError)
	address := f.String("url", "http://127.0.0.1:7331", "Daemon base URL")
	tokenFile := f.String("token-file", filepath.Join(defaultData(), "auth.token"), "Admin token file (manual CLI operations only)")
	sid := f.String("session", "", "Registered session id")
	name := f.String("tool", "", "Tool name")
	params := f.String("args", "{}", "JSON object arguments")
	if err := f.Parse(args); err != nil {
		return err
	}
	var a map[string]any
	if err := json.Unmarshal([]byte(*params), &a); err != nil || a == nil {
		return errors.New("--args must be a JSON object")
	}
	token, err := readToken(*tokenFile)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"session": *sid, "name": *name, "arguments": a})
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(*address, "/")+"/v1/tools/call", bytes.NewReader(body))
	if err != nil {
		return err
	}
	if request.URL.Scheme != "http" || net.ParseIP(request.URL.Hostname()) == nil || !net.ParseIP(request.URL.Hostname()).IsLoopback() {
		return errors.New("CLI manual call requires a loopback daemon; use SSH forwarding for remote hosts")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, err = io.Copy(os.Stdout, io.LimitReader(response.Body, 32<<20))
	if response.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return err
}
func doctor() error {
	report := map[string]any{"version": server.Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "chromium": browser.Executable(), "productionQualified": false, "note": "Presence is not a functional qualification. No system changes were made."}
	return json.NewEncoder(os.Stdout).Encode(report)
}

func serviceCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("service command: install, status or stop")
	}
	f := flag.NewFlagSet("service "+args[0], flag.ContinueOnError)
	data := f.String("data", defaultData(), "Private data directory on THIS host")
	port := f.Int("port", 7331, "Daemon loopback port")
	mode := f.String("mode", "process", "process or user-service (at login)")
	headless := f.Bool("headless", true, "Run managed Chromium headlessly")
	replace := f.Bool("replace", false, "Explicitly stop an existing daemon before replacing it")
	credentials := f.Bool("reply-credentials", false, "PRIVATE SSH bootstrap only: include auth token on stdout; never log or expose to a model")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	switch args[0] {
	case "install":
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		result, err := service.Install(ctx, service.InstallOptions{Data: *data, Port: *port, Mode: *mode, Headless: *headless, Executable: exe, Replace: *replace})
		if err != nil {
			return err
		}
		output := map[string]any{"result": result}
		if *credentials {
			token, err := os.ReadFile(result.Info.TokenFile)
			if err != nil {
				return err
			}
			output["token"] = strings.TrimSpace(string(token))
		}
		return json.NewEncoder(os.Stdout).Encode(output)
	case "status":
		b, err := service.Request(ctx, *data, "GET", "/v1/health")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	case "stop":
		return service.Stop(ctx, *data)
	default:
		return errors.New("service command must be install, status or stop")
	}
}
