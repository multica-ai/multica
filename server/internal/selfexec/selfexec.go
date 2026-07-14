// Package selfexec resolves the executable backing the current process.
package selfexec

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Resolve prefers the OS-reported executable path. Some launch environments
// can omit that metadata, so it falls back to argv[0] using normal executable
// lookup semantics instead of treating a bare command name as relative to the
// current directory.
func Resolve() (string, error) {
	return resolveWith(os.Executable, os.Args)
}

func resolveWith(osExecutable func() (string, error), args []string) (string, error) {
	exePath, err := osExecutable()
	if err == nil {
		return exePath, nil
	}
	osExecutableErr := fmt.Errorf("os.Executable: %w", err)

	if len(args) == 0 || args[0] == "" {
		return "", errors.Join(osExecutableErr, errors.New("argv[0] is empty"))
	}

	candidate, fallbackErr := exec.LookPath(args[0])
	if fallbackErr == nil {
		candidate, fallbackErr = filepath.Abs(candidate)
	}
	if fallbackErr == nil {
		var info os.FileInfo
		info, fallbackErr = os.Stat(candidate)
		if fallbackErr == nil && !info.Mode().IsRegular() {
			fallbackErr = fmt.Errorf("%s is not a regular file", candidate)
		}
	}
	if fallbackErr != nil {
		return "", errors.Join(
			osExecutableErr,
			fmt.Errorf("resolve argv[0] %q: %w", args[0], fallbackErr),
		)
	}

	return candidate, nil
}
