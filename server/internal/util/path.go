package util

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrUnresolvablePath is returned by ResolveSymlinksBestEffort when the
// operating system's own resolution of the path cannot be determined well
// enough to compare against a root: an unknown redirecting reparse point whose
// final location even a handle query cannot name, or a Windows drive-relative
// path on a drive whose current directory this process cannot observe. A
// caller that uses the result as a containment check must treat the error as
// OUTSIDE — "cannot say where this opens" is not "opens where the string
// says". A caller that only wants a canonical form should fall back to the
// input unchanged.
var ErrUnresolvablePath = errors.New("path resolution cannot be determined")

// ResolveSymlinksBestEffort canonicalizes p the way the operating system would
// open it, as far as the filesystem allows: it follows every symlink in the
// existing prefix of p — on Windows directory junctions and other reparse
// points too — applying ".." segments to the FOLLOWED path, not to the string,
// and re-attaches the part that does not exist yet. The result is absolute,
// and the error is ErrUnresolvablePath when even a fail-closed answer is
// impossible to compute (see above).
//
// Windows path kinds are preserved rather than flattened onto the working
// directory. A root-relative path (`\tmp\desc.md`) resolves against the root
// of the working directory's volume, and a drive-relative path (`C:tmp\desc`)
// against the working directory only when that drive IS the working
// directory's drive, because those are the files os.ReadFile actually opens.
// Prefixing either with the working directory instead would let a same-named
// file inside the workdir shadow the outside file the kernel reads, and a
// containment check would pass on the shadow. The same grammar applies to
// link targets, which the kernel resolves by the same rules rather than
// relative to the link.
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
// root it actually escapes. The walk below resolves the longest prefix the
// filesystem can speak for and re-attaches the rest, so every component that
// exists is still resolved and a symlink or junction planted inside the root
// cannot smuggle a candidate past a containment check. Any resolution error
// stops the descent, not just "not exists" — permission-denied on an ancestor
// is equally a "cannot canonicalize here".
//
// Second, ".." is resolved physically wherever the filesystem can say so. The
// path is never lexically cleaned before resolution, because filepath.Clean
// (and filepath.Abs, and filepath.Join across the whole input) collapse
// "escape/.." into nothing and thereby cross a symlink boundary invisibly: a
// caller comparing the cleaned string sees a path inside its root while the
// kernel would open one outside it. The walk applies each ".." to the resolved
// prefix, which is the order the kernel uses. Only the tail after the first
// unresolvable component is joined lexically, where a mismatch is inert — a
// path with an unresolvable prefix cannot be opened at all.
//
// This mirrors Python's Path.resolve(strict=False).
func ResolveSymlinksBestEffort(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	abs := absNoClean(p)
	if !filepath.IsAbs(abs) {
		// absNoClean could not say what the path is relative to: a working
		// directory that cannot be observed, or a Windows drive-relative path
		// on a drive this process has no observable directory for. There is no
		// honest string to return — the kernel may well open something this
		// process cannot name — so this is the error, not a guess.
		return "", fmt.Errorf("%w: %q has no observable base to resolve against", ErrUnresolvablePath, p)
	}
	// Resolve the path as given first. This is the only step that can see a
	// ".." the way the kernel does, so it must not be preceded by cleaning.
	resolved, err := evalPath(abs)
	if errors.Is(err, ErrUnresolvablePath) {
		return "", err
	}
	if err == nil {
		return resolved, nil
	}
	// Walk the UNCLEANED path one component at a time: resolve what exists,
	// apply ".." to what has been resolved, and re-attach everything after the
	// first component the filesystem cannot speak for. The root is the walk's
	// starting point without being resolved — "/" always resolves on Unix, and
	// on Windows (measured on go1.26.2, 10.0.19045) a drive letter with no
	// volume behind it is equally served by treating it as resolved: the tail
	// is unreachable for the kernel either way, so the lexical form is the
	// best answer available.
	root, segs := splitNoClean(abs)
	walked := root
	for i, seg := range segs {
		switch seg {
		case "..":
			walked = dropLastSegment(walked)
			continue
		case ".", "":
			continue
		}
		next, err := evalPath(joinSegment(walked, seg))
		if errors.Is(err, ErrUnresolvablePath) {
			// A component the kernel may resolve somewhere this process cannot
			// name. Lexically re-attaching the tail would read as inside a
			// workdir the kernel is not confined to, so this error propagates
			// instead of degrading into an answer.
			return "", err
		}
		if err != nil {
			// seg is the first unresolvable component: missing, a permission
			// wall, too many links, a junction whose target is gone. The
			// kernel stops there too, so the rest is re-attached lexically —
			// nothing after an unresolvable component can be opened, which
			// makes any divergence inside the tail inert.
			for _, tail := range segs[i:] {
				walked = joinSegment(walked, tail)
			}
			return walked, nil
		}
		walked = next
	}
	return walked, nil
}

// splitNoClean splits an absolute path into its root and its components
// WITHOUT cleaning: "/a/../b" gives root "/" and segments ["a", "..", "b"],
// `C:\a\b` gives root `C:\` and segments ["a", "b"]. Empty segments from
// doubled or trailing separators are dropped, which is how the kernel treats
// them. On a relative path the root comes back empty and the segments are the
// components as given.
func splitNoClean(p string) (root string, segs []string) {
	vol := filepath.VolumeName(p)
	rest := p[len(vol):]
	i := 0
	for i < len(rest) && os.IsPathSeparator(rest[i]) {
		i++
	}
	root = vol + rest[:i]
	rest = rest[i:]
	for len(rest) > 0 {
		j := 0
		for j < len(rest) && !os.IsPathSeparator(rest[j]) {
			j++
		}
		if j > 0 {
			segs = append(segs, rest[:j])
		}
		for j < len(rest) && os.IsPathSeparator(rest[j]) {
			j++
		}
		rest = rest[j:]
	}
	return root, segs
}

// dropLastSegment returns the parent of a resolved, canonical, symlink-free
// path, fixpointing at the root: the kernel's ".." from `/`, `C:\`, or a UNC
// share stays there. The name carries the invariant — the argument must
// already be free of "." and "..", because filepath.Dir cleans, and cleaning
// a not-yet-resolved path is the exact trap this file exists to avoid.
func dropLastSegment(resolved string) string {
	return filepath.Dir(resolved)
}

// joinSegment appends one raw component to a resolved prefix. The component
// may legitimately be ".." or "." — inside the unresolvable tail the lexical
// meaning is the point — and filepath.Join applies it to the prefix without
// disturbing the rest, because a resolved prefix has nothing left to clean.
func joinSegment(resolved, seg string) string {
	return filepath.Join(resolved, seg)
}
