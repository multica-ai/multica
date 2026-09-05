package util

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolveSymlinksBestEffort canonicalizes p the way the operating system would,
// as far as the filesystem allows: it follows every symlink in the existing
// prefix of p — applying ".." segments to the FOLLOWED path, not to the string
// — and re-attaches the part that does not exist yet. The result is absolute.
//
// Two properties matter to callers that compare the result against a root.
//
// First, a path that does not fully exist still has to land in the same
// namespace as the root. filepath.EvalSymlinks fails outright when any
// component is missing, and falling back to filepath.Clean does not follow
// symlinks at all, so the two sides of the comparison end up in different
// namespaces (on macOS every /tmp and /var path is a symlink into /private) and
// a path inside the root reads as outside it. Falling back one level via
// filepath.Dir only narrows that to "the leaf is missing" — and inverts the
// error for a deeper miss, since an unresolvable tail then reads as inside a
// root it actually escapes. Walking up to the first existing ancestor closes
// both: the walk terminates because filepath.Dir fixpoints at the root (or
// volume on Windows), and every component that exists is still resolved, so a
// symlink planted inside the root cannot smuggle a candidate past a containment
// check. Any EvalSymlinks error stops the descent, not just "not exists" —
// permission-denied on an ancestor is equally a "cannot canonicalize here".
//
// Second, ".." is resolved physically wherever the filesystem can say so. The
// input is handed to EvalSymlinks BEFORE any lexical cleaning, because
// filepath.Clean (and filepath.Abs, and filepath.Join) collapse "escape/.."
// into nothing and thereby cross a symlink boundary invisibly: a caller
// comparing the cleaned string sees a path inside its root while the kernel
// would open one outside it. Only the missing tail is joined lexically, where a
// mismatch is inert — a path whose intermediate directories do not exist cannot
// be opened at all.
//
// This mirrors Python's Path.resolve(strict=False).
func ResolveSymlinksBestEffort(p string) string {
	if p == "" {
		return p
	}
	// Resolve the path as given first. This is the only step that can see a
	// ".." the way the kernel does, so it must not be preceded by cleaning.
	if resolved, err := filepath.EvalSymlinks(absNoClean(p)); err == nil {
		return resolved
	}
	abs := p
	if a, err := filepath.Abs(p); err == nil {
		abs = a
	}
	var missing []string
	for cur := abs; ; {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// A root that does not resolve. No test pins this branch and none
			// can cheaply: "/" always resolves on Unix, and on Windows
			// (measured on go1.26.2, 10.0.19045) filepath.EvalSymlinks(`Z:\`)
			// returns `Z:\` with a nil error for a drive letter that has no
			// volume behind it, even though os.Lstat on the same root fails. So
			// the walk always finds an "existing" ancestor at the root in
			// practice. This is a termination invariant rather than a
			// behaviour: what EvalSymlinks does at a root is unspecified and has
			// already been observed to differ from os.Lstat, so the loop must
			// not depend on it to end. The cleaned absolute form is the best
			// answer available if it is ever reached.
			return abs
		}
		missing = append(missing, filepath.Base(cur))
		cur = parent
	}
}

// absNoClean makes p absolute by prefixing the working directory verbatim,
// without filepath.Abs's implied Clean, so any ".." survives for EvalSymlinks
// to apply after following symlinks.
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
