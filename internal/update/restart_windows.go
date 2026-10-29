//go:build windows

package update

import (
	"os"
	"os/exec"
	"path/filepath"
)

func restartProcess(exe string) error {
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "FLASHROUTE_RESTART=1")
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
