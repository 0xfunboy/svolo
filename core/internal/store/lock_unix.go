//go:build !windows

package store

import (
	"fmt"
	"os"
	"syscall"
)

// The kernel releases the lock on crash. A stale file does not require deletion.
func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("data directory is already in use by another process: %w", err)
	}
	return f, nil
}
