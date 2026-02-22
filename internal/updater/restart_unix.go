//go:build !windows

package updater

import (
	"os"
	"path/filepath"
	"syscall"
)

// Restart replaces the running process with a fresh copy of the same binary
// using execve(2). The PID is preserved, which means systemd keeps tracking
// the service without any restart bookkeeping.
func Restart() error {
	execPath, err := os.Executable()
	if err != nil {
		return err
	}
	// Resolve any symlink so we exec the real file, not a dangling symlink
	// to the binary we just replaced.
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return err
	}
	return syscall.Exec(execPath, os.Args, os.Environ())
}
