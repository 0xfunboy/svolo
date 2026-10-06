// Package computer supervises native desktop providers. Commands are serialized;
// a timeout kills the provider and is NEVER retried (the action may have happened).
// The caller must apply session/permission policy before invoking Call.
package computer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"svolo.local/core/internal/processenv"
	"svolo.local/core/internal/store"
	"runtime"
	"sync"
	"time"
)

//go:embed native/*
var sources embed.FS

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

type response struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *Error          `json:"error"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}
type Helper struct {
	mu         sync.Mutex
	root       string
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	replies    chan response
	dead       chan error
	id         uint64
	token      string
	Notify     func(string, json.RawMessage)
	Foreground bool
}

func New(root string) *Helper { return &Helper{root: filepath.Join(root, "native-computer")} }
func (h *Helper) start() error {
	if h.cmd != nil {
		return nil
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return errors.New("this native provider supports Linux and Windows; macOS uses the bundled Swift helper")
	}
	if err := os.MkdirAll(h.root, 0700); err != nil {
		return err
	}
	entries, _ := sources.ReadDir("native")
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := sources.ReadFile("native/" + e.Name())
		if err != nil {
			return err
		}
		p := filepath.Join(h.root, e.Name())
		sum := sha256.Sum256(b)
		existing, _ := os.ReadFile(p)
		if sha256.Sum256(existing) != sum {
			if err = store.Atomic(p, b, 0600); err != nil {
				return err
			}
		}
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-STA", "-File", filepath.Join(h.root, "windows.ps1"))
	} else {
		cmd = exec.Command("python3", "-I", "-u", filepath.Join(h.root, "linux.py"))
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return err
	}
	h.token = hex.EncodeToString(token)
	cmd.Env = append(processenv.Safe(), "SVOLO_NATIVE_TOKEN="+h.token, fmt.Sprintf("SVOLO_OWNER_PIDS=%d,%d", os.Getpid(), os.Getppid()))
	if h.Foreground {
		cmd.Env = append(cmd.Env, "SVOLO_NATIVE_FOREGROUND=1")
	}
	// Never forward helper stderr (which may contain app text) into the journal.
	cmd.Stderr = io.Discard
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return err
	}
	if err = cmd.Start(); err != nil {
		in.Close()
		return err
	}
	h.cmd, h.stdin = cmd, in
	h.replies = make(chan response, 1)
	h.dead = make(chan error, 1)
	replies, dead := h.replies, h.dead
	notify := h.Notify
	go func() {
		scan := bufio.NewScanner(out)
		scan.Buffer(make([]byte, 4096), 16<<20)
		for scan.Scan() {
			var r response
			if err := json.Unmarshal(scan.Bytes(), &r); err != nil {
				_ = cmd.Process.Kill()
				break
			}
			if r.Method != "" {
				if notify != nil {
					notify(r.Method, r.Params)
				}
				continue
			}
			select {
			case replies <- r:
			default:
				_ = cmd.Process.Kill()
			}
		}
		err := scan.Err()
		wait := cmd.Wait()
		if err == nil {
			err = wait
		}
		if err == nil {
			err = io.EOF
		}
		dead <- err
	}()
	return nil
}
func (h *Helper) stopLocked() {
	if h.stdin != nil {
		_ = h.stdin.Close()
	}
	if h.cmd != nil {
		_ = h.cmd.Process.Kill()
	}
	h.cmd = nil
	h.stdin = nil
	h.token = ""
}
func (h *Helper) Close() { h.mu.Lock(); defer h.mu.Unlock(); h.stopLocked() }
func (h *Helper) ConfigureForeground(v bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if v != h.Foreground {
		h.stopLocked()
		h.Foreground = v
	}
}
func (h *Helper) Call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := h.start(); err != nil {
		return nil, err
	}
	if params == nil {
		params = map[string]any{}
	}
	h.id++
	id := h.id
	b, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params, "token": h.token})
	if err != nil {
		return nil, err
	}
	if len(b) > 2<<20 {
		return nil, errors.New("native request too large")
	}
	// A timed out operation kills the entire child, so no late action may run after
	// the next approval/control epoch. It is reported as uncertain, never replayed.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	writeErr := make(chan error, 1)
	in := h.stdin
	go func() { _, err := in.Write(append(b, '\n')); writeErr <- err }()
	select {
	case err = <-writeErr:
		if err != nil {
			h.stopLocked()
			return nil, err
		}
	case <-ctx.Done():
		h.stopLocked()
		return nil, errors.New("native write timed out; action outcome uncertain")
	}
	select {
	case r := <-h.replies:
		if r.ID != id {
			h.stopLocked()
			return nil, errors.New("native protocol identity mismatch")
		}
		if r.Error != nil {
			return nil, r.Error
		}
		if method == "shutdown" {
			h.stopLocked()
		}
		if method == "screenshot" {
			var shot map[string]any
			if json.Unmarshal(r.Result, &shot) == nil {
				if png, ok := shot["png"].(string); ok {
					b, e := base64.StdEncoding.DecodeString(png)
					if e != nil {
						return nil, e
					}
					cfg, _, e := image.DecodeConfig(bytes.NewReader(b))
					if e != nil || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 20000000 {
						return nil, errors.New("native image dimensions refused")
					}
					im, _, e := image.Decode(bytes.NewReader(b))
					if e != nil {
						return nil, e
					}
					var out bytes.Buffer
					if e = jpeg.Encode(&out, im, &jpeg.Options{Quality: 85}); e != nil {
						return nil, e
					}
					delete(shot, "png")
					shot["jpeg"] = base64.StdEncoding.EncodeToString(out.Bytes())
					return json.Marshal(shot)
				}
			}
		}
		return r.Result, nil
	case err := <-h.dead:
		h.stopLocked()
		return nil, fmt.Errorf("native helper exited; action outcome uncertain: %w", err)
	case <-ctx.Done():
		h.stopLocked()
		return nil, errors.New("native operation cancelled or timed out; verify outcome before retrying")
	}
}
