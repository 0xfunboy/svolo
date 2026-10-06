//go:build windows

package proc

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

func Configure(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200, HideWindow: true}
	c.Cancel = func() error {
		if c.Process == nil {
			return os.ErrProcessDone
		}
		return exec.Command("taskkill", "/PID", strconv.Itoa(c.Process.Pid), "/T", "/F").Run()
	}
}
func Terminate(c *exec.Cmd) error {
	if c.Process == nil {
		return os.ErrProcessDone
	}
	return c.Process.Kill()
}
