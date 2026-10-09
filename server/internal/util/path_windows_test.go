//go:build windows

package util

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// TestClassifyTarget pins the link-target grammar on pure string inputs, with
// no filesystem fixture: every branch is a classification decision the walk
// relies on, and the ordering between them is load-bearing — VolumeName is
// non-empty for drive-absolute paths too, so the IsAbs branch must be asked
// before the drive-relative one or a junction's absolute target resolves
// against the working directory (which is exactly how a junction escape read
// as inside the workdir before this table existed).
func TestClassifyTarget(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	vol := filepath.VolumeName(cwd)
	sep := string(filepath.Separator)

	cases := []struct {
		name         string
		target       string
		wantBase     string
		wantTail     string
		wantVerbatim bool
		wantOK       bool
	}{
		{
			name:     "drive-absolute target is walked from its own volume root",
			target:   vol + `\outside\dir`,
			wantBase: vol + sep,
			wantTail: `outside\dir`,
			wantOK:   true,
		},
		{
			name:     "UNC target is walked from the share root",
			target:   `\\srv\share\dir`,
			wantBase: `\\srv\share` + sep,
			wantTail: `dir`,
			wantOK:   true,
		},
		{
			name:     "root-relative target resolves from the link's volume root, not the link's directory",
			target:   `\outside\dir`,
			wantBase: vol + sep,
			wantTail: `outside\dir`,
			wantOK:   true,
		},
		{
			name:     "drive-relative target on the current drive resolves against the working directory",
			target:   vol + `outside\dir`,
			wantBase: vol + sep,
			// The working directory's components are part of the walked tail,
			// not of the root the walk starts from.
			wantTail: strings.TrimPrefix(cwd, vol+sep) + sep + `outside` + sep + `dir`,
			wantOK:   true,
		},
		{
			name:   "drive-relative target on another drive is unobservable",
			target: `Q:outside\dir`,
			wantOK: false,
		},
		{
			name:     "plain-relative target keeps the link's directory as base",
			target:   `relative\dir`,
			wantBase: cwd,
			wantTail: `relative\dir`,
			wantOK:   true,
		},
		{
			name:     "extended-length drive path reduces to its Win32 spelling",
			target:   `\\?\` + vol + `\outside\dir`,
			wantBase: vol + sep,
			wantTail: `outside\dir`,
			wantOK:   true,
		},
		{
			name:     "extended-length UNC path reduces to its Win32 spelling",
			target:   `\\?\UNC\srv\share\dir`,
			wantBase: `\\srv\share` + sep,
			wantTail: `dir`,
			wantOK:   true,
		},
		{
			name:         "volume-GUID target has no Win32 spelling and stays verbatim",
			target:       `\\?\Volume{01234567-89ab-cdef-0123-456789abcdef}\dir`,
			wantBase:     `\\?\Volume{01234567-89ab-cdef-0123-456789abcdef}\dir`,
			wantVerbatim: true,
			wantOK:       true,
		},
		{
			name:     "object-manager root is stripped before classification",
			target:   `\??\` + vol + `\outside\dir`,
			wantBase: vol + sep,
			wantTail: `outside\dir`,
			wantOK:   true,
		},
		{
			name:     "object-manager UNC target becomes a Win32 UNC path",
			target:   `\??\UNC\srv\share\dir`,
			wantBase: `\\srv\share` + sep,
			wantTail: `dir`,
			wantOK:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base, segs, verbatim, ok := classifyTarget(tc.target, cwd)
			if ok != tc.wantOK {
				t.Fatalf("classifyTarget(%q) ok = %v, want %v", tc.target, ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if verbatim != tc.wantVerbatim {
				t.Fatalf("classifyTarget(%q) verbatim = %v, want %v", tc.target, verbatim, tc.wantVerbatim)
			}
			if base != tc.wantBase {
				t.Errorf("classifyTarget(%q) base = %q, want %q", tc.target, base, tc.wantBase)
			}
			if tc.wantVerbatim {
				return
			}
			if got := filepath.Join(segs...); !strings.EqualFold(got, tc.wantTail) {
				t.Errorf("classifyTarget(%q) segs = %q, want %q", tc.target, got, tc.wantTail)
			}
		})
	}

	t.Run("a drive-absolute target is never misread as drive-relative", func(t *testing.T) {
		// The regression the table exists to catch, spelled out: VolumeName is
		// non-empty for `C:\dir` too, so if the IsAbs branch is not asked
		// first, a junction's absolute target resolves against the working
		// directory and an escape reads as inside the workdir.
		base, _, _, ok := classifyTarget(vol+`\outside\dir`, cwd)
		if !ok {
			t.Fatal("classifyTarget reported !ok for a drive-absolute target")
		}
		if !strings.EqualFold(base, vol+sep) {
			t.Fatalf("classifyTarget(%q) base = %q, want the volume root %q", vol+`\outside\dir`, base, vol+sep)
		}
	})
}

// mklinkJunction makes dst a directory junction to src. mklink /J is used
// directly so the junction shape is exercised even on a runner where symlinks
// are permitted.
func mklinkJunction(t *testing.T, src, dst string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", dst, src).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J %s %s: %s: %v", dst, src, out, err)
	}
}

// TestResolveSymlinksFollowsJunctions is #8946. A workspaces root moved to
// another drive and left behind as a junction put a junction in the MIDDLE of
// every task path, where filepath.EvalSymlinks fails with ENOTDIR, and the
// checkout authorization built on it refused every checkout. A junction at the
// END of a path is the opposite failure: EvalSymlinks returns it unfollowed,
// so a junction pointing out of a workdir reads as inside it. ResolveSymlinks
// must follow both, fail on a missing component, and — for the spelling
// callers compare — land on exactly what EvalSymlinks gives the junction-free
// path (t.TempDir sits under an 8.3 short name on hosted runners, which is
// what the spelling check exercises).
func TestResolveSymlinksFollowsJunctions(t *testing.T) {
	target := t.TempDir()
	workdirViaTarget := filepath.Join(target, "ws", "task", "workdir")
	if err := os.MkdirAll(workdirViaTarget, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	root := filepath.Join(t.TempDir(), "multica_workspaces")
	mklinkJunction(t, target, root)
	outside := t.TempDir()
	escape := filepath.Join(workdirViaTarget, "escape")
	mklinkJunction(t, outside, escape)

	workdirViaRoot := filepath.Join(root, "ws", "task", "workdir")
	if _, err := filepath.EvalSymlinks(workdirViaRoot); err == nil {
		t.Logf("filepath.EvalSymlinks now passes through a junction on this platform; ResolveSymlinks is still required for a junction at the end of a path")
	}

	wantWorkdir, err := filepath.EvalSymlinks(workdirViaTarget)
	if err != nil {
		t.Fatalf("resolve target workdir: %v", err)
	}
	wantOutside, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatalf("resolve outside: %v", err)
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "junction in the middle of the path", in: workdirViaRoot, want: wantWorkdir},
		{name: "junction-free path keeps EvalSymlinks' spelling", in: workdirViaTarget, want: wantWorkdir},
		{name: "junction at the end of the path is followed", in: escape, want: wantOutside},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveSymlinks(tc.in)
			if err != nil {
				t.Fatalf("ResolveSymlinks(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ResolveSymlinks(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	missing := filepath.Join(root, "ws", "missing")
	if got, err := ResolveSymlinks(missing); err == nil {
		t.Fatalf("ResolveSymlinks(%q) = %q; a missing component behind a junction must be an error", missing, got)
	}
}

// volumeGUIDRoot returns the `\\?\Volume{GUID}\` name of the volume dir lives
// on — a spelling of it with no Win32 form to walk.
func volumeGUIDRoot(t *testing.T, dir string) string {
	t.Helper()
	mountPoint, err := windows.UTF16PtrFromString(filepath.VolumeName(dir) + `\`)
	if err != nil {
		t.Fatalf("encode mount point: %v", err)
	}
	buf := make([]uint16, 64)
	if err := windows.GetVolumeNameForVolumeMountPoint(mountPoint, &buf[0], uint32(len(buf))); err != nil {
		t.Fatalf("volume GUID of %s: %v", dir, err)
	}
	return windows.UTF16ToString(buf)
}

// TestResolveSymlinksExtendedLengthPaths pins the device-namespace exits of
// the walk. evalPath hands back a `\\?\` input unwalked and joins a path
// lexically onto a volume-GUID link target; for the best-effort resolver that
// fails closed against a drive-letter root, but a strict caller can compare two
// device spellings with each other, where `\\?\C:\w\escape\sub` reads as inside
// `\\?\C:\w` although escape leads to another directory. ResolveSymlinks must
// walk every extended-length path that has a Win32 spelling and refuse the ones
// that do not.
func TestResolveSymlinksExtendedLengthPaths(t *testing.T) {
	workdir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workdir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	mklinkJunction(t, outside, filepath.Join(workdir, "escape"))

	wantWorkdir, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		t.Fatalf("resolve workdir: %v", err)
	}
	wantOutsideSub, err := filepath.EvalSymlinks(filepath.Join(outside, "sub"))
	if err != nil {
		t.Fatalf("resolve outside: %v", err)
	}

	resolves := []struct {
		name string
		in   string
		want string
	}{
		{name: "extended-length directory", in: `\\?\` + workdir, want: wantWorkdir},
		{name: "extended-length path through a junction is walked, not joined", in: `\\?\` + filepath.Join(workdir, "escape", "sub"), want: wantOutsideSub},
	}
	for _, tc := range resolves {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveSymlinks(tc.in)
			if err != nil {
				t.Fatalf("ResolveSymlinks(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ResolveSymlinks(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	missing := `\\?\` + filepath.Join(workdir, "missing")
	if got, err := ResolveSymlinks(missing); err == nil {
		t.Fatalf("ResolveSymlinks(%q) = %q; a missing extended-length path must be an error", missing, got)
	}

	guidOutside := volumeGUIDRoot(t, outside) + strings.TrimPrefix(outside, filepath.VolumeName(outside)+`\`)
	guidLink := filepath.Join(workdir, "vg")
	mklinkJunction(t, guidOutside, guidLink)
	refused := []struct {
		name string
		in   string
	}{
		{name: "volume-GUID path", in: guidOutside},
		{name: "junction into a volume GUID", in: filepath.Join(guidLink, "sub")},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveSymlinks(tc.in)
			if !errors.Is(err, ErrUnresolvablePath) {
				t.Fatalf("ResolveSymlinks(%q) = %q, %v; want ErrUnresolvablePath", tc.in, got, err)
			}
		})
	}
}
