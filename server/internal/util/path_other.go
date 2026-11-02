//go:build !windows

package util

import (
	"os"
	"path/filepath"
	"strings"
)

// absNoClean makes p absolute by prefixing the working directory verbatim,
// without filepath.Abs's implied Clean, so any ".." survives for evalPath to
// apply after following symlinks. Unix has exactly one relative kind —
// relative to the working directory — so there is nothing to classify; the
// Windows counterpart preserves several (see path_windows.go).
func absNoClean(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	cwd, err := os.Getwd()
	if err != nil {
		return p
	}
	sep := string(filepath.Separator)
	return strings.TrimSuffix(cwd, sep) + sep + p
}

// evalPath resolves p the way the kernel opens it: every symlink followed and
// every ".." applied to the followed path. filepath.EvalSymlinks already does
// exactly that; the Windows counterpart is a component-wise resolver because
// EvalSymlinks refuses to descend through directory junctions (see
// path_windows.go). Like EvalSymlinks, it fails when any component is missing
// or unreadable — the signal ResolveSymlinksBestEffort's walk switches to its
// lexical tail on.
func evalPath(p string) (string, error) {
	return filepath.EvalSymlinks(p)
}

// evalPathStrict is evalPath on Unix: filepath.EvalSymlinks already walks every
// component and has no namespace it returns unwalked.
func evalPathStrict(p string) (string, error) {
	return evalPath(p)
}

// canonicalSpelling is the identity on Unix: evalPath is filepath.EvalSymlinks
// here, so its result is already the spelling callers compare against.
func canonicalSpelling(resolved string) string {
	return resolved
}
