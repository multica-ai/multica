package pathutil

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var windowsAbsPathRe = regexp.MustCompile(`^[A-Za-z]:\\`)

// NormalizeLocalPath validates and normalizes a user-provided local path.
func NormalizeLocalPath(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		return "", errors.New("path must not be empty")
	}
	if strings.IndexFunc(path, unicode.IsControl) >= 0 {
		return "", errors.New("path contains control characters")
	}

	path = filepath.Clean(path)
	if path == "." || path == "" {
		return "", errors.New("path must not be empty")
	}
	if len(path) > 1024 {
		return "", errors.New("path is too long")
	}

	// filepath.IsAbs is OS-specific; also allow Windows abs/UNC when running on non-Windows hosts.
	if !(filepath.IsAbs(path) || windowsAbsPathRe.MatchString(path) || strings.HasPrefix(path, `\\`)) {
		return "", errors.New("path must be absolute")
	}

	return path, nil
}
