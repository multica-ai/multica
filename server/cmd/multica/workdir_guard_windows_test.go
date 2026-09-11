//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
)

// createJunction makes dst a directory junction to src and reports what the Go
// runtime makes of it. mklink /J needs no elevation, unlike a directory
// symlink, which is why the junction shape is the one that actually shows up on
// Windows hosts — pnpm's node_modules layout is built from them, and this
// repo's own GC tests use the same fixture.
//
// It deliberately does NOT assert on the mode bits. Whether os.Lstat reports a
// junction with ModeSymlink depends on the GODEBUG winsymlink setting (see the
// test below), so a fixture that fatals on that observation would destroy the
// coverage of the test it sets up. The bits are logged instead, so a CI run
// records what the platform did.
func createJunction(t *testing.T, src, dst string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", dst, src).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J %s %s: %s: %v", dst, src, out, err)
	}
	fi, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("lstat junction: %v", err)
	}
	target, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat through junction: %v", err)
	}
	if !target.IsDir() {
		t.Fatalf("junction %s does not lead to a directory", dst)
	}
	t.Logf("junction %s: Lstat mode=%v symlinkBit=%v irregular=%v",
		dst, fi.Mode(), fi.Mode()&os.ModeSymlink != 0, fi.Mode()&os.ModeIrregular != 0)
}

// TestFileWithinWorkingDirWindowsPaths runs the containment guard's core
// judgments on Windows, where the path grammar the resolution walks is not the
// one the Linux and macOS jobs exercise: paths carry a volume, both `\` and `/`
// separate, and a root behaves differently from `/` (see the drive-letter case
// at the end). cmd/multica is not part of any Windows job today, so without
// this file none of that is covered anywhere. It matters twice over on a
// non-admin host, where the cross-platform cases skip for want of the symlink
// privilege while a junction needs none.
func TestFileWithinWorkingDirWindowsPaths(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("exists.txt", []byte("x"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	outside := t.TempDir()

	cases := []struct {
		name string
		path string
		want bool
	}{
		{name: "existing file", path: "exists.txt", want: true},
		{name: "missing leaf", path: "missing.txt", want: true},
		{name: "missing intermediate directory", path: `subdir\report.md`, want: true},
		{name: "several missing levels", path: `a\b\c\d\report.md`, want: true},
		{name: "forward slashes are separators too", path: "a/b/report.md", want: true},
		{name: "traversal out of the workdir", path: `..\escaped.md`, want: false},
		{name: "absolute path outside", path: filepath.Join(outside, "stale.md"), want: false},
		{name: "missing absolute path outside", path: filepath.Join(outside, "gone", "stale.md"), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := fileWithinWorkingDir(tc.path)
			if err != nil {
				t.Fatalf("fileWithinWorkingDir(%q): %v", tc.path, err)
			}
			if got != tc.want {
				t.Errorf("fileWithinWorkingDir(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}

	t.Run("a path on another volume reads as outside the workdir", func(t *testing.T) {
		// The Windows-only branch of the guard: filepath.Rel cannot relate two
		// paths on different volumes, and returns an error rather than a "..".
		// Reporting that error as a resolve failure — `Rel: can't make Z:\...
		// relative to C:\...` — tells the caller nothing about the workdir rule
		// it just broke. No Unix input reaches this branch, so this is the only
		// coverage it has.
		//
		// Also measured here (10.0.19045 / go1.26.6): filepath.EvalSymlinks(`Z:\`)
		// returns `Z:\` with a nil error for a drive letter with no volume behind
		// it, while os.Stat on the same root fails. That is why the resolution
		// comes back purely lexical, and why the walk's termination guard stays
		// unreached even on this shape (see internal/util/path.go).
		letter := unusedDriveLetter(t)
		in := letter + `:\multica-does-not-exist-0d1f\a\b`
		if got := util.ResolveSymlinksBestEffort(in); got != filepath.Clean(in) {
			t.Errorf("ResolveSymlinksBestEffort(%q) = %q, want %q", in, got, filepath.Clean(in))
		}
		within, err := fileWithinWorkingDir(in)
		if err != nil {
			t.Fatalf("fileWithinWorkingDir(%q) must report a cross-volume path as outside, not as an error: %v", in, err)
		}
		if within {
			t.Errorf("fileWithinWorkingDir(%q) = true, want false", in)
		}
	})
}

// unusedDriveLetter returns a drive letter with no volume behind it. os.Stat is
// the test, not filepath.EvalSymlinks: on go1.26.2 the latter reports success
// for such a root, which is exactly the observation this file records.
func unusedDriveLetter(t *testing.T) string {
	t.Helper()
	for _, letter := range "ZYXWVU" {
		root := string(letter) + `:\`
		if _, err := os.Stat(root); err != nil {
			return string(letter)
		}
	}
	t.Skip("every candidate drive letter is mounted on this host; cannot build a path on an absent volume")
	return ""
}

// TestFileWithinWorkingDirWindowsJunctionIsAKnownGap pins what the guard does
// with a directory junction, the one containment-relevant link shape Windows has
// that filepath.EvalSymlinks will not descend through (this repo asserts that
// refusal directly in internal/daemon/config_windows_test.go). Measured on
// 10.0.19045 / go1.26.6, inside `go test` and again from a standalone program
// run in this module's context:
//
//	os.Lstat(junction)                 mode=?rw-rw-rw- symlinkBit=false irregular=true
//	fsutil reparsepoint query          0xa0000003, Name Surrogate, Mount Point
//	EvalSymlinks("escape")             returns "escape", nil        <- resolves to itself
//	EvalSymlinks(`escape\stale.md`)    "", The system cannot find the path specified.
//
// The third line is why the gap exists: the junction itself looks like a
// perfectly resolved directory, so the ancestor walk stops there and re-attaches
// the tail lexically. A junction inside the workdir pointing out of it therefore
// reads as inside — before this change and after it.
//
// Which of those two behaviours you get is not a property of the host. It is the
// GODEBUG `winsymlink` setting, whose default follows the MAIN MODULE's go
// directive (os/types_windows.go: mode() files a mount point under
// ModeIrregular, modePreGo1_23() files it under ModeSymlink;
// internal/godebugs/table.go: `winsymlink, Changed: 23, Old: "0"`). This module
// declares go 1.26.6, so the modern semantics above are what the CLI ships with,
// and the same is true of the daemon-side test that asserts the refusal.
//
// Outside a module there is no go directive to follow, so the default comes from
// the toolchain that compiled the probe instead — which under GOTOOLCHAIN=auto is
// the base go on PATH, not the one this module selects. A probe built that way
// with a pre-1.23 base toolchain resolves junctions happily and contradicts
// everything above: take junction observations from inside this module, or state
// the toolchain.
//
// The gap is deliberate, not overlooked. Failing closed on a component that
// exists but cannot be canonicalized would reject every path under a pnpm-style
// node_modules junction, and worse: on a host whose workdir is itself reached
// through a junction it would reject every --content-file and --attachment on
// that host, which is a larger break than the one it prevents.
//
// Closing it does not need GetFinalPathNameByHandle: os.Readlink answers on a
// junction under both winsymlink settings, returning a clean absolute target with
// no \??\ prefix, and it answers even when the target no longer exists. Measured
// in this module's context on the same host, alongside the two facts that decide
// whether resolving is safe: a junction pointing INSIDE the workdir (the pnpm
// shape) resolves to a path still inside it, so resolving costs no false
// rejections; and a junction chain needs a bounded follow loop, since one
// Readlink step only reaches the next link. That is a behaviour change on Windows
// with its own blast radius, so it belongs in its own change rather than riding
// along here.
//
// The assertion is written so it fails, loudly and with instructions, if the
// platform ever starts resolving junctions: the guard's answer must track what
// the resolution can actually see.
func TestFileWithinWorkingDirWindowsJunctionIsAKnownGap(t *testing.T) {
	workdir := t.TempDir()
	t.Chdir(workdir)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "stale.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	createJunction(t, outside, filepath.Join(workdir, "escape"))

	candidate := `escape\stale.md`
	resolved, evalErr := filepath.EvalSymlinks(candidate)
	within, err := fileWithinWorkingDir(candidate)
	if err != nil {
		t.Fatalf("fileWithinWorkingDir(%q): %v", candidate, err)
	}
	t.Logf("EvalSymlinks(%q) = %q err=%v", candidate, resolved, evalErr)

	if evalErr == nil {
		// The platform resolves junctions here, so the guard has to reject.
		if within {
			t.Errorf("EvalSymlinks resolves junctions on this host, so %q must read as outside the workdir", candidate)
		}
		return
	}
	if !within {
		t.Fatalf("%q was rejected, which means junction resolution started working "+
			"somewhere in this path: flip this test to assert rejection and delete "+
			"the known-gap note above it", candidate)
	}
	t.Logf("known gap pinned: EvalSymlinks refuses the junction (%v), so the guard judges %q lexically and admits it", evalErr, candidate)
}
