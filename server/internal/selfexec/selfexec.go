// Package selfexec resolves the executable backing the current process.
package selfexec

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Resolve prefers the OS-reported executable path. Some launch environments
// can omit that metadata, so it falls back to argv[0] using normal executable
// lookup semantics instead of treating a bare command name as relative to the
// current directory.
func Resolve() (string, error) {
	return resolveWith(os.Executable, os.Args)
}

// IsEphemeralBuild reports whether path sits under a `go run` build directory
// (`<tmp>/go-build<random>/b001/exe/<name>`), which the Go toolchain deletes
// when the launching `go run` exits. Binaries produced by `go build -o` or
// installed by a package manager never do. Callers that record their own
// executable path for a later re-exec need this: the recorded path resolves
// cleanly and stops existing minutes later.
//
// Resolve cannot catch it on its own — while `go run` is alive, os.Executable
// succeeds and the file is genuinely on disk, so the argv[0] fallback (which
// only triggers on an os.Executable error) never runs.
//
// Both separators are split on regardless of GOOS, so a Windows path stays
// classifiable when it is inspected from a test or a log on another platform.
func IsEphemeralBuild(path string) bool {
	segments := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' })
	for _, segment := range segments {
		if isGoBuildTempDir(segment) {
			return true
		}
	}
	return false
}

// isGoBuildTempDir matches the directory name `go run` creates via
// os.MkdirTemp("", "go-build"): the literal prefix plus the random digits
// MkdirTemp appends. The bare name `go-build` is the persistent build cache
// (GOCACHE) and is deliberately not matched.
func isGoBuildTempDir(segment string) bool {
	const prefix = "go-build"
	digits, ok := strings.CutPrefix(segment, prefix)
	if !ok || digits == "" {
		return false
	}
	return strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' }) < 0
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
