//go:build !windows

// Package proc owns process groups; cancellation never shuts down the host.
package proc

import (
	"os"
	"os/exec"
	"syscall"
)

func Configure(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
}
func Terminate(c *exec.Cmd) error {
	if c.Process == nil {
		return os.ErrProcessDone
	}
	return syscall.Kill(-c.Process.Pid, syscall.SIGTERM)
}
