// Package service manages only per-user svolo-core processes. It does not change
// system power settings, require root, accept host keys or install global services.
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"svolo.local/core/internal/store"
)

type Info struct {
	Type      string `json:"type"`
	URL       string `json:"url"`
	Data      string `json:"data"`
	TokenFile string `json:"tokenFile"`
	Version   string `json:"version"`
	PID       int    `json:"pid"`
}
type InstallOptions struct {
	Data       string
	Executable string
	Port       int
	Mode       string
	Headless   bool
	Replace    bool
}
type InstallResult struct {
	Info               Info   `json:"info"`
	Mode               string `json:"mode"`
	Unit               string `json:"unit,omitempty"`
	SurvivesDisconnect bool   `json:"survivesDisconnect"`
	StartsAtLogin      bool   `json:"startsAtLogin"`
	Note               string `json:"note,omitempty"`
}

func Load(data string) (Info, error) {
	var info Info
	b, err := os.ReadFile(filepath.Join(data, "service.json"))
	if err != nil {
		return info, err
	}
	if json.Unmarshal(b, &info) != nil {
		return info, errors.New("invalid service identity")
	}
	address, err := neturl(info.URL)
	if err != nil {
		return info, err
	}
	_ = address
	abs, err := filepath.Abs(data)
	if err != nil {
		return info, err
	}
	if info.Data != abs || info.TokenFile != filepath.Join(abs, "auth.token") {
		return info, errors.New("service identity does not belong to selected data directory")
	}
	return info, nil
}
func neturl(address string) (string, error) {
	if !strings.HasPrefix(address, "http://") {
		return "", errors.New("service must use a loopback HTTP address")
	}
	host, port, err := net.SplitHostPort(strings.TrimPrefix(address, "http://"))
	if err != nil {
		return "", err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", errors.New("non-loopback service address refused")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", errors.New("invalid service port")
	}
	return address, nil
}
func Request(ctx context.Context, data, method, path string) ([]byte, error) {
	info, err := Load(data)
	if err != nil {
		return nil, err
	}
	token, err := os.ReadFile(info.TokenFile)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, info.URL+path, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("service redirect refused") }}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	b, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("service HTTP %d: %.500s", response.StatusCode, b)
	}
	return b, nil
}
func Stop(ctx context.Context, data string) error {
	_, err := Request(ctx, data, "POST", "/v1/daemon/stop")
	if err != nil {
		return err
	}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			if _, err := Request(ctx, data, "GET", "/v1/health"); err != nil {
				return nil
			}
		}
	}
}
func Install(ctx context.Context, o InstallOptions) (InstallResult, error) {
	var result InstallResult
	if o.Port < 1 || o.Port > 65535 {
		return result, errors.New("invalid port")
	}
	if o.Mode != "process" && o.Mode != "user-service" {
		return result, errors.New("mode must be process or user-service")
	}
	var err error
	o.Data, err = filepath.Abs(o.Data)
	if err != nil {
		return result, err
	}
	o.Executable, err = filepath.Abs(o.Executable)
	if err != nil {
		return result, err
	}
	if err = os.MkdirAll(o.Data, 0700); err != nil {
		return result, err
	}
	if _, err = Request(ctx, o.Data, "GET", "/v1/health"); err == nil {
		if !o.Replace {
			result.Info, err = Load(o.Data)
			result.Mode = "existing"
			result.Note = "Existing daemon retained; explicit --replace is required to stop running work."
			result.SurvivesDisconnect = true
			return result, err
		}
		if err = Stop(ctx, o.Data); err != nil {
			return result, fmt.Errorf("stop existing daemon: %w", err)
		}
	}
	// Never overwrite or delete a running owner's lock. New serve must acquire it.
	args := []string{"serve", "--data", o.Data, "--listen", "127.0.0.1:" + strconv.Itoa(o.Port)}
	if o.Headless {
		args = append(args, "--headless")
	}
	result.Mode = o.Mode
	if o.Mode == "user-service" {
		result.Unit, err = installUserUnit(ctx, o, args)
		result.StartsAtLogin = err == nil
		result.Note = "User services require the user's service manager/login session. No system-level service or linger setting was changed."
	} else {
		logPath := filepath.Join(o.Data, "daemon.log")
		if st, e := os.Stat(logPath); e == nil && st.Size() > 8<<20 {
			_ = os.Rename(logPath, logPath+".previous")
		}
		log, errOpen := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if errOpen != nil {
			return result, errOpen
		}
		cmd := exec.Command(o.Executable, args...)
		cmd.Stdin = nil
		cmd.Stdout = log
		cmd.Stderr = log
		detach(cmd)
		err = cmd.Start()
		log.Close()
		if err == nil {
			_ = cmd.Process.Release()
		}
		result.Note = "Detached process; persists independently of this client, but not across reboot or administrative termination."
	}
	if err != nil {
		return result, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-waitCtx.Done():
			return result, fmt.Errorf("installed but daemon not ready: %w; inspect %s", waitCtx.Err(), filepath.Join(o.Data, "daemon.log"))
		case <-tick.C:
			if _, err = Request(waitCtx, o.Data, "GET", "/v1/health"); err == nil {
				result.Info, err = Load(o.Data)
				result.SurvivesDisconnect = true
				return result, err
			}
		}
	}
}
func WriteInfo(data string, info Info) error {
	b, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return store.Atomic(filepath.Join(data, "service.json"), b, 0600)
}
