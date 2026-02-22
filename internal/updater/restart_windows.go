//go:build windows

package updater

import (
	"os"
	"os/exec"
	"path/filepath"
)

// Restart spawns a new copy of the binary and exits the current process.
// Windows does not support execve, so the PID changes on restart.
func Restart() error {
	execPath, err := os.Executable()
	if err != nil {
		return err
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return err
	}
	cmd := exec.Command(execPath, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
