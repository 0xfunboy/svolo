package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"svolo.local/core/internal/store"
	"strconv"
	"strings"
)

func unitQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`).Replace(s) + `"`
}
func installUserUnit(ctx context.Context, o InstallOptions, args []string) (string, error) {
	if strings.ContainsAny(o.Executable+o.Data, "\n\r\x00") {
		return "", errors.New("newline in service path")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	name := "svolo-agent-" + strconv.Itoa(o.Port) + ".service"
	path := filepath.Join(home, ".config", "systemd", "user", name)
	command := unitQuote(o.Executable)
	for _, a := range args {
		command += " " + unitQuote(a)
	}
	unit := fmt.Sprintf("[Unit]\nDescription=Svolo host\nAfter=network.target\n\n[Service]\nType=simple\nExecStart=%s\nRestart=on-failure\nRestartSec=3\nTimeoutStopSec=20\nUMask=0077\nNoNewPrivileges=true\n\n[Install]\nWantedBy=default.target\n", command)
	if err = store.Atomic(path, []byte(unit), 0600); err != nil {
		return "", err
	}
	for _, a := range [][]string{{"--user", "daemon-reload"}, {"--user", "enable", name}, {"--user", "restart", name}} {
		b, e := exec.CommandContext(ctx, "systemctl", a...).CombinedOutput()
		if e != nil {
			return path, fmt.Errorf("systemctl: %w: %.1000s", e, b)
		}
	}
	return path, nil
}
