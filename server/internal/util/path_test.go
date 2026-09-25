package util

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveSymlinksBestEffort pins the property every containment check built
// on this helper depends on: the returned path is in the SAME namespace as
// filepath.EvalSymlinks would produce for the existing part of the input, no
// matter how much of the tail is missing. filepath.EvalSymlinks alone fails on
// any missing component, and the obvious fallbacks (Clean, or a single Dir
// step) silently return an unresolved path, which makes a path inside a
// symlinked root compare as outside it.
func TestResolveSymlinksBestEffort(t *testing.T) {
	// realRoot is the canonical form of a directory reached through a symlink,
	// so every expectation below can be written against the resolved namespace.
	root := t.TempDir()
	physical := filepath.Join(root, "physical")
	if err := os.MkdirAll(filepath.Join(physical, "existing"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(physical, "existing", "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	logical := filepath.Join(root, "logical")
	if err := os.Symlink(physical, logical); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	realPhysical, err := filepath.EvalSymlinks(physical)
	if err != nil {
		t.Fatalf("resolve physical: %v", err)
	}
	if err := os.Symlink(filepath.Join(physical, "nowhere"), filepath.Join(physical, "dangling")); err != nil {
		t.Fatalf("symlink dangling: %v", err)
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "existing file behind a symlinked ancestor",
			in:   filepath.Join(logical, "existing", "file.txt"),
			want: filepath.Join(realPhysical, "existing", "file.txt"),
		},
		{
			name: "missing leaf keeps the resolved parent",
			in:   filepath.Join(logical, "existing", "missing.txt"),
			want: filepath.Join(realPhysical, "existing", "missing.txt"),
		},
		{
			name: "missing intermediate directory still resolves the ancestor",
			in:   filepath.Join(logical, "subdir", "file.txt"),
			want: filepath.Join(realPhysical, "subdir", "file.txt"),
		},
		{
			name: "several missing levels still resolve the ancestor",
			in:   filepath.Join(logical, "a", "b", "c", "d", "file.txt"),
			want: filepath.Join(realPhysical, "a", "b", "c", "d", "file.txt"),
		},
		{
			name: "dangling symlink is not followed but its parent is resolved",
			in:   filepath.Join(logical, "dangling"),
			want: filepath.Join(realPhysical, "dangling"),
		},
		{
			name: "directory itself",
			in:   logical,
			want: realPhysical,
		},
		{
			name: "empty input is returned unchanged",
			in:   "",
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveSymlinksBestEffort(tc.in)
			if err != nil {
				t.Fatalf("ResolveSymlinksBestEffort(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ResolveSymlinksBestEffort(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	t.Run("relative input is made absolute against the working directory", func(t *testing.T) {
		t.Chdir(logical)
		got, err := ResolveSymlinksBestEffort(filepath.Join("subdir", "file.txt"))
		if err != nil {
			t.Fatalf("ResolveSymlinksBestEffort(relative): %v", err)
		}
		want := filepath.Join(realPhysical, "subdir", "file.txt")
		if got != want {
			t.Errorf("ResolveSymlinksBestEffort(relative) = %q, want %q", got, want)
		}
	})

	t.Run("dot-dot is applied after following a symlink, not to the string", func(t *testing.T) {
		// The distinction a containment check lives or dies by: lexically
		// "link/.." cancels out and the path looks like it never left, while the
		// kernel follows the link first and then goes up — landing next to the
		// link's TARGET. Anything that cleans before resolving reports the wrong
		// namespace here, and does so for a path that exists and can be read.
		sep := string(filepath.Separator)
		got, err := ResolveSymlinksBestEffort(filepath.Join(logical, "existing") + sep + ".." + sep + "sibling.md")
		if err != nil {
			t.Fatalf("ResolveSymlinksBestEffort(dot-dot): %v", err)
		}
		want := filepath.Join(realPhysical, "sibling.md")
		if got != want {
			t.Errorf("ResolveSymlinksBestEffort(dot-dot) = %q, want %q", got, want)
		}

		outsideRoot := t.TempDir()
		target := filepath.Join(outsideRoot, "shared")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(outsideRoot, "other-run.md"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		link := filepath.Join(physical, "escape")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		realOutside, err := filepath.EvalSymlinks(outsideRoot)
		if err != nil {
			t.Fatalf("resolve outside: %v", err)
		}
		got, err = ResolveSymlinksBestEffort(link + sep + ".." + sep + "other-run.md")
		if err != nil {
			t.Fatalf("ResolveSymlinksBestEffort(escape/..): %v", err)
		}
		want = filepath.Join(realOutside, "other-run.md")
		if got != want {
			t.Errorf("ResolveSymlinksBestEffort(escape/..) = %q, want %q", got, want)
		}
	})

	t.Run("dot-dot in the walk applies to the followed path even when the tail is missing", func(t *testing.T) {
		// The phase-one resolution only sees a ".." when the whole path exists.
		// Once something is missing the walk takes over, and it must apply the
		// ".." to the RESOLVED prefix there too: cleaning first would collapse
		// "esc/.." against the string and report the tail under the workdir,
		// while the kernel — if the missing components were ever created —
		// would place them next to the link's target.
		outsideRoot := t.TempDir()
		target := filepath.Join(outsideRoot, "out")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		link := filepath.Join(physical, "esc")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		realOutsideRoot, err := filepath.EvalSymlinks(outsideRoot)
		if err != nil {
			t.Fatalf("resolve outside root: %v", err)
		}
		// Concatenated, not filepath.Join: Join would clean "esc/.." out of
		// the input and the walk would never see the link at all — the very
		// mistake this file exists to catch.
		sep := string(filepath.Separator)
		in := filepath.Join(logical, "esc") + sep + ".." + sep + filepath.Join("missingdir", "x.md")
		want := filepath.Join(realOutsideRoot, "missingdir", "x.md")
		got, err := ResolveSymlinksBestEffort(in)
		if err != nil {
			t.Fatalf("ResolveSymlinksBestEffort(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("ResolveSymlinksBestEffort(%q) = %q, want %q", in, got, want)
		}
	})

	t.Run("a fully missing path keeps its lexical tail under the resolved root", func(t *testing.T) {
		// "/" always resolves, so the walk starts from it and re-attaches
		// everything below it lexically. The root itself is never resolved by
		// the walk — it is the starting point — which is what serves the
		// Windows shape in cmd/multica's cross-volume test: a drive letter
		// with no volume behind it yields the cleaned lexical form rather
		// than depending on what EvalSymlinks does at a root.
		in := filepath.Join(string(filepath.Separator), "multica-does-not-exist-0d1f", "a", "b")
		want, err := filepath.Abs(in)
		if err != nil {
			t.Fatalf("abs: %v", err)
		}
		got, err := ResolveSymlinksBestEffort(in)
		if err != nil {
			t.Fatalf("ResolveSymlinksBestEffort(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("ResolveSymlinksBestEffort(%q) = %q, want %q", in, got, want)
		}
	})
}
