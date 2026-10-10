// Copyright 2026 Multica. All rights reserved.

package testutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// EnvTB is the part of testing.T needed to redirect environment variables.
type EnvTB interface {
	Helper()
	Setenv(key, value string)
}

// IsolateUserHome creates and installs a process-wide scratch home. Callers
// must exit without running tests when this returns an error: continuing would
// let home-relative test writes reach the user's real files.
func IsolateUserHome(pattern string) (string, error) {
	home, err := os.MkdirTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("create scratch home: %w", err)
	}
	cleanupError := func(err error) (string, error) {
		_ = os.RemoveAll(home)
		return "", err
	}
	for _, key := range []string{"HOME", "USERPROFILE"} {
		if err := os.Setenv(key, home); err != nil {
			return cleanupError(fmt.Errorf("set %s: %w", key, err))
		}
	}
	resolved, err := os.UserHomeDir()
	if err != nil {
		return cleanupError(fmt.Errorf("verify scratch home: %w", err))
	}
	if filepath.Clean(resolved) != filepath.Clean(home) {
		return cleanupError(fmt.Errorf("verify scratch home: os.UserHomeDir returned %q, want %q", resolved, home))
	}
	return home, nil
}

// RedirectUserHome points both home environment variables at dir. Go reads
// HOME on Unix and USERPROFILE on Windows, so tests must always set the pair.
func RedirectUserHome(t EnvTB, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}
