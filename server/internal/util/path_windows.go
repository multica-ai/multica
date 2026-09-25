//go:build windows

package util

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// absNoClean makes p absolute the way the Windows loader would resolve it,
// without cleaning, so any ".." survives for evalPath to apply after following
// links. Windows has several relative kinds and they do NOT all resolve
// against the working directory, so they are preserved rather than prefixed:
//
//   - drive-absolute `C:\x` and UNC `\\srv\share\x` are already absolute.
//   - root-relative `\x` (or `/x`) resolves against the ROOT of the current
//     directory's volume — for a UNC current directory, against the share.
//     Prefixing the working directory instead would let `C:\task\workdir\tmp`
//     shadow the `C:\tmp` that os.ReadFile actually opens, and a containment
//     check would pass on the shadow while the kernel reads past it.
//   - drive-relative `C:x` resolves against the current directory ON C:. Only
//     the current drive's directory is observable to this process — it IS
//     os.Getwd() — so for that drive the prefix is dropped and the working
//     directory substituted. Any other drive's per-drive directory cannot be
//     read, so the path is handed back unresolved: ResolveSymlinksBestEffort
//     reports ErrUnresolvablePath for it rather than guessing.
func absNoClean(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	cwd, err := os.Getwd()
	if err != nil {
		return p
	}
	sep := string(filepath.Separator)
	if len(p) > 0 && os.IsPathSeparator(p[0]) {
		return filepath.VolumeName(cwd) + filepath.FromSlash(p)
	}
	if vol := filepath.VolumeName(p); vol != "" {
		if strings.EqualFold(vol, filepath.VolumeName(cwd)) {
			return strings.TrimSuffix(cwd, sep) + sep + p[len(vol):]
		}
		return p
	}
	return strings.TrimSuffix(cwd, sep) + sep + p
}

// maxFollowedLinks bounds how many reparse points one resolution may follow —
// the budget filepath.EvalSymlinks works with on Unix — so a junction chain
// that closes on itself turns into an error rather than a hang.
const maxFollowedLinks = 255

var errTooManyLinks = errors.New("too many levels of symbolic links")

// evalPath resolves p the way the kernel opens it, following both NTFS
// symlinks and directory junctions. filepath.EvalSymlinks cannot serve here:
// under the winsymlink semantics this module's go directive selects (Go 1.23+)
// it refuses to descend through a junction — it resolves the junction to its
// own name — which is what let an out-of-workdir junction read as inside the
// workdir. os.Readlink answers on a junction under both winsymlink settings,
// with a clean absolute target and no \??\ prefix (measured on 10.0.19045 /
// go1.26.6), and it answers even when the target no longer exists.
//
// Components are walked one at a time so the first missing or unreadable
// component is the error ResolveSymlinksBestEffort's walk switches to its
// lexical tail on, and ".." is applied to the resolved prefix, which is the
// order the kernel uses. When a reparse point cannot be resolved at all —
// including by handle — the error wraps ErrUnresolvablePath, because the
// kernel may still open the path somewhere this process cannot name.
func evalPath(p string) (string, error) {
	if strings.HasPrefix(p, `\\?\`) || strings.HasPrefix(p, `\\.\`) {
		// The walk never descends into a device namespace: components below a
		// volume GUID or a raw device are joined lexically (see the verbatim
		// branch below), and reparse resolution has nothing to add there.
		// Splitting the string would misread it — `?` is not a component — so
		// a device-namespace path is its own resolution.
		return p, nil
	}
	root, segs := splitNoClean(p)
	resolved := root
	follows := 0
	for i := 0; i < len(segs); {
		seg := segs[i]
		i++
		switch seg {
		case "..":
			resolved = dropLastSegment(resolved)
			continue
		case ".", "":
			continue
		}
		cand := joinSegment(resolved, seg)
		fi, err := os.Lstat(cand)
		if err != nil {
			return "", err
		}
		if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0 {
			resolved = cand
			continue
		}
		follows++
		if follows > maxFollowedLinks {
			return "", errTooManyLinks
		}
		target, rerr := os.Readlink(cand)
		if rerr != nil {
			// os.Readlink decodes only the symlink and mount-point tags. A
			// name-surrogate it cannot decode — a container-isolation link,
			// say — still redirects: the kernel follows it wherever the filter
			// points, so a Readlink failure cannot mean "does not redirect".
			// Resolve the component through a handle instead; the error case
			// is ErrUnresolvablePath, never "keep the lexical location".
			final, ferr := finalPathByHandle(cand)
			if ferr != nil {
				return "", fmt.Errorf("%w: reparse point %q: %w", ErrUnresolvablePath, cand, ferr)
			}
			if sameWin32Path(final, cand) {
				// The component resolves to itself: it redirects nothing (a
				// OneDrive placeholder, a deduplicated file). The kernel opens
				// it where it stands.
				resolved = cand
				continue
			}
			// It redirects: adopt the path the handle names and keep walking
			// from there, through the same classification as any link target.
			target = final
		}
		base, tSegs, verbatim, ok := classifyTarget(target, resolved)
		if !ok {
			return "", fmt.Errorf("%w: link target %q cannot be placed on an observable volume", ErrUnresolvablePath, target)
		}
		if verbatim {
			// A device-namespace target (`\\?\Volume{GUID}\...`) has no Win32
			// spelling to walk or to compare; the kernel jumps to it directly.
			// Join the remaining components lexically and stop: a containment
			// comparison against a drive-letter root fails closed across the
			// namespace, which is the honest answer for a mount target.
			for _, tail := range segs[i:] {
				base = joinSegment(base, tail)
			}
			return base, nil
		}
		// Splice the target's components ahead of the remaining ones and keep
		// walking: a target can itself contain links, and its missing tail is
		// resolved in the same pass instead of being mistaken for a missing
		// component of the original path.
		resolved = base
		segs = append(tSegs, segs[i:]...)
		i = 0
	}
	return resolved, nil
}

// classifyTarget makes a link target absolute the way the kernel resolves it
// and splits it for the walk. The kernel does NOT resolve targets against the
// link's directory by default — it applies the same grammar as any path:
//
//   - device-namespace forms are absolute. `\\?\C:\dir` and `\\?\UNC\srv\share`
//     have a plain Win32 spelling to walk and compare; a volume GUID or a raw
//     device does not, and is returned verbatim so the containment comparison
//     fails closed across the namespace instead of splicing a mangled
//     "relative" path under the workdir. The prefix rules match
//     internal/daemon/canonical_path_windows.go (#6883), which needs the same
//     distinction for launchability rather than containment.
//   - `\??\` (the object-manager root a junction's substitute name carries)
//     is stripped and the remainder re-classified; mklink junctions normally
//     arrive from os.Readlink without it (measured).
//   - a root-relative target (`\outside`) resolves against the root of the
//     volume the link sits on — NOT against the link's directory. Judging it
//     from the link's parent is how a same-named shadow under the workdir
//     passes while os.ReadFile opens the volume-root path.
//   - a drive-relative target (`C:x`) resolves against the current directory
//     on C:. Only the current drive's directory is observable (it is
//     os.Getwd()); any other drive reports !ok and fails closed.
//   - a plain-relative target resolves against the directory containing the
//     link — the caller's resolved prefix stays the base.
func classifyTarget(target, linkDir string) (base string, tSegs []string, verbatim bool, ok bool) {
	if strings.HasPrefix(target, `\\?\`) || strings.HasPrefix(target, `\\.\`) {
		if conv, converted := win32FromExtendedLength(target); converted {
			root, segs := splitNoClean(conv)
			return root, segs, false, true
		}
		return target, nil, true, true
	}
	if strings.HasPrefix(target, `\??\`) || strings.HasPrefix(target, `\\??\`) {
		// Strip the object-manager root and re-classify the remainder. The
		// two spellings differ in length, so the longer one strips first.
		t := target
		switch {
		case strings.HasPrefix(t, `\\??\`):
			t = t[len(`\\??\`):]
		default:
			t = t[len(`\??\`):]
		}
		if rest, found := strings.CutPrefix(t, `UNC\`); found {
			t = `\\` + rest
		}
		return classifyTarget(t, linkDir)
	}
	if filepath.IsAbs(target) {
		// Drive-absolute and UNC. This MUST precede the VolumeName check
		// below: VolumeName is non-empty for `C:\dir` too, and misreading a
		// drive-absolute target as drive-relative resolves it against the
		// working directory instead of the volume root — which is how a
		// junction escape read as inside the workdir on CI.
		root, segs := splitNoClean(target)
		return root, segs, false, true
	}
	if len(target) > 0 && os.IsPathSeparator(target[0]) {
		conv := filepath.VolumeName(linkDir) + filepath.FromSlash(target)
		root, segs := splitNoClean(conv)
		return root, segs, false, true
	}
	if vol := filepath.VolumeName(target); vol != "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", nil, false, false
		}
		if strings.EqualFold(vol, filepath.VolumeName(cwd)) {
			sep := string(filepath.Separator)
			root, segs := splitNoClean(strings.TrimSuffix(cwd, sep) + sep + target[len(vol):])
			return root, segs, false, true
		}
		return "", nil, false, false
	}
	_, segs := splitNoClean(target)
	return linkDir, segs, false, true
}

// win32FromExtendedLength converts a `\\?\`- or `\\.\`-prefixed path to its
// plain Win32 form when one exists: a drive-qualified path (`\\?\C:\dir`) or a
// UNC path (`\\?\UNC\srv\share`). Anything else under the prefix — a volume
// GUID, a raw device — has no Win32 spelling, and is returned unchanged with
// false so the caller keeps it verbatim. The conversion ignores path length
// on purpose: the form is only ever compared here, never handed to an API,
// and Rel across a mismatched prefix would read a long in-workdir path as
// outside it.
func win32FromExtendedLength(p string) (string, bool) {
	if rest, found := strings.CutPrefix(p, `\\?\UNC\`); found {
		return `\\` + rest, true
	}
	for _, prefix := range []string{`\\?\`, `\\.\`} {
		if rest, found := strings.CutPrefix(p, prefix); found {
			if len(rest) >= 3 && rest[1] == ':' && os.IsPathSeparator(rest[2]) {
				return rest, true
			}
			return p, false
		}
	}
	return p, false
}

// sameWin32Path compares the path a handle names with the path the walk is
// standing on. GetFinalPathNameByHandle answers in extended-length form; both
// sides are reduced to plain Win32 before comparing, case-insensitively, the
// way Win32 matches paths.
func sameWin32Path(final, cand string) bool {
	if conv, ok := win32FromExtendedLength(final); ok {
		final = conv
	}
	return strings.EqualFold(final, cand)
}

// finalPathByHandle names the path the kernel actually opens for p, following
// every reparse point in it — including the tags os.Readlink cannot decode.
// It opens p with zero desired access, so it performs no reads and no writes
// (a cloud placeholder is not hydrated), and passes FILE_FLAG_BACKUP_SEMANTICS
// because directories cannot be opened without it. An error here means the
// resolution is genuinely unknowable to this process; it is the caller's
// fail-closed signal, not a license to judge the lexical form.
func finalPathByHandle(p string) (string, error) {
	pUTF16, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(
		pUTF16,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)
	buffer := make([]uint16, syscall.MAX_PATH)
	for {
		length, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
		if err != nil {
			return "", err
		}
		if length < uint32(len(buffer)) {
			return windows.UTF16ToString(buffer[:length]), nil
		}
		buffer = make([]uint16, length+1)
	}
}
