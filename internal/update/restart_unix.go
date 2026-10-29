//go:build !windows

package update

import (
	"os"
	"path/filepath"
	"syscall"
)

func restartProcess(exe string) error {
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	args := append([]string{exe}, os.Args[1:]...)
	return syscall.Exec(exe, args, os.Environ())
}
