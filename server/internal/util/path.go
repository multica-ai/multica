package util

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrUnresolvablePath is returned by ResolveSymlinks and
// ResolveSymlinksBestEffort when the operating system's own resolution of the
// path cannot be determined well enough to compare against a root: an unknown
// redirecting reparse point whose final location even a handle query cannot
// name, or a Windows drive-relative path on a drive whose current directory
// this process cannot observe. A caller that uses the result as a containment
// check must treat the error as OUTSIDE — "cannot say where this opens" is not
// "opens where the string says". A caller that only wants a canonical form
// should fall back to the input unchanged.
var ErrUnresolvablePath = errors.New("path resolution cannot be determined")

// ResolveSymlinks is the strict counterpart of ResolveSymlinksBestEffort: it
// resolves p the way the operating system opens it — every symlink followed,
// and on Windows every directory junction too — and fails when any component
// is missing, unreadable, or cannot be resolved. It is for callers that must
// prove a path lies inside a root before trusting it, where
// filepath.EvalSymlinks cannot serve on Windows: under the winsymlink
// semantics this module's go directive selects, it fails with ENOTDIR on any
// path that passes THROUGH a junction (a workspaces root moved to another
// drive and left behind as a junction, #8946), and returns a junction at the
// END of a path unfollowed, so a link out of the root reads as inside it.
//
// Every location it returns was walked component by component. On Windows a
// path that can only be spelled in the device namespace — a volume GUID, a raw
// device, or a link into one — is ErrUnresolvablePath rather than a lexical
// answer, because two such strings can compare as nested while a junction
// between them leads somewhere else entirely (see evalPathStrict).
//
// On Unix this is filepath.EvalSymlinks. On Windows a path with no junction in
// it keeps the spelling filepath.EvalSymlinks gives it (see canonicalSpelling),
// so switching a caller over changes nothing for such paths.
//
// This mirrors Python's Path.resolve(strict=True).
func ResolveSymlinks(p string) (string, error) {
	abs := absNoClean(p)
	if !filepath.IsAbs(abs) {
		return "", fmt.Errorf("%w: %q has no observable base to resolve against", ErrUnresolvablePath, p)
	}
	resolved, err := evalPathStrict(abs)
	if err != nil {
		return "", err
	}
	return canonicalSpelling(resolved), nil
}

// ResolveSymlinksBestEffort returns an absolute path with existing symlinks
// resolved and an unresolved tail reattached. Return ErrUnresolvablePath when
// even a fail-closed result cannot be computed.
//
// Resolve .. against the followed path, never a lexically cleaned input: cleaning
// first can hide a symlink escape. Resolve the longest existing prefix so missing
// descendants remain in the same namespace as a containment root. Stop descent
// on any resolution failure, including permission errors.
//
// Preserve Windows volume-rooted and drive-relative semantics for both input and
// link targets so the result matches the path the kernel would open.
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
