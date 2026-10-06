package service

import (
	"context"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"svolo.local/core/internal/store"
	"strconv"
)

func installUserUnit(ctx context.Context, o InstallOptions, args []string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	label := "io.svolo.agent." + strconv.Itoa(o.Port)
	path := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	command := "<string>" + html.EscapeString(o.Executable) + "</string>"
	for _, a := range args {
		command += "<string>" + html.EscapeString(a) + "</string>"
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>%s</string><key>ProgramArguments</key><array>%s</array><key>RunAtLoad</key><true/><key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict><key>StandardOutPath</key><string>%s</string><key>StandardErrorPath</key><string>%s</string></dict></plist>`, label, command, html.EscapeString(filepath.Join(o.Data, "daemon.log")), html.EscapeString(filepath.Join(o.Data, "daemon.log")))
	if err = store.Atomic(path, []byte(plist), 0600); err != nil {
		return "", err
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	_ = exec.CommandContext(ctx, "launchctl", "bootout", domain+"/"+label).Run()
	b, e := exec.CommandContext(ctx, "launchctl", "bootstrap", domain, path).CombinedOutput()
	if e != nil {
		return path, fmt.Errorf("launchctl: %w: %.1000s", e, b)
	}
	return path, nil
}
