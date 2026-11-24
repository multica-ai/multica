//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
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
// junction test below), so a fixture that fatals on that observation would
// destroy the coverage of the test it sets up. The bits are logged instead, so
// a CI run records what the platform did.
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
// separate, and several relative kinds do not resolve against the working
// directory at all. cmd/multica is not part of any Windows job otherwise, so
// without this file none of that is covered anywhere. It matters twice over on
// a non-admin host, where the cross-platform cases skip for want of the symlink
// privilege while a junction needs none.
func TestFileWithinWorkingDirWindowsPaths(t *testing.T) {
	workdir := t.TempDir()
	t.Chdir(workdir)
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

	t.Run("a root-relative path is judged against its volume root, not the workdir", func(t *testing.T) {
		// `\tmp\desc.md` is not relative to the working directory: the loader
		// resolves it against the root of the current directory's volume, so
		// os.ReadFile opens `C:\tmp\desc.md` no matter where the workdir sits.
		// A guard that prefixes the workdir instead inspects
		// `C:\task\workdir\tmp\desc.md` — and with that shadow present, an
		// outside read passes on the shadow. The drive-relative sibling case
		// (`C:tmp\desc.md`) is the same trap one level up. Both must read as
		// outside here, and the drive-relative same-drive case below pins the
		// one shape that legitimately DOES resolve into the workdir.
		if err := os.MkdirAll(filepath.Join(workdir, "tmp"), 0o755); err != nil {
			t.Fatalf("mkdir shadow dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(workdir, "tmp", "desc.md"), []byte("shadow"), 0o644); err != nil {
			t.Fatalf("write shadow: %v", err)
		}
		for _, candidate := range []string{`\tmp\desc.md`, "/tmp/desc.md"} {
			within, err := fileWithinWorkingDir(candidate)
			if err != nil {
				t.Fatalf("fileWithinWorkingDir(%q): %v", candidate, err)
			}
			if within {
				t.Errorf("fileWithinWorkingDir(%q) = true; the loader would read the volume root's %q, not the in-workdir shadow", candidate, candidate)
			}
		}
	})

	t.Run("a drive-relative path on the current drive resolves against the workdir", func(t *testing.T) {
		// `C:tmp\desc.md` resolves against the current directory on C:, which
		// for the current drive IS the process working directory — so this
		// genuinely reads workdir\tmp\desc.md, and the guard admits it.
		if err := os.MkdirAll(filepath.Join(workdir, "tmp"), 0o755); err != nil {
			t.Fatalf("mkdir fixture dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(workdir, "tmp", "desc.md"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		vol := filepath.VolumeName(workdir)
		if vol == "" {
			t.Fatalf("workdir %q carries no volume name", workdir)
		}
		candidate := vol + `tmp\desc.md`
		within, err := fileWithinWorkingDir(candidate)
		if err != nil {
			t.Fatalf("fileWithinWorkingDir(%q): %v", candidate, err)
		}
		if !within {
			t.Errorf("fileWithinWorkingDir(%q) = false; on the current drive this resolves against the workdir itself", candidate)
		}
	})

	t.Run("a drive-relative path on another drive reads as outside the workdir", func(t *testing.T) {
		// The per-drive current directory of a drive the process is not on
		// cannot be observed, so the resolution hands the path back
		// uncanonicalized and the guard must fail closed: filepath.Rel
		// refuses to relate it to the workdir's volume.
		letter := unusedDriveLetter(t)
		candidate := letter + `:report.md`
		within, err := fileWithinWorkingDir(candidate)
		if err != nil {
			t.Fatalf("fileWithinWorkingDir(%q) must report the cross-drive path as outside, not as an error: %v", candidate, err)
		}
		if within {
			t.Errorf("fileWithinWorkingDir(%q) = true, want false", candidate)
		}
	})

	t.Run("a path on another volume reads as outside the workdir", func(t *testing.T) {
		// The Windows-only branch of the guard: filepath.Rel cannot relate two
		// paths on different volumes, and returns an error rather than a "..".
		// Reporting that error as a resolve failure — `Rel: can't make Z:\...
		// relative to C:\...` — tells the caller nothing about the workdir rule
		// it just broke. No Unix input reaches this branch, so this is the only
		// coverage it has.
		//
		// The resolution comes back purely lexical here, because the walk
		// starts from the root without needing it to resolve: a drive letter
		// with no volume behind it never has to answer for itself (measured on
		// 10.0.19045 / go1.26.6: os.Stat fails at such a root while
		// filepath.EvalSymlinks reports success).
		letter := unusedDriveLetter(t)
		in := letter + `:\multica-does-not-exist-0d1f\a\b`
		got, err := util.ResolveSymlinksBestEffort(in)
		if err != nil {
			t.Fatalf("ResolveSymlinksBestEffort(%q): %v", in, err)
		}
		if got != filepath.Clean(in) {
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

	t.Run("a root-relative symlink target is judged against the volume root", func(t *testing.T) {
		// A symlink whose stored target is root-relative (`\Users\...\out`) is
		// resolved by the kernel from the root of the volume the link sits on,
		// NOT from the link's directory. A resolver that splices the target
		// onto the link's parent judges the shadow created below, while
		// os.ReadFile opens the volume-root path — so the shadow exists on
		// purpose and this test fails while the resolution is
		// link-parent-relative.
		outside := t.TempDir()
		out := filepath.Join(outside, "out")
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatalf("mkdir out: %v", err)
		}
		if err := os.WriteFile(filepath.Join(out, "stale.md"), []byte("outside"), 0o644); err != nil {
			t.Fatalf("write out: %v", err)
		}
		rel := strings.TrimPrefix(out, filepath.VolumeName(out))
		if err := os.MkdirAll(filepath.Join(workdir, rel), 0o755); err != nil {
			t.Fatalf("mkdir shadow: %v", err)
		}
		if err := os.WriteFile(filepath.Join(workdir, rel, "stale.md"), []byte("shadow"), 0o644); err != nil {
			t.Fatalf("write shadow: %v", err)
		}
		createDirSymlink(t, rel, filepath.Join(workdir, "rrel"))
		candidate := `rrel\stale.md`
		if got, err := os.ReadFile(candidate); err != nil {
			t.Fatalf("read through the link: %v", err)
		} else if string(got) != "outside" {
			t.Fatalf("the kernel opened the shadow (%q), not the volume-root target — mklink stored the root-relative target as link-relative on this host, and the test's premise does not hold", got)
		}
		within, err := fileWithinWorkingDir(candidate)
		if err != nil {
			t.Fatalf("fileWithinWorkingDir(%q): %v", candidate, err)
		}
		if within {
			t.Errorf("fileWithinWorkingDir(%q) = true; the kernel resolves the root-relative target %q from the volume root, not from the link's directory", candidate, rel)
		}
	})

	t.Run("a volume-GUID mount target is judged in its own namespace", func(t *testing.T) {
		// A junction whose substitute name is `\\?\Volume{GUID}\...` is
		// absolute in the device namespace: the kernel jumps to that volume
		// directly, and the file through it is really readable — this is not
		// an inert shape. There is no Win32 spelling to relate to a
		// drive-letter workdir, so the guard must reject; a resolver that
		// strips the `\\?\` prefix turns the absolute mount target into a
		// relative path it splices under the workdir, where the shadow below
		// would pass for the real outside read.
		out := filepath.Join(t.TempDir(), "out")
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatalf("mkdir out: %v", err)
		}
		if err := os.WriteFile(filepath.Join(out, "stale.md"), []byte("outside"), 0o644); err != nil {
			t.Fatalf("write out: %v", err)
		}
		guid := volumeGUIDOfSystemDrive(t)
		vol := filepath.VolumeName(out)
		guidTarget := guid + strings.TrimPrefix(out, vol+`\`)
		createJunction(t, guidTarget, filepath.Join(workdir, "vg"))
		shadow := filepath.Join(workdir, strings.TrimPrefix(guid, `\\?\`)+strings.TrimPrefix(out, vol+`\`))
		if err := os.MkdirAll(filepath.Dir(shadow), 0o755); err != nil {
			t.Fatalf("mkdir shadow: %v", err)
		}
		if err := os.WriteFile(shadow, []byte("shadow"), 0o644); err != nil {
			t.Fatalf("write shadow: %v", err)
		}
		candidate := `vg\stale.md`
		if got, err := os.ReadFile(candidate); err != nil {
			t.Fatalf("read through the junction: %v", err)
		} else if string(got) != "outside" {
			t.Fatalf("the kernel opened the shadow (%q), not the GUID-mounted target; the fixture does not hold on this host", got)
		}
		within, err := fileWithinWorkingDir(candidate)
		if err != nil {
			t.Fatalf("fileWithinWorkingDir(%q): %v", candidate, err)
		}
		if within {
			t.Errorf("fileWithinWorkingDir(%q) = true; the junction's target %q is a device-namespace mount the kernel opens outside the workdir", candidate, guidTarget)
		}
	})
}

// unusedDriveLetter returns a drive letter with no volume behind it. os.Stat is
// the test, not filepath.EvalSymlinks: on go1.26.2 the latter reports success
// for such a root, which is exactly the observation the cross-volume case
// records.
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

// createDirSymlink makes link a directory symlink to the target string as
// given. os.Symlink is not used for this: it decides the relative flag from
// filepath.IsAbs, and a root-relative spelling (`\Users\...`) is not IsAbs,
// while mklink marks a rooted target absolute — which is the kernel behavior
// the root-relative case pins. Directory symlinks need the symlink privilege;
// CI runners have it and non-admin hosts do not, which is why the fixture
// skips rather than fails there.
func createDirSymlink(t *testing.T, target, link string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/D", link, target).CombinedOutput(); err != nil {
		if strings.Contains(strings.ToLower(string(out)), "privilege") {
			t.Skipf("symlink privilege unavailable on this host: %s", out)
		}
		t.Fatalf("mklink /D %s -> %s: %s: %v", link, target, out, err)
	}
}

// volumeGUIDRe matches the `\\?\Volume{GUID}\` form mountvol prints for a
// mounted volume.
var volumeGUIDRe = regexp.MustCompile(`\\\\\?\\Volume\{[0-9a-fA-F-]+\}\\`)

// volumeGUIDOfSystemDrive returns the device-namespace spelling of the system
// drive's volume, the form a junction can carry as its substitute name and a
// plain Win32 comparison cannot relate to a drive-letter workdir.
func volumeGUIDOfSystemDrive(t *testing.T) string {
	t.Helper()
	drive := os.Getenv("SystemDrive")
	if drive == "" {
		drive = `C:`
	}
	out, err := exec.Command("mountvol", drive, "/L").CombinedOutput()
	if err != nil {
		t.Fatalf("mountvol %s /L: %s: %v", drive, out, err)
	}
	guid := volumeGUIDRe.FindString(string(out))
	if guid == "" {
		t.Fatalf("mountvol %s /L printed no volume GUID: %s", drive, out)
	}
	return guid
}

// TestFileWithinWorkingDirWindowsJunctions pins how the guard treats directory
// junctions — the one containment-relevant link shape Windows has that needs no
// elevation to create, which is why it is the shape that actually shows up on
// real hosts (pnpm's node_modules layout is built from them).
//
// The resolution behind the guard follows junctions itself, because under the
// winsymlink semantics this module's go directive selects (Go 1.23+) it is
// filepath.EvalSymlinks that refuses to descend through them: it resolves the
// junction to its own name, which used to let `workdir\escape\stale.md` —
// escape a junction pointing out of the workdir — read as inside. Which
// semantics apply is not a property of the host but of the GODEBUG setting
// (internal/godebugs/table.go: `winsymlink, Changed: 23, Old: "0"`), so
// os.Readlink, which answers on a junction under both, is what the resolution
// uses (see internal/util/path_windows.go). The same refusal is asserted
// daemon-side in internal/daemon/config_windows_test.go, which is about
// filepath.EvalSymlinks and unaffected by this.
//
// A junction pointing INSIDE the workdir — the pnpm shape — must keep reading
// as inside: resolving costs no false rejections there, measured alongside the
// escape facts on the same host.
func TestFileWithinWorkingDirWindowsJunctions(t *testing.T) {
	workdir := t.TempDir()
	t.Chdir(workdir)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "stale.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	createJunction(t, outside, filepath.Join(workdir, "escape"))

	t.Run("a junction out of the workdir is rejected", func(t *testing.T) {
		candidate := `escape\stale.md`
		within, err := fileWithinWorkingDir(candidate)
		if err != nil {
			t.Fatalf("fileWithinWorkingDir(%q): %v", candidate, err)
		}
		if within {
			t.Errorf("fileWithinWorkingDir(%q) = true; the junction resolves to %q, outside the workdir", candidate, outside)
		}
	})

	t.Run("a junction into the workdir is still admitted", func(t *testing.T) {
		// The false-rejection direction: a junction whose target stays inside
		// the workdir resolves to a path that is still inside it, and the
		// guard must not start rejecting the pnpm shape.
		if err := os.MkdirAll(filepath.Join(workdir, "inner"), 0o755); err != nil {
			t.Fatalf("mkdir inner: %v", err)
		}
		if err := os.WriteFile(filepath.Join(workdir, "inner", "file.md"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		createJunction(t, filepath.Join(workdir, "inner"), filepath.Join(workdir, "link"))
		candidate := `link\file.md`
		within, err := fileWithinWorkingDir(candidate)
		if err != nil {
			t.Fatalf("fileWithinWorkingDir(%q): %v", candidate, err)
		}
		if !within {
			t.Errorf("fileWithinWorkingDir(%q) = false; the junction's target is inside the workdir", candidate)
		}
	})

	t.Run("a junction chain out of the workdir is rejected", func(t *testing.T) {
		// One Readlink step only reaches the next link, so the resolution
		// splices targets back into the walk; a chain must not fall off the
		// end of that into a lexical judgment.
		mid := filepath.Join(workdir, "mid")
		if err := os.Mkdir(mid, 0o755); err != nil {
			t.Fatalf("mkdir mid: %v", err)
		}
		createJunction(t, outside, filepath.Join(mid, "hop"))
		createJunction(t, mid, filepath.Join(workdir, "chain"))
		candidate := `chain\hop\stale.md`
		within, err := fileWithinWorkingDir(candidate)
		if err != nil {
			t.Fatalf("fileWithinWorkingDir(%q): %v", candidate, err)
		}
		if within {
			t.Errorf("fileWithinWorkingDir(%q) = true; the junction chain resolves to %q, outside the workdir", candidate, outside)
		}
	})

	t.Run("a junction whose target is gone resolves lexically and stays inert", func(t *testing.T) {
		// os.Readlink still answers when the target no longer exists, but the
		// walk cannot resolve THROUGH the dead target, so the junction becomes
		// the first unresolvable component and the tail is re-attached
		// lexically — reading as inside. That is the documented inert shape,
		// not a hole: the kernel stops at the junction too, so nothing after
		// it can be opened and os.ReadFile surfaces the not-found error. The
		// containment risk only exists while the target resolves, which the
		// first case rejects. If this ever starts reading as outside — the
		// resolution learned to carry the target's missing tail through a
		// dead junction — the guard's answer gets stricter, which is safe.
		dangling := t.TempDir()
		createJunction(t, dangling, filepath.Join(workdir, "dangle"))
		if err := os.Remove(dangling); err != nil {
			t.Fatalf("remove junction target: %v", err)
		}
		candidate := `dangle\stale.md`
		within, err := fileWithinWorkingDir(candidate)
		if err != nil {
			t.Fatalf("fileWithinWorkingDir(%q): %v", candidate, err)
		}
		if !within {
			t.Errorf("fileWithinWorkingDir(%q) = false; a dead junction resolves nothing and the kernel cannot open the path either way", candidate)
		}
		if _, err := os.ReadFile(candidate); !strings.Contains(err.Error(), "cannot find") {
			t.Logf("os.ReadFile(%q) = %v (the guard's admission is inert only while that is a not-found)", candidate, err)
		}
	})
}
